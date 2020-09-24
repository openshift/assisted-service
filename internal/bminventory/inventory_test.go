package bminventory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/openshift/assisted-service/internal/hostutil"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/pkg/errors"

	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	"github.com/openshift/assisted-service/pkg/job"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/restapi/operations/installer"

	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const ClusterStatusInstalled = "installed"

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "inventory_test")
}

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}

func getTestAuthHandler() auth.AuthHandler {
	fakeConfigDisabled := auth.Config{
		EnableAuth: false,
		JwkCertURL: "",
		JwkCert:    "",
	}
	return *auth.NewAuthHandler(fakeConfigDisabled, nil, getTestLog().WithField("pkg", "auth"))
}

func strToUUID(s string) *strfmt.UUID {
	u := strfmt.UUID(s)
	return &u
}

func mockGenerateISOSuccess(mockKubeJob *job.MockAPI, mockLocalJob *job.MockLocalJob, times int) {
	if mockKubeJob != nil {
		mockKubeJob.EXPECT().GenerateISO(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(times)
	}
	if mockLocalJob != nil {
		mockLocalJob.EXPECT().GenerateISO(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(times)
	}
}

func mockGenerateISOFailure(mockKubeJob *job.MockAPI, mockLocalJob *job.MockLocalJob, times int) {
	if mockKubeJob != nil {
		mockKubeJob.EXPECT().GenerateISO(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("error")).Times(times)
	}
	if mockLocalJob != nil {
		mockLocalJob.EXPECT().GenerateISO(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("error")).Times(times)
	}
}

func mockGenerateInstallConfigSuccess(mockKubeJob *job.MockAPI, mockLocalJob *job.MockLocalJob, times int) {
	if mockKubeJob != nil {
		mockKubeJob.EXPECT().GenerateInstallConfig(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}
	if mockLocalJob != nil {
		mockLocalJob.EXPECT().GenerateInstallConfig(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}
}

func mockAbortInstallConfig(mockKubeJob *job.MockAPI, mockLocalJob *job.MockLocalJob) {
	if mockKubeJob != nil {
		mockKubeJob.EXPECT().AbortInstallConfig(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
	if mockLocalJob != nil {
		mockLocalJob.EXPECT().AbortInstallConfig(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
}

var _ = Describe("GenerateClusterISO", func() {
	var (
		bm           *bareMetalInventory
		cfg          Config
		db           *gorm.DB
		ctx          = context.Background()
		ctrl         *gomock.Controller
		mockKubeJob  *job.MockAPI
		mockLocalJob *job.MockLocalJob
		mockEvents   *events.MockHandler
		mockS3Client *s3wrapper.MockAPI
		dbName       = "generate_cluster_iso"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = common.PrepareTestDB(dbName)
		mockEvents = events.NewMockHandler(ctrl)
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	registerClusterWithHTTPProxy := func(pullSecretSet bool, httpProxy string) *common.Cluster {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID:            &clusterID,
			PullSecretSet: pullSecretSet,
			HTTPProxy:     httpProxy,
		}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		return &cluster
	}

	registerCluster := func(pullSecretSet bool) *common.Cluster {
		return registerClusterWithHTTPProxy(pullSecretSet, "")
	}

	RunGenerateClusterISOTests := func() {
		It("success", func() {
			clusterId := registerCluster(true).ID
			mockGenerateISOSuccess(mockKubeJob, mockLocalJob, 1)
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (proxy URL is \"\", SSH public key is not set)", gomock.Any())
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
			getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: *clusterId}).(*installer.GetClusterOK)
			Expect(getReply.Payload.ImageInfo.GeneratorVersion).To(Equal("quay.io/ocpmetal/assisted-iso-create:latest"))
		})

		It("success with proxy", func() {
			clusterId := registerClusterWithHTTPProxy(true, "http://1.1.1.1:1234").ID
			mockGenerateISOSuccess(mockKubeJob, mockLocalJob, 1)
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (proxy URL is \"http://1.1.1.1:1234\", SSH public key "+
				"is not set)", gomock.Any())
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))

		})

		It("image already exists", func() {
			clusterId := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{Cluster: models.Cluster{
				ID:            &clusterId,
				PullSecretSet: true,
			}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
			cluster.ProxyHash, _ = computeClusterProxyHash(nil, nil, nil)
			cluster.ImageInfo = &models.ImageInfo{GeneratorVersion: bm.Config.ImageBuilder}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			mockS3Client.EXPECT().IsAwsS3().Return(true)
			mockS3Client.EXPECT().UpdateObjectTimestamp(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockS3Client.EXPECT().GeneratePresignedDownloadURL(gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).Times(1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, nil, models.EventSeverityInfo, "Re-used existing image rather than generating a new one", gomock.Any())
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         clusterId,
				ImageCreateParams: &models.ImageCreateParams{},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
			getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: clusterId}).(*installer.GetClusterOK)
			Expect(getReply.Payload.ImageInfo.GeneratorVersion).To(Equal("quay.io/ocpmetal/assisted-iso-create:latest"))
		})

		It("image expired", func() {
			clusterId := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{Cluster: models.Cluster{
				ID:            &clusterId,
				PullSecretSet: true,
			}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
			cluster.ProxyHash, _ = computeClusterProxyHash(nil, nil, nil)
			cluster.ImageInfo = &models.ImageInfo{GeneratorVersion: bm.Config.ImageBuilder}
			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())

			mockGenerateISOSuccess(mockKubeJob, mockLocalJob, 1)
			mockS3Client.EXPECT().IsAwsS3().Return(true)
			mockS3Client.EXPECT().UpdateObjectTimestamp(gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockS3Client.EXPECT().GeneratePresignedDownloadURL(gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).Times(1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, nil, models.EventSeverityInfo, "Generated image (proxy URL is \"\", SSH public key is not set)", gomock.Any())
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         clusterId,
				ImageCreateParams: &models.ImageCreateParams{},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
			getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: clusterId}).(*installer.GetClusterOK)
			Expect(getReply.Payload.ImageInfo.GeneratorVersion).To(Equal("quay.io/ocpmetal/assisted-iso-create:latest"))
		})

		It("success with AWS S3", func() {
			clusterId := registerCluster(true).ID
			mockGenerateISOSuccess(mockKubeJob, mockLocalJob, 1)
			mockS3Client.EXPECT().IsAwsS3().Return(true)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockS3Client.EXPECT().GeneratePresignedDownloadURL(gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).Times(1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (proxy URL is \"\", SSH public key is not set)", gomock.Any())
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
			getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: *clusterId}).(*installer.GetClusterOK)
			Expect(getReply.Payload.ImageInfo.GeneratorVersion).To(Equal("quay.io/ocpmetal/assisted-iso-create:latest"))
		})

		It("cluster_not_exists", func() {
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         strfmt.UUID(uuid.New().String()),
				ImageCreateParams: &models.ImageCreateParams{},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISONotFound()))
		})

		It("failed_to_create_job", func() {
			clusterId := registerCluster(true).ID
			mockGenerateISOFailure(mockKubeJob, mockLocalJob, 1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityError, gomock.Any(), gomock.Any())
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOInternalServerError()))
		})

		It("job_failed", func() {
			clusterId := registerCluster(true).ID
			mockGenerateISOFailure(mockKubeJob, mockLocalJob, 1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityError, gomock.Any(), gomock.Any())
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOInternalServerError()))
		})

		It("failed_missing_pull_secret", func() {
			clusterId := registerCluster(false).ID
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOBadRequest()))
		})

		It("failed_missing_openshift_token", func() {
			cluster := registerCluster(true)
			cluster.PullSecret = "{\"auths\":{\"another.cloud.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"
			clusterId := cluster.ID
			mockGenerateISOFailure(mockKubeJob, mockLocalJob, 1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityError, gomock.Any(), gomock.Any())
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOInternalServerError()))
		})
	}

	Context("when kube job is used as generator", func() {
		BeforeEach(func() {
			mockKubeJob = job.NewMockAPI(ctrl)
			bm = NewBareMetalInventory(db, getTestLog(), nil, nil, cfg, mockKubeJob, mockEvents, mockS3Client, nil, getTestAuthHandler())
		})
		RunGenerateClusterISOTests()
	})

	Context("when local job is used as generator", func() {
		BeforeEach(func() {
			mockLocalJob = job.NewMockLocalJob(ctrl)
			bm = NewBareMetalInventory(db, getTestLog(), nil, nil, cfg, mockLocalJob, mockEvents, mockS3Client, nil, getTestAuthHandler())
		})
		RunGenerateClusterISOTests()
	})
})

var _ = Describe("IgnitionParameters", func() {

	var (
		bm *bareMetalInventory
	)

	cluster := common.Cluster{Cluster: models.Cluster{
		ID:            strToUUID("a640ef36-dcb1-11ea-87d0-0242ac130003"),
		PullSecretSet: false,
	}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}

	RunIgnitionConfigurationTests := func() {

		It("ignition_file_contains_url", func() {
			bm.ServiceBaseURL = "file://10.56.20.70:7878"
			text, err := bm.formatIgnitionFile(&cluster, installer.GenerateClusterISOParams{
				ImageCreateParams: &models.ImageCreateParams{},
			})

			Expect(err).Should(BeNil())
			Expect(text).Should(ContainSubstring(fmt.Sprintf("--url %s", bm.ServiceBaseURL)))
		})

		It("enabled_cert_verification", func() {
			bm.SkipCertVerification = false
			text, err := bm.formatIgnitionFile(&cluster, installer.GenerateClusterISOParams{
				ImageCreateParams: &models.ImageCreateParams{},
			})

			Expect(err).Should(BeNil())
			Expect(text).Should(ContainSubstring("--insecure=false"))
		})

		It("disabled_cert_verification", func() {
			bm.SkipCertVerification = true
			text, err := bm.formatIgnitionFile(&cluster, installer.GenerateClusterISOParams{
				ImageCreateParams: &models.ImageCreateParams{},
			})

			Expect(err).Should(BeNil())
			Expect(text).Should(ContainSubstring("--insecure=true"))
		})

		It("cert_verification_enabled_by_default", func() {
			text, err := bm.formatIgnitionFile(&cluster, installer.GenerateClusterISOParams{
				ImageCreateParams: &models.ImageCreateParams{},
			})

			Expect(err).Should(BeNil())
			Expect(text).Should(ContainSubstring("--insecure=false"))
		})

		It("ignition_file_contains_http_proxy", func() {
			bm.ServiceBaseURL = "file://10.56.20.70:7878"
			proxyCluster := cluster
			proxyCluster.HTTPProxy = "http://10.10.1.1:3128"
			proxyCluster.NoProxy = "quay.io"
			text, err := bm.formatIgnitionFile(&proxyCluster, installer.GenerateClusterISOParams{
				ImageCreateParams: &models.ImageCreateParams{},
			})

			Expect(err).Should(BeNil())
			Expect(text).Should(ContainSubstring(`"proxy": { "httpProxy": "http://10.10.1.1:3128", "noProxy": ["quay.io"] }`))
		})
	}

	Context("start with clean configuration", func() {
		BeforeEach(func() {
			bm = &bareMetalInventory{}
		})
		RunIgnitionConfigurationTests()
	})
})

