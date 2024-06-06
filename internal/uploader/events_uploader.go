package uploader

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	eventModels "github.com/openshift/assisted-service/pkg/uploader/models"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
)

const (
	metadataFileName = "metadata.json"
)

type eventsUploader struct {
	Config
	db     *gorm.DB
	log    log.FieldLogger
	client k8sclient.K8SClient
}

func (e *eventsUploader) UploadEvents(ctx context.Context, cluster *common.Cluster, eventsHandler eventsapi.Handler) error {
	pullSecret, err := getPullSecret(cluster.PullSecret, e.client, openshiftTokenKey)
	if err != nil {
		return errors.Wrapf(err, "failed to get pull secret to upload event data for cluster %s", cluster.ID)
	}
	buffer, err := prepareFiles(ctx, e.db, cluster, eventsHandler, pullSecret, e.Config)
	if err != nil {
		return errors.Wrapf(err, "failed to prepare files to upload for cluster %s", cluster.ID)
	}
	formBuffer, contentType, err := prepareBody(buffer)
	if err != nil {
		return errors.Wrapf(err, "failed to prepare event data content to upload for cluster %s", *cluster.ID)
	}
	req, err := http.NewRequest(http.MethodPost, e.DataUploadEndpoint, formBuffer)
	if err != nil {
		return errors.Wrapf(err, "failed preparing new https request for endpoint %s", e.DataUploadEndpoint)
	}
	if err := e.setHeaders(req, cluster.ID, pullSecret.AuthRaw, contentType); err != nil {
		return errors.Wrapf(err, "failed setting header for endpoint %s", e.DataUploadEndpoint)
	}
	return e.sendRequest(req)
}

func (e *eventsUploader) IsEnabled() bool {
	return e.EnableDataCollection && isOCMPullSecretOptIn(e.client) && isURLReachable(e.DataUploadEndpoint)
}

func (e *eventsUploader) setHeaders(req *http.Request, clusterID *strfmt.UUID, token, formDataContentType string) error {
	if token == "" {
		return fmt.Errorf("no auth token available for cluster %s", clusterID)
	}
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Content-Type", formDataContentType)
	req.Header.Set("User-Agent", fmt.Sprintf("assisted-installer-operator/%s cluster/%s", e.AssistedServiceVersion, *clusterID))
	return nil
}

func (e *eventsUploader) sendRequest(req *http.Request) error {
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{},
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     time.Minute,
	}
	client := &http.Client{Transport: transport, Timeout: time.Minute}

	res, err := client.Do(req)
	if err != nil {
		return errors.Wrapf(err, "failed to send request to %s", req.URL)
	}
	// Successful http response code from the ingress server is in the 200s
	if res.StatusCode >= 200 && res.StatusCode <= 299 {
		e.log.Debugf("Successful response received from %s. Red Hat Insights Request ID: %+v", req.URL, res.Header.Get("X-Rh-Insights-Request-Id"))
		return nil
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		e.log.Debugf("error reading response body for request to %s: %s", req.URL, err.Error())
	}
	return fmt.Errorf("uploading events: upload to %s returned status code %d (body: %s)", req.URL, res.StatusCode, string(body))
}

