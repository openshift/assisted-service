package bminventory

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/thoas/go-funk"
	"github.com/vincent-petithory/dataurl"

	"github.com/danielerez/go-dns-client/pkg/dnsproviders"
	"github.com/filanov/bm-inventory/internal/metrics"

	"github.com/filanov/bm-inventory/internal/cluster"
	"github.com/filanov/bm-inventory/internal/cluster/validations"
	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/internal/events"
	"github.com/filanov/bm-inventory/internal/host"
	"github.com/filanov/bm-inventory/internal/installcfg"
	"github.com/filanov/bm-inventory/internal/network"
	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/pkg/filemiddleware"
	"github.com/filanov/bm-inventory/pkg/job"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/filanov/bm-inventory/pkg/requestid"
	awsS3CLient "github.com/filanov/bm-inventory/pkg/s3Client"
	"github.com/filanov/bm-inventory/pkg/transaction"
	"github.com/filanov/bm-inventory/restapi"
	"github.com/filanov/bm-inventory/restapi/operations/installer"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

const kubeconfigPrefix = "generate-kubeconfig"
const kubeconfig = "kubeconfig"

const (
	ResourceKindHost    = "Host"
	ResourceKindCluster = "Cluster"
)

const DefaultUser = "kubeadmin"
const ConsoleUrlPrefix = "https://console-openshift-console.apps"

var (
	DefaultClusterNetworkCidr       = "10.128.0.0/14"
	DefaultClusterNetworkHostPrefix = int64(23)
	DefaultServiceNetworkCidr       = "172.30.0.0/16"
)

type Config struct {
	ImageBuilder        string            `envconfig:"IMAGE_BUILDER" default:"quay.io/ocpmetal/installer-image-build:latest"`
	ImageBuilderCmd     string            `envconfig:"IMAGE_BUILDER_CMD" default:"echo hello"`
	AgentDockerImg      string            `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/ocpmetal/agent:latest"`
	KubeconfigGenerator string            `envconfig:"KUBECONFIG_GENERATE_IMAGE" default:"quay.io/ocpmetal/ignition-manifests-and-kubeconfig-generate:latest"` // TODO: update the latest once the repository has git workflow
	InventoryURL        string            `envconfig:"INVENTORY_URL" default:"10.35.59.36"`
	InventoryPort       string            `envconfig:"INVENTORY_PORT" default:"30485"`
	S3EndpointURL       string            `envconfig:"S3_ENDPOINT_URL" default:"http://10.35.59.36:30925"`
	S3Bucket            string            `envconfig:"S3_BUCKET" default:"test"`
	AwsAccessKeyID      string            `envconfig:"AWS_ACCESS_KEY_ID" default:"accessKey1"`
	AwsSecretAccessKey  string            `envconfig:"AWS_SECRET_ACCESS_KEY" default:"verySecretKey1"`
	Namespace           string            `envconfig:"NAMESPACE" default:"assisted-installer"`
	UseK8s              bool              `envconfig:"USE_K8S" default:"true"` // TODO remove when jobs running deprecated
	BaseDNSDomains      map[string]string `envconfig:"BASE_DNS_DOMAINS" default:""`
	JobCPULimit         string            `envconfig:"JOB_CPU_LIMIT" default:"500m"`
	JobMemoryLimit      string            `envconfig:"JOB_MEMORY_LIMIT" default:"1000Mi"`
	JobCPURequests      string            `envconfig:"JOB_CPU_REQUESTS" default:"300m"`
	JobMemoryRequests   string            `envconfig:"JOB_MEMORY_REQUESTS" default:"400Mi"`
}

const ignitionConfigFormat = `{
"ignition": { "version": "3.0.0" },
  "passwd": {
    "users": [
      {{.userSshKey}}
    ]
  },
"systemd": {
"units": [{
"name": "agent.service",
"enabled": true,
"contents": "[Service]\nType=simple\nRestart=always\nRestartSec=3\nStartLimitIntervalSec=0\nEnvironment=HTTPS_PROXY={{.ProxyURL}}\nEnvironment=HTTP_PROXY={{.ProxyURL}}\nEnvironment=http_proxy={{.ProxyURL}}\nEnvironment=https_proxy={{.ProxyURL}}\nExecStartPre=podman run --privileged --rm -v /usr/local/bin:/hostbin {{.AgentDockerImg}} cp /usr/bin/agent /hostbin\nExecStart=/usr/local/bin/agent --host {{.InventoryURL}} --port {{.InventoryPort}} --cluster-id {{.clusterId}} --agent-version {{.AgentDockerImg}}\n\n[Install]\nWantedBy=multi-user.target"
}]
},
"storage": {
    "files": [{
      "path": "/etc/assisted-installer.ps",
      "mode": 420,
      "contents": { "source": "{{.PULL_SECRET}}" }
    }]
  }
}`

var clusterFileNames = []string{
	"kubeconfig",
	"bootstrap.ign",
	"master.ign",
	"worker.ign",
	"metadata.json",
	"kubeadmin-password",
	"kubeconfig-noingress",
}

type debugCmd struct {
	cmd    string
	stepID string
}

type bareMetalInventory struct {
	Config
	imageBuildCmd []string
	db            *gorm.DB
	debugCmdMap   map[strfmt.UUID]debugCmd
	debugCmdMux   sync.Mutex
	log           logrus.FieldLogger
	job           job.API
	hostApi       host.API
	clusterApi    cluster.API
	eventsHandler events.Handler
	s3Client      awsS3CLient.S3Client
	metricApi     metrics.API
}

var _ restapi.InstallerAPI = &bareMetalInventory{}

func NewBareMetalInventory(
	db *gorm.DB,
	log logrus.FieldLogger,
	hostApi host.API,
	clusterApi cluster.API,
	cfg Config,
	jobApi job.API,
	eventsHandler events.Handler,
	s3Client awsS3CLient.S3Client,
	metricApi metrics.API,
) *bareMetalInventory {

	b := &bareMetalInventory{
		db:            db,
		log:           log,
		Config:        cfg,
		debugCmdMap:   make(map[strfmt.UUID]debugCmd),
		hostApi:       hostApi,
		clusterApi:    clusterApi,
		job:           jobApi,
		eventsHandler: eventsHandler,
		s3Client:      s3Client,
		metricApi:     metricApi,
	}
	if cfg.ImageBuilderCmd != "" {
		b.imageBuildCmd = strings.Split(cfg.ImageBuilderCmd, " ")
	}

	if b.Config.UseK8s {
		//Run first ISO dummy for image pull, this is done so that the image will be pulled and the api will take less time.
		b.generateDummyISOImage()
	}
	return b
}

func (b *bareMetalInventory) generateDummyISOImage() {
	var (
		dummyId   = "00000000-0000-0000-0000-000000000000"
		jobName   = fmt.Sprintf("dummyimage-%s-%s", dummyId, time.Now().Format("20060102150405"))
		imgName   = fmt.Sprintf("discovery-image-%s", dummyId)
		requestID = requestid.NewID()
		log       = requestid.RequestIDLogger(b.log, requestID)
	)
	if err := b.job.Create(requestid.ToContext(context.Background(), requestID),
		b.createImageJob(jobName, imgName, "Dummy")); err != nil {
		log.WithError(err).Errorf("failed to generate dummy ISO image")
	}
}

func getQuantity(s string) resource.Quantity {
	reply, _ := resource.ParseQuantity(s)
	return reply
}

// create discovery image generation job, return job name and error
func (b *bareMetalInventory) createImageJob(jobName, imgName, ignitionConfig string) *batch.Job {
	return &batch.Job{
		TypeMeta: meta.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: meta.ObjectMeta{
			Name:      jobName,
			Namespace: b.Namespace,
		},
		Spec: batch.JobSpec{
			BackoffLimit: swag.Int32(2),
			Template: core.PodTemplateSpec{
				ObjectMeta: meta.ObjectMeta{
					Name:      jobName,
					Namespace: b.Namespace,
				},
				Spec: core.PodSpec{
					Containers: []core.Container{
						{
							Resources: core.ResourceRequirements{
								Limits: core.ResourceList{
									"cpu":    getQuantity(b.JobCPULimit),
									"memory": getQuantity(b.JobMemoryLimit),
								},
								Requests: core.ResourceList{
									"cpu":    getQuantity(b.JobCPURequests),
									"memory": getQuantity(b.JobMemoryRequests),
								},
							},
							Name:            "image-creator",
							Image:           b.Config.ImageBuilder,
							Command:         b.imageBuildCmd,
							ImagePullPolicy: "IfNotPresent",
							Env: []core.EnvVar{
								{
									Name:  "S3_ENDPOINT_URL",
									Value: b.S3EndpointURL,
								},
								{
									Name:  "IGNITION_CONFIG",
									Value: ignitionConfig,
								},
								{
									Name:  "IMAGE_NAME",
									Value: imgName,
								},
								{
									Name:  "S3_BUCKET",
									Value: b.S3Bucket,
								},
								{
									Name:  "aws_access_key_id",
									Value: b.AwsAccessKeyID,
								},
								{
									Name:  "aws_secret_access_key",
									Value: b.AwsSecretAccessKey,
								},
							},
						},
					},
					RestartPolicy: "Never",
				},
			},
		},
	}
}