var _ = Describe("RegisterHost", func() {
	var (
		bm                *bareMetalInventory
		cfg               Config
		db                *gorm.DB
		ctx               = context.Background()
		dbName            = "register_host_api"
		hostID            strfmt.UUID
		ctrl              *gomock.Controller
		mockClusterAPI    *cluster.MockAPI
		mockHostAPI       *host.MockAPI
		mockEventsHandler *events.MockHandler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClusterAPI = cluster.NewMockAPI(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockEventsHandler = events.NewMockHandler(ctrl)
		hostID = strfmt.UUID(uuid.New().String())
		db = common.PrepareTestDB(dbName)
		bm = NewBareMetalInventory(db, getTestLog(), mockHostAPI, mockClusterAPI, cfg, nil, mockEventsHandler, nil, nil, getTestAuthHandler())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("register host to none existing cluster", func() {
		reply := bm.RegisterHost(ctx, installer.RegisterHostParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                &hostID,
			},
		})
		apiErr, ok := reply.(*common.ApiErrorResponse)
		Expect(ok).Should(BeTrue())
		Expect(apiErr.StatusCode()).Should(Equal(int32(http.StatusNotFound)))
	})

	It("register_success", func() {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:     &clusterID,
				Status: swag.String(models.ClusterStatusInsufficient),
			},
		}
		Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())

		mockClusterAPI.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockHostAPI.EXPECT().RegisterHost(gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, h *models.Host) error {
				// validate that host is registered with auto-assign role
				Expect(h.Role).Should(Equal(models.HostRoleAutoAssign))
				return nil
			}).Times(1)
		mockHostAPI.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockEventsHandler.EXPECT().
			AddEvent(gomock.Any(), clusterID, &hostID, models.EventSeverityInfo, gomock.Any(), gomock.Any()).
			Times(1)

		reply := bm.RegisterHost(ctx, installer.RegisterHostParams{
			ClusterID: clusterID,
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                &hostID,
			},
		})
		_, ok := reply.(*installer.RegisterHostCreated)
		Expect(ok).Should(BeTrue())
	})
})

var _ = Describe("GetNextSteps", func() {
	var (
		bm                *bareMetalInventory
		cfg               Config
		db                *gorm.DB
		ctx               = context.Background()
		ctrl              *gomock.Controller
		mockHostApi       *host.MockAPI
		mockJob           *job.MockAPI
		mockEvents        *events.MockHandler
		defaultNextStepIn int64
		dbName            = "get_next_steps"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		defaultNextStepIn = 60
		db = common.PrepareTestDB(dbName)
		mockHostApi = host.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, nil, cfg, mockJob, mockEvents, nil, nil, getTestAuthHandler())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("get_next_steps_unknown_host", func() {
		clusterId := strToUUID(uuid.New().String())
		unregistered_hostID := strToUUID(uuid.New().String())

		generateReply := bm.GetNextSteps(ctx, installer.GetNextStepsParams{
			ClusterID: *clusterId,
			HostID:    *unregistered_hostID,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGetNextStepsNotFound()))
	})

	It("get_next_steps_success", func() {
		clusterId := strToUUID(uuid.New().String())
		hostId := strToUUID(uuid.New().String())
		host := models.Host{
			ID:        hostId,
			ClusterID: *clusterId,
			Status:    swag.String("discovering"),
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		var err error
		expectedStepsReply := models.Steps{NextInstructionSeconds: defaultNextStepIn, Instructions: []*models.Step{{StepType: models.StepTypeInventory},
			{StepType: models.StepTypeConnectivityCheck}}}
		mockHostApi.EXPECT().GetNextSteps(gomock.Any(), gomock.Any()).Return(expectedStepsReply, err)
		reply := bm.GetNextSteps(ctx, installer.GetNextStepsParams{
			ClusterID: *clusterId,
			HostID:    *hostId,
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewGetNextStepsOK()))
		stepsReply := reply.(*installer.GetNextStepsOK).Payload
		expectedStepsType := []models.StepType{models.StepTypeInventory, models.StepTypeConnectivityCheck}
		Expect(stepsReply.Instructions).To(HaveLen(len(expectedStepsType)))
		for i, step := range stepsReply.Instructions {
			Expect(step.StepType).Should(Equal(expectedStepsType[i]))
		}
	})
})

func makeFreeAddresses(network string, ips ...strfmt.IPv4) *models.FreeNetworkAddresses {
	return &models.FreeNetworkAddresses{
		FreeAddresses: ips,
		Network:       network,
	}
}

func makeFreeNetworksAddresses(elems ...*models.FreeNetworkAddresses) models.FreeNetworksAddresses {
	return models.FreeNetworksAddresses(elems)
}

func makeFreeNetworksAddressesStr(elems ...*models.FreeNetworkAddresses) string {
	toMarshal := models.FreeNetworksAddresses(elems)
	b, err := json.Marshal(&toMarshal)
	Expect(err).ToNot(HaveOccurred())
	return string(b)
}

var _ = Describe("PostStepReply", func() {
	var (
		bm             *bareMetalInventory
		cfg            Config
		db             *gorm.DB
		ctx            = context.Background()
		ctrl           *gomock.Controller
		mockClusterApi *cluster.MockAPI
		mockHostApi    *host.MockAPI
		mockJob        *job.MockAPI
		mockEvents     *events.MockHandler
		dbName         = "post_step_reply"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = common.PrepareTestDB(dbName)
		mockHostApi = host.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		mockClusterApi = cluster.NewMockAPI(ctrl)
		bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, mockClusterApi, cfg, mockJob, mockEvents, nil, nil, getTestAuthHandler())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("Free addresses", func() {
		var makeStepReply = func(clusterID, hostID strfmt.UUID, freeAddresses models.FreeNetworksAddresses) installer.PostStepReplyParams {
			b, _ := json.Marshal(&freeAddresses)
			return installer.PostStepReplyParams{
				ClusterID: clusterID,
				HostID:    hostID,
				Reply: &models.StepReply{
					Output:   string(b),
					StepType: models.StepTypeFreeNetworkAddresses,
				},
			}
		}

		It("free addresses success", func() {
			clusterId := strToUUID(uuid.New().String())
			hostId := strToUUID(uuid.New().String())
			host := models.Host{
				ID:        hostId,
				ClusterID: *clusterId,
				Status:    swag.String("discovering"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			toMarshal := makeFreeNetworksAddresses(makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.1"))
			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewPostStepReplyNoContent()))
			var h models.Host
			Expect(db.Take(&h, "cluster_id = ? and id = ?", clusterId.String(), hostId.String()).Error).ToNot(HaveOccurred())
			var f models.FreeNetworksAddresses
			Expect(json.Unmarshal([]byte(h.FreeAddresses), &f)).ToNot(HaveOccurred())
			Expect(&f).To(Equal(&toMarshal))
		})

		It("free addresses empty", func() {
			clusterId := strToUUID(uuid.New().String())
			hostId := strToUUID(uuid.New().String())
			host := models.Host{
				ID:        hostId,
				ClusterID: *clusterId,
				Status:    swag.String("discovering"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			toMarshal := makeFreeNetworksAddresses()
			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewPostStepReplyInternalServerError()))
			var h models.Host
			Expect(db.Take(&h, "cluster_id = ? and id = ?", clusterId.String(), hostId.String()).Error).ToNot(HaveOccurred())
			Expect(h.FreeAddresses).To(BeEmpty())
		})
	})

	Context("Dhcp allocation", func() {
		var (
			clusterId, hostId *strfmt.UUID
			makeStepReply     = func(clusterID, hostID strfmt.UUID, dhcpAllocationResponse *models.DhcpAllocationResponse) installer.PostStepReplyParams {
				b, err := json.Marshal(dhcpAllocationResponse)
				Expect(err).ToNot(HaveOccurred())
				return installer.PostStepReplyParams{
					ClusterID: clusterID,
					HostID:    hostID,
					Reply: &models.StepReply{
						Output:   string(b),
						StepType: models.StepTypeDhcpLeaseAllocate,
					},
				}
			}
			makeResponse = func(apiVipStr, ingressVipStr string) *models.DhcpAllocationResponse {
				apiVip := strfmt.IPv4(apiVipStr)
				ingressVip := strfmt.IPv4(ingressVipStr)
				ret := models.DhcpAllocationResponse{
					APIVipAddress:     &apiVip,
					IngressVipAddress: &ingressVip,
				}
				return &ret
			}
		)
		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())
			host := models.Host{
				ID:        hostId,
				ClusterID: *clusterId,
				Status:    swag.String("insufficient"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})
		It("Happy flow", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                 clusterId,
					VipDhcpAllocation:  swag.Bool(true),
					MachineNetworkCidr: "1.2.3.0/24",
					Status:             swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse("1.2.3.10", "1.2.3.11"))
			mockClusterApi.EXPECT().SetVips(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewPostStepReplyNoContent()))
		})
		It("DHCP not enabled", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                 clusterId,
					VipDhcpAllocation:  swag.Bool(false),
					MachineNetworkCidr: "1.2.3.0/24",
					Status:             swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse("1.2.3.10", "1.2.3.11"))
			reply := bm.PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewPostStepReplyInternalServerError()))
		})
		It("Bad ingress VIP", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                 clusterId,
					VipDhcpAllocation:  swag.Bool(true),
					MachineNetworkCidr: "1.2.3.0/24",
					Status:             swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse("1.2.3.10", "1.2.4.11"))
			reply := bm.PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewPostStepReplyInternalServerError()))
		})
		It("New IPs while in insufficient", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                 clusterId,
					VipDhcpAllocation:  swag.Bool(true),
					MachineNetworkCidr: "1.2.3.0/24",
					APIVip:             "1.2.3.20",
					IngressVip:         "1.2.3.11",
					Status:             swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse("1.2.3.10", "1.2.3.11"))
			mockClusterApi.EXPECT().SetVips(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewPostStepReplyNoContent()))
		})
		It("New IPs while in installing", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                 clusterId,
					VipDhcpAllocation:  swag.Bool(true),
					MachineNetworkCidr: "1.2.3.0/24",
					APIVip:             "1.2.3.20",
					IngressVip:         "1.2.3.11",
					Status:             swag.String(models.ClusterStatusInstalling),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			mockClusterApi.EXPECT().SetVips(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("Stam"))
			params := makeStepReply(*clusterId, *hostId, makeResponse("1.2.3.10", "1.2.3.11"))
			reply := bm.PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewPostStepReplyInternalServerError()))
		})
	})
})

