package bminventory

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cavaliercoder/go-cpio"
	ign_3_1 "github.com/coreos/ignition/v2/config/v3_1"
	ign_3_1_types "github.com/coreos/ignition/v2/config/v3_1/types"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	amgmtv1 "github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/dns"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/garbagecollector"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/isoeditor"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/types"
)

const FakeServiceBaseURL = "http://192.168.11.22:12345"

var (
	ctrl                     *gomock.Controller
	mockClusterApi           *cluster.MockAPI
	mockHostApi              *host.MockAPI
	mockEvents               *events.MockHandler
	mockS3Client             *s3wrapper.MockAPI
	mockSecretValidator      *validations.MockPullSecretValidator
	mockIsoEditorFactory     *isoeditor.MockFactory
	mockGenerator            *generator.MockISOInstallConfigGenerator
	mockVersions             *versions.MockHandler
	mockMetric               *metrics.MockAPI
	mockUsage                *usage.MockAPI
	mockK8sClient            *k8sclient.MockK8SClient
	mockCRDUtils             *MockCRDUtils
	mockAccountsMgmt         *ocm.MockOCMAccountsMgmt
	mockOperatorManager      *operators.MockAPI
	mockHwValidator          *hardware.MockValidator
	mockIgnitionBuilder      *ignition.MockIgnitionBuilder
	mockInstallConfigBuilder *installcfg.MockInstallConfigBuilder
	mockStaticNetworkConfig  *staticnetworkconfig.MockStaticNetworkConfig
	secondDayWorkerIgnition  = []byte(`{
		"ignition": {
		  "version": "3.1.0",
		  "config": {
			"merge": [{
			  "source": "http://1.1.1.1:22624/config/abc}"
			}]
		  }
		}
	  }`)
	discovery_ignition_3_1 = `{
		"ignition": {
		  "version": "3.1.0"
		},
		"storage": {
		  "files": [{
			  "path": "/tmp/example",
			  "contents": {
				"source": "data:text/plain;base64,aGVsbG8gd29ybGQK"
			  }
		  }]
		}
	}`
)

func mockClusterRegisterSteps() {
	mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(1)
	mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
}

func mockClusterRegisterSuccess(bm *bareMetalInventory, withEvents bool) {
	mockClusterRegisterSteps()
	mockMetric.EXPECT().ClusterRegistered(common.TestDefaultConfig.ReleaseVersion, gomock.Any(), gomock.Any()).Times(1)

	if withEvents {
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.RegisteredClusterEventName))).Times(2)
	}
}

func mockInfraEnvRegisterSuccess() {
	mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(1)
	mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
	mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).Times(2)
	mockS3Client.EXPECT().GetBaseIsoObject(gomock.Any(), gomock.Any()).Return("rhcos", nil).Times(1)
	mockS3Client.EXPECT().UploadISO(gomock.Any(), gomock.Any(), "rhcos", gomock.Any()).Return(nil).Times(1)
	mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
	mockS3Client.EXPECT().IsAwsS3().Return(false)
	mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), nil, models.EventSeverityInfo, gomock.Any(), gomock.Any()).AnyTimes()
}

func mockInfraEnvUpdateSuccess() {
	mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).Times(2)
	mockS3Client.EXPECT().GetBaseIsoObject(gomock.Any(), gomock.Any()).Return("rhcos", nil).Times(1)
	mockS3Client.EXPECT().UploadISO(gomock.Any(), gomock.Any(), "rhcos", gomock.Any()).Return(nil).Times(1)
	mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
	mockS3Client.EXPECT().IsAwsS3().Return(false)
	mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), nil, models.EventSeverityInfo, gomock.Any(), gomock.Any()).AnyTimes()
}

func mockInfraEnvUpdateSuccessNoImageGeneration() {
	mockS3Client.EXPECT().UpdateObjectTimestamp(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
	mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
}

func mockInfraEnvDeRegisterSuccess() {
	mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
}

func mockAMSSubscription(ctx context.Context) {
	mockAccountsMgmt.EXPECT().CreateSubscription(ctx, gomock.Any(), gomock.Any()).Return(&amgmtv1.Subscription{}, nil)
}

func mockUsageReports() {
	mockUsage.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	mockUsage.EXPECT().Remove(gomock.Any(), gomock.Any()).AnyTimes()
	mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
}

func TestValidator(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "inventory_test")
}

func getTestAuthHandler() auth.Authenticator {
	return auth.NewNoneAuthenticator(common.GetTestLog().WithField("pkg", "auth"))
}

func strToUUID(s string) *strfmt.UUID {
	u := strfmt.UUID(s)
	return &u
}

func mockGenerateInstallConfigSuccess(mockGenerator *generator.MockISOInstallConfigGenerator, mockVersions *versions.MockHandler) {
	if mockGenerator != nil {
		mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(1)
		mockGenerator.EXPECT().GenerateInstallConfig(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}
}

func mockGetInstallConfigSuccess(mockInstallConfigBuilder *installcfg.MockInstallConfigBuilder) {
	mockInstallConfigBuilder.EXPECT().GetInstallConfig(gomock.Any(), gomock.Any(), gomock.Any()).Return([]byte("some string"), nil).Times(1)
}

var _ = Describe("GenerateClusterISO", func() {
	var (
		bm             *bareMetalInventory
		cfg            Config
		db             *gorm.DB
		ctx            = context.Background()
		dbName         string
		ignitionReader = ioutil.NopCloser(strings.NewReader(`{
				"ignition":{"version":"3.1.0"},
				"storage":{
					"files":[
						{
							"path":"/opt/openshift/manifests/cvo-overrides.yaml",
							"contents":{
								"source":"data:text/plain;charset=utf-8;base64,YXBpVmVyc2lvbjogY29uZmlnLm9wZW5zaGlmdC5pby92MQpraW5kOiBDbHVzdGVyVmVyc2lvbgptZXRhZGF0YToKICBuYW1lc3BhY2U6IG9wZW5zaGlmdC1jbHVzdGVyLXZlcnNpb24KICBuYW1lOiB2ZXJzaW9uCnNwZWM6CiAgdXBzdHJlYW06IGh0dHBzOi8vYXBpLm9wZW5zaGlmdC5jb20vYXBpL3VwZ3JhZGVzX2luZm8vdjEvZ3JhcGgKICBjaGFubmVsOiBzdGFibGUtNC42CiAgY2x1c3RlcklEOiA0MTk0MGVlOC1lYzk5LTQzZGUtODc2Ni0xNzQzODFiNDkyMWQK"
							}
						}
					]
				},
				"systemd":{}
		}`))
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		cfg.ServiceBaseURL = FakeServiceBaseURL
		bm = createInventory(db, cfg)

		mockS3Client.EXPECT().Download(gomock.Any(), gomock.Any()).Return(ignitionReader, int64(0), nil).MinTimes(0)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	createClusterInDBWithHTTPProxy := func(pullSecretSet bool, httpProxy string) *common.Cluster {
		clusterID := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			PullSecretSet:    pullSecretSet,
			HTTPProxy:        httpProxy,
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
		}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		Expect(common.CreateInfraEnvForCluster(db, &cluster, models.ImageTypeFullIso)).ShouldNot(HaveOccurred())
		return &cluster
	}

	createClusterInDB := func(pullSecretSet bool) *common.Cluster {
		return createClusterInDBWithHTTPProxy(pullSecretSet, "")
	}

	mockUploadIso := func(cluster *common.Cluster, returnValue error) {
		srcIso := "rhcos"
		mockS3Client.EXPECT().GetBaseIsoObject(cluster.OpenshiftVersion, gomock.Any()).Return(srcIso, nil).Times(1)
		mockS3Client.EXPECT().UploadISO(gomock.Any(), gomock.Any(), srcIso,
			fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, cluster.ID.String())).Return(returnValue).Times(1)
	}

	mockUploadIsoInfraEnv := func(infraEnv *common.InfraEnv, returnValue error) {
		srcIso := "rhcos"
		mockS3Client.EXPECT().GetBaseIsoObject(infraEnv.OpenshiftVersion, gomock.Any()).Return(srcIso, nil).Times(1)
		mockS3Client.EXPECT().UploadISO(gomock.Any(), gomock.Any(), srcIso,
			fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, infraEnv.ID.String())).Return(returnValue).Times(1)
	}

	rollbackClusterImageCreationDate := func(clusterID *strfmt.UUID) {
		updatedTime := time.Now().Add(-11 * time.Second)
		updates := map[string]interface{}{}
		updates["generated_at"] = updatedTime
		db.Model(&common.InfraEnv{}).Where("id = ?", clusterID).Updates(updates)
	}

	It("success", func() {
		cluster := createClusterInDB(true)
		clusterId := cluster.ID
		mockS3Client.EXPECT().IsAwsS3().Return(false)
		mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
		mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		mockUploadIso(cluster, nil)
		mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (Image type is \"full-iso\", SSH public key is not set)", gomock.Any())
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
		getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: *clusterId}).(*installer.GetClusterOK)
		Expect(getReply.Payload.ID).To(Equal(clusterId))
		Expect(generateReply.(*installer.GenerateClusterISOCreated).Payload.HostNetworks).ToNot(BeNil())
		Expect(getReply.Payload.ImageInfo.DownloadURL).To(Equal(FakeServiceBaseURL + "/api/assisted-install/v1/clusters/" + clusterId.String() + "/downloads/image"))
	})

	It("success - infra-env", func() {
		infraEnvID := strfmt.UUID(uuid.New().String())
		infraEnv := createInfraEnvWithPullSecret(db, infraEnvID)
		mockS3Client.EXPECT().IsAwsS3().Return(false)
		mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		mockUploadIsoInfraEnv(infraEnv, nil)
		mockEvents.EXPECT().AddEvent(gomock.Any(), infraEnvID, nil, models.EventSeverityInfo, "Generated image (Image type is \"\", SSH public key is not set)", gomock.Any())
		err := bm.GenerateInfraEnvISOInternal(ctx, infraEnv)
		Expect(err).ToNot(HaveOccurred())
		getReply := bm.GetInfraEnv(ctx, installer.GetInfraEnvParams{InfraEnvID: infraEnvID}).(*installer.GetInfraEnvOK)
		Expect(getReply.Payload.ID).To(Equal(infraEnvID))
		Expect(getReply.Payload.DownloadURL).To(Equal(FakeServiceBaseURL + "/api/assisted-install/v2/infra-envs/" + infraEnvID.String() + "/downloads/image"))
	})

	It("success with proxy", func() {
		cluster := createClusterInDBWithHTTPProxy(true, "http://1.1.1.1:1234")
		clusterId := cluster.ID
		mockS3Client.EXPECT().IsAwsS3().Return(false)
		mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
		mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		mockUploadIso(cluster, nil)
		mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (proxy URL is \"http://1.1.1.1:1234\", Image type is \"full-iso\", SSH public key is not set)", gomock.Any())
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
		getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: *clusterId}).(*installer.GetClusterOK)
		Expect(getReply.Payload.ImageInfo.DownloadURL).To(Equal(FakeServiceBaseURL + "/api/assisted-install/v1/clusters/" + clusterId.String() + "/downloads/image"))

	})

	It("sets the auth token when using local auth and retry image creation", func() {
		// Use a local auth handler
		pub, priv, err := gencrypto.ECDSAKeyPairPEM()
		Expect(err).NotTo(HaveOccurred())
		os.Setenv("EC_PRIVATE_KEY_PEM", priv)
		defer os.Unsetenv("EC_PRIVATE_KEY_PEM")
		bm.authHandler, err = auth.NewLocalAuthenticator(
			&auth.Config{AuthType: auth.TypeLocal, ECPublicKeyPEM: pub},
			common.GetTestLog().WithField("pkg", "auth"),
			db,
		)
		Expect(err).NotTo(HaveOccurred())

		// Success flow
		cluster := createClusterInDB(true)
		clusterId := cluster.ID
		mockS3Client.EXPECT().IsAwsS3().Return(false)
		mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
		mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		mockUploadIso(cluster, nil)
		mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (Image type is \"full-iso\", SSH public key is not set)", gomock.Any())
		bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})

		// Check that a valid token was added to the URL
		getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: *clusterId}).(*installer.GetClusterOK)
		u, err := url.Parse(getReply.Payload.ImageInfo.DownloadURL)
		Expect(err).NotTo(HaveOccurred())
		tok := u.Query().Get("api_key")
		_, err = bm.authHandler.AuthURLAuth(tok)
		Expect(err).NotTo(HaveOccurred())
		firstURL := getReply.Payload.ImageInfo.DownloadURL

		// Attempt to create the image again and validate that the URL was not changed
		bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		getReply = bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: *clusterId}).(*installer.GetClusterOK)
		Expect(getReply.Payload.ImageInfo.DownloadURL).To(Equal(firstURL))
	})

	It("image already exists", func() {
		clusterId := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:            &clusterId,
				PullSecretSet: true,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		infraEnv := common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:            clusterId,
				PullSecretSet: true,
				Type:          models.ImageTypeFullIso,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
			Generated:  true,
		}
		infraEnv.ProxyHash, _ = computeProxyHash(nil)
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())

		mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
		mockS3Client.EXPECT().UpdateObjectTimestamp(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
		mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, nil, models.EventSeverityInfo,
			fmt.Sprintf(`Re-used existing image rather than generating a new one (image type is "%s")`, infraEnv.Type),
			gomock.Any())
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
		getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: clusterId}).(*installer.GetClusterOK)
		Expect(*getReply.Payload.ID).To(Equal(clusterId))
		Expect(generateReply.(*installer.GenerateClusterISOCreated).Payload.HostNetworks).ToNot(BeNil())
	})

	It("image expired", func() {
		clusterId := strfmt.UUID(uuid.New().String())
		cluster := common.Cluster{
			Cluster: models.Cluster{
				ID:               &clusterId,
				PullSecretSet:    true,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		infraEnv := common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:               clusterId,
				PullSecretSet:    true,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
				Type:             models.ImageTypeFullIso,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
			Generated:  true,
		}
		infraEnv.ProxyHash, _ = computeProxyHash(nil)
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		Expect(db.Create(&infraEnv).Error).ShouldNot(HaveOccurred())

		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
		mockUploadIso(&cluster, nil)
		mockS3Client.EXPECT().UpdateObjectTimestamp(gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
		mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).Times(1)
		mockEvents.EXPECT().AddEvent(gomock.Any(), clusterId, nil, models.EventSeverityInfo, "Generated image (Image type is \"full-iso\", SSH public key is not set)", gomock.Any())
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
		getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: clusterId}).(*installer.GetClusterOK)
		Expect(*getReply.Payload.ID).To(Equal(clusterId))
	})

	It("success with AWS S3", func() {
		cluster := createClusterInDB(true)
		clusterId := cluster.ID
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockUploadIso(cluster, nil)
		mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
		mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", nil).Times(1)
		mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (Image type is \"full-iso\", SSH public key is not set)", gomock.Any())
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
		getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: *clusterId}).(*installer.GetClusterOK)
		Expect(getReply.Payload.ID).To(Equal(clusterId))
	})

	It("cluster_not_exists", func() {
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         strfmt.UUID(uuid.New().String()),
			ImageCreateParams: &models.ImageCreateParams{},
		})
		verifyApiError(generateReply, http.StatusNotFound)
	})

	It("failed_to_upload_iso", func() {
		cluster := createClusterInDB(true)
		clusterId := cluster.ID
		mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
		mockUploadIso(cluster, errors.New("failed"))
		mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityError, gomock.Any(), gomock.Any())
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		verifyApiError(generateReply, http.StatusInternalServerError)
	})

	It("failed_missing_pull_secret", func() {
		clusterId := createClusterInDB(false).ID
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		verifyApiError(generateReply, http.StatusBadRequest)
	})

	It("failed_missing_openshift_token", func() {
		cluster := createClusterInDB(true)
		cluster.PullSecret = "{\"auths\":{\"another.cloud.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"
		clusterId := cluster.ID
		mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
		mockUploadIso(cluster, errors.New("failed"))
		mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityError, gomock.Any(), gomock.Any())
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
		generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID:         *clusterId,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		verifyApiError(generateReply, http.StatusInternalServerError)
	})

	It("failed corrupted ssh public key", func() {
		clusterID := createClusterInDB(true).ID
		reply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
			ClusterID: *clusterID,
			ImageCreateParams: &models.ImageCreateParams{
				SSHPublicKey: "anything but a valid key",
			},
		})
		verifyApiErrorString(reply, http.StatusBadRequest, "SSH")
	})

	Context("static network config", func() {
		map1 := models.MacInterfaceMap{
			&models.MacInterfaceMapItems0{MacAddress: "mac10", LogicalNicName: "nic10"},
			&models.MacInterfaceMapItems0{MacAddress: "mac11", LogicalNicName: "nic11"},
		}
		map2 := models.MacInterfaceMap{
			&models.MacInterfaceMapItems0{MacAddress: "mac20", LogicalNicName: "nic20"},
			&models.MacInterfaceMapItems0{MacAddress: "mac21", LogicalNicName: "nic21"},
		}
		map3 := models.MacInterfaceMap{
			&models.MacInterfaceMapItems0{MacAddress: "mac30", LogicalNicName: "nic30"},
			&models.MacInterfaceMapItems0{MacAddress: "mac31", LogicalNicName: "nic31"},
		}

		staticNetworkConfig := []*models.HostStaticNetworkConfig{
			common.FormatStaticConfigHostYAML("nic10", "02000048ba38", "192.168.126.30", "192.168.141.30", "192.168.126.1", map1),
			common.FormatStaticConfigHostYAML("nic20", "02000048ba48", "192.168.126.31", "192.168.141.31", "192.168.126.1", map2),
			common.FormatStaticConfigHostYAML("nic30", "02000048ba58", "192.168.126.32", "192.168.141.32", "192.168.126.1", map3),
		}
		staticNetworkFormatRes := "static network format result"

		It("static network config - success", func() {
			cluster := createClusterInDB(true)
			clusterId := cluster.ID
			mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(staticNetworkConfig).Return(nil).Times(1)
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockUploadIso(cluster, nil)
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes).Times(1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (Image type is \"full-iso\", SSH public key is not set)", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{StaticNetworkConfig: staticNetworkConfig},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
		})

		It("static network config - validation failed", func() {
			cluster := createClusterInDB(true)
			clusterId := cluster.ID
			mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(staticNetworkConfig).Return(fmt.Errorf("failed network validation")).Times(1)
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{StaticNetworkConfig: staticNetworkConfig},
			})
			verifyApiError(generateReply, http.StatusBadRequest)
		})

		It("static network config  - same static network config, image already exists", func() {
			cluster := createClusterInDB(true)
			clusterId := cluster.ID
			mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(staticNetworkConfig).Return(nil).Times(1)
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockUploadIso(cluster, nil)
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes).Times(1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (Image type is \"full-iso\", SSH public key is not set)", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{StaticNetworkConfig: staticNetworkConfig},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))

			rollbackClusterImageCreationDate(clusterId)

			mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(staticNetworkConfig).Return(nil).Times(1)
			mockS3Client.EXPECT().UpdateObjectTimestamp(gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes).Times(1)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo,
				`Re-used existing image rather than generating a new one (image type is "full-iso")`,
				gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(0)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(0)
			generateReply = bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{StaticNetworkConfig: staticNetworkConfig},
			})

			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
		})

		It("static network config  - different static network config", func() {
			cluster := createClusterInDB(true)
			clusterId := cluster.ID
			mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(staticNetworkConfig).Return(nil).Times(1)
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes).Times(1)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockUploadIso(cluster, nil)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (Image type is \"full-iso\", SSH public key is not set)", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			generateReply := bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{StaticNetworkConfig: staticNetworkConfig},
			})
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))

			rollbackClusterImageCreationDate(clusterId)

			newStaticNetworkConfig := []*models.HostStaticNetworkConfig{
				common.FormatStaticConfigHostYAML("0200003ef74c", "02000048ba48", "192.168.126.41", "192.168.141.41", "192.168.126.1", map1),
				common.FormatStaticConfigHostYAML("0200003ef73c", "02000048ba38", "192.168.126.40", "192.168.141.40", "192.168.126.1", map2),
				common.FormatStaticConfigHostYAML("0200003ef75c", "02000048ba58", "192.168.126.42", "192.168.141.42", "192.168.126.1", map3),
			}

			mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(newStaticNetworkConfig).Return(nil).Times(1)
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockUploadIso(cluster, nil)
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(newStaticNetworkConfig).Return("new static network res").Times(1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, nil, models.EventSeverityInfo, "Generated image (Image type is \"full-iso\", SSH public key is not set)", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			generateReply = bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID:         *clusterId,
				ImageCreateParams: &models.ImageCreateParams{StaticNetworkConfig: newStaticNetworkConfig},
			})

			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
			getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: *clusterId}).(*installer.GetClusterOK)
			Expect(getReply.Payload.ImageInfo.DownloadURL).To(Equal(FakeServiceBaseURL + "/api/assisted-install/v1/clusters/" + clusterId.String() + "/downloads/image"))
		})

	})

	Context("minimal iso", func() {
		var (
			cluster     *common.Cluster
			isoFilePath string
		)
		BeforeEach(func() {
			cluster = createClusterInDB(true)

			isoCacheDir, err := ioutil.TempDir("", "minimalisotest")
			Expect(err).NotTo(HaveOccurred())
			bm.ISOCacheDir = isoCacheDir

			f, err := ioutil.TempFile("", "minimalisotest")
			Expect(err).NotTo(HaveOccurred())
			isoFilePath = f.Name()
		})
		AfterEach(func() {
			s3wrapper.ClearFileCache()
			os.Remove(isoFilePath)
			os.RemoveAll(bm.ISOCacheDir)
		})

		generateClusterISO := func(imageType models.ImageType) middleware.Responder {
			return bm.GenerateClusterISO(ctx, installer.GenerateClusterISOParams{
				ClusterID: *cluster.ID,
				ImageCreateParams: &models.ImageCreateParams{
					ImageType: imageType,
				},
			})
		}

		stubWithEditor := func(factory *isoeditor.MockFactory, editor isoeditor.Editor) {
			factory.EXPECT().WithEditor(ctx, gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, isoPath string, log logrus.FieldLogger, proc isoeditor.EditFunc) error {
					return proc(editor)
				})
		}

		It("Creates the iso successfully", func() {
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
			mockS3Client.EXPECT().GetMinimalIsoObjectName(cluster.OpenshiftVersion, gomock.Any()).Return("rhcos-minimal.iso", nil)
			mockS3Client.EXPECT().DownloadPublic(gomock.Any(), "rhcos-minimal.iso").Return(ioutil.NopCloser(strings.NewReader("totallyaniso")), int64(12), nil)
			editor := isoeditor.NewMockEditor(ctrl)
			editor.EXPECT().CreateClusterMinimalISO(gomock.Any(), nil, gomock.Any()).Return(isoFilePath, nil)

			stubWithEditor(mockIsoEditorFactory, editor)

			mockS3Client.EXPECT().UploadFile(gomock.Any(), isoFilePath, fmt.Sprintf("discovery-image-%s.iso", cluster.ID))
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *cluster.ID, nil, models.EventSeverityInfo, "Generated image (Image type is \"minimal-iso\", SSH public key is not set)", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)

			generateReply := generateClusterISO(models.ImageTypeMinimalIso)
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
			_, err := os.Stat(isoFilePath)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("Regenerates the iso for a new type", func() {
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(2)
			mockS3Client.EXPECT().IsAwsS3().Return(false).Times(2)

			// Generate full-iso
			mockUploadIso(cluster, nil)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *cluster.ID, nil, models.EventSeverityInfo, "Generated image (Image type is \"full-iso\", SSH public key is not set)", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			generateReply := generateClusterISO(models.ImageTypeFullIso)
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))

			// Rollback cluster's ImageInfo.CreatedAt by 10 seconds to avoid "request came too soon" error
			// (see GenerateClusterISOInternal func)
			rollbackClusterImageCreationDate(cluster.ID)

			// Generate minimal-iso
			editor := isoeditor.NewMockEditor(ctrl)
			stubWithEditor(mockIsoEditorFactory, editor)
			editor.EXPECT().CreateClusterMinimalISO(gomock.Any(), nil, gomock.Any()).Return(isoFilePath, nil)
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
			mockS3Client.EXPECT().UploadFile(gomock.Any(), isoFilePath, fmt.Sprintf("discovery-image-%s.iso", cluster.ID))
			mockS3Client.EXPECT().GetMinimalIsoObjectName(cluster.OpenshiftVersion, gomock.Any()).Return("rhcos-minimal.iso", nil)
			mockS3Client.EXPECT().DownloadPublic(gomock.Any(), "rhcos-minimal.iso").Return(ioutil.NopCloser(strings.NewReader("totallyaniso")), int64(12), nil)
			mockEvents.EXPECT().AddEvent(gomock.Any(), *cluster.ID, nil, models.EventSeverityInfo, "Generated image (Image type is \"minimal-iso\", SSH public key is not set)", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)

			generateReply = generateClusterISO(models.ImageTypeMinimalIso)
			Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewGenerateClusterISOCreated()))
			_, err := os.Stat(isoFilePath)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("Failed to get minimal ISO object name", func() {
			expectedErrMsg := "some-internal-error"

			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
			mockS3Client.EXPECT().GetMinimalIsoObjectName(cluster.OpenshiftVersion, gomock.Any()).Return("", errors.New(expectedErrMsg))
			mockEvents.EXPECT().AddEvent(gomock.Any(), *cluster.ID, nil, models.EventSeverityError, "Failed to generate minimal ISO", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(0)

			generateReply := generateClusterISO(models.ImageTypeMinimalIso)
			Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(generateReply.(*common.ApiErrorResponse).Error()).Should(Equal(expectedErrMsg))
		})

		It("Failed to download minimal ISO", func() {
			expectedErrMsg := "some-internal-error"

			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
			mockS3Client.EXPECT().GetMinimalIsoObjectName(cluster.OpenshiftVersion, gomock.Any()).Return("rhcos-minimal.iso", nil)
			mockS3Client.EXPECT().DownloadPublic(gomock.Any(), "rhcos-minimal.iso").Return(nil, int64(0), errors.New(expectedErrMsg))
			mockEvents.EXPECT().AddEvent(gomock.Any(), *cluster.ID, nil, models.EventSeverityError, "Failed to generate minimal ISO", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(0)

			generateReply := generateClusterISO(models.ImageTypeMinimalIso)
			Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(generateReply.(*common.ApiErrorResponse).Error()).Should(Equal(expectedErrMsg))
		})

		It("Failed to create iso editor", func() {
			expectedErrMsg := "some-internal-error"

			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
			mockS3Client.EXPECT().GetMinimalIsoObjectName(cluster.OpenshiftVersion, gomock.Any()).Return("rhcos-minimal.iso", nil)
			mockS3Client.EXPECT().DownloadPublic(gomock.Any(), "rhcos-minimal.iso").Return(ioutil.NopCloser(strings.NewReader("totallyaniso")), int64(12), nil)
			mockIsoEditorFactory.EXPECT().WithEditor(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New(expectedErrMsg))
			mockEvents.EXPECT().AddEvent(gomock.Any(), *cluster.ID, nil, models.EventSeverityError, "Failed to generate minimal ISO", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(0)

			generateReply := generateClusterISO(models.ImageTypeMinimalIso)
			Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(generateReply.(*common.ApiErrorResponse).Error()).Should(Equal(expectedErrMsg))
		})

		It("Failed to create minimal discovery ISO", func() {
			expectedErrMsg := "some-internal-error"

			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
			mockS3Client.EXPECT().GetMinimalIsoObjectName(cluster.OpenshiftVersion, gomock.Any()).Return("rhcos-minimal.iso", nil)
			mockS3Client.EXPECT().DownloadPublic(gomock.Any(), "rhcos-minimal.iso").Return(ioutil.NopCloser(strings.NewReader("totallyaniso")), int64(12), nil)
			editor := isoeditor.NewMockEditor(ctrl)
			stubWithEditor(mockIsoEditorFactory, editor)
			editor.EXPECT().CreateClusterMinimalISO(gomock.Any(), nil, gomock.Any()).Return("", errors.New(expectedErrMsg))
			mockEvents.EXPECT().AddEvent(gomock.Any(), *cluster.ID, nil, models.EventSeverityError, "Failed to generate minimal ISO", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(0)

			generateReply := generateClusterISO(models.ImageTypeMinimalIso)
			Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(generateReply.(*common.ApiErrorResponse).Error()).Should(Equal(expectedErrMsg))
		})

		It("Failed to upload minimal discovery ISO", func() {
			expectedErrMsg := "some-internal-error"

			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
			mockS3Client.EXPECT().GetMinimalIsoObjectName(cluster.OpenshiftVersion, gomock.Any()).Return("rhcos-minimal.iso", nil)
			mockS3Client.EXPECT().DownloadPublic(gomock.Any(), "rhcos-minimal.iso").Return(ioutil.NopCloser(strings.NewReader("totallyaniso")), int64(12), nil)
			editor := isoeditor.NewMockEditor(ctrl)
			stubWithEditor(mockIsoEditorFactory, editor)
			editor.EXPECT().CreateClusterMinimalISO(gomock.Any(), nil, gomock.Any()).Return(isoFilePath, nil)
			mockS3Client.EXPECT().UploadFile(gomock.Any(), isoFilePath, fmt.Sprintf("discovery-image-%s.iso", cluster.ID)).Return(errors.New(expectedErrMsg))
			mockEvents.EXPECT().AddEvent(gomock.Any(), *cluster.ID, nil, models.EventSeverityError, "Failed to generate minimal ISO", gomock.Any())
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType()).Return(discovery_ignition_3_1, nil).Times(0)

			generateReply := generateClusterISO(models.ImageTypeMinimalIso)
			Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(generateReply.(*common.ApiErrorResponse).Error()).Should(Equal(expectedErrMsg))
		})
	})
})

func createClusterWithAvailability(db *gorm.DB, status string, highAvailabilityMode string) *common.Cluster {
	clusterID := strfmt.UUID(uuid.New().String())
	c := &common.Cluster{
		Cluster: models.Cluster{
			ID:                   &clusterID,
			Status:               swag.String(status),
			HighAvailabilityMode: &highAvailabilityMode,
		},
	}
	Expect(db.Create(c).Error).ToNot(HaveOccurred())
	return c
}

func createCluster(db *gorm.DB, status string) *common.Cluster {
	return createClusterWithAvailability(db, status, models.ClusterCreateParamsHighAvailabilityModeFull)
}

func createInfraEnv(db *gorm.DB, id strfmt.UUID) *common.InfraEnv {
	infraEnv := &common.InfraEnv{
		InfraEnv: models.InfraEnv{
			ID:        id,
			ClusterID: id,
		},
	}
	Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())
	return infraEnv
}

func createInfraEnvWithPullSecret(db *gorm.DB, id strfmt.UUID) *common.InfraEnv {
	infraEnv := &common.InfraEnv{
		InfraEnv: models.InfraEnv{
			ID:            id,
			ClusterID:     id,
			PullSecretSet: true,
		},
		PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
	}
	Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())
	return infraEnv
}

var _ = Describe("GetHost", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		hostID     strfmt.UUID
		clusterId  strfmt.UUID
		infraEnvId strfmt.UUID
	)

	BeforeEach(func() {
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = clusterId
		hostID = strfmt.UUID(uuid.New().String())
		db, _ = common.PrepareTestDB()
		bm = createInventory(db, cfg)

		hostObj := models.Host{
			ID:         &hostID,
			InfraEnvID: infraEnvId,
			ClusterID:  &clusterId,
			Status:     swag.String("discovering"),
		}
		Expect(db.Create(&hostObj).Error).ShouldNot(HaveOccurred())
	})

	It("Get host failed", func() {
		ctx := context.Background()
		params := installer.GetHostParams{
			ClusterID: clusterId,
			HostID:    "no-such-host",
		}

		response := bm.GetHost(ctx, params)
		Expect(response).Should(BeAssignableToTypeOf(&installer.GetHostNotFound{}))
	})

	It("Get host succeed", func() {
		ctx := context.Background()
		params := installer.GetHostParams{
			ClusterID: clusterId,
			HostID:    hostID,
		}

		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		response := bm.GetHost(ctx, params)
		Expect(response).Should(BeAssignableToTypeOf(&installer.GetHostOK{}))
	})

	It("Validate customization have occurred", func() {
		ctx := context.Background()

		hostObj := models.Host{
			ID:         &hostID,
			InfraEnvID: infraEnvId,
			ClusterID:  &clusterId,
			Status:     swag.String("discovering"),
			Bootstrap:  true,
		}
		Expect(db.Model(&hostObj).Update("Bootstrap", true).Error).ShouldNot(HaveOccurred())
		objectAfterUpdating, _ := common.GetHostFromDB(db, infraEnvId.String(), hostID.String())
		Expect(objectAfterUpdating.Bootstrap).To(BeTrue())
		Expect(objectAfterUpdating.ProgressStages).To(BeEmpty())
		params := installer.GetHostParams{
			ClusterID: clusterId,
			HostID:    hostID,
		}
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(host.BootstrapStages[:]).Times(1)
		response := bm.GetHost(ctx, params)
		Expect(response).Should(BeAssignableToTypeOf(&installer.GetHostOK{}))
		Expect(response.(*installer.GetHostOK).Payload.ProgressStages).To(ConsistOf(host.BootstrapStages[:]))
	})
})