func (b *bareMetalInventory) formatIgnitionFile(cluster *common.Cluster, params installer.GenerateClusterISOParams) (string, error) {
	var ignitionParams = map[string]string{
		"userSshKey":     b.getUserSshKey(params),
		"AgentDockerImg": b.AgentDockerImg,
		"InventoryURL":   b.InventoryURL,
		"InventoryPort":  b.InventoryPort,
		"clusterId":      cluster.ID.String(),
		"ProxyURL":       params.ImageCreateParams.ProxyURL,
		"PULL_SECRET":    dataurl.EncodeBytes([]byte(cluster.PullSecret)),
	}
	tmpl, err := template.New("ignitionConfig").Parse(ignitionConfigFormat)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, ignitionParams); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (b *bareMetalInventory) getUserSshKey(params installer.GenerateClusterISOParams) string {
	sshKey := params.ImageCreateParams.SSHPublicKey
	if sshKey == "" {
		return ""
	}
	return fmt.Sprintf(`{
		"name": "core",
		"passwordHash": "$6$MWO4bibU8TIWG0XV$Hiuj40lWW7pHiwJmXA8MehuBhdxSswLgvGxEh8ByEzeX2D1dk87JILVUYS4JQOP45bxHRegAB9Fs/SWfszXa5.",
		"sshAuthorizedKeys": [
		"%s"],
		"groups": [ "sudo" ]}`, sshKey)
}

func (b *bareMetalInventory) RegisterCluster(ctx context.Context, params installer.RegisterClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	id := strfmt.UUID(uuid.New().String())
	url := installer.GetClusterURL{ClusterID: id}
	log.Infof("Register cluster: %s with id %s", swag.StringValue(params.NewClusterParams.Name), id)

	if params.NewClusterParams.ClusterNetworkCidr == nil {
		params.NewClusterParams.ClusterNetworkCidr = &DefaultClusterNetworkCidr
	}
	if params.NewClusterParams.ClusterNetworkHostPrefix == 0 {
		params.NewClusterParams.ClusterNetworkHostPrefix = DefaultClusterNetworkHostPrefix
	}
	if params.NewClusterParams.ServiceNetworkCidr == nil {
		params.NewClusterParams.ServiceNetworkCidr = &DefaultServiceNetworkCidr
	}

	cluster := common.Cluster{Cluster: models.Cluster{
		ID:                       &id,
		Href:                     swag.String(url.String()),
		Kind:                     swag.String(ResourceKindCluster),
		BaseDNSDomain:            params.NewClusterParams.BaseDNSDomain,
		ClusterNetworkCidr:       swag.StringValue(params.NewClusterParams.ClusterNetworkCidr),
		ClusterNetworkHostPrefix: params.NewClusterParams.ClusterNetworkHostPrefix,
		IngressVip:               params.NewClusterParams.IngressVip,
		Name:                     swag.StringValue(params.NewClusterParams.Name),
		OpenshiftVersion:         swag.StringValue(params.NewClusterParams.OpenshiftVersion),
		ServiceNetworkCidr:       swag.StringValue(params.NewClusterParams.ServiceNetworkCidr),
		SSHPublicKey:             params.NewClusterParams.SSHPublicKey,
		UpdatedAt:                strfmt.DateTime{},
	}}
	if params.NewClusterParams.PullSecret != "" {
		err := validations.ValidatePullSecret(params.NewClusterParams.PullSecret)
		if err != nil {
			log.WithError(err).Errorf("Pull-secret for new cluster has invalid format")
			return installer.NewRegisterClusterBadRequest().
				WithPayload(common.GenerateError(http.StatusBadRequest, errors.New("Pull-secret has invalid format")))
		}
		setPullSecret(&cluster, params.NewClusterParams.PullSecret)
	}
	if err := validations.ValidateClusterNameFormat(swag.StringValue(params.NewClusterParams.Name)); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}

	err := b.clusterApi.RegisterCluster(ctx, &cluster)
	if err != nil {
		log.Errorf("failed to register cluster %s ", swag.StringValue(params.NewClusterParams.Name))
		return installer.NewRegisterClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	b.metricApi.ClusterRegistered(swag.StringValue(params.NewClusterParams.OpenshiftVersion))
	return installer.NewRegisterClusterCreated().WithPayload(&cluster.Cluster)
}

func (b *bareMetalInventory) DeregisterCluster(ctx context.Context, params installer.DeregisterClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	log.Infof("Deregister cluster id %s", params.ClusterID)

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return installer.NewDeregisterClusterNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	if err := b.deleteDNSRecordSets(ctx, cluster); err != nil {
		log.Warnf("failed to delete DNS record sets for base domain: %s", cluster.BaseDNSDomain)
	}

	err := b.clusterApi.DeregisterCluster(ctx, &cluster)
	if err != nil {
		log.WithError(err).Errorf("failed to deregister cluster cluster %s", params.ClusterID)
		return installer.NewDeregisterClusterNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	return installer.NewDeregisterClusterNoContent()
}

func (b *bareMetalInventory) DownloadClusterISO(ctx context.Context, params installer.DownloadClusterISOParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	if err := b.db.First(&common.Cluster{}, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster %s", params.ClusterID)
		return installer.NewDownloadClusterISONotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}
	imgName := getImageName(params.ClusterID)
	imageURL := fmt.Sprintf("%s/%s/%s", b.S3EndpointURL, b.S3Bucket, imgName)

	log.Info("Image URL: ", imageURL)
	resp, err := http.Get(imageURL)
	if err != nil {
		log.WithError(err).Errorf("Failed to get ISO: %s", imgName)
		return installer.NewDownloadClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		b, _ := ioutil.ReadAll(resp.Body)
		log.WithError(fmt.Errorf("%d - %s", resp.StatusCode, string(b))).
			Errorf("Failed to get ISO: %s", imgName)
		if resp.StatusCode == http.StatusNotFound {
			return installer.NewDownloadClusterISONotFound().
				WithPayload(common.GenerateError(http.StatusNotFound, errors.New("The image was not found "+
					"(perhaps it expired) - please generate a new one and try again")))
		}
		return installer.NewDownloadClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, errors.New(string(b))))
	}
	b.eventsHandler.AddEvent(ctx, params.ClusterID.String(), models.EventSeverityInfo, "Started image download", time.Now())

	return filemiddleware.NewResponder(installer.NewDownloadClusterISOOK().WithPayload(resp.Body),
		fmt.Sprintf("cluster-%s-discovery.iso", params.ClusterID.String()),
		resp.ContentLength)
}

func (b *bareMetalInventory) GenerateClusterISO(ctx context.Context, params installer.GenerateClusterISOParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("prepare image for cluster %s", params.ClusterID)
	var cluster common.Cluster

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("generate cluster ISO failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("generate cluster ISO failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return installer.NewInstallClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction")))
	}

	if err := tx.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		return installer.NewGenerateClusterISONotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	/* We need to ensure that the metadata in the DB matches the image that will be uploaded to S3,
	so we check that at least 10 seconds have past since the previous request to reduce the chance
	of a race between two consecutive requests.
	*/
	now := time.Now()
	previousCreatedAt := time.Time(cluster.ImageInfo.CreatedAt)
	if previousCreatedAt.Add(10 * time.Second).After(now) {
		log.Error("request came too soon after previous request")
		return installer.NewGenerateClusterISOConflict().WithPayload(common.GenerateError(http.StatusConflict,
			errors.New("Another request to generate an image has been recently submitted. Please wait a few seconds and try again.")))
	}

	/* If the request has the same parameters as the previous request and the image is still in S3,
	just refresh the timestamp.
	*/
	var imageExists bool
	if cluster.ImageInfo.ProxyURL == params.ImageCreateParams.ProxyURL &&
		cluster.ImageInfo.SSHPublicKey == params.ImageCreateParams.SSHPublicKey &&
		cluster.ImageInfo.GeneratorVersion == b.Config.ImageBuilder {
		var err error
		imgName := getImageName(params.ClusterID)
		imageExists, err = b.s3Client.UpdateObjectTag(ctx, imgName, b.S3Bucket, "create_sec_since_epoch", strconv.FormatInt(now.Unix(), 10))
		if err != nil {
			log.WithError(tx.Error).Errorf("failed to contact storage backend")
			return installer.NewInstallClusterInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, errors.New("failed to contact storage backend")))
		}
	}

	updates := map[string]interface{}{}
	updates["image_proxy_url"] = params.ImageCreateParams.ProxyURL
	updates["image_ssh_public_key"] = params.ImageCreateParams.SSHPublicKey
	updates["image_created_at"] = strfmt.DateTime(now)
	updates["image_generator_version"] = b.Config.ImageBuilder
	dbReply := tx.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(updates)
	if dbReply.Error != nil {
		log.WithError(dbReply.Error).Errorf("failed to update cluster: %s", params.ClusterID)
		return installer.NewGenerateClusterISOInternalServerError()
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewGenerateClusterISOInternalServerError()
	}
	txSuccess = true
	if err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster %s after update", params.ClusterID)
		return installer.NewUpdateClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if imageExists {
		log.Infof("Re-used existing cluster <%s> image", params.ClusterID)
		b.eventsHandler.AddEvent(ctx, cluster.ID.String(), models.EventSeverityInfo, "Re-used existing image rather than generating a new one", time.Now())
		return installer.NewGenerateClusterISOCreated().WithPayload(&cluster.Cluster)
	}

	// Kill the previous job in case it's still running
	prevJobName := fmt.Sprintf("createimage-%s-%s", cluster.ID, previousCreatedAt.Format("20060102150405"))
	log.Info("Attempting to delete job %s", prevJobName)
	if err := b.job.Delete(ctx, prevJobName, b.Namespace); err != nil {
		log.WithError(err).Errorf("failed to kill previous job in cluster %s", cluster.ID)
		return installer.NewGenerateClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	log.Info("Finished attempting to delete job %s", prevJobName)

	ignitionConfig, formatErr := b.formatIgnitionFile(&cluster, params)
	if formatErr != nil {
		log.WithError(formatErr).Errorf("failed to format ignition config file for cluster %s", cluster.ID)
		return installer.NewGenerateClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, formatErr))
	}

	// This job name is exactly 63 characters which is the maximum for a job - be careful if modifying
	jobName := fmt.Sprintf("createimage-%s-%s", cluster.ID, now.Format("20060102150405"))
	imgName := getImageName(params.ClusterID)
	log.Infof("Creating job %s", jobName)
	if err := b.job.Create(ctx, b.createImageJob(jobName, imgName, ignitionConfig)); err != nil {
		log.WithError(err).Error("failed to create image job")
		return installer.NewGenerateClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if err := b.job.Monitor(ctx, jobName, b.Namespace); err != nil {
		log.WithError(err).Error("image creation failed")
		return installer.NewGenerateClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	log.Infof("Generated cluster <%s> image with ignition config %s", params.ClusterID, ignitionConfig)
	msg := fmt.Sprintf("Generated image (proxy URL is \"%s\", ", params.ImageCreateParams.ProxyURL)
	if params.ImageCreateParams.SSHPublicKey != "" {
		msg += "SSH public key is set)"
	} else {
		msg += "SSH public key is not set)"
	}
	b.eventsHandler.AddEvent(ctx, cluster.ID.String(), models.EventSeverityInfo, msg, time.Now())
	return installer.NewGenerateClusterISOCreated().WithPayload(&cluster.Cluster)
}

func getImageName(clusterID strfmt.UUID) string {
	return fmt.Sprintf("discovery-image-%s", clusterID.String())
}

type clusterInstaller struct {
	ctx    context.Context
	b      *bareMetalInventory
	log    logrus.FieldLogger
	params installer.InstallClusterParams
}

func (b *bareMetalInventory) verifyClusterNetworkConfig(ctx context.Context, cluster *common.Cluster) error {
	cidr, err := network.CalculateMachineNetworkCIDR(cluster.APIVip, cluster.IngressVip, cluster.Hosts)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}
	if cidr != cluster.MachineNetworkCidr {
		return common.NewApiError(http.StatusBadRequest,
			fmt.Errorf("Cluster machine CIDR %s is different than the calculated CIDR %s", cluster.MachineNetworkCidr, cidr))
	}
	if err = network.VerifyVips(cluster.Hosts, cluster.MachineNetworkCidr, cluster.APIVip, cluster.IngressVip,
		true, b.log); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}
	machineCidrHosts, err := network.GetMachineCIDRHosts(b.log, cluster)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}
	masterNodesIds, err := b.clusterApi.GetMasterNodesIds(ctx, cluster, b.db)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	hostIDInCidrHosts := func(id strfmt.UUID, hosts []*models.Host) bool {
		for _, h := range hosts {
			if *h.ID == id {
				return true
			}
		}
		return false
	}

	for _, id := range masterNodesIds {
		if !hostIDInCidrHosts(*id, machineCidrHosts) {
			return common.NewApiError(http.StatusBadRequest,
				fmt.Errorf("Master id %s does not have an interface with IP belonging to machine CIDR %s",
					*id, cluster.MachineNetworkCidr))
		}
	}
	return nil
}