var _ = Describe("GetFreeAddresses", func() {
	var (
		bm          *bareMetalInventory
		cfg         Config
		db          *gorm.DB
		ctx         = context.Background()
		ctrl        *gomock.Controller
		mockHostApi *host.MockAPI
		mockJob     *job.MockAPI
		mockEvents  *events.MockHandler
		dbName      = "get_free_addresses"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = common.PrepareTestDB(dbName)
		mockHostApi = host.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, nil, cfg, mockJob, mockEvents, nil, nil, getTestAuthHandler())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	var makeHost = func(clusterId *strfmt.UUID, freeAddresses, status string) *models.Host {
		hostId := strToUUID(uuid.New().String())
		ret := models.Host{
			ID:            hostId,
			ClusterID:     *clusterId,
			FreeAddresses: freeAddresses,
			Status:        &status,
		}
		Expect(db.Create(&ret).Error).ToNot(HaveOccurred())
		return &ret
	}

	var makeGetFreeAddressesParams = func(clusterID strfmt.UUID, network string) installer.GetFreeAddressesParams {
		return installer.GetFreeAddressesParams{
			ClusterID: clusterID,
			Network:   network,
		}
	}

	It("success", func() {
		clusterId := strToUUID(uuid.New().String())

		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.0/16", "10.0.10.1", "10.0.20.0", "10.0.9.250")), models.HostStatusInsufficient)
		params := makeGetFreeAddressesParams(*clusterId, "10.0.0.0/16")
		reply := bm.GetFreeAddresses(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewGetFreeAddressesOK()))
		actualReply := reply.(*installer.GetFreeAddressesOK)
		Expect(len(actualReply.Payload)).To(Equal(3))
		Expect(actualReply.Payload[0]).To(Equal(strfmt.IPv4("10.0.9.250")))
		Expect(actualReply.Payload[1]).To(Equal(strfmt.IPv4("10.0.10.1")))
		Expect(actualReply.Payload[2]).To(Equal(strfmt.IPv4("10.0.20.0")))
	})

	It("success with limit", func() {
		clusterId := strToUUID(uuid.New().String())

		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.0/16", "10.0.10.1", "10.0.20.0", "10.0.9.250")), models.HostStatusInsufficient)
		params := makeGetFreeAddressesParams(*clusterId, "10.0.0.0/16")
		params.Limit = swag.Int64(2)
		reply := bm.GetFreeAddresses(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewGetFreeAddressesOK()))
		actualReply := reply.(*installer.GetFreeAddressesOK)
		Expect(len(actualReply.Payload)).To(Equal(2))
		Expect(actualReply.Payload[0]).To(Equal(strfmt.IPv4("10.0.9.250")))
		Expect(actualReply.Payload[1]).To(Equal(strfmt.IPv4("10.0.10.1")))
	})

	It("success with limit and prefix", func() {
		clusterId := strToUUID(uuid.New().String())

		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.0/16", "10.0.10.1", "10.0.20.0", "10.0.9.250", "10.0.1.0")), models.HostStatusInsufficient)
		params := makeGetFreeAddressesParams(*clusterId, "10.0.0.0/16")
		params.Limit = swag.Int64(2)
		params.Prefix = swag.String("10.0.1")
		reply := bm.GetFreeAddresses(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewGetFreeAddressesOK()))
		actualReply := reply.(*installer.GetFreeAddressesOK)
		Expect(len(actualReply.Payload)).To(Equal(2))
		Expect(actualReply.Payload[0]).To(Equal(strfmt.IPv4("10.0.1.0")))
		Expect(actualReply.Payload[1]).To(Equal(strfmt.IPv4("10.0.10.1")))
	})

	It("one disconnected", func() {
		clusterId := strToUUID(uuid.New().String())

		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.1")), models.HostStatusInsufficient)
		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.2")), models.HostStatusKnown)
		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.0/24")), models.HostStatusDisconnected)
		params := makeGetFreeAddressesParams(*clusterId, "10.0.0.0/24")
		reply := bm.GetFreeAddresses(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewGetFreeAddressesOK()))
		actualReply := reply.(*installer.GetFreeAddressesOK)
		Expect(len(actualReply.Payload)).To(Equal(1))
		Expect(actualReply.Payload).To(ContainElement(strfmt.IPv4("10.0.0.0")))
	})

	It("empty result", func() {
		clusterId := strToUUID(uuid.New().String())

		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("192.168.0.0/24"),
			makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.1")), models.HostStatusInsufficient)
		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.2"),
			makeFreeAddresses("192.168.0.0/24")), models.HostStatusKnown)
		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.0/24", "10.0.0.1", "10.0.0.2")), models.HostStatusInsufficient)
		params := makeGetFreeAddressesParams(*clusterId, "10.0.0.0/24")
		reply := bm.GetFreeAddresses(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewGetFreeAddressesOK()))
		actualReply := reply.(*installer.GetFreeAddressesOK)
		Expect(actualReply.Payload).To(BeEmpty())
	})

	It("malformed", func() {
		clusterId := strToUUID(uuid.New().String())

		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("192.168.0.0/24"),
			makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.1")), models.HostStatusInsufficient)
		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.2"),
			makeFreeAddresses("192.168.0.0/24")), models.HostStatusKnown)
		_ = makeHost(clusterId, "blah ", models.HostStatusInsufficient)
		params := makeGetFreeAddressesParams(*clusterId, "10.0.0.0/24")
		reply := bm.GetFreeAddresses(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewGetFreeAddressesOK()))
		actualReply := reply.(*installer.GetFreeAddressesOK)
		Expect(len(actualReply.Payload)).To(Equal(1))
		Expect(actualReply.Payload).To(ContainElement(strfmt.IPv4("10.0.0.0")))
	})

	It("no matching  hosts", func() {
		clusterId := strToUUID(uuid.New().String())

		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("192.168.0.0/24"),
			makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.1")), models.HostStatusDisconnected)
		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.2"),
			makeFreeAddresses("192.168.0.0/24")), models.HostStatusDiscovering)
		_ = makeHost(clusterId, makeFreeNetworksAddressesStr(makeFreeAddresses("10.0.0.1/24", "10.0.0.0", "10.0.0.2")), models.HostStatusInstalling)
		params := makeGetFreeAddressesParams(*clusterId, "10.0.0.0/24")
		verifyApiError(bm.GetFreeAddresses(ctx, params), http.StatusNotFound)
	})
})

var _ = Describe("UpdateHostInstallProgress", func() {
	var (
		bm                   *bareMetalInventory
		cfg                  Config
		db                   *gorm.DB
		ctx                  = context.Background()
		ctrl                 *gomock.Controller
		mockJob              *job.MockAPI
		mockHostApi          *host.MockAPI
		mockEvents           *events.MockHandler
		defaultProgressStage models.HostStage
		dbName               = "update_host_install_progress"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = common.PrepareTestDB(dbName)
		mockHostApi = host.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, nil, cfg, mockJob, mockEvents, nil, nil, getTestAuthHandler())
		defaultProgressStage = "some progress"
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("host exists", func() {
		var (
			hostID         strfmt.UUID
			clusterID      strfmt.UUID
			progressParams *models.HostProgress
		)

		BeforeEach(func() {
			hostID = strfmt.UUID(uuid.New().String())
			clusterID = strfmt.UUID(uuid.New().String())
			progressParams = &models.HostProgress{
				CurrentStage: defaultProgressStage,
			}

			err := db.Create(&models.Host{
				ID:        &hostID,
				ClusterID: clusterID,
			}).Error
			Expect(err).ShouldNot(HaveOccurred())

		})

		It("success", func() {
			mockEvents.EXPECT().AddEvent(gomock.Any(), clusterID, &hostID, models.EventSeverityInfo, gomock.Any(), gomock.Any())
			mockHostApi.EXPECT().UpdateInstallProgress(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.UpdateHostInstallProgress(ctx, installer.UpdateHostInstallProgressParams{
				ClusterID:    clusterID,
				HostProgress: progressParams,
				HostID:       hostID,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressOK()))
		})

		It("update_failed", func() {
			mockHostApi.EXPECT().UpdateInstallProgress(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))
			reply := bm.UpdateHostInstallProgress(ctx, installer.UpdateHostInstallProgressParams{
				ClusterID:    clusterID,
				HostProgress: progressParams,
				HostID:       hostID,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressInternalServerError()))
		})
	})

	It("host_dont_exist", func() {
		reply := bm.UpdateHostInstallProgress(ctx, installer.UpdateHostInstallProgressParams{
			ClusterID: strfmt.UUID(uuid.New().String()),
			HostProgress: &models.HostProgress{
				CurrentStage: defaultProgressStage,
			},
			HostID: strfmt.UUID(uuid.New().String()),
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressNotFound()))
	})
})