var _ = Describe("V2GetHost", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		hostID     strfmt.UUID
		infraEnvId strfmt.UUID
	)

	BeforeEach(func() {
		infraEnvId = strfmt.UUID(uuid.New().String())
		hostID = strfmt.UUID(uuid.New().String())
		db, _ = common.PrepareTestDB()
		bm = createInventory(db, cfg)

		hostObj := models.Host{
			ID:         &hostID,
			InfraEnvID: infraEnvId,
			ClusterID:  nil,
			Status:     swag.String("discovering"),
		}
		Expect(db.Create(&hostObj).Error).ShouldNot(HaveOccurred())
	})

	It("V2 Get host failed", func() {
		ctx := context.Background()
		params := installer.V2GetHostParams{
			InfraEnvID: infraEnvId,
			HostID:     "no-such-host",
		}

		response := bm.V2GetHost(ctx, params)
		Expect(response).Should(BeAssignableToTypeOf(&installer.V2GetHostNotFound{}))
	})

	It("V2 Get host succeed", func() {
		ctx := context.Background()
		params := installer.V2GetHostParams{
			InfraEnvID: infraEnvId,
			HostID:     hostID,
		}

		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		response := bm.V2GetHost(ctx, params)
		Expect(response).Should(BeAssignableToTypeOf(&installer.V2GetHostOK{}))
	})

	It("V2 Validate customization have occurred", func() {
		ctx := context.Background()

		hostObj := models.Host{
			ID:         &hostID,
			InfraEnvID: infraEnvId,
			ClusterID:  &infraEnvId,
			Status:     swag.String("discovering"),
			Bootstrap:  true,
		}
		Expect(db.Model(&hostObj).Update("Bootstrap", true).Error).ShouldNot(HaveOccurred())
		objectAfterUpdating, _ := common.GetHostFromDB(db, infraEnvId.String(), hostID.String())
		Expect(objectAfterUpdating.Bootstrap).To(BeTrue())
		Expect(objectAfterUpdating.ProgressStages).To(BeEmpty())
		params := installer.V2GetHostParams{
			InfraEnvID: infraEnvId,
			HostID:     hostID,
		}
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(host.BootstrapStages[:]).Times(1)
		response := bm.V2GetHost(ctx, params)
		Expect(response).Should(BeAssignableToTypeOf(&installer.V2GetHostOK{}))
		Expect(response.(*installer.V2GetHostOK).Payload.ProgressStages).To(ConsistOf(host.BootstrapStages[:]))
	})
})

var _ = Describe("RegisterHost", func() {
	var (
		bm     *bareMetalInventory
		cfg    Config
		db     *gorm.DB
		ctx    = context.Background()
		dbName string
		hostID strfmt.UUID
	)

	BeforeEach(func() {
		hostID = strfmt.UUID(uuid.New().String())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("register host to non-existing cluster", func() {
		reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                &hostID,
			},
		})
		apiErr, ok := reply.(*common.ApiErrorResponse)
		Expect(ok).Should(BeTrue())
		Expect(apiErr.StatusCode()).Should(Equal(int32(http.StatusNotFound)))
	})

	It("register host to a cluster while installation is in progress", func() {
		By("creating the cluster")
		cluster := createCluster(db, models.ClusterStatusInstalling)
		_ = createInfraEnv(db, *cluster.ID)

		allowedStates := []string{
			models.ClusterStatusInsufficient, models.ClusterStatusReady,
			models.ClusterStatusPendingForInput, models.ClusterStatusAddingHosts}
		err := errors.Errorf(
			"Cluster %s is in installing state, host can register only in one of %s",
			cluster.ID, allowedStates)

		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(err).Times(1)

		mockEvents.EXPECT().
			AddEvent(gomock.Any(), *cluster.ID, &hostID, models.EventSeverityError, gomock.Any(), gomock.Any()).
			Times(1)

		By("trying to register an host while installation takes place")
		reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
			InfraEnvID: *cluster.ID,
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                &hostID,
			},
		})

		By("verifying returned response")
		apiErr, ok := reply.(*common.ApiErrorResponse)
		Expect(ok).Should(BeTrue())
		Expect(apiErr.StatusCode()).Should(Equal(int32(http.StatusConflict)))
		Expect(apiErr.Error()).Should(Equal(err.Error()))
	})

	Context("Register success", func() {
		for _, test := range []struct {
			availability string
			expectedRole models.HostRole
		}{
			{availability: models.ClusterHighAvailabilityModeFull, expectedRole: models.HostRoleAutoAssign},
			{availability: models.ClusterHighAvailabilityModeNone, expectedRole: models.HostRoleMaster},
		} {
			test := test

			It(fmt.Sprintf("cluster availability mode %s expected default host role %s",
				test.availability, test.expectedRole), func() {
				cluster := createClusterWithAvailability(db, models.ClusterStatusInsufficient, test.availability)
				infraEnv := createInfraEnv(db, *cluster.ID)

				mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, h *models.Host, db *gorm.DB) error {
						// validate that host is registered with auto-assign role
						Expect(h.Role).Should(Equal(test.expectedRole))
						Expect(h.InfraEnvID).Should(Equal(infraEnv.ID))
						return nil
					}).Times(1)
				mockCRDUtils.EXPECT().CreateAgentCR(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockEvents.EXPECT().
					AddEvent(gomock.Any(), *cluster.ID, &hostID, models.EventSeverityInfo, gomock.Any(), gomock.Any()).
					Times(1)

				bm.ServiceBaseURL = uuid.New().String()
				bm.ServiceCACertPath = uuid.New().String()
				bm.AgentDockerImg = uuid.New().String()
				bm.SkipCertVerification = true

				reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
					InfraEnvID: *cluster.ID,
					NewHostParams: &models.HostCreateParams{
						DiscoveryAgentVersion: "v1",
						HostID:                &hostID,
					},
				})
				_, ok := reply.(*installer.V2RegisterHostCreated)
				Expect(ok).Should(BeTrue())

				By("register_returns_next_step_runner_command")
				payload := reply.(*installer.V2RegisterHostCreated).Payload
				Expect(payload).ShouldNot(BeNil())
				command := payload.NextStepRunnerCommand
				Expect(command).ShouldNot(BeNil())
				Expect(command.Command).ShouldNot(BeEmpty())
				Expect(command.Args).ShouldNot(BeEmpty())
			})
		}
	})

	It("add_crd_failure", func() {
		cluster := createCluster(db, models.ClusterStatusInsufficient)
		infraEnv := createInfraEnv(db, *cluster.ID)
		expectedErrMsg := "some-internal-error"

		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, h *models.Host, db *gorm.DB) error {
				// validate that host is registered with auto-assign role
				Expect(h.Role).Should(Equal(models.HostRoleAutoAssign))
				Expect(h.InfraEnvID).Should(Equal(infraEnv.ID))
				return nil
			}).Times(1)
		mockCRDUtils.EXPECT().CreateAgentCR(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New(expectedErrMsg)).Times(1)
		mockEvents.EXPECT().
			AddEvent(gomock.Any(), *cluster.ID, &hostID, models.EventSeverityInfo, gomock.Any(), gomock.Any()).
			Times(1)
		reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
			InfraEnvID: *cluster.ID,
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                &hostID,
			},
		})
		_, ok := reply.(*installer.V2RegisterHostInternalServerError)
		Expect(ok).Should(BeTrue())
		payload := reply.(*installer.V2RegisterHostInternalServerError).Payload
		Expect(*payload.Code).Should(Equal(strconv.Itoa(http.StatusInternalServerError)))
		Expect(*payload.Reason).Should(ContainSubstring(expectedErrMsg))
	})

	It("host_api_failure", func() {
		cluster := createCluster(db, models.ClusterStatusInsufficient)
		_ = createInfraEnv(db, *cluster.ID)
		expectedErrMsg := "some-internal-error"

		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New(expectedErrMsg)).Times(1)
		mockEvents.EXPECT().
			AddEvent(gomock.Any(), *cluster.ID, &hostID, models.EventSeverityError, gomock.Any(), gomock.Any()).
			Times(1)
		reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
			InfraEnvID: *cluster.ID,
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                &hostID,
			},
		})
		err, ok := reply.(*common.ApiErrorResponse)
		Expect(ok).Should(BeTrue())
		Expect(err.StatusCode()).Should(Equal(int32(http.StatusBadRequest)))
		Expect(err.Error()).Should(ContainSubstring(expectedErrMsg))
	})
})

var _ = Describe("v2RegisterHost", func() {
	var (
		bm     *bareMetalInventory
		cfg    Config
		db     *gorm.DB
		ctx    = context.Background()
		dbName string
		hostID strfmt.UUID
	)

	BeforeEach(func() {
		hostID = strfmt.UUID(uuid.New().String())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("register host to non-existing infra-env", func() {
		reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                &hostID,
			},
		})
		apiErr, ok := reply.(*common.ApiErrorResponse)
		Expect(ok).Should(BeTrue())
		Expect(apiErr.StatusCode()).Should(Equal(int32(http.StatusNotFound)))
	})

	It("register host to a cluster while installation is in progress", func() {
		By("creating the cluster")
		cluster := createCluster(db, models.ClusterStatusInstalling)
		_ = createInfraEnv(db, *cluster.ID)

		allowedStates := []string{
			models.ClusterStatusInsufficient, models.ClusterStatusReady,
			models.ClusterStatusPendingForInput, models.ClusterStatusAddingHosts}
		err := errors.Errorf(
			"Cluster %s is in installing state, host can register only in one of %s",
			cluster.ID, allowedStates)

		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(err).Times(1)

		mockEvents.EXPECT().
			AddEvent(gomock.Any(), *cluster.ID, &hostID, models.EventSeverityError, gomock.Any(), gomock.Any()).
			Times(1)

		By("trying to register an host while installation takes place")
		reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
			InfraEnvID: *cluster.ID,
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                &hostID,
			},
		})

		By("verifying returned response")
		apiErr, ok := reply.(*common.ApiErrorResponse)
		Expect(ok).Should(BeTrue())
		Expect(apiErr.StatusCode()).Should(Equal(int32(http.StatusConflict)))
		Expect(apiErr.Error()).Should(Equal(err.Error()))
	})

	Context("Register success", func() {
		for _, test := range []struct {
			availability string
			expectedRole models.HostRole
		}{
			{availability: models.ClusterHighAvailabilityModeFull, expectedRole: models.HostRoleAutoAssign},
			{availability: models.ClusterHighAvailabilityModeNone, expectedRole: models.HostRoleMaster},
		} {
			test := test

			It(fmt.Sprintf("cluster availability mode %s expected default host role %s",
				test.availability, test.expectedRole), func() {
				cluster := createClusterWithAvailability(db, models.ClusterStatusInsufficient, test.availability)
				infraEnv := createInfraEnv(db, *cluster.ID)

				mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, h *models.Host, db *gorm.DB) error {
						// validate that host is registered with auto-assign role
						Expect(h.Role).Should(Equal(test.expectedRole))
						Expect(h.InfraEnvID).Should(Equal(infraEnv.ID))
						return nil
					}).Times(1)
				mockCRDUtils.EXPECT().CreateAgentCR(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockEvents.EXPECT().
					AddEvent(gomock.Any(), *cluster.ID, &hostID, models.EventSeverityInfo, gomock.Any(), gomock.Any()).
					Times(1)

				bm.ServiceBaseURL = uuid.New().String()
				bm.ServiceCACertPath = uuid.New().String()
				bm.AgentDockerImg = uuid.New().String()
				bm.SkipCertVerification = true

				reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
					InfraEnvID: *cluster.ID,
					NewHostParams: &models.HostCreateParams{
						DiscoveryAgentVersion: "v1",
						HostID:                &hostID,
					},
				})
				_, ok := reply.(*installer.V2RegisterHostCreated)
				Expect(ok).Should(BeTrue())

				By("register_returns_next_step_runner_command")
				payload := reply.(*installer.V2RegisterHostCreated).Payload
				Expect(payload).ShouldNot(BeNil())
				command := payload.NextStepRunnerCommand
				Expect(command).ShouldNot(BeNil())
				Expect(command.Command).ShouldNot(BeEmpty())
				Expect(command.Args).ShouldNot(BeEmpty())
			})
		}
	})

	It("add_crd_failure", func() {
		cluster := createCluster(db, models.ClusterStatusInsufficient)
		infraEnv := createInfraEnv(db, *cluster.ID)
		expectedErrMsg := "some-internal-error"

		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, h *models.Host, db *gorm.DB) error {
				// validate that host is registered with auto-assign role
				Expect(h.Role).Should(Equal(models.HostRoleAutoAssign))
				Expect(h.InfraEnvID).Should(Equal(infraEnv.ID))
				return nil
			}).Times(1)
		mockCRDUtils.EXPECT().CreateAgentCR(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New(expectedErrMsg)).Times(1)
		mockEvents.EXPECT().
			AddEvent(gomock.Any(), *cluster.ID, &hostID, models.EventSeverityInfo, gomock.Any(), gomock.Any()).
			Times(1)
		reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
			InfraEnvID: *cluster.ID,
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                &hostID,
			},
		})
		_, ok := reply.(*installer.V2RegisterHostInternalServerError)
		Expect(ok).Should(BeTrue())
		payload := reply.(*installer.V2RegisterHostInternalServerError).Payload
		Expect(*payload.Code).Should(Equal(strconv.Itoa(http.StatusInternalServerError)))
		Expect(*payload.Reason).Should(ContainSubstring(expectedErrMsg))
	})

	It("host_api_failure", func() {
		cluster := createCluster(db, models.ClusterStatusInsufficient)
		_ = createInfraEnv(db, *cluster.ID)
		expectedErrMsg := "some-internal-error"

		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New(expectedErrMsg)).Times(1)
		mockEvents.EXPECT().
			AddEvent(gomock.Any(), *cluster.ID, &hostID, models.EventSeverityError, gomock.Any(), gomock.Any()).
			Times(1)
		reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
			InfraEnvID: *cluster.ID,
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                &hostID,
			},
		})
		err, ok := reply.(*common.ApiErrorResponse)
		Expect(ok).Should(BeTrue())
		Expect(err.StatusCode()).Should(Equal(int32(http.StatusBadRequest)))
		Expect(err.Error()).Should(ContainSubstring(expectedErrMsg))
	})
})

