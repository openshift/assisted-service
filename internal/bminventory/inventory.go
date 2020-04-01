package bminventory

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/filanov/bm-inventory/internal/installcfg"
	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/restapi/operations/inventory"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const baseHref = "/api/bm-inventory/v1"

const (
	ClusterStatusCreating = "creating"
	ClusterStatusReady    = "ready"
	ClusterStatusError    = "error"
)

const (
	HostStatusDisabled    = "disabled"
	HostStatusInstalling  = "installing"
	HostStatusInstalled   = "installed"
	HostStatusDiscovering = "discovering"
)

const (
	ResourceKindHost    = "host"
	ResourceKindCluster = "cluster"
)

type Config struct {
	ImageBuilder    string `envconfig:"IMAGE_BUILDER" default:"quay.io/oscohen/installer-image-build"`
	ImageBuilderCmd string `envconfig:"IMAGE_BUILDER_CMD" default:"echo hello"`
	AgentDockerImg  string `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/oamizur/introspector:latest"`
	InventoryURL    string `envconfig:"INVENTORY_URL" default:"10.35.59.36"`
	InventoryPort   string `envconfig:"INVENTORY_PORT" default:"30485"`
	S3EndpointURL   string `envconfig:"S3_ENDPOINT_URL" default:"http://10.35.59.36:30925"`
	S3Bucket        string `envconfig:"S3_BUCKET" default:"test"`
}

const ignitionConfigFormat = `{
"ignition": { "version": "3.0.0" },
  "passwd": {
    "users": [
      {
        "groups": [
          "sudo",
          "docker"
        ],
        "name": "core",
        "passwordHash": "$6$MWO4bibU8TIWG0XV$Hiuj40lWW7pHiwJmXA8MehuBhdxSswLgvGxEh8ByEzeX2D1dk87JILVUYS4JQOP45bxHRegAB9Fs/SWfszXa5."
      }
	 %s
    ]
  },
"systemd": {
"units": [{
"name": "introspector.service",
"enabled": true,
"contents": "[Service]\nType=simple\nExecStartPre=docker run --privileged --rm -v /usr/local/bin:/hostbin %s cp /usr/bin/introspector /usr/sbin/dmidecode /hostbin\nExecStart=/usr/local/bin/introspector --host %s --port %s --cluster-id %s\n\n[Install]\nWantedBy=multi-user.target"
}]
}
}`

type bareMetalInventory struct {
	Config
	imageBuildCmd []string
	db            *gorm.DB
	kube          client.Client
	debugCmdMap   map[strfmt.UUID]string
	debugCmdMux   sync.Mutex
}

func NewBareMetalInventory(db *gorm.DB, kclient client.Client, cfg Config) *bareMetalInventory {
	b := &bareMetalInventory{db: db, kube: kclient, Config: cfg, debugCmdMap: make(map[strfmt.UUID]string)}
	if cfg.ImageBuilderCmd != "" {
		b.imageBuildCmd = strings.Split(cfg.ImageBuilderCmd, " ")
	}
	return b
}

func strToURI(str string) *strfmt.URI {
	uri := strfmt.URI(str)
	return &uri
}

func buildHrefURI(base, id string) *strfmt.URI {
	return strToURI(fmt.Sprintf("%s/%ss/%s", baseHref, base, id))
}

func (b *bareMetalInventory) monitorImageBuild(ctx context.Context, id string) error {
	var job batch.Job
	if err := b.kube.Get(ctx, client.ObjectKey{
		Namespace: "default",
		Name:      fmt.Sprintf("create-image-%s", id),
	}, &job); err != nil {
		return err
	}

	for job.Status.Succeeded == 0 && job.Status.Failed == 0 {
		if err := b.kube.Get(ctx, client.ObjectKey{
			Namespace: "default",
			Name:      fmt.Sprintf("create-image-%s", id),
		}, &job); err != nil {
			return err
		}
	}

	if job.Status.Failed > 0 {
		logrus.Error("job failed")
		return fmt.Errorf("job failed")
	}

	if err := b.kube.Delete(context.Background(), &job); err != nil {
		logrus.WithError(err).Error("failed to delete job")
	}

	return nil
}