var _ = Describe("cluster", func() {
	masterHostId1 := strfmt.UUID(uuid.New().String())
	masterHostId2 := strfmt.UUID(uuid.New().String())
	masterHostId3 := strfmt.UUID(uuid.New().String())

	var (
		bm             *bareMetalInventory
		cfg            Config
		db             *gorm.DB
		ctx            = context.Background()
		ctrl           *gomock.Controller
		mockHostApi    *host.MockAPI
		mockClusterApi *cluster.MockAPI
		mockS3Client   *s3wrapper.MockAPI
		mockKubeJob    *job.MockAPI
		mockLocalJob   *job.MockLocalJob
		clusterID      strfmt.UUID
		mockEvents     *events.MockHandler
		mockMetric     *metrics.MockAPI
		dbName         = "inventory_cluster"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = common.PrepareTestDB(dbName)
		mockClusterApi = cluster.NewMockAPI(ctrl)
		mockHostApi = host.NewMockAPI(ctrl)
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		mockEvents = events.NewMockHandler(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	addHost := func(hostId strfmt.UUID, role models.HostRole, state string, clusterId strfmt.UUID, inventory string, db *gorm.DB) models.Host {
		host := models.Host{
			ID:        &hostId,
			ClusterID: clusterId,
			Status:    swag.String(state),
			Role:      role,
			Inventory: inventory,
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		return host
	}

	mockClusterPrepareForInstallationSuccess := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().PrepareForInstallation(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}
	mockClusterPrepareForInstallationFailure := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().PrepareForInstallation(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.Errorf("error")).Times(1)
	}
	mockHostPrepareForInstallationSuccess := func(mockHostApi *host.MockAPI, times int) {
		mockHostApi.EXPECT().PrepareForInstallation(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(times)
	}
	mockHostPrepareForRefresh := func(mockHostApi *host.MockAPI) {
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
	mockClusterRefreshStatus := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	}
	mockHostPrepareForInstallationFailure := func(mockHostApi *host.MockAPI, times int) {
		mockHostApi.EXPECT().PrepareForInstallation(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.Errorf("error")).Times(times)
	}
	setDefaultInstall := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	}
	setIsReadyForInstallationTrue := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").AnyTimes()
	}
	setIsReadyForInstallationFalse := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(false, "cluster is not ready to install")
	}
	setDefaultGetMasterNodesIds := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, nil).AnyTimes()
	}
	setDefaultHostInstall := func(mockClusterApi *cluster.MockAPI, done chan int) {
		count := 0
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3).
			Do(func(ctx context.Context, h *models.Host, db *gorm.DB) {
				count += 1
				if count == 3 {
					done <- 1
				}
			})

	}
	setDefaultHostSetBootstrap := func(mockClusterApi *cluster.MockAPI) {
		mockHostApi.EXPECT().SetBootstrap(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
	setIgnitionGeneratorVersionSuccess := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().SetGeneratorVersion(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}
	setDefaultMetricInstallatioStarted := func(mockMetricApi *metrics.MockAPI) {
		mockMetricApi.EXPECT().InstallationStarted(gomock.Any()).AnyTimes()
	}
	mockHandlePreInstallationError := func(mockClusterApi *cluster.MockAPI, done chan int) {
		mockClusterApi.EXPECT().HandlePreInstallError(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).
			Do(func(ctx, c, err interface{}) { done <- 1 })
	}
	setCancelInstallationSuccess := func() {
		mockClusterApi.EXPECT().CancelInstallation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockHostApi.EXPECT().CancelInstallation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
	setCancelInstallationHostConflict := func() {
		mockClusterApi.EXPECT().CancelInstallation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockHostApi.EXPECT().CancelInstallation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.NewApiError(http.StatusConflict, nil)).Times(1)
	}
	setCancelInstallationInternalServerError := func() {
		mockClusterApi.EXPECT().CancelInstallation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.NewApiError(http.StatusInternalServerError, nil)).Times(1)
	}
	setResetClusterSuccess := func() {
		mockAbortInstallConfig(mockKubeJob, mockLocalJob)
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockClusterApi.EXPECT().ResetCluster(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockHostApi.EXPECT().ResetHost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
	setResetClusterConflict := func() {
		mockAbortInstallConfig(mockKubeJob, mockLocalJob)
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockClusterApi.EXPECT().ResetCluster(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.NewApiError(http.StatusConflict, nil)).Times(1)
	}
	setResetClusterInternalServerError := func() {
		mockAbortInstallConfig(mockKubeJob, mockLocalJob)
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockClusterApi.EXPECT().ResetCluster(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.NewApiError(http.StatusInternalServerError, nil)).Times(1)
	}
	mockAutoAssignFailed := func() {
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.Errorf("")).Times(1)
	}
	mockAutoAssignSuccess := func(times int) {
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(times)
	}
	mockClusterRefreshStatusSuccess := func() {
		mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, c *common.Cluster, db *gorm.DB) (*common.Cluster, error) {
				return c, nil
			})
	}
	mockClusterIsReadyForInstallationSuccess := func() {
		mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "").Times(1)
	}

	getInventoryStr := func(hostname, bootMode string, ipv4Addresses ...string) string {
		inventory := models.Inventory{
			Interfaces: []*models.Interface{
				{
					IPV4Addresses: append(make([]string, 0), ipv4Addresses...),
					MacAddress:    "some MAC address",
				},
			},
			Hostname: hostname,
			Boot:     &models.Boot{CurrentBootMode: bootMode},
		}
		ret, _ := json.Marshal(&inventory)
		return string(ret)
	}

	sortedHosts := func(arr []strfmt.UUID) []strfmt.UUID {
		sort.Slice(arr, func(i, j int) bool { return arr[i] < arr[j] })
		return arr
	}

	sortedNetworks := func(arr []*models.HostNetwork) []*models.HostNetwork {
		sort.Slice(arr, func(i, j int) bool { return arr[i].Cidr < arr[j].Cidr })
		return arr
	}

	RunClusterTests := func() {
		Context("Get", func() {
			{
				BeforeEach(func() {
					clusterID = strfmt.UUID(uuid.New().String())
					err := db.Create(&common.Cluster{Cluster: models.Cluster{
						ID:                 &clusterID,
						APIVip:             "10.11.12.13",
						IngressVip:         "10.11.12.14",
						MachineNetworkCidr: "10.11.0.0/16",
					}}).Error
					Expect(err).ShouldNot(HaveOccurred())

					addHost(masterHostId1, models.HostRoleMaster, "known", clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
					addHost(masterHostId2, models.HostRoleMaster, "known", clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
					addHost(masterHostId3, models.HostRoleMaster, "known", clusterID, getInventoryStr("hostname2", "bootMode", "1.2.3.6/24", "7.8.9.10/24"), db)
				})

				It("GetCluster", func() {
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(3) // Number of hosts
					reply := bm.GetCluster(ctx, installer.GetClusterParams{
						ClusterID: clusterID,
					})
					actual, ok := reply.(*installer.GetClusterOK)
					Expect(ok).To(BeTrue())
					Expect(actual.Payload.APIVip).To(BeEquivalentTo("10.11.12.13"))
					Expect(actual.Payload.IngressVip).To(BeEquivalentTo("10.11.12.14"))
					Expect(actual.Payload.MachineNetworkCidr).To(Equal("10.11.0.0/16"))
					expectedNetworks := sortedNetworks([]*models.HostNetwork{
						{
							Cidr: "1.2.3.0/24",
							HostIds: sortedHosts([]strfmt.UUID{
								masterHostId1,
								masterHostId2,
								masterHostId3,
							}),
						},
						{
							Cidr: "10.11.0.0/16",
							HostIds: sortedHosts([]strfmt.UUID{
								masterHostId1,
								masterHostId2,
							}),
						},
						{
							Cidr: "7.8.9.0/24",
							HostIds: []strfmt.UUID{
								masterHostId3,
							},
						},
					})
					actualNetworks := sortedNetworks(actual.Payload.HostNetworks)
					Expect(len(actualNetworks)).To(Equal(3))
					actualNetworks[0].HostIds = sortedHosts(actualNetworks[0].HostIds)
					actualNetworks[1].HostIds = sortedHosts(actualNetworks[1].HostIds)
					actualNetworks[2].HostIds = sortedHosts(actualNetworks[2].HostIds)
					Expect(actualNetworks).To(Equal(expectedNetworks))
				})
			}
		})
		Context("Update", func() {
			It("update_cluster_while_installing", func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())

				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(errors.Errorf("wrong state")).Times(1)

				apiVip := "8.8.8.8"
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						APIVip: &apiVip,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterConflict()))
			})

			It("Invalid pull-secret", func() {
				pullSecret := "asdfasfda"
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						PullSecret: &pullSecret,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterBadRequest()))
			})

			It("ssh key with newline", func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				sshKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDi8KHZYGyPQjECHwytquI3rmpgoUn6M+lkeOD2nEKvYElLE5mPIeqF0izJIl56u" +
					"ar2wda+3z107M9QkatE+dP4S9/Ltrlm+/ktAf4O6UoxNLUzv/TGHasb9g3Xkt8JTkohVzVK36622Sd8kLzEc61v1AonLWIADtpwq6/GvH" +
					"MAuPK2R/H0rdKhTokylKZLDdTqQ+KUFelI6RNIaUBjtVrwkx1j0htxN11DjBVuUyPT2O1ejWegtrM0T+4vXGEA3g3YfbT2k0YnEzjXXqng" +
					"qbXCYEJCZidp3pJLH/ilo4Y4BId/bx/bhzcbkZPeKlLwjR8g9sydce39bzPIQj+b7nlFv1Vot/77VNwkjXjYPUdUPu0d1PkFD9jKDOdB3f" +
					"AC61aG2a/8PFS08iBrKiMa48kn+hKXC4G4D5gj/QzIAgzWSl2tEzGQSoIVTucwOAL/jox2dmAa0RyKsnsHORppanuW4qD7KAcmas1GHrAq" +
					"IfNyDiU2JR50r1jCxj5H76QxIuM= root@ocp-edge34.lab.eng.tlv2.redhat.com gggggggg fdddddddddddddddddddddddd" +
					"dddddddddddddddd"
				sshKeyWithNewLine := sshKey + " \n"

				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						SSHPublicKey: &sshKeyWithNewLine,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
				var cluster common.Cluster
				err = db.First(&cluster, "id = ?", clusterID).Error
				Expect(err).ShouldNot(HaveOccurred())
				Expect(cluster.SSHPublicKey).Should(Equal(sshKey))
			})

			It("empty pull-secret", func() {
				pullSecret := ""
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						PullSecret: &pullSecret,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterNotFound()))
			})

			Context("Update Proxy", func() {
				const emptyProxyHash = "d41d8cd98f00b204e9800998ecf8427e"
				BeforeEach(func() {
					clusterID = strfmt.UUID(uuid.New().String())
					err := db.Create(&common.Cluster{
						Cluster: models.Cluster{
							ID: &clusterID,
						},
						ProxyHash: emptyProxyHash}).Error
					Expect(err).ShouldNot(HaveOccurred())
					mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
				})

				updateCluster := func(httpProxy, httpsProxy, noProxy string) *common.Cluster {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							HTTPProxy:  &httpProxy,
							HTTPSProxy: &httpsProxy,
							NoProxy:    &noProxy,
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					var cluster common.Cluster
					err := db.First(&cluster, "id = ?", clusterID).Error
					Expect(err).ShouldNot(HaveOccurred())
					return &cluster
				}

				It("set an empty proxy", func() {
					cluster := updateCluster("", "", "")
					Expect(cluster.ProxyHash).To(Equal(emptyProxyHash))
				})

				It("set a valid proxy", func() {
					mockEvents.EXPECT().AddEvent(gomock.Any(), clusterID, nil, models.EventSeverityInfo, "Proxy settings changed", gomock.Any())
					cluster := updateCluster("http://proxy.proxy", "", "proxy.proxy")

					// ProxyHash shouldn't be changed when proxy is updated, only when generating new ISO
					Expect(cluster.ProxyHash).To(Equal(emptyProxyHash))
				})
			})

			Context("Hostname", func() {
				BeforeEach(func() {
					clusterID = strfmt.UUID(uuid.New().String())
					err := db.Create(&common.Cluster{Cluster: models.Cluster{
						ID: &clusterID,
					}}).Error
					Expect(err).ShouldNot(HaveOccurred())
					addHost(masterHostId1, models.HostRoleMaster, "known", clusterID, getInventoryStr("1.2.3.4/24", "10.11.50.90/16"), db)
					err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: clusterID}).UpdateColumn("free_addresses",
						makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16"))).Error
					Expect(err).ToNot(HaveOccurred())
					mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
				})
				It("Valid hostname", func() {
					mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Cluster{}, nil).Times(1)
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
								{
									Hostname: "a.b.c",
									ID:       masterHostId1,
								},
							},
						}})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
				})
				It("Valid splitted hostname", func() {
					mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Cluster{}, nil).Times(1)
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
								{
									Hostname: "abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.123456789",
									ID:       masterHostId1,
								},
							},
						}})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
				})
				It("Too long part", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
								{
									Hostname: "abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij",
									ID:       masterHostId1,
								},
							},
						}})
					Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
				})
				It("Too long", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
								{
									Hostname: "abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.1234567890",
									ID:       masterHostId1,
								},
							},
						}})
					Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
				})
				It("Preceding hyphen", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							HostsNames: []*models.ClusterUpdateParamsHostsNamesItems0{
								{
									Hostname: "-abc",
									ID:       masterHostId1,
								},
							},
						}})
					Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
				})
			})

			Context("Update Network", func() {
				BeforeEach(func() {
					clusterID = strfmt.UUID(uuid.New().String())
					err := db.Create(&common.Cluster{Cluster: models.Cluster{
						ID: &clusterID,
					}}).Error
					Expect(err).ShouldNot(HaveOccurred())
					addHost(masterHostId1, models.HostRoleMaster, "known", clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
					addHost(masterHostId2, models.HostRoleMaster, "known", clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
					addHost(masterHostId3, models.HostRoleMaster, "known", clusterID, getInventoryStr("hostname2", "bootMode", "1.2.3.6/24", "7.8.9.10/24"), db)
					err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: clusterID}).UpdateColumn("free_addresses",
						makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16"))).Error
					Expect(err).ToNot(HaveOccurred())
					mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
				})
				Context("Non DHCP", func() {

					It("No machine network", func() {
						apiVip := "8.8.8.8"
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip: &apiVip,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})
					It("Api and ingress mismatch", func() {
						apiVip := "10.11.12.15"
						ingressVip := "1.2.3.20"
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:     &apiVip,
								IngressVip: &ingressVip,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})
					It("Same api and ingress", func() {
						apiVip := "10.11.12.15"
						ingressVip := apiVip
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:     &apiVip,
								IngressVip: &ingressVip,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})
					It("Update success", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(3) // Number of hosts
						mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
						mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:     &apiVip,
								IngressVip: &ingressVip,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
						actual := reply.(*installer.UpdateClusterCreated)
						Expect(actual.Payload.APIVip).To(Equal(apiVip))
						Expect(actual.Payload.IngressVip).To(Equal(ingressVip))
						Expect(actual.Payload.MachineNetworkCidr).To(Equal("10.11.0.0/16"))
						expectedNetworks := sortedNetworks([]*models.HostNetwork{
							{
								Cidr: "1.2.3.0/24",
								HostIds: sortedHosts([]strfmt.UUID{
									masterHostId1,
									masterHostId2,
									masterHostId3,
								}),
							},
							{
								Cidr: "10.11.0.0/16",
								HostIds: sortedHosts([]strfmt.UUID{
									masterHostId1,
									masterHostId2,
								}),
							},
							{
								Cidr: "7.8.9.0/24",
								HostIds: []strfmt.UUID{
									masterHostId3,
								},
							},
						})
						actualNetworks := sortedNetworks(actual.Payload.HostNetworks)
						Expect(len(actualNetworks)).To(Equal(3))
						actualNetworks[0].HostIds = sortedHosts(actualNetworks[0].HostIds)
						actualNetworks[1].HostIds = sortedHosts(actualNetworks[1].HostIds)
						actualNetworks[2].HostIds = sortedHosts(actualNetworks[2].HostIds)
						Expect(actualNetworks).To(Equal(expectedNetworks))
					})
					It("Machine network CIDR in non dhcp", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:             &apiVip,
								IngressVip:         &ingressVip,
								MachineNetworkCidr: swag.String("10.12.0.0/16"),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})
				})
				Context("Advanced networking validations", func() {

					It("Update success", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(3) // Number of hosts
						mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
						mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:                   &apiVip,
								IngressVip:               &ingressVip,
								ClusterNetworkCidr:       swag.String("192.168.0.0/21"),
								ServiceNetworkCidr:       swag.String("193.168.5.0/24"),
								ClusterNetworkHostPrefix: swag.Int64(23),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
						actual := reply.(*installer.UpdateClusterCreated)
						Expect(actual.Payload.APIVip).To(Equal(apiVip))
						Expect(actual.Payload.IngressVip).To(Equal(ingressVip))
						Expect(actual.Payload.MachineNetworkCidr).To(Equal("10.11.0.0/16"))
						Expect(actual.Payload.ClusterNetworkCidr).To(Equal("192.168.0.0/21"))
						Expect(actual.Payload.ServiceNetworkCidr).To(Equal("193.168.5.0/24"))
						expectedNetworks := sortedNetworks([]*models.HostNetwork{
							{
								Cidr: "1.2.3.0/24",
								HostIds: sortedHosts([]strfmt.UUID{
									masterHostId1,
									masterHostId2,
									masterHostId3,
								}),
							},
							{
								Cidr: "10.11.0.0/16",
								HostIds: sortedHosts([]strfmt.UUID{
									masterHostId1,
									masterHostId2,
								}),
							},
							{
								Cidr: "7.8.9.0/24",
								HostIds: []strfmt.UUID{
									masterHostId3,
								},
							},
						})
						actualNetworks := sortedNetworks(actual.Payload.HostNetworks)
						Expect(len(actualNetworks)).To(Equal(3))
						actualNetworks[0].HostIds = sortedHosts(actualNetworks[0].HostIds)
						actualNetworks[1].HostIds = sortedHosts(actualNetworks[1].HostIds)
						actualNetworks[2].HostIds = sortedHosts(actualNetworks[2].HostIds)
						Expect(actualNetworks).To(Equal(expectedNetworks))
					})
					It("Overlapping", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:                   &apiVip,
								IngressVip:               &ingressVip,
								ClusterNetworkCidr:       swag.String("192.168.0.0/21"),
								ServiceNetworkCidr:       swag.String("192.168.4.0/23"),
								ClusterNetworkHostPrefix: swag.Int64(23),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})
					It("Prefix out of range", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:                   &apiVip,
								IngressVip:               &ingressVip,
								ClusterNetworkCidr:       swag.String("192.168.5.0/24"),
								ServiceNetworkCidr:       swag.String("193.168.4.0/23"),
								ClusterNetworkHostPrefix: swag.Int64(26),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})
					It("Subnet prefix out of range", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:                   &apiVip,
								IngressVip:               &ingressVip,
								ClusterNetworkCidr:       swag.String("192.168.0.0/23"),
								ServiceNetworkCidr:       swag.String("193.168.4.0/27"),
								ClusterNetworkHostPrefix: swag.Int64(25),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})
					It("OK", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(3) // Number of hosts
						mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
						mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:                   &apiVip,
								IngressVip:               &ingressVip,
								ClusterNetworkCidr:       swag.String("192.168.0.0/23"),
								ServiceNetworkCidr:       swag.String("193.168.4.0/25"),
								ClusterNetworkHostPrefix: swag.Int64(25),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					})
					It("Bad subnet", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:                   &apiVip,
								IngressVip:               &ingressVip,
								ClusterNetworkCidr:       swag.String("1.168.0.0/23"),
								ServiceNetworkCidr:       swag.String("193.168.4.0/1"),
								ClusterNetworkHostPrefix: swag.Int64(23),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})
					It("Not enough addresses", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:                   &apiVip,
								IngressVip:               &ingressVip,
								ClusterNetworkCidr:       swag.String("192.168.0.0/23"),
								ServiceNetworkCidr:       swag.String("193.168.4.0/25"),
								ClusterNetworkHostPrefix: swag.Int64(24),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})
				})
				Context("DHCP", func() {
					It("Vips in DHCP", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:             &apiVip,
								IngressVip:         &ingressVip,
								MachineNetworkCidr: swag.String("10.11.0.0/16"),
								VipDhcpAllocation:  swag.Bool(true),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})

					It("Success in DHCP", func() {
						apiVip := "10.11.12.15"
						ingressVip := "10.11.12.16"
						mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(9)
						mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(9)
						mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(3)
						mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(2)
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:     &apiVip,
								IngressVip: &ingressVip,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
						actual := reply.(*installer.UpdateClusterCreated)
						Expect(actual.Payload.APIVip).To(Equal(apiVip))
						Expect(actual.Payload.IngressVip).To(Equal(ingressVip))
						Expect(actual.Payload.MachineNetworkCidr).To(Equal("10.11.0.0/16"))
						reply = bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								MachineNetworkCidr: swag.String("1.2.3.0/24"),
								VipDhcpAllocation:  swag.Bool(true),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
						actual = reply.(*installer.UpdateClusterCreated)
						Expect(actual.Payload.APIVip).To(BeEmpty())
						Expect(actual.Payload.IngressVip).To(BeEmpty())
						Expect(actual.Payload.MachineNetworkCidr).To(Equal("1.2.3.0/24"))
						expectedNetworks := sortedNetworks([]*models.HostNetwork{
							{
								Cidr: "1.2.3.0/24",
								HostIds: sortedHosts([]strfmt.UUID{
									masterHostId1,
									masterHostId2,
									masterHostId3,
								}),
							},
							{
								Cidr: "10.11.0.0/16",
								HostIds: sortedHosts([]strfmt.UUID{
									masterHostId1,
									masterHostId2,
								}),
							},
							{
								Cidr: "7.8.9.0/24",
								HostIds: []strfmt.UUID{
									masterHostId3,
								},
							},
						})
						actualNetworks := sortedNetworks(actual.Payload.HostNetworks)
						Expect(len(actualNetworks)).To(Equal(3))
						actualNetworks[0].HostIds = sortedHosts(actualNetworks[0].HostIds)
						actualNetworks[1].HostIds = sortedHosts(actualNetworks[1].HostIds)
						actualNetworks[2].HostIds = sortedHosts(actualNetworks[2].HostIds)
						Expect(actualNetworks).To(Equal(expectedNetworks))
						reply = bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:            &apiVip,
								IngressVip:        &ingressVip,
								VipDhcpAllocation: swag.Bool(false),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
						actual = reply.(*installer.UpdateClusterCreated)
						Expect(actual.Payload.APIVip).To(Equal(apiVip))
						Expect(actual.Payload.IngressVip).To(Equal(ingressVip))
						Expect(actual.Payload.MachineNetworkCidr).To(Equal("10.11.0.0/16"))
					})
					It("DHCP non existent network", func() {
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								MachineNetworkCidr: swag.String("10.13.0.0/16"),
								VipDhcpAllocation:  swag.Bool(true),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
						Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					})

				})
			})
		})

		Context("Install", func() {
			var DoneChannel chan int

			waitForDoneChannel := func() {
				select {
				case <-DoneChannel:
					break
				case <-time.After(1 * time.Second):
					panic("not all api calls where made")
				}
			}

			BeforeEach(func() {
				DoneChannel = make(chan int)
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID:                 &clusterID,
					APIVip:             "10.11.12.13",
					IngressVip:         "10.11.20.50",
					MachineNetworkCidr: "10.11.0.0/16",
					Status:             swag.String(models.ClusterStatusReady),
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())

				addHost(masterHostId1, models.HostRoleMaster, "known", clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
				addHost(masterHostId2, models.HostRoleMaster, "known", clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
				addHost(masterHostId3, models.HostRoleMaster, "known", clusterID, getInventoryStr("hostname2", "bootMode", "10.11.200.180/16"), db)
				err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: clusterID}).UpdateColumn("free_addresses",
					makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16", "10.11.12.13", "10.11.20.50"))).Error
				Expect(err).ToNot(HaveOccurred())
			})

			It("success", func() {
				mockAutoAssignSuccess(3)
				mockClusterRefreshStatusSuccess()
				mockClusterIsReadyForInstallationSuccess()
				mockGenerateInstallConfigSuccess(mockKubeJob, mockLocalJob, 1)
				mockClusterPrepareForInstallationSuccess(mockClusterApi)
				mockHostPrepareForRefresh(mockHostApi)
				mockHostPrepareForInstallationSuccess(mockHostApi, 3)
				setIgnitionGeneratorVersionSuccess(mockClusterApi)
				setDefaultInstall(mockClusterApi)
				setDefaultGetMasterNodesIds(mockClusterApi)
				setDefaultHostSetBootstrap(mockClusterApi)
				setDefaultHostInstall(mockClusterApi, DoneChannel)
				setDefaultMetricInstallatioStarted(mockMetric)
				setIsReadyForInstallationTrue(mockClusterApi)
				mockClusterRefreshStatus(mockClusterApi)
				setIsReadyForInstallationTrue(mockClusterApi)

				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: clusterID,
				})

				Expect(reply).Should(BeAssignableToTypeOf(installer.NewInstallClusterAccepted()))
				waitForDoneChannel()
			})

			It("cluster doesn't exists", func() {
				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: strfmt.UUID(uuid.New().String()),
				})
				verifyApiError(reply, http.StatusNotFound)
			})

			It("failed to auto-assign role", func() {
				mockAutoAssignFailed()
				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: clusterID,
				})
				verifyApiError(reply, http.StatusInternalServerError)
			})

			It("failed to prepare cluster", func() {
				mockAutoAssignSuccess(3)
				mockClusterRefreshStatusSuccess()
				mockClusterIsReadyForInstallationSuccess()
				// validations
				mockHostPrepareForRefresh(mockHostApi)
				setDefaultGetMasterNodesIds(mockClusterApi)
				// sync prepare for installation
				mockClusterPrepareForInstallationFailure(mockClusterApi)
				mockClusterRefreshStatus(mockClusterApi)
				setIsReadyForInstallationTrue(mockClusterApi)

				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: clusterID,
				})
				verifyApiError(reply, http.StatusInternalServerError)
			})

			It("failed to prepare host", func() {
				mockAutoAssignSuccess(3)
				mockClusterRefreshStatusSuccess()
				mockClusterIsReadyForInstallationSuccess()
				// validations
				mockHostPrepareForRefresh(mockHostApi)
				setDefaultGetMasterNodesIds(mockClusterApi)
				// sync prepare for installation
				mockClusterPrepareForInstallationSuccess(mockClusterApi)
				mockHostPrepareForInstallationSuccess(mockHostApi, 2)
				mockHostPrepareForInstallationFailure(mockHostApi, 1)
				mockClusterRefreshStatus(mockClusterApi)
				setIsReadyForInstallationTrue(mockClusterApi)

				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: clusterID,
				})
				verifyApiError(reply, http.StatusInternalServerError)
			})

			It("cluster is not ready to install", func() {
				mockAutoAssignSuccess(3)
				mockClusterRefreshStatusSuccess()
				mockHostPrepareForRefresh(mockHostApi)
				mockClusterRefreshStatus(mockClusterApi)
				setIsReadyForInstallationFalse(mockClusterApi)

				Expect(db.Model(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).UpdateColumn("status", "insufficient").Error).To(Not(HaveOccurred()))
				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: clusterID,
				})
				verifyApiError(reply, http.StatusConflict)
			})

			It("cluster failed to update", func() {
				mockAutoAssignSuccess(3)
				mockClusterRefreshStatusSuccess()
				mockClusterIsReadyForInstallationSuccess()
				mockHostPrepareForRefresh(mockHostApi)
				mockGenerateInstallConfigSuccess(mockKubeJob, mockLocalJob, 1)
				mockHostPrepareForRefresh(mockHostApi)
				mockClusterPrepareForInstallationSuccess(mockClusterApi)
				mockHostPrepareForInstallationSuccess(mockHostApi, 3)
				setIgnitionGeneratorVersionSuccess(mockClusterApi)
				mockHandlePreInstallationError(mockClusterApi, DoneChannel)
				setDefaultGetMasterNodesIds(mockClusterApi)
				mockClusterApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.Errorf("cluster has a error"))
				mockClusterRefreshStatus(mockClusterApi)
				setIsReadyForInstallationTrue(mockClusterApi)
				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: clusterID,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewInstallClusterAccepted()))
				waitForDoneChannel()
			})

			It("host failed to install", func() {
				mockAutoAssignSuccess(3)
				mockClusterRefreshStatusSuccess()
				mockClusterIsReadyForInstallationSuccess()
				mockHostPrepareForRefresh(mockHostApi)
				mockGenerateInstallConfigSuccess(mockKubeJob, mockLocalJob, 1)
				mockClusterPrepareForInstallationSuccess(mockClusterApi)
				mockHostPrepareForInstallationSuccess(mockHostApi, 3)
				setDefaultInstall(mockClusterApi)
				setDefaultGetMasterNodesIds(mockClusterApi)
				setIgnitionGeneratorVersionSuccess(mockClusterApi)
				setIsReadyForInstallationTrue(mockClusterApi)

				mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(errors.Errorf("host has a error")).AnyTimes()
				setDefaultHostSetBootstrap(mockClusterApi)
				mockHandlePreInstallationError(mockClusterApi, DoneChannel)
				mockClusterRefreshStatus(mockClusterApi)
				setIsReadyForInstallationTrue(mockClusterApi)

				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: clusterID,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewInstallClusterAccepted()))
				waitForDoneChannel()
			})

			It("list of masters for setting bootstrap return empty list", func() {
				mockAutoAssignSuccess(3)
				mockClusterRefreshStatusSuccess()
				mockClusterIsReadyForInstallationSuccess()
				mockHostPrepareForRefresh(mockHostApi)
				mockGenerateInstallConfigSuccess(mockKubeJob, mockLocalJob, 1)
				mockClusterPrepareForInstallationSuccess(mockClusterApi)
				mockHostPrepareForInstallationSuccess(mockHostApi, 3)
				setDefaultInstall(mockClusterApi)
				setIgnitionGeneratorVersionSuccess(mockClusterApi)
				mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
					Return([]*strfmt.UUID{}, nil).Times(1)
				mockHandlePreInstallationError(mockClusterApi, DoneChannel)
				mockClusterRefreshStatus(mockClusterApi)
				setIsReadyForInstallationTrue(mockClusterApi)

				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: clusterID,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewInstallClusterAccepted()))
				waitForDoneChannel()
			})

			It("GetMasterNodesIds fails in the go routine", func() {
				mockGenerateInstallConfigSuccess(mockKubeJob, mockLocalJob, 1)
				setIgnitionGeneratorVersionSuccess(mockClusterApi)
				mockHandlePreInstallationError(mockClusterApi, DoneChannel)
				setDefaultInstall(mockClusterApi)
				mockAutoAssignSuccess(3)
				mockClusterRefreshStatusSuccess()
				mockClusterIsReadyForInstallationSuccess()
				mockHostPrepareForRefresh(mockHostApi)
				mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
					Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, errors.Errorf("nop"))
				mockClusterRefreshStatus(mockClusterApi)
				mockClusterPrepareForInstallationSuccess(mockClusterApi)
				mockHostPrepareForInstallationSuccess(mockHostApi, 3)
				setIsReadyForInstallationTrue(mockClusterApi)

				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: clusterID,
				})

				Expect(reply).Should(BeAssignableToTypeOf(installer.NewInstallClusterAccepted()))
				waitForDoneChannel()
			})

			It("GetMasterNodesIds returns empty list", func() {
				mockGenerateInstallConfigSuccess(mockKubeJob, mockLocalJob, 1)
				mockClusterPrepareForInstallationSuccess(mockClusterApi)
				mockHostPrepareForInstallationSuccess(mockHostApi, 3)
				setIgnitionGeneratorVersionSuccess(mockClusterApi)
				mockHandlePreInstallationError(mockClusterApi, DoneChannel)
				setDefaultInstall(mockClusterApi)
				mockAutoAssignSuccess(3)
				mockClusterRefreshStatusSuccess()
				mockClusterIsReadyForInstallationSuccess()
				mockHostPrepareForRefresh(mockHostApi)
				mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
					Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, errors.Errorf("nop"))
				mockClusterRefreshStatus(mockClusterApi)
				setIsReadyForInstallationTrue(mockClusterApi)

				reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
					ClusterID: clusterID,
				})

				Expect(reply).Should(BeAssignableToTypeOf(installer.NewInstallClusterAccepted()))
				waitForDoneChannel()
			})

			It("get DNS domain success", func() {
				bm.Config.BaseDNSDomains = map[string]string{
					"dns.example.com": "abc/route53",
				}
				dnsDomain, err := bm.getDNSDomain("test-cluster", "dns.example.com")
				Expect(err).NotTo(HaveOccurred())
				Expect(dnsDomain.ID).Should(Equal("abc"))
				Expect(dnsDomain.Provider).Should(Equal("route53"))
				Expect(dnsDomain.APIDomainName).Should(Equal("api.test-cluster.dns.example.com"))
				Expect(dnsDomain.IngressDomainName).Should(Equal("*.apps.test-cluster.dns.example.com"))
			})
			It("get DNS domain invalid", func() {
				bm.Config.BaseDNSDomains = map[string]string{
					"dns.example.com": "abc",
				}
				_, err := bm.getDNSDomain("test-cluster", "dns.example.com")
				Expect(err).To(HaveOccurred())
			})
			It("get DNS domain undefined", func() {
				dnsDomain, err := bm.getDNSDomain("test-cluster", "dns.example.com")
				Expect(err).NotTo(HaveOccurred())
				Expect(dnsDomain).Should(BeNil())
			})

			Context("CancelInstallation", func() {
				BeforeEach(func() {
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
				})
				It("cancel installation success", func() {
					setCancelInstallationSuccess()

					cancelReply := bm.CancelInstallation(ctx, installer.CancelInstallationParams{
						ClusterID: clusterID,
					})
					Expect(cancelReply).Should(BeAssignableToTypeOf(installer.NewCancelInstallationAccepted()))
				})
				It("cancel installation conflict", func() {
					setCancelInstallationHostConflict()

					cancelReply := bm.CancelInstallation(ctx, installer.CancelInstallationParams{
						ClusterID: clusterID,
					})

					verifyApiError(cancelReply, http.StatusConflict)
				})
				It("cancel installation internal error", func() {
					setCancelInstallationInternalServerError()

					cancelReply := bm.CancelInstallation(ctx, installer.CancelInstallationParams{
						ClusterID: clusterID,
					})

					verifyApiError(cancelReply, http.StatusInternalServerError)
				})
			})

			Context("reset cluster", func() {
				BeforeEach(func() {
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
				})
				It("reset installation success", func() {
					setResetClusterSuccess()

					resetReply := bm.ResetCluster(ctx, installer.ResetClusterParams{
						ClusterID: clusterID,
					})
					Expect(resetReply).Should(BeAssignableToTypeOf(installer.NewResetClusterAccepted()))
				})
				It("reset cluster conflict", func() {
					setResetClusterConflict()

					cancelReply := bm.ResetCluster(ctx, installer.ResetClusterParams{
						ClusterID: clusterID,
					})

					verifyApiError(cancelReply, http.StatusConflict)
				})
				It("reset cluster internal error", func() {
					setResetClusterInternalServerError()

					cancelReply := bm.ResetCluster(ctx, installer.ResetClusterParams{
						ClusterID: clusterID,
					})

					verifyApiError(cancelReply, http.StatusInternalServerError)
				})
			})

			Context("complete installation", func() {
				success := true
				errorInfo := "dummy"
				It("complete success", func() {
					mockClusterApi.EXPECT().CompleteInstallation(ctx, gomock.Any(), success, errorInfo).Return(nil).Times(1)
					reply := bm.CompleteInstallation(ctx, installer.CompleteInstallationParams{
						ClusterID:        clusterID,
						CompletionParams: &models.CompletionParams{ErrorInfo: errorInfo, IsSuccess: &success},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewCompleteInstallationAccepted()))
				})
				It("complete bad request", func() {
					mockClusterApi.EXPECT().CompleteInstallation(ctx, gomock.Any(), success, errorInfo).Return(common.NewApiError(http.StatusBadRequest, nil)).Times(1)

					reply := bm.CompleteInstallation(ctx, installer.CompleteInstallationParams{
						ClusterID:        clusterID,
						CompletionParams: &models.CompletionParams{ErrorInfo: errorInfo, IsSuccess: &success},
					})

					verifyApiError(reply, http.StatusBadRequest)
				})
			})

			AfterEach(func() {
				close(DoneChannel)
			})
		})
	}

	Context("when kube job is used as generator", func() {
		BeforeEach(func() {
			mockKubeJob = job.NewMockAPI(ctrl)
			bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, mockClusterApi, cfg, mockKubeJob, mockEvents, mockS3Client, mockMetric, getTestAuthHandler())
		})
		RunClusterTests()
	})

	Context("when local job is used as generator", func() {
		BeforeEach(func() {
			mockLocalJob = job.NewMockLocalJob(ctrl)
			bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, mockClusterApi, cfg, mockLocalJob, mockEvents, mockS3Client, mockMetric, getTestAuthHandler())
		})
		RunClusterTests()
	})
})