var _ = Describe("GetNextSteps", func() {
	var (
		bm                *bareMetalInventory
		cfg               Config
		db                *gorm.DB
		ctx               = context.Background()
		defaultNextStepIn int64
		dbName            string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		defaultNextStepIn = 60
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("get_next_steps_unknown_host", func() {
		clusterId := strToUUID(uuid.New().String())
		unregistered_hostID := strToUUID(uuid.New().String())

		generateReply := bm.V2GetNextSteps(ctx, installer.V2GetNextStepsParams{
			InfraEnvID: *clusterId,
			HostID:     *unregistered_hostID,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewV2GetNextStepsNotFound()))
	})

	It("get_next_steps_success", func() {
		clusterId := strToUUID(uuid.New().String())
		infraEnvId := *clusterId
		hostId := strToUUID(uuid.New().String())
		checkedInAt := strfmt.DateTime(time.Now().Add(-time.Second))
		host := models.Host{
			ID:          hostId,
			InfraEnvID:  infraEnvId,
			ClusterID:   clusterId,
			Status:      swag.String("discovering"),
			CheckedInAt: checkedInAt,
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		var err error
		expectedStepsReply := models.Steps{NextInstructionSeconds: defaultNextStepIn, Instructions: []*models.Step{{StepType: models.StepTypeInventory},
			{StepType: models.StepTypeConnectivityCheck}}}
		h1, err := common.GetHostFromDB(db, infraEnvId.String(), hostId.String())
		Expect(err).ToNot(HaveOccurred())
		mockHostApi.EXPECT().GetNextSteps(gomock.Any(), gomock.Any()).Return(expectedStepsReply, err)
		reply := bm.V2GetNextSteps(ctx, installer.V2GetNextStepsParams{
			InfraEnvID: *clusterId,
			HostID:     *hostId,
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2GetNextStepsOK()))
		stepsReply := reply.(*installer.V2GetNextStepsOK).Payload
		expectedStepsType := []models.StepType{models.StepTypeInventory, models.StepTypeConnectivityCheck}
		Expect(stepsReply.Instructions).To(HaveLen(len(expectedStepsType)))
		for i, step := range stepsReply.Instructions {
			Expect(step.StepType).Should(Equal(expectedStepsType[i]))
		}
		h2, err := common.GetHostFromDB(db, infraEnvId.String(), hostId.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(h1.UpdatedAt).To(Equal(h2.UpdatedAt))
		Expect(h1.CheckedInAt).ToNot(Equal(h2.CheckedInAt))
	})
})

var _ = Describe("v2GetNextSteps", func() {
	var (
		bm                *bareMetalInventory
		cfg               Config
		db                *gorm.DB
		ctx               = context.Background()
		defaultNextStepIn int64
		dbName            string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		defaultNextStepIn = 60
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("get_next_steps_unknown_host", func() {
		clusterId := strToUUID(uuid.New().String())
		unregistered_hostID := strToUUID(uuid.New().String())

		generateReply := bm.V2GetNextSteps(ctx, installer.V2GetNextStepsParams{
			InfraEnvID: *clusterId,
			HostID:     *unregistered_hostID,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewV2GetNextStepsNotFound()))
	})

	It("get_next_steps_success", func() {
		clusterId := strToUUID(uuid.New().String())
		infraEnvId := *clusterId
		hostId := strToUUID(uuid.New().String())
		checkedInAt := strfmt.DateTime(time.Now().Add(-time.Second))
		host := models.Host{
			ID:          hostId,
			InfraEnvID:  infraEnvId,
			ClusterID:   clusterId,
			Status:      swag.String("discovering"),
			CheckedInAt: checkedInAt,
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		var err error
		expectedStepsReply := models.Steps{NextInstructionSeconds: defaultNextStepIn, Instructions: []*models.Step{{StepType: models.StepTypeInventory},
			{StepType: models.StepTypeConnectivityCheck}}}
		h1, err := common.GetHostFromDB(db, infraEnvId.String(), hostId.String())
		Expect(err).ToNot(HaveOccurred())
		mockHostApi.EXPECT().GetNextSteps(gomock.Any(), gomock.Any()).Return(expectedStepsReply, err)
		reply := bm.V2GetNextSteps(ctx, installer.V2GetNextStepsParams{
			InfraEnvID: *clusterId,
			HostID:     *hostId,
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2GetNextStepsOK()))
		stepsReply := reply.(*installer.V2GetNextStepsOK).Payload
		expectedStepsType := []models.StepType{models.StepTypeInventory, models.StepTypeConnectivityCheck}
		Expect(stepsReply.Instructions).To(HaveLen(len(expectedStepsType)))
		for i, step := range stepsReply.Instructions {
			Expect(step.StepType).Should(Equal(expectedStepsType[i]))
		}
		h2, err := common.GetHostFromDB(db, infraEnvId.String(), hostId.String())
		Expect(err).ToNot(HaveOccurred())
		Expect(h1.UpdatedAt).To(Equal(h2.UpdatedAt))
		Expect(h1.CheckedInAt).ToNot(Equal(h2.CheckedInAt))
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
		bm     *bareMetalInventory
		cfg    Config
		db     *gorm.DB
		ctx    = context.Background()
		dbName string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("Media disconnection", func() {
		var (
			clusterId *strfmt.UUID
			hostId    *strfmt.UUID
			host      *models.Host
		)

		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())
			host = &models.Host{
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("insufficient"),
			}
			Expect(db.Create(host).Error).ShouldNot(HaveOccurred())
		})

		It("Media disconnection occurred", func() {
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, hostId, models.EventSeverityError, gomock.Any(), gomock.Any())

			params := installer.V2PostStepReplyParams{
				InfraEnvID: *clusterId,
				HostID:     *hostId,
				Reply: &models.StepReply{
					ExitCode: MediaDisconnected,
					Output:   "output",
					StepType: models.StepTypeFreeNetworkAddresses,
				},
			}

			Expect(bm.V2PostStepReply(ctx, params)).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
			Expect(db.Take(host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String()).Error).ToNot(HaveOccurred())
			Expect(*host.Status).To(BeEquivalentTo(models.HostStatusError))
			Expect(*host.StatusInfo).To(BeEquivalentTo("Failed - Cannot read from the media (ISO) - media was likely disconnected"))
		})

		It("Media disconnection - wrapping an existing error", func() {
			updates := map[string]interface{}{}
			updates["Status"] = models.HostStatusError
			updates["StatusInfo"] = models.HostStatusError
			updateErr := db.Model(&common.Host{}).Where("id = ?", hostId).Updates(updates).Error
			Expect(updateErr).ShouldNot(HaveOccurred())
			params := installer.V2PostStepReplyParams{
				InfraEnvID: *clusterId,
				HostID:     *hostId,
				Reply: &models.StepReply{
					ExitCode: MediaDisconnected,
					Output:   "output",
					StepType: models.StepTypeFreeNetworkAddresses,
				},
			}

			Expect(bm.V2PostStepReply(ctx, params)).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
			Expect(db.Take(host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String()).Error).ToNot(HaveOccurred())
			Expect(*host.Status).To(BeEquivalentTo(models.HostStatusError))
			Expect(*host.StatusInfo).To(BeEquivalentTo("Failed - Cannot read from the media (ISO) - media was likely disconnected. error"))
		})

		It("Media disconnection - appending stderr", func() {
			updates := map[string]interface{}{}
			updates["Status"] = models.HostStatusError
			updateErr := db.Model(&common.Host{}).Where("id = ?", hostId).Updates(updates).Error
			Expect(updateErr).ShouldNot(HaveOccurred())
			params := installer.V2PostStepReplyParams{
				InfraEnvID: *clusterId,
				HostID:     *hostId,
				Reply: &models.StepReply{
					ExitCode: MediaDisconnected,
					Output:   "output",
					Error:    "error",
					StepType: models.StepTypeFreeNetworkAddresses,
				},
			}

			Expect(bm.V2PostStepReply(ctx, params)).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
			Expect(db.Take(host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String()).Error).ToNot(HaveOccurred())
			Expect(*host.Status).To(BeEquivalentTo(models.HostStatusError))
			Expect(*host.StatusInfo).To(BeEquivalentTo("Failed - Cannot read from the media (ISO) - media was likely disconnected. error"))
		})
	})

	Context("Free addresses", func() {
		var makeStepReply = func(clusterID, hostID strfmt.UUID, freeAddresses models.FreeNetworksAddresses) installer.V2PostStepReplyParams {
			b, _ := json.Marshal(&freeAddresses)
			return installer.V2PostStepReplyParams{
				InfraEnvID: clusterID,
				HostID:     hostID,
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
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("discovering"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			toMarshal := makeFreeNetworksAddresses(makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.1"))
			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
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
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("discovering"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			toMarshal := makeFreeNetworksAddresses()
			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
			var h models.Host
			Expect(db.Take(&h, "cluster_id = ? and id = ?", clusterId.String(), hostId.String()).Error).ToNot(HaveOccurred())
			Expect(h.FreeAddresses).To(BeEmpty())
		})
	})

	Context("Dhcp allocation", func() {
		var (
			clusterId, hostId *strfmt.UUID
			makeStepReply     = func(clusterID, hostID strfmt.UUID, dhcpAllocationResponse *models.DhcpAllocationResponse) installer.V2PostStepReplyParams {
				b, err := json.Marshal(dhcpAllocationResponse)
				Expect(err).ToNot(HaveOccurred())
				return installer.V2PostStepReplyParams{
					InfraEnvID: clusterID,
					HostID:     hostID,
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
			makeResponseWithLeases = func(apiVipStr, ingressVipStr, apiLease, ingressLease string) *models.DhcpAllocationResponse {
				apiVip := strfmt.IPv4(apiVipStr)
				ingressVip := strfmt.IPv4(ingressVipStr)
				ret := models.DhcpAllocationResponse{
					APIVipAddress:     &apiVip,
					IngressVipAddress: &ingressVip,
					APIVipLease:       apiLease,
					IngressVipLease:   ingressLease,
				}
				return &ret
			}
		)
		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())
			host := models.Host{
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("insufficient"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})
		It("Happy flow with leases", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponseWithLeases(common.TestIPv4Networking.APIVip, common.TestIPv4Networking.IngressVip, "lease { hello abc; }", "lease { hello abc; }"))
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
		It("API lease invalid", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponseWithLeases(common.TestIPv4Networking.APIVip, common.TestIPv4Networking.IngressVip, "llease { hello abc; }", "lease { hello abc; }"))
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
		It("Happy flow", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse(common.TestIPv4Networking.APIVip, common.TestIPv4Networking.IngressVip))
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
		It("Happy flow IPv6", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv6Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse("1001:db8::10", "1001:db8::11"))
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
		It("DHCP not enabled", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(false),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse(common.TestIPv4Networking.APIVip, common.TestIPv4Networking.IngressVip))
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
		It("Bad ingress VIP", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse(common.TestIPv4Networking.APIVip, "1.2.4.11"))
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
		It("New IPs while in insufficient", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					APIVip:            common.TestIPv4Networking.APIVip,
					IngressVip:        common.TestIPv4Networking.IngressVip,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse(
				common.IncrementIPString(common.TestIPv4Networking.APIVip), common.IncrementIPString(common.TestIPv4Networking.IngressVip)))
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
		It("New IPs while in installing", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					APIVip:            common.TestIPv4Networking.APIVip,
					IngressVip:        common.TestIPv4Networking.IngressVip,
					Status:            swag.String(models.ClusterStatusInstalling),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("Stam"))
			params := makeStepReply(*clusterId, *hostId, makeResponse(
				common.IncrementIPString(common.TestIPv4Networking.APIVip), common.IncrementIPString(common.TestIPv4Networking.IngressVip)))
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
	})

	Context("NTP synchronizer", func() {
		var (
			clusterId *strfmt.UUID
			hostId    *strfmt.UUID
		)

		var makeStepReply = func(clusterID, hostID strfmt.UUID, ntpSources []*models.NtpSource) installer.V2PostStepReplyParams {
			response := models.NtpSynchronizationResponse{
				NtpSources: ntpSources,
			}

			b, _ := json.Marshal(&response)

			return installer.V2PostStepReplyParams{
				InfraEnvID: clusterID,
				HostID:     hostID,
				Reply: &models.StepReply{
					Output:   string(b),
					StepType: models.StepTypeNtpSynchronizer,
				},
			}
		}

		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())

			host := models.Host{
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("discovering"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})

		It("NTP synchronizer success", func() {
			toMarshal := []*models.NtpSource{
				common.TestNTPSourceSynced,
				common.TestNTPSourceUnsynced,
			}

			mockHostApi.EXPECT().UpdateNTP(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})

		It("NTP synchronizer error", func() {
			mockHostApi.EXPECT().UpdateNTP(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.Errorf("Some error"))

			toMarshal := []*models.NtpSource{}
			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
	})

	Context("Image availability", func() {
		var (
			clusterId *strfmt.UUID
			hostId    *strfmt.UUID
		)

		var makeStepReply = func(clusterID, hostID strfmt.UUID, statuses []*models.ContainerImageAvailability) installer.V2PostStepReplyParams {
			response := models.ContainerImageAvailabilityResponse{
				Images: statuses,
			}

			b, err := json.Marshal(&response)
			Expect(err).ShouldNot(HaveOccurred())

			return installer.V2PostStepReplyParams{
				InfraEnvID: clusterID,
				HostID:     hostID,
				Reply: &models.StepReply{
					Output:   string(b),
					StepType: models.StepTypeContainerImageAvailability,
				},
			}
		}

		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())

			host := models.Host{
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("discovering"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})

		It("Image availability success", func() {
			toMarshal := []*models.ContainerImageAvailability{
				{Name: "image", Result: models.ContainerImageAvailabilityResultSuccess},
				{Name: "image2", Result: models.ContainerImageAvailabilityResultFailure},
			}

			mockHostApi.EXPECT().UpdateImageStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(2)

			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})

		It("Image availability error", func() {
			mockHostApi.EXPECT().UpdateImageStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.Errorf("Some error")).Times(1)

			toMarshal := []*models.ContainerImageAvailability{
				{Name: "image", Result: models.ContainerImageAvailabilityResultSuccess},
				{Name: "image2", Result: models.ContainerImageAvailabilityResultFailure},
			}
			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
	})
	Context("Disk speed", func() {
		var (
			clusterId *strfmt.UUID
			hostId    *strfmt.UUID
		)

		var makeStepReply = func(clusterID, hostID strfmt.UUID, path string, ioDuration, exitCode int64) installer.V2PostStepReplyParams {
			response := models.DiskSpeedCheckResponse{
				IoSyncDuration: ioDuration,
				Path:           path,
			}

			b, err := json.Marshal(&response)
			Expect(err).ShouldNot(HaveOccurred())

			return installer.V2PostStepReplyParams{
				InfraEnvID: clusterID,
				HostID:     hostID,
				Reply: &models.StepReply{
					ExitCode: exitCode,
					Output:   string(b),
					StepType: models.StepTypeInstallationDiskSpeedCheck,
				},
			}
		}

		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())

			host := models.Host{
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("discovering"),
			}

			cluster := common.Cluster{Cluster: models.Cluster{
				ID:               clusterId,
				PullSecretSet:    true,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}

			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})

		It("Disk speed success", func() {
			mockHostApi.EXPECT().SetDiskSpeed(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockMetric.EXPECT().DiskSyncDuration(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
			mockHwValidator.EXPECT().GetInstallationDiskSpeedThresholdMs(gomock.Any(), gomock.Any(), gomock.Any()).Return(int64(10), nil).Times(1)
			params := makeStepReply(*clusterId, *hostId, "/dev/sda", 5, 0)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})

		It("Disk speed failure", func() {
			mockHostApi.EXPECT().SetDiskSpeed(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

			params := makeStepReply(*clusterId, *hostId, "/dev/sda", 5, -1)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
	})
})

var _ = Describe("v2PostStepReply", func() {
	var (
		bm     *bareMetalInventory
		cfg    Config
		db     *gorm.DB
		ctx    = context.Background()
		dbName string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("Media disconnection", func() {
		var (
			clusterId *strfmt.UUID
			hostId    *strfmt.UUID
			host      *models.Host
		)

		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())
			host = &models.Host{
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("insufficient"),
			}
			Expect(db.Create(host).Error).ShouldNot(HaveOccurred())
		})

		It("Media disconnection occurred", func() {
			mockEvents.EXPECT().AddEvent(gomock.Any(), *clusterId, hostId, models.EventSeverityError, gomock.Any(), gomock.Any())

			params := installer.V2PostStepReplyParams{
				InfraEnvID: *clusterId,
				HostID:     *hostId,
				Reply: &models.StepReply{
					ExitCode: MediaDisconnected,
					Output:   "output",
					StepType: models.StepTypeFreeNetworkAddresses,
				},
			}

			Expect(bm.V2PostStepReply(ctx, params)).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
			Expect(db.Take(host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String()).Error).ToNot(HaveOccurred())
			Expect(*host.Status).To(BeEquivalentTo(models.HostStatusError))
			Expect(*host.StatusInfo).To(BeEquivalentTo("Failed - Cannot read from the media (ISO) - media was likely disconnected"))
		})

		It("Media disconnection - wrapping an existing error", func() {
			updates := map[string]interface{}{}
			updates["Status"] = models.HostStatusError
			updates["StatusInfo"] = models.HostStatusError
			updateErr := db.Model(&common.Host{}).Where("id = ?", hostId).Updates(updates).Error
			Expect(updateErr).ShouldNot(HaveOccurred())
			params := installer.V2PostStepReplyParams{
				InfraEnvID: *clusterId,
				HostID:     *hostId,
				Reply: &models.StepReply{
					ExitCode: MediaDisconnected,
					Output:   "output",
					StepType: models.StepTypeFreeNetworkAddresses,
				},
			}

			Expect(bm.V2PostStepReply(ctx, params)).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
			Expect(db.Take(host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String()).Error).ToNot(HaveOccurred())
			Expect(*host.Status).To(BeEquivalentTo(models.HostStatusError))
			Expect(*host.StatusInfo).To(BeEquivalentTo("Failed - Cannot read from the media (ISO) - media was likely disconnected. error"))
		})

		It("Media disconnection - appending stderr", func() {
			updates := map[string]interface{}{}
			updates["Status"] = models.HostStatusError
			updateErr := db.Model(&common.Host{}).Where("id = ?", hostId).Updates(updates).Error
			Expect(updateErr).ShouldNot(HaveOccurred())
			params := installer.V2PostStepReplyParams{
				InfraEnvID: *clusterId,
				HostID:     *hostId,
				Reply: &models.StepReply{
					ExitCode: MediaDisconnected,
					Output:   "output",
					Error:    "error",
					StepType: models.StepTypeFreeNetworkAddresses,
				},
			}

			Expect(bm.V2PostStepReply(ctx, params)).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
			Expect(db.Take(host, "cluster_id = ? and id = ?", clusterId.String(), hostId.String()).Error).ToNot(HaveOccurred())
			Expect(*host.Status).To(BeEquivalentTo(models.HostStatusError))
			Expect(*host.StatusInfo).To(BeEquivalentTo("Failed - Cannot read from the media (ISO) - media was likely disconnected. error"))
		})
	})

	Context("Free addresses", func() {
		var makeStepReply = func(clusterID, hostID strfmt.UUID, freeAddresses models.FreeNetworksAddresses) installer.V2PostStepReplyParams {
			b, _ := json.Marshal(&freeAddresses)
			return installer.V2PostStepReplyParams{
				InfraEnvID: clusterID,
				HostID:     hostID,
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
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("discovering"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			toMarshal := makeFreeNetworksAddresses(makeFreeAddresses("10.0.0.0/24", "10.0.0.0", "10.0.0.1"))
			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
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
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("discovering"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
			toMarshal := makeFreeNetworksAddresses()
			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
			var h models.Host
			Expect(db.Take(&h, "cluster_id = ? and id = ?", clusterId.String(), hostId.String()).Error).ToNot(HaveOccurred())
			Expect(h.FreeAddresses).To(BeEmpty())
		})
	})

	Context("Dhcp allocation", func() {
		var (
			clusterId, hostId *strfmt.UUID
			makeStepReply     = func(clusterID, hostID strfmt.UUID, dhcpAllocationResponse *models.DhcpAllocationResponse) installer.V2PostStepReplyParams {
				b, err := json.Marshal(dhcpAllocationResponse)
				Expect(err).ToNot(HaveOccurred())
				return installer.V2PostStepReplyParams{
					InfraEnvID: clusterID,
					HostID:     hostID,
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
			makeResponseWithLeases = func(apiVipStr, ingressVipStr, apiLease, ingressLease string) *models.DhcpAllocationResponse {
				apiVip := strfmt.IPv4(apiVipStr)
				ingressVip := strfmt.IPv4(ingressVipStr)
				ret := models.DhcpAllocationResponse{
					APIVipAddress:     &apiVip,
					IngressVipAddress: &ingressVip,
					APIVipLease:       apiLease,
					IngressVipLease:   ingressLease,
				}
				return &ret
			}
		)
		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())
			host := models.Host{
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("insufficient"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})
		It("Happy flow with leases", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponseWithLeases(common.TestIPv4Networking.APIVip, common.TestIPv4Networking.IngressVip, "lease { hello abc; }", "lease { hello abc; }"))
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
		It("API lease invalid", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponseWithLeases(common.TestIPv4Networking.APIVip, common.TestIPv4Networking.IngressVip, "llease { hello abc; }", "lease { hello abc; }"))
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
		It("Happy flow", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse(common.TestIPv4Networking.APIVip, common.TestIPv4Networking.IngressVip))
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
		It("Happy flow IPv6", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv6Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse("1001:db8::10", "1001:db8::11"))
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
		It("DHCP not enabled", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(false),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse(common.TestIPv4Networking.APIVip, common.TestIPv4Networking.IngressVip))
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
		It("Bad ingress VIP", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse(common.TestIPv4Networking.APIVip, "1.2.4.11"))
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
		It("New IPs while in insufficient", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					APIVip:            common.TestIPv4Networking.APIVip,
					IngressVip:        common.TestIPv4Networking.IngressVip,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse(
				common.IncrementIPString(common.TestIPv4Networking.APIVip), common.IncrementIPString(common.TestIPv4Networking.IngressVip)))
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
		It("New IPs while in installing", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					APIVip:            common.TestIPv4Networking.APIVip,
					IngressVip:        common.TestIPv4Networking.IngressVip,
					Status:            swag.String(models.ClusterStatusInstalling),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("Stam"))
			params := makeStepReply(*clusterId, *hostId, makeResponse(
				common.IncrementIPString(common.TestIPv4Networking.APIVip), common.IncrementIPString(common.TestIPv4Networking.IngressVip)))
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
	})

	Context("NTP synchronizer", func() {
		var (
			clusterId *strfmt.UUID
			hostId    *strfmt.UUID
		)

		var makeStepReply = func(clusterID, hostID strfmt.UUID, ntpSources []*models.NtpSource) installer.V2PostStepReplyParams {
			response := models.NtpSynchronizationResponse{
				NtpSources: ntpSources,
			}

			b, _ := json.Marshal(&response)

			return installer.V2PostStepReplyParams{
				InfraEnvID: clusterID,
				HostID:     hostID,
				Reply: &models.StepReply{
					Output:   string(b),
					StepType: models.StepTypeNtpSynchronizer,
				},
			}
		}

		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())

			host := models.Host{
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("discovering"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})

		It("NTP synchronizer success", func() {
			toMarshal := []*models.NtpSource{
				common.TestNTPSourceSynced,
				common.TestNTPSourceUnsynced,
			}

			mockHostApi.EXPECT().UpdateNTP(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})

		It("NTP synchronizer error", func() {
			mockHostApi.EXPECT().UpdateNTP(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.Errorf("Some error"))

			toMarshal := []*models.NtpSource{}
			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
	})

	Context("Image availability", func() {
		var (
			clusterId *strfmt.UUID
			hostId    *strfmt.UUID
		)

		var makeStepReply = func(clusterID, hostID strfmt.UUID, statuses []*models.ContainerImageAvailability) installer.V2PostStepReplyParams {
			response := models.ContainerImageAvailabilityResponse{
				Images: statuses,
			}

			b, err := json.Marshal(&response)
			Expect(err).ShouldNot(HaveOccurred())

			return installer.V2PostStepReplyParams{
				InfraEnvID: clusterID,
				HostID:     hostID,
				Reply: &models.StepReply{
					Output:   string(b),
					StepType: models.StepTypeContainerImageAvailability,
				},
			}
		}

		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())

			host := models.Host{
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("discovering"),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})

		It("Image availability success", func() {
			toMarshal := []*models.ContainerImageAvailability{
				{Name: "image", Result: models.ContainerImageAvailabilityResultSuccess},
				{Name: "image2", Result: models.ContainerImageAvailabilityResultFailure},
			}

			mockHostApi.EXPECT().UpdateImageStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(2)

			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})

		It("Image availability error", func() {
			mockHostApi.EXPECT().UpdateImageStatus(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.Errorf("Some error")).Times(1)

			toMarshal := []*models.ContainerImageAvailability{
				{Name: "image", Result: models.ContainerImageAvailabilityResultSuccess},
				{Name: "image2", Result: models.ContainerImageAvailabilityResultFailure},
			}
			params := makeStepReply(*clusterId, *hostId, toMarshal)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
	})
	Context("Disk speed", func() {
		var (
			clusterId *strfmt.UUID
			hostId    *strfmt.UUID
		)

		var makeStepReply = func(clusterID, hostID strfmt.UUID, path string, ioDuration, exitCode int64) installer.V2PostStepReplyParams {
			response := models.DiskSpeedCheckResponse{
				IoSyncDuration: ioDuration,
				Path:           path,
			}

			b, err := json.Marshal(&response)
			Expect(err).ShouldNot(HaveOccurred())

			return installer.V2PostStepReplyParams{
				InfraEnvID: clusterID,
				HostID:     hostID,
				Reply: &models.StepReply{
					ExitCode: exitCode,
					Output:   string(b),
					StepType: models.StepTypeInstallationDiskSpeedCheck,
				},
			}
		}

		BeforeEach(func() {
			clusterId = strToUUID(uuid.New().String())
			hostId = strToUUID(uuid.New().String())

			host := models.Host{
				ID:         hostId,
				InfraEnvID: *clusterId,
				ClusterID:  clusterId,
				Status:     swag.String("discovering"),
			}

			cluster := common.Cluster{Cluster: models.Cluster{
				ID:               clusterId,
				PullSecretSet:    true,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}

			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})

		It("Disk speed success", func() {
			mockHostApi.EXPECT().SetDiskSpeed(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockMetric.EXPECT().DiskSyncDuration(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
			mockHwValidator.EXPECT().GetInstallationDiskSpeedThresholdMs(gomock.Any(), gomock.Any(), gomock.Any()).Return(int64(10), nil).Times(1)
			params := makeStepReply(*clusterId, *hostId, "/dev/sda", 5, 0)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})

		It("Disk speed failure", func() {
			mockHostApi.EXPECT().SetDiskSpeed(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

			params := makeStepReply(*clusterId, *hostId, "/dev/sda", 5, -1)
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
	})
})

var _ = Describe("GetFreeAddresses", func() {
	var (
		bm     *bareMetalInventory
		cfg    Config
		db     *gorm.DB
		ctx    = context.Background()
		dbName string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	var makeHost = func(clusterId *strfmt.UUID, freeAddresses, status string) *models.Host {
		hostId := strToUUID(uuid.New().String())
		ret := models.Host{
			ID:            hostId,
			InfraEnvID:    *clusterId,
			ClusterID:     clusterId,
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
		bm     *bareMetalInventory
		cfg    Config
		db     *gorm.DB
		ctx    = context.Background()
		dbName string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
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
				CurrentStage: common.TestDefaultConfig.HostProgressStage,
			}

			err := db.Create(&models.Host{
				ID:         &hostID,
				InfraEnvID: clusterID,
				ClusterID:  &clusterID,
			}).Error
			Expect(err).ShouldNot(HaveOccurred())

		})

		It("success", func() {

			By("update with new data", func() {
				mockEvents.EXPECT().AddEvent(gomock.Any(), clusterID, &hostID, models.EventSeverityInfo, gomock.Any(), gomock.Any())
				mockHostApi.EXPECT().UpdateInstallProgress(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				mockClusterApi.EXPECT().UpdateInstallProgress(ctx, clusterID)
				reply := bm.UpdateHostInstallProgress(ctx, installer.UpdateHostInstallProgressParams{
					ClusterID:    clusterID,
					HostProgress: progressParams,
					HostID:       hostID,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressOK()))
			})

			By("update with no changes", func() {
				// We used an hostmock so DB wasn't updated after first step.
				reply := bm.UpdateHostInstallProgress(ctx, installer.UpdateHostInstallProgressParams{
					ClusterID:    clusterID,
					HostProgress: &models.HostProgress{},
					HostID:       hostID,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressOK()))
			})
		})

		It("update_failed", func() {
			mockHostApi.EXPECT().UpdateInstallProgress(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.Errorf("some error"))
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
				CurrentStage: common.TestDefaultConfig.HostProgressStage,
			},
			HostID: strfmt.UUID(uuid.New().String()),
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressNotFound()))
	})
})

var _ = Describe("V2UpdateHostInstallProgress", func() {
	var (
		bm     *bareMetalInventory
		cfg    Config
		db     *gorm.DB
		ctx    = context.Background()
		dbName string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("host exists", func() {
		var (
			hostID         strfmt.UUID
			infraEnvID     strfmt.UUID
			clusterID      strfmt.UUID
			progressParams *models.HostProgress
		)

		BeforeEach(func() {
			hostID = strfmt.UUID(uuid.New().String())
			infraEnvID = strfmt.UUID(uuid.New().String())
			clusterID = strfmt.UUID(uuid.New().String())
			progressParams = &models.HostProgress{
				CurrentStage: common.TestDefaultConfig.HostProgressStage,
			}

			err := db.Create(&models.Host{
				ID:         &hostID,
				InfraEnvID: infraEnvID,
				ClusterID:  &clusterID,
			}).Error
			Expect(err).ShouldNot(HaveOccurred())

		})

		It("success", func() {

			By("update with new data", func() {
				mockEvents.EXPECT().AddEvent(gomock.Any(), infraEnvID, &hostID, models.EventSeverityInfo, gomock.Any(), gomock.Any())
				mockHostApi.EXPECT().UpdateInstallProgress(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				mockClusterApi.EXPECT().UpdateInstallProgress(ctx, clusterID)
				reply := bm.V2UpdateHostInstallProgress(ctx, installer.V2UpdateHostInstallProgressParams{
					InfraEnvID:   infraEnvID,
					HostProgress: progressParams,
					HostID:       hostID,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostInstallProgressOK()))
			})

			By("update with no changes", func() {
				// We used an hostmock so DB wasn't updated after first step.
				reply := bm.V2UpdateHostInstallProgress(ctx, installer.V2UpdateHostInstallProgressParams{
					InfraEnvID:   infraEnvID,
					HostProgress: &models.HostProgress{},
					HostID:       hostID,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostInstallProgressOK()))
			})
		})

		It("update_failed", func() {
			mockHostApi.EXPECT().UpdateInstallProgress(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.Errorf("some error"))
			reply := bm.V2UpdateHostInstallProgress(ctx, installer.V2UpdateHostInstallProgressParams{
				InfraEnvID:   infraEnvID,
				HostProgress: progressParams,
				HostID:       hostID,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostInstallProgressInternalServerError()))
		})
	})

	It("host_dont_exist", func() {
		reply := bm.V2UpdateHostInstallProgress(ctx, installer.V2UpdateHostInstallProgressParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			HostProgress: &models.HostProgress{
				CurrentStage: common.TestDefaultConfig.HostProgressStage,
			},
			HostID: strfmt.UUID(uuid.New().String()),
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostInstallProgressNotFound()))
	})
})

func mockSetConnectivityMajorityGroupsForCluster(mockClusterApi *cluster.MockAPI) {
	mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).Times(1)
}

func mockSetConnectivityMajorityGroupsForClusterTimes(mockClusterApi *cluster.MockAPI, times int) {
	mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).Times(times)
}

var _ = Describe("cluster", func() {
	masterHostId1 := strfmt.UUID(uuid.New().String())
	masterHostId2 := strfmt.UUID(uuid.New().String())
	masterHostId3 := strfmt.UUID(uuid.New().String())
	diskID1 := "/dev/sda"
	diskID2 := "/dev/sdb"

	var (
		bm             *bareMetalInventory
		cfg            Config
		db             *gorm.DB
		ctx            = context.Background()
		clusterID      strfmt.UUID
		dbName         string
		ignitionReader io.ReadCloser
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		Expect(cfg.IPv6Support).Should(BeTrue())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
		bm.ocmClient = nil

		ignitionReader = ioutil.NopCloser(strings.NewReader(`{
				"ignition":{"version":"3.1.0"},
				"storage":{
					"files":[
						{
							"path":"/opt/openshift/manifests/cvo-overrides.yaml",
							"contents":{
								"source":"data:text/plain;charset=utf-8;base64,YXBpVmVyc2lvbjogY29uZmlnLm9wZW5zaGlmdC5pby92MQpraW5kOiBDbHVzdGVyVmVyc2lvbgptZXRhZGF0YToKICBuYW1lc3BhY2U6IG9wZW5zaGlmdC1jbHVzdGVyLXZlcnNpb24KICBuYW1lOiB2ZXJzaW9uCnNwZWM6CiAgdXBzdHJlYW06IGh0dHBzOi8vYXBpLm9wZW5zaGlmdC5jb20vYXBpL3VwZ3JhZGVzX2luZm8vdjEvZ3JhcGgKICBjaGFubmVsOiBzdGFibGUtNC42CiAgY2x1c3RlcklEOiA0MTk0MGVlOC1lYzk5LTQzZGUtODc2Ni0xNzQzODFiNDkyMWQK"
							}
						}
					]
				},
				"systemd":{}
		}`))
		mockS3Client.EXPECT().Download(gomock.Any(), gomock.Any()).Return(ignitionReader, int64(0), nil).MinTimes(0)
		mockUsageReports()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	addHost := func(hostId strfmt.UUID, role models.HostRole, state, kind string, clusterId strfmt.UUID, inventory string, db *gorm.DB) models.Host {
		host := models.Host{
			ID:         &hostId,
			InfraEnvID: clusterId,
			ClusterID:  &clusterId,
			Status:     swag.String(state),
			Kind:       swag.String(kind),
			Role:       role,
			Inventory:  inventory,
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		return host
	}

	mockClusterPrepareForInstallationSuccess := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().PrepareForInstallation(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}

	mockDurationsSuccess := func() {
		mockMetric.EXPECT().Duration(gomock.Any(), gomock.Any()).Return().AnyTimes()
	}

	mockClusterPrepareForInstallationFailure := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().PrepareForInstallation(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.Errorf("error")).Times(1)
	}
	mockHostPrepareForRefresh := func(mockHostApi *host.MockAPI) {
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
	mockClusterRefreshStatus := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
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
	mockClusterDeleteLogsSuccess := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().DeleteClusterLogs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}
	mockClusterDeleteLogsFailure := func(mockClusterApi *cluster.MockAPI) {
		mockClusterApi.EXPECT().DeleteClusterLogs(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("error")).Times(1)
	}
	setDefaultHostSetBootstrap := func(mockClusterApi *cluster.MockAPI) {
		mockHostApi.EXPECT().SetBootstrap(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
	mockHandlePreInstallationSuccess := func(mockClusterApi *cluster.MockAPI, done chan int) {
		mockClusterApi.EXPECT().HandlePreInstallSuccess(gomock.Any(), gomock.Any()).Times(1).
			Do(func(ctx, c interface{}) { done <- 1 })
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
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		mockClusterApi.EXPECT().DeleteClusterFiles(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockClusterApi.EXPECT().ResetCluster(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockHostApi.EXPECT().ResetHost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	}
	setResetClusterConflict := func() {
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		mockClusterApi.EXPECT().ResetCluster(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.NewApiError(http.StatusConflict, nil)).Times(1)
	}
	setResetClusterInternalServerError := func() {
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
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

	mockGenerateAdditionalManifestsSuccess := func() {
		mockClusterApi.EXPECT().GenerateAdditionalManifests(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}

	sortedHosts := func(arr []strfmt.UUID) []strfmt.UUID {
		sort.Slice(arr, func(i, j int) bool { return arr[i] < arr[j] })
		return arr
	}

	sortedNetworks := func(arr []*models.HostNetwork) []*models.HostNetwork {
		sort.Slice(arr, func(i, j int) bool { return arr[i].Cidr < arr[j].Cidr })
		return arr
	}

	Context("Get", func() {
		BeforeEach(func() {
			clusterID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.Cluster{Cluster: models.Cluster{
				ID:                 &clusterID,
				OpenshiftVersion:   common.TestDefaultConfig.OpenShiftVersion,
				APIVip:             "10.11.12.13",
				IngressVip:         "10.11.12.14",
				MachineNetworks:    []*models.MachineNetwork{{Cidr: "10.11.0.0/16"}},
				MachineNetworkCidr: "10.11.0.0/16", // TODO MGMT-7365: Deprecate single network
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
			addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
			addHost(masterHostId3, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname2", "bootMode", "1.2.3.6/24", "7.8.9.10/24"), db)
		})

		Context("GetCluster", func() {
			It("success", func() {
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(3) // Number of hosts
				mockDurationsSuccess()
				reply := bm.GetCluster(ctx, installer.GetClusterParams{
					ClusterID: clusterID,
				})
				actual, ok := reply.(*installer.GetClusterOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload.APIVip).To(BeEquivalentTo("10.11.12.13"))
				Expect(actual.Payload.IngressVip).To(BeEquivalentTo("10.11.12.14"))
				validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{{Cidr: "10.11.0.0/16"}})
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

			It("Unfamilliar ID", func() {
				resp := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: "12345"})
				Expect(resp).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
			})

			It("DB inaccessible", func() {
				common.DeleteTestDB(db, dbName)
				resp := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: clusterID})
				Expect(resp).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusInternalServerError, errors.Errorf(""))))
			})
		})

		Context("GetUnregisteredClusters", func() {
			deleteCluster := func(deletePermanently bool) {
				c, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
				Expect(err).ShouldNot(HaveOccurred())

				tempDB := db

				if deletePermanently {
					tempDB = db.Unscoped()
				}

				for _, host := range c.Hosts {
					Expect(tempDB.Delete(host).Error).ShouldNot(HaveOccurred())
				}
				Expect(tempDB.Delete(&c).Error).ShouldNot(HaveOccurred())
			}

			It("success", func() {
				deleteCluster(false)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(3)
				resp := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: clusterID, GetUnregisteredClusters: swag.Bool(true)})
				cluster := resp.(*installer.GetClusterOK).Payload
				Expect(cluster.ID.String()).Should(Equal(clusterID.String()))
				Expect(cluster.TotalHostCount).Should(Equal(int64(3)))
				Expect(cluster.ReadyHostCount).Should(Equal(int64(3)))
				Expect(cluster.EnabledHostCount).Should(Equal(int64(3)))

				expectedMasterHostsIDs := []strfmt.UUID{masterHostId1, masterHostId2, masterHostId3}
				for _, h := range cluster.Hosts {
					Expect(expectedMasterHostsIDs).To(ContainElement(*h.ID))
				}
			})

			It("failure - cluster was permanently deleted", func() {
				deleteCluster(true)
				resp := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: clusterID, GetUnregisteredClusters: swag.Bool(true)})
				Expect(resp).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
			})

			It("failure - not an admin user", func() {
				deleteCluster(false)
				payload := &ocm.AuthPayload{}
				payload.Role = ocm.UserRole
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)
				resp := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: clusterID, GetUnregisteredClusters: swag.Bool(true)})
				Expect(resp).Should(BeAssignableToTypeOf(common.NewInfraError(http.StatusForbidden, errors.Errorf(""))))
			})
		})
	})
	Context("Create non HA cluster", func() {
		BeforeEach(func() {
			mockDurationsSuccess()
		})
		It("happy flow", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil)

			mockClusterRegisterSuccess(bm, true)
			noneHaMode := models.ClusterHighAvailabilityModeNone
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                 swag.String("some-cluster-name"),
					OpenshiftVersion:     swag.String(MinimalOpenShiftVersionForNoneHA),
					PullSecret:           swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
					HighAvailabilityMode: &noneHaMode,
					VipDhcpAllocation:    swag.Bool(true),
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
			actual := reply.(*installer.RegisterClusterCreated)
			Expect(actual.Payload.HighAvailabilityMode).To(Equal(swag.String(noneHaMode)))
			Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
			// verify VipDhcpAllocation was set to false even though it was sent as true
			Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
		})
		It("create non ha cluster fail, release version is lower than minimal", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil)
			noneHaMode := models.ClusterHighAvailabilityModeNone
			insufficientOpenShiftVersionForNoneHA := "4.7"
			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                 swag.String("some-cluster-name"),
					OpenshiftVersion:     swag.String(insufficientOpenShiftVersionForNoneHA),
					PullSecret:           swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
					HighAvailabilityMode: &noneHaMode,
				},
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("create non ha cluster fail, release version is pre-release and lower than minimal", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil)
			noneHaMode := models.ClusterHighAvailabilityModeNone
			insufficientOpenShiftVersionForNoneHA := "4.7.0-fc.1"
			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                 swag.String("some-cluster-name"),
					OpenshiftVersion:     swag.String(insufficientOpenShiftVersionForNoneHA),
					PullSecret:           swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
					HighAvailabilityMode: &noneHaMode,
				},
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("create non ha cluster success, release version is greater than minimal", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil)

			mockClusterRegisterSuccess(bm, true)
			noneHaMode := models.ClusterHighAvailabilityModeNone
			openShiftVersionForNoneHA := "4.8.0"
			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                 swag.String("some-cluster-name"),
					OpenshiftVersion:     swag.String(openShiftVersionForNoneHA),
					PullSecret:           swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
					HighAvailabilityMode: &noneHaMode,
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
			actual := reply.(*installer.RegisterClusterCreated)
			Expect(actual.Payload.HighAvailabilityMode).To(Equal(swag.String(noneHaMode)))
			Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
			// verify VipDhcpAllocation was set to false even though it was sent as true
			Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
		})
		It("create non ha cluster success, release version is pre-release and greater than minimal", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil)

			mockClusterRegisterSuccess(bm, true)
			noneHaMode := models.ClusterHighAvailabilityModeNone
			openShiftVersionForNoneHA := "4.8.0-fc.2"
			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                 swag.String("some-cluster-name"),
					OpenshiftVersion:     swag.String(openShiftVersionForNoneHA),
					PullSecret:           swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
					HighAvailabilityMode: &noneHaMode,
					VipDhcpAllocation:    swag.Bool(true),
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
			actual := reply.(*installer.RegisterClusterCreated)
			Expect(actual.Payload.HighAvailabilityMode).To(Equal(swag.String(noneHaMode)))
			Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
			// verify VipDhcpAllocation was set to false even though it was sent as true
			Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
		})
	})
	It("create non ha cluster success, release version is ci-release and greater than minimal", func() {
		bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
			db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil)

		mockClusterRegisterSuccess(bm, true)
		noneHaMode := models.ClusterHighAvailabilityModeNone
		openShiftVersionForNoneHA := "4.8.0-0.ci.test-2021-05-20-000749-ci-op-7xrzwgwy-latest"
		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                 swag.String("some-cluster-name"),
				OpenshiftVersion:     swag.String(openShiftVersionForNoneHA),
				PullSecret:           swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
				HighAvailabilityMode: &noneHaMode,
				VipDhcpAllocation:    swag.Bool(true),
			},
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
		actual := reply.(*installer.RegisterClusterCreated)
		Expect(actual.Payload.HighAvailabilityMode).To(Equal(swag.String(noneHaMode)))
		Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
		// verify VipDhcpAllocation was set to false even though it was sent as true
		Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
	})

	Context("Update", func() {
		BeforeEach(func() {
			mockDurationsSuccess()
		})
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
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusConflict, errors.Errorf("error"))))
		})

		Context("check pull secret", func() {
			BeforeEach(func() {
				v, err := validations.NewPullSecretValidator(validations.Config{})
				Expect(err).ShouldNot(HaveOccurred())
				bm.secretValidator = v
			})

			It("Invalid pull-secret", func() {
				pullSecret := "asdfasfda"
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						PullSecret: &pullSecret,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf(""))))
			})

			It("pull-secret with newline", func() {
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
				pullSecret := "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}" // #nosec
				pullSecretWithNewline := pullSecret + " \n"
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						PullSecret: &pullSecretWithNewline,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
			})
		})

		It("update role in none ha mode, must fail", func() {
			clusterID = strfmt.UUID(uuid.New().String())
			noneHaMode := models.ClusterHighAvailabilityModeNone
			err := db.Create(&common.Cluster{Cluster: models.Cluster{
				ID:                   &clusterID,
				HighAvailabilityMode: &noneHaMode,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())
			mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
			addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
			testRole := models.ClusterUpdateParamsHostsRolesItems0{ID: masterHostId1, Role: models.HostRoleUpdateParamsMaster}
			reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{&testRole},
				},
			})
			verifyApiError(reply, http.StatusBadRequest)
		})

		It("update cluster day1 with APIVipDNSName failed", func() {
			mockOperators := operators.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, mockEvents, nil, nil, nil, nil, mockOperators, nil, nil, nil)

			mockClusterRegisterSuccess(bm, true)

			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("some-cluster-name"),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			actual := reply.(*installer.RegisterClusterCreated)
			c, err := bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())

			newClusterName := "day1-cluster-new-name"

			reply = bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: *c.ID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					Name:          &newClusterName,
					APIVipDNSName: swag.String("some dns name"),
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
		})

		It("cluster update failure on inventory refresh failure", func() {
			mockOperators := operators.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, mockEvents, nil, nil, nil, nil, mockOperators, nil, nil, nil)

			mockClusterRegisterSuccess(bm, true)

			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("some-cluster-name"),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			actual := reply.(*installer.RegisterClusterCreated)

			clusterId := *actual.Payload.ID
			addHost(strfmt.UUID(uuid.New().String()), models.HostRoleMaster, "known", models.HostKindHost, clusterId, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
			newClusterName := "new-cluster-name"

			refreshError := errors.New("boom!")
			mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(refreshError)

			reply = bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: clusterId,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					Name: &newClusterName,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(reply.(*common.ApiErrorResponse).Error()).To(BeEquivalentTo(refreshError.Error()))
		})

		Context("Monitored Operators", func() {
			var (
				testOLMOperators = []*models.MonitoredOperator{
					{
						Name:           "0",
						OperatorType:   models.OperatorTypeOlm,
						TimeoutSeconds: 1000,
					},
					{
						Name:           "1",
						OperatorType:   models.OperatorTypeOlm,
						TimeoutSeconds: 2000,
					},
				}
			)

			mockGetOperatorByName := func(operatorName string) {
				testOLMOperatorIndex, err := strconv.Atoi(operatorName)
				Expect(err).ShouldNot(HaveOccurred())

				mockOperatorManager.EXPECT().GetOperatorByName(operatorName).Return(
					&models.MonitoredOperator{
						Name:             testOLMOperators[testOLMOperatorIndex].Name,
						OperatorType:     testOLMOperators[testOLMOperatorIndex].OperatorType,
						TimeoutSeconds:   testOLMOperators[testOLMOperatorIndex].TimeoutSeconds,
						Namespace:        testOLMOperators[testOLMOperatorIndex].Namespace,
						SubscriptionName: testOLMOperators[testOLMOperatorIndex].SubscriptionName,
					}, nil).Times(1)
			}

			Context("RegisterCluster", func() {
				BeforeEach(func() {
					bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
						db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil)
				})

				It("OLM register default value - only builtins", func() {
					mockClusterRegisterSuccess(bm, true)

					reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
						NewClusterParams: &models.ClusterCreateParams{
							Name:             swag.String("some-cluster-name"),
							OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
							PullSecret:       swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
						},
					})
					Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
					actual := reply.(*installer.RegisterClusterCreated)
					Expect(actual.Payload.MonitoredOperators).To(ContainElement(&common.TestDefaultConfig.MonitoredOperator))
				})

				It("OLM register non default value", func() {
					newOperatorName := testOLMOperators[0].Name
					newProperties := "blob-info"

					mockClusterRegisterSuccess(bm, true)
					mockGetOperatorByName(newOperatorName)
					mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any()).
						DoAndReturn(func(operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
							return operators, nil
						}).Times(1)

					reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
						NewClusterParams: &models.ClusterCreateParams{
							Name:             swag.String("some-cluster-name"),
							OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
							PullSecret:       swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
							OlmOperators: []*models.OperatorCreateParams{
								{Name: newOperatorName, Properties: newProperties},
							},
						},
					})
					Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
					actual := reply.(*installer.RegisterClusterCreated)

					expectedMonitoredOperator := models.MonitoredOperator{
						Name:             newOperatorName,
						Properties:       newProperties,
						OperatorType:     testOLMOperators[0].OperatorType,
						TimeoutSeconds:   testOLMOperators[0].TimeoutSeconds,
						Namespace:        testOLMOperators[0].Namespace,
						SubscriptionName: testOLMOperators[0].SubscriptionName,
						ClusterID:        *actual.Payload.ID,
					}

					Expect(actual.Payload.MonitoredOperators).To(ContainElement(&common.TestDefaultConfig.MonitoredOperator))
					Expect(actual.Payload.MonitoredOperators).To(ContainElement(&expectedMonitoredOperator))
				})

				It("Resolve OLM dependencies", func() {
					newOperatorName := testOLMOperators[1].Name

					mockClusterRegisterSuccess(bm, true)
					mockGetOperatorByName(newOperatorName)
					mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any()).
						DoAndReturn(func(operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
							return append(operators, testOLMOperators[0]), nil
						}).Times(1)

					reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
						NewClusterParams: &models.ClusterCreateParams{
							Name:             swag.String("some-cluster-name"),
							OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
							PullSecret:       swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
							OlmOperators: []*models.OperatorCreateParams{
								{Name: newOperatorName},
							},
						},
					})
					Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
					actual := reply.(*installer.RegisterClusterCreated)

					expectedUpdatedMonitoredOperator := models.MonitoredOperator{
						Name:             newOperatorName,
						OperatorType:     testOLMOperators[1].OperatorType,
						TimeoutSeconds:   testOLMOperators[1].TimeoutSeconds,
						Namespace:        testOLMOperators[1].Namespace,
						SubscriptionName: testOLMOperators[1].SubscriptionName,
						ClusterID:        *actual.Payload.ID,
					}

					expectedResolvedMonitoredOperator := models.MonitoredOperator{
						Name:             testOLMOperators[0].Name,
						OperatorType:     testOLMOperators[0].OperatorType,
						TimeoutSeconds:   testOLMOperators[0].TimeoutSeconds,
						Namespace:        testOLMOperators[0].Namespace,
						SubscriptionName: testOLMOperators[0].SubscriptionName,
						ClusterID:        *actual.Payload.ID,
					}

					Expect(actual.Payload.MonitoredOperators).To(ContainElements(
						&common.TestDefaultConfig.MonitoredOperator,
						&expectedUpdatedMonitoredOperator,
						&expectedResolvedMonitoredOperator,
					))
				})

				It("OLM invalid name", func() {
					newOperatorName := "invalid-name"

					mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(1)
					mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
					mockOperatorManager.EXPECT().GetOperatorByName(newOperatorName).Return(nil, errors.Errorf("error")).Times(1)

					reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
						NewClusterParams: &models.ClusterCreateParams{
							Name:             swag.String("some-cluster-name"),
							OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
							PullSecret:       swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
							OlmOperators: []*models.OperatorCreateParams{
								{Name: newOperatorName},
							},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
				})
			})

			Context("UpdateCluster", func() {
				var (
					defaultProperties = "properties"
				)

				tests := []struct {
					name              string
					originalOperators []*models.MonitoredOperator
					updateOperators   []*models.OperatorCreateParams
					expectedOperators []*models.MonitoredOperator
				}{
					{
						name:              "No operators",
						originalOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator, testOLMOperators[0]},
						updateOperators:   nil,
						expectedOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator, testOLMOperators[0]},
					},
					{
						name:              "Reset list of operators",
						originalOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator, testOLMOperators[0]},
						updateOperators:   []*models.OperatorCreateParams{},
						expectedOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator},
					},
					{
						name:              "Update properties - set",
						originalOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator, testOLMOperators[0]},
						updateOperators:   []*models.OperatorCreateParams{{Name: testOLMOperators[0].Name, Properties: defaultProperties}},
						expectedOperators: []*models.MonitoredOperator{
							&common.TestDefaultConfig.MonitoredOperator,
							{
								Name: testOLMOperators[0].Name, OperatorType: testOLMOperators[0].OperatorType,
								TimeoutSeconds: testOLMOperators[0].TimeoutSeconds, Properties: defaultProperties,
							},
						},
					},
					{
						name: "Update properties - unset",
						originalOperators: []*models.MonitoredOperator{
							&common.TestDefaultConfig.MonitoredOperator,
							{
								Name: testOLMOperators[0].Name, OperatorType: testOLMOperators[0].OperatorType,
								TimeoutSeconds: testOLMOperators[0].TimeoutSeconds, Properties: defaultProperties,
							},
						},
						updateOperators: []*models.OperatorCreateParams{{Name: testOLMOperators[0].Name}},
						expectedOperators: []*models.MonitoredOperator{
							&common.TestDefaultConfig.MonitoredOperator,
							{
								Name: testOLMOperators[0].Name, OperatorType: testOLMOperators[0].OperatorType,
								TimeoutSeconds: testOLMOperators[0].TimeoutSeconds, Properties: "",
							},
						},
					},
					{
						name: "Add new operator to list",
						originalOperators: []*models.MonitoredOperator{
							&common.TestDefaultConfig.MonitoredOperator,
							{
								Name: testOLMOperators[0].Name, OperatorType: testOLMOperators[0].OperatorType,
								TimeoutSeconds: testOLMOperators[0].TimeoutSeconds, Properties: defaultProperties,
							},
						},
						updateOperators: []*models.OperatorCreateParams{
							{Name: testOLMOperators[0].Name, Properties: defaultProperties},
							{Name: testOLMOperators[1].Name, Properties: defaultProperties + "2"},
						},
						expectedOperators: []*models.MonitoredOperator{
							&common.TestDefaultConfig.MonitoredOperator,
							{
								Name: testOLMOperators[0].Name, OperatorType: testOLMOperators[0].OperatorType,
								TimeoutSeconds: testOLMOperators[0].TimeoutSeconds, Properties: defaultProperties,
							},
							{
								Name: testOLMOperators[1].Name, OperatorType: testOLMOperators[1].OperatorType,
								TimeoutSeconds: testOLMOperators[1].TimeoutSeconds, Properties: defaultProperties + "2",
							},
						},
					},
					{
						name:              "Remove operator from list",
						originalOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator, testOLMOperators[0], testOLMOperators[1]},
						updateOperators:   []*models.OperatorCreateParams{{Name: testOLMOperators[1].Name}},
						expectedOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator, testOLMOperators[1]},
					},
				}

				for i := range tests {
					test := tests[i]
					It(test.name, func() {
						// Setup
						clusterID = strfmt.UUID(uuid.New().String())

						for _, operator := range test.originalOperators {
							operator.ClusterID = clusterID
						}
						for _, operator := range test.expectedOperators {
							operator.ClusterID = clusterID
						}

						err := db.Create(&common.Cluster{Cluster: models.Cluster{
							ID:                 &clusterID,
							MonitoredOperators: test.originalOperators,
						}}).Error
						Expect(err).ShouldNot(HaveOccurred())

						// Update
						mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
						mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
						mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)

						for _, updateOperator := range test.updateOperators {
							mockGetOperatorByName(updateOperator.Name)
						}
						if test.updateOperators != nil {
							mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any()).
								DoAndReturn(func(operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
									return operators, nil
								}).Times(1)
						}

						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								OlmOperators: test.updateOperators,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
						actual := reply.(*installer.UpdateClusterCreated)
						Expect(actual.Payload.MonitoredOperators).To(HaveLen(len(test.expectedOperators)))
						Expect(actual.Payload.MonitoredOperators).To(ContainElements(test.expectedOperators))
					})
				}
			})

			It("Resolve OLM dependencies", func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())

				// Update
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)

				newOperatorName := testOLMOperators[1].Name

				mockGetOperatorByName(newOperatorName)
				mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any()).
					DoAndReturn(func(operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
						return append(operators, testOLMOperators[0]), nil
					}).Times(1)

				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: newOperatorName},
						},
					},
				})

				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
				actual := reply.(*installer.UpdateClusterCreated)

				expectedUpdatedMonitoredOperator := models.MonitoredOperator{
					Name:             newOperatorName,
					OperatorType:     testOLMOperators[1].OperatorType,
					TimeoutSeconds:   testOLMOperators[1].TimeoutSeconds,
					Namespace:        testOLMOperators[1].Namespace,
					SubscriptionName: testOLMOperators[1].SubscriptionName,
					ClusterID:        *actual.Payload.ID,
				}

				expectedResolvedMonitoredOperator := models.MonitoredOperator{
					Name:             testOLMOperators[0].Name,
					OperatorType:     testOLMOperators[0].OperatorType,
					TimeoutSeconds:   testOLMOperators[0].TimeoutSeconds,
					Namespace:        testOLMOperators[0].Namespace,
					SubscriptionName: testOLMOperators[0].SubscriptionName,
					ClusterID:        *actual.Payload.ID,
				}

				Expect(actual.Payload.MonitoredOperators).To(ContainElements(
					&expectedUpdatedMonitoredOperator,
					&expectedResolvedMonitoredOperator,
				))
			})

			It("OLM invalid name", func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())

				newOperatorName := "invalid-name"

				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
				mockOperatorManager.EXPECT().GetOperatorByName(newOperatorName).Return(nil, errors.Errorf("error")).Times(1)
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: newOperatorName},
						},
					},
				})
				Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
			})
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
				"IfNyDiU2JR50r1jCxj5H76QxIuM= root@ocp-edge34.lab.eng.tlv2.redhat.com"
			sshKeyWithNewLine := sshKey + " \n"

			mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
			mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
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
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
		})

		It("Update SchedulableMasters", func() {

			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
			clusterID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.Cluster{Cluster: models.Cluster{
				ID: &clusterID,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())
			mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
			mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
			reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					SchedulableMasters: swag.Bool(true),
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
			actual := reply.(*installer.UpdateClusterCreated)
			Expect(actual.Payload.SchedulableMasters).To(Equal(swag.Bool(true)))
		})

		Context("Update Proxy", func() {
			//const emptyProxyHash = "d41d8cd98f00b204e9800998ecf8427e"
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
					}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
			})

			updateCluster := func(httpProxy, httpsProxy, noProxy string) *common.Cluster {
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
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

			It("set a valid proxy", func() {
				mockEvents.EXPECT().AddEvent(gomock.Any(), clusterID, nil, models.EventSeverityInfo, "Proxy settings changed", gomock.Any())
				_ = updateCluster("http://proxy.proxy", "", "proxy.proxy")
			})
		})

		Context("Hostname", func() {
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("1.2.3.4/24", "10.11.50.90/16"), db)
				err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: &clusterID}).UpdateColumn("free_addresses",
					makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16"))).Error
				Expect(err).ToNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
			})
			It("Valid hostname", func() {
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Cluster{}, nil).Times(1)
				mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
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
				mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
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

		Context("MachineConfigPoolName", func() {
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("1.2.3.4/24", "10.11.50.90/16"), db)
				err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: &clusterID}).UpdateColumn("free_addresses",
					makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16"))).Error
				Expect(err).ToNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
			})
			It("Valid machine config pool name", func() {
				mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Cluster{}, nil).Times(1)
				mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						HostsMachineConfigPoolNames: []*models.ClusterUpdateParamsHostsMachineConfigPoolNamesItems0{
							{
								MachineConfigPoolName: "new_pool",
								ID:                    masterHostId1,
							},
						},
					}})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
			})
		})

		Context("Installation Disk Path", func() {
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
				}}).Error
				addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
				Expect(err).ShouldNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
			})

			It("Valid selection of install disk", func() {
				mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Cluster{}, nil).Times(1)
				mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						DisksSelectedConfig: []*models.ClusterUpdateParamsDisksSelectedConfigItems0{
							{
								DisksConfig: []*models.DiskConfigParams{
									{ID: &diskID1, Role: models.DiskRoleInstall},
									{ID: &diskID2, Role: models.DiskRoleNone},
								},
								ID: masterHostId1,
							},
						},
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
			})

			It("duplicate install selected", func() {
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						DisksSelectedConfig: []*models.ClusterUpdateParamsDisksSelectedConfigItems0{
							{
								DisksConfig: []*models.DiskConfigParams{
									{ID: &diskID1, Role: models.DiskRoleInstall},
									{ID: &diskID2, Role: models.DiskRoleInstall},
								},
								ID: masterHostId1,
							},
						},
					},
				})
				verifyApiError(reply, http.StatusConflict)
			})
		})

		Context("Day2 api vip dnsname/ip", func() {
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID:   &clusterID,
					Kind: swag.String(models.ClusterKindAddHostsCluster),
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
			})
			It("update api vip dnsname success", func() {
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Cluster{}, nil).Times(1)
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						APIVipDNSName: swag.String("some dns name"),
					}})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
			})
		})

		Context("Day2 update hostname", func() {
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID:   &clusterID,
					Kind: swag.String(models.ClusterKindAddHostsCluster),
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindAddToExistingClusterHost, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
				addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindAddToExistingClusterHost, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
			})
			It("update hostname, all in known", func() {
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(2)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(2)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Cluster{}, nil).Times(1)
				mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(2)
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
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
			It("update hostname, one host in installing", func() {
				addHost(masterHostId3, models.HostRoleMaster, "added-to-existing-cluster", models.HostKindAddToExistingClusterHost, clusterID, getInventoryStr("hostname3", "bootMode", "1.2.3.6/24", "10.11.50.70/16"), db)
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(3)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Cluster{}, nil).Times(1)
				mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
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
		})

		Context("Update Network", func() {
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
				addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
				addHost(masterHostId3, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname2", "bootMode", "1.2.3.6/24", "7.8.9.10/24"), db)
				err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: &clusterID}).UpdateColumn("free_addresses",
					makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16"))).Error
				Expect(err).ToNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(1)
			})

			mockSuccess := func(times int) {
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(times * 3) // Number of hosts
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(times * 3)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(times * 1)
				mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(times * 3)
				mockSetConnectivityMajorityGroupsForClusterTimes(mockClusterApi, times)
			}

			Context("Single node", func() {
				BeforeEach(func() {
					Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Updates(map[string]interface{}{
						"user_managed_networking": true,
						"high_availability_mode":  models.ClusterHighAvailabilityModeNone,
					}).Error).ShouldNot(HaveOccurred())
				})

				It("Fail to unset UserManagedNetworking", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
						},
					})

					verifyApiErrorString(reply, http.StatusBadRequest, "disabling UserManagedNetworking is not allowed in single node mode")
				})

				It("Set Machine CIDR", func() {
					Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Updates(map[string]interface{}{
						"api_vip":     common.TestIPv4Networking.APIVip,
						"ingress_vip": common.TestIPv4Networking.IngressVip,
					}).Error).ShouldNot(HaveOccurred())

					mockSuccess(1)

					machineNetworks := common.TestIPv4Networking.MachineNetworks
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							MachineNetworks: machineNetworks,
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
					Expect(actual.Payload.APIVip).To(Equal(""))
					Expect(actual.Payload.IngressVip).To(Equal(""))
					validateNetworkConfiguration(actual.Payload, nil, nil, &machineNetworks)
				})

				It("Fail with bad Machine CIDR", func() {
					badMachineCidr := "2.2.3.128/24"
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							MachineNetworks: []*models.MachineNetwork{{Cidr: models.Subnet(badMachineCidr)}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("%s is not a valid network CIDR", badMachineCidr))
				})

				It("Success with machine cidr that is not part of cluster networks", func() {
					mockSuccess(1)
					wrongMachineCidrNetworks := []*models.MachineNetwork{{Cidr: models.Subnet("2.2.3.0/24")}}

					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							MachineNetworks: wrongMachineCidrNetworks,
						},
					})

					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
					Expect(actual.Payload.APIVip).To(Equal(""))
					Expect(actual.Payload.IngressVip).To(Equal(""))
					validateNetworkConfiguration(actual.Payload, nil, nil, &wrongMachineCidrNetworks)
				})
			})

			Context("UserManagedNetworking", func() {
				It("success", func() {
					mockSuccess(1)
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
				})

				It("Unset non relevant parameters", func() {
					mockSuccess(1)

					Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Updates(map[string]interface{}{
						"api_vip":     common.TestIPv4Networking.APIVip,
						"ingress_vip": common.TestIPv4Networking.IngressVip,
					}).Error).ShouldNot(HaveOccurred())
					Expect(db.Model(&models.MachineNetwork{}).Save(
						&models.MachineNetwork{Cidr: common.TestIPv4Networking.MachineNetworks[0].Cidr, ClusterID: clusterID}).Error).ShouldNot(HaveOccurred())

					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
					Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
					Expect(actual.Payload.APIVip).To(Equal(""))
					Expect(actual.Payload.IngressVip).To(Equal(""))
					validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{})
				})

				It("Fail with DHCP", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							VipDhcpAllocation:     swag.Bool(true),
						},
					})

					verifyApiErrorString(reply, http.StatusBadRequest, "VIP DHCP Allocation cannot be enabled with User Managed Networking")
				})

				It("Fail with DHCP when UserManagedNetworking was set", func() {
					Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update("user_managed_networking", true).Error).ShouldNot(HaveOccurred())

					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							VipDhcpAllocation: swag.Bool(true),
						},
					})

					verifyApiErrorString(reply, http.StatusBadRequest, "VIP DHCP Allocation cannot be enabled with User Managed Networking")
				})

				It("Fail with Ingress VIP", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							IngressVip:            swag.String("10.35.20.10"),
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Ingress VIP cannot be set with User Managed Networking")
				})

				It("Fail with API VIP", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							APIVip:                swag.String("10.35.20.10"),
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "API VIP cannot be set with User Managed Networking")
				})

				It("Fail with Machine CIDR", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							MachineNetworks:       []*models.MachineNetwork{{Cidr: "10.11.0.0/16"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Machine Network CIDR cannot be set with User Managed Networking")
				})

				It("Fail with non-x86_64 CPU architecture", func() {
					clusterID = strfmt.UUID(uuid.New().String())
					err := db.Create(&common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterID,
						CPUArchitecture:       "arm64",
						UserManagedNetworking: swag.Bool(true),
					}}).Error
					Expect(err).ShouldNot(HaveOccurred())

					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
						},
					})

					verifyApiErrorString(reply, http.StatusBadRequest, "disabling User Managed Networking is not allowed for clusters with non-x86_64 CPU architecture")
				})
			})

			It("NetworkType", func() {
				mockSuccess(1)
				networkType := "OpenShiftSDN"
				reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.ClusterUpdateParams{
						NetworkType: &networkType,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
				actual := reply.(*installer.UpdateClusterCreated)
				Expect(actual.Payload.NetworkType).To(Equal(&networkType))
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
					verifyApiErrorString(reply, http.StatusBadRequest,
						fmt.Sprintf("Calculate machine network CIDR: No suitable matching CIDR found for VIP %s", apiVip))
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
					verifyApiErrorString(reply, http.StatusBadRequest,
						"ingress-vip <1.2.3.20> does not belong to machine-network-cidr <10.11.0.0/16>")
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
					verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("api-vip and ingress-vip cannot have the same value: %s", apiVip))
				})
				It("Bad apiVip ip", func() {
					apiVip := "not.an.ip.test"
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							APIVip: &apiVip,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("Could not parse VIP ip %s", apiVip))
				})
				It("Bad ingressVip ip", func() {
					ingressVip := "not.an.ip.test"
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							IngressVip: &ingressVip,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("Could not parse VIP ip %s", ingressVip))
				})
				It("Update success", func() {
					mockSuccess(1)

					apiVip := "10.11.12.15"
					ingressVip := "10.11.12.16"
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
					validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{{Cidr: "10.11.0.0/16"}})
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
							APIVip:          &apiVip,
							IngressVip:      &ingressVip,
							MachineNetworks: []*models.MachineNetwork{{Cidr: "10.12.0.0/16"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						"Setting Machine network CIDR is forbidden when cluster is not in vip-dhcp-allocation mode")
				})
			})
			Context("Advanced networking validations", func() {

				var (
					apiVip     = "10.11.12.15"
					ingressVip = "10.11.12.16"
				)

				It("Update success", func() {
					mockSuccess(1)

					clusterNetworks := []*models.ClusterNetwork{{Cidr: "192.168.0.0/21", HostPrefix: 23}}
					serviceNetworks := []*models.ServiceNetwork{{Cidr: "193.168.5.0/24"}}

					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							APIVip:          &apiVip,
							IngressVip:      &ingressVip,
							ClusterNetworks: clusterNetworks,
							ServiceNetworks: serviceNetworks,
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					Expect(actual.Payload.APIVip).To(Equal(apiVip))
					Expect(actual.Payload.IngressVip).To(Equal(ingressVip))

					validateNetworkConfiguration(actual.Payload,
						&clusterNetworks, &serviceNetworks, &[]*models.MachineNetwork{{Cidr: "10.11.0.0/16"}})

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
				Context("Overlapping", func() {
					It("not part of update", func() {
						cidr := models.Subnet("1.2.0.0/16")
						Expect(db.Model(&models.ClusterNetwork{}).Save(
							&models.ClusterNetwork{ClusterID: clusterID, Cidr: cidr, HostPrefix: 20}).Error).ShouldNot(HaveOccurred())
						Expect(db.Model(&models.ServiceNetwork{}).Save(
							&models.ServiceNetwork{ClusterID: clusterID, Cidr: cidr}).Error).ShouldNot(HaveOccurred())
						Expect(db.Model(&models.MachineNetwork{}).Save(
							&models.MachineNetwork{ClusterID: clusterID, Cidr: cidr}).Error).ShouldNot(HaveOccurred())

						mockSuccess(1)
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								UserManagedNetworking: swag.Bool(false),
							},
						})

						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					})
					It("part of update", func() {
						Expect(db.Model(&models.ClusterNetwork{}).Save(
							&models.ClusterNetwork{ClusterID: clusterID, Cidr: models.Subnet("1.3.0.0/16"), HostPrefix: 20}).Error).ShouldNot(HaveOccurred())
						Expect(db.Model(&models.ServiceNetwork{}).Save(
							&models.ServiceNetwork{ClusterID: clusterID, Cidr: models.Subnet("1.2.0.0/16")}).Error).ShouldNot(HaveOccurred())
						Expect(db.Model(&models.MachineNetwork{}).Save(
							&models.MachineNetwork{ClusterID: clusterID, Cidr: models.Subnet("1.4.0.0/16")}).Error).ShouldNot(HaveOccurred())

						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								ServiceNetworks: []*models.ServiceNetwork{{Cidr: "1.3.5.0/24"}},
							},
						})

						verifyApiErrorString(reply, http.StatusBadRequest, "CIDRS 1.3.5.0/24 and 1.3.0.0/16 overlap")
					})
				})
				It("Prefix out of range", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							APIVip:          &apiVip,
							IngressVip:      &ingressVip,
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: "192.168.5.0/24", HostPrefix: 26}},
							ServiceNetworks: []*models.ServiceNetwork{{Cidr: "193.168.4.0/23"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						"Host prefix, now 26, must be less than or equal to 25 to allow at least 128 addresses")
				})
				It("Subnet prefix out of range", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							APIVip:          &apiVip,
							IngressVip:      &ingressVip,
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: "192.168.0.0/23", HostPrefix: 25}},
							ServiceNetworks: []*models.ServiceNetwork{{Cidr: "193.168.4.0/27"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						"Service network CIDR 193.168.4.0/27: Address mask size must be between 1 to 25 and must include at least 128 addresses")
				})
				It("Bad subnet", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							APIVip:          &apiVip,
							IngressVip:      &ingressVip,
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: "1.168.0.0/23", HostPrefix: 23}},
							ServiceNetworks: []*models.ServiceNetwork{{Cidr: "193.168.4.0/1"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						"Cluster network CIDR prefix 23 does not contain enough addresses for 3 hosts each one with 23 prefix (512 addresses)")
				})
				It("Not enough addresses", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							APIVip:          &apiVip,
							IngressVip:      &ingressVip,
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: "192.168.0.0/23", HostPrefix: 24}},
							ServiceNetworks: []*models.ServiceNetwork{{Cidr: "193.168.4.0/25"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						"Cluster network CIDR prefix 23 does not contain enough addresses for 3 hosts each one with 24 prefix (256 addresses)")
				})
			})
			Context("DHCP", func() {

				var (
					apiVip             = "10.11.12.15"
					ingressVip         = "10.11.12.16"
					primaryMachineCIDR = models.Subnet("10.11.0.0/16")
				)

				It("Vips in DHCP", func() {
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							APIVip:            &apiVip,
							IngressVip:        &ingressVip,
							VipDhcpAllocation: swag.Bool(true),
							MachineNetworks:   []*models.MachineNetwork{{Cidr: primaryMachineCIDR}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Setting API VIP is forbidden when cluster is in vip-dhcp-allocation mode")
				})

				It("Success in DHCP", func() {
					mockSuccess(3)
					mockClusterApi.EXPECT().VerifyClusterUpdatability(gomock.Any()).Return(nil).Times(2)

					By("Original machine cidr", func() {
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
						validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{{Cidr: primaryMachineCIDR}})
					})

					By("Override machine cidr", func() {
						machineNetworks := common.TestIPv4Networking.MachineNetworks
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								VipDhcpAllocation: swag.Bool(true),
								MachineNetworks:   machineNetworks,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
						actual := reply.(*installer.UpdateClusterCreated)
						Expect(actual.Payload.APIVip).To(BeEmpty())
						Expect(actual.Payload.IngressVip).To(BeEmpty())
						validateNetworkConfiguration(actual.Payload, nil, nil, &machineNetworks)

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

					By("Turn off DHCP allocation", func() {
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								APIVip:            &apiVip,
								IngressVip:        &ingressVip,
								VipDhcpAllocation: swag.Bool(false),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
						actual := reply.(*installer.UpdateClusterCreated)
						Expect(actual.Payload.APIVip).To(Equal(apiVip))
						Expect(actual.Payload.IngressVip).To(Equal(ingressVip))
						validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{{Cidr: primaryMachineCIDR}})

					})
				})
				It("DHCP non existent network (no error)", func() {
					mockSuccess(1)
					machineNetworks := []*models.MachineNetwork{{Cidr: "10.13.0.0/16"}}

					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							MachineNetworks:   machineNetworks,
							VipDhcpAllocation: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					validateNetworkConfiguration(actual.Payload, nil, nil, &machineNetworks)
				})

				Context("IPv6", func() {
					var (
						machineNetworks = []*models.MachineNetwork{{Cidr: "2001:db8::/64"}}
					)

					BeforeEach(func() {
						for _, network := range machineNetworks {
							network.ClusterID = clusterID
						}
					})

					It("Fail to set IPv6 machine CIDR when VIP DHCP is true", func() {
						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								MachineNetworks:   machineNetworks,
								VipDhcpAllocation: swag.Bool(true),
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "VIP DHCP allocation is unsupported with IPv6 network")
					})

					It("Fail to set IPv6 machine CIDR when VIP DHCP was true", func() {
						Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update("vip_dhcp_allocation", true).Error).ShouldNot(HaveOccurred())

						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								MachineNetworks: machineNetworks,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "VIP DHCP allocation is unsupported with IPv6 network")
					})

					It("Set VIP DHCP true when machine CIDR was IPv6", func() {
						mockSuccess(1)

						Expect(db.Model(&models.MachineNetwork{}).Save(machineNetworks[0]).Error).ShouldNot(HaveOccurred())

						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								VipDhcpAllocation: swag.Bool(true),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
						actual := reply.(*installer.UpdateClusterCreated)
						Expect(actual.Payload.VipDhcpAllocation).NotTo(BeNil())
						Expect(*actual.Payload.VipDhcpAllocation).To(BeTrue())
						validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{})
					})
				})

				Context("Dual-stack", func() {
					var (
						machineNetworks = []*models.MachineNetwork{{Cidr: "10.13.0.0/16"}, {Cidr: "2001:db8::/64"}}
					)

					BeforeEach(func() {
						for _, network := range machineNetworks {
							network.ClusterID = clusterID
						}
					})

					It("Allow setting dual-stack machine CIDRs when VIP DHCP is true and IPv4 is the first one", func() {
						mockSuccess(1)

						Expect(db.Model(&models.MachineNetwork{}).Save(machineNetworks[0]).Error).ShouldNot(HaveOccurred())
						Expect(db.Model(&models.MachineNetwork{}).Save(machineNetworks[1]).Error).ShouldNot(HaveOccurred())

						reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.ClusterUpdateParams{
								MachineNetworks:   machineNetworks,
								VipDhcpAllocation: swag.Bool(true),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
						actual := reply.(*installer.UpdateClusterCreated)
						Expect(actual.Payload.VipDhcpAllocation).NotTo(BeNil())
						Expect(*actual.Payload.VipDhcpAllocation).To(BeTrue())
						validateNetworkConfiguration(actual.Payload, nil, nil, &machineNetworks)
					})
				})
			})

			Context("NTP", func() {
				It("Empty NTP source", func() {
					mockSuccess(1)

					ntpSource := ""
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							AdditionalNtpSource: &ntpSource,
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					Expect(actual.Payload.AdditionalNtpSource).To(Equal(ntpSource))
				})

				It("Valid IP NTP source", func() {
					mockSuccess(1)

					ntpSource := "1.1.1.1"
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							AdditionalNtpSource: &ntpSource,
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					Expect(actual.Payload.AdditionalNtpSource).To(Equal(ntpSource))
				})

				It("Valid Hostname NTP source", func() {
					mockSuccess(1)

					ntpSource := "clock.redhat.com"
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							AdditionalNtpSource: &ntpSource,
						},
					})

					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					Expect(actual.Payload.AdditionalNtpSource).To(Equal(ntpSource))
				})

				It("Valid comma-separated NTP sources", func() {
					mockSuccess(1)

					ntpSource := "clock.redhat.com,1.1.1.1"
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							AdditionalNtpSource: &ntpSource,
						},
					})

					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					Expect(actual.Payload.AdditionalNtpSource).To(Equal(ntpSource))
				})

				It("Invalid NTP source", func() {
					ntpSource := "inject'"
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							AdditionalNtpSource: &ntpSource,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("Invalid NTP source: %s", ntpSource))
				})
			})

			Context("Networks", func() {
				var (
					clusterNetworks = []*models.ClusterNetwork{{Cidr: "1.1.0.0/24", HostPrefix: 24}, {Cidr: "2.2.0.0/24", HostPrefix: 24}}
					serviceNetworks = []*models.ServiceNetwork{{Cidr: "3.3.0.0/24"}, {Cidr: "4.4.0.0/24"}}
					machineNetworks = []*models.MachineNetwork{{Cidr: "5.5.0.0/24"}, {Cidr: "6.6.0.0/24"}}
				)

				setNetworksClusterID := func(clusterID strfmt.UUID,
					clusterNetworks []*models.ClusterNetwork,
					serviceNetworks []*models.ServiceNetwork,
					machineNetworks []*models.MachineNetwork,
				) {
					for _, network := range clusterNetworks {
						network.ClusterID = clusterID
						Expect(db.Model(&models.ClusterNetwork{}).Save(network).Error).ShouldNot(HaveOccurred())
					}
					for _, network := range serviceNetworks {
						network.ClusterID = clusterID
						Expect(db.Model(&models.ServiceNetwork{}).Save(network).Error).ShouldNot(HaveOccurred())
					}
					for _, network := range machineNetworks {
						network.ClusterID = clusterID
						Expect(db.Model(&models.MachineNetwork{}).Save(network).Error).ShouldNot(HaveOccurred())
					}

					// TODO MGMT-7365: Deprecate single network
					Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Updates(map[string]interface{}{
						"cluster_network_cidr":        clusterNetworks[0].Cidr,
						"cluster_network_host_prefix": clusterNetworks[0].HostPrefix,
						"machine_network_cidr":        machineNetworks[0].Cidr,
						"service_network_cidr":        serviceNetworks[0].Cidr,
					}).Error).ShouldNot(HaveOccurred())

					cluster, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
					Expect(err).ToNot(HaveOccurred())
					validateNetworkConfiguration(&cluster.Cluster, &clusterNetworks, &serviceNetworks, &machineNetworks)
				}

				BeforeEach(func() {
					setNetworksClusterID(clusterID, clusterNetworks, serviceNetworks, machineNetworks)
				})

				It("No new networks data", func() {
					mockSuccess(1)
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID:           clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)

					validateNetworkConfiguration(actual.Payload, &clusterNetworks, &serviceNetworks, &machineNetworks)
				})

				It("Empty networks", func() {
					mockSuccess(1)
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							ClusterNetworks: []*models.ClusterNetwork{},
							ServiceNetworks: []*models.ServiceNetwork{},
							MachineNetworks: []*models.MachineNetwork{},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)

					Expect(actual.Payload.ClusterNetworks).To(BeEmpty())
					Expect(actual.Payload.ServiceNetworks).To(BeEmpty())
					Expect(actual.Payload.MachineNetworks).To(BeEmpty())
				})

				It("Override networks", func() {
					clusterNetworks = []*models.ClusterNetwork{{Cidr: "11.11.0.0/21", HostPrefix: 24}, {Cidr: "12.12.0.0/21", HostPrefix: 24}}
					serviceNetworks = []*models.ServiceNetwork{{Cidr: "13.13.0.0/21"}, {Cidr: "14.14.0.0/21"}}
					machineNetworks = []*models.MachineNetwork{{Cidr: "15.15.0.0/21"}, {Cidr: "16.16.0.0/21"}}

					mockSuccess(1)
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							ClusterNetworks:   clusterNetworks,
							ServiceNetworks:   serviceNetworks,
							MachineNetworks:   machineNetworks,
							VipDhcpAllocation: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)

					validateNetworkConfiguration(actual.Payload, &clusterNetworks, &serviceNetworks, &machineNetworks)
				})

				It("Multiple clusters", func() {
					secondClusterID := strfmt.UUID(uuid.New().String())
					Expect(db.Create(&common.Cluster{Cluster: models.Cluster{ID: &secondClusterID}}).Error).ShouldNot(HaveOccurred())
					setNetworksClusterID(secondClusterID, clusterNetworks, serviceNetworks, machineNetworks)

					mockSuccess(1)
					reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.ClusterUpdateParams{
							ClusterNetworks:   clusterNetworks,
							ServiceNetworks:   serviceNetworks,
							MachineNetworks:   machineNetworks,
							VipDhcpAllocation: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
					actual := reply.(*installer.UpdateClusterCreated)
					validateNetworkConfiguration(actual.Payload, &clusterNetworks, &serviceNetworks, &machineNetworks)

					cluster, err := common.GetClusterFromDB(db, secondClusterID, common.UseEagerLoading)
					Expect(err).ToNot(HaveOccurred())
					validateNetworkConfiguration(&cluster.Cluster, &clusterNetworks, &serviceNetworks, &machineNetworks)

					var dbMachineNetworks []*models.MachineNetwork
					Expect(db.Find(&dbMachineNetworks).Error).ShouldNot(HaveOccurred())
					Expect(dbMachineNetworks).To(HaveLen(len(machineNetworks) * 2))
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
				ID:               &clusterID,
				APIVip:           "10.11.12.13",
				IngressVip:       "10.11.20.50",
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
				Status:           swag.String(models.ClusterStatusReady),
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
			addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
			addHost(masterHostId3, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname2", "bootMode", "10.11.200.180/16"), db)
			err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: &clusterID}).UpdateColumn("free_addresses",
				makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16", "10.11.12.13", "10.11.20.50"))).Error
			Expect(err).ToNot(HaveOccurred())
			mockDurationsSuccess()
			ignitionReader = ioutil.NopCloser(strings.NewReader(`{
					"ignition":{"version":"3.1.0"},
					"storage":{
						"files":[
							{
								"path":"/opt/openshift/manifests/cvo-overrides.yaml",
								"contents":{
									"source":"data:text/plain;charset=utf-8;base64,YXBpVmVyc2lvbjogY29uZmlnLm9wZW5zaGlmdC5pby92MQpraW5kOiBDbHVzdGVyVmVyc2lvbgptZXRhZGF0YToKICBuYW1lc3BhY2U6IG9wZW5zaGlmdC1jbHVzdGVyLXZlcnNpb24KICBuYW1lOiB2ZXJzaW9uCnNwZWM6CiAgdXBzdHJlYW06IGh0dHBzOi8vYXBpLm9wZW5zaGlmdC5jb20vYXBpL3VwZ3JhZGVzX2luZm8vdjEvZ3JhcGgKICBjaGFubmVsOiBzdGFibGUtNC42CiAgY2x1c3RlcklEOiA0MTk0MGVlOC1lYzk5LTQzZGUtODc2Ni0xNzQzODFiNDkyMWQK"
								}
							}
						]
					},
					"systemd":{}
			}`))
			mockS3Client.EXPECT().Download(gomock.Any(), gomock.Any()).Return(ignitionReader, int64(0), nil).MinTimes(0)
		})

		It("success", func() {

			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockClusterIsReadyForInstallationSuccess()
			mockGenerateAdditionalManifestsSuccess()
			mockGetInstallConfigSuccess(mockInstallConfigBuilder)
			mockGenerateInstallConfigSuccess(mockGenerator, mockVersions)
			mockClusterPrepareForInstallationSuccess(mockClusterApi)
			mockHostPrepareForRefresh(mockHostApi)
			mockHandlePreInstallationSuccess(mockClusterApi, DoneChannel)
			setDefaultGetMasterNodesIds(mockClusterApi)
			setDefaultHostSetBootstrap(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockClusterRefreshStatus(mockClusterApi)
			mockClusterDeleteLogsSuccess(mockClusterApi)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
			mockEvents.EXPECT().
				AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				MinTimes(0)

			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewInstallClusterAccepted()))
			waitForDoneChannel()

			count := db.Model(&models.Cluster{}).Where("openshift_cluster_id <> ''").First(&models.Cluster{}).RowsAffected
			Expect(count).To(Equal(int64(1)))
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
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)

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
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)

			Expect(db.Model(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).UpdateColumn("status", "insufficient").Error).To(Not(HaveOccurred()))
			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusConflict)
		})

		It("list of masters for setting bootstrap return empty list", func() {
			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockHostPrepareForRefresh(mockHostApi)
			mockClusterPrepareForInstallationSuccess(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)

			mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]*strfmt.UUID{}, nil).Times(1)

			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusInternalServerError)
		})

		It("GetMasterNodesIds fails in the go routine", func() {
			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockHostPrepareForRefresh(mockHostApi)
			mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, errors.Errorf("nop"))
			mockClusterPrepareForInstallationSuccess(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)

			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusInternalServerError)
		})

		It("GetMasterNodesIds returns empty list", func() {
			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockHostPrepareForRefresh(mockHostApi)
			mockClusterPrepareForInstallationSuccess(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)

			mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, errors.Errorf("nop"))

			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusInternalServerError)
		})

		It("failed to delete logs", func() {
			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockClusterIsReadyForInstallationSuccess()
			mockGenerateAdditionalManifestsSuccess()
			mockGetInstallConfigSuccess(mockInstallConfigBuilder)
			mockGenerateInstallConfigSuccess(mockGenerator, mockVersions)
			mockClusterPrepareForInstallationSuccess(mockClusterApi)
			mockHostPrepareForRefresh(mockHostApi)
			setDefaultGetMasterNodesIds(mockClusterApi)
			setDefaultHostSetBootstrap(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockClusterRefreshStatus(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockClusterDeleteLogsFailure(mockClusterApi)
			mockHandlePreInstallationSuccess(mockClusterApi, DoneChannel)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
			mockEvents.EXPECT().
				AddEvent(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				MinTimes(0)

			reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
				ClusterID: clusterID,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewInstallClusterAccepted()))
			waitForDoneChannel()

			count := db.Model(&models.Cluster{}).Where("openshift_cluster_id <> ''").First(&models.Cluster{}).RowsAffected
			Expect(count).To(Equal(int64(1)))
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
			errorInfo := "dummy"
			It("complete success", func() {
				success := true
				// TODO: MGMT-4458
				// This function can be removed once the controller will stop sending this request
				// The service is already capable of completing the installation on its own

				reply := bm.CompleteInstallation(ctx, installer.CompleteInstallationParams{
					ClusterID:        clusterID,
					CompletionParams: &models.CompletionParams{ErrorInfo: errorInfo, IsSuccess: &success},
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewCompleteInstallationAccepted()))
			})
			It("complete failure", func() {
				success := false
				mockClusterApi.EXPECT().CompleteInstallation(ctx, gomock.Any(), gomock.Any(), success, errorInfo).Return(nil, nil).Times(1)

				reply := bm.CompleteInstallation(ctx, installer.CompleteInstallationParams{
					ClusterID:        clusterID,
					CompletionParams: &models.CompletionParams{ErrorInfo: errorInfo, IsSuccess: &success},
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewCompleteInstallationAccepted()))
			})
			It("complete failure bad request", func() {
				success := false
				mockClusterApi.EXPECT().CompleteInstallation(ctx, gomock.Any(), gomock.Any(), success, errorInfo).Return(nil, errors.New("error")).Times(1)

				reply := bm.CompleteInstallation(ctx, installer.CompleteInstallationParams{
					ClusterID:        clusterID,
					CompletionParams: &models.CompletionParams{ErrorInfo: errorInfo, IsSuccess: &success},
				})

				verifyApiError(reply, http.StatusInternalServerError)
			})
		})

		AfterEach(func() {
			close(DoneChannel)
			common.DeleteTestDB(db, dbName)
		})
	})
})