func (b *bareMetalInventory) createImageJob(ctx context.Context, cluster *models.Cluster) error {
	id := cluster.ID
	if err := b.kube.Create(ctx, &batch.Job{
		TypeMeta: meta.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: meta.ObjectMeta{
			Name:      fmt.Sprintf("create-image-%s", id),
			Namespace: "default",
		},
		Spec: batch.JobSpec{
			BackoffLimit: swag.Int32(2),
			Template: core.PodTemplateSpec{
				ObjectMeta: meta.ObjectMeta{
					Name:      fmt.Sprintf("create-image-%s", id),
					Namespace: "default",
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
									Value: b.formatIgnitionFile(cluster),
								},
								{
									Name:  "IMAGE_NAME",
									Value: fmt.Sprintf("installer-image-%s", id),
								},
								{
									Name:  "S3_BUCKET",
									Value: b.S3Bucket,
								},
							},
						},
					},
					RestartPolicy: "Never",
				},
			},
		},
	}); err != nil {
		return err
	}
	return nil
}

func (b *bareMetalInventory) formatIgnitionFile(cluster *models.Cluster) string {
	return fmt.Sprintf(ignitionConfigFormat, b.getUserSshKey(cluster), b.AgentDockerImg, b.InventoryURL,
		b.InventoryPort, cluster.ID.String())
}

func (b *bareMetalInventory) getUserSshKey(cluster *models.Cluster) string {
	if cluster.SSHPublicKey == "" {
		return ""
	}
	return fmt.Sprintf(`,{
		"name": "systemUser",
		"passwordHash": "$6$MWO4bibU8TIWG0XV$Hiuj40lWW7pHiwJmXA8MehuBhdxSswLgvGxEh8ByEzeX2D1dk87JILVUYS4JQOP45bxHRegAB9Fs/SWfszXa5.",
		"sshAuthorizedKeys": [
		"%s"],
		"groups": [ "sudo" ]}`, cluster.SSHPublicKey)
}

func (b *bareMetalInventory) RegisterCluster(ctx context.Context, params inventory.RegisterClusterParams) middleware.Responder {
	logrus.Infof("Register cluster: %s", swag.StringValue(params.NewClusterParams.Name))
	id := strfmt.UUID(uuid.New().String())
	cluster := models.Cluster{
		Base: models.Base{
			Href: buildHrefURI(ResourceKindCluster, id.String()),
			ID:   &id,
			Kind: swag.String(ResourceKindCluster),
		},
		APIVip:                   params.NewClusterParams.APIVip,
		BaseDNSDomain:            params.NewClusterParams.BaseDNSDomain,
		ClusterNetworkCIDR:       params.NewClusterParams.ClusterNetworkCIDR,
		ClusterNetworkHostPrefix: params.NewClusterParams.ClusterNetworkHostPrefix,
		DNSVip:                   params.NewClusterParams.DNSVip,
		IngressVip:               params.NewClusterParams.IngressVip,
		Name:                     swag.StringValue(params.NewClusterParams.Name),
		OpenshiftVersion:         params.NewClusterParams.OpenshiftVersion,
		PullSecret:               params.NewClusterParams.PullSecret,
		ServiceNetworkCIDR:       params.NewClusterParams.ServiceNetworkCIDR,
		SSHPublicKey:             params.NewClusterParams.SSHPublicKey,
		Status:                   swag.String(ClusterStatusReady),
		UpdatedAt:                strfmt.DateTime{},
	}

	if err := b.db.Preload("Hosts").Create(&cluster).Error; err != nil {
		return inventory.NewRegisterClusterInternalServerError()
	}

	return inventory.NewRegisterClusterCreated().WithPayload(&cluster)
}