var _ = Describe("KubeConfig download", func() {

	var (
		bm           *bareMetalInventory
		cfg          Config
		db           *gorm.DB
		ctx          = context.Background()
		ctrl         *gomock.Controller
		mockS3Client *s3wrapper.MockAPI
		clusterID    strfmt.UUID
		c            common.Cluster
		mockJob      *job.MockAPI
		clusterApi   cluster.API
		dbName       = "kubeconfig_download"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = common.PrepareTestDB(dbName)
		clusterID = strfmt.UUID(uuid.New().String())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		clusterApi = cluster.NewManager(cluster.Config{}, getTestLog().WithField("pkg", "cluster-monitor"),
			db, nil, nil, nil, nil)

		bm = NewBareMetalInventory(db, getTestLog(), nil, clusterApi, cfg, mockJob, nil, mockS3Client, nil, getTestAuthHandler())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &clusterID,
			APIVip: "10.11.12.13",
		}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("kubeconfig presigned backend not aws", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(false)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  kubeconfig,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
	})
	It("kubeconfig presigned cluster is not in installed state", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  kubeconfig,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
	})
	It("kubeconfig presigned happy flow", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		fileName := fmt.Sprintf("%s/%s", clusterID, kubeconfig)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, gomock.Any()).Return("url", nil)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  kubeconfig,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.GetPresignedForClusterFilesOK{}))
		replyPayload := generateReply.(*installer.GetPresignedForClusterFilesOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})

	It("kubeconfig download no cluster id", func() {
		clusterId := strToUUID(uuid.New().String())
		generateReply := bm.DownloadClusterKubeconfig(ctx, installer.DownloadClusterKubeconfigParams{
			ClusterID: *clusterId,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusNotFound)))
	})
	It("kubeconfig download cluster is not in installed state", func() {
		generateReply := bm.DownloadClusterKubeconfig(ctx, installer.DownloadClusterKubeconfigParams{
			ClusterID: clusterID,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
	})
	It("kubeconfig download s3download failure", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		fileName := fmt.Sprintf("%s/%s", clusterID, kubeconfig)
		mockS3Client.EXPECT().Download(ctx, fileName).Return(nil, int64(0), errors.Errorf("dummy"))
		generateReply := bm.DownloadClusterKubeconfig(ctx, installer.DownloadClusterKubeconfigParams{
			ClusterID: clusterID,
		})
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
	})
	It("kubeconfig download happy flow", func() {
		status := ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		fileName := fmt.Sprintf("%s/%s", clusterID, kubeconfig)
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.DownloadClusterKubeconfig(ctx, installer.DownloadClusterKubeconfigParams{
			ClusterID: clusterID,
		})
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewDownloadClusterKubeconfigOK().WithPayload(r), kubeconfig, 4)))
	})
})

