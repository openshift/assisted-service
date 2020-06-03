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
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/filanov/bm-inventory/restapi"

	"github.com/filanov/bm-inventory/internal/common"

	"github.com/filanov/bm-inventory/internal/events"

	awsS3CLient "github.com/filanov/bm-inventory/pkg/s3Client"

	"github.com/filanov/bm-inventory/internal/cluster"
	"github.com/filanov/bm-inventory/internal/host"
	"github.com/filanov/bm-inventory/internal/installcfg"
	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/pkg/filemiddleware"
	"github.com/filanov/bm-inventory/pkg/job"
	logutil "github.com/filanov/bm-inventory/pkg/log"
	"github.com/filanov/bm-inventory/restapi/operations/installer"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

const kubeconfigPrefix = "generate-kubeconfig"
const kubeconfig = "kubeconfig"

const (
	ClusterStatusReady      = "ready"
	ClusterStatusInstalling = "installing"
	ClusterStatusInstalled  = "installed"
	ClusterStatusError      = "error"
)

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
	ImageBuilder        string `envconfig:"IMAGE_BUILDER" default:"quay.io/ocpmetal/installer-image-build:stable"`
	ImageBuilderCmd     string `envconfig:"IMAGE_BUILDER_CMD" default:"echo hello"`
	AgentDockerImg      string `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/oamizur/agent:latest"`
	KubeconfigGenerator string `envconfig:"KUBECONFIG_GENERATE_IMAGE" default:"quay.io/ocpmetal/ignition-manifests-and-kubeconfig-generate:stable"`
	InventoryURL        string `envconfig:"INVENTORY_URL" default:"10.35.59.36"`
	InventoryPort       string `envconfig:"INVENTORY_PORT" default:"30485"`
	S3EndpointURL       string `envconfig:"S3_ENDPOINT_URL" default:"http://10.35.59.36:30925"`
	S3Bucket            string `envconfig:"S3_BUCKET" default:"test"`
	AwsAccessKeyID      string `envconfig:"AWS_ACCESS_KEY_ID" default:"accessKey1"`
	AwsSecretAccessKey  string `envconfig:"AWS_SECRET_ACCESS_KEY" default:"verySecretKey1"`
	Namespace           string `envconfig:"NAMESPACE" default:"assisted-installer"`
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
"contents": "[Service]\nType=simple\nRestart=always\nEnvironment=HTTPS_PROXY={{.ProxyURL}}\nEnvironment=HTTP_PROXY={{.ProxyURL}}\nEnvironment=http_proxy={{.ProxyURL}}\nEnvironment=https_proxy={{.ProxyURL}}\nExecStartPre=docker run --privileged --rm -v /usr/local/bin:/hostbin {{.AgentDockerImg}} cp /usr/bin/agent /hostbin\nExecStart=/usr/local/bin/agent --host {{.InventoryURL}} --port {{.InventoryPort}} --cluster-id {{.clusterId}}\n\n[Install]\nWantedBy=multi-user.target"
}]
}
}`

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
	}
	if cfg.ImageBuilderCmd != "" {
		b.imageBuildCmd = strings.Split(cfg.ImageBuilderCmd, " ")
	}
	//	Run first ISO dummy for image pull, this is done so that the image will be pulled and the api will take less time.
	generateDummyISOImage(jobApi, b, log)
	return b
}

func generateDummyISOImage(jobApi job.API, b *bareMetalInventory, log logrus.FieldLogger) {
	dummyId := "00000000-0000-0000-0000-000000000000"
	jobName := fmt.Sprintf("dummyimage-%s-%s", dummyId, time.Now().Format("20060102150405"))
	imgName := fmt.Sprintf("discovery-image-%s", dummyId)
	if err := jobApi.Create(context.Background(), b.createImageJob(jobName, imgName, "Dummy")); err != nil {
		log.WithError(err).Errorf("failed to generate dummy ISO image")
	}
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

func (b *bareMetalInventory) formatIgnitionFile(cluster *models.Cluster, params installer.GenerateClusterISOParams) (string, error) {
	var ignitionParams = map[string]string{
		"userSshKey":     b.getUserSshKey(params),
		"AgentDockerImg": b.AgentDockerImg,
		"InventoryURL":   b.InventoryURL,
		"InventoryPort":  b.InventoryPort,
		"clusterId":      cluster.ID.String(),
		"ProxyURL":       params.ImageCreateParams.ProxyURL,
	}
	tmpl, err := template.New("ignitionConfig").Parse(ignitionConfigFormat)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	if err := tmpl.Execute(buf, ignitionParams); err != nil {
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

	cluster := models.Cluster{
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
	}
	setPullSecret(&cluster, params.NewClusterParams.PullSecret)

	err := b.clusterApi.RegisterCluster(ctx, &cluster)
	if err != nil {
		log.Errorf("failed to register cluster %s ", swag.StringValue(params.NewClusterParams.Name))
		return installer.NewRegisterClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	return installer.NewRegisterClusterCreated().WithPayload(&cluster)
}

func (b *bareMetalInventory) DeregisterCluster(ctx context.Context, params installer.DeregisterClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster models.Cluster

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return installer.NewDeregisterClusterNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
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
	if err := b.db.First(&models.Cluster{}, "id = ?", params.ClusterID).Error; err != nil {
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
				WithPayload(common.GenerateError(http.StatusNotFound, errors.New(string(b))))
		}
		return installer.NewDownloadClusterISOInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, errors.New(string(b))))
	}

	return filemiddleware.NewResponder(installer.NewDownloadClusterISOOK().WithPayload(resp.Body),
		fmt.Sprintf("cluster-%s-discovery.iso", params.ClusterID.String()))
}

func (b *bareMetalInventory) GenerateClusterISO(ctx context.Context, params installer.GenerateClusterISOParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("prepare image for cluster %s", params.ClusterID)
	var cluster models.Cluster

	tx := b.db.Begin()
	defer func() {
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
		tx.Rollback()
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
		tx.Rollback()
		return installer.NewGenerateClusterISOConflict()
	}

	cluster.ImageInfo.ProxyURL = params.ImageCreateParams.ProxyURL
	cluster.ImageInfo.SSHPublicKey = params.ImageCreateParams.SSHPublicKey
	cluster.ImageInfo.CreatedAt = strfmt.DateTime(now)

	if err := tx.Model(&cluster).Update(cluster).Error; err != nil {
		tx.Rollback()
		log.WithError(err).Errorf("failed to update cluster: %s", params.ClusterID)
		return installer.NewGenerateClusterISOInternalServerError()
	}

	if tx.Commit().Error != nil {
		tx.Rollback()
		return installer.NewGenerateClusterISOInternalServerError()
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
	return installer.NewGenerateClusterISOCreated().WithPayload(&cluster)
}

func getImageName(clusterID strfmt.UUID) string {
	return fmt.Sprintf("discovery-image-%s", clusterID.String())
}

type clusterInstaller struct {
	ctx    context.Context
	b      *bareMetalInventory
	params installer.InstallClusterParams
}

func (c *clusterInstaller) verifyClusterNetworkConfig(cluster *models.Cluster, tx *gorm.DB) error {
	cidr, err := common.CalculateMachineNetworkCIDR(cluster)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}
	if cidr != cluster.MachineNetworkCidr {
		return common.NewApiError(http.StatusBadRequest,
			fmt.Errorf("Cluster machine CIDR %s is different than the calculated CIDR %s", cluster.MachineNetworkCidr, cidr))
	}
	if err = common.VerifyVips(cluster, true); err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}
	machineCidrHosts, err := common.GetMachineCIDRHosts(cluster)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, err)
	}
	masterNodesIds, err := c.b.clusterApi.GetMasterNodesIds(c.ctx, cluster, tx)
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

func (c *clusterInstaller) installHosts(cluster *models.Cluster, tx *gorm.DB) error {
	// move hosts states to installing
	for i := range cluster.Hosts {
		if _, err := c.b.hostApi.Install(c.ctx, cluster.Hosts[i], tx); err != nil {
			return common.NewApiError(http.StatusConflict, errors.Wrapf(err, "failed to install hosts <%s> in cluster: %s",
				cluster.Hosts[i].ID.String(), cluster.ID.String()))
		}
	}
	return nil
}

func (c clusterInstaller) install(tx *gorm.DB) error {
	var cluster models.Cluster
	var err error
	if err = tx.Preload("Hosts").First(&cluster, "id = ?", c.params.ClusterID).Error; err != nil {
		return common.NewApiError(http.StatusNotFound, err)
	}
	if err = c.verifyClusterNetworkConfig(&cluster, tx); err != nil {
		return err
	}

	if err = c.b.clusterApi.Install(c.ctx, &cluster, tx); err != nil {
		return common.NewApiError(http.StatusConflict, errors.Wrapf(err, "failed to install cluster %s", cluster.ID.String()))
	}

	// set one of the master nodes as bootstrap
	if err = c.b.setBootstrapHost(c.ctx, cluster, tx); err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	// move hosts states to installing
	if err = c.installHosts(&cluster, tx); err != nil {
		return err
	}

	if err = c.b.generateClusterInstallConfig(c.ctx, cluster); err != nil {
		return common.NewApiError(http.StatusInternalServerError, err)
	}
	return nil
}

func (b *bareMetalInventory) InstallCluster(ctx context.Context, params installer.InstallClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster models.Cluster
	var err error

	defer func() {
		if r := recover(); r != nil {
			log.Error("update cluster failed")
		}
	}()
	err = b.db.Transaction(clusterInstaller{
		ctx:    ctx,
		b:      b,
		params: params,
	}.install)

	if err != nil {
		log.WithError(err).Warn("Cluster install")
		return common.GenerateErrorResponder(err)
	}
	if err = b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return common.GenerateErrorResponder(err)
	}
	log.Infof("Successfully started cluster <%s> installation", params.ClusterID.String())
	return installer.NewInstallClusterAccepted().WithPayload(&cluster)
}

func (b *bareMetalInventory) setBootstrapHost(ctx context.Context, cluster models.Cluster, db *gorm.DB) error {
	log := logutil.FromContext(ctx, b.log)

	masterNodesIds, err := b.clusterApi.GetMasterNodesIds(ctx, &cluster, db)
	if err != nil {
		log.WithError(err).Errorf("failed to get cluster %s master node id's", cluster.ID)
		return errors.Wrapf(err, "Failed to get cluster %s master node id's", cluster.ID)
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

func (b *bareMetalInventory) generateClusterInstallConfig(ctx context.Context, cluster models.Cluster) error {
	log := logutil.FromContext(ctx, b.log)

	cfg, err := installcfg.GetInstallConfig(&cluster)
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
	return nil
}

func (b *bareMetalInventory) refreshClusterHosts(ctx context.Context, cluster *models.Cluster, tx *gorm.DB, log logrus.FieldLogger) middleware.Responder {
	for _, h := range cluster.Hosts {
		var host models.Host
		var err error
		if err = tx.Take(&host, "id = ? and cluster_id = ?",
			h.ID.String(), cluster.ID.String()).Error; err != nil {
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				h.ID.String(), cluster.ID.String())
			return installer.NewUpdateClusterNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		}
		if _, err = b.hostApi.RefreshStatus(ctx, &host, tx); err != nil {
			log.WithError(err).Errorf("failed to refresh state of host %s cluster %s", *h.ID, cluster.ID.String())
			return installer.NewInstallClusterInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}
	return nil
}

func (b *bareMetalInventory) UpdateCluster(ctx context.Context, params installer.UpdateClusterParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster models.Cluster
	var err error
	log.Info("update cluster ", params.ClusterID)

	tx := b.db.Begin()
	defer func() {
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

	if err = tx.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		tx.Rollback()
		return installer.NewUpdateClusterNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
	}
	updateIPv4 := func(target *strfmt.IPv4, source *strfmt.IPv4) {
		if source != nil {
			*target = *source
		}
	}
	updateString := func(target *string, source *string) {
		if source != nil {
			*target = *source
		}
	}

	updateString(&cluster.Name, params.ClusterUpdateParams.Name)
	updateIPv4(&cluster.APIVip, params.ClusterUpdateParams.APIVip)
	updateString(&cluster.BaseDNSDomain, params.ClusterUpdateParams.BaseDNSDomain)
	if params.ClusterUpdateParams.ClusterNetworkCidr != nil {
		cluster.ClusterNetworkCidr = *params.ClusterUpdateParams.ClusterNetworkCidr
	}
	if params.ClusterUpdateParams.ClusterNetworkHostPrefix != nil {
		cluster.ClusterNetworkHostPrefix = *params.ClusterUpdateParams.ClusterNetworkHostPrefix
	}
	if params.ClusterUpdateParams.ServiceNetworkCidr != nil {
		cluster.ServiceNetworkCidr = *params.ClusterUpdateParams.ServiceNetworkCidr
	}
	updateIPv4(&cluster.IngressVip, params.ClusterUpdateParams.IngressVip)
	updateString(&cluster.SSHPublicKey, params.ClusterUpdateParams.SSHPublicKey)
	var machineCidr string
	if machineCidr, err = common.CalculateMachineNetworkCIDR(&cluster); err != nil {
		tx.Rollback()
		log.WithError(err).Errorf("failed to calculate machine network cidr for cluster: %s", params.ClusterID)
		return installer.NewUpdateClusterBadRequest().WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}
	machineCidrUpdated := machineCidr != cluster.MachineNetworkCidr
	cluster.MachineNetworkCidr = machineCidr
	err = common.VerifyVips(&cluster, false)
	if err != nil {
		tx.Rollback()
		log.WithError(err).Errorf("VIP verification failed for cluster: %s", params.ClusterID)
		return installer.NewUpdateClusterBadRequest().WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}
	setPullSecret(&cluster, swag.StringValue(params.ClusterUpdateParams.PullSecret))

	if err = tx.Model(&cluster).Update(cluster).Error; err != nil {
		tx.Rollback()
		log.WithError(err).Errorf("failed to update cluster: %s", params.ClusterID)
		return installer.NewUpdateClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	for i := range params.ClusterUpdateParams.HostsRoles {
		log.Infof("Update host %s to role: %s", params.ClusterUpdateParams.HostsRoles[i].ID,
			params.ClusterUpdateParams.HostsRoles[i].Role)
		var host models.Host
		if err = tx.First(&host, "id = ? and cluster_id = ?",
			params.ClusterUpdateParams.HostsRoles[i].ID, params.ClusterID).Error; err != nil {
			tx.Rollback()
			log.WithError(err).Errorf("failed to find host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsRoles[i].ID, params.ClusterID)
			return installer.NewUpdateClusterNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		}
		if _, err = b.hostApi.UpdateRole(ctx, &host, params.ClusterUpdateParams.HostsRoles[i].Role, tx); err != nil {
			tx.Rollback()
			log.WithError(err).Errorf("failed to set role <%s> host <%s> in cluster <%s>",
				params.ClusterUpdateParams.HostsRoles[i].Role, params.ClusterUpdateParams.HostsRoles[i].ID,
				params.ClusterID)
			return installer.NewUpdateClusterConflict().WithPayload(common.GenerateError(http.StatusConflict, err))
		}
	}

	if machineCidrUpdated {
		responder := b.refreshClusterHosts(ctx, &cluster, tx, log)
		if responder != nil {
			tx.Rollback()
			return responder
		}
	}

	if _, err = b.clusterApi.RefreshStatus(ctx, &cluster, tx); err != nil {
		tx.Rollback()
		log.WithError(err).Errorf("failed to validate or update cluster %s state", params.ClusterID)
		return installer.NewUpdateClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	if tx.Commit().Error != nil {
		tx.Rollback()
		return installer.NewUpdateClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, errors.New("DB error, failed to commit")))
	}

	if err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster %s after update", params.ClusterID)
		return installer.NewUpdateClusterInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	cluster.HostNetworks = calculateHostNetworks(&cluster)
	return installer.NewUpdateClusterCreated().WithPayload(&cluster)
}

func calculateHostNetworks(cluster *models.Cluster) []*models.HostNetwork {
	cidrHostsMap := make(map[string][]strfmt.UUID)
	for _, h := range cluster.Hosts {
		if h.Inventory == "" {
			continue
		}
		var inventory models.Inventory
		err := json.Unmarshal([]byte(h.Inventory), &inventory)
		if err != nil {
			logrus.WithError(err).Warnf("Could not parse inventory of host %s", *h.ID)
			continue
		}
		for _, intf := range inventory.Interfaces {
			for _, ipv4Address := range intf.IPV4Addresses {
				_, ipnet, err := net.ParseCIDR(ipv4Address)
				if err != nil {
					logrus.WithError(err).Warnf("Could not parse CIDR %s", ipv4Address)
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
	var clusters []*models.Cluster
	if err := b.db.Preload("Hosts").Find(&clusters).Error; err != nil {
		log.WithError(err).Error("failed to list clusters")
		return installer.NewListClustersInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}

	return installer.NewListClustersOK().WithPayload(clusters)
}

func (b *bareMetalInventory) GetCluster(ctx context.Context, params installer.GetClusterParams) middleware.Responder {
	var cluster models.Cluster
	if err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		// TODO: check for the right error
		return installer.NewGetClusterNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}
	cluster.HostNetworks = calculateHostNetworks(&cluster)
	return installer.NewGetClusterOK().WithPayload(&cluster)
}

func (b *bareMetalInventory) RegisterHost(ctx context.Context, params installer.RegisterHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var host models.Host
	log.Infof("Register host: %+v", params)

	if err := b.db.First(&models.Cluster{}, "id = ?", params.ClusterID.String()).Error; err != nil {
		log.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID.String())
		return installer.NewRegisterHostBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	url := installer.GetHostURL{ClusterID: params.ClusterID, HostID: *params.NewHostParams.HostID}
	host = models.Host{
		ID:          params.NewHostParams.HostID,
		Href:        swag.String(url.String()),
		Kind:        swag.String(ResourceKindHost),
		ClusterID:   params.ClusterID,
		CheckedInAt: strfmt.DateTime(time.Now()),
	}

	if err := b.hostApi.RegisterHost(ctx, &host); err != nil {
		log.WithError(err).Errorf("failed to register host <%s> cluster <%s>",
			params.NewHostParams.HostID.String(), params.ClusterID.String())
		return installer.NewRegisterHostBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	return installer.NewRegisterHostCreated().WithPayload(&host)
}

func (b *bareMetalInventory) DeregisterHost(ctx context.Context, params installer.DeregisterHostParams) middleware.Responder {
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
	return installer.NewListHostsOK().WithPayload(hosts)
}

func createStepID(stepType models.StepType) string {
	return fmt.Sprintf("%s-%s", stepType, uuid.New().String()[:8])
}

func (b *bareMetalInventory) GetNextSteps(ctx context.Context, params installer.GetNextStepsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var steps models.Steps
	var host models.Host

	tx := b.db.Begin()
	defer func() {
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
		tx.Rollback()
		return installer.NewGetNextStepsNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	host.CheckedInAt = strfmt.DateTime(time.Now())
	if err := tx.Model(&host).Update("checked_in_at", host.CheckedInAt).Error; err != nil {
		tx.Rollback()
		log.WithError(err).Errorf("failed to update host: %s", params.ClusterID)
		return installer.NewGetNextStepsInternalServerError()
	}

	if tx.Commit().Error != nil {
		tx.Rollback()
		return installer.NewGetNextStepsInternalServerError()
	}

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
		steps = append(steps, step)
		delete(b.debugCmdMap, params.HostID)
	}
	b.debugCmdMux.Unlock()

	return installer.NewGetNextStepsOK().WithPayload(steps)
}

func (b *bareMetalInventory) PostStepReply(ctx context.Context, params installer.PostStepReplyParams) middleware.Responder {
	var err error
	log := logutil.FromContext(ctx, b.log)
	log.Infof("Received step reply <%s> from cluster <%s> host <%s>  exit-code <%d> stdout <%s> stderr <%s>", params.Reply.StepID, params.ClusterID,
		params.HostID, params.Reply.ExitCode, params.Reply.Output, params.Reply.Error)

	//check the output exit code
	if params.Reply.ExitCode != 0 {
		err = fmt.Errorf("Exit code is %d reply error is %s for %s reply for host %s cluster %s",
			params.Reply.ExitCode, params.Reply.Error, params.Reply.StepID, params.HostID, params.ClusterID)
		log.WithError(err).Errorf("Exit code is <%d> , reply error is <%s> for <%s> reply for host <%s> cluster <%s>",
			params.Reply.ExitCode, params.Reply.Error, params.Reply.StepID, params.HostID, params.ClusterID)
		return installer.NewPostStepReplyBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, err))
	}

	var host models.Host
	if err = b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("Failed to find host <%s> cluster <%s> step <%s>",
			params.HostID, params.ClusterID, params.Reply.StepID)
		return installer.NewPostStepReplyNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

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

func handleReplyByType(params installer.PostStepReplyParams, b *bareMetalInventory, ctx context.Context, host models.Host, stepReply string) error {
	var err error
	if strings.HasPrefix(params.Reply.StepID, string(models.StepTypeHardwareInfo)) {
		_, err = b.hostApi.UpdateHwInfo(ctx, &host, stepReply)
	}
	if strings.HasPrefix(params.Reply.StepID, string(models.StepTypeConnectivityCheck)) {
		err = b.hostApi.UpdateConnectivityReport(ctx, &host, stepReply)
	}
	if strings.HasPrefix(params.Reply.StepID, string(models.StepTypeInventory)) {
		_, err = b.hostApi.UpdateInventory(ctx, &host, stepReply)
	}
	return err
}

func filterReplyByType(params installer.PostStepReplyParams) (string, error) {
	var stepReply string
	var err error
	// To make sure we store only information defined in swagger we unmarshal and marshal the stepReplyParams.
	if strings.HasPrefix(params.Reply.StepID, string(models.StepTypeHardwareInfo)) {
		stepReply, err = filterReply(&models.Introspection{}, params.Reply.Output)
	}

	if strings.HasPrefix(params.Reply.StepID, string(models.StepTypeConnectivityCheck)) {
		stepReply, err = filterReply(&models.ConnectivityReport{}, params.Reply.Output)
	}

	if strings.HasPrefix(params.Reply.StepID, string(models.StepTypeInventory)) {
		stepReply, err = filterReply(&models.Inventory{}, params.Reply.Output)
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
		return installer.NewDisableHostNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	if _, err := b.hostApi.DisableHost(ctx, &host); err != nil {
		log.WithError(err).Errorf("failed to disable host <%s> from cluster <%s>", params.HostID, params.ClusterID)
		return installer.NewDisableHostConflict().
			WithPayload(common.GenerateError(http.StatusConflict, err))
	}
	return installer.NewDisableHostNoContent()
}

func (b *bareMetalInventory) EnableHost(ctx context.Context, params installer.EnableHostParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var host models.Host
	log.Info("enable host: ", params.HostID)

	if err := b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		return installer.NewEnableHostNotFound().
			WithPayload(common.GenerateError(http.StatusNotFound, err))
	}

	if _, err := b.hostApi.EnableHost(ctx, &host); err != nil {
		log.WithError(err).Errorf("failed to enable host <%s> from cluster <%s>", params.HostID, params.ClusterID)
		return installer.NewEnableHostConflict().
			WithPayload(common.GenerateError(http.StatusConflict, err))
	}
	return installer.NewEnableHostNoContent()
}

func (b *bareMetalInventory) createKubeconfigJob(cluster *models.Cluster, jobName string, cfg []byte) *batch.Job {
	id := cluster.ID
	// [TODO] need to find more generic way to set the openshift release image
	//https://mirror.openshift.com/pub/openshift-v4/clients/ocp-dev-preview/4.5.0-0.nightly-2020-05-21-015458/
	overrideImageName := "quay.io/openshift-release-dev/ocp-release-nightly@sha256:a9f7564e0f2edef2c15cc1da699ebd1d11f5acd717c3668940848b3fed0d13c7"
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
	var cluster models.Cluster

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
	clusterStatus := swag.StringValue(cluster.Status)
	allowedStatuses := []string{ClusterStatusInstalling, ClusterStatusInstalled, ClusterStatusError}
	if !funk.ContainsString(allowedStatuses, clusterStatus) {
		msg := fmt.Sprintf("Cluster %s is in %s state, files can be downloaded only when status is one of: %s",
			params.ClusterID, clusterStatus, allowedStatuses)
		log.Warn(msg)
		return installer.NewDownloadClusterFilesConflict().
			WithPayload(common.GenerateError(http.StatusConflict, errors.New(msg)))
	}

	respBody, err := b.s3Client.DownloadFileFromS3(ctx, fmt.Sprintf("%s/%s", params.ClusterID, params.FileName), b.S3Bucket)
	if err != nil {
		return installer.NewDownloadClusterFilesInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(installer.NewDownloadClusterFilesOK().WithPayload(respBody), params.FileName)
}

func (b *bareMetalInventory) DownloadClusterKubeconfig(ctx context.Context, params installer.DownloadClusterKubeconfigParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster models.Cluster

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
	clusterStatus := swag.StringValue(cluster.Status)
	if clusterStatus != ClusterStatusInstalled {
		msg := fmt.Sprintf("Cluster %s is in %s state, %s can be downloaded only in installed state", kubeconfig, params.ClusterID, clusterStatus)
		log.Warn(msg)
		return installer.NewDownloadClusterKubeconfigConflict().
			WithPayload(common.GenerateError(http.StatusConflict, errors.New(msg)))
	}

	respBody, err := b.s3Client.DownloadFileFromS3(ctx, fmt.Sprintf("%s/%s", params.ClusterID, kubeconfig), b.S3Bucket)

	if err != nil {
		return installer.NewDownloadClusterKubeconfigInternalServerError().
			WithPayload(common.GenerateError(http.StatusInternalServerError, err))
	}
	return filemiddleware.NewResponder(installer.NewDownloadClusterKubeconfigOK().WithPayload(respBody), kubeconfig)
}

func (b *bareMetalInventory) GetCredentials(ctx context.Context, params installer.GetCredentialsParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	var cluster models.Cluster

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
	clusterStatus := swag.StringValue(cluster.Status)
	if clusterStatus != ClusterStatusInstalling && clusterStatus != ClusterStatusInstalled {
		msg := fmt.Sprintf("Cluster %s is in %s state, credentials are available only in installing or installed state", params.ClusterID, clusterStatus)
		log.Warn(msg)
		return installer.NewGetCredentialsConflict().
			WithPayload(common.GenerateError(http.StatusConflict, errors.New(msg)))
	}
	fileName := "kubeadmin-password"
	filesUrl := fmt.Sprintf("%s/%s/%s", b.S3EndpointURL, b.S3Bucket,
		fmt.Sprintf("%s/%s", params.ClusterID, fileName))
	log.Info("File URL: ", filesUrl)
	resp, err := http.Get(filesUrl)
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
	if err := b.hostApi.UpdateInstallProgress(ctx, &host, string(params.HostInstallProgressParams)); err != nil {
		log.WithError(err).Errorf("failed to update host %s progress", params.HostID)
		// host have nothing to do with the error so we just log it
		return installer.NewUpdateHostInstallProgressOK()
	}
	msg := fmt.Sprintf("Host %s in cluster %s reached installation step %s", host.ID, host.ClusterID, params.HostInstallProgressParams)
	b.eventsHandler.AddEvent(ctx, host.ID.String(), msg, time.Now(), host.ClusterID.String())
	return installer.NewUpdateHostInstallProgressOK()
}

func (b *bareMetalInventory) UploadClusterIngressCert(ctx context.Context, params installer.UploadClusterIngressCertParams) middleware.Responder {
	log := logutil.FromContext(ctx, b.log)
	log.Infof("UploadClusterIngressCert for cluster %s with params %s", params.ClusterID, params.IngressCertParams)
	var cluster models.Cluster

	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		log.WithError(err).Errorf("failed to find cluster %s", params.ClusterID)
		if gorm.IsRecordNotFoundError(err) {
			return installer.NewUploadClusterIngressCertNotFound().WithPayload(common.GenerateError(http.StatusNotFound, err))
		} else {
			return installer.NewUploadClusterIngressCertInternalServerError().
				WithPayload(common.GenerateError(http.StatusInternalServerError, err))
		}
	}

	clusterStatus := swag.StringValue(cluster.Status)
	if clusterStatus != ClusterStatusInstalled {
		msg := fmt.Sprintf("Cluster %s is in %s state, upload ingress ca can be done only in installed state", params.ClusterID, clusterStatus)
		log.Warn(msg)
		return installer.NewUploadClusterIngressCertBadRequest().
			WithPayload(common.GenerateError(http.StatusBadRequest, errors.New(msg)))
	}

	fileName := fmt.Sprintf("%s/%s", cluster.ID, kubeconfig)
	exists, err := b.s3Client.DoesObjectExists(ctx, fileName, b.S3Bucket)
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
	resp, err := b.s3Client.DownloadFileFromS3(ctx, noigress, b.S3Bucket)
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

func setPullSecret(cluster *models.Cluster, pullSecret string) {
	cluster.PullSecret = pullSecret
	if pullSecret != "" {
		cluster.PullSecretSet = true
	} else {
		cluster.PullSecretSet = false
	}
}