var _ = Describe("infraEnvs", func() {

	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		infraEnvID strfmt.UUID
		dbName     string
	)

	const (
		AdditionalNtpSources = "ADDITIONAL_NTP_SOURCES"
		DownloadUrl          = "DOWNLOAD_URL"
		HREF                 = "HREF"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("Delete", func() {
		BeforeEach(func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:                   infraEnvID,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 HREF,
				DownloadURL:          DownloadUrl,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())
		})

		Context("DeRegisterInfraEnv", func() {
			It("success", func() {
				mockInfraEnvDeRegisterSuccess()
				reply := bm.GetInfraEnv(ctx, installer.GetInfraEnvParams{
					InfraEnvID: infraEnvID,
				})
				_, ok := reply.(*installer.GetInfraEnvOK)
				Expect(ok).To(BeTrue())
				reply = bm.DeregisterInfraEnv(ctx, installer.DeregisterInfraEnvParams{InfraEnvID: infraEnvID})
				Expect(reply).Should(BeAssignableToTypeOf(&installer.DeregisterInfraEnvNoContent{}))
				reply = bm.GetInfraEnv(ctx, installer.GetInfraEnvParams{
					InfraEnvID: infraEnvID,
				})
				Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
			})

			It("failure - hosts exists", func() {
				hostID := strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Host{
					Host: models.Host{
						ID:         &hostID,
						InfraEnvID: infraEnvID,
					}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				reply := bm.DeregisterInfraEnv(ctx, installer.DeregisterInfraEnvParams{InfraEnvID: infraEnvID})
				Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusConflict, errors.Errorf(""))))
			})
		})
	})

	Context("List", func() {
		BeforeEach(func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:                   infraEnvID,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 HREF,
				DownloadURL:          DownloadUrl,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			infraEnvID = strfmt.UUID(uuid.New().String())
			err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:                   infraEnvID,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 HREF,
				DownloadURL:          DownloadUrl,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())
		})

		Context("List InfraEnvs", func() {
			It("success", func() {
				resp := bm.ListInfraEnvs(ctx, installer.ListInfraEnvsParams{})
				Expect(resp).Should(BeAssignableToTypeOf(installer.NewListInfraEnvsOK()))
				payload := resp.(*installer.ListInfraEnvsOK).Payload
				Expect(len(payload)).Should(Equal(2))
				Expect(payload[1].ID.String()).Should(Equal(infraEnvID.String()))
				Expect(payload[1].OpenshiftVersion).Should(Equal(common.TestDefaultConfig.OpenShiftVersion))
				Expect(payload[1].AdditionalNtpSources).Should(Equal(AdditionalNtpSources))
				Expect(payload[1].Href).Should(Equal(HREF))
			})
		})
	})

	Context("List Infra Env Hosts", func() {
		var (
			infraEnvId1, infraEnvId2 strfmt.UUID
		)
		BeforeEach(func() {
			infraEnvId1 = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:                   infraEnvId1,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 HREF,
				DownloadURL:          DownloadUrl,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			for i := 0; i != 2; i++ {
				hostID := strfmt.UUID(uuid.New().String())
				err = db.Create(&models.Host{
					ID:         &hostID,
					InfraEnvID: infraEnvId1,
				}).Error
				Expect(err).ToNot(HaveOccurred())
			}

			infraEnvId2 = strfmt.UUID(uuid.New().String())
			err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:                   infraEnvId2,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 HREF,
				DownloadURL:          DownloadUrl,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())
			for i := 0; i != 3; i++ {
				hostID := strfmt.UUID(uuid.New().String())
				err = db.Create(&models.Host{
					ID:         &hostID,
					InfraEnvID: infraEnvId2,
				}).Error
				Expect(err).ToNot(HaveOccurred())
			}
		})

		Context("List Hosts", func() {
			It("success", func() {
				resp := bm.V2ListHosts(ctx, installer.V2ListHostsParams{
					InfraEnvID: infraEnvId1,
				})
				payload := resp.(*installer.V2ListHostsOK).Payload
				Expect(len(payload)).Should(Equal(2))
				resp = bm.V2ListHosts(ctx, installer.V2ListHostsParams{
					InfraEnvID: infraEnvId2,
				})
				payload = resp.(*installer.V2ListHostsOK).Payload
				Expect(len(payload)).Should(Equal(3))
			})
		})
	})

	Context("Get", func() {
		BeforeEach(func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:                   infraEnvID,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 HREF,
				DownloadURL:          DownloadUrl,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())
		})

		Context("GetInfraEnv", func() {
			It("success", func() {
				reply := bm.GetInfraEnv(ctx, installer.GetInfraEnvParams{
					InfraEnvID: infraEnvID,
				})
				actual, ok := reply.(*installer.GetInfraEnvOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload.OpenshiftVersion).To(BeEquivalentTo(common.TestDefaultConfig.OpenShiftVersion))
				Expect(actual.Payload.AdditionalNtpSources).To(Equal(AdditionalNtpSources))
				Expect(actual.Payload.DownloadURL).To(Equal(DownloadUrl))
				Expect(actual.Payload.Href).To(Equal(HREF))
			})

			It("Unfamilliar ID", func() {
				resp := bm.GetInfraEnv(ctx, installer.GetInfraEnvParams{InfraEnvID: "12345"})
				Expect(resp).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
			})

			It("DB inaccessible", func() {
				common.DeleteTestDB(db, dbName)
				resp := bm.GetInfraEnv(ctx, installer.GetInfraEnvParams{InfraEnvID: infraEnvID})
				Expect(resp).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusInternalServerError, errors.Errorf(""))))
			})
		})

	})

	Context("Create InfraEnv", func() {
		It("happy flow", func() {
			mockInfraEnvRegisterSuccess()
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					OpenshiftVersion: swag.String(MinimalOpenShiftVersionForNoneHA),
					PullSecret:       swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterInfraEnvCreated())))
			actual := reply.(*installer.RegisterInfraEnvCreated)
			Expect(actual.Payload.Name).To(Equal("some-infra-env-name"))
		})

		It("Create with ClusterID - CPU architecture match", func() {
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"

			clusterID := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
				CPUArchitecture:  "x86_64",
			}}
			err := db.Create(&cluster).Error
			Expect(err).ShouldNot(HaveOccurred())

			mockInfraEnvRegisterSuccess()
			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					OpenshiftVersion: swag.String(MinimalOpenShiftVersionForNoneHA),
					PullSecret:       swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
					ClusterID:        &clusterID,
					CPUArchitecture:  "x86_64",
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterInfraEnvCreated())))
			actual := reply.(*installer.RegisterInfraEnvCreated)
			Expect(actual.Payload.Name).To(Equal("some-infra-env-name"))
		})

		It("No version specified", func() {
			mockVersions.EXPECT().GetLatestOpenshiftVersion(gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(1)
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("").Times(1)
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).Times(2)
			mockS3Client.EXPECT().GetBaseIsoObject(gomock.Any(), gomock.Any()).Return("rhcos", nil).Times(1)
			mockS3Client.EXPECT().UploadISO(gomock.Any(), gomock.Any(), "rhcos", gomock.Any()).Return(nil).Times(1)
			mockS3Client.EXPECT().GetObjectSizeBytes(gomock.Any(), gomock.Any()).Return(int64(100), nil).Times(1)
			mockS3Client.EXPECT().IsAwsS3().Return(false)
			mockEvents.EXPECT().AddEvent(gomock.Any(), gomock.Any(), nil, models.EventSeverityInfo, gomock.Any(), gomock.Any()).AnyTimes()

			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:       swag.String("some-infra-env-name"),
					PullSecret: swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterInfraEnvCreated())))
			actual := reply.(*installer.RegisterInfraEnvCreated)
			Expect(actual.Payload.Name).To(Equal("some-infra-env-name"))
		})

		It("Create with ClusterID - CPU architecture mismatch", func() {
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"

			clusterID := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
				CPUArchitecture:  "x86_64",
			}}
			err := db.Create(&cluster).Error
			Expect(err).ShouldNot(HaveOccurred())

			mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(1)
			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					OpenshiftVersion: swag.String(MinimalOpenShiftVersionForNoneHA),
					PullSecret:       swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
					ClusterID:        &clusterID,
					CPUArchitecture:  "arm64",
				},
			})
			verifyApiErrorString(reply, http.StatusBadRequest, "CPU architecture doesn't match")
		})

		It("Invalid Ignition", func() {
			mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(1)
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			override := `{"ignition": {"wdd": "3.9.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:                   swag.String("some-infra-env-name"),
					OpenshiftVersion:       swag.String(MinimalOpenShiftVersionForNoneHA),
					PullSecret:             swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
					IgnitionConfigOverride: override,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf(""))))
		})
	})

	Context("Update", func() {
		Context("Update infraEnv", func() {
			infraEnvName := "some-infra-env"
			var (
				i *common.InfraEnv
			)
			BeforeEach(func() {
				mockInfraEnvRegisterSuccess()
				reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
					InfraenvCreateParams: &models.InfraEnvCreateParams{
						Name:             swag.String(infraEnvName),
						OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
						PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
					},
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterInfraEnvCreated()))
				actual := reply.(*installer.RegisterInfraEnvCreated)
				var err error
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: actual.Payload.ID})
				Expect(err).ToNot(HaveOccurred())
				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("generated_at", strfmt.DateTime(time.Now().AddDate(0, 0, -1))).Error
				Expect(err).ToNot(HaveOccurred())
			})
			It("Update AdditionalNtpSources", func() {
				mockInfraEnvUpdateSuccess()
				Expect(i.AdditionalNtpSources).To(Equal(""))
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						AdditionalNtpSources: swag.String("1.1.1.1"),
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				var err error
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.AdditionalNtpSources).ToNot(Equal(nil))
				Expect(i.AdditionalNtpSources).To(Equal("1.1.1.1"))
			})
			It("Update AdditionalNtpSources same", func() {
				var err error
				mockInfraEnvUpdateSuccessNoImageGeneration()
				additionalNtpSources := "1.1.1.1"
				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("additional_ntp_sources", additionalNtpSources).Error
				Expect(err).ToNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						AdditionalNtpSources: swag.String("1.1.1.1"),
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.AdditionalNtpSources).ToNot(Equal(nil))
				Expect(i.AdditionalNtpSources).To(Equal("1.1.1.1"))
			})
			It("Update Ignition", func() {
				mockInfraEnvUpdateSuccess()
				override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						IgnitionConfigOverride: override,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				var err error
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.IgnitionConfigOverride).To(Equal(override))
			})
			It("Update Ignition - same ", func() {
				var err error
				mockInfraEnvUpdateSuccessNoImageGeneration()
				override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("ignition_config_override", override).Error
				Expect(err).ToNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						IgnitionConfigOverride: override,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.IgnitionConfigOverride).To(Equal(override))
			})
			It("Update Image type", func() {
				var err error
				mockInfraEnvUpdateSuccess()
				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("type", models.ImageTypeMinimalIso).Error
				Expect(err).ToNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						ImageType: models.ImageTypeFullIso,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.Type).To(Equal(models.ImageTypeFullIso))
			})

			It("Update Image type same", func() {
				var err error
				mockInfraEnvUpdateSuccessNoImageGeneration()
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						ImageType: models.ImageTypeFullIso,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.Type).To(Equal(models.ImageTypeFullIso))
			})

			It("Update StaticNetwork", func() {
				mockInfraEnvUpdateSuccess()
				staticNetworkFormatRes := "static network format result"
				map1 := models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{MacAddress: "mac10", LogicalNicName: "nic10"},
					&models.MacInterfaceMapItems0{MacAddress: "mac11", LogicalNicName: "nic11"},
				}
				map2 := models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{MacAddress: "mac20", LogicalNicName: "nic20"},
					&models.MacInterfaceMapItems0{MacAddress: "mac21", LogicalNicName: "nic21"},
				}
				map3 := models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{MacAddress: "mac30", LogicalNicName: "nic30"},
					&models.MacInterfaceMapItems0{MacAddress: "mac31", LogicalNicName: "nic31"},
				}
				staticNetworkConfig := []*models.HostStaticNetworkConfig{
					common.FormatStaticConfigHostYAML("0200003ef74c", "02000048ba48", "192.168.126.41", "192.168.141.41", "192.168.126.1", map1),
					common.FormatStaticConfigHostYAML("0200003ef73c", "02000048ba38", "192.168.126.40", "192.168.141.40", "192.168.126.1", map2),
					common.FormatStaticConfigHostYAML("0200003ef75c", "02000048ba58", "192.168.126.42", "192.168.141.42", "192.168.126.1", map3),
				}
				mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(staticNetworkConfig).Return(nil).Times(1)
				mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes).Times(1)
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						StaticNetworkConfig: staticNetworkConfig,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				var err error
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.StaticNetworkConfig).To(Equal(staticNetworkFormatRes))
			})
			It("Update StaticNetwork same", func() {
				mockInfraEnvUpdateSuccessNoImageGeneration()
				var err error
				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("static_network_config", "static network format result").Error
				Expect(err).ToNot(HaveOccurred())
				staticNetworkFormatRes := "static network format result"
				map1 := models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{MacAddress: "mac10", LogicalNicName: "nic10"},
					&models.MacInterfaceMapItems0{MacAddress: "mac11", LogicalNicName: "nic11"},
				}
				map2 := models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{MacAddress: "mac20", LogicalNicName: "nic20"},
					&models.MacInterfaceMapItems0{MacAddress: "mac21", LogicalNicName: "nic21"},
				}
				map3 := models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{MacAddress: "mac30", LogicalNicName: "nic30"},
					&models.MacInterfaceMapItems0{MacAddress: "mac31", LogicalNicName: "nic31"},
				}
				staticNetworkConfig := []*models.HostStaticNetworkConfig{
					common.FormatStaticConfigHostYAML("0200003ef74c", "02000048ba48", "192.168.126.41", "192.168.141.41", "192.168.126.1", map1),
					common.FormatStaticConfigHostYAML("0200003ef73c", "02000048ba38", "192.168.126.40", "192.168.141.40", "192.168.126.1", map2),
					common.FormatStaticConfigHostYAML("0200003ef75c", "02000048ba58", "192.168.126.42", "192.168.141.42", "192.168.126.1", map3),
				}
				mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(staticNetworkConfig).Return(nil).Times(1)
				mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes).Times(1)
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						StaticNetworkConfig: staticNetworkConfig,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.StaticNetworkConfig).To(Equal(staticNetworkFormatRes))
			})
		})

		Context("check pull secret", func() {
			BeforeEach(func() {
				v, err := validations.NewPullSecretValidator(validations.Config{})
				Expect(err).ShouldNot(HaveOccurred())
				bm.secretValidator = v
			})

			It("Invalid pull-secret", func() {
				pullSecret := "asdfasfda"
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: infraEnvID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						PullSecret: pullSecret,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf(""))))
			})

			It("pull-secret with newline", func() {
				mockInfraEnvUpdateSuccess()
				pullSecret := "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}" // #nosec
				pullSecretWithNewline := pullSecret + " \n"
				infraEnvID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
					ID: infraEnvID,
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: infraEnvID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						PullSecret: pullSecretWithNewline,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
			})
		})

		It("ssh key with newline", func() {
			mockInfraEnvUpdateSuccess()
			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{
				PullSecret: "PULL_SECRET",
				InfraEnv: models.InfraEnv{
					ID:            infraEnvID,
					PullSecretSet: true,
				}}).Error
			Expect(err).ShouldNot(HaveOccurred())
			sshKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDi8KHZYGyPQjECHwytquI3rmpgoUn6M+lkeOD2nEKvYElLE5mPIeqF0izJIl56u" +
				"ar2wda+3z107M9QkatE+dP4S9/Ltrlm+/ktAf4O6UoxNLUzv/TGHasb9g3Xkt8JTkohVzVK36622Sd8kLzEc61v1AonLWIADtpwq6/GvH" +
				"MAuPK2R/H0rdKhTokylKZLDdTqQ+KUFelI6RNIaUBjtVrwkx1j0htxN11DjBVuUyPT2O1ejWegtrM0T+4vXGEA3g3YfbT2k0YnEzjXXqng" +
				"qbXCYEJCZidp3pJLH/ilo4Y4BId/bx/bhzcbkZPeKlLwjR8g9sydce39bzPIQj+b7nlFv1Vot/77VNwkjXjYPUdUPu0d1PkFD9jKDOdB3f" +
				"AC61aG2a/8PFS08iBrKiMa48kn+hKXC4G4D5gj/QzIAgzWSl2tEzGQSoIVTucwOAL/jox2dmAa0RyKsnsHORppanuW4qD7KAcmas1GHrAq" +
				"IfNyDiU2JR50r1jCxj5H76QxIuM= root@ocp-edge34.lab.eng.tlv2.redhat.com"
			sshKeyWithNewLine := sshKey + " \n"

			reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
				InfraEnvID: infraEnvID,
				InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
					SSHAuthorizedKey: &sshKeyWithNewLine,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
			var infraEnv common.InfraEnv
			err = db.First(&infraEnv, "id = ?", infraEnvID).Error
			Expect(err).ShouldNot(HaveOccurred())
			Expect(infraEnv.SSHAuthorizedKey).Should(Equal(sshKey))
		})

		It("empty pull-secret", func() {
			pullSecret := ""
			reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
				InfraEnvID: infraEnvID,
				InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
					PullSecret: pullSecret,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
		})

		Context("Update Proxy", func() {
			BeforeEach(func() {
				infraEnvID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.InfraEnv{
					PullSecret: "PULL_SECRET",
					InfraEnv: models.InfraEnv{
						ID:            infraEnvID,
						PullSecretSet: true,
					}}).Error
				Expect(err).ShouldNot(HaveOccurred())
			})

			updateInfraEnv := func(httpProxy, httpsProxy, noProxy string) *common.InfraEnv {
				mockInfraEnvUpdateSuccess()
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: infraEnvID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						Proxy: &models.Proxy{
							HTTPProxy:  &httpProxy,
							HTTPSProxy: &httpsProxy,
							NoProxy:    &noProxy,
						},
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				var infraEnv common.InfraEnv
				err := db.First(&infraEnv, "id = ?", infraEnvID).Error
				Expect(err).ShouldNot(HaveOccurred())
				return &infraEnv
			}

			It("set a valid proxy", func() {
				_ = updateInfraEnv("http://proxy.proxy", "", "proxy.proxy")
			})
		})

		Context("GenerateInfraEnvISOInternal", func() {
			It("GenerateInfraEnvISOInternal Fail - too fast", func() {
				infraEnvID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.InfraEnv{
					GeneratedAt: strfmt.DateTime(time.Now()),
					PullSecret:  "PULL_SECRET",
					InfraEnv: models.InfraEnv{
						ID:            infraEnvID,
						PullSecretSet: true,
					}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID:           infraEnvID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{},
				})
				Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
				Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
			})

			It("UpdateInternal GeneratedAt updated in response", func() {
				mockInfraEnvUpdateSuccess()
				infraEnvID = strfmt.UUID(uuid.New().String())
				generatedAt := strfmt.NewDateTime()
				err := db.Create(&common.InfraEnv{
					GeneratedAt: generatedAt,
					PullSecret:  "PULL_SECRET",
					InfraEnv: models.InfraEnv{
						ID:            infraEnvID,
						PullSecretSet: true,
					}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				reponse, err := bm.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID:           infraEnvID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{},
				})
				Expect(err).ShouldNot(HaveOccurred())
				Expect(reponse.GeneratedAt).ShouldNot(Equal(generatedAt))
			})
		})

		Context("Update Network", func() {
			BeforeEach(func() {
				infraEnvID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.InfraEnv{
					PullSecret: "PULL_SECRET",
					InfraEnv: models.InfraEnv{
						ID:            infraEnvID,
						PullSecretSet: true,
					}}).Error
				Expect(err).ShouldNot(HaveOccurred())
			})

			updateInfraEnv := func(ntpSource string) *common.InfraEnv {
				mockInfraEnvUpdateSuccess()
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: infraEnvID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						AdditionalNtpSources: &ntpSource,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				actual := reply.(*installer.UpdateInfraEnvCreated)
				Expect(actual.Payload.AdditionalNtpSources).To(Equal(ntpSource))
				var infraEnv common.InfraEnv
				err := db.First(&infraEnv, "id = ?", infraEnvID).Error
				Expect(err).ShouldNot(HaveOccurred())
				return &infraEnv
			}

			Context("NTP", func() {
				It("Empty NTP source", func() {
					ntpSource := ""
					_ = updateInfraEnv(ntpSource)
				})

				It("Valid IP NTP source", func() {
					ntpSource := "1.1.1.1"
					_ = updateInfraEnv(ntpSource)
				})

				It("Valid Hostname NTP source", func() {
					ntpSource := "clock.redhat.com"
					_ = updateInfraEnv(ntpSource)
				})

				It("Valid comma-separated NTP sources", func() {
					ntpSource := "clock.redhat.com,1.1.1.1"
					_ = updateInfraEnv(ntpSource)
				})

				It("Invalid NTP source", func() {
					ntpSource := "inject'"
					reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
						InfraEnvID: infraEnvID,
						InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
							AdditionalNtpSources: &ntpSource,
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
					Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				})
			})
		})
	})
})

var _ = Describe("infraEnvs host", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		infraEnvID strfmt.UUID
		dbName     string
	)

	const (
		AdditionalNtpSources = "ADDITIONAL_NTP_SOURCES"
		DownloadUrl          = "DOWNLOAD_URL"
		HREF                 = "HREF"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)

		infraEnvID = strfmt.UUID(uuid.New().String())
		err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:                   infraEnvID,
			OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
			Href:                 HREF,
			AdditionalNtpSources: AdditionalNtpSources,
			DownloadURL:          DownloadUrl,
		}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		hostID := strfmt.UUID(uuid.New().String())
		err = db.Create(&models.Host{
			ID:         &hostID,
			InfraEnvID: infraEnvID,
		}).Error
		Expect(err).ToNot(HaveOccurred())
	})

	Context("Update Host", func() {

		var hostID strfmt.UUID
		var clusterID strfmt.UUID
		diskID1 := "/dev/sda"
		diskID2 := "/dev/sdb"

		BeforeEach(func() {
			hostID = strfmt.UUID(uuid.New().String())
			clusterID = strfmt.UUID(uuid.New().String())
			err := db.Create(&models.Host{
				ID:         &hostID,
				InfraEnvID: infraEnvID,
				ClusterID:  &clusterID,
			}).Error
			Expect(err).ToNot(HaveOccurred())
		})

		It("update host role success", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), models.HostRole("master"), gomock.Any()).Return(nil).Times(1)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					HostRole: swag.String("master"),
				},
			})
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
		})

		It("update host role failure", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), models.HostRole("master"), gomock.Any()).Return(fmt.Errorf("some error")).Times(1)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					HostRole: swag.String("master"),
				},
			})
			Expect(resp).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(resp.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusInternalServerError)))
		})

		It("update host name success", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), "somehostname", gomock.Any()).Return(nil).Times(1)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					HostName: swag.String("somehostname"),
				},
			})
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
		})

		It("update host name failure", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), "somehostname", gomock.Any()).Return(fmt.Errorf("some error")).Times(1)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					HostName: swag.String("somehostname"),
				},
			})
			Expect(resp).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(resp.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
		})

		It("update host name invalid format", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					HostName: swag.String("somehostnamei@dflkh"),
				},
			})
			Expect(resp).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(resp.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
		})

		It("update disks config success", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), "somehostname", gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), diskID1).Return(nil).Times(1)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					DisksSelectedConfig: []*models.DiskConfigParams{
						{ID: &diskID1, Role: models.DiskRoleInstall},
						{ID: &diskID2, Role: models.DiskRoleNone},
					},
				},
			})
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
		})

		It("update disks config invalid config, multiple boot disk", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), "somehostname", gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					DisksSelectedConfig: []*models.DiskConfigParams{
						{ID: &diskID1, Role: models.DiskRoleInstall},
						{ID: &diskID2, Role: models.DiskRoleInstall},
					},
				},
			})
			Expect(resp).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(resp.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
		})

		It("update machine pool success", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), "machinepool").Return(nil).Times(1)
			mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					MachineConfigPoolName: swag.String("machinepool"),
				},
			})
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
		})

		It("update machine pool failure", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), "machinepool").Return(fmt.Errorf("some error")).Times(1)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					MachineConfigPoolName: swag.String("machinepool"),
				},
			})
			Expect(resp).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(resp.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
		})

	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("KubeConfig download", func() {

	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		c         common.Cluster
		dbName    string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())

		bm = createInventory(db, cfg)
		c = common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			APIVip:           "10.11.12.13",
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
			FileName:  constants.Kubeconfig,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
	})
	It("kubeconfig presigned cluster is not in installed state", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  constants.Kubeconfig,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
	})
	It("kubeconfig presigned happy flow", func() {
		status := models.ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		fileName := fmt.Sprintf("%s/%s", clusterID, constants.Kubeconfig)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, constants.Kubeconfig, gomock.Any()).Return("url", nil)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  constants.Kubeconfig,
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
		status := models.ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		fileName := fmt.Sprintf("%s/%s", clusterID, constants.Kubeconfig)
		mockS3Client.EXPECT().Download(ctx, fileName).Return(nil, int64(0), errors.Errorf("dummy"))
		generateReply := bm.DownloadClusterKubeconfig(ctx, installer.DownloadClusterKubeconfigParams{
			ClusterID: clusterID,
		})
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
	})
	It("kubeconfig download happy flow", func() {
		status := models.ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		fileName := fmt.Sprintf("%s/%s", clusterID, constants.Kubeconfig)
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.DownloadClusterKubeconfig(ctx, installer.DownloadClusterKubeconfigParams{
			ClusterID: clusterID,
		})
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewDownloadClusterKubeconfigOK().WithPayload(r), constants.Kubeconfig, 4)))
	})
})

var _ = Describe("DownloadMinimalInitrd", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		infraEnv  common.InfraEnv
		dbName    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		infraEnv = common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:            clusterID,
				PullSecretSet: true,
				Type:          models.ImageTypeMinimalIso,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		err := db.Create(&infraEnv).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("returns not found with a non-existant cluster", func() {
		params := installer.DownloadMinimalInitrdParams{InfraEnvID: strfmt.UUID(uuid.New().String())}
		response := bm.DownloadMinimalInitrd(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns conflict when not minimal-iso", func() {
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnv = common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:            clusterID,
				PullSecretSet: true,
				Type:          models.ImageTypeFullIso,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		err := db.Create(&infraEnv).Error
		Expect(err).ShouldNot(HaveOccurred())

		params := installer.DownloadMinimalInitrdParams{InfraEnvID: clusterID}
		response := bm.DownloadMinimalInitrd(ctx, params)
		verifyApiError(response, http.StatusConflict)
	})

	It("returns no content without network customizations", func() {
		params := installer.DownloadMinimalInitrdParams{InfraEnvID: clusterID}
		response := bm.DownloadMinimalInitrd(ctx, params)
		Expect(response).Should(BeAssignableToTypeOf(&installer.DownloadMinimalInitrdNoContent{}))
	})

	It("returns legit archive", func() {
		clusterID = strfmt.UUID(uuid.New().String())

		httpProxy := "http://10.10.1.1:3128"
		httpsProxy := "https://10.10.1.1:3128"
		noProxy := "quay.io"

		clusterProxyInfo := isoeditor.ClusterProxyInfo{
			HTTPProxy:  httpProxy,
			HTTPSProxy: httpsProxy,
			NoProxy:    noProxy,
		}

		infraEnv = common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:            clusterID,
				PullSecretSet: true,
				Type:          models.ImageTypeMinimalIso,
				Proxy: &models.Proxy{
					HTTPProxy:  &httpProxy,
					HTTPSProxy: &httpsProxy,
					NoProxy:    &noProxy,
				},
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		err := db.Create(&infraEnv).Error
		Expect(err).ShouldNot(HaveOccurred())

		params := installer.DownloadMinimalInitrdParams{InfraEnvID: clusterID}
		responsePayload := bm.DownloadMinimalInitrd(ctx, params).(*installer.DownloadMinimalInitrdOK).Payload

		gzipReader, err := gzip.NewReader(responsePayload)
		Expect(err).ToNot(HaveOccurred())

		var rootfsServiceConfigContent string
		r := cpio.NewReader(gzipReader)
		for {
			hdr, err := r.Next()
			if err == io.EOF {
				break
			}
			Expect(err).ToNot(HaveOccurred())
			switch hdr.Name {
			case "/etc/systemd/system/coreos-livepxe-rootfs.service.d/10-proxy.conf":
				rootfsServiceConfigBytes, err := ioutil.ReadAll(r)
				Expect(err).ToNot(HaveOccurred())
				rootfsServiceConfigContent = string(rootfsServiceConfigBytes)
			}
		}

		rootfsServiceConfig := fmt.Sprintf("[Service]\n"+
			"Environment=http_proxy=%s\nEnvironment=https_proxy=%s\nEnvironment=no_proxy=%s\n"+
			"Environment=HTTP_PROXY=%s\nEnvironment=HTTPS_PROXY=%s\nEnvironment=NO_PROXY=%s",
			clusterProxyInfo.HTTPProxy, clusterProxyInfo.HTTPSProxy, clusterProxyInfo.NoProxy,
			clusterProxyInfo.HTTPProxy, clusterProxyInfo.HTTPSProxy, clusterProxyInfo.NoProxy)
		Expect(rootfsServiceConfigContent).To(Equal(rootfsServiceConfig))
	})
})

var _ = Describe("UploadClusterIngressCert test", func() {
	var (
		bm                  *bareMetalInventory
		cfg                 Config
		db                  *gorm.DB
		ctx                 = context.Background()
		clusterID           strfmt.UUID
		c                   common.Cluster
		ingressCa           models.IngressCertParams
		kubeconfigFile      *os.File
		kubeconfigNoingress string
		kubeconfigObject    string
		dbName              string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
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
		bm = createInventory(db, cfg)
		mockOperators := operators.NewMockAPI(ctrl)
		bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
			db, nil, nil, nil, nil, nil, mockOperators, nil, nil, nil)
		c = common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			APIVip:           "10.11.12.13",
		}}
		kubeconfigNoingress = fmt.Sprintf("%s/%s", clusterID, "kubeconfig-noingress")
		kubeconfigObject = fmt.Sprintf("%s/%s", clusterID, constants.Kubeconfig)
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
var _ = Describe("List clusters", func() {

	var (
		bm                 *bareMetalInventory
		cfg                Config
		db                 *gorm.DB
		ctx                = context.Background()
		clusterID          strfmt.UUID
		hostID             strfmt.UUID
		openshiftClusterID = strToUUID("41940ee8-ec99-43de-8766-174381b4921d")
		amsSubscriptionID  = strToUUID("1sOMjCKRmEHYanIsp1bPbplb47t")
		c                  common.Cluster
		kubeconfigFile     *os.File
		dbName             string
		host1              models.Host
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		c = common.Cluster{Cluster: models.Cluster{
			ID:                 &clusterID,
			OpenshiftVersion:   common.TestDefaultConfig.OpenShiftVersion,
			Name:               "mycluster",
			APIVip:             "10.11.12.13",
			OpenshiftClusterID: *openshiftClusterID,
			AmsSubscriptionID:  *amsSubscriptionID,
		}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		hostID = strfmt.UUID(uuid.New().String())
		host1 = addHost(hostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
		kubeconfigFile.Close()
	})

	Context("List unregistered clusters", func() {
		It("success", func() {
			Expect(db.Delete(&c).Error).ShouldNot(HaveOccurred())
			Expect(db.Delete(&host1).Error).ShouldNot(HaveOccurred())
			resp := bm.ListClusters(ctx, installer.ListClustersParams{GetUnregisteredClusters: swag.Bool(true)})
			payload := resp.(*installer.ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(1))
			Expect(payload[0].ID.String()).Should(Equal(clusterID.String()))
			Expect(payload[0].TotalHostCount).Should(Equal(int64(1)))
			Expect(payload[0].EnabledHostCount).Should(Equal(int64(1)))
			Expect(payload[0].ReadyHostCount).Should(Equal(int64(1)))
			Expect(len(payload[0].Hosts)).Should(Equal(0))
		})

		It("with hosts success", func() {
			Expect(db.Delete(&c).Error).ShouldNot(HaveOccurred())
			Expect(db.Delete(&host1).Error).ShouldNot(HaveOccurred())
			resp := bm.ListClusters(ctx, installer.ListClustersParams{GetUnregisteredClusters: swag.Bool(true), WithHosts: true})
			clusterList := resp.(*installer.ListClustersOK).Payload
			Expect(len(clusterList)).Should(Equal(1))
			Expect(clusterList[0].ID.String()).Should(Equal(clusterID.String()))
			Expect(len(clusterList[0].Hosts)).Should(Equal(1))
			Expect(clusterList[0].Hosts[0].ID.String()).Should(Equal(hostID.String()))
		})

		It("failure - cluster was permanently deleted", func() {
			Expect(db.Unscoped().Delete(&c).Error).ShouldNot(HaveOccurred())
			Expect(db.Unscoped().Delete(&host1).Error).ShouldNot(HaveOccurred())
			resp := bm.ListClusters(ctx, installer.ListClustersParams{GetUnregisteredClusters: swag.Bool(true)})
			payload := resp.(*installer.ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(0))
		})

		It("failure - not an admin user", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.UserRole
			authCtx := context.WithValue(ctx, restapi.AuthKey, payload)
			Expect(db.Unscoped().Delete(&c).Error).ShouldNot(HaveOccurred())
			Expect(db.Unscoped().Delete(&host1).Error).ShouldNot(HaveOccurred())
			resp := bm.ListClusters(authCtx, installer.ListClustersParams{GetUnregisteredClusters: swag.Bool(true)})
			Expect(resp).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusForbidden, errors.New("only admin users are allowed to get unregistered clusters"))))
		})
	})

	Context("Filter based openshift cluster ID", func() {
		tests := []struct {
			role ocm.RoleType
		}{
			{
				role: ocm.UserRole,
			},
			{
				role: ocm.AdminRole,
			},
		}

		for index := range tests {
			test := tests[index]
			It(fmt.Sprintf("%s role", test.role), func() {
				payload := &ocm.AuthPayload{Role: test.role}
				authCtx := context.WithValue(ctx, restapi.AuthKey, payload)

				By("searching for an existing openshift cluster ID", func() {
					resp := bm.ListClusters(authCtx, installer.ListClustersParams{OpenshiftClusterID: openshiftClusterID})
					payload := resp.(*installer.ListClustersOK).Payload
					Expect(len(payload)).Should(Equal(1))
				})

				By("discarding cluster ID field", func() {
					resp := bm.ListClusters(authCtx, installer.ListClustersParams{})
					payload := resp.(*installer.ListClustersOK).Payload
					Expect(len(payload)).Should(Equal(1))
				})

				By("searching for a non-existing openshift cluster ID", func() {
					resp := bm.ListClusters(authCtx, installer.ListClustersParams{OpenshiftClusterID: strToUUID("00000000-0000-0000-0000-000000000000")})
					payload := resp.(*installer.ListClustersOK).Payload
					Expect(len(payload)).Should(Equal(0))
				})
			})
		}
	})

	It("filters based on AMS subscription ID", func() {
		payload := &ocm.AuthPayload{Role: ocm.UserRole}
		authCtx := context.WithValue(ctx, restapi.AuthKey, payload)

		By("searching for a single existing AMS subscription ID", func() {
			resp := bm.ListClusters(authCtx, installer.ListClustersParams{AmsSubscriptionIds: []string{amsSubscriptionID.String()}})
			payload := resp.(*installer.ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(1))
		})

		By("discarding AMS subscription ID field", func() {
			resp := bm.ListClusters(authCtx, installer.ListClustersParams{})
			payload := resp.(*installer.ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(1))
		})

		By("searching for a non-existing AMS subscription ID", func() {
			resp := bm.ListClusters(authCtx, installer.ListClustersParams{AmsSubscriptionIds: []string{"1sOMjCKRmEHYanIsp1bPbplbXXX"}})
			payload := resp.(*installer.ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(0))
		})

		By("searching for both existing and non-existing AMS subscription IDs", func() {
			resp := bm.ListClusters(authCtx, installer.ListClustersParams{
				AmsSubscriptionIds: []string{
					amsSubscriptionID.String(),
					"1sOMjCKRmEHYanIsp1bPbplbXXX",
				},
			})
			payload := resp.(*installer.ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(1))
		})
	})
})

var _ = Describe("Upload and Download logs test", func() {

	var (
		bm             *bareMetalInventory
		cfg            Config
		db             *gorm.DB
		ctx            = context.Background()
		infraEnvID     strfmt.UUID
		clusterID      strfmt.UUID
		hostID         strfmt.UUID
		c              common.Cluster
		kubeconfigFile *os.File
		dbName         string
		request        *http.Request
		host1          models.Host
		hostLogsType   = string(models.LogsTypeHost)
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())

		bm = createInventory(db, cfg)
		c = common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Name:             "mycluster",
			APIVip:           "10.11.12.13",
		}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		kubeconfigFile, err = os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())
		hostID = strfmt.UUID(uuid.New().String())
		host1 = addHost(hostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)

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
		common.DeleteTestDB(db, dbName)
		kubeconfigFile.Close()
		ctrl.Finish()
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
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
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
	It("Upload Hosts logs Happy flow", func() {

		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		params := installer.UploadHostLogsParams{
			ClusterID:   clusterID,
			HostID:      *host.ID,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
		}
		fileName := bm.getLogsFullName(clusterID.String(), host.ID.String())
		mockEvents.EXPECT().AddEvent(gomock.Any(), clusterID, host.ID, models.EventSeverityInfo, gomock.Any(), gomock.Any()).Times(1)
		mockS3Client.EXPECT().UploadStream(gomock.Any(), gomock.Any(), fileName).Return(nil).Times(1)
		mockHostApi.EXPECT().SetUploadLogsAt(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().UpdateLogsProgress(gomock.Any(), gomock.Any(), string(models.LogsStateCollecting)).Return(nil).Times(1)
		reply := bm.UploadHostLogs(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewUploadHostLogsNoContent()))
	})
	It("start collecting hosts logs indication", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		params := installer.UpdateHostLogsProgressParams{
			ClusterID:   clusterID,
			HostID:      *host.ID,
			HTTPRequest: request,
			LogsProgressParams: &models.LogsProgressParams{
				LogsState: models.LogsStateRequested,
			},
		}
		mockHostApi.EXPECT().UpdateLogsProgress(gomock.Any(), gomock.Any(), string(models.LogsStateRequested)).Return(nil).Times(1)
		reply := bm.UpdateHostLogsProgress(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostLogsProgressNoContent()))
	})
	It("complete collecting hosts logs indication", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		params := installer.UpdateHostLogsProgressParams{
			ClusterID:   clusterID,
			HostID:      *host.ID,
			HTTPRequest: request,
			LogsProgressParams: &models.LogsProgressParams{
				LogsState: models.LogsStateCompleted,
			},
		}
		mockHostApi.EXPECT().UpdateLogsProgress(gomock.Any(), gomock.Any(), string(models.LogsStateCompleted)).Return(nil).Times(1)
		reply := bm.UpdateHostLogsProgress(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewUpdateHostLogsProgressNoContent()))
	})

	It(" V2 start collecting hosts logs indication", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, "{}", db)
		params := installer.V2UpdateHostLogsProgressParams{
			InfraEnvID:  infraEnvID,
			HostID:      *host.ID,
			HTTPRequest: request,
			LogsProgressParams: &models.LogsProgressParams{
				LogsState: models.LogsStateRequested,
			},
		}
		mockHostApi.EXPECT().UpdateLogsProgress(gomock.Any(), gomock.Any(), string(models.LogsStateRequested)).Return(nil).Times(1)
		reply := bm.V2UpdateHostLogsProgress(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostLogsProgressNoContent()))
	})
	It("V2 complete collecting hosts logs indication", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, "{}", db)
		params := installer.V2UpdateHostLogsProgressParams{
			InfraEnvID:  infraEnvID,
			HostID:      *host.ID,
			HTTPRequest: request,
			LogsProgressParams: &models.LogsProgressParams{
				LogsState: models.LogsStateCompleted,
			},
		}
		mockHostApi.EXPECT().UpdateLogsProgress(gomock.Any(), gomock.Any(), string(models.LogsStateCompleted)).Return(nil).Times(1)
		reply := bm.V2UpdateHostLogsProgress(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostLogsProgressNoContent()))
	})

	It("Upload Controller logs Happy flow", func() {
		params := installer.UploadLogsParams{
			ClusterID:   clusterID,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
			LogsType:    string(models.LogsTypeController),
		}
		fileName := bm.getLogsFullName(clusterID.String(), string(models.LogsTypeController))
		mockEvents.EXPECT().AddEvent(gomock.Any(), clusterID, nil, models.EventSeverityInfo, gomock.Any(), gomock.Any()).Times(1)
		mockS3Client.EXPECT().UploadStream(gomock.Any(), gomock.Any(), fileName).Return(nil).Times(2)
		mockClusterApi.EXPECT().SetUploadControllerLogsAt(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(2)
		mockClusterApi.EXPECT().UpdateLogsProgress(gomock.Any(), gomock.Any(), string(models.LogsStateCollecting)).Return(nil).Times(2)
		By("Upload cluster logs for the first time")
		reply := bm.UploadLogs(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewUploadLogsNoContent()))
		By("Upload cluster logs for the second time - expect no additional event to be published")
		err := db.Model(c).Update("controller_logs_collected_at", strfmt.DateTime(time.Now())).Error
		Expect(err).ShouldNot(HaveOccurred())
		reply = bm.UploadLogs(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewUploadLogsNoContent()))
	})
	It("Download controller log where not uploaded yet", func() {
		logsType := string(models.LogsTypeController)
		params := installer.DownloadClusterLogsParams{
			ClusterID: clusterID,
			LogsType:  &logsType,
		}
		verifyApiError(bm.DownloadClusterLogs(ctx, params), http.StatusConflict)
	})
	It("Download S3 logs where not uploaded yet", func() {
		params := installer.DownloadHostLogsParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		verifyApiError(bm.DownloadHostLogs(ctx, params), http.StatusConflict)
	})
	It("Download S3 object not found", func() {
		params := installer.DownloadHostLogsParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		mockS3Client.EXPECT().Download(ctx, fileName).Return(nil, int64(0), common.NotFound(fileName))
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

	It("Download Hosts logs happy flow", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		params := installer.DownloadHostLogsParams{
			ClusterID: clusterID,
			HostID:    *host.ID,
		}
		host.Bootstrap = true
		fileName := bm.getLogsFullName(clusterID.String(), host.ID.String())
		host.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host)
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.DownloadHostLogs(ctx, params)
		downloadFileName := fmt.Sprintf("mycluster_bootstrap_%s.tar.gz", newHostID.String())
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewDownloadHostLogsOK().WithPayload(r), downloadFileName, 4)))
	})
	It("Download Controller logs happy flow", func() {
		logsType := string(models.LogsTypeController)
		params := installer.DownloadClusterLogsParams{
			ClusterID: clusterID,
			LogsType:  &logsType,
		}
		fileName := bm.getLogsFullName(clusterID.String(), logsType)
		c.ControllerLogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&c)
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.DownloadClusterLogs(ctx, params)
		downloadFileName := fmt.Sprintf("mycluster_%s_%s.tar.gz", clusterID, logsType)
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewDownloadClusterLogsOK().WithPayload(r), downloadFileName, 4)))
	})
	It("Logs presigned host not found", func() {
		hostID := strfmt.UUID(uuid.New().String())
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
			LogsType:  &hostLogsType,
		})
		verifyApiError(generateReply, http.StatusNotFound)
	})
	It("Logs presigned no logs found", func() {
		hostID := strfmt.UUID(uuid.New().String())
		_ = addHost(hostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
			LogsType:  &hostLogsType,
		})
		verifyApiError(generateReply, http.StatusConflict)
	})
	It("Logs presigned s3 error", func() {
		hostID := strfmt.UUID(uuid.New().String())
		host1 = addHost(hostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, fmt.Sprintf("mycluster_master_%s.tar.gz", hostID.String()), gomock.Any()).Return("",
			errors.Errorf("Dummy"))
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
			LogsType:  &hostLogsType,
		})
		verifyApiError(generateReply, http.StatusInternalServerError)
	})
	It("host logs presigned happy flow", func() {
		hostID := strfmt.UUID(uuid.New().String())
		host1 = addHost(hostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, fmt.Sprintf("mycluster_master_%s.tar.gz", hostID.String()), gomock.Any()).Return("url", nil)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
			LogsType:  &hostLogsType,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.GetPresignedForClusterFilesOK{}))
		replyPayload := generateReply.(*installer.GetPresignedForClusterFilesOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})
	It("host logs presigned happy flow without log type", func() {
		hostID := strfmt.UUID(uuid.New().String())
		host1 = addHost(hostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, fmt.Sprintf("mycluster_master_%s.tar.gz", hostID.String()), gomock.Any()).Return("url", nil)
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
		mockClusterApi.EXPECT().CreateTarredClusterLogs(ctx, gomock.Any(), gomock.Any()).Return("", errors.Errorf("dummy"))
		verifyApiError(bm.DownloadClusterLogs(ctx, params), http.StatusInternalServerError)
	})

	It("download cluster logs Download failed", func() {
		params := installer.DownloadClusterLogsParams{
			ClusterID: clusterID,
		}
		fileName := fmt.Sprintf("%s_logs.zip", clusterID)
		mockClusterApi.EXPECT().CreateTarredClusterLogs(ctx, gomock.Any(), gomock.Any()).Return(fileName, nil)
		mockS3Client.EXPECT().Download(ctx, fileName).Return(nil, int64(0), errors.Errorf("dummy"))
		verifyApiError(bm.DownloadClusterLogs(ctx, params), http.StatusInternalServerError)
	})

	It("download cluster logs happy flow", func() {
		params := installer.DownloadClusterLogsParams{
			ClusterID: clusterID,
		}
		fileName := fmt.Sprintf("%s/logs/cluster_logs.tar", clusterID)
		mockClusterApi.EXPECT().CreateTarredClusterLogs(ctx, gomock.Any(), gomock.Any()).Return(fileName, nil)
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.DownloadClusterLogs(ctx, params)
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewDownloadClusterLogsOK().WithPayload(r),
			fmt.Sprintf("mycluster_%s.tar", clusterID), 4)))
	})

	It("Logs presigned cluster logs failed", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockClusterApi.EXPECT().CreateTarredClusterLogs(ctx, gomock.Any(), gomock.Any()).Return("", errors.Errorf("dummy"))
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
		})
		verifyApiError(generateReply, http.StatusInternalServerError)
	})

	It("Logs presigned cluster logs happy flow", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockClusterApi.EXPECT().CreateTarredClusterLogs(ctx, gomock.Any(), gomock.Any()).Return("tarred", nil)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, "tarred", fmt.Sprintf("mycluster_%s.tar", clusterID.String()), gomock.Any()).Return("url", nil)
		generateReply := bm.GetPresignedForClusterFiles(ctx, installer.GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.GetPresignedForClusterFilesOK{}))
		replyPayload := generateReply.(*installer.GetPresignedForClusterFilesOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})

	It("Download unregistered cluster controller log success", func() {
		logsType := string(models.LogsTypeController)
		params := installer.DownloadClusterLogsParams{
			ClusterID: clusterID,
			LogsType:  &logsType,
		}
		c.ControllerLogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&c)
		dbReply := db.Delete(&host1)
		Expect(int(dbReply.RowsAffected)).Should(Equal(1))
		dbReply = db.Where("id = ?", clusterID).Delete(&common.Cluster{})
		Expect(int(dbReply.RowsAffected)).Should(Equal(1))
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		fileName := bm.getLogsFullName(clusterID.String(), logsType)
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.DownloadClusterLogs(ctx, params)
		downloadFileName := fmt.Sprintf("mycluster_%s_%s.tar.gz", clusterID, logsType)
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewDownloadClusterLogsOK().WithPayload(r), downloadFileName, 4)))
	})

	It("Download unregistered cluster controller log failure - permanently deleted", func() {
		logsType := string(models.LogsTypeController)
		params := installer.DownloadClusterLogsParams{
			ClusterID: clusterID,
			LogsType:  &logsType,
		}
		c.ControllerLogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&c)
		dbReply := db.Unscoped().Delete(&host1)
		Expect(int(dbReply.RowsAffected)).Should(Equal(1))
		dbReply = db.Unscoped().Where("id = ?", clusterID).Delete(&common.Cluster{})
		Expect(int(dbReply.RowsAffected)).Should(Equal(1))
		verifyApiError(bm.DownloadClusterLogs(ctx, params), http.StatusNotFound)
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
		dbName    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		c = common.Cluster{Cluster: models.Cluster{
			ID:                     &clusterID,
			BaseDNSDomain:          "example.com",
			OpenshiftVersion:       common.TestDefaultConfig.OpenShiftVersion,
			InstallConfigOverrides: `{"controlPlane": {"hyperthreading": "Disabled"}}`,
		}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("check get install config flow", func() {
		params := installer.GetClusterInstallConfigParams{ClusterID: clusterID}
		mockInstallConfigBuilder.EXPECT().GetInstallConfig(gomock.Any(), false, "").Return([]byte("some string"), nil).Times(1)
		response := bm.GetClusterInstallConfig(ctx, params)
		_, ok := response.(*installer.GetClusterInstallConfigOK)
		Expect(ok).To(BeTrue())
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
		dbName    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		c = common.Cluster{
			Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			},
		}

		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		mockUsageReports()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("saves the given string to the cluster", func() {
		override := `{"controlPlane": {"hyperthreading": "Disabled"}}`
		params := installer.UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: override,
		}
		mockEvents.EXPECT().AddEvent(gomock.Any(), params.ClusterID, nil, models.EventSeverityInfo, "Custom install config was applied to the cluster", gomock.Any())
		mockInstallConfigBuilder.EXPECT().ValidateInstallConfigPatch(gomock.Any(), params.InstallConfigParams).Return(nil).Times(1)
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
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns bad request when validation fails", func() {
		override := `{"controlPlane": {"hyperthreading": "Disabled"`
		params := installer.UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: override,
		}
		mockInstallConfigBuilder.EXPECT().ValidateInstallConfigPatch(gomock.Any(), params.InstallConfigParams).Return(fmt.Errorf("some error")).Times(1)
		response := bm.UpdateClusterInstallConfig(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})
})

var _ = Describe("GetDiscoveryIgnition", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		c         common.Cluster
		infraEnv  common.InfraEnv
		dbName    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		c = common.Cluster{Cluster: models.Cluster{
			ID:            &clusterID,
			PullSecretSet: true,
		}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		infraEnv = common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:            clusterID,
			PullSecretSet: true,
		}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
		err = db.Create(&infraEnv).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("returns successfully without overrides", func() {
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).Times(1)
		params := installer.GetDiscoveryIgnitionParams{ClusterID: clusterID}
		response := bm.GetDiscoveryIgnition(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.GetDiscoveryIgnitionOK{}))
		actual, ok := response.(*installer.GetDiscoveryIgnitionOK)
		Expect(ok).To(BeTrue())

		config, report, err := ign_3_1.Parse([]byte(actual.Payload.Config))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
	})

	It("returns not found with a non-existant cluster", func() {
		params := installer.GetDiscoveryIgnitionParams{ClusterID: strfmt.UUID(uuid.New().String())}
		response := bm.GetDiscoveryIgnition(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns successfully with overrides", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(override, nil).Times(1)
		db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update("ignition_config_overrides", override)

		params := installer.GetDiscoveryIgnitionParams{ClusterID: clusterID}
		response := bm.GetDiscoveryIgnition(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.GetDiscoveryIgnitionOK{}))
		actual, ok := response.(*installer.GetDiscoveryIgnitionOK)
		Expect(ok).To(BeTrue())

		config, report, err := ign_3_1.Parse([]byte(actual.Payload.Config))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())

		var file *ign_3_1_types.File
		for i, f := range config.Storage.Files {
			if f.Path == "/tmp/example" {
				file = &config.Storage.Files[i]
			}
		}
		Expect(file).NotTo(BeNil())
		Expect(*file.Contents.Source).To(Equal("data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"))
	})
})

var _ = Describe("UpdateDiscoveryIgnition", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		c         common.Cluster
		infraEnv  common.InfraEnv
		dbName    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		c = common.Cluster{Cluster: models.Cluster{ID: &clusterID}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		infraEnv = common.InfraEnv{InfraEnv: models.InfraEnv{ID: clusterID}}
		err = db.Create(&infraEnv).Error
		Expect(err).ShouldNot(HaveOccurred())
		mockUsageReports()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("saves the given string to the cluster", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateDiscoveryIgnitionParams{
			ClusterID:               clusterID,
			DiscoveryIgnitionParams: &models.DiscoveryIgnitionParams{Config: override},
		}
		mockS3Client.EXPECT().DeleteObject(gomock.Any(),
			fmt.Sprintf("%s.iso", fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, clusterID.String()))).Return(false, nil)
		mockEvents.EXPECT().AddEvent(gomock.Any(), clusterID, nil, models.EventSeverityInfo, "Custom discovery ignition config was applied to the cluster", gomock.Any())
		response := bm.UpdateDiscoveryIgnition(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.UpdateDiscoveryIgnitionCreated{}))

		var updatedCluster common.Cluster
		err := db.First(&updatedCluster, "id = ?", clusterID).Error
		Expect(err).ShouldNot(HaveOccurred())
		Expect(updatedCluster.IgnitionConfigOverrides).To(Equal(override))
	})

	It("returns not found with a non-existant cluster", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateDiscoveryIgnitionParams{
			ClusterID:               strfmt.UUID(uuid.New().String()),
			DiscoveryIgnitionParams: &models.DiscoveryIgnitionParams{Config: override},
		}
		response := bm.UpdateDiscoveryIgnition(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns bad request when provided invalid json", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}}}`
		params := installer.UpdateDiscoveryIgnitionParams{
			ClusterID:               clusterID,
			DiscoveryIgnitionParams: &models.DiscoveryIgnitionParams{Config: override},
		}
		response := bm.UpdateDiscoveryIgnition(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("returns bad request when provided invalid options", func() {
		// Missing the version
		override := `{"storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateDiscoveryIgnitionParams{
			ClusterID:               clusterID,
			DiscoveryIgnitionParams: &models.DiscoveryIgnitionParams{Config: override},
		}
		response := bm.UpdateDiscoveryIgnition(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("returns bad request when provided an old version", func() {
		// Wrong version
		override := `{"ignition": {"version": "3.0.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateDiscoveryIgnitionParams{
			ClusterID:               clusterID,
			DiscoveryIgnitionParams: &models.DiscoveryIgnitionParams{Config: override},
		}
		response := bm.UpdateDiscoveryIgnition(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("returns an error if we fail to delete the iso", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateDiscoveryIgnitionParams{
			ClusterID:               clusterID,
			DiscoveryIgnitionParams: &models.DiscoveryIgnitionParams{Config: override},
		}
		mockS3Client.EXPECT().DeleteObject(gomock.Any(),
			fmt.Sprintf("%s.iso", fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, clusterID.String()))).Return(false, fmt.Errorf("error"))
		mockEvents.EXPECT().AddEvent(gomock.Any(), params.ClusterID, nil, models.EventSeverityInfo, "Custom discovery ignition config was applied to the cluster", gomock.Any())
		response := bm.UpdateDiscoveryIgnition(ctx, params)
		verifyApiError(response, http.StatusInternalServerError)
	})

	It("adds an event if an old iso was removed", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateDiscoveryIgnitionParams{
			ClusterID:               clusterID,
			DiscoveryIgnitionParams: &models.DiscoveryIgnitionParams{Config: override},
		}
		mockS3Client.EXPECT().DeleteObject(gomock.Any(),
			fmt.Sprintf("%s.iso", fmt.Sprintf(s3wrapper.DiscoveryImageTemplate, clusterID.String()))).Return(true, nil)
		mockEvents.EXPECT().AddEvent(gomock.Any(), params.ClusterID, nil, models.EventSeverityInfo, "Custom discovery ignition config was applied to the cluster", gomock.Any())
		mockEvents.EXPECT().AddEvent(gomock.Any(), clusterID, nil, models.EventSeverityInfo, "Deleted image from backend because its ignition was updated. The image may be regenerated at any time.", gomock.Any())
		response := bm.UpdateDiscoveryIgnition(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.UpdateDiscoveryIgnitionCreated{}))
	})
})

var _ = Describe("GetSupportedPlatformsFromInventory", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		dbName    string
		ctx       = context.Background()
		clusterID strfmt.UUID
		c         *models.Cluster
	)

	BeforeEach(func() {
		cfg.DefaultNTPSource = ""
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
		bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
			db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil)
		mockUsageReports()
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)
		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                 swag.String("some-cluster-name"),
				OpenshiftVersion:     swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:           swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
		c = reply.(*installer.RegisterClusterCreated).Payload
		clusterID = *c.ID

	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	addHost := func(clusterId strfmt.UUID, inventory string, role models.HostRole) {
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(2)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, clusterId, clusterID, inventory, db)
	}

	addVsphereHost := func(clusterId strfmt.UUID, role models.HostRole) {
		const vsphereInventory = "{\"system_vendor\": {\"manufacturer\": \"VMware, Inc.\", \"product_name\": \"VMware7,1\", \"serial_number\": \"VMware-12 34 56 78 90 12 ab cd-ef gh 12 34 56 67 89 90\", \"virtual\": true}}"
		addHost(clusterId, vsphereInventory, role)
	}

	addGenericHost := func(clusterId strfmt.UUID, role models.HostRole) {
		const genericInventory = "{\"system_vendor\": {\"manufacturer\": \"Red Hat\", \"product_name\": \"KVM\", \"serial_number\": \"\", \"virtual\": true}}"
		addHost(clusterId, genericInventory, role)
	}

	validateInventory := func(host models.Host, manufacturer string) bool {
		var inventory models.Inventory
		if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
			Expect(err).ShouldNot(HaveOccurred())
		}
		return inventory.SystemVendor.Manufacturer == manufacturer
	}

	validateHostsInventory := func(vsphereHostsCount int, genericHostsCount int) {
		getReply := bm.GetCluster(ctx, installer.GetClusterParams{ClusterID: clusterID}).(*installer.GetClusterOK)
		Expect(len(getReply.Payload.Hosts)).Should(Equal(vsphereHostsCount + genericHostsCount))
		vsphereHosts := 0
		genericHosts := 0
		for _, h := range getReply.Payload.Hosts {
			if validateInventory(*h, common.VmwareManufacturer) {
				vsphereHosts++
			}
			if validateInventory(*h, "Red Hat") {
				genericHosts++
			}
		}

		Expect(vsphereHosts).Should(Equal(vsphereHostsCount))
		Expect(genericHosts).Should(Equal(genericHostsCount))

	}

	getClusterPlatforms := func() *[]models.PlatformType {
		platformReplay := bm.GetClusterSupportedPlatforms(ctx, installer.GetClusterSupportedPlatformsParams{ClusterID: clusterID})
		Expect(platformReplay).Should(BeAssignableToTypeOf(installer.NewGetClusterSupportedPlatformsOK()))
		return &platformReplay.(*installer.GetClusterSupportedPlatformsOK).Payload

	}

	It("no hosts", func() {
		platformReplay := bm.GetClusterSupportedPlatforms(ctx, installer.GetClusterSupportedPlatformsParams{ClusterID: clusterID})
		Expect(platformReplay).Should(BeAssignableToTypeOf(installer.NewGetClusterSupportedPlatformsOK()))
		platforms := platformReplay.(*installer.GetClusterSupportedPlatformsOK).Payload
		Expect(len(platforms)).Should(Equal(1))
		Expect(platforms[0]).Should(Equal(models.PlatformTypeBaremetal))
	})

	It("single SNO vsphere host", func() {
		*c.HighAvailabilityMode = models.ClusterHighAvailabilityModeNone
		db.Save(c)

		addVsphereHost(clusterID, models.HostRoleMaster)
		validateHostsInventory(1, 0)
		platformReplay := bm.GetClusterSupportedPlatforms(ctx, installer.GetClusterSupportedPlatformsParams{ClusterID: clusterID})
		Expect(platformReplay).Should(BeAssignableToTypeOf(installer.NewGetClusterSupportedPlatformsOK()))
		platforms := platformReplay.(*installer.GetClusterSupportedPlatformsOK).Payload
		Expect(len(platforms)).Should(Equal(1))
		Expect(platforms[0]).Should(Equal(models.PlatformTypeBaremetal))
	})

	It("single vsphere host", func() {
		addVsphereHost(clusterID, models.HostRoleMaster)
		validateHostsInventory(1, 0)
		platformReplay := bm.GetClusterSupportedPlatforms(ctx, installer.GetClusterSupportedPlatformsParams{ClusterID: clusterID})
		Expect(platformReplay).Should(BeAssignableToTypeOf(installer.NewGetClusterSupportedPlatformsOK()))
		platforms := platformReplay.(*installer.GetClusterSupportedPlatformsOK).Payload
		Expect(len(platforms)).Should(Equal(2))

		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeVsphere}
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})

	It("3 vsphere hosts", func() {
		supportedPlatforms := []models.PlatformType{models.PlatformTypeVsphere, models.PlatformTypeBaremetal}

		addVsphereHost(clusterID, models.HostRoleMaster)
		addVsphereHost(clusterID, models.HostRoleMaster)
		addVsphereHost(clusterID, models.HostRoleMaster)

		validateHostsInventory(3, 0)

		platforms := *getClusterPlatforms()
		Expect(len(platforms)).Should(Equal(2))
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})

	It("5 vsphere hosts", func() {
		supportedPlatforms := []models.PlatformType{models.PlatformTypeVsphere, models.PlatformTypeBaremetal}

		addVsphereHost(clusterID, models.HostRoleMaster)
		addVsphereHost(clusterID, models.HostRoleMaster)
		addVsphereHost(clusterID, models.HostRoleMaster)
		addVsphereHost(clusterID, models.HostRoleMaster)
		addVsphereHost(clusterID, models.HostRoleMaster)

		validateHostsInventory(5, 0)

		platforms := *getClusterPlatforms()
		Expect(len(platforms)).Should(Equal(2))
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})

	It("2 vsphere hosts 1 generic host", func() {
		addVsphereHost(clusterID, models.HostRoleMaster)
		addVsphereHost(clusterID, models.HostRoleMaster)
		addGenericHost(clusterID, models.HostRoleMaster)

		validateHostsInventory(2, 1)

		platforms := *getClusterPlatforms()
		Expect(len(platforms)).Should(Equal(1))
		Expect(platforms[0]).Should(Equal(models.PlatformTypeBaremetal))
	})

	It("3 vsphere masters 2 generic workers", func() {
		addVsphereHost(clusterID, models.HostRoleMaster)
		addVsphereHost(clusterID, models.HostRoleMaster)
		addVsphereHost(clusterID, models.HostRoleMaster)
		addGenericHost(clusterID, models.HostRoleMaster)
		addGenericHost(clusterID, models.HostRoleMaster)

		validateHostsInventory(3, 2)

		platforms := *getClusterPlatforms()
		Expect(len(platforms)).Should(Equal(1))
		Expect(platforms[0]).Should(Equal(models.PlatformTypeBaremetal))
	})
})