var _ = Describe("UploadClusterIngressCert test", func() {

	var (
		bm                  *bareMetalInventory
		cfg                 Config
		db                  *gorm.DB
		ctx                 = context.Background()
		ctrl                *gomock.Controller
		mockS3Client        *s3wrapper.MockAPI
		clusterID           strfmt.UUID
		c                   common.Cluster
		ingressCa           models.IngressCertParams
		kubeconfigFile      *os.File
		kubeconfigNoingress string
		kubeconfigObject    string
		mockJob             *job.MockAPI
		clusterApi          cluster.API
		dbName              = "upload_cluster_ingress_cert"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = common.PrepareTestDB(dbName)
		ingressCa = "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
			"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
			"MQswCQYDVQQHDAJkZDELMAkGA1UECgwCZGQxCzAJBgNVBAsMAmRkMQswCQYDVQQDDAJkZDERMA8GCSqGSIb3DQEJARYCZGQwHhcNMjAwNTI1MTYwNTAwWhcNMzA" +
			"wNTIzMTYwNTAwWjBhMQswCQYDVQQGEwJpczELMAkGA1UECAwCZGQxCzAJBgNVBAcMAmRkMQswCQYDVQQKDAJkZDELMAkGA1UECwwCZGQxCzAJBgNVBAMMAmRkMREwDwYJKoZIh" +
			"vcNAQkBFgJkZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAML63CXkBb+lvrJKfdfYBHLDYfuaC6exCSqASUAosJWWrfyDiDMUbmfs06PLKyv7N8efDhza74ov0EQJ" +
			"NRhMNaCE+A0ceq6ZXmmMswUYFdLAy8K2VMz5mroBFX8sj5PWVr6rDJ2ckBaFKWBB8NFmiK7MTWSIF9n8M107/9a0QURCvThUYu+sguzbsLODFtXUxG5rtTVKBVcPZvEfRky2Tkt4AySFS" +
			"mkO6Kf4sBd7MC4mKWZm7K8k7HrZYz2usSpbrEtYGtr6MmN9hci+/ITDPE291DFkzIcDCF493v/3T+7XsnmQajh6kuI+bjIaACfo8N+twEoJf/N1PmphAQdEiC0CAwEAAaNTMFEwHQYDVR0O" +
			"BBYEFNvmSprQQ2HUUtPxs6UOuxq9lKKpMB8GA1UdIwQYMBaAFNvmSprQQ2HUUtPxs6UOuxq9lKKpMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAJEWxnxtQV5IqPVRr2SM" +
			"WNNxcJ7A/wyet39l5VhHjbrQGynk5WS80psn/riLUfIvtzYMWC0IR0pIMQuMDF5sNcKp4D8Xnrd+Bl/4/Iy/iTOoHlw+sPkKv+NL2XR3iO8bSDwjtjvd6L5NkUuzsRoSkQCG2fHASqqgFoyV9Ld" +
			"RsQa1w9ZGebtEWLuGsrJtR7gaFECqJnDbb0aPUMixmpMHID8kt154TrLhVFmMEqGGC1GvZVlQ9Of3GP9y7X4vDpHshdlWotOnYKHaeu2d5cRVFHhEbrslkISgh/TRuyl7VIpnjOYUwMBpCiVH6M" +
			"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
		clusterID = strfmt.UUID(uuid.New().String())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		mockJob = job.NewMockAPI(ctrl)
		clusterApi = cluster.NewManager(cluster.Config{}, getTestLog().WithField("pkg", "cluster-monitor"),
			db, nil, nil, nil, nil)
		bm = NewBareMetalInventory(db, getTestLog(), nil, clusterApi, cfg, mockJob, nil, mockS3Client, nil, getTestAuthHandler())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &clusterID,
			APIVip: "10.11.12.13",
		}}
		kubeconfigNoingress = fmt.Sprintf("%s/%s", clusterID, "kubeconfig-noingress")
		kubeconfigObject = fmt.Sprintf("%s/%s", clusterID, kubeconfig)
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		kubeconfigFile, err = os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())

	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
		kubeconfigFile.Close()
	})

	objectExists := func() {
		mockS3Client.EXPECT().DoesObjectExist(ctx, kubeconfigObject).Return(false, nil).Times(1)
	}

	It("UploadClusterIngressCert no cluster id", func() {
		clusterId := strToUUID(uuid.New().String())
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         *clusterId,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertNotFound()))
	})
	It("UploadClusterIngressCert cluster is not in installed state", func() {
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertBadRequest()))

	})
	It("UploadClusterIngressCert kubeconfig already exists, return ok", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		mockS3Client.EXPECT().DoesObjectExist(ctx, kubeconfigObject).Return(true, nil).Times(1)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertCreated()))
	})
	It("UploadClusterIngressCert DoesObjectExist fails ", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		mockS3Client.EXPECT().DoesObjectExist(ctx, kubeconfigObject).Return(true, errors.Errorf("dummy")).Times(1)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertInternalServerError()))
	})
	It("UploadClusterIngressCert s3download failure", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		objectExists()
		mockS3Client.EXPECT().Download(ctx, kubeconfigNoingress).Return(nil, int64(0), errors.Errorf("dummy")).Times(1)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertInternalServerError()))
	})
	It("UploadClusterIngressCert bad kubeconfig, mergeIngressCaIntoKubeconfig failure", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		objectExists()
		mockS3Client.EXPECT().Download(ctx, kubeconfigNoingress).Return(r, int64(0), nil).Times(1)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertInternalServerError()))
	})
	It("UploadClusterIngressCert bad ingressCa, mergeIngressCaIntoKubeconfig failure", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		objectExists()
		mockS3Client.EXPECT().Download(ctx, kubeconfigNoingress).Return(kubeconfigFile, int64(0), nil)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: "bad format",
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertInternalServerError()))
	})

	It("UploadClusterIngressCert push fails", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		data, err := os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())
		kubeConfigAsBytes, err := ioutil.ReadAll(data)
		Expect(err).ShouldNot(HaveOccurred())
		log := logrus.New()
		merged, err := mergeIngressCaIntoKubeconfig(kubeConfigAsBytes, []byte(ingressCa), log)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(merged).ShouldNot(Equal(kubeConfigAsBytes))
		Expect(merged).ShouldNot(Equal([]byte(ingressCa)))
		objectExists()
		mockS3Client.EXPECT().Download(ctx, kubeconfigNoingress).Return(kubeconfigFile, int64(0), nil).Times(1)
		mockS3Client.EXPECT().Upload(ctx, merged, kubeconfigObject).Return(errors.Errorf("Dummy"))
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewUploadClusterIngressCertInternalServerError()))
	})

	It("UploadClusterIngressCert download happy flow", func() {
		status := models.ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		data, err := os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())
		kubeConfigAsBytes, err := ioutil.ReadAll(data)
		Expect(err).ShouldNot(HaveOccurred())
		log := logrus.New()
		merged, err := mergeIngressCaIntoKubeconfig(kubeConfigAsBytes, []byte(ingressCa), log)
		Expect(err).ShouldNot(HaveOccurred())
		objectExists()
		mockS3Client.EXPECT().Download(ctx, kubeconfigNoingress).Return(kubeconfigFile, int64(0), nil).Times(1)
		mockS3Client.EXPECT().Upload(ctx, merged, kubeconfigObject).Return(nil)
		generateReply := bm.UploadClusterIngressCert(ctx, installer.UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(Equal(installer.NewUploadClusterIngressCertCreated()))
	})
})