func (b *bareMetalInventory) DeregisterCluster(ctx context.Context, params inventory.DeregisterClusterParams) middleware.Responder {
	var cluster models.Cluster
	var txErr error
	tx := b.db.Begin()

	defer func() {
		if txErr != nil {
			tx.Rollback()
		}
	}()

	if err := tx.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return inventory.NewDeregisterClusterNotFound()
	}

	for i := range cluster.Hosts {
		if txErr = tx.Where("id = ? and cluster_id = ?", cluster.Hosts[i], params.ClusterID).Delete(&models.Host{}).Error; txErr != nil {
			logrus.WithError(txErr).Errorf("failed to delete host: %s", cluster.Hosts[i].ID)
			// TODO: fix error code
			return inventory.NewDeregisterClusterNotFound()
		}
	}
	if txErr = tx.Delete(cluster).Error; txErr != nil {
		logrus.WithError(txErr).Errorf("failed to delete cluster %s", cluster.ID)
		// TODO: fix error code
		return inventory.NewDeregisterClusterNotFound()
	}

	if txErr = tx.Commit().Error; txErr != nil {
		logrus.WithError(txErr).Errorf("failed to delete cluster %s, commit tx", cluster.ID)
		// TODO: fix error code
		return inventory.NewDeregisterClusterNotFound()
	}

	return inventory.NewDeregisterClusterNoContent()
}

func (b *bareMetalInventory) DownloadClusterISO(ctx context.Context, params inventory.DownloadClusterISOParams) middleware.Responder {
	logrus.Infof("prepare and download image for cluster %s", params.ClusterID)
	var cluster models.Cluster
	if err := b.db.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		logrus.WithError(err).Errorf("failed to get cluster %s", params.ClusterID)
		return inventory.NewDownloadClusterISONotFound()
	}

	if err := b.createImageJob(ctx, &cluster); err != nil {
		logrus.WithError(err).Error("failed to create image job")
		return inventory.NewDownloadClusterISOInternalServerError()
	}

	if err := b.monitorImageBuild(ctx, params.ClusterID.String()); err != nil {
		logrus.WithError(err).Error("image creation failed")
		return inventory.NewDownloadClusterISOInternalServerError()
	}

	imageURL := fmt.Sprintf("%s/%s/%s", b.S3EndpointURL, b.S3Bucket,
		fmt.Sprintf("installer-image-%s", params.ClusterID))
	logrus.Info("Image URL: ", imageURL)
	resp, err := http.Get(imageURL)
	if err != nil {
		logrus.WithError(err).Error("failed to get image")
		return inventory.NewDownloadClusterISOInternalServerError()
	}

	return inventory.NewDownloadClusterISOOK().WithPayload(resp.Body)
}

func (b *bareMetalInventory) InstallCluster(ctx context.Context, params inventory.InstallClusterParams) middleware.Responder {
	var cluster models.Cluster
	tx := b.db.Begin()
	if tx.Error != nil {
		logrus.WithError(tx.Error).Errorf("failed to start db transaction")
		return inventory.NewInstallClusterInternalServerError()
	}
	defer func() {
		if r := recover(); r != nil {
			logrus.Error("update cluster failed")
			tx.Rollback()
		}
	}()

	if err := tx.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return inventory.NewInstallClusterNotFound()
	}

	// create install-config.yaml
	cfg, err := installcfg.GetInstallConfig(&cluster)
	if err != nil {
		logrus.WithError(err).Errorf("failed to get install config for cluster %s", params.ClusterID)
		return inventory.NewInstallClusterInternalServerError()
	}
	fmt.Println("Install config: \n", string(cfg))

	// generate ignition files from install-config.yaml

	// move hosts states to installing
	if reply := tx.Model(&models.Host{}).
		Where("cluster_id = ?", params.ClusterID).
		Update("status", HostStatusInstalling); reply.Error != nil || reply.RowsAffected == 0 {
		logrus.WithError(reply.Error).Errorf("failed to update hosts in cluster: %s state to %s",
			cluster.ID.String(), HostStatusInstalling)
		tx.Rollback()
		return inventory.NewInstallClusterInternalServerError()
	}

	if err := tx.Model(&models.Cluster{}).Where("id = ?", cluster.ID.String()).
		Update("status", "installing").Error; err != nil {
		logrus.WithError(err).Errorf("failed to update cluster %s to status installing", cluster.ID.String())
		tx.Rollback()
		return inventory.NewInstallClusterInternalServerError()
	}
	if err := tx.Commit().Error; err != nil {
		tx.Rollback()
		logrus.WithError(err).Errorf("failed to commit cluster %s changes on installation", cluster.ID.String())
		return inventory.NewInstallClusterInternalServerError()
	}

	if err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		return inventory.NewInstallClusterInternalServerError()
	}

	return inventory.NewInstallClusterOK().WithPayload(&cluster)
}