func verifyApiError(responder middleware.Responder, expectedHttpStatus int32) {
	ExpectWithOffset(1, responder).To(BeAssignableToTypeOf(common.NewApiError(expectedHttpStatus, nil)))
	concreteError := responder.(*common.ApiErrorResponse)
	ExpectWithOffset(1, concreteError.StatusCode()).To(Equal(expectedHttpStatus))
}

func verifyApiErrorString(responder middleware.Responder, expectedHttpStatus int32, expectedSubstring string) {
	ExpectWithOffset(1, responder).To(BeAssignableToTypeOf(common.NewApiError(expectedHttpStatus, nil)))
	concreteError := responder.(*common.ApiErrorResponse)
	ExpectWithOffset(1, concreteError.StatusCode()).To(Equal(expectedHttpStatus))
	ExpectWithOffset(1, concreteError.Error()).To(ContainSubstring(expectedSubstring))
}

func addHost(hostId strfmt.UUID, role models.HostRole, state, kind string, infraEnvId, clusterId strfmt.UUID, inventory string, db *gorm.DB) models.Host {
	host := models.Host{
		ID:         &hostId,
		InfraEnvID: infraEnvId,
		ClusterID:  &clusterId,
		Kind:       swag.String(kind),
		Status:     swag.String(state),
		Role:       role,
		Inventory:  inventory,
	}

	Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
	return host
}