var _ = Describe("Upload and Download logs test", func() {

	var (
		bm             *bareMetalInventory
		cfg            Config
		db             *gorm.DB
		ctx            = context.Background()
		ctrl           *gomock.Controller
		clusterID      strfmt.UUID
		hostID         strfmt.UUID
		c              common.Cluster
		kubeconfigFile *os.File
		mockClusterAPI *cluster.MockAPI
		dbName         = "upload_logs"
		mockS3Client   *s3wrapper.MockAPI
		request        *http.Request
		mockHostApi    *host.MockAPI
		host1          models.Host
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		db = common.PrepareTestDB(dbName)
		clusterID = strfmt.UUID(uuid.New().String())
		mockClusterAPI = cluster.NewMockAPI(ctrl)
		mockJob := job.NewMockAPI(ctrl)
		mockHostApi = host.NewMockAPI(ctrl)
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		bm = NewBareMetalInventory(db, getTestLog(), mockHostApi, mockClusterAPI, cfg, mockJob, nil, mockS3Client, nil, getTestAuthHandler())
		c = common.Cluster{Cluster: models.Cluster{
			ID:     &clusterID,
			APIVip: "10.11.12.13",
		}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		kubeconfigFile, err = os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())
		hostID = strfmt.UUID(uuid.New().String())
		host1 = addHost(hostID, models.HostRoleMaster, "known", clusterID, "{}", db)

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		part, err := writer.CreateFormFile("upfile", "test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())
		_, _ = io.Copy(part, kubeconfigFile)
		writer.Close()
		request, err = http.NewRequest("POST", "test", body)
		Expect(err).ShouldNot(HaveOccurred())
		request.Header.Add("Content-Type", writer.FormDataContentType())
		_ = request.ParseMultipartForm(32 << 20)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
		kubeconfigFile.Close()
	})

	It("Upload logs cluster not exits", func() {
		clusterId := strToUUID(uuid.New().String())
		params := installer.UploadHostLogsParams{
			ClusterID:   *clusterId,
			HostID:      hostID,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
		}
		verifyApiError(bm.UploadHostLogs(ctx, params), http.StatusNotFound)
	})
	It("Upload logs host not exits", func() {
		hostId := strToUUID(uuid.New().String())
		params := installer.UploadHostLogsParams{
			ClusterID:   clusterID,
			HostID:      *hostId,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
		}
		verifyApiError(bm.UploadHostLogs(ctx, params), http.StatusNotFound)
	})

	It("Upload S3 upload fails", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", clusterID, "{}", db)
		params := installer.UploadHostLogsParams{
			ClusterID:   clusterID,
			HostID:      *host.ID,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
		}
		fileName := bm.getLogsFullName(clusterID.String(), host.ID.String())
		mockS3Client.EXPECT().UploadStream(gomock.Any(), gomock.Any(), fileName).Return(errors.Errorf("Dummy")).Times(1)
		verifyApiError(bm.UploadHostLogs(ctx, params), http.StatusInternalServerError)
	})
	It("Upload Happy flow", func() {

		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", clusterID, "{}", db)
		params := installer.UploadHostLogsParams{
			ClusterID:   clusterID,
			HostID:      *host.ID,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
		}
		fileName := bm.getLogsFullName(clusterID.String(), host.ID.String())
		mockS3Client.EXPECT().UploadStream(gomock.Any(), gomock.Any(), fileName).Return(nil).Times(1)
		mockHostApi.EXPECT().SetUploadLogsAt(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		reply := bm.UploadHostLogs(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewUploadHostLogsNoContent()))
	})
	It("Download S3 logs where not uploaded yet", func() {
		params := installer.DownloadHostLogsParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		verifyApiError(bm.DownloadHostLogs(ctx, params), http.StatusNotFound)
	})
	It("Download S3 object not found", func() {
		params := installer.DownloadHostLogsParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		mockS3Client.EXPECT().Download(ctx, fileName).Return(nil, int64(0), s3wrapper.NotFound(fileName))
		verifyApiError(bm.DownloadHostLogs(ctx, params), http.StatusNotFound)
	})

	It("Download S3 object failed", func() {
		params := installer.DownloadHostLogsParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		mockS3Client.EXPECT().Download(ctx, fileName).Return(nil, int64(0), errors.Errorf("dummy"))
		verifyApiError(bm.DownloadHostLogs(ctx, params), http.StatusInternalServerError)
	})

	It("Download S3 object happy flow", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", clusterID, "{}", db)
		params := installer.DownloadHostLogsParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
		}
		fileName := bm.getLogsFullName(clusterID.String(), host.ID.String())
		host.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host)
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.DownloadHostLogs(ctx, params)
		downloadFileName := fmt.Sprintf("%s_%s", hostutil.GetHostnameForMsg(&host), filepath.Base(fileName))
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewDownloadHostLogsOK().WithPayload(r), downloadFileName, 4)))
	})
	It("Logs presigned host not found", func() {
		hostID := strfmt.UUID(uuid.New().String())
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
		})
		verifyApiError(generateReply, http.StatusNotFound)
	})
	It("Logs presigned no logs found", func() {
		hostID := strfmt.UUID(uuid.New().String())
		_ = addHost(hostID, models.HostRoleMaster, "known", clusterID, "{}", db)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, gomock.Any()).Return("",
			errors.Errorf("Dummy"))
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
		})
		verifyApiError(generateReply, http.StatusInternalServerError)
	})
	It("logs presigned happy flow", func() {
		hostID := strfmt.UUID(uuid.New().String())
		_ = addHost(hostID, models.HostRoleMaster, "known", clusterID, "{}", db)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, gomock.Any()).Return("url", nil)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.GetPresignedForClusterFilesOK{}))
		replyPayload := generateReply.(*installer.GetPresignedForClusterFilesOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})
	It("download cluster logs no cluster", func() {
		clusterId := strToUUID(uuid.New().String())
		params := installer.DownloadClusterLogsParams{
			ClusterID: *clusterId,
		}
		verifyApiError(bm.DownloadClusterLogs(ctx, params), http.StatusNotFound)
	})

	It("download cluster logs CreateTarredClusterLogs failed", func() {
		params := installer.DownloadClusterLogsParams{
			ClusterID: clusterID,
		}
		mockClusterAPI.EXPECT().CreateTarredClusterLogs(ctx, gomock.Any(), gomock.Any()).Return("", errors.Errorf("dummy"))
		verifyApiError(bm.DownloadClusterLogs(ctx, params), http.StatusInternalServerError)
	})

	It("download cluster logs Download failed", func() {
		params := installer.DownloadClusterLogsParams{
			ClusterID: clusterID,
		}
		fileName := fmt.Sprintf("%s_logs.zip", clusterID)
		mockClusterAPI.EXPECT().CreateTarredClusterLogs(ctx, gomock.Any(), gomock.Any()).Return(fileName, nil)
		mockS3Client.EXPECT().Download(ctx, fileName).Return(nil, int64(0), errors.Errorf("dummy"))
		verifyApiError(bm.DownloadClusterLogs(ctx, params), http.StatusInternalServerError)
	})

	It("download cluster logs happy flow", func() {
		params := installer.DownloadClusterLogsParams{
			ClusterID: clusterID,
		}
		fileName := fmt.Sprintf("%s_logs.zip", clusterID)
		mockClusterAPI.EXPECT().CreateTarredClusterLogs(ctx, gomock.Any(), gomock.Any()).Return(fileName, nil)
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.DownloadClusterLogs(ctx, params)
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewDownloadClusterLogsOK().WithPayload(r), fileName, 4)))
	})

	It("Logs presigned cluster logs failed", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockClusterAPI.EXPECT().CreateTarredClusterLogs(ctx, gomock.Any(), gomock.Any()).Return("", errors.Errorf("dummy"))
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
		})
		verifyApiError(generateReply, http.StatusInternalServerError)
	})

	It("Logs presigned cluster logs happy flow", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockClusterAPI.EXPECT().CreateTarredClusterLogs(ctx, gomock.Any(), gomock.Any()).Return("tarred", nil)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, "tarred", gomock.Any()).Return("url", nil)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.GetPresignedForClusterFilesOK{}))
		replyPayload := generateReply.(*installer.GetPresignedForClusterFilesOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})

})