func (b *bareMetalInventory) UpdateCluster(ctx context.Context, params inventory.UpdateClusterParams) middleware.Responder {
	var cluster models.Cluster
	logrus.Info("update cluster ", params.ClusterID)

	tx := b.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			logrus.Error("update cluster failed")
			tx.Rollback()
		}
	}()

	if tx.Error != nil {
		logrus.WithError(tx.Error).Error("failed to start transaction")
	}

	if err := tx.First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		logrus.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID)
		tx.Rollback()
		return inventory.NewUpdateClusterNotFound()
	}

	cluster.Name = params.ClusterUpdateParams.Name
	cluster.APIVip = params.ClusterUpdateParams.APIVip
	cluster.BaseDNSDomain = params.ClusterUpdateParams.BaseDNSDomain
	cluster.ClusterNetworkCIDR = params.ClusterUpdateParams.ClusterNetworkCIDR
	cluster.ClusterNetworkHostPrefix = params.ClusterUpdateParams.ClusterNetworkHostPrefix
	cluster.DNSVip = params.ClusterUpdateParams.DNSVip
	cluster.IngressVip = params.ClusterUpdateParams.IngressVip
	cluster.OpenshiftVersion = params.ClusterUpdateParams.OpenshiftVersion
	cluster.PullSecret = params.ClusterUpdateParams.PullSecret
	cluster.ServiceNetworkCIDR = params.ClusterUpdateParams.ServiceNetworkCIDR
	cluster.SSHPublicKey = params.ClusterUpdateParams.SSHPublicKey

	if err := tx.Model(&cluster).Update(cluster).Error; err != nil {
		tx.Rollback()
		logrus.WithError(err).Errorf("failed to update cluster: %s", params.ClusterID)
		return inventory.NewUpdateClusterInternalServerError()
	}

	for i := range params.ClusterUpdateParams.HostsRoles {
		logrus.Infof("Update host %s to role: %s", params.ClusterUpdateParams.HostsRoles[i].ID,
			params.ClusterUpdateParams.HostsRoles[i].Role)
		reply := tx.Model(&models.Host{}).
			Where("id = ? and cluster_id = ?", params.ClusterUpdateParams.HostsRoles[i].ID, params.ClusterID).
			Update("role", params.ClusterUpdateParams.HostsRoles[i].Role)
		if reply.Error != nil || reply.RowsAffected == 0 {
			tx.Rollback()
			logrus.WithError(reply.Error).Error("failed to update host: ",
				params.ClusterUpdateParams.HostsRoles[i].ID)
			return inventory.NewUpdateClusterNotFound()
		}
	}

	if tx.Commit().Error != nil {
		tx.Rollback()
		return inventory.NewUpdateClusterInternalServerError()
	}

	if err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		logrus.WithError(err).Errorf("failed to get cluster %s after update", params.ClusterID)
		return inventory.NewUpdateClusterInternalServerError()
	}

	return inventory.NewUpdateClusterCreated().WithPayload(&cluster)
}

func (b *bareMetalInventory) ListClusters(ctx context.Context, params inventory.ListClustersParams) middleware.Responder {
	var clusters []*models.Cluster
	if err := b.db.Preload("Hosts").Find(&clusters).Error; err != nil {
		logrus.WithError(err).Error("failed to list clusters")
		return inventory.NewListClustersInternalServerError()
	}

	return inventory.NewListClustersOK().WithPayload(clusters)
}