func getInventoryStr(hostname, bootMode string, ipv4Addresses ...string) string {
	inventory := models.Inventory{
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: append(make([]string, 0), ipv4Addresses...),
				MacAddress:    "some MAC address",
			},
		},
		Hostname: hostname,
		Boot:     &models.Boot{CurrentBootMode: bootMode},
		Disks: []*models.Disk{
			{Path: "/dev/sda", Bootable: true},
			{Path: "/dev/sdb", Bootable: false},
		},
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

func getInventoryStrWithIPv6(hostname, bootMode string, ipv4Addresses []string, ipv6Addresses []string) string {
	inventory := models.Inventory{
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: ipv4Addresses,
				IPV6Addresses: ipv6Addresses,
				MacAddress:    "some MAC address",
			},
		},
		Hostname: hostname,
		Boot:     &models.Boot{CurrentBootMode: bootMode},
		Disks: []*models.Disk{
			{Path: "/dev/sda", Bootable: true},
			{Path: "/dev/sdb", Bootable: false},
		},
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

var _ = Describe("Register AddHostsCluster test", func() {

	var (
		bm            *bareMetalInventory
		cfg           Config
		db            *gorm.DB
		dbName        string
		ctx           = context.Background()
		clusterID     strfmt.UUID
		clusterName   string
		apiVIPDnsname string
		request       *http.Request
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		clusterName = "add-hosts-cluster"
		apiVIPDnsname = "api-vip.redhat.com"
		bm = createInventory(db, cfg)
		body := &bytes.Buffer{}
		request, _ = http.NewRequest("POST", "test", body)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("Create AddHosts cluster", func() {
		defaultHostNetworks := make([]*models.HostNetwork, 0)
		defaultHosts := make([]*models.Host, 0)

		params := installer.RegisterAddHostsClusterParams{
			HTTPRequest: request,
			NewAddHostsClusterParams: &models.AddHostsClusterCreateParams{
				APIVipDnsname:    &apiVIPDnsname,
				ID:               &clusterID,
				Name:             &clusterName,
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
			},
		}
		mockClusterApi.EXPECT().RegisterAddHostsCluster(ctx, gomock.Any(), true, gomock.Any()).Return(nil).Times(1)
		mockMetric.EXPECT().ClusterRegistered(common.TestDefaultConfig.ReleaseVersion, clusterID, "Unknown").Times(1)
		mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(1)
		res := bm.RegisterAddHostsCluster(ctx, params)
		actual := res.(*installer.RegisterAddHostsClusterCreated)

		Expect(actual.Payload.HostNetworks).To(Equal(defaultHostNetworks))
		Expect(actual.Payload.Hosts).To(Equal(defaultHosts))
		Expect(actual.Payload.OpenshiftVersion).To(Equal(common.TestDefaultConfig.ReleaseVersion))
		Expect(actual.Payload.OcpReleaseImage).To(Equal(common.TestDefaultConfig.ReleaseImage))
		Expect(res).Should(BeAssignableToTypeOf(installer.NewRegisterAddHostsClusterCreated()))
	})

	It("Create AddHosts cluster -  cluster id already exists", func() {
		params := installer.RegisterAddHostsClusterParams{
			HTTPRequest: request,
			NewAddHostsClusterParams: &models.AddHostsClusterCreateParams{
				APIVipDnsname:    &apiVIPDnsname,
				ID:               &clusterID,
				Name:             &clusterName,
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
			},
		}
		err := db.Create(&common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			Kind:             swag.String(models.ClusterKindAddHostsCluster),
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusAddingHosts),
		}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		res := bm.RegisterAddHostsCluster(ctx, params)
		verifyApiError(res, http.StatusBadRequest)
	})
})