func (c *clusterInstaller) installHosts(cluster *common.Cluster, tx *gorm.DB) error {
	success := true
	err := errors.Errorf("Failed to install cluster <%s>", cluster.ID.String())
	for i := range cluster.Hosts {
		if installErr := c.b.hostApi.Install(c.ctx, cluster.Hosts[i], tx); installErr != nil {
			success = false
			// collect multiple errors
			err = errors.Wrap(installErr, err.Error())
		}
	}
	if !success {
		return common.NewApiError(http.StatusConflict, err)
	}
	return nil
}

func (b *bareMetalInventory) validateHostsInventory(cluster *common.Cluster) error {
	for _, chost := range cluster.Hosts {
		sufficient, err := b.hostApi.ValidateCurrentInventory(chost, cluster)
		if err != nil {
			msg := fmt.Sprintf("failed to validate host <%s> in cluster: %s", chost.ID.String(), cluster.ID.String())
			b.log.WithError(err).Warn(msg)
			return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, msg))
		}
		if !sufficient.IsSufficient {
			msg := fmt.Sprintf("host <%s>  failed to pass hardware validation in cluster: %s. Reason %s",
				chost.ID.String(), cluster.ID.String(), sufficient.Reason)
			b.log.Warn(msg)
			return common.NewApiError(http.StatusConflict, errors.Errorf(msg))
		}
	}
	return nil
}

func (c clusterInstaller) install(tx *gorm.DB) error {
	var cluster common.Cluster
	var err error
	if err = tx.Preload("Hosts").First(&cluster, "id = ?", c.params.ClusterID).Error; err != nil {
		return errors.Wrapf(err, "failed to find cluster %s", c.params.ClusterID)
	}

	if err = c.b.createDNSRecordSets(c.ctx, cluster); err != nil {
		return errors.Wrapf(err, "failed to create DNS record sets for base domain: %s", cluster.BaseDNSDomain)
	}

	if err = c.b.clusterApi.Install(c.ctx, &cluster, tx); err != nil {
		return errors.Wrapf(err, "failed to install cluster %s", cluster.ID.String())
	}

	// set one of the master nodes as bootstrap
	if err = c.b.setBootstrapHost(c.ctx, cluster, tx); err != nil {
		return err
	}

	// move hosts states to installing
	if err = c.installHosts(&cluster, tx); err != nil {
		return err
	}

	return nil
}

func (b *bareMetalInventory) validateAllHostsCanBeInstalled(cluster *common.Cluster) error {
	notInstallableHosts := make([]string, 0, len(cluster.Hosts))
	for _, h := range cluster.Hosts {
		if !b.hostApi.IsInstallable(h) {
			notInstallableHosts = append(notInstallableHosts, h.ID.String())
		}
	}

	if len(notInstallableHosts) > 0 {
		return common.NewApiError(http.StatusConflict,
			errors.Errorf("Not all hosts are ready for installation: %s", notInstallableHosts))
	}
	return nil
}

func (b *bareMetalInventory) InstallCluster(ctx context.Context, params installer.InstallClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	var err error

	if err = b.db.Preload("Hosts", "status <> ?", host.HostStatusDisabled).First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return common.NewApiError(http.StatusNotFound, err)
	}

	if err = b.validateHostsInventory(&cluster); err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err = b.verifyClusterNetworkConfig(ctx, &cluster); err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err = b.validateAllHostsCanBeInstalled(&cluster); err != nil {
		return common.GenerateErrorResponder(err)
	}

	// prepare cluster and hosts for installation
	err = b.db.Transaction(func(tx *gorm.DB) error {
		if err = b.clusterApi.PrepareForInstallation(ctx, &cluster, tx); err != nil {
			return err
		}

		for i := range cluster.Hosts {
			if err = b.hostApi.PrepareForInstallation(ctx, cluster.Hosts[i], tx); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err = b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return common.GenerateErrorResponder(err)
	}

	go func() {
		var err error
		asyncCtx := requestid.ToContext(context.Background(), requestid.FromContext(ctx))

		defer func() {
			if err != nil {
				log.WithError(err).Warn("Cluster install")
				b.clusterApi.HandlePreInstallError(asyncCtx, &cluster, err)
			}
		}()

		if err = b.generateClusterInstallConfig(asyncCtx, cluster); err != nil {
			return
		}

		cInstaller := clusterInstaller{
			ctx:    asyncCtx, // Need a new context for async part
			b:      b,
			log:    log,
			params: params,
		}
		err = b.db.Transaction(cInstaller.install)
		if err == nil {
			//send metric when the installation process has been started
			b.metricApi.InstallationStarted(cluster.OpenshiftVersion)
		}
	}()

	log.Infof("Successfully prepared cluster <%s> for installation", params.ClusterID.String())
	return installer.NewInstallClusterAccepted().WithPayload(&cluster.Cluster)
}

func (b *bareMetalInventory) setBootstrapHost(ctx context.Context, cluster common.Cluster, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)

	// check if cluster already has bootstrap
	for _, h := range cluster.Hosts {
		if h.Bootstrap {
			log.Infof("Bootstrap ID is %s", h.ID)
			return nil
		}
	}

	masterNodesIds, err := b.clusterApi.GetMasterNodesIds(ctx, &cluster, db)
	if err != nil {
		log.WithError(err).Errorf("failed to get cluster %s master node id's", cluster.ID)
		return errors.Wrapf(err, "Failed to get cluster %s master node id's", cluster.ID)
	}
	if len(masterNodesIds) == 0 {
		return errors.Errorf("Cluster have no master hosts that can operate as bootstrap")
	}
	bootstrapId := masterNodesIds[len(masterNodesIds)-1]
	log.Infof("Bootstrap ID is %s", bootstrapId)
	for i := range cluster.Hosts {
		if cluster.Hosts[i].ID.String() == bootstrapId.String() {
			err = b.hostApi.SetBootstrap(ctx, cluster.Hosts[i], true, db)
			if err != nil {
				log.WithError(err).Errorf("failed to update bootstrap host for cluster %s", cluster.ID)
				return errors.Wrapf(err, "Failed to update bootstrap host for cluster %s", cluster.ID)
			}
		}
	}
	return nil
}