func (b *bareMetalInventory) GetCluster(ctx context.Context, params inventory.GetClusterParams) middleware.Responder {
	var cluster models.Cluster
	if err := b.db.Preload("Hosts").First(&cluster, "id = ?", params.ClusterID).Error; err != nil {
		// TODO: check for the right error
		return inventory.NewGetClusterNotFound()
	}
	return inventory.NewGetClusterOK().WithPayload(&cluster)
}

func (b *bareMetalInventory) RegisterHost(ctx context.Context, params inventory.RegisterHostParams) middleware.Responder {
	host := &models.Host{
		Base: models.Base{
			Href: strToURI(fmt.Sprintf("%s/clusters/%s/hosts/%s", baseHref, params.ClusterID, *params.NewHostParams.HostID)),
			ID:   params.NewHostParams.HostID,
			Kind: swag.String(ResourceKindHost),
		},
		Status:           swag.String("discovering"),
		ClusterID:        params.ClusterID,
		HostCreateParams: *params.NewHostParams,
	}

	logrus.Infof("Register host: %+v", host)

	if err := b.db.First(&models.Cluster{}, "id = ?", params.ClusterID.String()).Error; err != nil {
		logrus.WithError(err).Errorf("failed to get cluster: %s", params.ClusterID.String())
		return inventory.NewRegisterClusterBadRequest()
	}

	if err := b.db.Create(host).Error; err != nil {
		// check if host already exists - if it does updated the status to discovering
		if getErr := b.db.First(&models.Host{}, "id = ? and cluster_id = ?",
			params.NewHostParams.HostID, params.ClusterID).Error; getErr == nil {
			host.Status = swag.String(HostStatusDiscovering)
			if err := b.db.Model(&host).
				Where("host_id = ? and cluster_id = ?", *params.NewHostParams.HostID, params.ClusterID).
				Update("status", host.Status).Error; err != nil {
				logrus.WithError(err).Error("failed to create host")
				return inventory.NewDisableHostInternalServerError()
			} else {
				logrus.Infof("Host %s from cluster %s registered again, will go to %s state",
					*host.ID, host.ClusterID, *host.Status)
				return inventory.NewRegisterHostCreated().WithPayload(host)
			}
		}
		logrus.WithError(err).Error("failed to create host")
		return inventory.NewRegisterClusterInternalServerError()
	}

	return inventory.NewRegisterHostCreated().WithPayload(host)
}

func (b *bareMetalInventory) DeregisterHost(ctx context.Context, params inventory.DeregisterHostParams) middleware.Responder {
	if err := b.db.Where("id = ? and cluster_id = ?", params.HostID, params.ClusterID).
		Delete(&models.Host{}).Error; err != nil {
		// TODO: check error type
		return inventory.NewDeregisterHostBadRequest()
	}

	// TODO: need to check that host can be deleted from the cluster
	return inventory.NewDeregisterHostNoContent()
}

func (b *bareMetalInventory) GetHost(ctx context.Context, params inventory.GetHostParams) middleware.Responder {
	var host models.Host
	// TODO: validate what is the error
	if err := b.db.Where("id = ? and cluster_id = ?", params.HostID, params.ClusterID).
		First(&host).Error; err != nil {
		return inventory.NewGetHostNotFound()
	}

	return inventory.NewGetHostOK().WithPayload(&host)
}

func (b *bareMetalInventory) ListHosts(ctx context.Context, params inventory.ListHostsParams) middleware.Responder {
	var hosts []*models.Host
	if err := b.db.Find(&hosts, "cluster_id = ?", params.ClusterID).Error; err != nil {
		logrus.WithError(err).Errorf("failed to get list of hosts for cluster %s", params.ClusterID)
		return inventory.NewListHostsInternalServerError()
	}
	return inventory.NewListHostsOK().WithPayload(hosts)
}

func createStepID(stepType models.StepType) string {
	return fmt.Sprintf("%s-%s", stepType, uuid.New().String()[:8])
}