var _ = Describe("GetClusterInstallConfig", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		c         common.Cluster
		dbName    = "get_cluster_install_config"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		clusterID = strfmt.UUID(uuid.New().String())
		bm = NewBareMetalInventory(db, getTestLog(), nil, nil, cfg, nil, nil, nil, nil, getTestAuthHandler())
		c = common.Cluster{Cluster: models.Cluster{
			ID:                     &clusterID,
			BaseDNSDomain:          "example.com",
			InstallConfigOverrides: `{"controlPlane": {"hyperthreading": "Disabled"}}`,
		}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("returns the correct install config", func() {
		params := installer.GetClusterInstallConfigParams{ClusterID: clusterID}
		response := bm.GetClusterInstallConfig(ctx, params)
		actual, ok := response.(*installer.GetClusterInstallConfigOK)
		Expect(ok).To(BeTrue())

		config := installcfg.InstallerConfigBaremetal{}
		err := yaml.Unmarshal([]byte(actual.Payload), &config)
		Expect(err).NotTo(HaveOccurred())

		Expect(config.ControlPlane.Hyperthreading).To(Equal("Disabled"))
		Expect(config.APIVersion).To(Equal("v1"))
		Expect(config.BaseDomain).To(Equal("example.com"))
	})

	It("returns not found with a non-existant cluster", func() {
		params := installer.GetClusterInstallConfigParams{ClusterID: strfmt.UUID(uuid.New().String())}
		response := bm.GetClusterInstallConfig(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})
})

var _ = Describe("UpdateClusterInstallConfig", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		c         common.Cluster
		dbName    = "update_cluster_install_config"
	)

	BeforeEach(func() {
		db = common.PrepareTestDB(dbName)
		clusterID = strfmt.UUID(uuid.New().String())
		bm = NewBareMetalInventory(db, getTestLog(), nil, nil, cfg, nil, nil, nil, nil, getTestAuthHandler())
		c = common.Cluster{Cluster: models.Cluster{ID: &clusterID}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("saves the given string to the cluster", func() {
		override := `{"controlPlane": {"hyperthreading": "Disabled"}}`
		params := installer.UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: override,
		}
		response := bm.UpdateClusterInstallConfig(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.UpdateClusterInstallConfigCreated{}))

		var updated common.Cluster
		err := db.First(&updated, "id = ?", clusterID).Error
		Expect(err).ShouldNot(HaveOccurred())
		Expect(updated.InstallConfigOverrides).To(Equal(override))
	})

	It("returns not found with a non-existant cluster", func() {
		override := `{"controlPlane": {"hyperthreading": "Disabled"}}`
		params := installer.UpdateClusterInstallConfigParams{
			ClusterID:           strfmt.UUID(uuid.New().String()),
			InstallConfigParams: override,
		}
		response := bm.UpdateClusterInstallConfig(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.UpdateClusterInstallConfigNotFound{}))
	})

	It("returns bad request when provided invalid json", func() {
		override := `{"controlPlane": {"hyperthreading": "Disabled"`
		params := installer.UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: override,
		}
		response := bm.UpdateClusterInstallConfig(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.UpdateClusterInstallConfigBadRequest{}))
	})

	It("returns bad request when provided invalid options", func() {
		override := `{"controlPlane": "foo"}`
		params := installer.UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: override,
		}
		response := bm.UpdateClusterInstallConfig(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.UpdateClusterInstallConfigBadRequest{}))
	})
})

func verifyApiError(responder middleware.Responder, expectedHttpStatus int32) {
	ExpectWithOffset(1, responder).To(BeAssignableToTypeOf(common.NewApiError(expectedHttpStatus, nil)))
	conncreteError := responder.(*common.ApiErrorResponse)
	ExpectWithOffset(1, conncreteError.StatusCode()).To(Equal(expectedHttpStatus))
}

func addHost(hostId strfmt.UUID, role models.HostRole, state string, clusterId strfmt.UUID, inventory string, db *gorm.DB) models.Host {
	host := models.Host{
		ID:        &hostId,
		ClusterID: clusterId,
		Status:    swag.String(state),
		Role:      role,
		Inventory: inventory,
	}
	Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	return host
}

var _ = Describe("proxySettingsForIgnition", func() {

	Context("test proxy settings in discovery ignition", func() {
		var parameters = []struct {
			httpProxy, httpsProxy, noProxy, res string
		}{
			{"", "", "", ""},
			{
				"http://proxy.proxy", "", "",
				`"proxy": { "httpProxy": "http://proxy.proxy" }`,
			},
			{
				"http://proxy.proxy", "https://proxy.proxy", "",
				`"proxy": { "httpProxy": "http://proxy.proxy", "httpsProxy": "https://proxy.proxy" }`,
			},
			{
				"http://proxy.proxy", "", ".domain",
				`"proxy": { "httpProxy": "http://proxy.proxy", "noProxy": [".domain"] }`,
			},
			{
				"http://proxy.proxy", "https://proxy.proxy", ".domain",
				`"proxy": { "httpProxy": "http://proxy.proxy", "httpsProxy": "https://proxy.proxy", "noProxy": [".domain"] }`,
			},
			{
				"", "https://proxy.proxy", ".domain,123.123.123.123",
				`"proxy": { "httpsProxy": "https://proxy.proxy", "noProxy": [".domain","123.123.123.123"] }`,
			},
			{
				"", "https://proxy.proxy", "",
				`"proxy": { "httpsProxy": "https://proxy.proxy" }`,
			},
			{
				"", "", ".domain", "",
			},
		}

		It("verify rendered proxy settings", func() {
			for _, p := range parameters {
				s, err := proxySettingsForIgnition(p.httpProxy, p.httpsProxy, p.noProxy)
				Expect(err).To(BeNil())
				Expect(s).To(Equal(p.res))
			}
		})
	})
})