func prepareBody(buffer *bytes.Buffer) (*bytes.Buffer, string, error) {
	if buffer == nil || buffer.Bytes() == nil {
		return nil, "", errors.Errorf("no data passed to prepare body")
	}
	mimeHeader := make(textproto.MIMEHeader)
	mimeHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`, "file", "events.tgz"))
	mimeHeader.Set("Content-Type", "application/vnd.redhat.assisted-installer.events+tar")

	var formBuffer bytes.Buffer
	w := multipart.NewWriter(&formBuffer)
	fw, err := w.CreatePart(mimeHeader)
	if err != nil {
		return nil, "", errors.Wrapf(err, "failed creating multipart section for request body")
	}

	if _, err := fw.Write(buffer.Bytes()); err != nil {
		return nil, "", err
	}

	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &formBuffer, w.FormDataContentType(), nil
}

func prepareFiles(ctx context.Context, db *gorm.DB, cluster *common.Cluster, eventsHandler eventsapi.Handler, pullSecret *validations.PullSecretCreds,
	config Config) (*bytes.Buffer, error) {
	buffer := &bytes.Buffer{}
	gz := gzip.NewWriter(buffer)
	tw := tar.NewWriter(gz)
	filesCreated := 0

	// errors creating files below will be ignored since failing to create one of the files
	// doesn't mean the rest of the function should fail
	if err := clusterFile(tw, cluster, pullSecret); err == nil {
		filesCreated++
	}

	infraEnvID, err := hostsFile(db, tw, cluster)
	if err == nil {
		filesCreated++
	}

	if err := infraEnvFile(db, tw, infraEnvID, cluster.ID); err == nil {
		filesCreated++
	}

	if err := eventsFile(ctx, cluster.ID, eventsHandler, tw); err == nil {
		filesCreated++
	}

	// There are no files to upload
	if filesCreated == 0 {
		return nil, errors.Errorf("no event data files created for cluster %s", cluster.ID)
	}

	// Add versions file to bundle
	if versionsJson, err := json.Marshal(versions.GetModelVersions(config.Versions)); err == nil {
		addFile(tw, versionsJson, fmt.Sprintf("%s/versions.json", *cluster.ID)) //nolint:errcheck // errors adding this file shouldn't prevent the data from being sent
	}

	// Add metadata file to bundle
	metadataFile(tw, cluster.ID, config)

	// produce tar
	if err := tw.Close(); err != nil {
		return nil, errors.Wrap(err, "failed closing tar file")
	}
	// produce gzip
	if err := gz.Close(); err != nil {
		return nil, errors.Wrap(err, "failed closing gzip file")
	}
	return buffer, nil
}

func metadataFile(tw *tar.Writer, clusterID *strfmt.UUID, config Config) {
	metadata := createMetadataContent(config)

	if metadataJson, err := json.Marshal(metadata); err == nil {
		addFile(tw, metadataJson, fmt.Sprintf("%s/%s", *clusterID, metadataFileName)) //nolint:errcheck // errors adding this file shouldn't prevent the data from being sent
	}
}

func createMetadataContent(config Config) eventModels.Metadata {
	return eventModels.Metadata{
		AssistedInstallerServiceVersion:    config.Versions.SelfVersion,
		DiscoveryAgentVersion:              config.Versions.AgentDockerImg,
		AssistedInstallerVersion:           config.Versions.InstallerImage,
		AssistedInstallerControllerVersion: config.Versions.ControllerImage,

		DeploymentType:    config.DeploymentType,
		DeploymentVersion: config.DeploymentVersion,
		GitRef:            config.AssistedServiceVersion,
	}
}

func eventsFile(ctx context.Context, clusterID *strfmt.UUID, eventsHandler eventsapi.Handler, tw *tar.Writer) error {
	if eventsHandler == nil {
		return errors.Errorf("failed to get events for cluster %s, events handler is nil", clusterID)
	}
	response, err := eventsHandler.V2GetEvents(ctx, common.GetDefaultV2GetEventsParams(clusterID, nil, nil, models.EventCategoryMetrics, models.EventCategoryUser))
	if err != nil {
		return errors.Wrapf(err, "failed to find events for cluster %s", clusterID)
	}

	var events []*models.Event
	for _, dbEvent := range response.GetEvents() {
		events = append(events, &dbEvent.Event)
	}

	contents, err := json.MarshalIndent(events, "", " ")
	if err != nil {
		return errors.Wrapf(err, "failed to marshal events")
	}
	return addFile(tw, contents, fmt.Sprintf("%s/events.json", *clusterID))
}

func clusterFile(tw *tar.Writer, cluster *common.Cluster, pullSecret *validations.PullSecretCreds) error {
	if cluster != nil && cluster.ID != nil {
		// To distinguish who is uploading the data
		if cluster.EmailDomain == "" || cluster.EmailDomain == "Unknown" {
			cluster.EmailDomain = getEmailDomain(pullSecret.Email)
		}
		if cluster.UserName == "" {
			cluster.UserName = pullSecret.Username
		}

		clusterJson, err := json.Marshal(cluster.Cluster)
		if err != nil {
			return errors.Wrapf(err, "failed to marshal cluster %s", *cluster.ID)
		}
		return addFile(tw, clusterJson, fmt.Sprintf("%s/events/cluster.json", *cluster.ID))
	}
	return errors.New("no cluster provided for cluster file")
}

func infraEnvFile(db *gorm.DB, tw *tar.Writer, infraEnvID *strfmt.UUID, clusterID *strfmt.UUID) error {
	if infraEnvID != nil {
		infraEnv, err := common.GetInfraEnvFromDB(db, *infraEnvID)
		if err != nil {
			return errors.Wrapf(err, "error getting infra-env %s from db", *infraEnvID)
		}
		iJson, err := json.Marshal(infraEnv.InfraEnv)
		if err != nil {
			return errors.Wrapf(err, "error marshalling infra-env %s", *infraEnvID)
		}
		// add the file content
		return addFile(tw, iJson, fmt.Sprintf("%s/events/infraenv.json", *clusterID))
	}
	return errors.Errorf("no infra-env id provided to upload data for cluster %s", clusterID)
}

func hostsFile(db *gorm.DB, tw *tar.Writer, cluster *common.Cluster) (*strfmt.UUID, error) {
	if cluster == nil || cluster.ID == nil {
		return nil, errors.New("no cluster specified for hosts file")
	}
	var infraEnvID *strfmt.UUID
	clusterWithHosts, err := common.GetClusterFromDBWithHosts(db, *cluster.ID)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to find cluster %s", *cluster.ID)
	}
	hosts := clusterWithHosts.Cluster.Hosts
	if len(hosts) > 0 {
		hostJson, err := json.MarshalIndent(hosts, "", " ")
		if err != nil {
			return nil, errors.Wrapf(err, "failed marshalling hosts for cluster %s for events file", *cluster.ID)
		}
		foundHost := funk.Find(hosts, func(host *models.Host) bool {
			return host.InfraEnvID.String() != ""
		})
		if host, ok := foundHost.(*models.Host); ok {
			infraEnvID = &host.InfraEnvID
		}
		return infraEnvID, addFile(tw, hostJson, fmt.Sprintf("%s/events/hosts.json", *cluster.ID))
	}
	return nil, errors.Errorf("no hosts found for cluster %s", *cluster.ID)
}

func addFile(tw *tar.Writer, contents []byte, fileName string) error {
	if len(contents) < 1 {
		return errors.Errorf("no file contents to write for %s", fileName)
	}
	// add the file content
	hdr := &tar.Header{
		Name: fileName,
		Mode: 0644,
		Size: int64(len(contents)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return errors.Wrapf(err, "failed writing file header for %s", fileName)
	}

	if _, err := tw.Write(contents); err != nil {
		return errors.Wrapf(err, "failed writing contents to file %s", fileName)
	}
	return nil
}

// This function indicates if it's a disconnected environment
func isURLReachable(url string) bool {
	client := http.Client{
		Timeout: time.Second,
	}
	_, err := client.Get(url)
	return err == nil
}