var _ = Describe("Reset Host test", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		hostID    strfmt.UUID
		dbName    string
		request   *http.Request
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		hostID = strfmt.UUID(uuid.New().String())
		err := db.Create(&common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			Kind:             swag.String(models.ClusterKindAddHostsCluster),
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusAddingHosts),
		}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("V2 Reset day2 host", func() {
		params := installer.V2ResetHostParams{
			HTTPRequest: request,
			InfraEnvID:  clusterID,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().ResetHost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		res := bm.V2ResetHost(ctx, params)
		Expect(res).Should(BeAssignableToTypeOf(installer.NewV2ResetHostOK()))
	})

	It("Reset day2 host", func() {
		params := installer.ResetHostParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().ResetHost(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		res := bm.ResetHost(ctx, params)
		Expect(res).Should(BeAssignableToTypeOf(installer.NewResetHostOK()))
	})

	It("V2 Reset day2 host, host not found", func() {
		params := installer.V2ResetHostParams{
			HTTPRequest: request,
			InfraEnvID:  clusterID,
			HostID:      strfmt.UUID(uuid.New().String()),
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		res := bm.V2ResetHost(ctx, params)
		verifyApiError(res, http.StatusNotFound)
	})

	It("Reset day2 host, host not found", func() {
		params := installer.ResetHostParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
			HostID:      strfmt.UUID(uuid.New().String()),
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		res := bm.ResetHost(ctx, params)
		verifyApiError(res, http.StatusNotFound)
	})

	It("Reset day2 host, host is not day2 host", func() {
		params := installer.ResetHostParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		res := bm.ResetHost(ctx, params)
		verifyApiError(res, http.StatusConflict)
	})

	It("V2 Reset day2 host, host is not day2 host", func() {
		params := installer.V2ResetHostParams{
			HTTPRequest: request,
			InfraEnvID:  clusterID,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		res := bm.V2ResetHost(ctx, params)
		verifyApiError(res, http.StatusConflict)
	})
})

var _ = Describe("Install Host test", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		clusterID  strfmt.UUID
		infraEnvId strfmt.UUID
		hostID     strfmt.UUID
		dbName     string
		request    *http.Request
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvId = clusterID
		hostID = strfmt.UUID(uuid.New().String())
		err := db.Create(&common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			Kind:             swag.String(models.ClusterKindAddHostsCluster),
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusAddingHosts),
		}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("Install day2 host", func() {
		params := installer.InstallHostParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
		res := bm.InstallHost(ctx, params)
		Expect(res).Should(BeAssignableToTypeOf(installer.NewInstallHostAccepted()))
	})

	It("[V2] Install day2 host", func() {
		params := installer.V2InstallHostParams{
			HTTPRequest: request,
			InfraEnvID:  infraEnvId,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
		res := bm.V2InstallHost(ctx, params)
		Expect(res).Should(BeAssignableToTypeOf(installer.NewInstallHostAccepted()))
	})

	It("Install day2 host - host not found", func() {
		params := installer.InstallHostParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
			HostID:      strfmt.UUID(uuid.New().String()),
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		res := bm.InstallHost(ctx, params)
		verifyApiError(res, http.StatusNotFound)
	})

	It("[V2] Install day2 host - host not found", func() {
		params := installer.V2InstallHostParams{
			HTTPRequest: request,
			InfraEnvID:  infraEnvId,
			HostID:      strfmt.UUID(uuid.New().String()),
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		res := bm.V2InstallHost(ctx, params)
		verifyApiError(res, http.StatusNotFound)
	})

	It("Install day2 host - not a day2 host", func() {
		params := installer.InstallHostParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		res := bm.InstallHost(ctx, params)
		verifyApiError(res, http.StatusConflict)
	})

	It("[V2] Install day2 host - not a day2 host", func() {
		params := installer.V2InstallHostParams{
			HTTPRequest: request,
			InfraEnvID:  infraEnvId,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		res := bm.V2InstallHost(ctx, params)
		verifyApiError(res, http.StatusConflict)
	})

	It("Install day2 host - host not in known state", func() {
		params := installer.InstallHostParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusInsufficient, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		res := bm.InstallHost(ctx, params)
		verifyApiError(res, http.StatusConflict)
	})

	It("[V2] Install day2 host - host not in known state", func() {
		params := installer.V2InstallHostParams{
			HTTPRequest: request,
			InfraEnvID:  infraEnvId,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusInsufficient, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		res := bm.V2InstallHost(ctx, params)
		verifyApiError(res, http.StatusConflict)
	})

	It("Install day2 host - ignition creation failed", func() {
		params := installer.InstallHostParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")).Times(0)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("ign failure")).Times(1)
		res := bm.InstallHost(ctx, params)
		verifyApiError(res, http.StatusInternalServerError)
	})

	It("[V2] Install day2 host - ignition creation failed", func() {
		params := installer.V2InstallHostParams{
			HTTPRequest: request,
			InfraEnvID:  infraEnvId,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")).Times(0)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("ign failure")).Times(1)
		res := bm.V2InstallHost(ctx, params)
		verifyApiError(res, http.StatusInternalServerError)
	})

	It("Install day2 host - ignition upload failed", func() {
		params := installer.InstallHostParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
		res := bm.InstallHost(ctx, params)
		verifyApiError(res, http.StatusInternalServerError)
	})

	It("[V2] Install day2 host - ignition upload failed", func() {
		params := installer.V2InstallHostParams{
			HTTPRequest: request,
			InfraEnvID:  infraEnvId,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
		res := bm.V2InstallHost(ctx, params)
		verifyApiError(res, http.StatusInternalServerError)
	})
})

var _ = Describe("InstallSingleDay2Host test", func() {

	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		dbName    string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		err := db.Create(&common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			Kind:             swag.String(models.ClusterKindAddHostsCluster),
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusAddingHosts),
		}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("Install Single Day2 Host", func() {
		hostId := strfmt.UUID(uuid.New().String())
		addHost(hostId, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
		res := bm.InstallSingleDay2HostInternal(ctx, clusterID, clusterID, hostId)
		Expect(res).Should(BeNil())
	})

	It("Install fail Single Day2 Host", func() {
		expectedErrMsg := "some-internal-error"
		hostId := strfmt.UUID(uuid.New().String())
		addHost(hostId, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New(expectedErrMsg)).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
		res := bm.InstallSingleDay2HostInternal(ctx, clusterID, clusterID, hostId)
		Expect(res.Error()).Should(Equal(expectedErrMsg))
	})
})

var _ = Describe("Transform day1 cluster to a day2 cluster test", func() {

	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		dbName    string
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		err := db.Create(&common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			Kind:             swag.String(models.ClusterKindCluster),
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusInstalled),
		}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("successfully transform day1 cluster to a day2 cluster", func() {
		mockClusterApi.EXPECT().TransformClusterToDay2(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(nil)
		_, err := bm.TransformClusterToDay2Internal(ctx, clusterID)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("fail to transform day1 cluster to a day2 cluster", func() {
		mockClusterApi.EXPECT().TransformClusterToDay2(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(errors.New("blah"))
		_, err := bm.TransformClusterToDay2Internal(ctx, clusterID)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("Install Hosts test", func() {

	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		dbName    string
		request   *http.Request
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		err := db.Create(&common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			Kind:             swag.String(models.ClusterKindAddHostsCluster),
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusAddingHosts),
		}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		bm = createInventory(db, cfg)
		body := &bytes.Buffer{}
		request, _ = http.NewRequest("POST", "test", body)
		mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("InstallHosts all known", func() {
		params := installer.InstallHostsParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
		}
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname2", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(3)
		res := bm.InstallHosts(ctx, params)
		Expect(res).Should(BeAssignableToTypeOf(installer.NewInstallHostsAccepted()))
	})

	It("InstallHosts not all known", func() {
		params := installer.InstallHostsParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
		}
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusInstalling, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusInsufficient, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		knownHostID := strfmt.UUID(uuid.New().String())
		addHost(knownHostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname2", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusInstalled, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname3", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusAddedToExistingCluster, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname4", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(5)
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		fileName := fmt.Sprintf("%s/worker-%s.ign", clusterID, knownHostID)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), fileName).Return(nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
		res := bm.InstallHosts(ctx, params)
		Expect(res).Should(BeAssignableToTypeOf(installer.NewInstallHostsAccepted()))
	})

	It("InstallHosts all not known", func() {
		params := installer.InstallHostsParams{
			HTTPRequest: request,
			ClusterID:   clusterID,
		}
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusInstalling, models.HostKindAddToExistingClusterHost, clusterID, clusterID, "", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusInsufficient, models.HostKindAddToExistingClusterHost, clusterID, clusterID, "", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusDisconnected, models.HostKindAddToExistingClusterHost, clusterID, clusterID, "", db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(0)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(0)
		res := bm.InstallHosts(ctx, params)
		Expect(res).Should(BeAssignableToTypeOf(installer.NewInstallHostsAccepted()))
	})
})

var _ = Describe("TestRegisterCluster", func() {
	var (
		bm     *bareMetalInventory
		cfg    Config
		db     *gorm.DB
		dbName string
		ctx    = context.Background()
	)

	BeforeEach(func() {
		cfg.DefaultNTPSource = ""
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
		bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
			db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil)
		mockUsageReports()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("success", func() {
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("some-cluster-name"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
	})

	It("SchedulableMasters default value", func() {
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("some-cluster-name"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
		actual := reply.(*installer.RegisterClusterCreated)
		Expect(actual.Payload.SchedulableMasters).To(Equal(swag.Bool(false)))
	})

	It("SchedulableMasters non default value", func() {
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:               swag.String("some-cluster-name"),
				OpenshiftVersion:   swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:         swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				SchedulableMasters: swag.Bool(true),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
		actual := reply.(*installer.RegisterClusterCreated)
		Expect(actual.Payload.SchedulableMasters).To(Equal(swag.Bool(true)))
	})

	It("UserManagedNetworking default value", func() {
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("some-cluster-name"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
		actual := reply.(*installer.RegisterClusterCreated)
		Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(false)))
	})

	It("UserManagedNetworking non default value", func() {
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                  swag.String("some-cluster-name"),
				OpenshiftVersion:      swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:            swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				UserManagedNetworking: swag.Bool(true),
				VipDhcpAllocation:     swag.Bool(false),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
		actual := reply.(*installer.RegisterClusterCreated)
		Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
	})

	It("Fail UserManagedNetworking with VIP DHCP", func() {
		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                  swag.String("some-cluster-name"),
				OpenshiftVersion:      swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:            swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				UserManagedNetworking: swag.Bool(true),
				VipDhcpAllocation:     swag.Bool(true),
			},
		})
		verifyApiError(reply, http.StatusBadRequest)
	})

	It("Fail UserManagedNetworking with Ingress Vip", func() {
		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                  swag.String("some-cluster-name"),
				OpenshiftVersion:      swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:            swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				UserManagedNetworking: swag.Bool(true),
				IngressVip:            "10.35.10.10",
			},
		})
		verifyApiError(reply, http.StatusBadRequest)
	})

	Context("Disk encryption", func() {

		It("Using tang mode without tang_servers", func() {
			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					DiskEncryption: &models.DiskEncryption{
						EnableOn: models.DiskEncryptionEnableOnAll,
						Mode:     models.DiskEncryptionModeTang,
					},
				},
			})
			verifyApiErrorString(reply, http.StatusBadRequest, "Setting Tang mode but tang_servers isn't set")
		})

		It("Invalid Tang server URL", func() {

			By("URL not set", func() {
				reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn:    models.DiskEncryptionEnableOnAll,
							Mode:        models.DiskEncryptionModeTang,
							TangServers: `[{"URL":"","Thumbprint":""}]`,
						},
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, "empty url")
			})

			By("URL not valid", func() {
				reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn:    models.DiskEncryptionEnableOnAll,
							Mode:        models.DiskEncryptionModeTang,
							TangServers: `[{"URL":"invalidUrl","Thumbprint":""}]`,
						},
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, "invalid URI for reques")
			})
		})

		It("Tang thumbprint not set", func() {
			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					DiskEncryption: &models.DiskEncryption{
						EnableOn:    models.DiskEncryptionEnableOnAll,
						Mode:        models.DiskEncryptionModeTang,
						TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
					},
				},
			})
			verifyApiErrorString(reply, http.StatusBadRequest, "Tang thumbprint isn't set")
		})

	})

	Context("NTPSource", func() {
		It("NTPSource default value", func() {
			defaultNtpSource := "clock.redhat.com"
			bm.Config.DefaultNTPSource = defaultNtpSource

			mockClusterRegisterSuccess(bm, true)
			mockAMSSubscription(ctx)

			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("some-cluster-name"),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
			actual := reply.(*installer.RegisterClusterCreated)
			Expect(actual.Payload.AdditionalNtpSource).To(Equal(defaultNtpSource))
		})

		It("NTPSource non default value", func() {
			newNtpSource := "new.ntp.source"

			mockClusterRegisterSuccess(bm, true)
			mockAMSSubscription(ctx)

			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                swag.String("some-cluster-name"),
					OpenshiftVersion:    swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:          swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
					AdditionalNtpSource: &newNtpSource,
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
			actual := reply.(*installer.RegisterClusterCreated)
			Expect(actual.Payload.AdditionalNtpSource).To(Equal(newNtpSource))
		})
	})

	It("cluster api failed to register", func() {
		bm.clusterApi = mockClusterApi
		mockClusterApi.EXPECT().RegisterCluster(ctx, gomock.Any(), true, gomock.Any()).Return(errors.Errorf("error")).Times(1)
		mockClusterRegisterSteps()

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("some-cluster-name"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}`),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusInternalServerError, errors.Errorf("error"))))
	})

	It("Host Networks default value", func() {
		defaultHostNetworks := make([]*models.HostNetwork, 0)
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("some-cluster-name"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
			},
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
		actual := reply.(*installer.RegisterClusterCreated)
		Expect(actual.Payload.HostNetworks).To(Equal(defaultHostNetworks))
	})

	It("cluster api failed to register with invalid pull secret", func() {
		mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(errors.New("error")).Times(1)
		mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{}).Times(1)
		mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(1)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("some-cluster-name"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:       swag.String(""),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("openshift version not supported", func() {
		mockVersions.EXPECT().GetOpenshiftVersion(gomock.Any(), gomock.Any()).Return(nil, errors.Errorf("OpenShift VVersion is not supported")).Times(1)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("some-cluster-name"),
				OpenshiftVersion: swag.String("999"),
				PullSecret:       swag.String(""),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("openshift release image and version successfully defined", func() {
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)
		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("some-cluster-name"),
				PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
			},
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
		actual := reply.(*installer.RegisterClusterCreated)
		Expect(actual.Payload.OpenshiftVersion).To(Equal(common.TestDefaultConfig.ReleaseVersion))
		Expect(actual.Payload.OcpReleaseImage).To(Equal(common.TestDefaultConfig.ReleaseImage))
	})

	It("Register cluster with default CPU architecture", func() {
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("some-cluster-name"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				CPUArchitecture:  common.TestDefaultConfig.CPUArchitecture,
				PullSecret:       swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
			},
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
		actual := reply.(*installer.RegisterClusterCreated)
		Expect(actual.Payload.CPUArchitecture).To(Equal(common.TestDefaultConfig.CPUArchitecture))
	})

	It("Register cluster with arm64 CPU architecture", func() {
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)
		mockVersions.EXPECT().GetCPUArchitectures(gomock.Any()).Return(
			[]string{common.TestDefaultConfig.OpenShiftVersion, "arm64"}, nil).Times(1)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                  swag.String("some-cluster-name"),
				OpenshiftVersion:      swag.String(common.TestDefaultConfig.OpenShiftVersion),
				CPUArchitecture:       "arm64",
				UserManagedNetworking: swag.Bool(true),
				VipDhcpAllocation:     swag.Bool(false),
				PullSecret:            swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
			},
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
		actual := reply.(*installer.RegisterClusterCreated)
		Expect(actual.Payload.CPUArchitecture).To(Equal("arm64"))
	})

	It("Register cluster with arm64 CPU architecture - without UserManagedNetworking", func() {
		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                  swag.String("some-cluster-name"),
				OpenshiftVersion:      swag.String(common.TestDefaultConfig.OpenShiftVersion),
				CPUArchitecture:       "arm64",
				UserManagedNetworking: swag.Bool(false),
				PullSecret:            swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("Register cluster without specified CPU architecture", func() {
		mockClusterRegisterSuccess(bm, true)
		mockAMSSubscription(ctx)

		reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("some-cluster-name"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:       swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
			},
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
		actual := reply.(*installer.RegisterClusterCreated)
		Expect(actual.Payload.CPUArchitecture).To(Equal(common.TestDefaultConfig.CPUArchitecture))
	})

	Context("Networking", func() {
		var (
			clusterNetworks = []*models.ClusterNetwork{{Cidr: "1.1.1.0/24", HostPrefix: 24}, {Cidr: "2.2.2.0/24", HostPrefix: 24}}
			serviceNetworks = []*models.ServiceNetwork{{Cidr: "3.3.3.0/24"}, {Cidr: "4.4.4.0/24"}}
			machineNetworks = []*models.MachineNetwork{{Cidr: "5.5.5.0/24"}, {Cidr: "6.6.6.0/24"}}
		)

		registerCluster := func() *models.Cluster {
			mockClusterRegisterSuccess(bm, true)
			mockAMSSubscription(ctx)
			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("some-cluster-name"),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					ClusterNetworks:  clusterNetworks,
					ServiceNetworks:  serviceNetworks,
					MachineNetworks:  machineNetworks,
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
			return reply.(*installer.RegisterClusterCreated).Payload
		}

		It("Networking defaults", func() {
			defaultClusterNetwork := "1.2.3.4/14"
			bm.Config.DefaultClusterNetworkCidr = defaultClusterNetwork
			defultServiceNetwork := "1.2.3.5/14"
			bm.Config.DefaultServiceNetworkCidr = defultServiceNetwork

			mockClusterRegisterSuccess(bm, true)
			mockAMSSubscription(ctx)
			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("some-cluster-name"),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterClusterCreated())))
			actual := reply.(*installer.RegisterClusterCreated)
			Expect(actual.Payload.ClusterNetworks).To(HaveLen(1))
			Expect(string(actual.Payload.ClusterNetworks[0].Cidr)).To(Equal(defaultClusterNetwork))
			Expect(actual.Payload.ServiceNetworks).To(HaveLen(1))
			Expect(string(actual.Payload.ServiceNetworks[0].Cidr)).To(Equal(defultServiceNetwork))
		})

		It("Multiple networks single cluster", func() {
			c := registerCluster()
			validateNetworkConfiguration(c, &clusterNetworks, &serviceNetworks, &machineNetworks)
		})

		It("Multiple networks multiple clusters", func() {
			By("Register")
			c1 := registerCluster()
			c2 := registerCluster()
			validateNetworkConfiguration(c1, &clusterNetworks, &serviceNetworks, &machineNetworks)
			validateNetworkConfiguration(c2, &clusterNetworks, &serviceNetworks, &machineNetworks)

			By("Check DB")
			cluster, err := common.GetClusterFromDB(db, *c1.ID, common.UseEagerLoading)
			Expect(err).ToNot(HaveOccurred())
			validateNetworkConfiguration(&cluster.Cluster, &clusterNetworks, &serviceNetworks, &machineNetworks)

			cluster, err = common.GetClusterFromDB(db, *c2.ID, common.UseEagerLoading)
			Expect(err).ToNot(HaveOccurred())
			validateNetworkConfiguration(&cluster.Cluster, &clusterNetworks, &serviceNetworks, &machineNetworks)
		})
	})
})

var _ = Describe("AMS subscriptions", func() {

	var (
		ctx         = context.Background()
		cfg         Config
		bm          *bareMetalInventory
		db          *gorm.DB
		dbName      string
		clusterName = "ams-cluster"
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
		bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil)
		mockUsageReports()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("With AMS subscriptions", func() {

		It("register cluster happy flow", func() {
			mockClusterRegisterSuccess(bm, true)
			mockAMSSubscription(ctx)

			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String(clusterName),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			actual := reply.(*installer.RegisterClusterCreated)
			c, err := bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(c.AmsSubscriptionID).To(Equal(strfmt.UUID("")))
		})

		It("register cluster - deregister if we failed to create AMS subscription", func() {
			bm.clusterApi = mockClusterApi
			mockClusterApi.EXPECT().RegisterCluster(ctx, gomock.Any(), true, gomock.Any()).Return(nil)
			mockClusterRegisterSteps()
			mockAccountsMgmt.EXPECT().CreateSubscription(ctx, gomock.Any(), clusterName).Return(nil, errors.New("dummy"))
			mockClusterApi.EXPECT().DeregisterCluster(ctx, gomock.Any())

			err := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String(clusterName),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(err).To(HaveOccurred())
		})

		It("register cluster - delete AMS subscription if we failed to patch DB with ams_subscription_id", func() {
			bm.clusterApi = mockClusterApi
			mockClusterApi.EXPECT().RegisterCluster(ctx, gomock.Any(), true, gomock.Any()).Return(nil)
			mockClusterRegisterSteps()
			mockAMSSubscription(ctx)
			mockClusterApi.EXPECT().UpdateAmsSubscriptionID(ctx, gomock.Any(), strfmt.UUID("")).Return(common.NewApiError(http.StatusInternalServerError, errors.New("dummy")))
			mockClusterApi.EXPECT().DeregisterCluster(ctx, gomock.Any())
			mockAccountsMgmt.EXPECT().DeleteSubscription(ctx, strfmt.UUID("")).Return(nil)

			err := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("ams-cluster"),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(err).To(HaveOccurred())
		})

		It("deregister cluster that don't have 'Reserved' subscriptions", func() {
			mockS3Client = s3wrapper.NewMockAPI(ctrl)
			mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, mockEvents, nil, nil, nil, nil, nil, nil, mockS3Client, nil)
			mockClusterRegisterSuccess(bm, true)
			mockAMSSubscription(ctx)

			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("ams-cluster"),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			clusterID := *reply.(*installer.RegisterClusterCreated).Payload.ID

			mockAccountsMgmt.EXPECT().GetSubscription(ctx, gomock.Any()).Return(&amgmtv1.Subscription{}, nil)
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.DeregisteredClusterEventName)))

			reply = bm.DeregisterCluster(ctx, installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(reply).Should(BeAssignableToTypeOf(&installer.DeregisterClusterNoContent{}))
		})

		It("update cluster name happy flow", func() {
			mockOperators := operators.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, mockEvents, nil, nil, nil, nil, mockOperators, nil, nil, nil)

			mockClusterRegisterSuccess(bm, true)
			mockAMSSubscription(ctx)

			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String(clusterName),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			actual := reply.(*installer.RegisterClusterCreated)
			c, err := bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())

			newClusterName := "ams-cluster-new-name"
			mockOperators.EXPECT().ValidateCluster(ctx, gomock.Any())
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName)))
			mockAccountsMgmt.EXPECT().UpdateSubscriptionDisplayName(ctx, c.AmsSubscriptionID, newClusterName).Return(nil)

			reply = bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: *c.ID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					Name: &newClusterName,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
		})

		It("update cluster name with same name", func() {
			mockOperators := operators.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, mockEvents, nil, nil, nil, nil, mockOperators, nil, nil, nil)

			mockClusterRegisterSuccess(bm, true)
			mockAMSSubscription(ctx)

			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String(clusterName),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			actual := reply.(*installer.RegisterClusterCreated)
			c, err := bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())

			mockOperators.EXPECT().ValidateCluster(ctx, gomock.Any())
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName)))

			reply = bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: *c.ID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					Name: &clusterName,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
		})

		It("update cluster without name field", func() {
			mockOperators := operators.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, mockEvents, nil, nil, nil, nil, mockOperators, nil, nil, nil)

			mockClusterRegisterSuccess(bm, true)
			mockAMSSubscription(ctx)

			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String(clusterName),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			actual := reply.(*installer.RegisterClusterCreated)
			c, err := bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())

			mockOperators.EXPECT().ValidateCluster(ctx, gomock.Any())
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName)))

			dummyDNSDomain := "dummy.test"
			reply = bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: *c.ID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					BaseDNSDomain: &dummyDNSDomain,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
		})

		tests := []struct {
			status string
		}{
			{status: "succeed"},
			{status: "failed"},
		}

		for i := range tests {
			test := tests[i]

			ignitionReader := ioutil.NopCloser(strings.NewReader(`{
					"ignition":{"version":"3.1.0"},
					"storage":{
						"files":[
							{
								"path":"/opt/openshift/manifests/cvo-overrides.yaml",
								"contents":{
									"source":"data:text/plain;charset=utf-8;base64,YXBpVmVyc2lvbjogY29uZmlnLm9wZW5zaGlmdC5pby92MQpraW5kOiBDbHVzdGVyVmVyc2lvbgptZXRhZGF0YToKICBuYW1lc3BhY2U6IG9wZW5zaGlmdC1jbHVzdGVyLXZlcnNpb24KICBuYW1lOiB2ZXJzaW9uCnNwZWM6CiAgdXBzdHJlYW06IGh0dHBzOi8vYXBpLm9wZW5zaGlmdC5jb20vYXBpL3VwZ3JhZGVzX2luZm8vdjEvZ3JhcGgKICBjaGFubmVsOiBzdGFibGUtNC42CiAgY2x1c3RlcklEOiA0MTk0MGVlOC1lYzk5LTQzZGUtODc2Ni0xNzQzODFiNDkyMWQK"
								}
							}
						]
					},
					"systemd":{}
			}`))

			It(fmt.Sprintf("InstallCluster %s to update openshift_cluster_id in AMS", test.status), func() {

				doneChannel := make(chan int)
				waitForDoneChannel := func() {
					select {
					case <-doneChannel:
						break
					case <-time.After(1 * time.Second):
						panic("not all api calls where made")
					}
				}

				bm.clusterApi = mockClusterApi
				clusterID := strfmt.UUID(uuid.New().String())

				By("register cluster", func() {
					err := db.Create(&common.Cluster{Cluster: models.Cluster{
						ID:               &clusterID,
						OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
						Status:           swag.String(models.ClusterStatusReady),
					}}).Error
					Expect(err).ShouldNot(HaveOccurred())
				})

				By("install cluster", func() {
					mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
					mockClusterApi.EXPECT().IsReadyForInstallation(gomock.Any()).Return(true, "")
					mockClusterApi.EXPECT().PrepareForInstallation(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					masterHostId := strfmt.UUID(uuid.New().String())
					mockClusterApi.EXPECT().GetMasterNodesIds(ctx, gomock.Any(), gomock.Any()).Return([]*strfmt.UUID{&masterHostId, &masterHostId, &masterHostId}, nil)
					mockClusterApi.EXPECT().GenerateAdditionalManifests(gomock.Any(), gomock.Any()).Return(nil)
					mockClusterApi.EXPECT().DeleteClusterLogs(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					mockGetInstallConfigSuccess(mockInstallConfigBuilder)
					mockGenerateInstallConfigSuccess(mockGenerator, mockVersions)
					mockS3Client.EXPECT().Download(gomock.Any(), gomock.Any()).Return(ignitionReader, int64(0), nil).MinTimes(0)
					if test.status == "succeed" {
						mockAccountsMgmt.EXPECT().UpdateSubscriptionOpenshiftClusterID(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
						mockClusterApi.EXPECT().HandlePreInstallSuccess(gomock.Any(), gomock.Any()).Times(1).Do(func(ctx, c interface{}) { doneChannel <- 1 })
					} else {
						mockAccountsMgmt.EXPECT().UpdateSubscriptionOpenshiftClusterID(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("dummy"))
						mockClusterApi.EXPECT().HandlePreInstallError(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Do(func(ctx, c, err interface{}) { doneChannel <- 1 })
					}

					reply := bm.InstallCluster(ctx, installer.InstallClusterParams{
						ClusterID: clusterID,
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewInstallClusterAccepted()))
					waitForDoneChannel()
				})
			})
		}

		It("register and deregister cluster happy flow - nil OCM client", func() {
			mockS3Client = s3wrapper.NewMockAPI(ctrl)
			mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Return(false, nil).Times(1)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, mockEvents, nil, nil, nil, nil, nil, nil, mockS3Client, nil)
			bm.ocmClient = nil
			mockClusterRegisterSuccess(bm, true)

			reply := bm.RegisterCluster(ctx, installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("ams-cluster"),
					OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
					PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
				},
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			clusterID := *reply.(*installer.RegisterClusterCreated).Payload.ID

			// deregister
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.DeregisteredClusterEventName)))

			reply = bm.DeregisterCluster(ctx, installer.DeregisterClusterParams{ClusterID: clusterID})
			Expect(reply).Should(BeAssignableToTypeOf(&installer.DeregisterClusterNoContent{}))
		})
	})
})

var _ = Describe("extract image version", func() {

	tag := uuid.New().String()
	image := "quay.io/ocpmetal/image"

	It("image contains name and tag", func() {
		Expect(extractImageTag(fmt.Sprintf("%s:%s", image, tag))).Should(Equal(tag))
	})

	It("image does not contain tag", func() {
		Expect(extractImageTag(image)).Should(Equal(image))
	})

	It("image contains multiple colons", func() {
		Expect(extractImageTag(fmt.Sprintf("%s:%s:last_element", image, tag))).Should(Equal("last_element"))
	})

	It("image is an empty string", func() {
		Expect(extractImageTag("")).Should(Equal(""))
	})
})

var _ = Describe("convert pull secret validation error to user error", func() {

	It("with a secret validation error", func() {
		err := secretValidationToUserError(&validations.PullSecretError{Msg: "user error"})
		Expect(err.Error()).Should(Equal("user error"))
	})

	It("with a non-validation error", func() {
		err := secretValidationToUserError(errors.New("other error"))
		Expect(err.Error()).Should(Equal("Failed validating pull secret"))
	})
})

var _ = Describe("GetHostIgnition and DownloadHostIgnition", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		dbName    string
		clusterID strfmt.UUID
		hostID    strfmt.UUID
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)

		// create a cluster
		clusterID = strfmt.UUID(uuid.New().String())
		status := models.ClusterStatusInstalling
		c := common.Cluster{Cluster: models.Cluster{ID: &clusterID, Status: &status}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())

		// add some hosts
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("return not found when given a non-existent cluster", func() {
		otherClusterID := strfmt.UUID(uuid.New().String())

		getParams := installer.GetHostIgnitionParams{
			ClusterID: otherClusterID,
			HostID:    hostID,
		}
		resp := bm.GetHostIgnition(ctx, getParams)
		verifyApiError(resp, http.StatusNotFound)

		downloadParams := installer.DownloadHostIgnitionParams{
			ClusterID: otherClusterID,
			HostID:    hostID,
		}
		resp = bm.DownloadHostIgnition(ctx, downloadParams)
		verifyApiError(resp, http.StatusNotFound)
	})

	It("return not found for a host in a different cluster", func() {
		otherClusterID := strfmt.UUID(uuid.New().String())
		c := common.Cluster{Cluster: models.Cluster{ID: &otherClusterID}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		otherHostID := strfmt.UUID(uuid.New().String())
		addHost(otherHostID, models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, otherClusterID, otherClusterID, "{}", db)

		getParams := installer.GetHostIgnitionParams{
			ClusterID: clusterID,
			HostID:    otherHostID,
		}
		resp := bm.GetHostIgnition(ctx, getParams)
		verifyApiError(resp, http.StatusNotFound)

		downloadParams := installer.DownloadHostIgnitionParams{
			ClusterID: clusterID,
			HostID:    otherHostID,
		}
		resp = bm.DownloadHostIgnition(ctx, downloadParams)
		verifyApiError(resp, http.StatusNotFound)
	})

	It("return conflict when the cluster is in the incorrect status", func() {
		db.Model(&common.Cluster{}).Where("id = ?", clusterID.String()).Update("status", models.ClusterStatusInsufficient)

		getParams := installer.GetHostIgnitionParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		resp := bm.GetHostIgnition(ctx, getParams)
		verifyApiError(resp, http.StatusConflict)

		downloadParams := installer.DownloadHostIgnitionParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		resp = bm.DownloadHostIgnition(ctx, downloadParams)
		verifyApiError(resp, http.StatusConflict)
	})

	It("return server error when the download fails", func() {
		mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/master-%s.ign", clusterID, hostID)).Return(nil, int64(0), errors.Errorf("download failed")).Times(2)

		getParams := installer.GetHostIgnitionParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		resp := bm.GetHostIgnition(ctx, getParams)
		verifyApiError(resp, http.StatusInternalServerError)

		downloadParams := installer.DownloadHostIgnitionParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		resp = bm.DownloadHostIgnition(ctx, downloadParams)
		verifyApiError(resp, http.StatusInternalServerError)
	})

	It("return the correct content", func() {
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/master-%s.ign", clusterID, hostID)).Return(r, int64(4), nil).Times(2)

		getParams := installer.GetHostIgnitionParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		resp := bm.GetHostIgnition(ctx, getParams)
		Expect(resp).To(BeAssignableToTypeOf(&installer.GetHostIgnitionOK{}))
		replyPayload := resp.(*installer.GetHostIgnitionOK).Payload
		Expect(replyPayload.Config).Should(Equal("test"))

		downloadParams := installer.DownloadHostIgnitionParams{
			ClusterID: clusterID,
			HostID:    hostID,
		}
		resp = bm.DownloadHostIgnition(ctx, downloadParams)
		Expect(resp).Should(Equal(filemiddleware.NewResponder(installer.NewDownloadHostIgnitionOK().WithPayload(r),
			fmt.Sprintf("master-%s.ign", hostID), 4)))
	})
})

var _ = Describe("V2GetHostIgnition", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		dbName     string
		clusterID  strfmt.UUID
		infraEnvID strfmt.UUID
		hostID     strfmt.UUID
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)

		// create a cluster
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = clusterID
		status := models.ClusterStatusInstalling
		c := common.Cluster{Cluster: models.Cluster{ID: &clusterID, Status: &status}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())

		// add some hosts
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("return not found when given a non-existent infra env", func() {
		otherInfraEnvID := strfmt.UUID(uuid.New().String())

		getParams := installer.V2GetHostIgnitionParams{
			InfraEnvID: otherInfraEnvID,
			HostID:     hostID,
		}
		resp := bm.V2GetHostIgnition(ctx, getParams)
		verifyApiError(resp, http.StatusNotFound)
	})

	It("return not found for a host in a different cluster", func() {
		otherClusterID := strfmt.UUID(uuid.New().String())
		c := common.Cluster{Cluster: models.Cluster{ID: &otherClusterID}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		otherHostID := strfmt.UUID(uuid.New().String())
		addHost(otherHostID, models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, otherClusterID, otherClusterID, "{}", db)

		getParams := installer.V2GetHostIgnitionParams{
			InfraEnvID: infraEnvID,
			HostID:     otherHostID,
		}
		resp := bm.V2GetHostIgnition(ctx, getParams)
		verifyApiError(resp, http.StatusNotFound)
	})

	It("return conflict when the cluster is in the incorrect status", func() {
		db.Model(&common.Cluster{}).Where("id = ?", clusterID.String()).Update("status", models.ClusterStatusInsufficient)

		getParams := installer.V2GetHostIgnitionParams{
			InfraEnvID: infraEnvID,
			HostID:     hostID,
		}
		resp := bm.V2GetHostIgnition(ctx, getParams)
		verifyApiError(resp, http.StatusConflict)
	})

	It("return server error when the download fails", func() {
		mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/master-%s.ign", clusterID, hostID)).Return(nil, int64(0), errors.Errorf("download failed")).Times(1)

		getParams := installer.V2GetHostIgnitionParams{
			InfraEnvID: infraEnvID,
			HostID:     hostID,
		}
		resp := bm.V2GetHostIgnition(ctx, getParams)
		verifyApiError(resp, http.StatusInternalServerError)
	})

	It("return the correct content", func() {
		r := ioutil.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/master-%s.ign", clusterID, hostID)).Return(r, int64(4), nil).Times(1)

		getParams := installer.V2GetHostIgnitionParams{
			InfraEnvID: infraEnvID,
			HostID:     hostID,
		}
		resp := bm.V2GetHostIgnition(ctx, getParams)
		Expect(resp).To(BeAssignableToTypeOf(&installer.V2GetHostIgnitionOK{}))
		replyPayload := resp.(*installer.V2GetHostIgnitionOK).Payload
		Expect(replyPayload.Config).Should(Equal("test"))
	})
})

var _ = Describe("UpdateHostIgnition", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		hostID    strfmt.UUID
		dbName    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		err := db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		// add some hosts
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("saves the given string to the host", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateHostIgnitionParams{
			ClusterID:          clusterID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		mockEvents.EXPECT().AddEvent(gomock.Any(), params.ClusterID, &params.HostID, models.EventSeverityInfo, fmt.Sprintf("Host %s: custom discovery ignition config was applied", params.HostID.String()), gomock.Any())
		response := bm.UpdateHostIgnition(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.UpdateHostIgnitionCreated{}))

		var updated models.Host
		err := db.First(&updated, "id = ?", hostID).Error
		Expect(err).ShouldNot(HaveOccurred())
		Expect(updated.IgnitionConfigOverrides).To(Equal(override))
	})

	It("returns not found with a non-existant cluster", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateHostIgnitionParams{
			ClusterID:          strfmt.UUID(uuid.New().String()),
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns not found with a non-existant host", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateHostIgnitionParams{
			ClusterID:          clusterID,
			HostID:             strfmt.UUID(uuid.New().String()),
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns bad request when provided invalid json", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}}}`
		params := installer.UpdateHostIgnitionParams{
			ClusterID:          clusterID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})

	It("returns bad request when provided invalid options", func() {
		// Missing the version
		override := `{"storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateHostIgnitionParams{
			ClusterID:          clusterID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})

	It("returns bad request when provided an old version", func() {
		// Wrong version
		override := `{"ignition": {"version": "3.0.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateHostIgnitionParams{
			ClusterID:          clusterID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})
})

var _ = Describe("V2UpdateHostIgnition", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		clusterID  strfmt.UUID
		infraEnvID strfmt.UUID
		hostID     strfmt.UUID
		dbName     string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		err := db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		// add some hosts
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
		addHost(strfmt.UUID(uuid.New().String()), models.HostRoleWorker, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("saves the given string to the host", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		mockEvents.EXPECT().AddEvent(gomock.Any(), params.InfraEnvID, &params.HostID, models.EventSeverityInfo, fmt.Sprintf("Host %s: custom discovery ignition config was applied", params.HostID.String()), gomock.Any())
		response := bm.V2UpdateHostIgnition(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2UpdateHostIgnitionCreated{}))

		var updated models.Host
		err := db.First(&updated, "id = ?", hostID).Error
		Expect(err).ShouldNot(HaveOccurred())
		Expect(updated.IgnitionConfigOverrides).To(Equal(override))
	})

	It("returns not found with a non-existant infra-env", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         strfmt.UUID(uuid.New().String()),
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.V2UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns not found with a non-existant host", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             strfmt.UUID(uuid.New().String()),
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.V2UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns bad request when provided invalid json", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.V2UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})

	It("returns bad request when provided invalid options", func() {
		// Missing the version
		override := `{"storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.V2UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})

	It("returns bad request when provided an old version", func() {
		// Wrong version
		override := `{"ignition": {"version": "3.0.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.V2UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})
})

var _ = Describe("BindHost", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		clusterID  strfmt.UUID
		hostID     strfmt.UUID
		infraEnvID strfmt.UUID
		dbName     string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		hostID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		err := db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{ID: infraEnvID}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		err = db.Create(&common.Host{Host: models.Host{ID: &hostID, InfraEnvID: infraEnvID}}).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("successful bind", func() {
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		mockEvents.EXPECT().AddEvent(gomock.Any(), infraEnvID, &params.HostID, models.EventSeverityInfo, gomock.Any(), gomock.Any())
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().BindHost(ctx, gomock.Any(), clusterID, gomock.Any())
		response := bm.BindHost(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.BindHostOK{}))
	})

	It("bad infraEnv", func() {
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     "12345",
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		response := bm.BindHost(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("bad cluster_id", func() {
		badClusterID := strfmt.UUID(uuid.New().String())
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &badClusterID},
		}
		response := bm.BindHost(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})

	It("already bound", func() {
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		var hostObj models.Host
		Expect(db.First(&hostObj, "id = ?", hostID).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&hostObj).Update("cluster_id", "some_cluster").Error).ShouldNot(HaveOccurred())
		response := bm.BindHost(ctx, params)
		verifyApiError(response, http.StatusConflict)
	})

	It("wrong cluster status", func() {
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		err := errors.Errorf("Cluster is in wrong state")
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(err).Times(1)
		response := bm.BindHost(ctx, params)
		verifyApiError(response, http.StatusConflict)
	})

	It("transition failed", func() {
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		err := errors.Errorf("Transition failed")
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().BindHost(ctx, gomock.Any(), clusterID, gomock.Any()).Return(err).Times(1)
		response := bm.BindHost(ctx, params)
		verifyApiError(response, http.StatusInternalServerError)
	})

	It("CPU architecture mismatch", func() {
		infraEnvID = strfmt.UUID(uuid.New().String())
		err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:              infraEnvID,
			CPUArchitecture: "arm64",
		}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		hostID = strfmt.UUID(uuid.New().String())
		err = db.Create(&common.Host{Host: models.Host{ID: &hostID, InfraEnvID: infraEnvID}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		mockEvents.EXPECT().AddEvent(gomock.Any(), infraEnvID, &params.HostID, models.EventSeverityInfo, gomock.Any(), gomock.Any())
		mockHostApi.EXPECT().BindHost(ctx, gomock.Any(), clusterID, gomock.Any())
		response := bm.BindHost(ctx, params)
		verifyApiErrorString(response, http.StatusBadRequest, "doesn't match")
	})

})

var _ = Describe("UnbindHost", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		clusterID  strfmt.UUID
		hostID     strfmt.UUID
		infraEnvID strfmt.UUID
		dbName     string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		hostID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{ID: infraEnvID}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		err = db.Create(&common.Host{Host: models.Host{ID: &hostID, InfraEnvID: infraEnvID, ClusterID: &clusterID}}).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("successful unbind", func() {
		params := installer.UnbindHostParams{
			HostID:     hostID,
			InfraEnvID: infraEnvID,
		}
		mockEvents.EXPECT().AddEvent(gomock.Any(), infraEnvID, &params.HostID, models.EventSeverityInfo, gomock.Any(), gomock.Any())
		mockHostApi.EXPECT().UnbindHost(ctx, gomock.Any(), gomock.Any())
		response := bm.UnbindHost(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.UnbindHostOK{}))
	})

	It("bad infraEnv", func() {
		params := installer.UnbindHostParams{
			HostID:     hostID,
			InfraEnvID: "12345",
		}
		response := bm.UnbindHost(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("already unbound", func() {
		params := installer.UnbindHostParams{
			HostID:     hostID,
			InfraEnvID: infraEnvID,
		}
		var hostObj models.Host
		Expect(db.First(&hostObj, "id = ?", hostID).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&hostObj).Update("cluster_id", nil).Error).ShouldNot(HaveOccurred())
		response := bm.UnbindHost(ctx, params)
		verifyApiError(response, http.StatusConflict)
	})

	It("transition failed", func() {
		params := installer.UnbindHostParams{
			HostID:     hostID,
			InfraEnvID: infraEnvID,
		}
		err := errors.Errorf("Transition failed")
		mockHostApi.EXPECT().UnbindHost(ctx, gomock.Any(), gomock.Any()).Return(err).Times(1)
		response := bm.UnbindHost(ctx, params)
		verifyApiError(response, http.StatusInternalServerError)
	})

})

var _ = Describe("UpdateHostInstallerArgs", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		ctx       = context.Background()
		clusterID strfmt.UUID
		hostID    strfmt.UUID
		dbName    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		err := db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		// add a host
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, clusterID, clusterID, "{}", db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("saves the given array to the host", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n"}
		params := installer.UpdateHostInstallerArgsParams{
			ClusterID:           clusterID,
			HostID:              hostID,
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}
		mockEvents.EXPECT().AddEvent(gomock.Any(), params.ClusterID, &params.HostID, models.EventSeverityInfo, fmt.Sprintf("Host %s: custom installer arguments were applied", params.HostID.String()), gomock.Any())
		response := bm.UpdateHostInstallerArgs(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.UpdateHostInstallerArgsCreated{}))

		var updated models.Host
		err := db.First(&updated, "id = ?", hostID).Error
		Expect(err).ShouldNot(HaveOccurred())

		var newArgs []string
		err = json.Unmarshal([]byte(updated.InstallerArgs), &newArgs)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(newArgs).To(Equal(args))
	})

	It("returns not found with a non-existant cluster", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n"}
		params := installer.UpdateHostInstallerArgsParams{
			ClusterID:           strfmt.UUID(uuid.New().String()),
			HostID:              hostID,
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}
		response := bm.UpdateHostInstallerArgs(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns not found with a non-existant host", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n"}
		params := installer.UpdateHostInstallerArgsParams{
			ClusterID:           clusterID,
			HostID:              strfmt.UUID(uuid.New().String()),
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}
		response := bm.UpdateHostInstallerArgs(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns bad request when provided an invalid flag", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-a"}
		params := installer.UpdateHostInstallerArgsParams{
			ClusterID:           clusterID,
			HostID:              hostID,
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}
		response := bm.UpdateHostInstallerArgs(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})
})

var _ = Describe("V2UpdateHostInstallerArgs", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		clusterID  strfmt.UUID
		infraEnvID strfmt.UUID
		hostID     strfmt.UUID
		dbName     string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		err := db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		// add a host
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("saves the given array to the host", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n"}
		params := installer.V2UpdateHostInstallerArgsParams{
			InfraEnvID:          infraEnvID,
			HostID:              hostID,
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}
		mockEvents.EXPECT().AddEvent(gomock.Any(), params.InfraEnvID, &params.HostID, models.EventSeverityInfo, fmt.Sprintf("Host %s: custom installer arguments were applied", params.HostID.String()), gomock.Any())
		response := bm.V2UpdateHostInstallerArgs(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2UpdateHostInstallerArgsCreated{}))

		var updated models.Host
		err := db.First(&updated, "id = ?", hostID).Error
		Expect(err).ShouldNot(HaveOccurred())

		var newArgs []string
		err = json.Unmarshal([]byte(updated.InstallerArgs), &newArgs)
		Expect(err).ShouldNot(HaveOccurred())

		Expect(newArgs).To(Equal(args))
	})

	It("returns not found with a non-existant infra-env", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n"}
		params := installer.V2UpdateHostInstallerArgsParams{
			InfraEnvID:          strfmt.UUID(uuid.New().String()),
			HostID:              hostID,
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}
		response := bm.V2UpdateHostInstallerArgs(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns not found with a non-existant host", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n"}
		params := installer.V2UpdateHostInstallerArgsParams{
			InfraEnvID:          infraEnvID,
			HostID:              strfmt.UUID(uuid.New().String()),
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}
		response := bm.V2UpdateHostInstallerArgs(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns bad request when provided an invalid flag", func() {
		args := []string{"--append-karg", "nameserver=8.8.8.8", "-a"}
		params := installer.V2UpdateHostInstallerArgsParams{
			InfraEnvID:          infraEnvID,
			HostID:              hostID,
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}
		response := bm.V2UpdateHostInstallerArgs(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})
})