func (b *bareMetalInventory) generateClusterInstallConfig(ctx context.Context, cluster common.Cluster) error {
	log := logutil.FromContext(ctx, b.log)

	cfg, err := installcfg.GetInstallConfig(log, &cluster)
	if err != nil {
		log.WithError(err).Errorf("failed to get install config for cluster %s", cluster.ID)
		return errors.Wrapf(err, "failed to get install config for cluster %s", cluster.ID)
	}
	jobName := fmt.Sprintf("%s-%s-%s", kubeconfigPrefix, cluster.ID.String(), uuid.New().String())[:63]
	if err := b.job.Create(ctx, b.createKubeconfigJob(&cluster, jobName, cfg)); err != nil {
		log.WithError(err).Errorf("Failed to create kubeconfig generation job %s for cluster %s", jobName, cluster.ID)
		return errors.Wrapf(err, "Failed to create kubeconfig generation job %s for cluster %s", jobName, cluster.ID)
	}

	if err := b.job.Monitor(ctx, jobName, b.Namespace); err != nil {
		log.WithError(err).Errorf("Generating kubeconfig files %s failed for cluster %s", jobName, cluster.ID)
		return errors.Wrapf(err, "Generating kubeconfig files %s failed for cluster %s", jobName, cluster.ID)
	}

	return b.clusterApi.SetGeneratorVersion(&cluster, b.Config.KubeconfigGenerator, b.db)
}

func (b *bareMetalInventory) refreshClusterHosts(ctx context.Context, cluster *common.Cluster, tx *gorm.DB, log logrus.FieldLogger) error {
	for _, h := range cluster.Hosts {
		var host models.Host
		var err error
		if err = tx.Take(&host, "id = ? and cluster_id = ?",
			h.ID.String(), cluster.ID.String()).Error; err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				h.ID.String(), cluster.ID.String())
			return common.NewApiError(http.StatusNotFound, err)
		}
		if _, err = b.hostApi.RefreshStatus(ctx, &host, tx); err != nil {
			log.WithError(err).Errorf("failed to refresh state of host %s cluster %s", *h.ID, cluster.ID.String())
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}
	return nil
}

func (b *bareMetalInventory) UpdateCluster(ctx context.Context, params installer.UpdateClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	var err error
	log.Info("update cluster ", params.ClusterID)

	if swag.StringValue(params.ClusterUpdateParams.PullSecret) != "" {
		err = validations.ValidatePullSecret(*params.ClusterUpdateParams.PullSecret)
		if err != nil {
			log.WithError(err).Errorf("Pull-secret for cluster %s, has invalid format", params.ClusterID)
			return installer.NewUpdateClusterBadRequest().
				WithPayload(common.GenerateError(http.StatusBadRequest, errors.New("Pull-secret has invalid format")))
		}
	}
	if newClusterName := swag.StringValue(params.ClusterUpdateParams.Name); newClusterName != "" {
		if err = validations.ValidateClusterNameFormat(newClusterName); err != nil {
			return common.NewApiError(http.StatusBadRequest, err)
		}
	}

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("update cluster failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("update cluster failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return installer.NewUpdateClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction")))
	}

	// in case host monitor already updated the state we need to use FOR UPDATE option
	transaction.AddForUpdateQueryOption(tx)

	if err = tx.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		return installer.NewUpdateClusterNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	if err = b.clusterApi.VerifyClusterUpdatability(&cluster); err != nil {
		log.WithError(err).Errorf("cluster %s can't be updated in current state", params.ClusterID)
		return installer.NewUpdateClusterConflict().WithPayload(common.GenerateError(http.StatusConflict, err))
	}

	if updateClusterConflict := b.validateDNSDomain(params, log); updateClusterConflict != nil {
		return updateClusterConflict
	}

	err = b.updateClusterData(ctx, &cluster, params, tx, log)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	err = b.updateHostsData(ctx, params, tx, log)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	err = b.updateHostsAndClusterStatus(ctx, &cluster, tx, log)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return common.GenerateErrorResponder(common.NewApiError(http.StatusInternalServerError, fmt.Errorf("DB error, failed to commit")))
	}
	txSuccess = true

	if err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster %s after update", params.ClusterID)
		return common.GenerateErrorResponder(common.NewApiError(http.StatusInternalServerError, err))
	}

	cluster.HostNetworks = calculateHostNetworks(log, &cluster)
	for _, host := range cluster.Hosts {
		if err := b.customizeHost(host); err != nil {
			return common.GenerateErrorResponder(common.NewApiError(http.StatusInternalServerError, err))
		}
	}

	return installer.NewUpdateClusterCreated().WithPayload(&cluster.Cluster)
}