func (b *bareMetalInventory) GetNextSteps(ctx context.Context, params inventory.GetNextStepsParams) middleware.Responder {
	steps := models.Steps{}
	var host models.Host

	//TODO check the error type
	if err := b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		logrus.WithError(err).Error("failed to find host")
		return inventory.NewGetNextStepsNotFound()
	}

	if swag.StringValue(host.Status) == HostStatusDisabled {
		return inventory.NewGetNextStepsOK().WithPayload(steps)
	}

	b.debugCmdMux.Lock()
	if cmd, ok := b.debugCmdMap[params.HostID]; ok {
		step := &models.Step{}
		step.StepType = models.StepTypeExecute
		step.StepID = createStepID(step.StepType)
		step.Command = "bash"
		step.Args = []string{"-c", cmd}
		steps = append(steps, step)
		delete(b.debugCmdMap, params.HostID)
	}
	b.debugCmdMux.Unlock()

	steps = append(steps, &models.Step{
		StepType: models.StepTypeHardawareInfo,
		StepID:   createStepID(models.StepTypeHardawareInfo),
	})
	for _, step := range steps {
		logrus.Infof("Submitting step <%s> to cluster <%s> host <%s> Command: <%s> Arguments: <%+v>", step.StepID, params.ClusterID, params.HostID,
			step.Command, step.Args)
	}
	return inventory.NewGetNextStepsOK().WithPayload(steps)
}

func (b *bareMetalInventory) PostStepReply(ctx context.Context, params inventory.PostStepReplyParams) middleware.Responder {
	logrus.Infof("Received step reply <%s> from cluster <%s> host <%s>  exit-code <%d> stdout <%s> stderr <%s>", params.Reply.StepID, params.ClusterID,
		params.HostID, params.Reply.ExitCode, params.Reply.Output, params.Reply.Error)
	return inventory.NewPostStepReplyNoContent()
}

func (b *bareMetalInventory) SetDebugStep(ctx context.Context, params inventory.SetDebugStepParams) middleware.Responder {
	b.debugCmdMux.Lock()
	b.debugCmdMap[params.HostID] = swag.StringValue(params.Step.Command)
	b.debugCmdMux.Unlock()
	logrus.Infof("Added new debug command for cluster <%s> host <%s>: <%s>", params.ClusterID, params.HostID, swag.StringValue(params.Step.Command))
	return inventory.NewSetDebugStepOK()
}

func (b *bareMetalInventory) DisableHost(ctx context.Context, params inventory.DisableHostParams) middleware.Responder {
	var host models.Host
	logrus.Info("disabling host: ", params.HostID)

	if err := b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		return inventory.NewDisableHostNotFound()
	}

	if swag.StringValue(host.Status) == HostStatusInstalling || swag.StringValue(host.Status) == HostStatusInstalled {
		return inventory.NewDisableHostConflict()
	}
	if err := b.db.Model(&host).
		Where("host_id = ? and cluster_id = ?", params.HostID.String(), params.ClusterID.String()).
		Update("status", HostStatusDisabled).Error; err != nil {
		return inventory.NewDisableHostInternalServerError()
	}
	return inventory.NewDisableHostNoContent()
}

func (b *bareMetalInventory) EnableHost(ctx context.Context, params inventory.EnableHostParams) middleware.Responder {
	var host models.Host
	logrus.Info("enable host: ", params.HostID)

	if err := b.db.First(&host, "id = ? and cluster_id = ?", params.HostID, params.ClusterID).Error; err != nil {
		return inventory.NewEnableHostNotFound()
	}

	if swag.StringValue(host.Status) != HostStatusDisabled {
		return inventory.NewEnableHostConflict()
	}

	//TODO clear HW info
	if err := b.db.Model(&host).Where("id = ? and cluster_id = ?", params.HostID.String(), params.ClusterID).
		Update("status", HostStatusDiscovering).Error; err != nil {
		return inventory.NewEnableHostInternalServerError()
	}
	return inventory.NewEnableHostNoContent()
}

func (b *bareMetalInventory) DownloadClusterKubeconfig(ctx context.Context, params inventory.DownloadClusterKubeconfigParams) middleware.Responder {
	return inventory.NewDownloadClusterKubeconfigNotFound()
}