var _ = Describe("UpdateHostApproved", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		clusterID  strfmt.UUID
		infraEnvID strfmt.UUID
		hostID     strfmt.UUID
		dbName     string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
		err := db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("get default approved value", func() {
		h, err := bm.GetCommonHostInternal(ctx, infraEnvID.String(), hostID.String())
		Expect(err).ShouldNot(HaveOccurred())
		Expect(h.Approved).To(Equal(false))
	})

	It("update approved value", func() {
		mockEvents.EXPECT().AddEvent(gomock.Any(), infraEnvID, &hostID, models.EventSeverityInfo,
			fmt.Sprintf("Host %s: updated approved to %t", hostID.String(), true),
			gomock.Any()).Times(1)
		err := bm.UpdateHostApprovedInternal(ctx, infraEnvID.String(), hostID.String(), true)
		Expect(err).ShouldNot(HaveOccurred())
		h, err := bm.GetCommonHostInternal(ctx, infraEnvID.String(), hostID.String())
		Expect(err).ShouldNot(HaveOccurred())
		Expect(h.Approved).To(Equal(true))
	})

})

var _ = Describe("Calculate host networks", func() {
	var (
		cfg       *Config
		inventory *bareMetalInventory
		db        *gorm.DB
		clusterID strfmt.UUID
		hostID    strfmt.UUID
		dbName    string
	)
	BeforeEach(func() {
		cfg = &Config{}
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		err := db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		// add a host
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusInsufficient, "kind", clusterID, clusterID,
			getInventoryStrWithIPv6("host", "bios", []string{
				"1.1.1.1/24",
				"1.1.1.2/24",
			}, []string{
				"fe80::1/64",
				"fe80::2/64",
			}), db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
	It("Duplicates and disabled IPv6", func() {
		cfg.IPv6Support = false
		inventory = createInventory(db, *cfg)
		cluster, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ToNot(HaveOccurred())
		networks := inventory.calculateHostNetworks(logrus.New(), cluster)
		Expect(len(networks)).To(Equal(1))
		Expect(networks[0].Cidr).To(Equal("1.1.1.0/24"))
		Expect(len(networks[0].HostIds)).To(Equal(1))
		Expect(networks[0].HostIds[0]).To(Equal(hostID))
	})
	It("Duplicates and enabled IPv6", func() {
		cfg.IPv6Support = true
		inventory = createInventory(db, *cfg)
		cluster, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ToNot(HaveOccurred())
		networks := inventory.calculateHostNetworks(logrus.New(), cluster)
		Expect(len(networks)).To(Equal(2))
		sort.Slice(networks, func(i, j int) bool {
			return networks[i].Cidr < networks[j].Cidr
		})
		Expect(networks[0].Cidr).To(Equal("1.1.1.0/24"))
		Expect(len(networks[0].HostIds)).To(Equal(1))
		Expect(networks[0].HostIds[0]).To(Equal(hostID))
		Expect(networks[1].Cidr).To(Equal("fe80::/64"))
		Expect(len(networks[1].HostIds)).To(Equal(1))
		Expect(networks[1].HostIds[0]).To(Equal(hostID))

	})
})

var _ = Describe("Get Cluster by Kube Key", func() {
	var (
		db     *gorm.DB
		dbName string
		bm     *bareMetalInventory
		cfg    Config
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("get cluster by kube key success", func() {
		mockClusterApi.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(&common.Cluster{}, nil).Times(1)
		cluster, err := bm.GetClusterByKubeKey(types.NamespacedName{Name: "name", Namespace: "namespace"})
		Expect(err).ShouldNot(HaveOccurred())
		Expect(cluster).Should(Equal(&common.Cluster{}))
	})

	It("get cluster by kube key failure", func() {
		mockClusterApi.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound).Times(1)
		cluster, err := bm.GetClusterByKubeKey(types.NamespacedName{Name: "name", Namespace: "namespace"})
		Expect(err).Should(HaveOccurred())
		Expect(errors.Is(err, gorm.ErrRecordNotFound)).Should(Equal(true))
		Expect(cluster).Should(BeNil())
	})
})

func createInventory(db *gorm.DB, cfg Config) *bareMetalInventory {
	ctrl = gomock.NewController(GinkgoT())

	mockClusterApi = cluster.NewMockAPI(ctrl)
	mockHostApi = host.NewMockAPI(ctrl)
	mockGenerator = generator.NewMockISOInstallConfigGenerator(ctrl)
	mockEvents = events.NewMockHandler(ctrl)
	mockS3Client = s3wrapper.NewMockAPI(ctrl)
	mockMetric = metrics.NewMockAPI(ctrl)
	mockUsage = usage.NewMockAPI(ctrl)
	mockK8sClient = k8sclient.NewMockK8SClient(ctrl)
	mockAccountsMgmt = ocm.NewMockOCMAccountsMgmt(ctrl)
	ocmClient := &ocm.Client{AccountsMgmt: mockAccountsMgmt}
	mockSecretValidator = validations.NewMockPullSecretValidator(ctrl)
	mockVersions = versions.NewMockHandler(ctrl)
	mockIsoEditorFactory = isoeditor.NewMockFactory(ctrl)
	mockCRDUtils = NewMockCRDUtils(ctrl)
	mockOperatorManager = operators.NewMockAPI(ctrl)
	mockIgnitionBuilder = ignition.NewMockIgnitionBuilder(ctrl)
	mockInstallConfigBuilder = installcfg.NewMockInstallConfigBuilder(ctrl)
	mockHwValidator = hardware.NewMockValidator(ctrl)
	mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
	dnsApi := dns.NewDNSHandler(cfg.BaseDNSDomains, common.GetTestLog())
	gcConfig := garbagecollector.Config{DeregisterInactiveAfter: 20 * 24 * time.Hour}
	return NewBareMetalInventory(db, common.GetTestLog(), mockHostApi, mockClusterApi, cfg,
		mockGenerator, mockEvents, mockS3Client, mockMetric, mockUsage, mockOperatorManager,
		getTestAuthHandler(), mockK8sClient, ocmClient, nil, mockSecretValidator, mockVersions,
		mockIsoEditorFactory, mockCRDUtils, mockIgnitionBuilder, mockHwValidator, dnsApi, mockInstallConfigBuilder, mockStaticNetworkConfig,
		gcConfig)
}

var _ = Describe("IPv6 support disabled", func() {

	const errorMsg = "IPv6 is not supported in this setup"

	var (
		bm  *bareMetalInventory
		cfg Config
		db  *gorm.DB
		ctx = context.Background()
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		Expect(cfg.IPv6Support).Should(BeTrue())
		cfg.IPv6Support = false
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Register cluster", func() {

		var params installer.RegisterClusterParams

		BeforeEach(func() {
			params = installer.RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{},
			}
		})

		Context("IPV6 cluster", func() {

			It("IPv6 cluster network rejected", func() {
				params.NewClusterParams.ClusterNetworks = []*models.ClusterNetwork{
					{Cidr: "2001:db8::/64"},
				}
				reply := bm.RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})

			It("IPv6 service network rejected", func() {
				params.NewClusterParams.ServiceNetworks = []*models.ServiceNetwork{
					{Cidr: "2001:db8::/64"},
				}
				reply := bm.RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})

			It("IPv6 ingress VIP rejected", func() {
				params.NewClusterParams.IngressVip = "2001:db8::1"
				reply := bm.RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})
		})
	})

	Context("Update cluster", func() {

		var params installer.UpdateClusterParams

		BeforeEach(func() {
			mockUsageReports()
			params = installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{},
			}
		})

		It("IPv6 cluster network rejected", func() {
			params.ClusterUpdateParams.ClusterNetworks = []*models.ClusterNetwork{
				{Cidr: "2001:db8::/64"},
			}
			reply := bm.UpdateCluster(ctx, params)
			verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
		})

		It("IPv6 service network rejected", func() {
			params.ClusterUpdateParams.ServiceNetworks = []*models.ServiceNetwork{
				{Cidr: "2001:db8::/64"},
			}
			reply := bm.UpdateCluster(ctx, params)
			verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
		})

		It("IPv6 machine network rejected", func() {
			params.ClusterUpdateParams.MachineNetworks = []*models.MachineNetwork{
				{Cidr: "2001:db8::/64"},
			}
			reply := bm.UpdateCluster(ctx, params)
			verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
		})

		It("IPv6 API VIP rejected", func() {
			params.ClusterUpdateParams.APIVip = swag.String("2003:db8::a")
			reply := bm.UpdateCluster(ctx, params)
			verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
		})

		It("IPv6 ingress VIP rejected", func() {
			params.ClusterUpdateParams.IngressVip = swag.String("2002:db8::1")
			reply := bm.UpdateCluster(ctx, params)
			verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
		})
	})
})

var _ = Describe("GetCredentials", func() {

	var (
		ctx    = context.Background()
		cfg    = Config{}
		bm     *bareMetalInventory
		db     *gorm.DB
		dbName string
		c      common.Cluster
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB(dbName)
		bm = createInventory(db, cfg)

		clusterID := strfmt.UUID(uuid.New().String())
		c = common.Cluster{
			Cluster: models.Cluster{
				ID: &clusterID,
			},
		}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("Console operator available", func() {

		mockClusterApi.EXPECT().IsOperatorAvailable(gomock.Any(), operators.OperatorConsole.Name).Return(true)
		objectName := fmt.Sprintf("%s/%s", *c.ID, "kubeadmin-password")
		mockS3Client.EXPECT().Download(ctx, objectName).Return(ioutil.NopCloser(strings.NewReader("my_password")), int64(0), nil)

		reply := bm.GetCredentials(ctx, installer.GetCredentialsParams{ClusterID: *c.ID})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewGetCredentialsOK())))
	})

	It("Console operator not available", func() {

		mockClusterApi.EXPECT().IsOperatorAvailable(gomock.Any(), operators.OperatorConsole.Name).Return(false)

		reply := bm.GetCredentials(ctx, installer.GetCredentialsParams{ClusterID: *c.ID})
		verifyApiError(reply, http.StatusConflict)
	})
})

var _ = Describe("AddOpenshiftVersion", func() {
	var (
		cfg          = Config{}
		ctx          = context.Background()
		bm           *bareMetalInventory
		db           *gorm.DB
		dbName       string
		pullSecret   = "test_pull_secret"
		releaseImage = "releaseImage"
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB(dbName)
		bm = createInventory(db, cfg)
	})

	It("successfully added version", func() {
		mockVersions.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.Version, nil).Times(1)

		version, err := bm.AddOpenshiftVersion(ctx, releaseImage, pullSecret)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(version).Should(Equal(common.TestDefaultConfig.Version))
	})

	It("failed to added version", func() {
		mockVersions.EXPECT().AddOpenshiftVersion(gomock.Any(), gomock.Any()).Return(nil, errors.New("failed")).Times(1)

		_, err := bm.AddOpenshiftVersion(ctx, releaseImage, pullSecret)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("Platform tests", func() {

	var (
		cfg                Config
		ctx                = context.Background()
		bm                 *bareMetalInventory
		db                 *gorm.DB
		dbName             string
		registerParams     *installer.RegisterClusterParams
		getVSpherePlatform = func() *models.Platform {
			dummy := "dummy"
			dummyPassword := strfmt.Password(dummy)

			return &models.Platform{
				Type: models.PlatformTypeVsphere,
				Vsphere: &models.VspherePlatform{
					Cluster:          &dummy,
					Datacenter:       &dummy,
					DefaultDatastore: &dummy,
					Folder:           &dummy,
					Network:          &dummy,
					Password:         &dummyPassword,
					Username:         &dummy,
					VCenter:          &dummy,
				},
			}
		}
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB(dbName)
		bm = createInventory(db, cfg)
		mockOperators := operators.NewMockAPI(ctrl)
		bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, mockEvents, nil, nil, nil, nil, mockOperators, nil, nil, nil)
		bm.ocmClient = nil

		registerParams = &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("cluster"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:       swag.String(`{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"`),
			},
		}

		mockClusterRegisterSuccess(bm, true)
		mockUsageReports()
		mockOperators.EXPECT().ValidateCluster(ctx, gomock.Any()).AnyTimes()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("Register cluster", func() {

		It("default platform", func() {
			reply := bm.RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			cluster := reply.(*installer.RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(cluster.Platform.Type).Should(BeEquivalentTo(models.PlatformTypeBaremetal))
			Expect(cluster.Platform.Vsphere.VCenter).Should(BeNil())
		})

		It("vsphere platform", func() {
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type:    models.PlatformTypeVsphere,
				Vsphere: &models.VspherePlatform{},
			}

			reply := bm.RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			cluster := reply.(*installer.RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(cluster.Platform.Type).Should(BeEquivalentTo(models.PlatformTypeVsphere))
			Expect(cluster.Platform.Vsphere).ShouldNot(BeNil())
		})

		It("vsphere platform with credentials", func() {
			registerParams.NewClusterParams.Platform = getVSpherePlatform()
			reply := bm.RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			cluster := reply.(*installer.RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(cluster.Platform.Type).Should(BeEquivalentTo(models.PlatformTypeVsphere))
			Expect(cluster.Platform.Vsphere).ShouldNot(BeNil())
		})
	})

	Context("Update cluster", func() {
		var (
			c                   *common.Cluster
			updateClusterParams = installer.UpdateClusterParams{
				ClusterUpdateParams: &models.ClusterUpdateParams{},
			}
			toVSphere = func() {
				updateClusterParams.ClusterID = *c.ID
				updateClusterParams.ClusterUpdateParams = &models.ClusterUpdateParams{
					Platform: getVSpherePlatform(),
				}

				reply := bm.UpdateCluster(ctx, updateClusterParams)
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
				var err error
				c, err = bm.getCluster(ctx, c.ID.String())
				Expect(err).ToNot(HaveOccurred())
				Expect(c.Platform).ShouldNot(BeNil())
				Expect(c.Platform.Type).Should(BeEquivalentTo(models.PlatformTypeVsphere))
				Expect(c.Platform.Vsphere).ShouldNot(BeNil())
				Expect(c.Platform.Vsphere.Network).Should(BeEquivalentTo(updateClusterParams.ClusterUpdateParams.Platform.Vsphere.Network))
			}
		)

		BeforeEach(func() {
			reply := bm.RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterClusterCreated()))
			actual := reply.(*installer.RegisterClusterCreated)
			var err error
			c, err = bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())
			mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName))).AnyTimes()
			Expect(c.Platform).ShouldNot(BeNil())
			Expect(c.Platform.Type).Should(BeEquivalentTo(models.PlatformTypeBaremetal))
			Expect(c.Platform.Vsphere.Username).Should(BeNil())
		})

		It("vsphere platform creation", func() {
			toVSphere()
		})

		It("switch to bare-metal platform", func() {
			toVSphere()
			reply := bm.UpdateCluster(ctx, installer.UpdateClusterParams{
				ClusterID: *c.ID,
				ClusterUpdateParams: &models.ClusterUpdateParams{
					Platform: &models.Platform{
						Type:    models.PlatformTypeBaremetal,
						Vsphere: nil,
					},
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateClusterCreated()))
			var err error
			c, err = bm.getCluster(ctx, c.ID.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(c.Platform).ShouldNot(BeNil())
			Expect(c.Platform.Type).Should(BeEquivalentTo(models.PlatformTypeBaremetal))
			Expect(c.Platform.Vsphere.Username).Should(BeNil())
		})
	})
})

var _ = Describe("DownloadClusterFiles", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		clusterID = strfmt.UUID(uuid.New().String())
		ctx       = context.Background()
		db        *gorm.DB
		dbName    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB(dbName)
		bm = createInventory(db, cfg)

		cluster := common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			PullSecretSet:    true,
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusInstalled),
		}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		Expect(common.CreateInfraEnvForCluster(db, &cluster, models.ImageTypeFullIso)).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("allows downloading cluster owner files when not using auth", func() {
		for _, fileName := range cluster.ClusterOwnerFileNames {
			By(fmt.Sprintf("downloading %s", fileName))

			params := installer.DownloadClusterFilesParams{
				ClusterID: clusterID,
				FileName:  fileName,
			}

			r := io.NopCloser(bytes.NewReader([]byte("testfile")))
			expected := filemiddleware.NewResponder(installer.NewDownloadClusterFilesOK().WithPayload(r), fileName, int64(8))
			mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/%s", clusterID, fileName)).Return(r, int64(8), nil)
			resp := bm.DownloadClusterFiles(ctx, params)
			Expect(resp).Should(Equal(expected))
		}
	})
})

var _ = Describe("[V2] V2DownloadClusterCredentials", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		clusterID = strfmt.UUID(uuid.New().String())
		ctx       = context.Background()
		db        *gorm.DB
		dbName    string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB(dbName)
		bm = createInventory(db, cfg)

		cluster := common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			PullSecretSet:    true,
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusInstalled),
		}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		Expect(common.CreateInfraEnvForCluster(db, &cluster, models.ImageTypeFullIso)).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("v2 allows downloading cluster credentials files", func() {
		for _, fileName := range cluster.ClusterOwnerFileNames {
			By(fmt.Sprintf("downloading %s", fileName))

			params := installer.V2DownloadClusterCredentialsParams{
				ClusterID: clusterID,
				FileName:  fileName,
			}

			r := io.NopCloser(bytes.NewReader([]byte("testfile")))
			expected := filemiddleware.NewResponder(installer.NewV2DownloadClusterCredentialsOK().WithPayload(r), fileName, int64(8))
			mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/%s", clusterID, fileName)).Return(r, int64(8), nil)
			resp := bm.V2DownloadClusterCredentials(ctx, params)
			Expect(resp).Should(Equal(expected))
		}
	})
})

func validateNetworkConfiguration(cluster *models.Cluster, clusterNetworks *[]*models.ClusterNetwork,
	serviceNetworks *[]*models.ServiceNetwork, machineNetworks *[]*models.MachineNetwork) {
	if clusterNetworks != nil {
		Expect(cluster.ClusterNetworks).To(HaveLen(len(*clusterNetworks)))
		for index := range *clusterNetworks {
			Expect(cluster.ClusterNetworks[index].ClusterID).To(Equal(*cluster.ID))
			Expect(cluster.ClusterNetworks[index].Cidr).To(Equal((*clusterNetworks)[index].Cidr))
			Expect(cluster.ClusterNetworks[index].HostPrefix).To(Equal((*clusterNetworks)[index].HostPrefix))
		}

		// TODO MGMT-7365: Deprecate single network
		if len(*clusterNetworks) > 0 {
			Expect(cluster.ClusterNetworkCidr).To(Equal(string((*clusterNetworks)[0].Cidr)))
			Expect(cluster.ClusterNetworkHostPrefix).To(Equal((*clusterNetworks)[0].HostPrefix))
		} else {
			Expect(cluster.ClusterNetworkCidr).To(Equal(""))
			Expect(cluster.ClusterNetworkHostPrefix).To(Equal(""))
		}
	}
	if serviceNetworks != nil {
		Expect(cluster.ServiceNetworks).To(HaveLen(len(*serviceNetworks)))
		for index := range *serviceNetworks {
			Expect(cluster.ServiceNetworks[index].ClusterID).To(Equal(*cluster.ID))
			Expect(cluster.ServiceNetworks[index].Cidr).To(Equal((*serviceNetworks)[index].Cidr))
		}

		// TODO MGMT-7365: Deprecate single network
		if len(*serviceNetworks) > 0 {
			Expect(cluster.ServiceNetworkCidr).To(Equal(string((*serviceNetworks)[0].Cidr)))
		} else {
			Expect(cluster.ServiceNetworkCidr).To(Equal(""))
		}
	}
	if machineNetworks != nil {
		Expect(cluster.MachineNetworks).To(HaveLen(len(*machineNetworks)))
		for index := range *machineNetworks {
			Expect(cluster.MachineNetworks[index].ClusterID).To(Equal(*cluster.ID))
			Expect(cluster.MachineNetworks[index].Cidr).To(Equal((*machineNetworks)[index].Cidr))
		}

		// TODO MGMT-7365: Deprecate single network
		if len(*machineNetworks) > 0 {
			Expect(cluster.MachineNetworkCidr).To(Equal(string((*machineNetworks)[0].Cidr)))
		} else {
			Expect(cluster.MachineNetworkCidr).To(Equal(""))
		}
	}
}