func (b *bareMetalInventory) updateClusterData(ctx context.Context, cluster *common.Cluster, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {
	updates := map[string]interface{}{}
	apiVip := cluster.APIVip
	ingressVip := cluster.IngressVip
	if params.ClusterUpdateParams.Name != nil {
		updates["name"] = *params.ClusterUpdateParams.Name
	}
	if params.ClusterUpdateParams.APIVip != nil {
		updates["api_vip"] = *params.ClusterUpdateParams.APIVip
		apiVip = *params.ClusterUpdateParams.APIVip
	}
	if params.ClusterUpdateParams.BaseDNSDomain != nil {
		updates["base_dns_domain"] = *params.ClusterUpdateParams.BaseDNSDomain
	}
	if params.ClusterUpdateParams.ClusterNetworkCidr != nil {
		updates["cluster_network_cidr"] = *params.ClusterUpdateParams.ClusterNetworkCidr
	}
	if params.ClusterUpdateParams.ClusterNetworkHostPrefix != nil {
		updates["cluster_network_host_prefix"] = *params.ClusterUpdateParams.ClusterNetworkHostPrefix
	}
	if params.ClusterUpdateParams.ServiceNetworkCidr != nil {
		updates["service_network_cidr"] = *params.ClusterUpdateParams.ServiceNetworkCidr
	}
	if params.ClusterUpdateParams.IngressVip != nil {
		updates["ingress_vip"] = *params.ClusterUpdateParams.IngressVip
		ingressVip = *params.ClusterUpdateParams.IngressVip
	}
	if params.ClusterUpdateParams.SSHPublicKey != nil {
		updates["ssh_public_key"] = *params.ClusterUpdateParams.SSHPublicKey
	}

	var machineCidr string

	machineCidr, err := network.CalculateMachineNetworkCIDR(apiVip, ingressVip, cluster.Hosts)
	if err != nil {
		log.WithError(err).Errorf("failed to calculate machine network cidr for cluster: %s", params.ClusterID)
		return common.NewApiError(http.StatusBadRequest, err)
	}
	updates["machine_network_cidr"] = machineCidr

	err = network.VerifyVips(cluster.Hosts, machineCidr, apiVip, ingressVip, false, log)
	if err != nil {
		log.WithError(err).Errorf("VIP verification failed for cluster: %s", params.ClusterID)
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if params.ClusterUpdateParams.PullSecret != nil {
		cluster.PullSecret = *params.ClusterUpdateParams.PullSecret
		updates["pull_secret"] = *params.ClusterUpdateParams.PullSecret
		if cluster.PullSecret != "" {
			updates["pull_secret_set"] = true
		} else {
			updates["pull_secret_set"] = false
		}
	}

	dbReply := db.Model(&common.Cluster{}).Where("id = ?", cluster.ID.String()).Updates(updates)
	if dbReply.Error != nil {
		log.WithError(dbReply.Error).Errorf("failed to update cluster: %s", params.ClusterID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	return nil
}

func (b *bareMetalInventory) updateHostsData(ctx context.Context, params installer.UpdateClusterParams, db *gorm.DB, log logrus.FieldLogger) error {
	for i := range params.ClusterUpdateParams.HostsRoles {
		log.Infof("Update host %s to role: %s", params.ClusterUpdateParams.HostsRoles[i].ID,
			params.ClusterUpdateParams.HostsRoles[i].Role)
		var host models.Host
		err := db.First(&host, "id = ? and cluster_id = ?",
			params.ClusterUpdateParams.HostsRoles[i].ID, params.ClusterID).Error
		if err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsRoles[i].ID, params.ClusterID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		err = b.hostApi.UpdateRole(ctx, &host, models.HostRole(params.ClusterUpdateParams.HostsRoles[i].Role), db)
		if err != nil {
			log.WithError(err).Errorf("failed to set role <%s> host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsRoles[i].Role, params.ClusterUpdateParams.HostsRoles[i].ID,
				params.ClusterID)
			return common.NewApiError(http.StatusInternalServerError, err)
		}
	}

	for i := range params.ClusterUpdateParams.HostsNames {
		log.Infof("Update host %s to request hostname %s", params.ClusterUpdateParams.HostsNames[i].ID,
			params.ClusterUpdateParams.HostsNames[i].Hostname)
		var host models.Host
		err := db.First(&host, "id = ? and cluster_id = ?",
			params.ClusterUpdateParams.HostsNames[i].ID, params.ClusterID).Error
		if err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsRoles[i].ID, params.ClusterID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		err = b.hostApi.UpdateHostname(ctx, &host, params.ClusterUpdateParams.HostsNames[i].Hostname, db)
		if err != nil {
			log.WithError(err).Errorf("failed to set hostname <%s> host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsNames[i].Hostname, params.ClusterUpdateParams.HostsNames[i].ID,
				params.ClusterID)
			return common.NewApiError(http.StatusConflict, err)
		}
	}

	return nil
}

func (b *bareMetalInventory) updateHostsAndClusterStatus(ctx context.Context, cluster *common.Cluster, db *gorm.DB, log logrus.FieldLogger) error {
	err := b.refreshClusterHosts(ctx, cluster, db, log)
	if err != nil {
		return err
	}

	if _, err = b.clusterApi.RefreshStatus(ctx, cluster, db); err != nil {
		log.WithError(err).Errorf("failed to validate or update cluster %s state", cluster.ID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	return nil
}

func calculateHostNetworks(log logrus.FieldLogger, cluster *common.Cluster) []*models.HostNetwork {
	cidrHostsMap := make(map[string][]strfmt.UUID)
	for _, h := range cluster.Hosts {
		if h.Inventory == "" {
			continue
		}
		var inventory models.Inventory
		err := json.Unmarshal([]byte(h.Inventory), &inventory)
		if err != nil {
			log.WithError(err).Warnf("Could not parse inventory of host %s", *h.ID)
			continue
		}
		for _, intf := range inventory.Interfaces {
			for _, ipv4Address := range intf.IPV4Addresses {
				_, ipnet, err := net.ParseCIDR(ipv4Address)
				if err != nil {
					log.WithError(err).Warnf("Could not parse CIDR %s", ipv4Address)
					continue
				}
				cidr := ipnet.String()
				cidrHostsMap[cidr] = append(cidrHostsMap[cidr], *h.ID)
			}
		}
	}
	ret := make([]*models.HostNetwork, 0)
	for k, v := range cidrHostsMap {
		ret = append(ret, &models.HostNetwork{
			Cidr:    k,
			HostIds: v,
		})
	}
	return ret
}

func (b *bareMetalInventory) ListClusters(ctx context.Context, params installer.ListClustersParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var clusters []*common.Cluster
	if err := b.db.Preload("Hosts").Find(&clusters).Error; err != nil {
		log.WithError(err).Error("failed to list clusters")
		return installer.NewListClustersInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	var mClusters []*models.Cluster = make([]*models.Cluster, len(clusters))
	for i, c := range clusters {
		mClusters[i] = &c.Cluster
	}

	return installer.NewListClustersOK().WithPayload(mClusters)
}

func (b *bareMetalInventory) GetCluster(ctx context.Context, params installer.GetClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	if err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		// TODO: check for the right error
		return installer.NewGetClusterNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	cluster.HostNetworks = calculateHostNetworks(log, &cluster)
	for _, host := range cluster.Hosts {
		if err := b.customizeHost(host); err != nil {
			return common.GenerateErrorResponder(common.NewApiError(http.StatusInternalServerError, err))
		}
	}

	return installer.NewGetClusterOK().WithPayload(&cluster.Cluster)
}

func (b *bareMetalInventory) RegisterHost(ctx context.Context, params installer.RegisterHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var host models.Host
	var cluster common.Cluster
	log.Infof("Register host: %+v", params)

	if err := b.db.First(&cluster, "id = ?", params.ClusterID.String()).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID.String())
		return installer.NewRegisterHostBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}
	err := b.db.First(&host, "id = ? and cluster_id = ?", *params.NewHostParams.HostID, params.ClusterID).Error
	if err != nil && !gorm.IsRecordNotFoundError(err) {
		log.WithError(err).Errorf("failed to get host %s in cluster: %s",
			*params.NewHostParams.HostID, params.ClusterID.String())
		return installer.NewRegisterHostInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	// In case host doesn't exists check if the cluster accept new hosts registration
	if err != nil && gorm.IsRecordNotFoundError(err) {

		if err := b.clusterApi.AcceptRegistration(&cluster); err != nil {
			log.WithError(err).Errorf("failed to register host <%s> to cluster %s due to: %s",
				params.NewHostParams.HostID, params.ClusterID.String(), err.Error())
			return installer.NewRegisterHostForbidden().
				WithPayload(common.GenerateError(http.StatusForbidden, err))
		}
	}

	url := installer.GetHostURL{ClusterID: params.ClusterID, HostID: *params.NewHostParams.HostID}
	host = models.Host{
		ID:                    params.NewHostParams.HostID,
		Href:                  swag.String(url.String()),
		Kind:                  swag.String(ResourceKindHost),
		ClusterID:             params.ClusterID,
		CheckedInAt:           strfmt.DateTime(time.Now()),
		DiscoveryAgentVersion: params.NewHostParams.DiscoveryAgentVersion,
	}

	if err := b.hostApi.RegisterHost(ctx, &host); err != nil {
		log.WithError(err).Errorf("failed to register host <%s> cluster <%s>",
			params.NewHostParams.HostID.String(), params.ClusterID.String())
		return installer.NewRegisterHostBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	if err := b.customizeHost(&host); err != nil {
		return common.GenerateErrorResponder(common.NewApiError(http.StatusInternalServerError, err))
	}

	return installer.NewRegisterHostCreated().WithPayload(&host)
}

func (b *bareMetalInventory) DeregisterHost(ctx context.Context, params installer.DeregisterHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Deregister host: %s cluster %s", params.HostID, params.ClusterID)

	if err := b.db.Where("id = ? and cluster_id = ?", params.HostID, params.ClusterID).
		Delete(&models.Host{}).Error; err != nil {
		// TODO: check error type
		return installer.NewDeregisterHostBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	// TODO: need to check that host can be deleted from the cluster
	return installer.NewDeregisterHostNoContent()
}

func (b *bareMetalInventory) GetHost(ctx context.Context, params installer.GetHostParams) middleware.Responder {
	var host models.Host
	// TODO: validate what is the error
	if err := b.db.Where("id = ? and cluster_id = ?", params.HostID, params.ClusterID).
		First(&host).Error; err != nil {
		return installer.NewGetHostNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	if err := b.customizeHost(&host); err != nil {
		return common.GenerateErrorResponder(common.NewApiError(http.StatusInternalServerError, err))
	}

	return installer.NewGetHostOK().WithPayload(&host)
}

func (b *bareMetalInventory) ListHosts(ctx context.Context, params installer.ListHostsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var hosts []*models.Host
	if err := b.db.Find(&hosts, "cluster_id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get list of hosts for cluster %s", params.ClusterID)
		return installer.NewListHostsInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	for _, host := range hosts {
		if err := b.customizeHost(host); err != nil {
			return common.GenerateErrorResponder(common.NewApiError(http.StatusInternalServerError, err))
		}
	}

	return installer.NewListHostsOK().WithPayload(hosts)
}

func createStepID(stepType models.StepType) string {
	return fmt.Sprintf("%s-%s", stepType, uuid.New().String()[:8])
}

func (b *bareMetalInventory) GetNextSteps(ctx context.Context, params installer.GetNextStepsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var steps models.Steps
	var host models.Host

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("get next steps failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("get next steps failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return installer.NewUpdateClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction")))
	}

	//TODO check the error type
	if err := tx.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find host: %s", params.HostID)
		return installer.NewGetNextStepsNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	host.CheckedInAt = strfmt.DateTime(time.Now())
	if err := tx.Model(&host).Update("checked_in_at", host.CheckedInAt).Error; err != nil {
		log.WithError(err).Errorf("failed to update host: %s", params.ClusterID)
		return installer.NewGetNextStepsInternalServerError()
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewGetNextStepsInternalServerError()
	}
	txSuccess = true

	var err error
	steps, err = b.hostApi.GetNextSteps(ctx, &host)
	if err != nil {
		log.WithError(err).Errorf("failed to get steps for host %s cluster %s", params.HostID, params.ClusterID)
	}

	b.debugCmdMux.Lock()
	if cmd, ok := b.debugCmdMap[params.HostID]; ok {
		step := &models.Step{}
		step.StepType = models.StepTypeExecute
		step.StepID = cmd.stepID
		step.Command = "bash"
		step.Args = []string{"-c", cmd.cmd}
		steps.Instructions = append(steps.Instructions, step)
		delete(b.debugCmdMap, params.HostID)
	}
	b.debugCmdMux.Unlock()

	return installer.NewGetNextStepsOK().WithPayload(&steps)
}

func (b *bareMetalInventory) PostStepReply(ctx context.Context, params installer.PostStepReplyParams) middleware.Responder {
	var err error
	log := logutil.FromContext(ctx, b.log)
	msg := fmt.Sprintf("Received step reply <%s> from cluster <%s> host <%s>  exit-code <%d> stdout <%s> stderr <%s>", params.Reply.StepID, params.ClusterID,
		params.HostID, params.Reply.ExitCode, params.Reply.Output, params.Reply.Error)

	var host models.Host
	if err = b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("Failed to find host <%s> cluster <%s> step <%s> exit code %d stdout <%s> stderr <%s>",
			params.HostID, params.ClusterID, params.Reply.StepID, params.Reply.ExitCode, params.Reply.Output, params.Reply.Error)
		return installer.NewPostStepReplyNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	//check the output exit code
	if params.Reply.ExitCode != 0 {
		err = fmt.Errorf(msg)
		log.WithError(err).Errorf("Exit code is <%d> ", params.Reply.ExitCode)
		handlingError := handleReplyError(params, b, ctx, &host)
		if handlingError != nil {
			log.WithError(handlingError).Errorf("Failed handling reply error for host <%s> cluster <%s>", params.HostID, params.ClusterID)
		}
		return installer.NewPostStepReplyBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	log.Infof(msg)

	var stepReply string
	stepReply, err = filterReplyByType(params)
	if err != nil {
		log.WithError(err).Errorf("Failed decode <%s> reply for host <%s> cluster <%s>",
			params.Reply.StepID, params.HostID, params.ClusterID)
		return installer.NewPostStepReplyBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	err = handleReplyByType(params, b, ctx, host, stepReply)
	if err != nil {
		log.WithError(err).Errorf("Failed to update step reply for host <%s> cluster <%s> step <%s>",
			params.HostID, params.ClusterID, params.Reply.StepID)
		return installer.NewPostStepReplyInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	return installer.NewPostStepReplyNoContent()
}

func handleReplyError(params installer.PostStepReplyParams, b *bareMetalInventory, ctx context.Context, h *models.Host) error {

	if params.Reply.StepType == models.StepTypeInstall {
		//if it's install step - need to move host to error
		return b.hostApi.HandleInstallationFailure(ctx, h)
	}
	return nil
}

func (b *bareMetalInventory) updateFreeAddressesReport(ctx context.Context, host *models.Host, freeAddressesReport string) error {
	var (
		err           error
		freeAddresses models.FreeNetworksAddresses
	)
	log := logutil.FromContext(ctx, b.log)
	if err = json.Unmarshal([]byte(freeAddressesReport), &freeAddresses); err != nil {
		log.WithError(err).Warnf("Json unmarshal free addresses of host %s", host.ID.String())
		return err
	}
	if len(freeAddresses) == 0 {
		err = fmt.Errorf("Free addresses for host %s is empty", host.ID.String())
		log.WithError(err).Warn("Update free addresses")
		return err
	}
	result := b.db.Model(&models.Host{}).Where("id = ? and cluster_id = ?", host.ID.String(),
		host.ClusterID.String()).Updates(map[string]interface{}{"free_addresses": freeAddressesReport})
	err = result.Error
	if err != nil {
		log.WithError(err).Warnf("Update free addresses of host %s", host.ID.String())
		return err
	}
	if result.RowsAffected != 1 {
		err = fmt.Errorf("Update free_addresses of host %s: %d affected rows", host.ID.String(), result.RowsAffected)
		log.WithError(err).Warn("Update free addresses")
		return err
	}
	return nil
}

func handleReplyByType(params installer.PostStepReplyParams, b *bareMetalInventory, ctx context.Context, host models.Host, stepReply string) error {
	var err error
	switch params.Reply.StepType {
	case models.StepTypeHardwareInfo:
		err = b.hostApi.UpdateHwInfo(ctx, &host, stepReply)
	case models.StepTypeInventory:
		err = b.hostApi.UpdateInventory(ctx, &host, stepReply)
	case models.StepTypeConnectivityCheck:
		err = b.hostApi.UpdateConnectivityReport(ctx, &host, stepReply)
	case models.StepTypeFreeNetworkAddresses:
		err = b.updateFreeAddressesReport(ctx, &host, stepReply)
	}
	return err
}

func filterReplyByType(params installer.PostStepReplyParams) (string, error) {
	var stepReply string
	var err error

	// To make sure we store only information defined in swagger we unmarshal and marshal the stepReplyParams.
	switch params.Reply.StepType {
	case models.StepTypeHardwareInfo:
		stepReply, err = filterReply(&models.Introspection{}, params.Reply.Output)
	case models.StepTypeInventory:
		stepReply, err = filterReply(&models.Inventory{}, params.Reply.Output)
	case models.StepTypeConnectivityCheck:
		stepReply, err = filterReply(&models.ConnectivityReport{}, params.Reply.Output)
	case models.StepTypeFreeNetworkAddresses:
		stepReply, err = filterReply(&models.FreeNetworksAddresses{}, params.Reply.Output)
	}
	return stepReply, err
}

// filterReply return only the expected parameters from the input.
func filterReply(expected interface{}, input string) (string, error) {
	if err := json.Unmarshal([]byte(input), expected); err != nil {
		return "", err
	}
	reply, err := json.Marshal(expected)
	if err != nil {
		return "", err
	}
	return string(reply), nil
}

func (b *bareMetalInventory) SetDebugStep(ctx context.Context, params installer.SetDebugStepParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	stepID := createStepID(models.StepTypeExecute)
	b.debugCmdMux.Lock()
	b.debugCmdMap[params.HostID] = debugCmd{
		cmd:    swag.StringValue(params.Step.Command),
		stepID: stepID,
	}
	b.debugCmdMux.Unlock()
	log.Infof("Added new debug command <%s> for cluster <%s> host <%s>: <%s>",
		stepID, params.ClusterID, params.HostID, swag.StringValue(params.Step.Command))
	return installer.NewSetDebugStepNoContent()
}

func (b *bareMetalInventory) DisableHost(ctx context.Context, params installer.DisableHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var host models.Host
	log.Info("disabling host: ", params.HostID)

	if err := b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			log.WithError(err).Errorf("host %s not found", params.HostID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if err := b.hostApi.DisableHost(ctx, &host); err != nil {
		log.WithError(err).Errorf("failed to disable host <%s> from cluster <%s>", params.HostID, params.ClusterID)
		return common.GenerateErrorResponderWithDefault(err, http.StatusConflict)
	}

	if err := b.customizeHost(&host); err != nil {
		return common.GenerateErrorResponder(common.NewApiError(http.StatusInternalServerError, err))
	}

	return installer.NewDisableHostOK().WithPayload(&host)
}

func (b *bareMetalInventory) EnableHost(ctx context.Context, params installer.EnableHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var host models.Host
	log.Info("enable host: ", params.HostID)

	if err := b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		if gorm.IsRecordNotFoundError(err) {
			log.WithError(err).Errorf("host %s not found", params.HostID)
			return common.NewApiError(http.StatusNotFound, err)
		}
		log.WithError(err).Errorf("failed to get host %s", params.HostID)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if err := b.hostApi.EnableHost(ctx, &host); err != nil {
		log.WithError(err).Errorf("failed to enable host <%s> from cluster <%s>", params.HostID, params.ClusterID)
		return common.GenerateErrorResponderWithDefault(err, http.StatusConflict)
	}

	if err := b.customizeHost(&host); err != nil {
		return common.GenerateErrorResponder(common.NewApiError(http.StatusInternalServerError, err))
	}

	return installer.NewEnableHostOK().WithPayload(&host)
}

func (b *bareMetalInventory) createKubeconfigJob(cluster *common.Cluster, jobName string, cfg []byte) *batch.Job {
	id := cluster.ID
	// [TODO] need to find more generic way to set the openshift release image
	//OCP 4.5.2
	overrideImageName := "quay.io/openshift-release-dev/ocp-release@sha256:8f923b7b8efdeac619eb0e7697106c1d17dd3d262c49d8742b38600417cf7d1d"
	// [TODO]  make sure that we use openshift-installer from the release image, otherwise the KubeconfigGenerator image must be updated here per opnshift version
	kubeConfigGeneratorImage := b.Config.KubeconfigGenerator
	return &batch.Job{
		TypeMeta: meta.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: meta.ObjectMeta{
			Name:      jobName,
			Namespace: b.Namespace,
		},
		Spec: batch.JobSpec{
			BackoffLimit: swag.Int32(2),
			Template: core.PodTemplateSpec{
				ObjectMeta: meta.ObjectMeta{
					Name:      jobName,
					Namespace: b.Namespace,
				},
				Spec: core.PodSpec{
					Containers: []core.Container{
						{
							Name:            kubeconfigPrefix,
							Image:           kubeConfigGeneratorImage,
							Command:         b.imageBuildCmd,
							ImagePullPolicy: "IfNotPresent",
							Env: []core.EnvVar{
								{
									Name:  "S3_ENDPOINT_URL",
									Value: b.S3EndpointURL,
								},
								{
									Name:  "INSTALLER_CONFIG",
									Value: string(cfg),
								},
								{
									Name:  "IMAGE_NAME",
									Value: jobName,
								},
								{
									Name:  "S3_BUCKET",
									Value: b.S3Bucket,
								},
								{
									Name:  "CLUSTER_ID",
									Value: id.String(),
								},
								{
									Name:  "OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE",
									Value: overrideImageName, //TODO: change this to match the cluster openshift version
								},
								{
									Name:  "aws_access_key_id",
									Value: b.AwsAccessKeyID,
								},
								{
									Name:  "aws_secret_access_key",
									Value: b.AwsSecretAccessKey,
								},
							},
							Resources: core.ResourceRequirements{
								Limits: core.ResourceList{
									"cpu":    getQuantity(b.JobCPULimit),
									"memory": getQuantity(b.JobMemoryLimit),
								},
								Requests: core.ResourceList{
									"cpu":    getQuantity(b.JobCPURequests),
									"memory": getQuantity(b.JobMemoryRequests),
								},
							},
						},
					},
					RestartPolicy: "Never",
				},
			},
		},
	}
}

func (b *bareMetalInventory) DownloadClusterFiles(ctx context.Context, params installer.DownloadClusterFilesParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	log.Infof("Download cluster files: %s for cluster %s", params.FileName, params.ClusterID)

	if !funk.Contains(clusterFileNames, params.FileName) {
		err := fmt.Errorf("invalid cluster file %s", params.FileName)
		log.WithError(err).Errorf("failed download file: %s from cluster: %s", params.FileName, params.ClusterID)
		return common.NewApiError(http.StatusBadRequest, err)
	}

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return installer.NewDownloadClusterFilesNotFound().
				WithPayload(common.GenerateError(http.StatusNotFound, err))
		} else {
			return installer.NewDownloadClusterFilesInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}
	if err := b.clusterApi.DownloadFiles(&cluster); err != nil {
		log.WithError(err).Errorf("failed to download cluster files %s", params.ClusterID)
		return installer.NewDownloadClusterFilesConflict().
			WithPayload(common.GenerateError(http.StatusConflict, err))
	}

	respBody, contentLength, err := b.s3Client.DownloadFileFromS3(ctx, fmt.Sprintf("%s/%s", params.ClusterID, params.FileName), b.S3Bucket)
	if err != nil {
		return installer.NewDownloadClusterFilesInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(installer.NewDownloadClusterFilesOK().WithPayload(respBody), params.FileName, contentLength)
}

func (b *bareMetalInventory) DownloadClusterKubeconfig(ctx context.Context, params installer.DownloadClusterKubeconfigParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster
	log.Infof("Download cluster kubeconfig for cluster %s", params.ClusterID)

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return installer.NewDownloadClusterKubeconfigNotFound().
				WithPayload(common.GenerateError(http.StatusNotFound, err))
		} else {
			return installer.NewDownloadClusterKubeconfigInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}
	if err := b.clusterApi.DownloadKubeconfig(&cluster); err != nil {
		return installer.NewDownloadClusterKubeconfigConflict().
			WithPayload(common.GenerateError(http.StatusConflict, err))
	}

	respBody, contentLength, err := b.s3Client.DownloadFileFromS3(ctx, fmt.Sprintf("%s/%s", params.ClusterID, kubeconfig), b.S3Bucket)

	if err != nil {
		return installer.NewDownloadClusterKubeconfigConflict().
			WithPayload(common.GenerateError(http.StatusConflict, errors.Wrap(err, "failed to download kubeconfig")))
	}
	return filemiddleware.NewResponder(installer.NewDownloadClusterKubeconfigOK().WithPayload(respBody), kubeconfig, contentLength)
}

func (b *bareMetalInventory) GetCredentials(ctx context.Context, params installer.GetCredentialsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster common.Cluster

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return installer.NewGetCredentialsNotFound().
				WithPayload(common.GenerateError(http.StatusNotFound, err))
		} else {
			return installer.NewGetCredentialsInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}
	if err := b.clusterApi.GetCredentials(&cluster); err != nil {
		return installer.NewGetCredentialsConflict().
			WithPayload(common.GenerateError(http.StatusConflict, err))
	}
	fileName := "kubeadmin-password"
	filesURL := fmt.Sprintf("%s/%s/%s", b.S3EndpointURL, b.S3Bucket,
		fmt.Sprintf("%s/%s", params.ClusterID, fileName))
	log.Info("File URL: ", filesURL)
	resp, err := http.Get(filesURL)
	if err != nil {
		log.WithError(err).Errorf("Failed to get clusters %s %s file", params.ClusterID, fileName)
		return installer.NewGetCredentialsInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	defer resp.Body.Close()
	password, err := ioutil.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || err != nil {
		log.WithError(fmt.Errorf("%s", password)).
			Errorf("Failed to get clusters %s %s", params.ClusterID, fileName)
		return installer.NewGetCredentialsConflict().
			WithPayload(common.GenerateError(http.StatusConflict, errors.New(string(password))))
	}
	return installer.NewGetCredentialsOK().WithPayload(
		&models.Credentials{Username: DefaultUser,
			Password:   string(password),
			ConsoleURL: fmt.Sprintf("%s.%s.%s", ConsoleUrlPrefix, cluster.Name, cluster.BaseDNSDomain)})
}

func (b *bareMetalInventory) UpdateHostInstallProgress(ctx context.Context, params installer.UpdateHostInstallProgressParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var host models.Host
	if err := b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find host %s", params.HostID)
		// host have nothing to do with the error so we just log it
		return installer.NewUpdateHostInstallProgressOK()
	}
	if err := b.hostApi.UpdateInstallProgress(ctx, &host, params.HostProgress); err != nil {
		log.WithError(err).Errorf("failed to update host %s progress", params.HostID)
		// host have nothing to do with the error so we just log it
		return installer.NewUpdateHostInstallProgressOK()
	}

	event := fmt.Sprintf("reached installation stage %s", params.HostProgress.CurrentStage)

	if params.HostProgress.ProgressInfo != "" {
		event += fmt.Sprintf(": %s", params.HostProgress.ProgressInfo)
	}

	log.Info(fmt.Sprintf("Host %s in cluster %s: %s", host.ID, host.ClusterID, event))
	msg := fmt.Sprintf("Host %s: %s", b.hostApi.GetHostname(&host), event)

	b.eventsHandler.AddEvent(ctx, host.ID.String(), models.EventSeverityInfo, msg, time.Now(), host.ClusterID.String())
	return installer.NewUpdateHostInstallProgressOK()
}

func (b *bareMetalInventory) UploadClusterIngressCert(ctx context.Context, params installer.UploadClusterIngressCertParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("UploadClusterIngressCert for cluster %s with params %s", params.ClusterID, params.IngressCertParams)
	var cluster common.Cluster

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return installer.NewUploadClusterIngressCertNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		} else {
			return installer.NewUploadClusterIngressCertInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	if err := b.clusterApi.UploadIngressCert(&cluster); err != nil {
		return installer.NewUploadClusterIngressCertBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	fileName := fmt.Sprintf("%s/%s", cluster.ID, kubeconfig)
	exists, err := b.s3Client.DoesObjectExist(ctx, fileName, b.S3Bucket)
	if err != nil {
		log.WithError(err).Errorf("Failed to upload ingress ca")
		return installer.NewUploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if exists {
		log.Infof("Ingress ca for cluster %s already exists", cluster.ID)
		return installer.NewUploadClusterIngressCertCreated()
	}

	noigress := fmt.Sprintf("%s/%s-noingress", cluster.ID, kubeconfig)
	resp, _, err := b.s3Client.DownloadFileFromS3(ctx, noigress, b.S3Bucket)
	if err != nil {
		return installer.NewUploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	kubeconfigData, err := ioutil.ReadAll(resp)
	if err != nil {
		log.WithError(err).Infof("Failed to convert kubeconfig s3 response to io reader")
		return installer.NewUploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	mergedKubeConfig, err := mergeIngressCaIntoKubeconfig(kubeconfigData, []byte(params.IngressCertParams), log)
	if err != nil {
		return installer.NewUploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if err := b.s3Client.PushDataToS3(ctx, mergedKubeConfig, fileName, b.S3Bucket); err != nil {
		return installer.NewUploadClusterIngressCertInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, fmt.Errorf("failed to upload %s to s3", fileName)))
	}
	return installer.NewUploadClusterIngressCertCreated()
}

// Merging given ingress ca certificate into kubeconfig
// Code was taken from openshift installer
func mergeIngressCaIntoKubeconfig(kubeconfigData []byte, ingressCa []byte, log logrus.FieldLogger) ([]byte, error) {

	kconfig, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		log.WithError(err).Errorf("Failed to convert kubeconfig data")
		return nil, err
	}
	if kconfig == nil || len(kconfig.Clusters) == 0 {
		err = errors.Errorf("kubeconfig is missing expected data")
		log.Error(err)
		return nil, err
	}

	for _, c := range kconfig.Clusters {
		clusterCABytes := c.CertificateAuthorityData
		if len(clusterCABytes) == 0 {
			err = errors.Errorf("kubeconfig CertificateAuthorityData not found")
			log.Errorf("%e, data %s", err, c.CertificateAuthorityData)
			return nil, err
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(clusterCABytes) {
			err = errors.Errorf("cluster CA found in kubeconfig not valid PEM format")
			log.Errorf("%e, ca :%s", err, clusterCABytes)
			return nil, err
		}
		if !certPool.AppendCertsFromPEM(ingressCa) {
			err = errors.Errorf("given ingress-ca is not valid PEM format")
			log.Errorf("%e %s", err, ingressCa)
			return nil, err
		}

		newCA := append(ingressCa, clusterCABytes...)
		c.CertificateAuthorityData = newCA
	}

	kconfigAsByteArray, err := clientcmd.Write(*kconfig)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert kubeconfig")
	}
	return kconfigAsByteArray, nil
}

func setPullSecret(cluster *common.Cluster, pullSecret string) {
	cluster.PullSecret = pullSecret
	if pullSecret != "" {
		cluster.PullSecretSet = true
	} else {
		cluster.PullSecretSet = false
	}
}

func (b *bareMetalInventory) CancelInstallation(ctx context.Context, params installer.CancelInstallationParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("canceling installation for cluster %s", params.ClusterID)

	var c common.Cluster

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("cancel installation failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("cancel installation failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return installer.NewCancelInstallationInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction")))
	}

	if err := tx.Preload("Hosts").First(&c, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return installer.NewCancelInstallationNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		}
		return installer.NewCancelInstallationInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	// cancellation is made by setting the cluster and and hosts states to error.
	if err := b.clusterApi.CancelInstallation(ctx, &c, "installation was canceled by user", tx); err != nil {
		return common.GenerateErrorResponder(err)
	}
	for _, h := range c.Hosts {
		if err := b.hostApi.CancelInstallation(ctx, h, "installation was canceled by user", tx); err != nil {
			return common.GenerateErrorResponder(err)
		}
		if err := b.customizeHost(h); err != nil {
			return installer.NewCancelInstallationInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewCancelInstallationInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction")))
	}
	txSuccess = true

	return installer.NewCancelInstallationAccepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) ResetCluster(ctx context.Context, params installer.ResetClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("resetting cluster %s", params.ClusterID)

	var c common.Cluster

	txSuccess := false
	tx := b.db.Begin()
	defer func() {
		if !txSuccess {
			log.Error("reset cluster failed")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			log.Error("reset cluster failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		log.WithError(tx.Error).Errorf("failed to start db transaction")
		return installer.NewResetClusterInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to start transaction")))
	}

	if err := tx.Preload("Hosts").First(&c, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return installer.NewResetClusterNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		}
		return installer.NewResetClusterInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if err := b.clusterApi.ResetCluster(ctx, &c, "cluster was reset by user", tx); err != nil {
		return common.GenerateErrorResponder(err)
	}
	for _, h := range c.Hosts {
		if err := b.hostApi.ResetHost(ctx, h, "cluster was reset by user", tx); err != nil {
			return common.GenerateErrorResponder(err)
		}
		if err := b.customizeHost(h); err != nil {
			return installer.NewResetClusterInternalServerError().WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	if err := b.deleteS3ClusterFiles(ctx, &c); err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	if err := b.deleteDNSRecordSets(ctx, c); err != nil {
		log.Warnf("failed to delete DNS record sets for base domain: %s", c.BaseDNSDomain)
	}

	if err := tx.Commit().Error; err != nil {
		log.Error(err)
		return installer.NewResetClusterInternalServerError().WithPayload(
			common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to commit transaction")))
	}
	txSuccess = true

	return installer.NewResetClusterAccepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) CompleteInstallation(ctx context.Context, params installer.CompleteInstallationParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	log.Infof("complete cluster %s installation", params.ClusterID)

	var c common.Cluster
	if err := b.db.Preload("Hosts").First(&c, "id = ?", params.ClusterID).Error; err != nil {
		return common.GenerateErrorResponder(err)
	}

	if err := b.clusterApi.CompleteInstallation(ctx, &c, *params.CompletionParams.IsSuccess, params.CompletionParams.ErrorInfo); err != nil {
		log.WithError(err).Errorf("Failed to set complete cluster state on %s ", params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}

	return installer.NewCompleteInstallationAccepted().WithPayload(&c.Cluster)
}

func (b *bareMetalInventory) deleteS3ClusterFiles(ctx context.Context, c *common.Cluster) error {
	for _, name := range clusterFileNames {
		if err := b.s3Client.DeleteFileFromS3(ctx, fmt.Sprintf("%s/%s", c.ID, name), b.S3Bucket); err != nil {
			return err
		}
	}
	return nil
}

func (b *bareMetalInventory) createDNSRecordSets(ctx context.Context, cluster common.Cluster) error {
	return b.changeDNSRecordSets(ctx, cluster, false)
}

func (b *bareMetalInventory) deleteDNSRecordSets(ctx context.Context, cluster common.Cluster) error {
	return b.changeDNSRecordSets(ctx, cluster, true)
}

func (b *bareMetalInventory) changeDNSRecordSets(ctx context.Context, cluster common.Cluster, delete bool) error {
	log := logutil.FromContext(ctx, b.log)

	domain, err := b.getDNSDomain(cluster.Name, cluster.BaseDNSDomain)
	if err != nil {
		return err
	}
	if domain == nil {
		// No supported base DNS domain specified
		return nil
	}

	switch domain.Provider {
	case "route53":
		var dnsProvider dnsproviders.Provider = dnsproviders.Route53{
			RecordSet: dnsproviders.RecordSet{
				RecordSetType: "A",
				TTL:           60,
			},
			HostedZoneID: domain.ID,
			SharedCreds:  true,
		}

		dnsRecordSetFunc := dnsProvider.CreateRecordSet
		if delete {
			dnsRecordSetFunc = dnsProvider.DeleteRecordSet
		}

		// Create/Delete A record for API Virtual IP
		_, err := dnsRecordSetFunc(domain.APIDomainName, cluster.APIVip)
		if err != nil {
			log.WithError(err).Errorf("failed to update DNS record: (%s, %s)",
				domain.APIDomainName, cluster.APIVip)
			return err
		}
		// Create/Delete A record for Ingress Virtual IP
		_, err = dnsRecordSetFunc(domain.IngressDomainName, cluster.IngressVip)
		if err != nil {
			log.WithError(err).Errorf("failed to update DNS record: (%s, %s)",
				domain.IngressDomainName, cluster.IngressVip)
			return err
		}
		log.Infof("Successfully created DNS records for base domain: %s", cluster.BaseDNSDomain)
	}
	return nil
}

type dnsDomain struct {
	Name              string
	ID                string
	Provider          string
	APIDomainName     string
	IngressDomainName string
}

func (b *bareMetalInventory) getDNSDomain(clusterName, baseDNSDomainName string) (*dnsDomain, error) {
	var dnsDomainID string
	var dnsProvider string

	// Parse base domains from config
	if val, ok := b.Config.BaseDNSDomains[baseDNSDomainName]; ok {
		re := regexp.MustCompile("/")
		if !re.MatchString(val) {
			return nil, errors.New(fmt.Sprintf("Invalid DNS domain: %s", val))
		}
		s := re.Split(val, 2)
		dnsDomainID = s[0]
		dnsProvider = s[1]
	} else {
		// No base domains defined in config
		return nil, nil
	}

	if dnsDomainID == "" || dnsProvider == "" {
		// Specified domain is not defined in config
		return nil, nil
	}

	return &dnsDomain{
		Name:              baseDNSDomainName,
		ID:                dnsDomainID,
		Provider:          dnsProvider,
		APIDomainName:     fmt.Sprintf("%s.%s.%s", "api", clusterName, baseDNSDomainName),
		IngressDomainName: fmt.Sprintf("*.%s.%s.%s", "apps", clusterName, baseDNSDomainName),
	}, nil
}

func (b *bareMetalInventory) validateDNSDomain(params installer.UpdateClusterParams, log logrus.FieldLogger) *installer.UpdateClusterConflict {
	clusterName := swag.StringValue(params.ClusterUpdateParams.Name)
	clusterBaseDomain := swag.StringValue(params.ClusterUpdateParams.BaseDNSDomain)
	dnsDomain, err := b.getDNSDomain(clusterName, clusterBaseDomain)
	if err == nil && dnsDomain != nil {
		// Cluster's baseDNSDomain is defined in config (BaseDNSDomains map)
		if err = b.validateBaseDNS(dnsDomain); err != nil {
			log.WithError(err).Errorf("Invalid base DNS domain: %s", clusterBaseDomain)
			return installer.NewUpdateClusterConflict().
				WithPayload(common.GenerateError(http.StatusConflict,
					errors.New("Base DNS domain isn't configured properly")))
		}
		if err = b.validateDNSRecords(dnsDomain); err != nil {
			log.WithError(err).Errorf("DNS records already exist for cluster: %s", params.ClusterID)
			return installer.NewUpdateClusterConflict().
				WithPayload(common.GenerateError(http.StatusConflict,
					errors.New("DNS records already exist for cluster - please change 'Cluster Name'")))
		}
	}
	return nil
}

func (b *bareMetalInventory) validateBaseDNS(domain *dnsDomain) error {
	return validations.ValidateBaseDNS(domain.Name, domain.ID, domain.Provider)
}

func (b *bareMetalInventory) validateDNSRecords(domain *dnsDomain) error {
	vipAddresses := []string{domain.APIDomainName, domain.IngressDomainName}
	return validations.CheckDNSRecordsExistence(vipAddresses, domain.ID, domain.Provider)
}

func ipAsUint(ipStr string, log logrus.FieldLogger) uint64 {
	parts := strings.Split(ipStr, ".")
	if len(parts) != 4 {
		log.Warnf("Invalid ip %s", ipStr)
		return 0
	}
	var result uint64 = 0
	for _, p := range parts {
		result = result << 8
		converted, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			log.WithError(err).Warnf("Conversion of %s to uint", p)
			return 0
		}
		result += converted
	}
	return result
}

func applyLimit(ret models.FreeAddressesList, limitParam *int64) models.FreeAddressesList {
	if limitParam != nil && *limitParam >= 0 && *limitParam < int64(len(ret)) {
		return ret[:*limitParam]
	}
	return ret
}

func (b *bareMetalInventory) getFreeAddresses(params installer.GetFreeAddressesParams, log logrus.FieldLogger) (models.FreeAddressesList, error) {
	var hosts []*models.Host
	err := b.db.Select("free_addresses").Find(&hosts, "cluster_id = ? and status in (?)", params.ClusterID.String(), []string{host.HostStatusInsufficient, host.HostStatusKnown}).Error
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "Error retreiving hosts for cluster %s", params.ClusterID.String()))
	}
	if len(hosts) == 0 {
		return nil, common.NewApiError(http.StatusNotFound, errors.Errorf("No hosts where found for cluster %s", params.ClusterID))
	}
	resultingSet := network.MakeFreeAddressesSet(hosts, params.Network, params.Prefix, log)

	ret := models.FreeAddressesList{}
	for a := range resultingSet {
		ret = append(ret, a)
	}

	// Sort addresses
	sort.Slice(ret, func(i, j int) bool {
		return ipAsUint(ret[i].String(), log) < ipAsUint(ret[j].String(), log)
	})

	ret = applyLimit(ret, params.Limit)

	return ret, nil
}

func (b *bareMetalInventory) GetFreeAddresses(ctx context.Context, params installer.GetFreeAddressesParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)

	results, err := b.getFreeAddresses(params, log)
	if err != nil {
		log.WithError(err).Warn("GetFreeAddresses")
		return common.GenerateErrorResponder(err)
	}
	return installer.NewGetFreeAddressesOK().WithPayload(results)
}

func (b *bareMetalInventory) customizeHost(host *models.Host) error {
	b.customizeHostStages(host)
	b.customizeHostname(host)
	return nil
}

func (b *bareMetalInventory) customizeHostStages(host *models.Host) {
	host.ProgressStages = b.hostApi.GetStagesByRole(host.Role, host.Bootstrap)
}

func (b *bareMetalInventory) customizeHostname(host *models.Host) {
	host.RequestedHostname = b.hostApi.GetHostname(host)
}
