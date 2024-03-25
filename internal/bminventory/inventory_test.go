package bminventory

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cavaliercoder/go-cpio"
	ign_3_1 "github.com/coreos/ignition/v2/config/v3_1"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	gomega_format "github.com/onsi/gomega/format"
	amgmtv1 "github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/cluster/validations"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	commontesting "github.com/openshift/assisted-service/internal/common/testing"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/dns"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/internal/garbagecollector"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/hardware"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/ignition"
	"github.com/openshift/assisted-service/internal/infraenv"
	"github.com/openshift/assisted-service/internal/installcfg"
	installcfg_builder "github.com/openshift/assisted-service/internal/installcfg/builder"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/internal/provider/vsphere"
	"github.com/openshift/assisted-service/internal/stream"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	"github.com/openshift/assisted-service/pkg/generator"
	"github.com/openshift/assisted-service/pkg/k8sclient"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/openshift/assisted-service/pkg/ocm"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/openshift/assisted-service/restapi"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/test"
	"gorm.io/gorm"
	"k8s.io/apimachinery/pkg/types"
)

const testCert string = `-----BEGIN CERTIFICATE-----
MIIFPjCCAyagAwIBAgIUBCE1YX2zJ0R/3NURq2XQaciEuVQwDQYJKoZIhvcNAQEL
BQAwFjEUMBIGA1UEAwwLZXhhbXBsZS5jb20wHhcNMjIxMTI3MjM0MjAyWhcNMzIx
MTI0MjM0MjAyWjAWMRQwEgYDVQQDDAtleGFtcGxlLmNvbTCCAiIwDQYJKoZIhvcN
AQEBBQADggIPADCCAgoCggIBAKY589W+Xifs9SfxofBI1r1/NKsMUVPvg3ZtDIPQ
EeNKf5OgtSOVFcoEmkS7ZWNTIu4Kd1WBf/rG+F5lm/aTTa3j720Q+fS+gsveGQPz
7taUpU/TjHHzoCqjjhaYMr4gIJ3jkpTXUWG5/vka/oNykSxkGCuZw1gyXHNujA8L
DJYY8VNUHPl5MmXGaT++6yEN4WdB2f7R/MmEaH6KnGo/LjhMeiVmDsIxHZ/xW9OR
izPklnUi78NfZJSxiknoV6CnQShNijLEq6nQowYQ1lQuNWs6sTM28I0BYWk+gDUz
NOWkVqSHFRMzGmpqYJs7JQiv0g33VN/92dwdP/kZc9sAYRqDaI6hplOZrD/OEsbG
lmN90x/o42wotJeBDN1hHlJ1JeRjR1Vk8XUfOmaTuOPzooKIM0h9K6Ah6u3lRQtE
n68yxn0sGD8yw6EydS5FD9zzvA6rgXBSsvpMFjk/N/FmnIzD4YinLEiflfub1O0M
9thEOX9IaOh00U2eGsRa/MOJcCZ5TUOgxVlv15ATUPHo1MW8QkmYOVx4BoM/Bw0J
0HibIU8VUw2AV1tupRdQma7Qg5gyjdx2doth78IG5+LkX95fSyz60Kf9l1xBQHNA
kVyzkXlx8jmdm53CeFvHVOrVrLuA2Dk+t21TNL1uFGgQ0iLxItCf1O6F6B78QqhI
YLOdAgMBAAGjgYMwgYAwHQYDVR0OBBYEFE6DFh3+wGzA8dOYBTL9Z0CyxLJ/MB8G
A1UdIwQYMBaAFE6DFh3+wGzA8dOYBTL9Z0CyxLJ/MA8GA1UdEwEB/wQFMAMBAf8w
LQYDVR0RBCYwJIILZXhhbXBsZS5jb22CD3d3dy5leGFtcGxlLm5ldIcECgAAATAN
BgkqhkiG9w0BAQsFAAOCAgEAoj+elkYHrek6DoqOvEFZZtRp6bPvof61/VJ3kP7x
HZXp5yVxvGOHt61YRziGLpsFbuiDczk0V61ZdozHUOtZ0sWB4VeyO1pAjfd/JwDI
CK6olfkSO78WFQfdG4lNoSM9dQJyEIEZ1sbvuUL3RHDBd9oEKue+vsstlM9ahdoq
fpTTFq4ENGCAIDvaqKIlpjKsAMrsTO47CKPVh2HUpugfVGKeBRsW1KAXFoC2INS5
7BY3h60jFFW6bz0v+FnzW96Mt2VNW+i/REX6fBaR4m/QfG81rA2EEmhxCGrany+N
6DUkwiJxcqBMH9jA2yVnF7BgwG2C3geBqXTTlvVQJD8GOktkvgLjlHcYqO1pI7B3
wP9F9ZF+w39jXwGMGBg8+/aQz1RjP2bOb18n7d0bc4/pbbkVAmE4sq4qMneFZAVE
uj9S2Jna3ut08ZP05Ych5vCGX4VJ8gNNgrJju2PJVBl8NNyDfHKeHfWSOR9uOMjT
vqK6iRD9xqu/oLJyrlAuOL8ZxRpeqjxF/g8NYYV/fvv8apaX58ua9qYAFQVGf590
mmjOozzn9VBqKenVmfwzen5v78CBSgS4Hd72Qp42rLCNgqI8gyQa2qZzaNjLP/wI
pBpFC21fkybGYPkislPQ3EI69ZGRafWDBjlFFTS3YkDM98tqTZD+JG4STY+ivHhK
gmY=
-----END CERTIFICATE-----`

const testCert2 string = `-----BEGIN CERTIFICATE-----
MIIFPjCCAyagAwIBAgIUV3ZmDsSwF6/E2CPhFChz3w14OLMwDQYJKoZIhvcNAQEL
BQAwFjEUMBIGA1UEAwwLZXhhbXBsZS5jb20wHhcNMjIxMTI3MjM0MjMwWhcNMzIx
MTI0MjM0MjMwWjAWMRQwEgYDVQQDDAtleGFtcGxlLmNvbTCCAiIwDQYJKoZIhvcN
AQEBBQADggIPADCCAgoCggIBALxURtV3Wd8NEFIplXSZpIdx5I0jFU8thmb2vZON
oNxr31OsHYqkA07RpGSmyn+hv03OI9g4AzuMGs48XoPxZGtWUr0wany1LDDW8t/J
PytYeZyXAJM0zl6/AlzSzYRPk22LykdzVBQosUeRP42a2xWEdDRkJqxxBHQ0eLiC
9g32w57YomhbgCR2OnUxzVmMuQmk987WG7u3/ssSBPEuIebOoX+6G3uLaw/Ka6zQ
XGzRgFq3mskPVfw3exQ46WZfgu6PtG5zxKmty75fNPPwdyw+lwm3u8pH5jpJYvOZ
RHbk7+nxWxLxe5r3FzaNeWskb24J9x53nQzwfcF0MtuRvMycO1i/3e5Y4TanEmmu
GbUOKlJxyaFQaVa2udWAxZ8w1W5u4aKrBprXEAXXDghXbxrgRry2zPO1vqZ/aLH8
YKnHLifjdsNMxrA3nsKAViY0erwYmTF+c551gxkW7vZCtJStzDcMVM16U76jato7
fNb64VUtviVCWeHvh7aTpxENPCh6T8eGh3K4HUESTNpBggs3TXhF1yEcS+aKVJ3z
6CZcke1ph/vpMt/684xx8tICp2KMWbwk3nIBaMw84hrVZyKFgpW/gZOE+ktV91zw
LF1oFn+2F8PwGSphBwhBE0uoyFRNmUXiPsHUyEh7kF7EU5gb1sxTzM5sWCNm6nIS
QRlXAgMBAAGjgYMwgYAwHQYDVR0OBBYEFHuAjvmIDJX76uWtnfirReeBU+f2MB8G
A1UdIwQYMBaAFHuAjvmIDJX76uWtnfirReeBU+f2MA8GA1UdEwEB/wQFMAMBAf8w
LQYDVR0RBCYwJIILZXhhbXBsZS5jb22CD3d3dy5leGFtcGxlLm5ldIcECgAAATAN
BgkqhkiG9w0BAQsFAAOCAgEACn2BTzH89jDBHAy1rREJY8nYhH8GQxsPQn3MZAjA
OiAQRSqqaduYdM+Q6X3V/A8n2vtS1vjs2msQwg6uNN/yNNgdo+Nobj74FmF+kwaf
hodvMJ7z+MyeuxONYL/rbolc8N031nPWim8HTQsS/hxiiwqMHzgz6hQou1OFPwTJ
QdhsfXgqbNRiMkF/UxLfIDEP8J5VAEzVJlyrGUrUOuaMU6TZ+tx1VbNQm3Xum5GW
UgtmE36wWp/M1VeNSsm3GOQRlyWFGmE0sgA95IxLRMgL1mpd8IS3iU6TVZLx0+sA
Bly38R1z8Vcwr1vOurQ8g76Epdet2ZkQNQBwvgeVvnCsoy4CQf2AvDzKgEeTdXMM
WdO6UnG2+PgJ6YQHyfCB34mjPqrJul/0YwWo/p+PxSHRKdJZJTKzZPi1sPuxA2iO
YiJIS94ZRlkPxrD4pYdGiXPigC+0motT6cYxQ8SKTVOs7aEax/xQngrcQPLNXTgn
LtoT4hLCJpP7PTLgL91Dvu/dUMR4SEUNojUBul67D5fIjD0sZvJFZGd78apl/gdf
PxkCHm4A07Zwl/x+89Ia73mk+y8O2u+CGh7oDrO565ADxKj6/UhxhVKmV9DG1ono
AjGUGkvXVVvurf5CwGxpwT/G5UXpSK+314eMVxz5s3yDb2J2J2rvIk6ROPxBK0ws
Sj8=
-----END CERTIFICATE-----`

var (
	ctrl                              *gomock.Controller
	mockStream                        *stream.MockNotifier
	mockClusterApi                    *cluster.MockAPI
	mockHostApi                       *host.MockAPI
	mockInfraEnvApi                   *infraenv.MockAPI
	mockEvents                        *eventsapi.MockHandler
	mockS3Client                      *s3wrapper.MockAPI
	mockSecretValidator               *validations.MockPullSecretValidator
	mockGenerator                     *generator.MockISOInstallConfigGenerator
	mockVersions                      *versions.MockHandler
	mockOSImages                      *versions.MockOSImages
	mockMetric                        *metrics.MockAPI
	mockUsage                         *usage.MockAPI
	mockK8sClient                     *k8sclient.MockK8SClient
	mockCRDUtils                      *MockCRDUtils
	mockAccountsMgmt                  *ocm.MockOCMAccountsMgmt
	mockOperatorManager               *operators.MockAPI
	mockHwValidator                   *hardware.MockValidator
	mockIgnitionBuilder               *ignition.MockIgnitionBuilder
	mockInstallConfigBuilder          *installcfg_builder.MockInstallConfigBuilder
	mockStaticNetworkConfig           *staticnetworkconfig.MockStaticNetworkConfig
	mockProviderRegistry              *registry.MockProviderRegistry
	mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
	secondDayWorkerIgnition           = []byte(`{
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
	imageServicePath    = "/api/image-services"
	imageServiceHost    = "image-service.example.com:8080"
	imageServiceBaseURL = fmt.Sprintf("https://%s%s", imageServiceHost, imageServicePath)
	fakePullSecret      = `{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"` // #nosec
	serviceBaseURL      = "https://assisted.example.com:6008"
)

type clusterIdMatcher struct {
	Cluster *common.Cluster
}

func createClusterIdMatcher(cluster *common.Cluster) gomock.Matcher {
	return clusterIdMatcher{Cluster: cluster}
}

func (e clusterIdMatcher) Matches(x interface{}) bool {
	cluster, ok := x.(*common.Cluster)
	if !ok {
		return false
	}
	equal := e.Cluster.ID.String() == cluster.ID.String()
	return equal
}

func (e clusterIdMatcher) String() string {
	return fmt.Sprintf("Cluster with ID %s", e.Cluster.ID)
}

func toMac(macStr string) *strfmt.MAC {
	mac := strfmt.MAC(macStr)
	return &mac
}

func mockClusterRegisterSteps() {
	mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
	mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
	mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
	mockProviderRegistry.EXPECT().SetPlatformUsages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
}

func mockClusterRegisterSuccess(withEvents bool) {
	mockClusterRegisterSteps()
	mockMetric.EXPECT().ClusterRegistered().Times(1)

	if withEvents {
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterRegistrationSucceededEventName))).Times(1)
	}
}

func mockClusterRegisterSuccessWithVersion(cpuArchitecture, openshiftVersion string) {
	releaseImage := &models.ReleaseImage{
		CPUArchitecture:  &cpuArchitecture,
		OpenshiftVersion: &openshiftVersion,
		URL:              swag.String(fmt.Sprintf("quay.io/openshift-release-dev/ocp-release:%s-%s", openshiftVersion, cpuArchitecture)),
		Version:          swag.String(openshiftVersion),
		SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
	}
	mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(releaseImage, nil).Times(1)
	mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
	mockProviderRegistry.EXPECT().SetPlatformUsages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	mockMetric.EXPECT().ClusterRegistered().Times(1)

	mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
		eventstest.WithNameMatcher(eventgen.ClusterRegistrationSucceededEventName))).Times(1)
}

func createInstallConfigBuilder() installcfg_builder.InstallConfigBuilder {
	log := common.GetTestLog().WithField("pkg", "installcfg")
	mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
	return installcfg_builder.NewInstallConfigBuilder(log, mockMirrorRegistriesConfigBuilder, registry.InitProviderRegistry(log))

}

func mockClusterUpdateSuccess(times int, hosts int) {
	mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(hosts * times)
	mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(hosts * times)
	mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).Times(times)
	mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(times)
	mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(hosts * times)
	mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, times)
}

func mockInfraEnvRegisterSuccess() {
	mockOSImages.EXPECT().GetOsImageOrLatest(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
	mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
	mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("", nil).Times(1)
	mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).Times(1)
	mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
		eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName))).AnyTimes()
}

func mockInfraEnvUpdateSuccess() {
	mockOSImages.EXPECT().GetOsImageOrLatest(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
	mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
	mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).Times(1)
	mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
		eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName))).AnyTimes()
}

func mockInfraEnvDeRegisterSuccess() {
	mockInfraEnvApi.EXPECT().DeregisterInfraEnv(gomock.Any(), gomock.Any()).Return(nil).Times(1)
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

func getTestAuthzHandler() auth.Authorizer {
	return &auth.NoneHandler{}
}

func strToUUID(s string) *strfmt.UUID {
	u := strfmt.UUID(s)
	return &u
}

func getDefaultClusterCreateParams() *models.ClusterCreateParams {
	return &models.ClusterCreateParams{
		Name:             swag.String("some-cluster-name"),
		OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
		PullSecret:       swag.String(fakePullSecret),
		Platform: &models.Platform{
			Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
		},
		CPUArchitecture:      models.ClusterCPUArchitectureX8664,
		HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
	}
}

func mockGenerateInstallConfigSuccess(mockGenerator *generator.MockISOInstallConfigGenerator, mockVersions *versions.MockHandler) {
	if mockGenerator != nil {
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
		mockGenerator.EXPECT().GenerateInstallConfig(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}
}

func mockGetInstallConfigSuccess(mockInstallConfigBuilder *installcfg_builder.MockInstallConfigBuilder) {
	mockInstallConfigBuilder.EXPECT().GetInstallConfig(gomock.Any(), gomock.Any(), gomock.Any()).Return([]byte("{}"), nil).AnyTimes()
}

func addVMToCluster(cluster *common.Cluster, db *gorm.DB) {
	hostID := strfmt.UUID(uuid.New().String())
	infraEnv := createInfraEnv(db, strfmt.UUID(uuid.New().String()), *cluster.ID)
	inventory := models.Inventory{
		SystemVendor: &models.SystemVendor{
			Virtual: true,
		},
	}
	inventoryByte, err := json.Marshal(inventory)
	Expect(err).ToNot(HaveOccurred())
	host := addHost(hostID, models.HostRoleAutoAssign, models.HostStatusKnown, models.HostKindHost,
		*infraEnv.ID, *cluster.ID, string(inventoryByte), db)
	cluster.Hosts = append(cluster.Hosts, &host)
}

func createClusterWithAvailability(db *gorm.DB, status string, highAvailabilityMode string) *common.Cluster {
	clusterID := strfmt.UUID(uuid.New().String())
	c := &common.Cluster{
		Cluster: models.Cluster{
			ID:                   &clusterID,
			Status:               swag.String(status),
			HighAvailabilityMode: &highAvailabilityMode,
			OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
			CPUArchitecture:      common.DefaultCPUArchitecture,
			Platform: &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
			},
		},
	}
	Expect(db.Create(c).Error).ToNot(HaveOccurred())
	return c
}

func createCluster(db *gorm.DB, status string) *common.Cluster {
	return createClusterWithAvailability(db, status, models.ClusterCreateParamsHighAvailabilityModeFull)
}

func createInfraEnv(db *gorm.DB, id strfmt.UUID, clusterID strfmt.UUID) *common.InfraEnv {
	infraEnv := &common.InfraEnv{
		InfraEnv: models.InfraEnv{
			ID:        &id,
			ClusterID: clusterID,
		},
	}
	Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())
	return infraEnv
}

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

	It("register host with non-existing cluster", func() {
		clusterID := strfmt.UUID(uuid.New().String())
		infraEnv := createInfraEnv(db, clusterID, clusterID)

		reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
			InfraEnvID: *infraEnv.ID,
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
		_ = createInfraEnv(db, *cluster.ID, *cluster.ID)

		allowedStates := []string{
			models.ClusterStatusInsufficient, models.ClusterStatusReady,
			models.ClusterStatusPendingForInput, models.ClusterStatusAddingHosts}
		err := errors.Errorf(
			"Cluster %s is in installing state, host can register only in one of %s",
			cluster.ID, allowedStates)

		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(err).Times(1)

		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostRegistrationFailedEventName),
			eventstest.WithHostIdMatcher(hostID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError))).Times(1)

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
				infraEnv := createInfraEnv(db, *cluster.ID, *cluster.ID)

				mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(ctx context.Context, h *models.Host, db *gorm.DB) error {
						// validate that host is registered with auto-assign role
						Expect(h.Role).Should(Equal(test.expectedRole))
						Expect(h.InfraEnvID).Should(Equal(*infraEnv.ID))
						return nil
					}).Times(1)
				mockCRDUtils.EXPECT().CreateAgentCR(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)

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
				Expect(command.Command).Should(BeEmpty())
				Expect(command.Args).ShouldNot(BeEmpty())
			})
		}

		It("Should only emit HostRegistrationSucceededEventName when not in pre installation state", func() {
			cluster := createClusterWithAvailability(db, models.ClusterStatusInstalling, models.ClusterHighAvailabilityModeFull)
			infraEnv := createInfraEnv(db, *cluster.ID, *cluster.ID)

			mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
			mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, h *models.Host, db *gorm.DB) error {
					// validate that host is registered with auto-assign role
					Expect(h.Role).Should(Equal(models.HostRoleAutoAssign))
					Expect(h.InfraEnvID).Should(Equal(*infraEnv.ID))
					return nil
				}).Times(1)
			mockCRDUtils.EXPECT().CreateAgentCR(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.HostRegistrationSucceededEventName),
				eventstest.WithHostIdMatcher(hostID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo))).MaxTimes(0)

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
			Expect(command.Command).Should(BeEmpty())
			Expect(command.Args).ShouldNot(BeEmpty())
		})
	})

	It("add_crd_failure", func() {
		cluster := createCluster(db, models.ClusterStatusInsufficient)
		infraEnv := createInfraEnv(db, *cluster.ID, *cluster.ID)
		expectedErrMsg := "some-internal-error"

		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, h *models.Host, db *gorm.DB) error {
				// validate that host is registered with auto-assign role
				Expect(h.Role).Should(Equal(models.HostRoleAutoAssign))
				Expect(h.InfraEnvID).Should(Equal(*infraEnv.ID))
				return nil
			}).Times(1)
		mockHostApi.EXPECT().UnRegisterHost(ctx, gomock.Any()).Return(nil).Times(1)
		mockCRDUtils.EXPECT().CreateAgentCR(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New(expectedErrMsg)).Times(1)
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
		_ = createInfraEnv(db, *cluster.ID, *cluster.ID)
		expectedErrMsg := "some-internal-error"

		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New(expectedErrMsg)).Times(1)
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostRegistrationFailedEventName),
			eventstest.WithHostIdMatcher(hostID.String()),
			eventstest.WithInfraEnvIdMatcher(cluster.ID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError))).Times(1)
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

	It("register day2 bound host", func() {
		cluster := createCluster(db, models.ClusterStatusAddingHosts)
		Expect(db.Model(&cluster).Update("kind", swag.String(models.ClusterKindAddHostsCluster)).Error).ShouldNot(HaveOccurred())
		infraEnvId := strToUUID(uuid.New().String())
		_ = createInfraEnv(db, *infraEnvId, "")

		hostId := strToUUID(uuid.New().String())
		host := models.Host{
			ID:         hostId,
			InfraEnvID: *infraEnvId,
			ClusterID:  cluster.ID,
			Status:     swag.String("binding"),
			Kind:       swag.String(models.HostKindHost),
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		mockHostApi.EXPECT().RegisterHost(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, h *models.Host, db *gorm.DB) error {
				// validate that host kind was set to day2
				Expect(swag.StringValue(h.Kind)).Should(Equal(models.HostKindAddToExistingClusterHost))
				return nil
			}).Times(1)
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockCRDUtils.EXPECT().CreateAgentCR(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)

		By("trying to register a host bound to day2 cluster")
		reply := bm.V2RegisterHost(ctx, installer.V2RegisterHostParams{
			InfraEnvID: *infraEnvId,
			NewHostParams: &models.HostCreateParams{
				DiscoveryAgentVersion: "v1",
				HostID:                hostId,
			},
		})

		By("verifying returned response")
		_, ok := reply.(*installer.V2RegisterHostCreated)
		Expect(ok).Should(BeTrue())
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
		apiErr, ok := generateReply.(*common.ApiErrorResponse)
		Expect(ok).Should(BeTrue())
		Expect(apiErr.StatusCode()).Should(Equal(int32(http.StatusNotFound)))
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

	Context("verify vips", func() {
		var (
			hostId    strfmt.UUID
			clusterId strfmt.UUID
		)
		verifiedVips := models.VerifyVipsResponse{
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          "1.2.3.4",
				VipType:      models.VipTypeAPI,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationSucceeded),
				Vip:          "1001:db8::100",
				VipType:      models.VipTypeAPI,
			},
			{
				Verification: common.VipVerificationPtr(models.VipVerificationFailed),
				Vip:          "1.2.3.5",
				VipType:      models.VipTypeIngress,
			},
		}
		BeforeEach(func() {
			ctx = context.Background()
			clusterId = strfmt.UUID(uuid.New().String())
			hostId = strfmt.UUID(uuid.New().String())
			host := models.Host{
				ID:         &hostId,
				InfraEnvID: clusterId,
				ClusterID:  &clusterId,
				Status:     swag.String(models.HostStatusInsufficient),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})
		var makeStepReply = func(clusterID, hostID strfmt.UUID, response models.VerifyVipsResponse) installer.V2PostStepReplyParams {
			b, _ := json.Marshal(&response)
			return installer.V2PostStepReplyParams{
				InfraEnvID: clusterID,
				HostID:     hostID,
				Reply: &models.StepReply{
					Output:   string(b),
					StepType: models.StepTypeVerifyVips,
				},
			}
		}

		marshal := func(response models.VerifyVipsResponse) string {
			b, err := json.Marshal(&response)
			Expect(err).ToNot(HaveOccurred())
			return string(b)
		}

		It("happy flow", func() {
			mockClusterApi.EXPECT().HandleVerifyVipsResponse(ctx, clusterId, marshal(verifiedVips)).Return(nil)
			reply := bm.V2PostStepReply(ctx, makeStepReply(clusterId, hostId, verifiedVips))
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
		It("error occurred", func() {
			mockClusterApi.EXPECT().HandleVerifyVipsResponse(ctx, clusterId, marshal(verifiedVips)).Return(errors.New("blah"))
			reply := bm.V2PostStepReply(ctx, makeStepReply(clusterId, hostId, verifiedVips))
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
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
			params := makeStepReply(*clusterId, *hostId, makeResponseWithLeases(string(common.TestIPv4Networking.APIVips[0].IP), string(common.TestIPv4Networking.IngressVips[0].IP), "lease { hello abc; }", "lease { hello abc; }"))
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
			params := makeStepReply(*clusterId, *hostId, makeResponseWithLeases(string(common.TestIPv4Networking.APIVips[0].IP), string(common.TestIPv4Networking.IngressVips[0].IP), "llease { hello abc; }", "lease { hello abc; }"))
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
			params := makeStepReply(*clusterId, *hostId, makeResponse(string(common.TestIPv4Networking.APIVips[0].IP), string(common.TestIPv4Networking.IngressVips[0].IP)))
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
			params := makeStepReply(*clusterId, *hostId, makeResponse(string(common.TestIPv4Networking.APIVips[0].IP), string(common.TestIPv4Networking.IngressVips[0].IP)))
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
			params := makeStepReply(*clusterId, *hostId, makeResponse(string(common.TestIPv4Networking.APIVips[0].IP), "1.2.4.11"))
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyInternalServerError()))
		})
		It("New IPs while in insufficient", func() {
			cluster := common.Cluster{
				Cluster: models.Cluster{
					ID:                clusterId,
					VipDhcpAllocation: swag.Bool(true),
					MachineNetworks:   common.TestIPv4Networking.MachineNetworks,
					APIVips:           common.TestIPv4Networking.APIVips,
					IngressVips:       common.TestIPv4Networking.IngressVips,
					Status:            swag.String(models.ClusterStatusInsufficient),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			params := makeStepReply(*clusterId, *hostId, makeResponse(
				common.IncrementIPString(string(common.TestIPv4Networking.APIVips[0].IP)), common.IncrementIPString(string(common.TestIPv4Networking.IngressVips[0].IP))))
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
					APIVips:           common.TestIPv4Networking.APIVips,
					IngressVips:       common.TestIPv4Networking.IngressVips,
					Status:            swag.String(models.ClusterStatusInstalling),
				},
			}
			Expect(db.Create(&cluster).Error).ToNot(HaveOccurred())
			mockClusterApi.EXPECT().SetVipsData(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("Stam"))

			params := makeStepReply(*clusterId, *hostId, makeResponse(
				common.IncrementIPString(string(common.TestIPv4Networking.APIVips[0].IP)), common.IncrementIPString(string(common.TestIPv4Networking.IngressVips[0].IP))))
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
			}, PullSecret: fakePullSecret}

			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		})

		It("Disk speed success", func() {
			mockHostApi.EXPECT().SetDiskSpeed(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockMetric.EXPECT().DiskSyncDuration(gomock.Any()).Times(1)
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

	Context("download boot artifacts", func() {
		var (
			infraEnvID strfmt.UUID
			hostID     strfmt.UUID
		)

		BeforeEach(func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			hostID = strfmt.UUID(uuid.New().String())

			host := &models.Host{
				ID:         &hostID,
				InfraEnvID: infraEnvID,
				Status:     swag.String(models.HostStatusReclaiming),
			}
			Expect(db.Create(host).Error).ToNot(HaveOccurred())
		})

		It("calls HandleReclaimBootArtifactDownload on success", func() {
			mockHostApi.EXPECT().HandleReclaimBootArtifactDownload(gomock.Any(), gomock.Any()).Return(nil)
			params := installer.V2PostStepReplyParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				Reply: &models.StepReply{
					Output:   "download success",
					StepType: models.StepTypeDownloadBootArtifacts,
				},
			}
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})

		It("calls HandleReclaimFailure on error", func() {
			mockHostApi.EXPECT().HandleReclaimFailure(gomock.Any(), gomock.Any()).Return(nil)
			params := installer.V2PostStepReplyParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				Reply: &models.StepReply{
					Output:   "download failed",
					ExitCode: -1,
					StepType: models.StepTypeDownloadBootArtifacts,
				},
			}
			reply := bm.V2PostStepReply(ctx, params)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2PostStepReplyNoContent()))
		})
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
				mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.HostInstallProgressUpdatedEventName),
					eventstest.WithHostIdMatcher(hostID.String()),
					eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
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
			Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusInternalServerError, errors.Errorf("some error"))))
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
		Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
	})
})

func mockSetConnectivityMajorityGroupsForCluster(mockClusterApi *cluster.MockAPI) {
	mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).Times(1)
}

func mockDetectAndStoreCollidingIPsForCluster(mockClusterApi *cluster.MockAPI, times int) {
	mockClusterApi.EXPECT().DetectAndStoreCollidingIPsForCluster(gomock.Any(), gomock.Any()).Return(nil).Times(times)
}

var _ = Describe("cluster", func() {
	masterHostId1 := strfmt.UUID(uuid.New().String())
	masterHostId2 := strfmt.UUID(uuid.New().String())
	masterHostId3 := strfmt.UUID(uuid.New().String())

	var (
		bm             *bareMetalInventory
		cfg            Config
		db             *gorm.DB
		ctx            = context.Background()
		clusterID      strfmt.UUID
		infraEnvID     strfmt.UUID
		dbName         string
		ignitionReader io.ReadCloser
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		Expect(cfg.IPv6Support).Should(BeTrue())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
		bm.ocmClient = nil

		ignitionReader = io.NopCloser(strings.NewReader(`{
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
		mockHostApi.EXPECT().CancelInstallation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.NewApiError(http.StatusConflict, fmt.Errorf("error"))).Times(1)
	}
	setCancelInstallationInternalServerError := func() {
		mockClusterApi.EXPECT().CancelInstallation(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.NewApiError(http.StatusInternalServerError, fmt.Errorf("error"))).Times(1)
	}
	setResetClusterSuccess := func() {
		mockS3Client.EXPECT().DeleteObject(gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
		mockClusterApi.EXPECT().ResetClusterFiles(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
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
			Return(false, errors.Errorf("")).Times(1)
	}
	mockAutoAssignSuccess := func(times int) {
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(times)
	}
	mockFalseAutoAssignSuccess := func(times int) {
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil).Times(times)
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
			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
				APIVips:          []*models.APIVip{{IP: "10.11.12.13"}},
				IngressVips:      []*models.IngressVip{{IP: "10.11.12.14"}},
				MachineNetworks:  []*models.MachineNetwork{{Cidr: "10.11.0.0/16"}},
				Platform: &models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
				},
				CPUArchitecture: common.DefaultCPUArchitecture,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
			addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
			addHost(masterHostId3, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname2", "bootMode", "1.2.3.6/24", "7.8.9.10/24"), db)
		})

		Context("ListClusterHosts", func() {
			workerHostId1 := strfmt.UUID(uuid.New().String())
			workerHostId2 := strfmt.UUID(uuid.New().String())
			BeforeEach(func() {
				addHost(workerHostId1, models.HostRoleWorker, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname3", "bootMode", "1.2.3.7/24", "10.11.50.70/16"), db)
				addHost(workerHostId2, models.HostRoleWorker, "insufficient", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname4", "bootMode", "1.2.3.8/24", "10.11.50.60/16"), db)
				Expect(db.Model(&models.Host{}).Where("cluster_id = ?", clusterID.String()).Update("connectivity", "123445").Error).ToNot(HaveOccurred())
			})

			expectedIds := func(hostList models.HostList, ids ...strfmt.UUID) {
				foundIds := make(map[strfmt.UUID]bool)
				for _, h := range hostList {
					Expect(ids).To(ContainElement(*h.ID))
					foundIds[*h.ID] = true
				}
				Expect(ids).To(HaveLen(len(foundIds)))
			}

			expectInventory := func(hostList models.HostList, inventoryExpected bool) {
				for _, h := range hostList {
					if inventoryExpected {
						ExpectWithOffset(1, h.Inventory).ToNot(BeEmpty())
					} else {
						ExpectWithOffset(1, h.Inventory).To(BeEmpty())
					}
				}
			}
			expectConnectivity := func(hostList models.HostList, connectivityExpected bool) {
				for _, h := range hostList {
					if connectivityExpected {
						ExpectWithOffset(1, h.Connectivity).ToNot(BeEmpty())
					} else {
						ExpectWithOffset(1, h.Connectivity).To(BeEmpty())
					}
				}
			}
			It("ListClusterHosts - unfiltered", func() {
				reply := bm.ListClusterHosts(ctx, installer.ListClusterHostsParams{
					ClusterID: clusterID,
				})
				actual, ok := reply.(*installer.ListClusterHostsOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload).To(HaveLen(5))
				expectedIds(actual.Payload, masterHostId1, masterHostId2, masterHostId3, workerHostId1, workerHostId2)
				expectInventory(actual.Payload, false)
				expectConnectivity(actual.Payload, false)
			})
			It("ListClusterHosts - filtered", func() {
				reply := bm.ListClusterHosts(ctx, installer.ListClusterHostsParams{
					ClusterID: clusterID,
					Role:      swag.String("master"),
				})
				actual, ok := reply.(*installer.ListClusterHostsOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload).To(HaveLen(3))
				expectedIds(actual.Payload, masterHostId1, masterHostId2, masterHostId3)
				expectInventory(actual.Payload, false)
				expectConnectivity(actual.Payload, false)

				reply = bm.ListClusterHosts(ctx, installer.ListClusterHostsParams{
					ClusterID: clusterID,
					Role:      swag.String("worker"),
				})
				actual, ok = reply.(*installer.ListClusterHostsOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload).To(HaveLen(2))
				expectedIds(actual.Payload, workerHostId1, workerHostId2)
				expectInventory(actual.Payload, false)
				expectConnectivity(actual.Payload, false)

				reply = bm.ListClusterHosts(ctx, installer.ListClusterHostsParams{
					ClusterID: clusterID,
					Status:    swag.String("known"),
				})
				actual, ok = reply.(*installer.ListClusterHostsOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload).To(HaveLen(4))
				expectedIds(actual.Payload, masterHostId1, masterHostId2, masterHostId3, workerHostId1)
				expectInventory(actual.Payload, false)
				expectConnectivity(actual.Payload, false)

				reply = bm.ListClusterHosts(ctx, installer.ListClusterHostsParams{
					ClusterID: clusterID,
					Status:    swag.String("known"),
					Role:      swag.String("worker"),
				})
				actual, ok = reply.(*installer.ListClusterHostsOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload).To(HaveLen(1))
				expectedIds(actual.Payload, workerHostId1)
				expectInventory(actual.Payload, false)
				expectConnectivity(actual.Payload, false)
			})
			It("ListClusterHosts - large fields", func() {
				reply := bm.ListClusterHosts(ctx, installer.ListClusterHostsParams{
					ClusterID:     clusterID,
					WithInventory: swag.Bool(true),
				})
				actual, ok := reply.(*installer.ListClusterHostsOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload).To(HaveLen(5))
				expectedIds(actual.Payload, masterHostId1, masterHostId2, masterHostId3, workerHostId1, workerHostId2)
				expectInventory(actual.Payload, true)
				expectConnectivity(actual.Payload, false)

				reply = bm.ListClusterHosts(ctx, installer.ListClusterHostsParams{
					ClusterID:        clusterID,
					WithInventory:    swag.Bool(true),
					WithConnectivity: swag.Bool(true),
				})
				actual, ok = reply.(*installer.ListClusterHostsOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload).To(HaveLen(5))
				expectedIds(actual.Payload, masterHostId1, masterHostId2, masterHostId3, workerHostId1, workerHostId2)
				expectInventory(actual.Payload, true)
				expectConnectivity(actual.Payload, true)
			})
		})

		Context("GetCluster", func() {
			It("success", func() {
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(3) // Number of hosts
				mockDurationsSuccess()
				reply := bm.V2GetCluster(ctx, installer.V2GetClusterParams{
					ClusterID: clusterID,
				})
				actual, ok := reply.(*installer.V2GetClusterOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload.Hosts).To(HaveLen(3))
				Expect(string(actual.Payload.APIVips[0].IP)).To(BeEquivalentTo("10.11.12.13"))
				Expect(string(actual.Payload.IngressVips[0].IP)).To(BeEquivalentTo("10.11.12.14"))
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
			It("exclude hosts", func() {
				mockDurationsSuccess()
				reply := bm.V2GetCluster(ctx, installer.V2GetClusterParams{
					ClusterID:    clusterID,
					ExcludeHosts: swag.Bool(true),
				})
				actual, ok := reply.(*installer.V2GetClusterOK)
				Expect(ok).To(BeTrue())
				Expect(actual.Payload.Hosts).To(BeEmpty())
				Expect(string(actual.Payload.APIVips[0].IP)).To(BeEquivalentTo("10.11.12.13"))
				Expect(string(actual.Payload.IngressVips[0].IP)).To(BeEquivalentTo("10.11.12.14"))
				validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{{Cidr: "10.11.0.0/16"}})
				Expect(actual.Payload.HostNetworks).To(BeEmpty())
			})

			It("Unfamilliar ID", func() {
				resp := bm.V2GetCluster(ctx, installer.V2GetClusterParams{ClusterID: "12345"})
				Expect(resp).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
			})

			It("DB inaccessible", func() {
				common.DeleteTestDB(db, dbName)
				resp := bm.V2GetCluster(ctx, installer.V2GetClusterParams{ClusterID: clusterID})
				Expect(resp).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusInternalServerError, errors.Errorf(""))))
			})
		})

		Context("GetUnregisteredClusters", func() {
			scopedDB := func(db *gorm.DB) *gorm.DB {
				return db
			}

			unscopedDB := func(db *gorm.DB) *gorm.DB {
				return db.Unscoped()
			}

			deleteCluster := func(deletePermanently bool) {
				c, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
				Expect(err).ShouldNot(HaveOccurred())

				getDB := scopedDB

				if deletePermanently {
					getDB = unscopedDB
				}

				for _, host := range c.Hosts {
					Expect(getDB(db).Delete(host).Error).ShouldNot(HaveOccurred())
				}
				Expect(getDB(db).Delete(&common.Cluster{}, "id = ?", clusterID.String()).Error).ShouldNot(HaveOccurred())
			}

			It("success", func() {
				deleteCluster(false)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(3)
				resp := bm.V2GetCluster(ctx, installer.V2GetClusterParams{ClusterID: clusterID, GetUnregisteredClusters: swag.Bool(true)})
				cluster := resp.(*installer.V2GetClusterOK).Payload
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
				resp := bm.V2GetCluster(ctx, installer.V2GetClusterParams{ClusterID: clusterID, GetUnregisteredClusters: swag.Bool(true)})
				Expect(resp).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
			})

			It("failure - not an admin user", func() {
				deleteCluster(false)
				payload := &ocm.AuthPayload{}
				payload.Role = ocm.UserRole
				ctx = context.WithValue(ctx, restapi.AuthKey, payload)
				authCfg := auth.GetConfigRHSSO()
				bm.authzHandler = auth.NewAuthzHandler(authCfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)
				resp := bm.V2GetCluster(ctx, installer.V2GetClusterParams{ClusterID: clusterID, GetUnregisteredClusters: swag.Bool(true)})
				Expect(resp).Should(BeAssignableToTypeOf(common.NewInfraError(http.StatusForbidden, errors.Errorf(""))))
			})
		})
	})
	Context("Create non HA cluster", func() {
		var clusterParams *models.ClusterCreateParams

		BeforeEach(func() {
			mockDurationsSuccess()
			clusterParams = getDefaultClusterCreateParams()
			clusterParams.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
		})
		It("happy flow", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, mockStream, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.8")

			noneHaMode := models.ClusterHighAvailabilityModeNone
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			clusterParams.OpenshiftVersion = swag.String(MinimalOpenShiftVersionForNoneHA)
			clusterParams.HighAvailabilityMode = &noneHaMode
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.HighAvailabilityMode).To(Equal(swag.String(noneHaMode)))
			Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
			Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
		})
		It("create non ha cluster fail, release version is lower than minimal", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
			noneHaMode := models.ClusterHighAvailabilityModeNone
			insufficientOpenShiftVersionForNoneHA := "4.7"
			clusterParams.OpenshiftVersion = swag.String(insufficientOpenShiftVersionForNoneHA)
			clusterParams.HighAvailabilityMode = &noneHaMode
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("create non ha cluster fail, release version is pre-release and lower than minimal", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
			noneHaMode := models.ClusterHighAvailabilityModeNone
			insufficientOpenShiftVersionForNoneHA := "4.7.0-fc.1"
			clusterParams.OpenshiftVersion = swag.String(insufficientOpenShiftVersionForNoneHA)
			clusterParams.HighAvailabilityMode = &noneHaMode
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("create non ha cluster success, release version is greater than minimal", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)

			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.8")
			noneHaMode := models.ClusterHighAvailabilityModeNone
			openShiftVersionForNoneHA := "4.8.0"
			clusterParams.OpenshiftVersion = swag.String(openShiftVersionForNoneHA)
			clusterParams.HighAvailabilityMode = &noneHaMode
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.HighAvailabilityMode).To(Equal(swag.String(noneHaMode)))
			Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
			Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
		})
		It("create non ha cluster success, release version is pre-release and greater than minimal", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)

			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.8")
			noneHaMode := models.ClusterHighAvailabilityModeNone
			openShiftVersionForNoneHA := "4.8.0-fc.2"
			clusterParams.OpenshiftVersion = swag.String(openShiftVersionForNoneHA)
			clusterParams.HighAvailabilityMode = &noneHaMode
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.HighAvailabilityMode).To(Equal(swag.String(noneHaMode)))
			Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
			Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
		})
		It("create non ha cluster fail, explicitly disabled UserManagedNetworking", func() {
			errStr := "Can't set none platform with user-managed-networking disabled"
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
			noneHaMode := models.ClusterHighAvailabilityModeNone
			openShiftVersionForNoneHA := "4.8.0-fc.2"
			clusterParams.OpenshiftVersion = swag.String(openShiftVersionForNoneHA)
			clusterParams.HighAvailabilityMode = &noneHaMode
			clusterParams.UserManagedNetworking = swag.Bool(false)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			verifyApiErrorString(reply, http.StatusBadRequest, errStr)
		})
		It("create non ha cluster fail, explicitly enabled VipDhcpAllocation", func() {
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
			noneHaMode := models.ClusterHighAvailabilityModeNone
			openShiftVersionForNoneHA := "4.8.0-fc.2"
			clusterParams.OpenshiftVersion = swag.String(openShiftVersionForNoneHA)
			clusterParams.HighAvailabilityMode = &noneHaMode
			clusterParams.VipDhcpAllocation = swag.Bool(true)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			verifyApiErrorString(reply, http.StatusBadRequest,
				"VIP DHCP Allocation cannot be enabled on single node OpenShift")
		})
	})
	It("create non ha cluster success, release version is ci-release and greater than minimal", func() {
		bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
			db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)

		mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.8")
		noneHaMode := models.ClusterHighAvailabilityModeNone
		openShiftVersionForNoneHA := "4.8.0-0.ci.test-2021-05-20-000749-ci-op-7xrzwgwy-latest"
		clusterParams := getDefaultClusterCreateParams()
		clusterParams.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
		clusterParams.OpenshiftVersion = swag.String(openShiftVersionForNoneHA)
		clusterParams.HighAvailabilityMode = &noneHaMode
		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: clusterParams,
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.HighAvailabilityMode).To(Equal(swag.String(noneHaMode)))
		Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
		Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
	})

	Context("Update", func() {
		BeforeEach(func() {
			mockDurationsSuccess()
		})

		mockSuccess := func() {
			mockClusterUpdateSuccess(1, 0)
		}

		It("update_cluster_while_installing", func() {
			clusterID = strfmt.UUID(uuid.New().String())
			cluster := &common.Cluster{Cluster: models.Cluster{
				ID: &clusterID,
			}}
			err := db.Create(cluster).Error
			Expect(err).ShouldNot(HaveOccurred())

			mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(errors.Errorf("wrong state")).Times(1)

			apiVip := "8.8.8.8"
			ingressVip := "1.1.1.1"
			reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					APIVips:     []*models.APIVip{{IP: models.IP(apiVip)}},
					IngressVips: []*models.IngressVip{{IP: models.IP(ingressVip)}},
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusConflict, errors.Errorf("error"))))
		})

		Context("check pull secret", func() {
			BeforeEach(func() {
				v, err := validations.NewPullSecretValidator(validations.Config{}, getTestAuthHandler())
				Expect(err).ShouldNot(HaveOccurred())
				bm.secretValidator = v
			})

			It("Invalid pull-secret", func() {
				pullSecret := "asdfasfda"
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						PullSecret: &pullSecret,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf(""))))
			})

			It("pull-secret with newline", func() {
				pullSecret := "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}" // #nosec
				pullSecretWithNewline := pullSecret + " \n"
				clusterID = strfmt.UUID(uuid.New().String())
				cluster := &common.Cluster{Cluster: models.Cluster{
					ID: &clusterID,
					Platform: &models.Platform{
						Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
					},
					OpenshiftVersion: "4.12",
					CPUArchitecture:  common.DefaultCPUArchitecture,
				}}
				err := db.Create(cluster).Error
				Expect(err).ShouldNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				mockSuccess()
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						PullSecret: &pullSecretWithNewline,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
			})
		})

		It("update cluster day1 with APIVipDNSName failed", func() {
			mockOperators := operators.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, mockOperators, nil, nil, nil, nil, nil, false)

			mockClusterRegisterSuccess(true)

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: getDefaultClusterCreateParams(),
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated)
			c, err := bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())

			newClusterName := "day1-cluster-new-name"

			reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
				ClusterID: *c.ID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					Name:          &newClusterName,
					APIVipDNSName: swag.String("some dns name"),
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
		})

		DescribeTable(
			"update cluster with compatible network_type and openshift version successfully",
			func(openshiftVersion, networkType string) {
				cpuArchitecture := "irrelevant"
				mockOperators := operators.NewMockAPI(ctrl)
				bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, mockOperators, nil, nil, nil, nil, nil, false)

				mockClusterRegisterSuccessWithVersion(cpuArchitecture, openshiftVersion)

				mockEvents.EXPECT().SendClusterEvent(ctx, gomock.Any())
				mockOperators.EXPECT().ValidateCluster(ctx, gomock.Any())

				clusterCreateParams := &models.ClusterCreateParams{
					Name:             swag.String("some-cluster-name"),
					OpenshiftVersion: swag.String(openshiftVersion),
					PullSecret:       swag.String(fakePullSecret),
					Platform: &models.Platform{
						Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
					},
					CPUArchitecture:      models.ClusterCPUArchitectureX8664,
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
				}

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: clusterCreateParams,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated)
				c, err := bm.getCluster(ctx, actual.Payload.ID.String())
				Expect(err).ToNot(HaveOccurred())

				newClusterName := "day1-cluster-new-name"
				reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: *c.ID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						Name:        &newClusterName,
						NetworkType: &networkType,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

			},
			Entry("OVN supported for 4.15", "4.15.2", models.ClusterCreateParamsNetworkTypeOVNKubernetes),
			Entry("OVN supported for versions above 4.15", "4.18.3", models.ClusterCreateParamsNetworkTypeOVNKubernetes),
			Entry("OVN supported for versions below 4.15", "4.13.1", models.ClusterCreateParamsNetworkTypeOVNKubernetes),
			Entry("SDN supported until 4.14", "4.14.0", models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
		)

		DescribeTable(
			"update cluster with incompatible network_type and openshift version failed",
			func(openshiftVersion, networkType string) {
				cpuArchitecture := "irrelevant"
				mockOperators := operators.NewMockAPI(ctrl)
				bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, mockOperators, nil, nil, nil, nil, nil, false)

				mockClusterRegisterSuccessWithVersion(cpuArchitecture, openshiftVersion)
				clusterCreateParams := &models.ClusterCreateParams{
					Name:             swag.String("some-cluster-name"),
					OpenshiftVersion: swag.String(openshiftVersion),
					PullSecret:       swag.String(fakePullSecret),
					Platform: &models.Platform{
						Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
					},
					CPUArchitecture:      models.ClusterCPUArchitectureX8664,
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
				}

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: clusterCreateParams,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated)
				c, err := bm.getCluster(ctx, actual.Payload.ID.String())
				Expect(err).ToNot(HaveOccurred())

				newClusterName := "day1-cluster-new-name"

				reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: *c.ID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						Name:        &newClusterName,
						NetworkType: &networkType,
					},
				})
				expectedErrorMessage := fmt.Sprintf("Openshift version %s is not supported for %s NetworkType", openshiftVersion, networkType)
				verifyApiErrorString(reply, http.StatusBadRequest, expectedErrorMessage)
			},
			Entry("SDN not supported for 4.15", "4.15.2", models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
			Entry("SDN not supported for versions above 4.15", "4.17.6", models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
		)

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

			mockLVMGetOperatorByName := func(operatorName string) {
				mockOperatorManager.EXPECT().GetOperatorByName(operatorName).Return(
					&models.MonitoredOperator{
						Name:           "lvm",
						OperatorType:   models.OperatorTypeOlm,
						Namespace:      "openshift-storage",
						TimeoutSeconds: 30 * 60,
					}, nil).Times(1)
			}

			mockCNVGetOperatorByName := func(operatorName string) {
				mockOperatorManager.EXPECT().GetOperatorByName(operatorName).Return(
					&models.MonitoredOperator{
						Name:             "cnv",
						Namespace:        "openshift-cnv",
						OperatorType:     models.OperatorTypeOlm,
						SubscriptionName: "hco-operatorhub",
						TimeoutSeconds:   60 * 60,
					}, nil).Times(1)
			}

			Context("V2RegisterCluster", func() {
				var openshiftVersion string

				BeforeEach(func() {
					openshiftVersion = "4.12.0"
					bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
						db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
				})
				Context("RegisterCluster - Multiple-VIPs Support", func() {

					It("Two APIVips and Two IngressVips - both IPv4 - negative", func() {
						apiVip := "8.8.8.7"
						ingressVip := "8.8.8.1"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("8.8.8.8")}}
						ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("8.8.8.2")}}
						err := "the second element of apiVIPs must be an IPv6 address. got: 8.8.8.8"
						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, err)
					})

					It("Two APIVips and Two IngressVips - IPv4 first and IPv6 second - positive", func() {
						mockClusterRegisterSuccess(true)

						apiVip := "8.8.8.7"
						ingressVip := "8.8.8.1"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::1")}}
						ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::2")}}

						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
						cluster := reply.(*installer.V2RegisterClusterCreated).Payload
						Expect(cluster.APIVips).To(Equal(apiVips))
						Expect(cluster.IngressVips).To(Equal(ingressVips))
					})

					It("Two APIVips and Two IngressVips - IPv6 first and IPv4 second - negative", func() {
						apiVip := "2001:db8::1"
						ingressVip := "2001:db8::2"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("8.8.8.7")}}
						ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("8.8.8.1")}}
						err := "the first element of apiVIPs must be an IPv4 address. got: 2001:db8::1"
						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, err)
					})

					It("Two APIVips and Two IngressVips - IPv6 - negative", func() {
						apiVip := "2001:db8::1"
						ingressVip := "2001:db8::2"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
						ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}}
						err := "the first element of apiVIPs must be an IPv4 address. got: 2001:db8::1"
						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, err)
					})

					It("More than two APIVips and More than two IngressVips -  negative", func() {
						apiVip := "8.8.8.1"
						ingressVip := "8.8.8.2"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}, {IP: models.IP("8.8.8.3")}}
						ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}, {IP: models.IP("8.8.8.4")}}
						err := "apiVIPs supports 2 vips. got: 3"

						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, err)
					})

					It("Non parsable APIVips and non parsable IngressVips - negative", func() {
						apiVip := "invalid apiVip"
						ingressVip := "invalid ingressVip"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("invalid apiVip 2")}}
						ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("invalid ingressVip 2")}}
						multiErr := "4 errors occurred:\n\t* Could not parse VIP ip invalid apiVip\n\t* Could not parse VIP ip invalid apiVip 2\n\t* Could not parse VIP ip invalid ingressVip\n\t* Could not parse VIP ip invalid ingressVip 2"

						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, multiErr)
					})

					It("Non parsable APIVips and parsable IngressVips - negative", func() {
						apiVip := "invalid apiVip"
						ingressVip := "8.8.8.2"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
						ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}}
						err := "1 error occurred:\n\t* Could not parse VIP ip invalid apiVip\n"

						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, err)
					})

					It("Different number of APIVips and IngressVips - negative", func() {
						apiVip := "8.8.8.1"
						ingressVip := "8.8.8.2"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
						ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}}
						err := "configuration must include the same number of apiVIPs (got 2) and ingressVIPs (got 1)"

						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, err)
					})

					It("Duplicated addresses in APIVips - negative", func() {
						apiVip := "8.8.8.1"
						ingressVip := "8.8.8.2"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP(apiVip)}}
						ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::3")}}
						err := "The IP address \"8.8.8.1\" appears multiple times in apiVIPs"

						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, err)
					})

					It("Duplicated addresses in IngressVips - negative", func() {
						apiVip := "8.8.8.1"
						ingressVip := "8.8.8.2"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
						ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP(ingressVip)}}
						err := "The IP address \"8.8.8.2\" appears multiple times in ingressVIPs"

						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, err)
					})

					It("Duplicated address across APIVips and IngressVips - negative", func() {
						apiVip := "8.8.8.1"
						apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
						ingressVips := []*models.IngressVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::4")}}
						err := "The IP address \"8.8.8.1\" appears both in apiVIPs and ingressVIPs"

						reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
							NewClusterParams: &models.ClusterCreateParams{
								Name:             swag.String("some-cluster-name"),
								OpenshiftVersion: swag.String(openshiftVersion),
								PullSecret:       swag.String(fakePullSecret),
								APIVips:          apiVips,
								IngressVips:      ingressVips,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, err)
					})

					Context("forbidden with OCP pre-4.12", func() {
						openshiftVersion = "4.11.0"

						It("2 APIVips and 2 IngressVips", func() {
							apiVip := "8.8.8.7"
							ingressVip := "8.8.8.1"
							apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::1")}}
							ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::2")}}
							err := "dual-stack VIPs are not supported in OpenShift 4.11.0"
							reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
								NewClusterParams: &models.ClusterCreateParams{
									Name:             swag.String("some-cluster-name"),
									OpenshiftVersion: swag.String("4.11.0"),
									PullSecret:       swag.String(fakePullSecret),
									APIVips:          apiVips,
									IngressVips:      ingressVips,
								},
							})
							verifyApiErrorString(reply, http.StatusBadRequest, err)
						})
					})

				})

				It("OLM register default value - only builtins", func() {
					mockClusterRegisterSuccess(true)

					reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
						NewClusterParams: getDefaultClusterCreateParams(),
					})
					Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
					actual := reply.(*installer.V2RegisterClusterCreated)
					Expect(containsMonitoredOperator(actual.Payload.MonitoredOperators, &common.TestDefaultConfig.MonitoredOperator)).To(BeTrue())
				})

				It("OLM register non default value", func() {
					newOperatorName := testOLMOperators[0].Name
					newProperties := "blob-info"

					mockClusterRegisterSuccess(true)
					mockGetOperatorByName(newOperatorName)
					mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
						DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
							return operators, nil
						}).Times(1)
					clusterParams := getDefaultClusterCreateParams()
					clusterParams.OlmOperators = []*models.OperatorCreateParams{
						{Name: newOperatorName, Properties: newProperties},
					}
					mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
						NewClusterParams: clusterParams,
					})
					Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
					actual := reply.(*installer.V2RegisterClusterCreated)

					expectedMonitoredOperator := models.MonitoredOperator{
						Name:             newOperatorName,
						Properties:       newProperties,
						OperatorType:     testOLMOperators[0].OperatorType,
						TimeoutSeconds:   testOLMOperators[0].TimeoutSeconds,
						Namespace:        testOLMOperators[0].Namespace,
						SubscriptionName: testOLMOperators[0].SubscriptionName,
						ClusterID:        *actual.Payload.ID,
					}
					Expect(containsMonitoredOperator(actual.Payload.MonitoredOperators, &common.TestDefaultConfig.MonitoredOperator)).To(BeTrue())
					Expect(containsMonitoredOperator(actual.Payload.MonitoredOperators, &expectedMonitoredOperator)).To(BeTrue())
				})

				It("Resolve OLM dependencies", func() {
					newOperatorName := testOLMOperators[1].Name

					mockClusterRegisterSuccess(true)
					mockGetOperatorByName(newOperatorName)
					mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
						DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
							return append(operators, testOLMOperators[0]), nil
						}).Times(1)
					clusterParams := getDefaultClusterCreateParams()
					clusterParams.OlmOperators = []*models.OperatorCreateParams{
						{Name: newOperatorName},
					}

					mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
						NewClusterParams: clusterParams,
					})
					Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
					actual := reply.(*installer.V2RegisterClusterCreated)

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
					for _, m := range []*models.MonitoredOperator{
						&common.TestDefaultConfig.MonitoredOperator,
						&expectedUpdatedMonitoredOperator,
						&expectedResolvedMonitoredOperator,
					} {
						Expect(containsMonitoredOperator(actual.Payload.MonitoredOperators, m)).To(BeTrue())
					}
				})

				It("OLM invalid name", func() {
					newOperatorName := "invalid-name"
					mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
					mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
					mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
					mockOperatorManager.EXPECT().GetOperatorByName(newOperatorName).Return(nil, errors.Errorf("error")).Times(1)

					clusterParams := getDefaultClusterCreateParams()
					clusterParams.OlmOperators = []*models.OperatorCreateParams{
						{Name: newOperatorName},
					}
					reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
						NewClusterParams: clusterParams,
					})
					Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
				})

				It("should return error when both cnv and lvm operator enabled", func() {
					mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
					mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
					mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
					mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
						DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
							return operators, nil
						}).Times(1)
					mockLVMGetOperatorByName("lvm")
					mockCNVGetOperatorByName("cnv")

					clusterParams := getDefaultClusterCreateParams()
					clusterParams.OlmOperators = []*models.OperatorCreateParams{
						{Name: "lvm"},
						{Name: "cnv"},
					}
					mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(
						errors.New("Currently, you can not install OpenShift Data Foundation Logical Volume Manager operator at the same time as Virtualization operator"))
					reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
						NewClusterParams: clusterParams,
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Currently, you can not install OpenShift Data Foundation Logical Volume Manager operator at the same time as Virtualization operator")
				})

				It("enable cnv and lvm after 4.12 is allowed", func() {
					mockClusterRegisterSuccess(true)
					mockCNVGetOperatorByName("cnv")
					mockCNVGetOperatorByName("lvm")
					mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
						DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
							return operators, nil
						}).Times(1)

					clusterParams := getDefaultClusterCreateParams()
					clusterParams.OpenshiftVersion = swag.String("4.12.0-ec.3")
					clusterParams.OlmOperators = []*models.OperatorCreateParams{
						{Name: "cnv"},
						{Name: "lvm"},
					}
					mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
						NewClusterParams: clusterParams,
					})

					foundExpected := false
					for _, monitoredOperator := range reply.(*installer.V2RegisterClusterCreated).Payload.MonitoredOperators {
						if monitoredOperator.Name == "cnv" {
							foundExpected = true
						}
					}
					Expect(foundExpected).Should(BeTrue())
				})

				It("return cnv when cnv operator enabled", func() {
					mockClusterRegisterSuccess(true)
					mockCNVGetOperatorByName("cnv")
					mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
						DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
							return operators, nil
						}).Times(1)

					clusterParams := getDefaultClusterCreateParams()
					clusterParams.OlmOperators = []*models.OperatorCreateParams{
						{Name: "cnv"},
					}
					mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
						NewClusterParams: clusterParams,
					})

					foundExpected := false
					for _, monitoredOperator := range reply.(*installer.V2RegisterClusterCreated).Payload.MonitoredOperators {
						if monitoredOperator.Name == "cnv" {
							foundExpected = true
						}
					}
					Expect(foundExpected).Should(BeTrue())
				})

				It("return LVM when LVM operator enabled", func() {
					mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.12")
					mockLVMGetOperatorByName("lvm")
					mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
						DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
							return operators, nil
						}).Times(1)

					clusterParams := getDefaultClusterCreateParams()
					clusterParams.Platform = &models.Platform{
						Type: common.PlatformTypePtr(models.PlatformTypeNone),
					}
					clusterParams.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
					clusterParams.OlmOperators = []*models.OperatorCreateParams{
						{Name: "lvm"},
					}
					clusterParams.OpenshiftVersion = swag.String("4.12")

					mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
						NewClusterParams: clusterParams,
					})

					foundExpected := false
					for _, monitoredOperator := range reply.(*installer.V2RegisterClusterCreated).Payload.MonitoredOperators {
						if monitoredOperator.Name == "lvm" {
							foundExpected = true
						}
					}
					Expect(foundExpected).Should(BeTrue())
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
						name:              "No change",
						originalOperators: []*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator},
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
						cluster := &common.Cluster{Cluster: models.Cluster{
							ID:                 &clusterID,
							MonitoredOperators: test.originalOperators,
							OpenshiftVersion:   common.TestDefaultConfig.OpenShiftVersion,
							Platform: &models.Platform{
								Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
							},
							CPUArchitecture: common.DefaultCPUArchitecture,
						}}
						err := db.Create(cluster).Error
						Expect(err).ShouldNot(HaveOccurred())

						// Update
						mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
						mockSuccess()

						for _, updateOperator := range test.updateOperators {
							mockGetOperatorByName(updateOperator.Name)
						}
						if test.updateOperators != nil {
							mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
								DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
									return operators, nil
								}).Times(1)
						}

						if test.updateOperators != nil {
							mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
						}

						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								OlmOperators: test.updateOperators,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
						actual := reply.(*installer.V2UpdateClusterCreated)
						Expect(actual.Payload.MonitoredOperators).To(HaveLen(len(test.expectedOperators)))
						Expect(equivalentMonitoredOperators(actual.Payload.MonitoredOperators, test.expectedOperators)).To(BeTrue())
					})
				}
			})

			Context("Ensure CNV or LVM enabled", func() {
				It("Replace CNV with LVM", func() {
					// Setup
					clusterID = strfmt.UUID(uuid.New().String())
					originalOperators := []*models.MonitoredOperator{
						&common.TestDefaultConfig.MonitoredOperator,
						{
							Name:           "cnv",
							TimeoutSeconds: 10,
							Properties:     "",
						},
					}
					cluster := &common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterID,
						MonitoredOperators:    originalOperators,
						OpenshiftVersion:      "4.12",
						HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
						UserManagedNetworking: swag.Bool(true),
						Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						CPUArchitecture:       common.DefaultCPUArchitecture,
					}}
					err := db.Create(cluster).Error
					Expect(err).ShouldNot(HaveOccurred())

					// Update
					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
					mockSuccess()
					mockLVMGetOperatorByName("lvm")
					mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
						DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
							return append(operators, testOLMOperators[0]), nil
						}).Times(1)

					mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							OlmOperators: []*models.OperatorCreateParams{
								{Name: "lvm", Properties: ""},
							},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				})

				It("Enable CNV then LVM", func() {
					// Setup
					clusterID = strfmt.UUID(uuid.New().String())
					originalOperators := []*models.MonitoredOperator{
						&common.TestDefaultConfig.MonitoredOperator,
						{
							Name:           "cnv",
							TimeoutSeconds: 10,
							Properties:     "",
						},
					}
					cluster := &common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterID,
						MonitoredOperators:    originalOperators,
						OpenshiftVersion:      "4.11",
						UserManagedNetworking: swag.Bool(true),
						Platform: &models.Platform{
							Type: common.PlatformTypePtr(models.PlatformTypeNone),
						},
						CPUArchitecture: common.DefaultCPUArchitecture,
					}}
					err := db.Create(cluster).Error
					Expect(err).ShouldNot(HaveOccurred())

					// Update
					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
					mockCNVGetOperatorByName("cnv")
					mockLVMGetOperatorByName("lvm")
					mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
						DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
							return append(operators, testOLMOperators[0]), nil
						}).Times(1)

					mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(
						errors.New("Currently, you can not install OpenShift Data Foundation Logical Volume Manager operator at the same time as Virtualization operator"))
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							OlmOperators: []*models.OperatorCreateParams{
								{Name: "cnv", Properties: ""},
								{Name: "lvm", Properties: ""},
							},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Currently, you can not install OpenShift Data Foundation Logical Volume Manager operator at the same time as Virtualization operator")
				})
			})

			Context("UpdateCluster - Multiple-VIPs Support ", func() {

				var cluster *common.Cluster

				BeforeEach(func() {
					Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
					db, dbName = common.PrepareTestDB()
					bm = createInventory(db, cfg)
					bm.ocmClient = nil
					mockUsageReports()

					clusterID = strfmt.UUID(uuid.New().String())
					infraEnvID = strfmt.UUID(uuid.New().String())

					cluster = &common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterID,
						OpenshiftVersion:      "4.12.0",
						Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
						UserManagedNetworking: swag.Bool(false),
						CPUArchitecture:       common.X86CPUArchitecture,
						MachineNetworks:       []*models.MachineNetwork{{Cidr: "1.3.4.0/24"}},
					}}
					err := db.Create(cluster).Error
					Expect(err).ShouldNot(HaveOccurred())
					addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
					addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
					addHost(masterHostId3, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname2", "bootMode", "1.2.3.6/24", "7.8.9.10/24"), db)
				})

				AfterEach(func() {
					ctrl.Finish()
					common.DeleteTestDB(db, dbName)
				})

				It("Test VIPs cleanup", func() {
					mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 2)
					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(2)
					mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).Times(2)
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(2)
					mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(6)
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(6)
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(6)

					apiVip := "1.2.3.100"
					ingressVip := "1.2.3.101"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips: []*models.IngressVip{{IP: models.IP(ingressVip)}},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					cluster = &common.Cluster{Cluster: *reply.(*installer.V2UpdateClusterCreated).Payload}
					Expect(string(cluster.APIVips[0].IP)).To(Equal(apiVip))
					Expect(network.GetApiVipById(cluster, 0)).To(Equal(apiVip))
					Expect(string(cluster.IngressVips[0].IP)).To(Equal(ingressVip))
					Expect(network.GetIngressVipById(cluster, 0)).To(Equal(ingressVip))

					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     []*models.APIVip{},
							IngressVips: []*models.IngressVip{},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					cluster = &common.Cluster{Cluster: *reply.(*installer.V2UpdateClusterCreated).Payload}
					Expect(len(cluster.APIVips)).To(BeZero())
					Expect(len(cluster.IngressVips)).To(BeZero())
				})

				It("Two APIVips and Two ingressVips - both IPv4 - negative", func() {
					apiVip := "1.2.3.100"
					ingressVip := "1.2.3.101"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("8.8.8.8")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("8.8.8.2")}}
					err := "the second element of apiVIPs must be an IPv6 address. got: 8.8.8.8"

					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, err)
				})

				It("Two APIVips and Two ingressVips - IPv4 first and IPv6 second - positive", func() {
					mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
					mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).Times(1)
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
					mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(3)
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(3)
					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					apiVip := "1.2.3.100"
					ingressVip := "1.2.3.101"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::1")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::2")}}

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					cluster := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(cluster.APIVips).To(Equal(apiVips))
					Expect(cluster.IngressVips).To(Equal(ingressVips))
				})

				It("Two APIVips and Two ingressVips - IPv6 first and IPv4 second - negative", func() {
					apiVip := "2001:db8::1"
					ingressVip := "2001:db8::2"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("8.8.8.7")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("8.8.8.1")}}
					err := "the first element of apiVIPs must be an IPv4 address. got: 2001:db8::1"

					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, err)
				})

				It("Two APIVips and Two ingressVips - IPv6 - negative", func() {
					apiVip := "2001:db8::1"
					ingressVip := "2001:db8::2"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}}
					err := "the first element of apiVIPs must be an IPv4 address. got: 2001:db8::1"

					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, err)
				})

				It("More than two APIVips and More than two ingressVips -  negative", func() {
					apiVip := "1.2.3.100"
					ingressVip := "1.2.3.101"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}, {IP: models.IP("8.8.8.3")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}, {IP: models.IP("8.8.8.4")}}
					err := "apiVIPs supports 2 vips. got: 3"

					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, err)
				})

				It("Non parsable APIVips and non parsable ingressVips - negative", func() {
					apiVip := "invalid apiVip"
					ingressVip := "invalid ingressVip"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("invalid apiVip 2")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("invalid ingressVip 2")}}
					multiErr := "4 errors occurred:\n\t* Could not parse VIP ip invalid apiVip\n\t* Could not parse VIP ip invalid apiVip 2\n\t* Could not parse VIP ip invalid ingressVip\n\t* Could not parse VIP ip invalid ingressVip 2"

					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, multiErr)
				})

				It("Non parsable APIVips and parsable ingressVips - negative", func() {
					apiVip := "invalid apiVip"
					ingressVip := "1.2.3.101"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::4")}}
					multiErr := "1 error occurred:\n\t* Could not parse VIP ip invalid apiVip\n"

					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, multiErr)
				})

				It("Different number of APIVips and ingressVips - negative", func() {
					apiVip := "1.2.3.100"
					ingressVip := "1.2.3.101"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}}
					err := "configuration must include the same number of apiVIPs (got 2) and ingressVIPs (got 1)"

					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, err)
				})

				It("Duplicated addresses in APIVips - negative", func() {
					apiVip := "1.2.3.100"
					ingressVip := "1.2.3.101"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP(apiVip)}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::3")}}
					err := "The IP address \"1.2.3.100\" appears multiple times in apiVIPs"

					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, err)
				})

				It("Duplicated addresses in ingressVips - negative", func() {
					apiVip := "1.2.3.100"
					ingressVip := "1.2.3.101"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP(ingressVip)}}
					err := "The IP address \"1.2.3.101\" appears multiple times in ingressVIPs"

					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, err)
				})

				It("Duplicated address across APIVips and ingressVips - negative", func() {
					apiVip := "1.2.3.100"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::3")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::4")}}
					err := "The IP address \"1.2.3.100\" appears both in apiVIPs and ingressVIPs"

					mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Name:        swag.String("some-cluster-name"),
							PullSecret:  swag.String(fakePullSecret),
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, err)
				})

			})

			It("Resolve OLM dependencies", func() {
				clusterID = strfmt.UUID(uuid.New().String())
				cluster := &common.Cluster{Cluster: models.Cluster{
					ID:               &clusterID,
					OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
					Platform: &models.Platform{
						Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
					},
					CPUArchitecture: common.DefaultCPUArchitecture,
				}}
				err := db.Create(cluster).Error
				Expect(err).ShouldNot(HaveOccurred())

				// Update
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				mockSuccess()

				newOperatorName := testOLMOperators[1].Name

				mockGetOperatorByName(newOperatorName)
				mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
					DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
						return append(operators, testOLMOperators[0]), nil
					}).Times(1)

				mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						OlmOperators: []*models.OperatorCreateParams{
							{Name: newOperatorName},
						},
					},
				})

				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				actual := reply.(*installer.V2UpdateClusterCreated)

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

				for _, m := range []*models.MonitoredOperator{
					&expectedUpdatedMonitoredOperator,
					&expectedResolvedMonitoredOperator,
				} {
					Expect(containsMonitoredOperator(actual.Payload.MonitoredOperators, m)).To(BeTrue())
				}
			})

			It("OLM invalid name", func() {
				clusterID = strfmt.UUID(uuid.New().String())
				cluster := &common.Cluster{Cluster: models.Cluster{
					ID:               &clusterID,
					OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
					Platform: &models.Platform{
						Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
					},
					CPUArchitecture: common.DefaultCPUArchitecture,
				}}
				err := db.Create(cluster).Error
				Expect(err).ShouldNot(HaveOccurred())

				newOperatorName := "invalid-name"

				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				mockOperatorManager.EXPECT().GetOperatorByName(newOperatorName).Return(nil, errors.Errorf("error")).Times(1)
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
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
			cluster := &common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: "4.12",
				CPUArchitecture:  common.DefaultCPUArchitecture,
				Platform: &models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
				},
			}}
			err := db.Create(cluster).Error
			Expect(err).ShouldNot(HaveOccurred())
			sshKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDi8KHZYGyPQjECHwytquI3rmpgoUn6M+lkeOD2nEKvYElLE5mPIeqF0izJIl56u" +
				"ar2wda+3z107M9QkatE+dP4S9/Ltrlm+/ktAf4O6UoxNLUzv/TGHasb9g3Xkt8JTkohVzVK36622Sd8kLzEc61v1AonLWIADtpwq6/GvH" +
				"MAuPK2R/H0rdKhTokylKZLDdTqQ+KUFelI6RNIaUBjtVrwkx1j0htxN11DjBVuUyPT2O1ejWegtrM0T+4vXGEA3g3YfbT2k0YnEzjXXqng" +
				"qbXCYEJCZidp3pJLH/ilo4Y4BId/bx/bhzcbkZPeKlLwjR8g9sydce39bzPIQj+b7nlFv1Vot/77VNwkjXjYPUdUPu0d1PkFD9jKDOdB3f" +
				"AC61aG2a/8PFS08iBrKiMa48kn+hKXC4G4D5gj/QzIAgzWSl2tEzGQSoIVTucwOAL/jox2dmAa0RyKsnsHORppanuW4qD7KAcmas1GHrAq" +
				"IfNyDiU2JR50r1jCxj5H76QxIuM= root@ocp-edge34.lab.eng.tlv2.redhat.com"
			sshKeyWithNewLine := sshKey + " \n"

			mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
			mockSuccess()
			reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					SSHPublicKey: &sshKeyWithNewLine,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
			err = db.First(&cluster, "id = ?", clusterID).Error
			Expect(err).ShouldNot(HaveOccurred())
			Expect(cluster.SSHPublicKey).Should(Equal(sshKey))
		})

		It("empty pull-secret", func() {
			pullSecret := ""
			reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					PullSecret: &pullSecret,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
		})

		It("Update SchedulableMasters", func() {
			clusterID = strfmt.UUID(uuid.New().String())
			cluster := &common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: "4.12",
				CPUArchitecture:  common.DefaultCPUArchitecture,
				Platform: &models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
				},
			}}
			err := db.Create(cluster).Error
			Expect(err).ShouldNot(HaveOccurred())
			mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
			mockSuccess()
			reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					SchedulableMasters: swag.Bool(true),
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
			actual := reply.(*installer.V2UpdateClusterCreated)
			Expect(actual.Payload.SchedulableMasters).To(Equal(swag.Bool(true)))
		})

		Context("Set and Update Cluster Proxy", func() {

			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				cluster := &common.Cluster{
					Cluster: models.Cluster{
						ID:               &clusterID,
						OpenshiftVersion: "4.8.0-fc.4",
						HTTPProxy:        "http://proxy.proxy",
						HTTPSProxy:       "https://proxy.proxy",
						NoProxy:          "*",
						Platform: &models.Platform{
							Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
						},
						CPUArchitecture: common.DefaultCPUArchitecture,
					}}
				err := db.Create(cluster).Error
				Expect(err).ShouldNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				mockClusterUpdateSuccess(1, 0)
			})

			updateCluster := func(httpProxy, httpsProxy, noProxy string) *common.Cluster {
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						HTTPProxy:  &httpProxy,
						HTTPSProxy: &httpsProxy,
						NoProxy:    &noProxy,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				var cluster common.Cluster
				err := db.First(&cluster, "id = ?", clusterID).Error
				Expect(err).ShouldNot(HaveOccurred())
				return &cluster
			}

			It("set a valid proxy", func() {
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ProxySettingsChangedEventName),
					eventstest.WithClusterIdMatcher(clusterID.String())))
				_ = updateCluster("http://proxy.proxy", "", "proxy.proxy")
			})

			It("set a valid noProxy wildcard", func() {
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ProxySettingsChangedEventName),
					eventstest.WithClusterIdMatcher(clusterID.String())))
				_ = updateCluster("", "", "*")
			})

			It("set a valid noProxy wildcard comma-delimited", func() {
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ProxySettingsChangedEventName),
					eventstest.WithClusterIdMatcher(clusterID.String())))
				_ = updateCluster("", "", "*,example.com")
			})
		})

		Context("Day2 api vip dnsname/ip", func() {
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				cluster := &common.Cluster{Cluster: models.Cluster{
					ID:   &clusterID,
					Kind: swag.String(models.ClusterKindAddHostsCluster),
					Platform: &models.Platform{
						Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
					},
					OpenshiftVersion: "4.12",
					CPUArchitecture:  common.DefaultCPUArchitecture,
				}}
				err := db.Create(cluster).Error
				Expect(err).ShouldNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
			})
			It("update api vip dnsname success", func() {
				mockSuccess()
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						APIVipDNSName: swag.String("some dns name"),
					}})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
			})
		})

		Context("Day2 update hostname", func() {
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				infraEnvID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Cluster{Cluster: models.Cluster{
					ID:   &clusterID,
					Kind: swag.String(models.ClusterKindAddHostsCluster),
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindAddToExistingClusterHost, infraEnvID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
				addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindAddToExistingClusterHost, infraEnvID, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)

				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
				mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			})
			It("update hostname, all in known", func() {
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

				resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
					InfraEnvID: infraEnvID,
					HostID:     masterHostId1,
					HostUpdateParams: &models.HostUpdateParams{
						HostName: swag.String("a.b.c"),
					},
				})
				Expect(resp).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
			})
			It("update hostname, one host in installing", func() {
				addHost(masterHostId3, models.HostRoleMaster, "added-to-existing-cluster", models.HostKindAddToExistingClusterHost, infraEnvID, clusterID, getInventoryStr("hostname3", "bootMode", "1.2.3.6/24", "10.11.50.70/16"), db)
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)

				resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
					InfraEnvID: infraEnvID,
					HostID:     masterHostId1,
					HostUpdateParams: &models.HostUpdateParams{
						HostName: swag.String("a.b.c"),
					},
				})
				Expect(resp).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
			})
		})

		Context("Update Cluster Tags", func() {
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				cluster := &common.Cluster{Cluster: models.Cluster{
					ID:   &clusterID,
					Kind: swag.String(models.ClusterKindAddHostsCluster),
					Platform: &models.Platform{
						Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
					},
					CPUArchitecture: common.DefaultCPUArchitecture,
				}}
				err := db.Create(cluster).Error
				Expect(err).ShouldNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
			})

			It("Update tags success", func() {
				mockSuccess()
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						Tags: swag.String("tag1,tag2"),
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				actual := reply.(*installer.V2UpdateClusterCreated)
				Expect(actual.Payload.Tags).To(Equal("tag1,tag2"))
			})

			It("Update cluster with invalid tags", func() {
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						Tags: swag.String("tag,,"),
					},
				})
				Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
				verifyApiErrorString(reply, http.StatusBadRequest, "Invalid format for Tags")
			})
		})

		Context("Update Network", func() {
			var cluster *common.Cluster
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				infraEnvID = strfmt.UUID(uuid.New().String())
				cluster = &common.Cluster{Cluster: models.Cluster{
					ID:                    &clusterID,
					OpenshiftVersion:      common.TestDefaultConfig.OpenShiftVersion,
					Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
					UserManagedNetworking: swag.Bool(false),
					CPUArchitecture:       common.X86CPUArchitecture,
					HighAvailabilityMode:  swag.String(models.ClusterCreateParamsHighAvailabilityModeFull),
				}}
				err := db.Create(cluster).Error
				Expect(err).ShouldNot(HaveOccurred())
				addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
				addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
				addHost(masterHostId3, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname2", "bootMode", "1.2.3.6/24", "7.8.9.10/24"), db)
				err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: &clusterID}).UpdateColumn("free_addresses",
					makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16"))).Error
				Expect(err).ToNot(HaveOccurred())
			})

			mockSuccess := func(times int) {
				mockClusterUpdateSuccess(times, 3)
			}

			mockClusterUpdatability := func(times int) {
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(times)
			}

			mockClusterUpdatabilityAnyTimes := func() {
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).AnyTimes()
			}

			Context("Single node", func() {
				BeforeEach(func() {
					mockClusterUpdatability(1)
					Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Updates(map[string]interface{}{
						"user_managed_networking": true,
						"platform_type":           models.PlatformTypeNone,
						"high_availability_mode":  models.ClusterHighAvailabilityModeNone,
						"cpu_architecture":        common.X86CPUArchitecture,
						"openshift_version":       "4.10",
					}).Error).ShouldNot(HaveOccurred())
				})

				It("Fail to unset UserManagedNetworking", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
						},
					})

					verifyApiErrorString(reply, http.StatusBadRequest, "disabling User Managed Networking or setting platform different than none or oci platforms is not allowed in single node Openshift")
				})

				It("Set Machine CIDR", func() {
					err := network.UpdateVipsTables(db,
						&common.Cluster{Cluster: models.Cluster{
							ID:          &clusterID,
							APIVips:     common.TestIPv4Networking.APIVips,
							IngressVips: common.TestIPv4Networking.IngressVips,
						}},
						true,
						true,
					)
					Expect(err).ShouldNot(HaveOccurred())

					mockSuccess(1)

					machineNetworks := common.TestIPv4Networking.MachineNetworks
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							MachineNetworks: machineNetworks,
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
					Expect(len(actual.Payload.APIVips)).To(BeZero())
					Expect(len(actual.Payload.IngressVips)).To(BeZero())
					validateNetworkConfiguration(actual.Payload, nil, nil, &machineNetworks)
				})

				It("Fail with bad Machine CIDR", func() {
					badMachineCidr := "2.2.3.128/24"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							MachineNetworks: []*models.MachineNetwork{{Cidr: models.Subnet(badMachineCidr)}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("%s is not a valid network CIDR", badMachineCidr))
				})

				It("Success with machine cidr that is not part of cluster networks", func() {
					mockSuccess(1)
					wrongMachineCidrNetworks := []*models.MachineNetwork{{Cidr: models.Subnet("2.2.3.0/24")}}

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							MachineNetworks: wrongMachineCidrNetworks,
						},
					})

					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
					Expect(len(actual.Payload.APIVips)).To(BeZero())
					Expect(len(actual.Payload.IngressVips)).To(BeZero())
					validateNetworkConfiguration(actual.Payload, nil, nil, &wrongMachineCidrNetworks)
				})
			})

			Context("UserManagedNetworking", func() {

				It("handle UserManagedNetworking and VIP arrays update at the same request", func() {
					mockClusterUpdatability(3)
					mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 3)
					mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
					mockProviderRegistry.EXPECT().SetPlatformUsages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

					apiVip := "10.11.12.15"
					ingressVip := "10.11.12.16"

					By("Set User Managed Networking: false + Add APIVip and IngressVip")
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
							APIVips:               []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:           []*models.IngressVip{{IP: models.IP(ingressVip)}},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("Set UserManagedNetworking true + Remove APIVip and IngressVip")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							APIVips:               []*models.APIVip{},
							IngressVips:           []*models.IngressVip{},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("Set User Managed Networking: false + Add APIVip and IngressVip again")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
							APIVips:               []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:           []*models.IngressVip{{IP: models.IP(ingressVip)}},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				})

				It("Fail to set cluster VIPs when UserManagedNetworking true was set", func() {
					mockClusterUpdatability(3)
					mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 2)
					mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
					mockProviderRegistry.EXPECT().SetPlatformUsages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

					By("Set User Managed Networking: false")
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("Fail to set UserManagedNetworking true")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("Set API VIP and Ingress VIP")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     []*models.APIVip{{IP: "10.11.12.15"}},
							IngressVips: []*models.IngressVip{{IP: "10.11.12.16"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "API VIPs cannot be set with User Managed Networking")
				})

				It("Fail to set UserManagedNetworking true when cluster VIPs were set", func() {
					mockClusterUpdatability(2)
					mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 2)
					mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

					By("Set User Managed Networking: false")
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("Set API VIP and Ingress VIP")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     []*models.APIVip{{IP: "10.11.12.15"}},
							IngressVips: []*models.IngressVip{{IP: "10.11.12.16"}},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("Fail to set UserManagedNetworking true")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "User Managed Networking cannot be set with API VIP")
				})

				It("Update both UserManagedNetworking and VipDhcpAllocation at the same update", func() {
					mockClusterUpdatability(4)
					mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 4)
					mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					By("Set User Managed Networking: false")
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("Set API VIP and Ingress VIP")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     []*models.APIVip{{IP: "10.11.12.15"}},
							IngressVips: []*models.IngressVip{{IP: "10.11.12.16"}},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("set UserManagedNetworking: false, and VipDhcpAllocation: true")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
							VipDhcpAllocation:     swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("set UserManagedNetworking: true, and VipDhcpAllocation: false")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							VipDhcpAllocation:     swag.Bool(false),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				})

				It("Test that DHCP VIPs were cleared when switching to UserManagedNetworking: true", func() {
					mockClusterUpdatability(2)
					mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 2)
					mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					apiVip := "10.11.12.15"
					ingressVip := "10.11.12.16"

					By("Set User Managed Networking: false and VipDhcpAllocation: true")
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
							VipDhcpAllocation:     swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("Update VIPS in DB cluster to mimic DHCP allocation")
					cluster, err := common.GetClusterFromDB(db, clusterID, common.SkipEagerLoading)
					Expect(err).ToNot(HaveOccurred())
					cluster.APIVips = []*models.APIVip{{IP: models.IP(apiVip)}}
					cluster.IngressVips = []*models.IngressVip{{IP: models.IP(ingressVip)}}
					db.Save(&cluster)

					By("Verify VIPs updates")
					replay := bm.V2GetCluster(ctx, installer.V2GetClusterParams{ClusterID: clusterID}).(*installer.V2GetClusterOK)
					Expect(len(replay.Payload.APIVips)).Should(Equal(1))
					Expect(len(replay.Payload.IngressVips)).Should(Equal(1))

					By("Set User Managed Networking: true and VipDhcpAllocation: false")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							VipDhcpAllocation:     swag.Bool(false),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					By("Verify that DHCP VIPs were cleared")
					replay = bm.V2GetCluster(ctx, installer.V2GetClusterParams{ClusterID: clusterID}).(*installer.V2GetClusterOK)
					Expect(len(replay.Payload.APIVips)).Should(BeZero())
					Expect(len(replay.Payload.IngressVips)).Should(BeZero())
				})

				It("success", func() {
					mockClusterUpdatability(1)
					mockSuccess(1)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
					Expect(*actual.Payload.Platform.Type).To(Equal(models.PlatformTypeNone))
				})

				It("Unset non relevant parameters", func() {
					mockClusterUpdatability(1)
					mockSuccess(1)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					Expect(db.Save(
						&models.MachineNetwork{Cidr: common.TestIPv4Networking.MachineNetworks[0].Cidr, ClusterID: clusterID}).Error).ShouldNot(HaveOccurred())

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(true)))
					Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
					Expect(len(actual.Payload.APIVips)).Should(BeZero())
					Expect(len(actual.Payload.IngressVips)).Should(BeZero())
					validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{})
				})

				It("Fail with DHCP", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							VipDhcpAllocation:     swag.Bool(true),
						},
					})

					verifyApiErrorString(reply, http.StatusBadRequest, "User Managed Networking cannot be set with VIP DHCP Allocation")
				})

				It("Fail with DHCP when UserManagedNetworking was set", func() {
					mockClusterUpdatability(1)
					Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update("user_managed_networking", true).Error).ShouldNot(HaveOccurred())
					Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update("platform_type", models.PlatformTypeNone).Error).ShouldNot(HaveOccurred())

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							VipDhcpAllocation: swag.Bool(true),
						},
					})

					verifyFeatureSupportApiErrorString(reply, http.StatusBadRequest, models.FeatureSupportLevelIDNONEPLATFORM, models.FeatureSupportLevelIDVIPAUTOALLOC)
				})

				It("Fail with Ingress VIP", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							APIVips:               []*models.APIVip{{IP: "10.35.20.9"}},
							IngressVips:           []*models.IngressVip{{IP: "10.35.20.10"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "User Managed Networking cannot be set with API VIP")
				})

				It("Fail with API and Ingress VIPs", func() {
					By("Fail API VIP with User Managed Networking")
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							APIVips:               []*models.APIVip{{IP: "10.35.20.10"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "User Managed Networking cannot be set with API VIP")

					By("Fail Ingress VIP with no Ingress VIP")
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							IngressVips:           []*models.IngressVip{{IP: "10.35.20.10"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "User Managed Networking cannot be set with Ingress VIP")
				})

				It("Don't fail with Machine CIDR", func() {
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)
					mockSuccess(1)
					mockClusterUpdatability(1)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							MachineNetworks:       []*models.MachineNetwork{{Cidr: "10.11.0.0/16"}},
						},
					})

					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				})

				It("Fail with non-x86_64 CPU architecture with 4.10", func() {
					mockClusterUpdatability(1)
					clusterID = strfmt.UUID(uuid.New().String())
					err := db.Create(&common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterID,
						CPUArchitecture:       common.ARM64CPUArchitecture,
						UserManagedNetworking: swag.Bool(true),
						Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeFull),
						OpenshiftVersion:      "4.10",
					}}).Error
					Expect(err).ShouldNot(HaveOccurred())

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
						},
					})

					verifyApiErrorString(reply, http.StatusBadRequest, "disabling User Managed Networking or setting Bare-Metal platform is not allowed for clusters with non-x86_64 CPU architecture")
				})

				It("Success with non-x86_64 CPU architecture in case override is supported - 4.11", func() {
					mockClusterUpdatability(1)
					mockClusterUpdateSuccess(1, 0)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any(), mockUsage)
					clusterID = strfmt.UUID(uuid.New().String())
					err := db.Create(&common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterID,
						CPUArchitecture:       common.ARM64CPUArchitecture,
						OpenshiftVersion:      "4.11",
						UserManagedNetworking: swag.Bool(true),
						Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeFull),
					}}).Error
					Expect(err).ShouldNot(HaveOccurred())
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
						},
					})

					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				})
			})

			It("NetworkType", func() {
				mockClusterUpdatability(1)
				mockSuccess(1)
				networkType := "OpenShiftSDN"
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						NetworkType: &networkType,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				actual := reply.(*installer.V2UpdateClusterCreated)
				Expect(actual.Payload.NetworkType).To(Equal(&networkType))
			})

			Context("Non DHCP", func() {

				It("No machine network", func() {
					mockClusterUpdatability(1)
					apiVip := "8.8.8.8"
					ingressVip := "1.2.3.4"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips: []*models.IngressVip{{IP: models.IP(ingressVip)}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						fmt.Sprintf("Calculate machine network CIDR: No suitable matching CIDR found for VIP %s", apiVip))
				})
				It("Api and ingress mismatch", func() {
					mockClusterUpdatability(1)
					apiVip := "10.11.12.15"
					ingressVip := "1.2.3.20"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips: []*models.IngressVip{{IP: models.IP(ingressVip)}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						"ingress-vip <1.2.3.20> does not belong to machine-network-cidr <10.11.0.0/16>")
				})
				It("Same api and ingress", func() {
					apiVip := "10.11.12.15"
					ingressVip := apiVip
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips: []*models.IngressVip{{IP: models.IP(ingressVip)}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("The IP address \"%s\" appears both in apiVIPs and ingressVIPs", apiVip))
				})

				It("Bad apiVip ip", func() {
					apiVip := "not.an.ip.test"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips: []*models.APIVip{{IP: models.IP(apiVip)}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("Could not parse VIP ip %s", apiVip))
				})

				It("Bad ingressVip ip", func() {
					ingressVip := "not.an.ip.test"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							IngressVips: []*models.IngressVip{{IP: models.IP(ingressVip)}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("Could not parse VIP ip %s", ingressVip))
				})

				It("Update success", func() {
					mockSuccess(1)
					mockClusterUpdatability(1)

					apiVip := "10.11.12.15"
					ingressVip := "10.11.12.16"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips: []*models.IngressVip{{IP: models.IP(ingressVip)}},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(string(actual.Payload.APIVips[0].IP)).To(Equal(apiVip))
					Expect(string(actual.Payload.IngressVips[0].IP)).To(Equal(ingressVip))
					validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{{Cidr: "10.11.0.0/16"}})
					validateHostsRequestedHostname(actual.Payload)
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
					mockClusterUpdatability(1)
					apiVip := "10.11.12.15"
					ingressVip := "10.11.12.16"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
							MachineNetworks: []*models.MachineNetwork{{Cidr: "10.11.0.0/16"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						"Setting Machine network CIDR is forbidden when cluster is not in vip-dhcp-allocation mode")
				})

				It("Machine network CIDR in non dhcp for dual-stack", func() {
					mockClusterUpdatability(1)
					mockSuccess(1)

					apiVip := "10.11.12.15"
					ingressVip := "10.11.12.16"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: "10.128.0.0/14", HostPrefix: 23}, {Cidr: "fd01::/48", HostPrefix: 64}},
							MachineNetworks: []*models.MachineNetwork{{Cidr: "10.11.0.0/16"}, {Cidr: "fd2e:6f44:5dd8:c956::/120"}},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(len(actual.Payload.MachineNetworks)).To(Equal(2))
				})
				It("Wrong order of machine network CIDRs in non dhcp for dual-stack", func() {
					mockClusterUpdatability(1)
					mockSuccess(1)

					apiVip := "10.11.12.15"
					ingressVip := "10.11.12.16"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: "10.128.0.0/14", HostPrefix: 23}, {Cidr: "fd01::/48", HostPrefix: 64}},
							MachineNetworks: []*models.MachineNetwork{{Cidr: "10.11.0.0/16"}, {Cidr: "fd2e:6f44:5dd8:c956::/120"}},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))

					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
							MachineNetworks: []*models.MachineNetwork{{Cidr: "fd2e:6f44:5dd8:c956::/120"}, {Cidr: "10.12.0.0/16"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "First machine network has to be IPv4 subnet")
				})
				It("API VIP in wrong subnet for dual-stack", func() {
					apiVip := "10.11.12.15"
					ingressVip := "10.11.12.16"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: "10.128.0.0/14", HostPrefix: 23}, {Cidr: "fd01::/48", HostPrefix: 64}},
							MachineNetworks: []*models.MachineNetwork{{Cidr: "10.12.0.0/16"}, {Cidr: "fd2e:6f44:5dd8:c956::/120"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "api-vip <10.11.12.15> does not belong to machine-network-cidr <10.12.0.0/16>")
				})
			})

			Context("Advanced networking validations", func() {

				var (
					apiVip     = "10.11.12.15"
					ingressVip = "10.11.12.16"
				)

				BeforeEach(func() {
					mockClusterUpdatability(1)
				})

				It("Update success", func() {
					mockSuccess(1)

					clusterNetworks := []*models.ClusterNetwork{{Cidr: "192.168.0.0/21", HostPrefix: 23}}
					serviceNetworks := []*models.ServiceNetwork{{Cidr: "193.168.5.0/24"}}

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
							ClusterNetworks: clusterNetworks,
							ServiceNetworks: serviceNetworks,
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(string(actual.Payload.APIVips[0].IP)).To(Equal(apiVip))
					Expect(string(actual.Payload.IngressVips[0].IP)).To(Equal(ingressVip))

					validateNetworkConfiguration(actual.Payload,
						&clusterNetworks, &serviceNetworks, &[]*models.MachineNetwork{{Cidr: "10.11.0.0/16"}})
					validateHostsRequestedHostname(actual.Payload)

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
						Expect(db.Save(
							&models.ClusterNetwork{ClusterID: clusterID, Cidr: cidr, HostPrefix: 20}).Error).ShouldNot(HaveOccurred())
						Expect(db.Save(
							&models.ServiceNetwork{ClusterID: clusterID, Cidr: cidr}).Error).ShouldNot(HaveOccurred())
						Expect(db.Save(
							&models.MachineNetwork{ClusterID: clusterID, Cidr: cidr}).Error).ShouldNot(HaveOccurred())

						mockSuccess(1)
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								UserManagedNetworking: swag.Bool(false),
							},
						})

						Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					})
					It("part of update", func() {
						Expect(db.Save(
							&models.ClusterNetwork{ClusterID: clusterID, Cidr: models.Subnet("1.3.0.0/16"), HostPrefix: 20}).Error).ShouldNot(HaveOccurred())
						Expect(db.Save(
							&models.ServiceNetwork{ClusterID: clusterID, Cidr: models.Subnet("1.2.0.0/16")}).Error).ShouldNot(HaveOccurred())
						Expect(db.Save(
							&models.MachineNetwork{ClusterID: clusterID, Cidr: models.Subnet("1.4.0.0/16")}).Error).ShouldNot(HaveOccurred())

						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								ServiceNetworks: []*models.ServiceNetwork{{Cidr: "1.3.5.0/24"}},
							},
						})

						verifyApiErrorString(reply, http.StatusBadRequest, "CIDRS 1.3.5.0/24 and 1.3.0.0/16 overlap")
					})
				})
				It("Prefix out of range", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: "192.168.5.0/24", HostPrefix: 26}},
							ServiceNetworks: []*models.ServiceNetwork{{Cidr: "193.168.4.0/23"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						"Host prefix, now 26, must be less than or equal to 25 to allow at least 128 addresses")
				})
				It("Subnet prefix out of range", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: "192.168.0.0/23", HostPrefix: 25}},
							ServiceNetworks: []*models.ServiceNetwork{{Cidr: "193.168.4.0/27"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						"Service network CIDR 193.168.4.0/27: Address mask size must be between 1 to 25 and must include at least 128 addresses")
				})
				It("Bad subnet", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: "1.168.0.0/23", HostPrefix: 23}},
							ServiceNetworks: []*models.ServiceNetwork{{Cidr: "193.168.4.0/1"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest,
						"Cluster network CIDR prefix 23 does not contain enough addresses for 3 hosts each one with 23 prefix (512 addresses)")
				})
				It("Not enough addresses", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
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

				verifyMachineCIDRTimestampUpdated := func(beforeTimestamp time.Time) {
					cluster, err := common.GetClusterFromDB(db, clusterID, common.SkipEagerLoading)
					Expect(err).ToNot(HaveOccurred())
					ExpectWithOffset(1, beforeTimestamp.After(cluster.MachineNetworkCidrUpdatedAt)).To(BeFalse())
				}

				It("Vips in DHCP", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:           []*models.APIVip{{IP: models.IP(apiVip)}},
							IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip)}},
							VipDhcpAllocation: swag.Bool(true),
							MachineNetworks:   []*models.MachineNetwork{{Cidr: primaryMachineCIDR}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Setting API VIPs is forbidden when cluster is in vip-dhcp-allocation mode")
				})

				It("Fail in DHCP", func() {
					mockClusterApi.EXPECT().DetectAndStoreCollidingIPsForCluster(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterUpdatability(1)
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
					mockClusterApi.EXPECT().SetConnectivityMajorityGroupsForCluster(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

					By("Override machine cidr", func() {
						machineNetworks := common.TestIPv4Networking.MachineNetworks
						beforeTimestamp := time.Now()
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								VipDhcpAllocation: swag.Bool(true),
								MachineNetworks:   machineNetworks,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
						verifyMachineCIDRTimestampUpdated(beforeTimestamp)
						actual := reply.(*installer.V2UpdateClusterCreated)
						Expect(len(actual.Payload.APIVips)).To(BeZero())
						Expect(len(actual.Payload.IngressVips)).To(BeZero())
						validateNetworkConfiguration(actual.Payload, nil, nil, &machineNetworks)
						validateHostsRequestedHostname(actual.Payload)

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
						mockClusterUpdatability(1)
						beforeTimestamp := time.Now()
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								APIVips:           []*models.APIVip{{IP: models.IP(apiVip)}},
								IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip)}},
								VipDhcpAllocation: swag.Bool(false),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
						verifyMachineCIDRTimestampUpdated(beforeTimestamp)
						actual := reply.(*installer.V2UpdateClusterCreated)
						Expect(string(actual.Payload.APIVips[0].IP)).To(Equal(apiVip))
						Expect(string(actual.Payload.IngressVips[0].IP)).To(Equal(ingressVip))
						validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{{Cidr: primaryMachineCIDR}})
						validateHostsRequestedHostname(actual.Payload)

					})
				})
				It("DHCP non existent network (no error)", func() {
					mockSuccess(2)
					mockClusterUpdatability(2)
					machineNetworks := []*models.MachineNetwork{{Cidr: "10.13.0.0/16"}}

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							VipDhcpAllocation: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					beforeTimestamp := time.Now()
					reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							MachineNetworks: machineNetworks,
						},
					})

					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					validateNetworkConfiguration(actual.Payload, nil, nil, &machineNetworks)
					validateHostsRequestedHostname(actual.Payload)
					verifyMachineCIDRTimestampUpdated(beforeTimestamp)
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
						mockClusterUpdatability(1)
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								MachineNetworks:   machineNetworks,
								VipDhcpAllocation: swag.Bool(true),
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "VIP DHCP allocation is unsupported with IPv6 network")
					})

					It("Fail to set IPv6 machine CIDR when VIP DHCP was true", func() {
						mockClusterUpdatability(1)
						Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update("vip_dhcp_allocation", true).Error).ShouldNot(HaveOccurred())

						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								MachineNetworks: machineNetworks,
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "VIP DHCP allocation is unsupported with IPv6 network")
					})

					It("Set VIP DHCP true when machine CIDR was IPv6", func() {
						mockClusterUpdatability(1)
						mockSuccess(1)

						Expect(db.Save(machineNetworks[0]).Error).ShouldNot(HaveOccurred())

						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								VipDhcpAllocation: swag.Bool(true),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
						actual := reply.(*installer.V2UpdateClusterCreated)
						Expect(actual.Payload.VipDhcpAllocation).NotTo(BeNil())
						Expect(*actual.Payload.VipDhcpAllocation).To(BeTrue())
						validateNetworkConfiguration(actual.Payload, nil, nil, &[]*models.MachineNetwork{})
						validateHostsRequestedHostname(actual.Payload)
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
						mockClusterUpdatability(1)
						mockSuccess(1)

						Expect(db.Save(machineNetworks[0]).Error).ShouldNot(HaveOccurred())
						Expect(db.Save(machineNetworks[1]).Error).ShouldNot(HaveOccurred())

						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								MachineNetworks:   machineNetworks,
								VipDhcpAllocation: swag.Bool(true),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
						actual := reply.(*installer.V2UpdateClusterCreated)
						Expect(actual.Payload.VipDhcpAllocation).NotTo(BeNil())
						Expect(*actual.Payload.VipDhcpAllocation).To(BeTrue())
						validateNetworkConfiguration(actual.Payload, nil, nil, &machineNetworks)
						validateHostsRequestedHostname(actual.Payload)
					})
				})
			})

			Context("NTP", func() {

				BeforeEach(func() {
					mockClusterUpdatability(1)
				})

				It("Empty NTP source", func() {
					mockSuccess(1)

					ntpSource := ""
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							AdditionalNtpSource: &ntpSource,
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(actual.Payload.AdditionalNtpSource).To(Equal(ntpSource))
				})

				It("Valid IP NTP source", func() {
					mockSuccess(1)

					ntpSource := "1.1.1.1"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							AdditionalNtpSource: &ntpSource,
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(actual.Payload.AdditionalNtpSource).To(Equal(ntpSource))
				})

				It("Valid Hostname NTP source", func() {
					mockSuccess(1)

					ntpSource := "clock.redhat.com"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							AdditionalNtpSource: &ntpSource,
						},
					})

					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(actual.Payload.AdditionalNtpSource).To(Equal(ntpSource))
				})

				It("Valid comma-separated NTP sources", func() {
					mockSuccess(1)

					ntpSource := "clock.redhat.com,1.1.1.1"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							AdditionalNtpSource: &ntpSource,
						},
					})

					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					Expect(actual.Payload.AdditionalNtpSource).To(Equal(ntpSource))
				})

				It("Invalid NTP source", func() {
					ntpSource := "inject'"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							AdditionalNtpSource: &ntpSource,
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("Invalid NTP source: %s", ntpSource))
				})
			})

			Context("Networks", func() {
				var (
					clusterNetworks = common.TestIPv4Networking.ClusterNetworks
					serviceNetworks = common.TestIPv4Networking.ServiceNetworks
					machineNetworks = common.TestIPv4Networking.MachineNetworks
				)

				setNetworksClusterID := func(clusterID strfmt.UUID,
					clusterNetworks []*models.ClusterNetwork,
					serviceNetworks []*models.ServiceNetwork,
					machineNetworks []*models.MachineNetwork,
				) {
					for _, network := range clusterNetworks {
						network.ClusterID = clusterID
						Expect(db.Save(network).Error).ShouldNot(HaveOccurred())
					}
					for _, network := range serviceNetworks {
						network.ClusterID = clusterID
						Expect(db.Save(network).Error).ShouldNot(HaveOccurred())
					}
					for _, network := range machineNetworks {
						network.ClusterID = clusterID
						Expect(db.Save(network).Error).ShouldNot(HaveOccurred())
					}

					cluster, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
					Expect(err).ToNot(HaveOccurred())
					validateNetworkConfiguration(&cluster.Cluster, &clusterNetworks, &serviceNetworks, &machineNetworks)
				}

				BeforeEach(func() {
					setNetworksClusterID(clusterID, clusterNetworks, serviceNetworks, machineNetworks)
					mockClusterUpdatabilityAnyTimes()
				})

				It("No new networks data", func() {
					mockSuccess(1)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID:           clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)

					validateNetworkConfiguration(actual.Payload, &clusterNetworks, &serviceNetworks, &machineNetworks)
					validateHostsRequestedHostname(actual.Payload)
				})

				It("Empty networks - valid", func() {
					mockSuccess(1)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks: []*models.ClusterNetwork{},
							ServiceNetworks: []*models.ServiceNetwork{},
							MachineNetworks: []*models.MachineNetwork{},
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)

					Expect(actual.Payload.ClusterNetworks).To(BeEmpty())
					Expect(actual.Payload.ServiceNetworks).To(BeEmpty())
					Expect(actual.Payload.MachineNetworks).To(BeEmpty())
					validateHostsRequestedHostname(actual.Payload)
				})

				// TODO(MGMT-9751-remove-single-network)
				It("Empty networks - no action", func() {
					mockSuccess(1)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworkCidr:       swag.String(""),
							ClusterNetworkHostPrefix: swag.Int64(0),
							ServiceNetworkCidr:       swag.String(""),
							MachineNetworkCidr:       swag.String(""),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)

					Expect(actual.Payload.ClusterNetworks).To(Equal(clusterNetworks))
					Expect(actual.Payload.ServiceNetworks).To(Equal(serviceNetworks))
					Expect(actual.Payload.MachineNetworks).To(Equal(machineNetworks))
					validateHostsRequestedHostname(actual.Payload)
				})

				// TODO(MGMT-9751-remove-single-network)
				It("Override networks with - no action", func() {
					mockSuccess(1)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworkCidr:       swag.String("10.128.0.0/14"),
							ClusterNetworkHostPrefix: swag.Int64(23),
							ServiceNetworkCidr:       swag.String("172.30.0.0/16"),
							MachineNetworkCidr:       swag.String("192.168.145.0/24"),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)

					Expect(actual.Payload.ClusterNetworks).To(Equal(clusterNetworks))
					Expect(actual.Payload.ServiceNetworks).To(Equal(serviceNetworks))
					Expect(actual.Payload.MachineNetworks).To(Equal(machineNetworks))
					validateHostsRequestedHostname(actual.Payload)
				})

				It("Empty networks - invalid empty ClusterNetwork", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks: []*models.ClusterNetwork{{}},
							ServiceNetworks: []*models.ServiceNetwork{},
							MachineNetworks: []*models.MachineNetwork{},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Cluster network CIDR : Failed to parse CIDR '': invalid CIDR address: ")
				})

				It("Empty networks - invalid CIDR, ClusterNetwork", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks: []*models.ClusterNetwork{{Cidr: ""}},
							ServiceNetworks: []*models.ServiceNetwork{},
							MachineNetworks: []*models.MachineNetwork{},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Cluster network CIDR : Failed to parse CIDR '': invalid CIDR address: ")
				})

				It("Empty networks - invalid HostPrefix, ClusterNetwork", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks: []*models.ClusterNetwork{{HostPrefix: 0}},
							ServiceNetworks: []*models.ServiceNetwork{},
							MachineNetworks: []*models.MachineNetwork{},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Cluster network CIDR : Failed to parse CIDR '': invalid CIDR address: ")
				})

				It("Empty networks - invalid empty ServiceNetwork", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks: []*models.ClusterNetwork{},
							ServiceNetworks: []*models.ServiceNetwork{{}},
							MachineNetworks: []*models.MachineNetwork{},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Service network CIDR : Failed to parse CIDR '': invalid CIDR address: ")
				})

				It("Empty networks - invalid CIDR, ServiceNetwork", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks: []*models.ClusterNetwork{},
							ServiceNetworks: []*models.ServiceNetwork{{Cidr: ""}},
							MachineNetworks: []*models.MachineNetwork{},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Service network CIDR : Failed to parse CIDR '': invalid CIDR address: ")
				})

				It("Empty networks - invalid empty MachineNetwork", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks: []*models.ClusterNetwork{},
							ServiceNetworks: []*models.ServiceNetwork{},
							MachineNetworks: []*models.MachineNetwork{{}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Machine network CIDR '': Failed to parse CIDR '': invalid CIDR address: ")
				})

				It("Empty networks - invalid CIDR, MachineNetwork", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks: []*models.ClusterNetwork{},
							ServiceNetworks: []*models.ServiceNetwork{},
							MachineNetworks: []*models.MachineNetwork{{Cidr: ""}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Machine network CIDR '': Failed to parse CIDR '': invalid CIDR address: ")
				})

				It("Multiple machine networks illegal for single-stack", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							MachineNetworks: []*models.MachineNetwork{{Cidr: "15.15.0.0/21"}, {Cidr: "16.16.0.0/21"}},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Single-stack cluster cannot contain multiple Machine Networks")
				})

				It("Override networks - additional cluster subnet", func() {
					mockSuccess(1)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks:   []*models.ClusterNetwork{{Cidr: "11.11.0.0/21", HostPrefix: 24}, {Cidr: "12.12.0.0/21", HostPrefix: 24}},
							ServiceNetworks:   serviceNetworks,
							MachineNetworks:   machineNetworks,
							VipDhcpAllocation: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)

					validateNetworkConfiguration(actual.Payload,
						&[]*models.ClusterNetwork{{Cidr: "11.11.0.0/21", HostPrefix: 24}, {Cidr: "12.12.0.0/21", HostPrefix: 24}},
						&serviceNetworks,
						&machineNetworks)
					validateHostsRequestedHostname(actual.Payload)
				})

				It("Override networks - 2 additional cluster subnets", func() {
					mockSuccess(1)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks:   []*models.ClusterNetwork{{Cidr: "10.10.0.0/21", HostPrefix: 24}, {Cidr: "11.11.0.0/21", HostPrefix: 24}, {Cidr: "12.12.0.0/21", HostPrefix: 24}},
							ServiceNetworks:   serviceNetworks,
							MachineNetworks:   machineNetworks,
							VipDhcpAllocation: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)

					validateNetworkConfiguration(actual.Payload,
						&[]*models.ClusterNetwork{{Cidr: "10.10.0.0/21", HostPrefix: 24}, {Cidr: "11.11.0.0/21", HostPrefix: 24}, {Cidr: "12.12.0.0/21", HostPrefix: 24}},
						&serviceNetworks,
						&machineNetworks)
					validateHostsRequestedHostname(actual.Payload)
				})

				It("Multiple clusters", func() {
					secondClusterID := strfmt.UUID(uuid.New().String())
					Expect(db.Create(&common.Cluster{Cluster: models.Cluster{ID: &secondClusterID}}).Error).ShouldNot(HaveOccurred())
					setNetworksClusterID(secondClusterID, clusterNetworks, serviceNetworks, machineNetworks)

					mockSuccess(1)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							ClusterNetworks:   clusterNetworks,
							ServiceNetworks:   serviceNetworks,
							MachineNetworks:   machineNetworks,
							VipDhcpAllocation: swag.Bool(true),
						},
					})
					Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated)
					validateNetworkConfiguration(actual.Payload, &clusterNetworks, &serviceNetworks, &machineNetworks)
					validateHostsRequestedHostname(actual.Payload)

					cluster, err := common.GetClusterFromDB(db, secondClusterID, common.UseEagerLoading)
					Expect(err).ToNot(HaveOccurred())
					validateNetworkConfiguration(&cluster.Cluster, &clusterNetworks, &serviceNetworks, &machineNetworks)

					var dbMachineNetworks []*models.MachineNetwork
					Expect(db.Find(&dbMachineNetworks).Error).ShouldNot(HaveOccurred())
					Expect(dbMachineNetworks).To(HaveLen(len(machineNetworks) * 2))
				})
			})

			Context("Multiple VIPs forbidden in update for pre-4.12", func() {
				It("2 APIVips and 2 IngressVips", func() {
					apiVip := "8.8.8.7"
					ingressVip := "8.8.8.1"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::1")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::2")}}

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})

					verifyApiErrorString(reply, http.StatusBadRequest, "dual-stack VIPs are not supported in OpenShift 4.6")
				})

				It("2 APIVips and 1 IngressVips", func() {
					apiVip := "8.8.8.7"
					ingressVip := "8.8.8.1"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}, {IP: models.IP("2001:db8::1")}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}}

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})

					verifyApiErrorString(reply, http.StatusBadRequest, "dual-stack VIPs are not supported in OpenShift 4.6")
				})

				It("1 APIVip and 2 IngressVips", func() {
					apiVip := "8.8.8.7"
					ingressVip := "8.8.8.1"
					apiVips := []*models.APIVip{{IP: models.IP(apiVip)}}
					ingressVips := []*models.IngressVip{{IP: models.IP(ingressVip)}, {IP: models.IP("2001:db8::2")}}

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							APIVips:     apiVips,
							IngressVips: ingressVips,
						},
					})

					verifyApiErrorString(reply, http.StatusBadRequest, "dual-stack VIPs are not supported in OpenShift 4.6")
				})
			})
		})
	})

	Context("OpenshiftVersion does not support wildcard", func() {

		It("Fail to create Cluster with a wildcard noProxy", func() {
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:                 swag.String("some-cluster-name"),
					OpenshiftVersion:     swag.String("4.8.0-fc.1"),
					NoProxy:              swag.String("*"),
					PullSecret:           swag.String(fakePullSecret),
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
				},
			})

			Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
			Expect(reply.(*common.ApiErrorResponse).Error()).To(Equal("Sorry, no-proxy value '*' is not supported in release: 4.8.0-fc.1"))
		})

		It("Fail to update Cluster with a wildcard noProxy", func() {
			clusterID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.Cluster{
				Cluster: models.Cluster{
					ID:               &clusterID,
					OpenshiftVersion: "4.8.0-fc.1",
					HTTPProxy:        "http://proxy.proxy",
					HTTPSProxy:       "https://proxy.proxy",
					NoProxy:          "foo.com",
				}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
				ClusterID: clusterID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					HTTPProxy:  swag.String(""),
					HTTPSProxy: swag.String(""),
					NoProxy:    swag.String("*"),
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
			Expect(reply.(*common.ApiErrorResponse).Error()).To(Equal("Sorry, no-proxy value '*' is not supported in release: 4.8.0-fc.1"))
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

		createCluster := func(cluster *common.Cluster) {
			clusterID = strfmt.UUID(uuid.New().String())
			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(cluster).Error
			Expect(err).ShouldNot(HaveOccurred())

			createInfraEnv(db, infraEnvID, clusterID)

			addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
			addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
			addHost(masterHostId3, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname2", "bootMode", "10.11.200.180/16"), db)
			err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: &clusterID}).UpdateColumn("free_addresses",
				makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16", "10.11.12.13", "10.11.20.50"))).Error
			Expect(err).ToNot(HaveOccurred())
			mockDurationsSuccess()
			ignitionReader = io.NopCloser(strings.NewReader(`{
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
		}

		defaultCluster := &common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			APIVips:          []*models.APIVip{{IP: "10.11.12.13"}},
			IngressVips:      []*models.IngressVip{{IP: "10.11.20.50"}},
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusReady),
		}}

		clusterWithIgnoredValidations := &common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			APIVips:          []*models.APIVip{{IP: "10.11.12.13"}},
			IngressVips:      []*models.IngressVip{{IP: "10.11.20.50"}},
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusInstalling),
		},
			IgnoredClusterValidations: "[\"no-cidrs-overlapping\"]",
			IgnoredHostValidations:    "[\"has-memory-for-role\"]"}

		BeforeEach(func() {
			DoneChannel = make(chan int)
		})

		It("success", func() {
			createCluster(defaultCluster)
			mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
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
			mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.IgnitionConfigImageGeneratedEventName),
				eventstest.WithClusterIdMatcher(clusterID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo))).MinTimes(0)

			err := db.Model(&models.Cluster{}).Where("id = ?", clusterID).Updates(&models.Cluster{
				LastInstallationPreparation: models.LastInstallationPreparation{
					Status: models.LastInstallationPreparationStatusFailed,
					Reason: "Testing clear of failure reason on installation start",
				},
			}).Error
			Expect(err).ShouldNot(HaveOccurred())

			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2InstallClusterAccepted()))
			waitForDoneChannel()

			count := db.Model(&models.Cluster{}).Where("openshift_cluster_id <> ''").First(&models.Cluster{}).RowsAffected
			Expect(count).To(Equal(int64(1)))

			cluster, err := common.GetClusterFromDBWhere(db, common.UseEagerLoading, common.DeleteRecordsState(false), "id = ? AND last_installation_preparation_status = ?", clusterID, models.LastInstallationPreparationStatusNotStarted)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.LastInstallationPreparation.Status).To(Equal(models.LastInstallationPreparationStatusNotStarted))
			Expect(cluster.LastInstallationPreparation.Reason).To(Equal("Preparation never performed"))
		})

		It("Should send event in case of ignored validations", func() {
			By("Start installation with ignored validations and expect events", func() {
				createCluster(clusterWithIgnoredValidations)
				mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
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
				mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.IgnitionConfigImageGeneratedEventName),
					eventstest.WithClusterIdMatcher(clusterID.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityInfo))).MinTimes(0)
				mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ValidationsIgnoredEventName),
					eventstest.WithClusterIdMatcher(clusterID.String()),
					eventstest.WithSeverityMatcher(models.EventSeverityInfo))).MinTimes(0)
				reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
					ClusterID: clusterID,
				})

				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2InstallClusterAccepted()))
				waitForDoneChannel()

				count := db.Model(&models.Cluster{}).Where("openshift_cluster_id <> ''").First(&models.Cluster{}).RowsAffected
				Expect(count).To(Equal(int64(1)))
			})

			By("Should not be able to modify ignored validations once installation has started", func() {
				err := db.Model(&models.Cluster{ID: &clusterID}).UpdateColumn("status", "preparing-for-installation").Error
				Expect(err).ToNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(clusterWithIgnoredValidations)).Return(errors.Errorf("wrong state")).Times(1)
				reply := bm.V2SetIgnoredValidations(ctx, installer.V2SetIgnoredValidationsParams{
					ClusterID: clusterID,
					IgnoredValidations: &models.IgnoredValidations{
						ClusterValidationIds: "[\"dns-domain-defined\"]",
						HostValidationIds:    "[\"has-cpu-cores-for-role\",\"has-memory-for-role\"]",
					},
				})
				reason := reply.(*installer.V2SetIgnoredValidationsBadRequest).Payload.Reason
				Expect(*reason).To(ContainSubstring("wrong state"))
			})
		})

		Describe("V2GetClusterUISettings", func() {
			It("returns ui settings for cluster", func() {
				createCluster(defaultCluster)
				uiSettings := "{\"foo\":\"bar\"}"
				response := db.Model(&common.Cluster{}).Where("id = ?", defaultCluster.ID.String()).Updates(map[string]interface{}{"ui_settings": uiSettings})
				Expect(response.Error).ToNot(HaveOccurred())
				Expect(response.Statement.RowsAffected).To(Equal(int64(1)))
				reply := bm.V2GetClusterUISettings(ctx, installer.V2GetClusterUISettingsParams{
					ClusterID: clusterID,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2GetClusterUISettingsOK()))
				Expect(reply.(*installer.V2GetClusterUISettingsOK).Payload).To(Equal(uiSettings))
			})
			It("returns not found when the cluster doesn't exist", func() {
				reply := bm.V2GetClusterUISettings(ctx, installer.V2GetClusterUISettingsParams{
					ClusterID: clusterID,
				})
				Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
			})
		})

		Describe("V2UpdateClusterUISettings", func() {
			It("updates ui settings for cluster", func() {
				createCluster(defaultCluster)
				settingsJSON := "{\"foo\":\"bar\"}"
				reply := bm.V2UpdateClusterUISettings(ctx, installer.V2UpdateClusterUISettingsParams{
					ClusterID:  clusterID,
					UISettings: settingsJSON,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterUISettingsOK()))
				updated, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
				Expect(err).ToNot(HaveOccurred())
				Expect(updated.UISettings).To(Equal(settingsJSON))
			})
			It("returns not found when the cluster doesn't exist", func() {
				settingsJSON := "{\"foo\":\"bar\"}"
				reply := bm.V2UpdateClusterUISettings(ctx, installer.V2UpdateClusterUISettingsParams{
					ClusterID:  clusterID,
					UISettings: settingsJSON,
				})
				Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusNotFound, errors.Errorf(""))))
			})
		})

		It("success arm64 baremetal platform with 4.11 where it is supported", func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			clusterID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				APIVips:          []*models.APIVip{{IP: "10.11.12.13"}},
				IngressVips:      []*models.IngressVip{{IP: "10.11.20.50"}},
				OpenshiftVersion: "4.11",
				Status:           swag.String(models.ClusterStatusReady),
				CPUArchitecture:  common.ARM64CPUArchitecture,
				Platform: &models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
				},
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			createInfraEnv(db, infraEnvID, clusterID)
			addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
			addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
			addHost(masterHostId3, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname2", "bootMode", "10.11.200.180/16"), db)
			err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: &clusterID}).UpdateColumn("free_addresses",
				makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16", "10.11.12.13", "10.11.20.50"))).Error
			Expect(err).ToNot(HaveOccurred())
			mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockClusterIsReadyForInstallationSuccess()
			mockGenerateAdditionalManifestsSuccess()
			armRelease := &models.ReleaseImage{
				URL: swag.String("quay.io/openshift-release-dev/ocp-release:4.6.16-aarch64"),
			}
			mockGetInstallConfigSuccess(mockInstallConfigBuilder)
			mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), common.ARM64CPUArchitecture, gomock.Any()).Return(armRelease, nil).Times(1)
			mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), common.DefaultCPUArchitecture, gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
			mockGenerator.EXPECT().GenerateInstallConfig(gomock.Any(), gomock.Any(), gomock.Any(), *armRelease.URL, *common.TestDefaultConfig.ReleaseImage.URL).Return(nil).Times(1)

			mockClusterPrepareForInstallationSuccess(mockClusterApi)
			mockHostPrepareForRefresh(mockHostApi)
			mockHandlePreInstallationSuccess(mockClusterApi, DoneChannel)
			setDefaultGetMasterNodesIds(mockClusterApi)
			setDefaultHostSetBootstrap(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockClusterRefreshStatus(mockClusterApi)
			mockClusterDeleteLogsSuccess(mockClusterApi)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
			mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.IgnitionConfigImageGeneratedEventName),
				eventstest.WithClusterIdMatcher(clusterID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityInfo))).MinTimes(0)

			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2InstallClusterAccepted()))
			waitForDoneChannel()

			count := db.Model(&models.Cluster{}).Where("openshift_cluster_id <> ''").First(&models.Cluster{}).RowsAffected
			Expect(count).To(Equal(int64(1)))
		})

		It("fail arm64 baremetal platform in case no x86 was found", func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			clusterID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				APIVips:          []*models.APIVip{{IP: "10.11.12.13"}},
				IngressVips:      []*models.IngressVip{{IP: "10.11.20.50"}},
				OpenshiftVersion: "4.11",
				Status:           swag.String(models.ClusterStatusReady),
				CPUArchitecture:  common.ARM64CPUArchitecture,
				Platform: &models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
				},
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			createInfraEnv(db, infraEnvID, clusterID)
			addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
			addHost(masterHostId2, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname1", "bootMode", "1.2.3.5/24", "10.11.50.80/16"), db)
			addHost(masterHostId3, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, getInventoryStr("hostname2", "bootMode", "10.11.200.180/16"), db)
			err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: &clusterID}).UpdateColumn("free_addresses",
				makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16", "10.11.12.13", "10.11.20.50"))).Error
			Expect(err).ToNot(HaveOccurred())
			mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockClusterIsReadyForInstallationSuccess()
			mockGenerateAdditionalManifestsSuccess()
			armRelease := &models.ReleaseImage{
				URL: swag.String("quay.io/openshift-release-dev/ocp-release:4.6.16-aarch64"),
			}
			mockGetInstallConfigSuccess(mockInstallConfigBuilder)
			mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), common.ARM64CPUArchitecture, gomock.Any()).Return(armRelease, nil).Times(1)
			mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), common.DefaultCPUArchitecture, gomock.Any()).Return(nil, errors.Errorf("Dummy")).Times(1)

			mockClusterPrepareForInstallationSuccess(mockClusterApi)
			mockHostPrepareForRefresh(mockHostApi)
			setDefaultGetMasterNodesIds(mockClusterApi)
			setDefaultHostSetBootstrap(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockClusterRefreshStatus(mockClusterApi)
			mockClusterDeleteLogsSuccess(mockClusterApi)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
			mockClusterApi.EXPECT().HandlePreInstallError(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Do(func(ctx, c, err interface{}) { DoneChannel <- 1 })

			_ = bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})
			waitForDoneChannel()
		})

		It("cluster doesn't exists", func() {
			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: strfmt.UUID(uuid.New().String()),
			})
			verifyApiError(reply, http.StatusNotFound)
		})

		It("failed to auto-assign role", func() {
			createCluster(defaultCluster)
			mockAutoAssignFailed()
			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusInternalServerError)
		})

		It("failed to prepare cluster", func() {
			createCluster(defaultCluster)
			mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
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

			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusInternalServerError)
		})

		It("cluster is not ready to install", func() {
			createCluster(defaultCluster)
			mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockHostPrepareForRefresh(mockHostApi)
			mockClusterRefreshStatus(mockClusterApi)
			setIsReadyForInstallationFalse(mockClusterApi)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)

			Expect(db.Model(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).UpdateColumn("status", "insufficient").Error).To(Not(HaveOccurred()))
			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusConflict)
		})

		It("cluster is not ready to install - not auto assigned", func() {
			createCluster(defaultCluster)
			mockFalseAutoAssignSuccess(3)
			setIsReadyForInstallationFalse(mockClusterApi)

			Expect(db.Model(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).UpdateColumn("status", "insufficient").Error).To(Not(HaveOccurred()))
			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusConflict)
		})

		It("list of masters for setting bootstrap return empty list", func() {
			createCluster(defaultCluster)
			mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockHostPrepareForRefresh(mockHostApi)
			mockClusterPrepareForInstallationSuccess(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)

			mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]*strfmt.UUID{}, nil).Times(1)

			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusInternalServerError)
		})

		It("GetMasterNodesIds fails in the go routine", func() {
			createCluster(defaultCluster)
			mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockHostPrepareForRefresh(mockHostApi)
			mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, errors.Errorf("nop"))
			mockClusterPrepareForInstallationSuccess(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)

			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusInternalServerError)
		})

		It("GetMasterNodesIds returns empty list", func() {
			createCluster(defaultCluster)
			mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
			mockAutoAssignSuccess(3)
			mockClusterRefreshStatusSuccess()
			mockHostPrepareForRefresh(mockHostApi)
			mockClusterPrepareForInstallationSuccess(mockClusterApi)
			setIsReadyForInstallationTrue(mockClusterApi)
			mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)

			mockClusterApi.EXPECT().GetMasterNodesIds(gomock.Any(), gomock.Any(), gomock.Any()).
				Return([]*strfmt.UUID{&masterHostId1, &masterHostId2, &masterHostId3}, errors.Errorf("nop"))

			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})
			verifyApiError(reply, http.StatusInternalServerError)
		})

		It("failed to delete logs", func() {
			createCluster(defaultCluster)
			mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
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
			mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.IgnitionConfigImageGeneratedEventName),
				eventstest.WithClusterIdMatcher(clusterID.String()),
				eventstest.WithSeverityMatcher(models.EventSeverityError))).MinTimes(0)

			reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
				ClusterID: clusterID,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2InstallClusterAccepted()))
			waitForDoneChannel()

			count := db.Model(&models.Cluster{}).Where("openshift_cluster_id <> ''").First(&models.Cluster{}).RowsAffected
			Expect(count).To(Equal(int64(1)))
		})

		Context("CancelInstallation", func() {
			BeforeEach(func() {
				createCluster(defaultCluster)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			})
			It("cancel installation success", func() {
				setCancelInstallationSuccess()

				cancelReply := bm.V2CancelInstallation(ctx, installer.V2CancelInstallationParams{
					ClusterID: clusterID,
				})
				Expect(cancelReply).Should(BeAssignableToTypeOf(installer.NewV2CancelInstallationAccepted()))
			})
			It("cancel installation conflict", func() {
				setCancelInstallationHostConflict()

				cancelReply := bm.V2CancelInstallation(ctx, installer.V2CancelInstallationParams{
					ClusterID: clusterID,
				})

				verifyApiError(cancelReply, http.StatusConflict)
			})
			It("cancel installation internal error", func() {
				setCancelInstallationInternalServerError()

				cancelReply := bm.V2CancelInstallation(ctx, installer.V2CancelInstallationParams{
					ClusterID: clusterID,
				})

				verifyApiError(cancelReply, http.StatusInternalServerError)
			})
		})

		Context("reset cluster", func() {
			BeforeEach(func() {
				createCluster(defaultCluster)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			})
			It("reset installation success", func() {
				setResetClusterSuccess()
				resetReply := bm.V2ResetCluster(ctx, installer.V2ResetClusterParams{
					ClusterID: clusterID,
				})
				Expect(resetReply).Should(BeAssignableToTypeOf(installer.NewV2ResetClusterAccepted()))
			})
			It("reset cluster conflict", func() {
				setResetClusterConflict()

				cancelReply := bm.V2ResetCluster(ctx, installer.V2ResetClusterParams{
					ClusterID: clusterID,
				})

				verifyApiError(cancelReply, http.StatusConflict)
			})
			It("reset cluster internal error", func() {
				setResetClusterInternalServerError()

				cancelReply := bm.V2ResetCluster(ctx, installer.V2ResetClusterParams{
					ClusterID: clusterID,
				})

				verifyApiError(cancelReply, http.StatusInternalServerError)
			})
		})

		Context("complete installation", func() {
			BeforeEach(func() {
				createCluster(defaultCluster)
			})
			errorInfo := "dummy"
			It("complete success", func() {
				success := true
				// TODO: MGMT-4458
				// This function can be removed once the controller will stop sending this request
				// The service is already capable of completing the installation on its own

				reply := bm.V2CompleteInstallation(ctx, installer.V2CompleteInstallationParams{
					ClusterID:        clusterID,
					CompletionParams: &models.CompletionParams{ErrorInfo: errorInfo, IsSuccess: &success},
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2CompleteInstallationAccepted()))
			})
			It("complete failure", func() {
				success := false

				reply := bm.V2CompleteInstallation(ctx, installer.V2CompleteInstallationParams{
					ClusterID:        clusterID,
					CompletionParams: &models.CompletionParams{ErrorInfo: errorInfo, IsSuccess: &success},
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2CompleteInstallationAccepted()))
			})
			It("complete with failing operator report", func() {
				success := false
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ClusterOperatorReportEventName),
					eventstest.WithMessageContainsMatcher("authentication, csi-snapshot-controller"))).Times(1)
				listOfOperaotrs := []models.OperatorMonitorReport{
					{
						Name:       "authentication",
						Status:     models.OperatorStatusFailed,
						StatusInfo: "APIServicesAvailable: PreconditionNotReady",
					},
					{
						Name:       "csi-snapshot-controller",
						Status:     models.OperatorStatusFailed,
						StatusInfo: "SISnapshotController_Deploying::CSISnapshotWebhookController_Deploying",
					},
					{
						Name:       "other",
						Status:     models.OperatorStatusProgressing,
						StatusInfo: "",
					},
					{
						Name:       "etcd",
						Status:     models.OperatorStatusAvailable,
						StatusInfo: "success",
					},
				}
				reply := bm.V2CompleteInstallation(ctx, installer.V2CompleteInstallationParams{
					ClusterID: clusterID,
					CompletionParams: &models.CompletionParams{
						ErrorInfo: errorInfo,
						IsSuccess: &success,
						Data:      map[string]interface{}{clusterOperatorReportKey: listOfOperaotrs},
					},
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2CompleteInstallationAccepted()))
			})
		})

		AfterEach(func() {
			close(DoneChannel)
			common.DeleteTestDB(db, dbName)
		})
	})
})

var _ = Describe("V2ClusterUpdate cluster", func() {

	masterHostId1 := strfmt.UUID(uuid.New().String())
	masterHostId3 := strfmt.UUID(uuid.New().String())

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

		ignitionReader = io.NopCloser(strings.NewReader(`{
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
		if role == models.HostRoleBootstrap {
			host.Bootstrap = true
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		return host
	}

	mockDurationsSuccess := func() {
		mockMetric.EXPECT().Duration(gomock.Any(), gomock.Any()).Return().AnyTimes()
	}

	mockSuccess := func() {
		mockClusterUpdateSuccess(1, 0)
	}

	Context("Update", func() {
		BeforeEach(func() {
			mockDurationsSuccess()
		})

		Context("Single node", func() {
			var cluster *common.Cluster
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				cluster = &common.Cluster{Cluster: models.Cluster{
					ID:       &clusterID,
					Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
				}}
				err := db.Create(cluster).Error
				Expect(err).ShouldNot(HaveOccurred())
				addHost(masterHostId1, models.HostRoleMaster, "known", models.HostKindHost, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
				err = db.Model(&models.Host{ID: &masterHostId3, ClusterID: &clusterID}).UpdateColumn("free_addresses",
					makeFreeNetworksAddressesStr(makeFreeAddresses("10.11.0.0/16", "10.11.12.15", "10.11.12.16"))).Error
				Expect(err).ToNot(HaveOccurred())
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
			})

			mockSuccess := func(times int) {
				mockClusterUpdateSuccess(times, 1)
			}

			JustBeforeEach(func() {
				Expect(db.Model(&common.Cluster{}).Where("id = ?", clusterID).Updates(map[string]interface{}{
					"user_managed_networking": true,
					"high_availability_mode":  models.ClusterHighAvailabilityModeNone,
					"platform_type":           models.PlatformTypeNone,
					"cpu_architecture":        common.X86CPUArchitecture,
				}).Error).ShouldNot(HaveOccurred())
			})

			It("Fail to unset UserManagedNetworking", func() {
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						UserManagedNetworking: swag.Bool(false),
					},
				})

				verifyApiErrorString(reply, http.StatusBadRequest, "disabling User Managed Networking or setting platform different than none or oci platforms is not allowed in single node Openshift")
			})

			It("Set Machine CIDR", func() {
				err := network.UpdateVipsTables(db,
					&common.Cluster{Cluster: models.Cluster{
						ID:          &clusterID,
						APIVips:     common.TestIPv4Networking.APIVips,
						IngressVips: common.TestIPv4Networking.IngressVips,
					}},
					true,
					true,
				)
				Expect(err).ShouldNot(HaveOccurred())

				mockSuccess(1)

				machineNetworks := common.TestIPv4Networking.MachineNetworks
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						MachineNetworks: machineNetworks,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				actual := reply.(*installer.V2UpdateClusterCreated)
				Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
				Expect(len(actual.Payload.APIVips)).To(BeZero())
				Expect(len(actual.Payload.IngressVips)).To(BeZero())
				validateNetworkConfiguration(actual.Payload, nil, nil, &machineNetworks)
			})

			It("Fail with bad Machine CIDR", func() {
				badMachineCidr := "2.2.3.128/24"
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						MachineNetworks: []*models.MachineNetwork{{Cidr: models.Subnet(badMachineCidr)}},
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, fmt.Sprintf("%s is not a valid network CIDR", badMachineCidr))
			})

			It("Success with machine cidr that is not part of cluster networks", func() {
				mockSuccess(1)
				wrongMachineCidrNetworks := []*models.MachineNetwork{{Cidr: models.Subnet("2.2.3.0/24")}}

				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						MachineNetworks: wrongMachineCidrNetworks,
					},
				})

				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				actual := reply.(*installer.V2UpdateClusterCreated)
				Expect(actual.Payload.VipDhcpAllocation).To(Equal(swag.Bool(false)))
				Expect(len(actual.Payload.APIVips)).To(BeZero())
				Expect(len(actual.Payload.IngressVips)).To(BeZero())
				validateNetworkConfiguration(actual.Payload, nil, nil, &wrongMachineCidrNetworks)
			})
		})
		Context("Platform", func() {
			Context("Update Platform while Cluster platform is baremetal", func() {
				var cluster *common.Cluster
				BeforeEach(func() {
					clusterID = strfmt.UUID(uuid.New().String())
					cluster = &common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterID,
						HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeFull),
						UserManagedNetworking: swag.Bool(false),
						OpenshiftVersion:      "4.14",
						CPUArchitecture:       common.DefaultCPUArchitecture,
						Platform: &models.Platform{
							Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
						},
					}}
					err := db.Create(cluster).Error
					Expect(err).ShouldNot(HaveOccurred())
					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				})

				It("Update UMN=false - success", func() {
					mockSuccess()

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))

				})

				It("Update platform=BM - success", func() {
					mockSuccess()
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
				})

				It("Update platform=BM and UMN=false - success", func() {
					mockSuccess()
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
				})

				addVsphereHostWithNetworking := func(clusterId strfmt.UUID, role models.HostRole) {
					inventory := models.Inventory{
						Interfaces: []*models.Interface{
							{
								Name: "eth0",
								IPV4Addresses: []string{
									"1.2.3.4/24",
									"10.0.0.100/24",
								},
							},
						},
						SystemVendor: &models.SystemVendor{
							Virtual:      true,
							Manufacturer: "VMware, Inc.",
							ProductName:  "VMware7,1",
							SerialNumber: "VMware-12 34 56 78 90 12 ab cd-ef gh 12 34 56 67 89 90",
						},
					}
					inventoryByte, err := json.Marshal(inventory)
					Expect(err).ToNot(HaveOccurred())
					addHost(strfmt.UUID(uuid.New().String()), role, models.HostStatusKnown, models.HostKindHost, clusterID, string(inventoryByte), db)
				}

				It("Update platform=vsphere with UMN=true - success", func() {
					mockClusterUpdateSuccess(1, 1)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					addVsphereHostWithNetworking(clusterID, models.HostRoleBootstrap)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							UserManagedNetworking: swag.Bool(true),
							VipDhcpAllocation:     swag.Bool(false),
							NetworkType:           swag.String(models.ClusterNetworkTypeOpenShiftSDN),
							ClusterNetworks:       []*models.ClusterNetwork{{Cidr: "10.128.0.0/14", ClusterID: clusterID, HostPrefix: 23}},
							ServiceNetworks:       []*models.ServiceNetwork{{Cidr: "172.30.0.0/16", ClusterID: clusterID}},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))

					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))

					var result installcfg.InstallerConfigBaremetal
					installConfig := createInstallConfigBuilder()
					mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
					data, err := installConfig.GetInstallConfig(&common.Cluster{Cluster: *actual}, []*common.InfraEnv{}, "")
					Expect(err).ShouldNot(HaveOccurred())
					err = json.Unmarshal(data, &result)
					Expect(err).ShouldNot(HaveOccurred())
					Expect(result.Platform.Vsphere).ShouldNot(BeNil())
					Expect(result.Platform.None).Should(BeNil())
					Expect(result.Platform.Baremetal).Should(BeNil())

					Expect(result.Platform.Vsphere.DeprecatedAPIVIP).Should(Equal(""))
					Expect(result.Platform.Vsphere.DeprecatedIngressVIP).Should(Equal(""))
					Expect(result.Networking.MachineNetwork).ShouldNot(BeNil())
				})

				It("Update UMN=true while cluster platform already set to vsphere - success", func() {
					mockClusterUpdateSuccess(2, 0)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))

					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)

					reply2 := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							VipDhcpAllocation:     swag.Bool(false),
							NetworkType:           swag.String(models.ClusterNetworkTypeOpenShiftSDN),
						},
					})

					Expect(reply2).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual2 := reply2.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual2.UserManagedNetworking)).To(Equal(true))
					Expect(*actual2.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=true and vphsere platform while cluster platform already set to none - success", func() {
					mockClusterUpdateSuccess(2, 0)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))

					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply2 := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							VipDhcpAllocation:     swag.Bool(false),
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							NetworkType:           swag.String(models.ClusterNetworkTypeOpenShiftSDN),
						},
					})

					Expect(reply2).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual2 := reply2.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual2.UserManagedNetworking)).To(Equal(true))
					Expect(*actual2.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=nil and baremetal platform while cluster platform is set to vsphere - success", func() {
					mockClusterUpdateSuccess(2, 0)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))

					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					reply2 := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						},
					})

					Expect(reply2).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual2 := reply2.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual2.UserManagedNetworking)).To(Equal(true))
					Expect(*actual2.Platform.Type).To(Equal(models.PlatformTypeNone))
				})

				It("Update UMN=nil and none platform while cluster platform is set to vsphere - success", func() {
					mockClusterUpdateSuccess(2, 0)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))

					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any(), mockUsage)

					reply2 := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
						},
					})

					Expect(reply2).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual2 := reply2.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual2.UserManagedNetworking)).To(Equal(false))
					Expect(*actual2.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
				})

				It("Update UMN=nil and baremetal platform while cluster platform is set to vsphere and umn true- failure", func() {
					mockClusterUpdateSuccess(1, 0)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))

					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
					reply2 := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
						},
					})
					verifyApiErrorString(reply2, http.StatusBadRequest, "Can't set baremetal platform with user-managed-networking enabled")
				})

				Context("Single node cluster", func() {
					BeforeEach(func() {
						clusterID = strfmt.UUID(uuid.New().String())
						err := db.Create(&common.Cluster{Cluster: models.Cluster{
							ID:                    &clusterID,
							HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
							UserManagedNetworking: swag.Bool(true),
							Platform: &models.Platform{
								Type: common.PlatformTypePtr(models.PlatformTypeNone),
							},
						}}).Error
						Expect(err).ShouldNot(HaveOccurred())

					})

					It("Update to vsphere platform while single node cluster - failure", func() {
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "Single node cluster is not supported alongside vsphere platform")
					})

					It("Update to baremetal platform while single node cluster - failure", func() {
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "disabling User Managed Networking or setting platform different than none or oci platforms is not allowed in single node Openshift")
					})

					It("Update to nutanix platform while single node cluster - failure", func() {
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "disabling User Managed Networking or setting platform different than none or oci platforms is not allowed in single node Openshift")
					})

					It("Update to external platform while single node cluster - success", func() {
						mockSuccess()
						mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeExternal), gomock.Any(), mockUsage)

						platformName := "platform-name"
						cloudControllerManager := models.PlatformExternalCloudControllerManagerEmpty

						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								Platform: &models.Platform{
									Type: common.PlatformTypePtr(models.PlatformTypeExternal),
									External: &models.PlatformExternal{
										PlatformName:           &platformName,
										CloudControllerManager: &cloudControllerManager,
									},
								},
							},
						})
						Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
						actual := reply.(*installer.V2UpdateClusterCreated).Payload
						Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeTrue())
						Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
						Expect(*actual.Platform.External.PlatformName).Should(Equal(platformName))
						Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
					})
				})

				It("Update UMN=nil and none platform while cluster platform is set to vsphere and umn false- failure", func() {
					mockClusterUpdateSuccess(1, 0)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))

					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
					reply2 := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						},
					})
					verifyApiErrorString(reply2, http.StatusBadRequest, "Can't set none platform with user-managed-networking disabled")
				})

				It("Update UMN=false and vphsere platform while cluster platform already set to none - success", func() {
					mockClusterUpdateSuccess(2, 0)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))

					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply2 := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							NetworkType:           swag.String(models.ClusterNetworkTypeOpenShiftSDN),
						},
					})

					Expect(reply2).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual2 := reply2.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual2.UserManagedNetworking)).To(Equal(false))
					Expect(*actual2.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=false and vphsere platform while cluster platform already set to baremetal - success", func() {
					mockClusterUpdateSuccess(1, 0)
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply2 := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							NetworkType:           swag.String(models.ClusterNetworkTypeOpenShiftSDN),
						},
					})

					Expect(reply2).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual2 := reply2.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual2.UserManagedNetworking)).To(Equal(false))
					Expect(*actual2.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=true and vphsere platform while cluster platform already set to baremetal - success", func() {
					mockClusterUpdateSuccess(1, 0)

					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply2 := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
							VipDhcpAllocation:     swag.Bool(false),
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							NetworkType:           swag.String(models.ClusterNetworkTypeOpenShiftSDN),
						},
					})

					Expect(reply2).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual2 := reply2.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual2.UserManagedNetworking)).To(Equal(true))
					Expect(*actual2.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=true - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))

				})

				It("Update UMN=true and platform=baremetal results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set baremetal platform with user-managed-networking enabled")
				})

				It("Update platform=none while cluster.platform=BM - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))

				})

				It("Update UMN=false and platform=none results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
							UserManagedNetworking: swag.Bool(false)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set none platform with user-managed-networking disabled")
				})

				It("Update UMN=true and platform=none while cluster.platform=BM - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))

				})

				It("Update platform=external - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeExternal)},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set external platform with user-managed-networking disabled")
				})

				It("Update UMN=false and platform=external results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeExternal)},
							UserManagedNetworking: swag.Bool(false)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set external platform with user-managed-networking disabled")
				})

				It("Update UMN=true and platform=external - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeExternal), gomock.Any(), mockUsage)

					platformName := "platform-name"
					cloudControllerManager := models.PlatformExternalCloudControllerManagerEmpty

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{
								Type: common.PlatformTypePtr(models.PlatformTypeExternal),
								External: &models.PlatformExternal{
									PlatformName:           &platformName,
									CloudControllerManager: &cloudControllerManager,
								},
							},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
					Expect(*actual.Platform.External.PlatformName).Should(Equal(platformName))
					Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
				})

				It("Update UMN=true, default cloud controller manager and platform=external - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeExternal), gomock.Any(), mockUsage)

					platformName := "platform-name"

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{
								Type: common.PlatformTypePtr(models.PlatformTypeExternal),
								External: &models.PlatformExternal{
									PlatformName: &platformName,
								},
							},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
					Expect(*actual.Platform.External.PlatformName).Should(Equal(platformName))
					Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(models.PlatformExternalCloudControllerManagerEmpty))
				})

				It("Update platform=nutanix - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNutanix), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNutanix))
				})

				It("Update platform=nutanix with multi cpu architecture - success", func() {
					err := db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update("cpu_architecture", models.ClusterCPUArchitectureMulti).Error
					Expect(err).ShouldNot(HaveOccurred())
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNutanix), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNutanix))
				})

				It("Update UMN=true and platform=nutanix results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set nutanix platform with user-managed-networking enabled")
				})

				It("Update UMN=false and platform=nutanix - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNutanix), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNutanix))
				})

				It("Update platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=true and platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeTrue())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=false and platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})
			})

			Context("Update Platform while Cluster platform is none", func() {
				var cluster *common.Cluster
				BeforeEach(func() {
					clusterID = strfmt.UUID(uuid.New().String())
					cluster = &common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterID,
						HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeFull),
						UserManagedNetworking: swag.Bool(true),
						Platform: &models.Platform{
							Type: common.PlatformTypePtr(models.PlatformTypeNone),
						},
						CPUArchitecture: common.X86CPUArchitecture,
					}}
					err := db.Create(cluster).Error
					Expect(err).ShouldNot(HaveOccurred())
					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				})

				It("Update UMN=true - success", func() {
					mockSuccess()

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
				})

				It("Update platform=none - success", func() {
					mockSuccess()
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))

				})

				It("Update platform=none and UMN=true - success", func() {
					mockSuccess()
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))

				})

				It("Update UMN=false - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))

				})

				It("Update UMN=false and platform=none results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
							UserManagedNetworking: swag.Bool(false)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set none platform with user-managed-networking disabled")
				})

				It("Update platform=baremetal - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))

				})

				It("Update UMN=true and platform=baremetal results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set baremetal platform with user-managed-networking enabled")
				})

				It("Update UMN=false and platform=baremetal - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
				})

				It("Update platform=external - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeExternal), gomock.Any(), mockUsage)

					platformName := "platform-name"
					cloudControllerManager := models.PlatformExternalCloudControllerManagerEmpty

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{
								Type: common.PlatformTypePtr(models.PlatformTypeExternal),
								External: &models.PlatformExternal{
									PlatformName:           &platformName,
									CloudControllerManager: &cloudControllerManager,
								},
							},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
					Expect(*actual.Platform.External.PlatformName).Should(Equal(platformName))
					Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))

				})

				It("Update UMN=false and platform=external results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeExternal)},
							UserManagedNetworking: swag.Bool(false)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set external platform with user-managed-networking disabled")
				})

				It("Update UMN=true and platform=external - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeExternal), gomock.Any(), mockUsage)

					platformName := "platform-name"
					cloudControllerManager := models.PlatformExternalCloudControllerManagerEmpty

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{
								Type: common.PlatformTypePtr(models.PlatformTypeExternal),
								External: &models.PlatformExternal{
									PlatformName:           &platformName,
									CloudControllerManager: &cloudControllerManager,
								},
							},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
					Expect(*actual.Platform.External.PlatformName).Should(Equal(platformName))
					Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
				})

				It("Update platform=nutanix - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set nutanix platform with user-managed-networking enabled")
				})

				It("Update UMN=true and platform=nutanix results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set nutanix platform with user-managed-networking enabled")
				})

				It("Update UMN=false and platform=nutanix - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNutanix), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNutanix))
				})

				It("Update platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeTrue())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=true and platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeTrue())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=false and platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})
			})

			Context("Update Platform while Cluster platform is external", func() {
				var cluster *common.Cluster
				var platformName string
				var cloudControllerManager string

				BeforeEach(func() {
					clusterID = strfmt.UUID(uuid.New().String())
					platformName = "platform-name"
					cloudControllerManager = models.PlatformExternalCloudControllerManagerEmpty

					cluster = &common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterID,
						HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeFull),
						UserManagedNetworking: swag.Bool(true),
						Platform: &models.Platform{
							Type: common.PlatformTypePtr(models.PlatformTypeExternal),
							External: &models.PlatformExternal{
								PlatformName:           &platformName,
								CloudControllerManager: &cloudControllerManager,
							},
						},
						CPUArchitecture: common.X86CPUArchitecture,
					}}
					err := db.Create(cluster).Error
					Expect(err).ShouldNot(HaveOccurred())
					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				})

				It("Update UMN=true - success", func() {
					mockSuccess()

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
					Expect(*actual.Platform.External.PlatformName).Should(Equal(platformName))
					Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
				})

				It("Update platform=external - success", func() {
					mockSuccess()
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeExternal)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
					Expect(*actual.Platform.External.PlatformName).Should(Equal(platformName))
					Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
				})

				It("Update platform name - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeExternal), gomock.Any(), mockUsage)

					newPlatformName := "new-platform-name"
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{
								External: &models.PlatformExternal{
									PlatformName: &newPlatformName,
								},
							},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
					Expect(*actual.Platform.External.PlatformName).Should(Equal(newPlatformName))
					Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
				})

				It("Update cloud controller manager - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeExternal), gomock.Any(), mockUsage)

					newCloudControllerManager := models.PlatformExternalCloudControllerManagerExternal
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{
								External: &models.PlatformExternal{
									CloudControllerManager: &newCloudControllerManager,
								},
							},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
					Expect(*actual.Platform.External.PlatformName).Should(Equal(platformName))
					Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(newCloudControllerManager))
				})

				It("Update UMN=true and platform=external - success", func() {
					mockSuccess()
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeExternal)},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
				})

				It("Update UMN=false and platform=external results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeExternal)},
							UserManagedNetworking: swag.Bool(false)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set external platform with user-managed-networking disabled")
				})

				It("Update platform=baremetal - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set baremetal platform with user-managed-networking enabled")
				})

				It("Update UMN=true and platform=baremetal results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set baremetal platform with user-managed-networking enabled")
				})

				It("Update UMN=false and platform=baremetal - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
					Expect(actual.Platform.External.PlatformName).Should(BeNil())
					Expect(actual.Platform.External.CloudControllerManager).Should(BeNil())
				})

				It("Update UMN=false and set platform name and platform=baremetal results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
								External: &models.PlatformExternal{
									PlatformName: &platformName,
								},
							},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "External settings can only be set with external platform type")
				})

				It("Update platform=none - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
					Expect(actual.Platform.External.PlatformName).Should(BeNil())
					Expect(actual.Platform.External.CloudControllerManager).Should(BeNil())

				})

				It("Update UMN=false and platform=none results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
							UserManagedNetworking: swag.Bool(false)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set none platform with user-managed-networking disabled")
				})

				It("Update UMN=true and platform=none - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
					Expect(actual.Platform.External.PlatformName).Should(BeNil())
					Expect(actual.Platform.External.CloudControllerManager).Should(BeNil())
				})

				It("Update platform=nutanix - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set nutanix platform with user-managed-networking enabled")
				})

				It("Update UMN=true and platform=nutanix results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set nutanix platform with user-managed-networking enabled")
				})

				It("Update UMN=false and platform=nutanix - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNutanix), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNutanix))
					Expect(actual.Platform.External.PlatformName).Should(BeNil())
					Expect(actual.Platform.External.CloudControllerManager).Should(BeNil())
				})

				It("Update platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeTrue())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
					Expect(actual.Platform.External.PlatformName).Should(BeNil())
					Expect(actual.Platform.External.CloudControllerManager).Should(BeNil())
				})

				It("Update UMN=true and platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeTrue())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
					Expect(actual.Platform.External.PlatformName).Should(BeNil())
					Expect(actual.Platform.External.CloudControllerManager).Should(BeNil())
				})

				It("Update UMN=false and platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
					Expect(actual.Platform.External.PlatformName).Should(BeNil())
					Expect(actual.Platform.External.CloudControllerManager).Should(BeNil())
				})

				Context("Update to OCI integration", func() {
					It("Update PlatformName=oci and CloudControllerManager=External - success", func() {
						mockSuccess()
						mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeExternal), gomock.Any(), mockUsage)

						ociPlatformName := "oci"
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								Platform: &models.Platform{
									External: &models.PlatformExternal{
										PlatformName:           &ociPlatformName,
										CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerExternal),
									},
								},
							},
						})
						Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
						actual := reply.(*installer.V2UpdateClusterCreated).Payload
						Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
						Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
						Expect(*actual.Platform.External.PlatformName).Should(Equal(ociPlatformName))
						Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(models.PlatformExternalCloudControllerManagerExternal))
					})

					It("Update PlatformName=oci results BadRequestError - failure", func() {
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								Platform: &models.Platform{
									External: &models.PlatformExternal{
										PlatformName: swag.String("oci"),
									},
								},
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "Cloud controller manager must be enabled when using oci external integration")
					})
				})

				Context("Single node cluster", func() {
					BeforeEach(func() {
						clusterID = strfmt.UUID(uuid.New().String())
						err := db.Create(&common.Cluster{Cluster: models.Cluster{
							ID:                    &clusterID,
							HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeNone),
							UserManagedNetworking: swag.Bool(true),
							Platform: &models.Platform{
								Type: common.PlatformTypePtr(models.PlatformTypeExternal),
								External: &models.PlatformExternal{
									PlatformName:           &platformName,
									CloudControllerManager: &cloudControllerManager,
								},
							},
						}}).Error
						Expect(err).ShouldNot(HaveOccurred())
					})

					It("Update to vsphere platform while single node cluster - failure", func() {
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "Single node cluster is not supported alongside vsphere platform")
					})

					It("Update to baremetal platform while single node cluster - failure", func() {
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "disabling User Managed Networking or setting platform different than none or oci platforms is not allowed in single node Openshift")
					})

					It("Update to nutanix platform while single node cluster - failure", func() {
						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							},
						})
						verifyApiErrorString(reply, http.StatusBadRequest, "disabling User Managed Networking or setting platform different than none or oci platforms is not allowed in single node Openshift")
					})

					It("Update to none platform while single node cluster - success", func() {
						mockSuccess()
						mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

						reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
							ClusterID: clusterID,
							ClusterUpdateParams: &models.V2ClusterUpdateParams{
								Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
							},
						})
						Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
						actual := reply.(*installer.V2UpdateClusterCreated).Payload
						Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeTrue())
						Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
					})
				})
			})

			Context("Update Platform while Cluster platform is nutanix", func() {
				var cluster *common.Cluster
				BeforeEach(func() {
					clusterID = strfmt.UUID(uuid.New().String())
					cluster = &common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterID,
						HighAvailabilityMode:  swag.String(models.ClusterHighAvailabilityModeFull),
						UserManagedNetworking: swag.Bool(false),
						Platform: &models.Platform{
							Type: common.PlatformTypePtr(models.PlatformTypeNutanix),
						},
						CPUArchitecture: common.X86CPUArchitecture,
					}}
					err := db.Create(cluster).Error
					Expect(err).ShouldNot(HaveOccurred())
					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				})
				It("Update UMN=true - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set nutanix platform with user-managed-networking enabled")
				})

				It("Update platform=nutanix - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNutanix), gomock.Any(), mockUsage)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNutanix))
				})

				It("Update UMN=false and platform=nutanix - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNutanix), gomock.Any(), mockUsage)
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNutanix))
				})

				It("Update platform=external - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeExternal)},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set external platform with user-managed-networking disabled")
				})

				It("Update UMN=false and platform=external results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeExternal)},
							UserManagedNetworking: swag.Bool(false)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set external platform with user-managed-networking disabled")
				})

				It("Update UMN=true and platform=external - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeExternal), gomock.Any(), mockUsage)

					platformName := "platform-name"
					cloudControllerManager := models.PlatformExternalCloudControllerManagerEmpty

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{
								Type: common.PlatformTypePtr(models.PlatformTypeExternal),
								External: &models.PlatformExternal{
									PlatformName:           &platformName,
									CloudControllerManager: &cloudControllerManager,
								},
							},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeTrue())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
					Expect(*actual.Platform.External.PlatformName).Should(Equal(platformName))
					Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
				})

				It("Update platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=true and platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeTrue())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})

				It("Update UMN=false and platform=vsphere - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeVsphere), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(BeFalse())
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
				})
				It("Update platform=baremetal - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
				})

				It("Update UMN=true and platform=baremetal results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							UserManagedNetworking: swag.Bool(true)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set baremetal platform with user-managed-networking enabled")
				})

				It("Update UMN=false and platform=baremetal - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeBaremetal), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
							UserManagedNetworking: swag.Bool(false),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
				})

				It("Update platform=none - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform: &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set none platform with user-managed-networking disabled")
				})

				It("Update UMN=false and platform=none results BadRequestError - failure", func() {
					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
							UserManagedNetworking: swag.Bool(false)},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "Can't set none platform with user-managed-networking disabled")
				})

				It("Update UMN=true and platform=none - success", func() {
					mockSuccess()
					mockProviderRegistry.EXPECT().SetPlatformUsages(commontesting.EqPlatformType(models.PlatformTypeNone), gomock.Any(), mockUsage)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)},
							UserManagedNetworking: swag.Bool(true),
						},
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
					Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
				})
			})
		})

		Context("Feature compatibility", func() {
			Context("OCP Version 4.13", func() {

				createCluster := func(clusterId strfmt.UUID, cpuArchitecture, openshiftVersion string, platformType models.PlatformType, umn bool, highAvailabilityMode string) {
					cluster := &common.Cluster{Cluster: models.Cluster{
						ID:                    &clusterId,
						HighAvailabilityMode:  swag.String(highAvailabilityMode),
						UserManagedNetworking: swag.Bool(umn),
						OpenshiftVersion:      openshiftVersion,
						Platform: &models.Platform{
							Type: common.PlatformTypePtr(platformType),
						},
						CPUArchitecture: cpuArchitecture,
					}}
					err := db.Create(cluster).Error
					Expect(err).ShouldNot(HaveOccurred())
					mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				}

				createInfraEnv := func(clusterId strfmt.UUID, cpuArchitecture string) {
					infraEnvID := strfmt.UUID(uuid.New().String())
					infraEnv := &common.InfraEnv{
						InfraEnv: models.InfraEnv{
							ID:              &infraEnvID,
							ClusterID:       clusterID,
							CPUArchitecture: cpuArchitecture,
						},
					}
					Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())
				}

				createClusterWithInfraEnv := func(clusterId strfmt.UUID, cpuArchitecture, openshiftVersion string, platformType models.PlatformType, umn bool, highAvailabilityMode string) {
					createCluster(clusterID, cpuArchitecture, openshiftVersion, platformType, umn, highAvailabilityMode)
					createInfraEnv(clusterID, cpuArchitecture)
				}

				It("s390x with SNO - compatible on 4.13", func() {
					clusterID = strfmt.UUID(uuid.New().String())
					createClusterWithInfraEnv(clusterID, models.ClusterCPUArchitectureS390x, "4.13", models.PlatformTypeNone, true, models.ClusterHighAvailabilityModeNone)
					mockSuccess()

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							NetworkType: swag.String(models.ClusterNetworkTypeOVNKubernetes),
						},
					})

					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.StringValue(actual.HighAvailabilityMode)).To(Equal(models.ClusterHighAvailabilityModeNone))
				})

				It("s390x with SNO - incompatible on 4.12", func() {
					clusterID = strfmt.UUID(uuid.New().String())
					createClusterWithInfraEnv(clusterID, models.ClusterCPUArchitectureS390x, "4.12", models.PlatformTypeNone, true, models.ClusterHighAvailabilityModeNone)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							NetworkType: swag.String(models.ClusterNetworkTypeOVNKubernetes),
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "cannot use Single Node OpenShift because it's not compatible with the s390x architecture on version 4.12 of OpenShift")
				})

				It("ppc64le with SNO - compatible on 4.13", func() {
					clusterID = strfmt.UUID(uuid.New().String())
					createClusterWithInfraEnv(clusterID, models.ClusterCPUArchitecturePpc64le, "4.13", models.PlatformTypeNone, true, models.ClusterHighAvailabilityModeNone)
					mockSuccess()

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							NetworkType: swag.String(models.ClusterNetworkTypeOVNKubernetes),
						},
					})

					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.StringValue(actual.HighAvailabilityMode)).To(Equal(models.ClusterHighAvailabilityModeNone))
				})

				It("ppc64le with SNO - incompatible on 4.12", func() {
					clusterID = strfmt.UUID(uuid.New().String())
					createClusterWithInfraEnv(clusterID, models.ClusterCPUArchitecturePpc64le, "4.12", models.PlatformTypeNone, true, models.ClusterHighAvailabilityModeNone)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							NetworkType: swag.String(models.ClusterNetworkTypeOVNKubernetes),
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "cannot use Single Node OpenShift because it's not compatible with the ppc64le architecture on version 4.12 of OpenShift")
				})

				It("update ClusterManagedNetworking with s390x - incompatible", func() {
					clusterID = strfmt.UUID(uuid.New().String())
					createClusterWithInfraEnv(clusterID, models.ClusterCPUArchitectureS390x, "4.13", models.PlatformTypeBaremetal, false, models.ClusterHighAvailabilityModeFull)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							NetworkType: swag.String(models.ClusterNetworkTypeOVNKubernetes),
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "cannot use Cluster Managed Networking because it's not compatible with the s390x architecture on version 4.13 of OpenShift")
				})

				It("update ClusterManagedNetworking with arm64 - compatible", func() {
					clusterID = strfmt.UUID(uuid.New().String())
					createClusterWithInfraEnv(clusterID, models.ClusterCPUArchitectureArm64, "4.13", models.PlatformTypeBaremetal, false, models.ClusterHighAvailabilityModeFull)
					mockSuccess()

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							NetworkType: swag.String(models.ClusterNetworkTypeOVNKubernetes),
						},
					})

					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
					actual := reply.(*installer.V2UpdateClusterCreated).Payload
					Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
				})

				It("update CNV operators with ppc64le - incompatible", func() {
					clusterID = strfmt.UUID(uuid.New().String())
					createClusterWithInfraEnv(clusterID, models.ClusterCPUArchitecturePpc64le, "4.13", models.PlatformTypeNone, true, models.ClusterHighAvailabilityModeFull)

					reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
						ClusterID: clusterID,
						ClusterUpdateParams: &models.V2ClusterUpdateParams{
							OlmOperators: []*models.OperatorCreateParams{
								{Name: "cnv"},
							},
						},
					})
					verifyApiErrorString(reply, http.StatusBadRequest, "cannot use OpenShift Virtualization because it's not compatible with the ppc64le architecture on version 4.13 of OpenShift")
				})

			})
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
		orgID1     = "300F3CE2-F122-4DA5-A845-2A4BC5956996"
		orgID2     = "DD71FD12-57FC-4480-917E-6F1900826543"
		userName1  = "test_user_1"
		userName2  = "test_user_2"
		userName3  = "test_user_3"
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
				ID:                   &infraEnvID,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 swag.String(HREF),
				DownloadURL:          DownloadUrl,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())
		})

		Context("DeRegisterInfraEnv", func() {
			It("success", func() {
				mockInfraEnvDeRegisterSuccess()
				mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.InfraEnvDeregisteredEventName),
					eventstest.WithInfraEnvIdMatcher(infraEnvID.String()))).Times(1)
				reply := bm.GetInfraEnv(ctx, installer.GetInfraEnvParams{
					InfraEnvID: infraEnvID,
				})
				_, ok := reply.(*installer.GetInfraEnvOK)
				Expect(ok).To(BeTrue())
				reply = bm.DeregisterInfraEnv(ctx, installer.DeregisterInfraEnvParams{InfraEnvID: infraEnvID})
				Expect(reply).Should(BeAssignableToTypeOf(&installer.DeregisterInfraEnvNoContent{}))
			})

			It("failure - hosts exists", func() {
				hostID := strfmt.UUID(uuid.New().String())
				err := db.Create(&common.Host{
					Host: models.Host{
						ID:         &hostID,
						InfraEnvID: infraEnvID,
					}}).Error
				Expect(err).ShouldNot(HaveOccurred())
				mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.InfraEnvDeregisterFailedEventName),
					eventstest.WithInfraEnvIdMatcher(infraEnvID.String()))).Times(1)
				reply := bm.DeregisterInfraEnv(ctx, installer.DeregisterInfraEnvParams{InfraEnvID: infraEnvID})
				Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusConflict, errors.Errorf(""))))
			})
		})
	})

	Context("List", func() {
		var (
			clusterID   strfmt.UUID
			infraEnvID2 strfmt.UUID
		)

		BeforeEach(func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:                   &infraEnvID,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 swag.String(HREF),
				DownloadURL:          DownloadUrl,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			infraEnvID2 = strfmt.UUID(uuid.New().String())
			clusterID = strfmt.UUID(uuid.New().String())
			err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:                   &infraEnvID2,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 swag.String(HREF),
				DownloadURL:          DownloadUrl,
				ClusterID:            clusterID,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("returns all infraenvs", func() {
			resp := bm.ListInfraEnvs(ctx, installer.ListInfraEnvsParams{})
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewListInfraEnvsOK()))
			payload := resp.(*installer.ListInfraEnvsOK).Payload
			Expect(len(payload)).Should(Equal(2))
			ids := []strfmt.UUID{*payload[0].ID, *payload[1].ID}
			Expect(ids).To(ContainElement(infraEnvID))
			Expect(ids).To(ContainElement(infraEnvID2))
		})

		It("filters by cluster id", func() {
			resp := bm.ListInfraEnvs(ctx, installer.ListInfraEnvsParams{ClusterID: &clusterID})
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewListInfraEnvsOK()))
			payload := resp.(*installer.ListInfraEnvsOK).Payload
			Expect(len(payload)).Should(Equal(1))
			Expect(*payload[0].ID).To(Equal(infraEnvID2))
		})
	})

	Context("Filter based on organization ID", func() {
		var authCtx context.Context

		BeforeEach(func() {
			cfg := auth.GetConfigRHSSO()
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)
			bm.authzHandler = auth.NewAuthzHandler(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)

			payload := &ocm.AuthPayload{Role: ocm.UserRole}
			payload.Username = userName1
			payload.Organization = orgID1
			authCtx = context.WithValue(ctx, restapi.AuthKey, payload)
		})

		It("multiple users in a single organization", func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:       &infraEnvID,
				OrgID:    orgID1,
				UserName: userName1,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			infraEnvID = strfmt.UUID(uuid.New().String())
			err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:       &infraEnvID,
				OrgID:    orgID1,
				UserName: userName2,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			resp := bm.ListInfraEnvs(authCtx, installer.ListInfraEnvsParams{})
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewListInfraEnvsOK()))
			payload := resp.(*installer.ListInfraEnvsOK).Payload
			Expect(len(payload)).Should(Equal(2))
			Expect(payload[0].OrgID).Should(Equal(orgID1))
			Expect(payload[1].OrgID).Should(Equal(orgID1))
		})

		It("multiple organizations", func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:       &infraEnvID,
				OrgID:    orgID1,
				UserName: userName1,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			infraEnvID = strfmt.UUID(uuid.New().String())
			err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:       &infraEnvID,
				OrgID:    orgID2,
				UserName: userName2,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			resp := bm.ListInfraEnvs(authCtx, installer.ListInfraEnvsParams{})
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewListInfraEnvsOK()))
			payload := resp.(*installer.ListInfraEnvsOK).Payload
			Expect(len(payload)).Should(Equal(1))
			Expect(payload[0].OrgID).Should(Equal(orgID1))
		})
	})

	Context("Filter by owner query param", func() {
		var authCtx context.Context

		BeforeEach(func() {
			cfg := auth.GetConfigRHSSO()
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)
			bm.authzHandler = auth.NewAuthzHandler(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)

			payload := &ocm.AuthPayload{Role: ocm.UserRole}
			payload.Username = userName1
			payload.Organization = orgID1
			authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:       &infraEnvID,
				OrgID:    orgID1,
				UserName: userName1,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			infraEnvID = strfmt.UUID(uuid.New().String())
			err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:       &infraEnvID,
				OrgID:    orgID1,
				UserName: userName2,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			infraEnvID = strfmt.UUID(uuid.New().String())
			err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:       &infraEnvID,
				OrgID:    orgID2,
				UserName: userName3,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("Owner query param is not specified", func() {
			params := installer.ListInfraEnvsParams{}
			resp := bm.ListInfraEnvs(authCtx, params)
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewListInfraEnvsOK()))
			payload := resp.(*installer.ListInfraEnvsOK).Payload
			Expect(len(payload)).Should(Equal(2))
		})

		It("Owner query param is specified - user in org", func() {
			params := installer.ListInfraEnvsParams{}
			params.Owner = &userName1
			resp := bm.ListInfraEnvs(authCtx, params)
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewListInfraEnvsOK()))
			payload := resp.(*installer.ListInfraEnvsOK).Payload
			Expect(len(payload)).Should(Equal(1))
			Expect(payload[0].UserName).Should(Equal(userName1))
		})

		It("Owner query param is specified - user not in org", func() {
			params := installer.ListInfraEnvsParams{}
			params.Owner = &userName3
			resp := bm.ListInfraEnvs(authCtx, params)
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewListInfraEnvsOK()))
			payload := resp.(*installer.ListInfraEnvsOK).Payload
			Expect(len(payload)).Should(Equal(0))
		})
	})

	Context("List Infra Env Hosts", func() {
		var (
			infraEnvId1, infraEnvId2 strfmt.UUID
		)
		BeforeEach(func() {
			infraEnvId1 = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:                   &infraEnvId1,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 swag.String(HREF),
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
				ID:                   &infraEnvId2,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 swag.String(HREF),
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
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(5)
				resp := bm.V2ListHosts(ctx, installer.V2ListHostsParams{
					InfraEnvID: infraEnvId1,
				})
				payload := resp.(*installer.V2ListHostsOK).Payload
				for i := range payload {
					Expect(payload[i].RequestedHostname).Should(Equal(payload[i].ID.String()))
				}
				Expect(len(payload)).Should(Equal(2))
				resp = bm.V2ListHosts(ctx, installer.V2ListHostsParams{
					InfraEnvID: infraEnvId2,
				})
				payload = resp.(*installer.V2ListHostsOK).Payload
				Expect(len(payload)).Should(Equal(3))
			})

			It("InfraEnv does not exist", func() {
				resp := bm.V2ListHosts(ctx, installer.V2ListHostsParams{
					InfraEnvID: strfmt.UUID(uuid.New().String()),
				})
				verifyApiError(resp, http.StatusNotFound)
			})
		})
	})

	Context("Get", func() {
		BeforeEach(func() {
			infraEnvID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:                   &infraEnvID,
				OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
				AdditionalNtpSources: AdditionalNtpSources,
				Href:                 swag.String(HREF),
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
				Expect(actual.Payload.Href).To(Equal(swag.String(HREF)))
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
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)
			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
					PullSecret:       swag.String(fakePullSecret),
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterInfraEnvCreated())))
			actual := reply.(*installer.RegisterInfraEnvCreated)
			Expect(*actual.Payload.Name).To(Equal("some-infra-env-name"))

			var dbInfraEnv common.InfraEnv
			Expect(db.First(&dbInfraEnv, "id = ?", actual.Payload.ID.String()).Error).To(Succeed())
			Expect(dbInfraEnv.ImageTokenKey).NotTo(Equal(""))
		})

		It("happy flow with kernel parameters", func() {
			mockInfraEnvRegisterSuccess()
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)
			kernelArguments := models.KernelArguments{
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     "p1",
				},
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     "p2",
				},
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     "p3",
				},
			}
			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
					PullSecret:       swag.String(fakePullSecret),
					KernelArguments:  kernelArguments,
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterInfraEnvCreated())))
			actual := reply.(*installer.RegisterInfraEnvCreated)

			var dbInfraEnv common.InfraEnv
			Expect(db.First(&dbInfraEnv, "id = ?", actual.Payload.ID.String()).Error).To(Succeed())
			Expect(dbInfraEnv.ImageTokenKey).NotTo(Equal(""))
			jsonEncodedKernelParameters, err := json.Marshal(&kernelArguments)
			Expect(err).ToNot(HaveOccurred())
			Expect(swag.StringValue(dbInfraEnv.KernelArguments)).To(Equal(string(jsonEncodedKernelParameters)))
		})
		It("happy flow with kernel parameters + cluster and usage", func() {
			mockInfraEnvRegisterSuccess()
			cluster := createCluster(db, models.ClusterStatusInsufficient)
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)
			mockUsage.EXPECT().Add(gomock.Any(), gomock.Not(usage.DiscoveryKernelArgumentsUsage), gomock.Any()).AnyTimes()
			mockUsage.EXPECT().Remove(gomock.Any(), gomock.Not(usage.DiscoveryKernelArgumentsUsage)).AnyTimes()
			mockUsage.EXPECT().Add(gomock.Any(), usage.DiscoveryKernelArgumentsUsage, nil)
			mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).MinTimes(1)

			kernelArguments := models.KernelArguments{
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     "p1",
				},
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     "p2",
				},
				{
					Operation: models.KernelArgumentOperationAppend,
					Value:     "p3",
				},
			}
			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
					PullSecret:       swag.String("{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"),
					KernelArguments:  kernelArguments,
					ClusterID:        cluster.ID,
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterInfraEnvCreated())))
			actual := reply.(*installer.RegisterInfraEnvCreated)

			var dbInfraEnv common.InfraEnv
			Expect(db.First(&dbInfraEnv, "id = ?", actual.Payload.ID.String()).Error).To(Succeed())
			Expect(dbInfraEnv.ImageTokenKey).NotTo(Equal(""))
			jsonEncodedKernelParameters, err := json.Marshal(&kernelArguments)
			Expect(err).ToNot(HaveOccurred())
			Expect(swag.StringValue(dbInfraEnv.KernelArguments)).To(Equal(string(jsonEncodedKernelParameters)))
		})

		It("sets the ignition config override feature usage when given a valid override", func() {
			override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			clusterID := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
				CPUArchitecture:  "x86_64",
				Platform:         &models.Platform{Type: models.NewPlatformType(models.PlatformTypeBaremetal)},
			}}
			err := db.Create(&cluster).Error
			Expect(err).ShouldNot(HaveOccurred())
			mockInfraEnvRegisterSuccess()
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).AnyTimes()
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)
			mockUsage.EXPECT().Add(gomock.Any(), usage.IgnitionConfigOverrideUsage, gomock.Any()).Times(1)
			mockUsage.EXPECT().Add(gomock.Any(), gomock.Not(usage.IgnitionConfigOverrideUsage), gomock.Any()).AnyTimes()
			mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			mockUsage.EXPECT().Remove(gomock.Any(), usage.IgnitionConfigOverrideUsage).Times(0)
			mockUsage.EXPECT().Remove(gomock.Any(), gomock.Not(usage.IgnitionConfigOverrideUsage)).AnyTimes()

			bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:                   swag.String("some-infra-env-name"),
					PullSecret:             swag.String(fakePullSecret),
					OpenshiftVersion:       MinimalOpenShiftVersionForNoneHA,
					IgnitionConfigOverride: override,
					ClusterID:              &clusterID,
				},
			})
		})

		It("doesn't set the ignition config override feature usage when no cluster is attached to the infra-env", func() {
			override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"

			mockInfraEnvRegisterSuccess()
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).AnyTimes()
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)
			mockUsage.EXPECT().Add(gomock.Any(), usage.IgnitionConfigOverrideUsage, gomock.Any()).Times(0)
			mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockUsage.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(0)

			bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:                   swag.String("some-infra-env-name"),
					PullSecret:             swag.String(fakePullSecret),
					OpenshiftVersion:       MinimalOpenShiftVersionForNoneHA,
					IgnitionConfigOverride: override,
				},
			})
		})

		It("doesn't set the ignition config override feature usage when no override is given", func() {
			override := ""
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			clusterID := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
				CPUArchitecture:  "x86_64",
				Platform:         &models.Platform{Type: models.NewPlatformType(models.PlatformTypeBaremetal)},
			}}
			err := db.Create(&cluster).Error
			Expect(err).ShouldNot(HaveOccurred())
			mockInfraEnvRegisterSuccess()
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)
			mockUsage.EXPECT().Add(gomock.Any(), usage.IgnitionConfigOverrideUsage, gomock.Any()).Times(0)
			mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockUsage.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(0)

			bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:                   swag.String("some-infra-env-name"),
					PullSecret:             swag.String(fakePullSecret),
					OpenshiftVersion:       MinimalOpenShiftVersionForNoneHA,
					IgnitionConfigOverride: override,
				},
			})
		})

		It("Create with ClusterID - CPU architecture match", func() {
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"

			clusterID := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
				CPUArchitecture:  "x86_64",
				Platform:         &models.Platform{Type: models.NewPlatformType(models.PlatformTypeBaremetal)},
			}}
			err := db.Create(&cluster).Error
			Expect(err).ShouldNot(HaveOccurred())
			mockUsageReports()
			mockInfraEnvRegisterSuccess()
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)
			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
					PullSecret:       swag.String(fakePullSecret),
					ClusterID:        &clusterID,
					CPUArchitecture:  "x86_64",
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterInfraEnvCreated())))
			actual := reply.(*installer.RegisterInfraEnvCreated)
			Expect(*actual.Payload.Name).To(Equal("some-infra-env-name"))
		})

		It("No version specified", func() {
			mockOSImages.EXPECT().GetOsImageOrLatest(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
			mockOSImages.EXPECT().GetOsImageOrLatest(
				*common.TestDefaultConfig.OsImage.OpenshiftVersion,
				*common.TestDefaultConfig.OsImage.CPUArchitecture).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("", nil).Times(1)
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).Times(1)

			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName)))
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName)))
			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:       swag.String("some-infra-env-name"),
					PullSecret: swag.String(fakePullSecret),
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterInfraEnvCreated())))
			actual := reply.(*installer.RegisterInfraEnvCreated)
			Expect(*actual.Payload.Name).To(Equal("some-infra-env-name"))
		})

		It("Create with ClusterID - CPU architecture mismatch", func() {
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"

			clusterID := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
				CPUArchitecture:  "x86_64",
				Platform:         &models.Platform{Type: models.NewPlatformType(models.PlatformTypeBaremetal)},
			}}
			err := db.Create(&cluster).Error
			Expect(err).ShouldNot(HaveOccurred())

			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegistrationFailedEventName),
				eventstest.WithMessageContainsMatcher("Specified CPU architecture (arm64) doesn't match the cluster (x86_64)"))).Times(1)

			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
					PullSecret:       swag.String(fakePullSecret),
					ClusterID:        &clusterID,
					CPUArchitecture:  common.ARM64CPUArchitecture,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf(""))))
		})

		It("fail to create with multiarch Cluster and missing release image", func() {
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"

			clusterID := strfmt.UUID(uuid.New().String())
			cluster := common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
				CPUArchitecture:  common.MultiCPUArchitecture,
				Platform:         &models.Platform{Type: models.NewPlatformType(models.PlatformTypeBaremetal)},
			}}
			err := db.Create(&cluster).Error
			Expect(err).ShouldNot(HaveOccurred())

			mockVersions.EXPECT().ValidateReleaseImageForRHCOS(gomock.Any(), gomock.Any()).Return(
				errors.Errorf("chocobomb-architecture is a nonsense architecture")).Times(1)
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegistrationFailedEventName))).Times(1)

			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
					PullSecret:       swag.String(fakePullSecret),
					ClusterID:        &clusterID,
					CPUArchitecture:  "chocobomb-architecture",
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf(""))))
		})

		It("Invalid Ignition - too recent", func() {
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			override := `{"ignition": {"version": "9.9.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
			mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegistrationFailedEventName),
				eventstest.WithMessageContainsMatcher("error parsing ignition: unsupported config version"))).Times(1)
			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:                   swag.String("some-infra-env-name"),
					OpenshiftVersion:       MinimalOpenShiftVersionForNoneHA,
					PullSecret:             swag.String(fakePullSecret),
					IgnitionConfigOverride: override,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf(""))))
		})

		It("static network usage should be removed when StaticNetworkConfig is unset", func() {
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			staticNetworkConfig := []*models.HostStaticNetworkConfig{}

			cluster := createCluster(db, models.ClusterStatusInstallingPendingUserAction)

			mockInfraEnvRegisterSuccess()
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)
			mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(gomock.Any())
			mockUsage.EXPECT().Add(gomock.Any(), gomock.Not(usage.StaticNetworkConfigUsage), gomock.Any()).AnyTimes()
			mockUsage.EXPECT().Remove(gomock.Any(), usage.StaticNetworkConfigUsage).Times(1)
			mockUsage.EXPECT().Remove(gomock.Any(), gomock.Not(usage.StaticNetworkConfigUsage)).AnyTimes()
			mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).MinTimes(1)

			bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:                swag.String("some-infra-env-name"),
					PullSecret:          swag.String(fakePullSecret),
					OpenshiftVersion:    MinimalOpenShiftVersionForNoneHA,
					StaticNetworkConfig: staticNetworkConfig,
					ClusterID:           cluster.ID,
				},
			})
		})

		It("static network usage should be added when StaticNetworkConfig is set", func() {
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			staticNetworkConfig := []*models.HostStaticNetworkConfig{}

			cluster := createCluster(db, models.ClusterStatusInstallingPendingUserAction)

			mockOSImages.EXPECT().GetOsImageOrLatest(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
			mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).Times(1)
			mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName))).AnyTimes()
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)
			mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(gomock.Any())

			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("static network format result", nil).Times(1)
			mockUsage.EXPECT().Add(gomock.Any(), usage.StaticNetworkConfigUsage, nil)
			mockUsage.EXPECT().Add(gomock.Any(), gomock.Not(usage.StaticNetworkConfigUsage), gomock.Any()).AnyTimes()
			mockUsage.EXPECT().Remove(gomock.Any(), gomock.Not(usage.StaticNetworkConfigUsage)).AnyTimes()
			mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).MinTimes(1)

			bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:                swag.String("some-infra-env-name"),
					PullSecret:          swag.String(fakePullSecret),
					OpenshiftVersion:    MinimalOpenShiftVersionForNoneHA,
					StaticNetworkConfig: staticNetworkConfig,
					ClusterID:           cluster.ID,
				},
			})
		})

		It("returns a bad request when provided config is too large (>256 KiB)", func() {
			// Create a large content for the ignition config
			contentBytes := make([]byte, validations.IgnitionImageSizePadding)
			_, err := rand.Read(contentBytes)
			Expect(err).ToNot(HaveOccurred())
			content := base64.StdEncoding.EncodeToString(contentBytes)
			largeDiscoveryIgnition := fmt.Sprintf(`{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,%s"}}]}}`, content)
			override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`

			mockOSImages.EXPECT().GetOsImageOrLatest(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
			mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(gomock.Any()).Return("", nil).Times(1)
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(largeDiscoveryIgnition, nil).AnyTimes()
			mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegistrationFailedEventName))).Times(1)

			reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:                   swag.String("some-infra-env-name"),
					PullSecret:             swag.String(fakePullSecret),
					IgnitionConfigOverride: override,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf(""))))
			err = reply.(*common.ApiErrorResponse)
			Expect(err.Error()).To(ContainSubstring("over the maximum allowable size"))
		})

		It("Invalid additional trust bundle - too long", func() {
			bytes := make([]byte, 65536)
			longString := string(bytes)
			params := &models.InfraEnvCreateParams{
				AdditionalTrustBundle: longString,
			}
			err := params.Validate(nil)
			Expect(err.Error()).To(ContainSubstring("should be at most 65535 chars long"))
		})
	})

	Context("Create InfraEnv - with rhsso auth", func() {
		var (
			authCtx      context.Context
			clusterID    strfmt.UUID
			mockOcmAuthz *ocm.MockOCMAuthorization
			payload      *ocm.AuthPayload
		)

		BeforeEach(func() {
			db, dbName = common.PrepareTestDB()
			clusterID = strfmt.UUID(uuid.New().String())

			cfg := auth.GetConfigRHSSO()
			bm = createInventory(db, Config{})
			mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)
			mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
			bm.authzHandler = auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
			payload = &ocm.AuthPayload{Role: ocm.UserRole}

			err := db.Create(&common.Cluster{
				Cluster: models.Cluster{
					ID:       &clusterID,
					Kind:     swag.String(models.ClusterKindCluster),
					UserName: userName1}}).Error
			Expect(err).ShouldNot(HaveOccurred())
		})

		AfterEach(func() {
			common.DeleteTestDB(db, dbName)
		})

		It("successful creation - cluster owner", func() {
			mockUsageReports()
			mockInfraEnvRegisterSuccess()
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			payload.Username = userName1
			authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

			mockEvents.EXPECT().SendInfraEnvEvent(authCtx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)

			reply := bm.RegisterInfraEnv(authCtx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					ClusterID:        &clusterID,
					OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
					PullSecret:       swag.String(fakePullSecret),
					ImageType:        models.ImageTypeFullIso,
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterInfraEnvCreated())))
			actual := reply.(*installer.RegisterInfraEnvCreated)
			Expect(*actual.Payload.Name).To(Equal("some-infra-env-name"))

			var dbInfraEnv common.InfraEnv
			Expect(db.First(&dbInfraEnv, "id = ?", actual.Payload.ID.String()).Error).To(Succeed())
			Expect(dbInfraEnv.ImageTokenKey).NotTo(Equal(""))
		})

		It("successful creation - clusterEditor", func() {
			mockUsageReports()
			mockInfraEnvRegisterSuccess()
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"

			payload.Username = userName2
			authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

			mockEvents.EXPECT().SendInfraEnvEvent(authCtx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegisteredEventName))).Times(1)
			mockOcmAuthz.EXPECT().AccessReview(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil)

			reply := bm.RegisterInfraEnv(authCtx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					ClusterID:        &clusterID,
					OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
					PullSecret:       swag.String(fakePullSecret),
					ImageType:        models.ImageTypeFullIso,
				},
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewRegisterInfraEnvCreated())))
			actual := reply.(*installer.RegisterInfraEnvCreated)
			Expect(*actual.Payload.Name).To(Equal("some-infra-env-name"))

			var dbInfraEnv common.InfraEnv
			Expect(db.First(&dbInfraEnv, "id = ?", actual.Payload.ID.String()).Error).To(Succeed())
			Expect(dbInfraEnv.ImageTokenKey).NotTo(Equal(""))
		})

		It("no access to specified cluster (can't update)", func() {
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			payload.Username = userName2
			authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

			mockEvents.EXPECT().SendInfraEnvEvent(authCtx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegistrationFailedEventName))).Times(1)
			mockOcmAuthz.EXPECT().AccessReview(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)

			reply := bm.RegisterInfraEnv(authCtx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					ClusterID:        &clusterID,
					OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
					PullSecret:       swag.String(fakePullSecret),
				},
			})
			verifyApiError(reply, http.StatusForbidden)
		})

		It("no access to specified cluster (can't read)", func() {
			MinimalOpenShiftVersionForNoneHA := "4.8.0-fc.0"
			payload.Username = userName2
			payload.Organization = "another_org"
			authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

			mockEvents.EXPECT().SendInfraEnvEvent(authCtx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.InfraEnvRegistrationFailedEventName))).Times(1)
			mockOcmAuthz.EXPECT().AccessReview(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)

			reply := bm.RegisterInfraEnv(authCtx, installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("some-infra-env-name"),
					ClusterID:        &clusterID,
					OpenshiftVersion: MinimalOpenShiftVersionForNoneHA,
					PullSecret:       swag.String(fakePullSecret),
				},
			})
			verifyApiError(reply, http.StatusNotFound)
		})
	})

	Context("Update", func() {
		Context("Update infraEnv", func() {
			infraEnvName := "some-infra-env"
			var (
				i   *common.InfraEnv
				err error
			)
			BeforeEach(func() {
				// TODO: specific event
				mockEvents.EXPECT().SendInfraEnvEvent(ctx, gomock.Any()).AnyTimes()
				mockInfraEnvRegisterSuccess()
				reply := bm.RegisterInfraEnv(ctx, installer.RegisterInfraEnvParams{
					InfraenvCreateParams: &models.InfraEnvCreateParams{
						Name:             swag.String(infraEnvName),
						OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
						PullSecret:       swag.String(fakePullSecret),
					},
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewRegisterInfraEnvCreated()))
				actual := reply.(*installer.RegisterInfraEnvCreated)
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *actual.Payload.ID})
				Expect(err).ToNot(HaveOccurred())
				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("generated_at", strfmt.DateTime(time.Now().AddDate(0, 0, -1))).Error
				Expect(err).ToNot(HaveOccurred())
			})
			Context("Update discovery kernel arguments", func() {
				jsonEncodeKernelArguments := func(array models.KernelArguments) string {
					b, e := json.Marshal(&array)
					Expect(e).ToNot(HaveOccurred())
					return string(b)
				}
				ka1 := models.KernelArguments{
					{
						Operation: models.KernelArgumentOperationAppend,
						Value:     "p1",
					},
					{
						Operation: models.KernelArgumentOperationAppend,
						Value:     "p2",
					},
				}
				ka2 := models.KernelArguments{
					{
						Operation: models.KernelArgumentOperationAppend,
						Value:     "p3",
					},
					{
						Operation: models.KernelArgumentOperationAppend,
						Value:     "p4",
					},
					{
						Operation: models.KernelArgumentOperationAppend,
						Value:     "p1",
					},
					{
						Operation: models.KernelArgumentOperationAppend,
						Value:     "p5",
					},
				}
				DescribeTable("Update discovery kernel arguments scenarios",
					func(initialKernelArguments, updateKernelArguments, expectedKernelArguments models.KernelArguments) {
						mockInfraEnvUpdateSuccess()
						Expect(i.KernelArguments).To(BeNil())
						if initialKernelArguments != nil {
							updateReply := db.Model(&models.InfraEnv{}).Where("id = ?", i.ID.String()).
								Update("kernel_arguments", jsonEncodeKernelArguments(initialKernelArguments))
							Expect(updateReply.Error).ToNot(HaveOccurred())
							Expect(updateReply.RowsAffected).To(Equal(int64(1)))
						}
						reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
							InfraEnvID: *i.ID,
							InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
								KernelArguments: updateKernelArguments,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
						i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
						Expect(err).ToNot(HaveOccurred())
						if expectedKernelArguments == nil {
							Expect(i.KernelArguments).To(BeNil())
						} else {
							Expect(swag.StringValue(i.KernelArguments)).To(Equal(jsonEncodeKernelArguments(expectedKernelArguments)))
						}
					},
					Entry("update discovery kernel arguments when none were previously set", nil, ka1, ka1),
					Entry("overwrites existing discovery kernel arguments with new ones", ka1, ka2, ka2),
					Entry("clears existing kernel arguments", ka2, models.KernelArguments{}, nil),
				)
				Context("kernel arguments with usage", func() {
					var cluster *common.Cluster
					BeforeEach(func() {
						cluster = createCluster(db, models.ClusterStatusInsufficient)
						Expect(db.Model(&models.InfraEnv{}).Where("id = ?", i.ID.String()).Update("cluster_id", cluster.ID.String()).Error).ToNot(HaveOccurred())
					})
					It("add kernel arguments", func() {
						mockInfraEnvUpdateSuccess()
						mockUsage.EXPECT().Add(gomock.Any(), gomock.Not(usage.DiscoveryKernelArgumentsUsage), gomock.Any()).AnyTimes()
						mockUsage.EXPECT().Remove(gomock.Any(), gomock.Not(usage.DiscoveryKernelArgumentsUsage)).AnyTimes()
						mockUsage.EXPECT().Add(gomock.Any(), usage.DiscoveryKernelArgumentsUsage, nil)
						mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).MinTimes(1)
						kargs := models.KernelArguments{
							{
								Operation: models.KernelArgumentOperationAppend,
								Value:     "p1",
							},
						}
						reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
							InfraEnvID: *i.ID,
							InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
								KernelArguments: kargs,
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
					})
					It("clear kernel arguments", func() {
						Expect(db.Model(&models.InfraEnv{}).Where("id = ?", i.ID.String()).
							Update("kernel_arguments", `[{"operation": "append", "value": "p1"}]`).Error).ToNot(HaveOccurred())
						mockInfraEnvUpdateSuccess()
						mockUsage.EXPECT().Add(gomock.Any(), gomock.Not(usage.DiscoveryKernelArgumentsUsage), gomock.Any()).AnyTimes()
						mockUsage.EXPECT().Remove(gomock.Any(), gomock.Not(usage.DiscoveryKernelArgumentsUsage)).AnyTimes()
						mockUsage.EXPECT().Remove(gomock.Any(), usage.DiscoveryKernelArgumentsUsage)
						mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).MinTimes(1)
						reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
							InfraEnvID: *i.ID,
							InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
								KernelArguments: make(models.KernelArguments, 0),
							},
						})
						Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
					})
				})
			})
			It("Update AdditionalNtpSources", func() {
				mockInfraEnvUpdateSuccess()
				Expect(i.AdditionalNtpSources).To(Equal(""))
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						AdditionalNtpSources: swag.String("1.1.1.1"),
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				var err error
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.AdditionalNtpSources).ToNot(Equal(nil))
				Expect(i.AdditionalNtpSources).To(Equal("1.1.1.1"))
			})
			It("Update Ignition", func() {
				mockInfraEnvUpdateSuccess()
				mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).AnyTimes()
				override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						IgnitionConfigOverride: override,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				var err error
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.IgnitionConfigOverride).To(Equal(override))
			})
			It("Update Image type", func() {
				var err error
				mockInfraEnvUpdateSuccess()
				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("type", models.ImageTypeMinimalIso).Error
				Expect(err).ToNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						ImageType: models.ImageTypeFullIso,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(common.ImageTypeValue(i.Type)).To(Equal(models.ImageTypeFullIso))
			})
			It("Update Image type on s390x architecture", func() {
				var err error
				mockOSImages.EXPECT().GetOsImageOrLatest(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
				mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
				mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName))).AnyTimes()

				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("cpu_architecture", models.ClusterCPUArchitectureS390x).Error
				Expect(err).ToNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						ImageType: "",
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(common.ImageTypeValue(i.Type)).To(Equal(models.ImageTypeFullIso))
			})
			It("Update Image type on s390x architecture to full - fail", func() {
				var err error
				mockOSImages.EXPECT().GetOsImageOrLatest(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
				mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).AnyTimes()
				mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName))).AnyTimes()

				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("cpu_architecture", models.ClusterCPUArchitectureS390x).Error
				Expect(err).ToNot(HaveOccurred())

				clusterID := strfmt.UUID(uuid.New().String())
				c := &common.Cluster{
					Cluster: models.Cluster{
						ID:                   &clusterID,
						HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeFull),
						OpenshiftVersion:     "4.12",
						CPUArchitecture:      models.ClusterCPUArchitectureS390x,
						Platform: &models.Platform{
							Type: common.PlatformTypePtr(models.PlatformTypeNone),
						},
						UserManagedNetworking: swag.Bool(true),
					},
				}
				Expect(db.Create(c).Error).ToNot(HaveOccurred())

				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("cluster_id", clusterID).Error
				Expect(err).ToNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						ImageType: models.ImageTypeMinimalIso,
					},
				})
				Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				Expect(reply.(*common.ApiErrorResponse).Error()).To(Equal("cannot use Minimal ISO because it's not compatible with the s390x architecture on version 4.12 of OpenShift"))
			})
			It("Update Image type on ppc64le architecture", func() {
				var err error
				mockInfraEnvUpdateSuccess()

				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("type", models.ImageTypeMinimalIso).Error
				Expect(err).ToNot(HaveOccurred())
				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("cpu_architecture", models.ClusterCPUArchitecturePpc64le).Error
				Expect(err).ToNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						ImageType: "",
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(common.ImageTypeValue(i.Type)).To(Equal(models.ImageTypeMinimalIso))
			})
			It("Update additional trust bundle", func() {
				var err error
				mockInfraEnvUpdateSuccess()

				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("additional_trust_bundle", testCert).Error
				Expect(err).ToNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						// New lines added before and after to test that they are ignored
						AdditionalTrustBundle: swag.String(fmt.Sprintf("\n%s\n\n", testCert2)),
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.AdditionalTrustBundle).To(Equal(testCert2))
			})
			It("Update additional trust bundle - invalid", func() {
				err := db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("additional_trust_bundle", testCert).Error
				Expect(err).ToNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						// New lines added before and after to test that they are ignored
						AdditionalTrustBundle: swag.String("somejunk"),
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
				Expect(reply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
			})
			It("Update additional trust bundle - empty", func() {
				var err error
				mockInfraEnvUpdateSuccess()

				err = db.Model(&common.InfraEnv{}).Where("id = ?", i.ID).Update("additional_trust_bundle", testCert).Error
				Expect(err).ToNot(HaveOccurred())
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						// New lines added before and after to test that they are ignored
						AdditionalTrustBundle: swag.String(""),
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.AdditionalTrustBundle).To(Equal(""))
			})
			It("Update additional trust bundle - too long", func() {
				bytes := make([]byte, 65536)
				longString := string(bytes)
				params := &models.InfraEnvUpdateParams{
					AdditionalTrustBundle: swag.String(longString),
				}
				err := params.Validate(nil)
				Expect(err.Error()).To(ContainSubstring("should be at most 65535 chars long"))
			})
			It("updates proxy when http and https are the same", func() {
				var err error
				mockInfraEnvUpdateSuccess()
				proxyURL := "http://[1001:db9::1]:3129"
				noProxy := ".test-infra-cluster-b0cea5e8.redhat.com,1001:db9::/120,2002:db8::/53,2003:db8::/112"
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						Proxy: &models.Proxy{
							HTTPProxy:  swag.String(proxyURL),
							HTTPSProxy: swag.String(proxyURL),
							NoProxy:    swag.String(noProxy),
						},
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.Proxy).ToNot(BeNil())
				Expect(swag.StringValue(i.Proxy.HTTPProxy)).To(Equal(proxyURL))
				Expect(swag.StringValue(i.Proxy.HTTPSProxy)).To(Equal(proxyURL))
				Expect(swag.StringValue(i.Proxy.NoProxy)).To(Equal(noProxy))
			})
			It("doesn't set https proxy when not provided", func() {
				var err error
				mockInfraEnvUpdateSuccess()
				proxyURL := "http://[1001:db9::1]:3129"
				noProxy := ".test-infra-cluster-b0cea5e8.redhat.com,1001:db9::/120,2002:db8::/53,2003:db8::/112"
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						Proxy: &models.Proxy{
							HTTPProxy: swag.String(proxyURL),
							NoProxy:   swag.String(noProxy),
						},
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.Proxy).ToNot(BeNil())
				Expect(swag.StringValue(i.Proxy.HTTPProxy)).To(Equal(proxyURL))
				Expect(swag.StringValue(i.Proxy.HTTPSProxy)).To(BeEmpty())
				Expect(swag.StringValue(i.Proxy.NoProxy)).To(Equal(noProxy))
			})

			It("updates proxy when http and https are different", func() {
				var err error
				mockInfraEnvUpdateSuccess()
				proxyURL1 := "http://[1001:db9::1]:3129"
				proxyURL2 := "http://[1001:db9::1]:3130"
				noProxy := ".test-infra-cluster-b0cea5e8.redhat.com,1001:db9::/120,2002:db8::/53,2003:db8::/112"
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						Proxy: &models.Proxy{
							HTTPProxy:  swag.String(proxyURL1),
							HTTPSProxy: swag.String(proxyURL2),
							NoProxy:    swag.String(noProxy),
						},
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.Proxy).ToNot(BeNil())
				Expect(swag.StringValue(i.Proxy.HTTPProxy)).To(Equal(proxyURL1))
				Expect(swag.StringValue(i.Proxy.HTTPSProxy)).To(Equal(proxyURL2))
				Expect(swag.StringValue(i.Proxy.NoProxy)).To(Equal(noProxy))
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
				mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes, nil).Times(1)
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						StaticNetworkConfig: staticNetworkConfig,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				var err error
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.StaticNetworkConfig).To(Equal(staticNetworkFormatRes))
			})
			It("Update StaticNetwork same", func() {
				var err error
				err = db.Model(&common.InfraEnv{}).Where("id = ?", *i.ID).Update("static_network_config", "static network format result").Error
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
				mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes, nil).Times(1)
				reply := bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						StaticNetworkConfig: staticNetworkConfig,
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))
				i, err = bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: *i.ID})
				Expect(err).ToNot(HaveOccurred())
				Expect(i.StaticNetworkConfig).To(Equal(staticNetworkFormatRes))
			})

			It("static network usage should be added when StaticNetworkConfig is set", func() {
				var err error

				cluster := createCluster(db, models.ClusterStatusInstallingPendingUserAction)
				err = db.Model(&common.InfraEnv{}).Where("id = ?", *i.ID).Update("cluster_id", cluster.ID).Error
				Expect(err).ToNot(HaveOccurred())

				staticNetworkFormatRes := "static network format result"
				map1 := models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{MacAddress: "mac10", LogicalNicName: "nic10"},
				}
				staticNetworkConfig := []*models.HostStaticNetworkConfig{
					common.FormatStaticConfigHostYAML("0200003ef74c", "02000048ba48", "192.168.126.41", "192.168.141.41", "192.168.126.1", map1),
				}

				mockInfraEnvUpdateSuccess()
				mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(staticNetworkConfig).Return(nil).Times(1)
				mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes, nil).Times(1)
				mockUsage.EXPECT().Add(gomock.Any(), usage.StaticNetworkConfigUsage, nil).Times(1)
				mockUsage.EXPECT().Save(gomock.Any(), *cluster.ID, gomock.Any()).Times(1)

				bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						StaticNetworkConfig: staticNetworkConfig,
					},
				})
			})

			It("static network usage should be removed when StaticNetworkConfig is unset", func() {
				var err error

				cluster := createCluster(db, models.ClusterStatusInstallingPendingUserAction)
				err = db.Model(&common.InfraEnv{}).Where("id = ?", *i.ID).Update("cluster_id", cluster.ID).Error
				Expect(err).ToNot(HaveOccurred())

				err = db.Model(&common.InfraEnv{}).Where("id = ?", *i.ID).Update("static_network_config", "static network format result").Error
				Expect(err).ToNot(HaveOccurred())

				staticNetworkFormatRes := ""
				staticNetworkConfig := []*models.HostStaticNetworkConfig{}

				mockInfraEnvUpdateSuccess()
				mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(staticNetworkConfig).Return(nil).Times(1)
				mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes, nil).Times(1)
				mockUsage.EXPECT().Remove(gomock.Any(), usage.StaticNetworkConfigUsage).Times(1)
				mockUsage.EXPECT().Save(gomock.Any(), *cluster.ID, gomock.Any()).Times(1)

				bm.UpdateInfraEnv(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID: *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{
						StaticNetworkConfig: staticNetworkConfig,
					},
				})
			})
		})

		Context("check pull secret", func() {
			BeforeEach(func() {
				v, err := validations.NewPullSecretValidator(validations.Config{}, getTestAuthHandler())
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
					ID:               &infraEnvID,
					CPUArchitecture:  common.TestDefaultConfig.CPUArchitecture,
					OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
					Type:             models.ImageTypeFullIso.Pointer(),
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
					ID:               &infraEnvID,
					PullSecretSet:    true,
					CPUArchitecture:  common.TestDefaultConfig.CPUArchitecture,
					OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
					Type:             models.ImageTypeFullIso.Pointer(),
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
						ID:               &infraEnvID,
						PullSecretSet:    true,
						OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
						Type:             models.ImageTypeFullIso.Pointer(),
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

			It("set a valid noProxy wildcard", func() {
				_ = updateInfraEnv("", "", "*")
			})
		})

		Context("GenerateInfraEnvISOInternal", func() {
			BeforeEach(func() {
				infraEnvID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.InfraEnv{
					GeneratedAt: strfmt.NewDateTime(),
					PullSecret:  "PULL_SECRET",
					InfraEnv: models.InfraEnv{
						ID:               &infraEnvID,
						OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
						PullSecretSet:    true,
						Type:             models.ImageTypeFullIso.Pointer(),
					},
				}).Error
				Expect(err).ToNot(HaveOccurred())
			})

			It("UpdateInternal GeneratedAt updated in response", func() {
				mockInfraEnvUpdateSuccess()
				reponse, err := bm.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID:           infraEnvID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{},
				},
					nil,
				)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(reponse.GeneratedAt).ShouldNot(Equal(strfmt.NewDateTime()))
			})

			It("sets the download url correctly with the image service - unbounded InfraEnv", func() {
				i, err := bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: infraEnvID})
				Expect(err).ToNot(HaveOccurred())

				mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, "").Return(common.TestDefaultConfig.OsImage, nil)
				mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType(), gomock.Any()).Return("ignitionconfigforlogging", nil).Times(1)
				mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName),
					eventstest.WithInfraEnvIdMatcher(i.ID.String()),
					eventstest.WithClusterIdMatcher(i.ClusterID.String()))).Times(1)

				response, err := bm.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID:           *i.ID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{ImageType: models.ImageTypeMinimalIso},
				},
					nil,
				)
				Expect(err).ToNot(HaveOccurred())

				parsed, err := url.Parse(response.DownloadURL)
				Expect(err).NotTo(HaveOccurred())

				Expect(parsed.Scheme).To(Equal("https"))
				Expect(parsed.Host).To(Equal("image-service.example.com:8080"))

				pathList := strings.Split(parsed.Path, "/")
				Expect(pathList).To(HaveLen(8))
				Expect(pathList[7]).To(Equal("minimal.iso"))
				Expect(pathList[5]).To(Equal(common.TestDefaultConfig.OpenShiftVersion))
			})

			It("sets the download url correctly with the image service - bounded InfraEnv", func() {

				boundedInfraEnvID := strfmt.UUID(uuid.New().String())
				clusterID := strfmt.UUID(uuid.New().String())

				boundedInfraEnv := &common.InfraEnv{
					GeneratedAt: strfmt.NewDateTime(),
					PullSecret:  "PULL_SECRET",
					InfraEnv: models.InfraEnv{
						ClusterID:        clusterID,
						ID:               &boundedInfraEnvID,
						OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
						PullSecretSet:    true,
					},
				}
				err := db.Create(boundedInfraEnv).Error
				Expect(err).ToNot(HaveOccurred())

				mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, "").Return(common.TestDefaultConfig.OsImage, nil)
				mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType(), gomock.Any()).Return("ignitionconfigforlogging", nil).Times(1)
				mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName),
					eventstest.WithInfraEnvIdMatcher(boundedInfraEnv.ID.String()),
					eventstest.WithClusterIdMatcher(boundedInfraEnv.ClusterID.String()))).Times(1)

				response, err := bm.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{
					InfraEnvID:           boundedInfraEnvID,
					InfraEnvUpdateParams: &models.InfraEnvUpdateParams{ImageType: models.ImageTypeMinimalIso},
				},
					nil,
				)
				Expect(err).ToNot(HaveOccurred())

				parsed, err := url.Parse(response.DownloadURL)
				Expect(err).NotTo(HaveOccurred())

				Expect(parsed.Scheme).To(Equal("https"))
				Expect(parsed.Host).To(Equal("image-service.example.com:8080"))

				pathList := strings.Split(parsed.Path, "/")
				Expect(pathList).To(HaveLen(8))
				Expect(pathList[7]).To(Equal("minimal.iso"))
				Expect(pathList[5]).To(Equal(common.TestDefaultConfig.OpenShiftVersion))
			})

			Context("with rhsso auth", func() {
				BeforeEach(func() {
					_, cert := auth.GetTokenAndCert(false)
					cfg := &auth.Config{JwkCert: string(cert)}
					bm.authHandler = auth.NewRHSSOAuthenticator(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)
					var err error
					bm.ImageExpirationTime, err = time.ParseDuration("4h")
					Expect(err).NotTo(HaveOccurred())
				})

				It("sets a valid image_token", func() {
					i, err := bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: infraEnvID})
					Expect(err).ToNot(HaveOccurred())
					mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, "").Return(common.TestDefaultConfig.OsImage, nil)
					mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType(), gomock.Any()).Return("ignitionconfigforlogging", nil)
					mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName),
						eventstest.WithInfraEnvIdMatcher(i.ID.String()),
						eventstest.WithClusterIdMatcher(i.ClusterID.String()))).Times(1)
					response, err := bm.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{
						InfraEnvID:           infraEnvID,
						InfraEnvUpdateParams: &models.InfraEnvUpdateParams{ImageType: models.ImageTypeMinimalIso},
					},
						nil,
					)
					Expect(err).ToNot(HaveOccurred())

					u, err := url.Parse(response.DownloadURL)
					Expect(err).ToNot(HaveOccurred())
					pathList := strings.Split(u.Path, "/")
					Expect(pathList).To(HaveLen(8))
					Expect(pathList[3]).To(Equal("bytoken"))

					tok := pathList[4]
					_, err = bm.authHandler.AuthImageAuth(tok)
					Expect(err).NotTo(HaveOccurred())
				})

				It("updates the infra-env expires_at time", func() {
					i, err := bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: infraEnvID})
					Expect(err).ToNot(HaveOccurred())
					mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, "").Return(common.TestDefaultConfig.OsImage, nil)
					mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType(), gomock.Any()).Return("ignitionconfigforlogging", nil)
					mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName),
						eventstest.WithInfraEnvIdMatcher(i.ID.String()),
						eventstest.WithClusterIdMatcher(i.ClusterID.String()))).Times(1)
					response, err := bm.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{
						InfraEnvID:           infraEnvID,
						InfraEnvUpdateParams: &models.InfraEnvUpdateParams{ImageType: models.ImageTypeMinimalIso},
					},
						nil,
					)
					Expect(err).ToNot(HaveOccurred())

					Expect(response.ExpiresAt.String()).ToNot(Equal("0001-01-01T00:00:00.000Z"))
					var infraEnv common.InfraEnv
					Expect(db.First(&infraEnv, "id = ?", infraEnvID.String()).Error).To(Succeed())
					Expect(infraEnv.ExpiresAt.Equal(response.ExpiresAt)).To(BeTrue())
				})
			})

			Context("with local auth", func() {
				BeforeEach(func() {
					// Use a local auth handler
					pub, priv, err := gencrypto.ECDSAKeyPairPEM()
					Expect(err).NotTo(HaveOccurred())
					os.Setenv("EC_PRIVATE_KEY_PEM", priv)
					bm.authHandler, err = auth.NewLocalAuthenticator(
						&auth.Config{AuthType: auth.TypeLocal, ECPublicKeyPEM: pub},
						common.GetTestLog().WithField("pkg", "auth"),
						db,
					)
					Expect(err).NotTo(HaveOccurred())
				})

				AfterEach(func() {
					os.Unsetenv("EC_PRIVATE_KEY_PEM")
				})

				updateInfraEnv := func(params *models.InfraEnvUpdateParams) string {
					response, err := bm.UpdateInfraEnvInternal(ctx, installer.UpdateInfraEnvParams{
						InfraEnvID:           infraEnvID,
						InfraEnvUpdateParams: params,
					},
						nil,
					)
					Expect(err).ToNot(HaveOccurred())
					return response.DownloadURL
				}

				It("does not update the image service url if nothing changed", func() {
					i, err := bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: infraEnvID})
					Expect(err).ToNot(HaveOccurred())
					mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, "").Return(common.TestDefaultConfig.OsImage, nil).Times(2)
					mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType(), gomock.Any()).Return("ignitionconfigforlogging", nil).Times(1)
					mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName),
						eventstest.WithInfraEnvIdMatcher(i.ID.String()),
						eventstest.WithClusterIdMatcher(i.ClusterID.String()))).Times(1)
					params := &models.InfraEnvUpdateParams{ImageType: models.ImageTypeMinimalIso}
					firstURL := updateInfraEnv(params)
					newURL := updateInfraEnv(params)

					Expect(newURL).To(Equal(firstURL))
				})

				It("updates the image service url when things change", func() {
					i, err := bm.GetInfraEnvInternal(ctx, installer.GetInfraEnvParams{InfraEnvID: infraEnvID})
					Expect(err).ToNot(HaveOccurred())
					mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, "").Return(common.TestDefaultConfig.OsImage, nil).Times(7)
					mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), bm.IgnitionConfig, true, bm.authHandler.AuthType(), gomock.Any()).Return("ignitionconfigforlogging", nil).Times(7)
					mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), bm.IgnitionConfig, false, bm.authHandler.AuthType(), gomock.Any()).Return(discovery_ignition_3_1, nil).AnyTimes()
					mockEvents.EXPECT().SendInfraEnvEvent(ctx, eventstest.NewEventMatcher(
						eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName),
						eventstest.WithInfraEnvIdMatcher(i.ID.String()),
						eventstest.WithClusterIdMatcher(i.ClusterID.String()))).Times(7)

					params := &models.InfraEnvUpdateParams{ImageType: models.ImageTypeMinimalIso}
					prevURL := updateInfraEnv(params)

					By("updating ignition overrides")
					params.IgnitionConfigOverride = `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
					newURL := updateInfraEnv(params)
					Expect(newURL).ToNot(Equal(prevURL))
					prevURL = newURL

					By("updating image type")
					params.ImageType = models.ImageTypeFullIso
					newURL = updateInfraEnv(params)
					Expect(newURL).ToNot(Equal(prevURL))
					prevURL = newURL

					By("updating proxy")
					proxy := &models.Proxy{
						HTTPProxy:  swag.String("http://proxy.example.com"),
						HTTPSProxy: swag.String("http://other-proxy.example.com"),
					}
					params.Proxy = proxy
					newURL = updateInfraEnv(params)
					Expect(newURL).ToNot(Equal(prevURL))
					prevURL = newURL

					By("updating ssh key")
					sshKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDi8KHZYGyPQjECHwytquI3rmpgoUn6M+lkeOD2nEKvYElLE5mPIeqF0izJIl56u" +
						"ar2wda+3z107M9QkatE+dP4S9/Ltrlm+/ktAf4O6UoxNLUzv/TGHasb9g3Xkt8JTkohVzVK36622Sd8kLzEc61v1AonLWIADtpwq6/GvH" +
						"MAuPK2R/H0rdKhTokylKZLDdTqQ+KUFelI6RNIaUBjtVrwkx1j0htxN11DjBVuUyPT2O1ejWegtrM0T+4vXGEA3g3YfbT2k0YnEzjXXqng" +
						"qbXCYEJCZidp3pJLH/ilo4Y4BId/bx/bhzcbkZPeKlLwjR8g9sydce39bzPIQj+b7nlFv1Vot/77VNwkjXjYPUdUPu0d1PkFD9jKDOdB3f" +
						"AC61aG2a/8PFS08iBrKiMa48kn+hKXC4G4D5gj/QzIAgzWSl2tEzGQSoIVTucwOAL/jox2dmAa0RyKsnsHORppanuW4qD7KAcmas1GHrAq" +
						"IfNyDiU2JR50r1jCxj5H76QxIuM= root@ocp-edge34.lab.eng.tlv2.redhat.com"
					params.SSHAuthorizedKey = &sshKey
					newURL = updateInfraEnv(params)
					Expect(newURL).ToNot(Equal(prevURL))
					prevURL = newURL

					By("updating static network config")
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
					mockStaticNetworkConfig.EXPECT().ValidateStaticConfigParams(staticNetworkConfig).Return(nil).Times(2)
					mockStaticNetworkConfig.EXPECT().FormatStaticNetworkConfigForDB(staticNetworkConfig).Return(staticNetworkFormatRes, nil).Times(2)
					params.StaticNetworkConfig = staticNetworkConfig
					newURL = updateInfraEnv(params)
					Expect(newURL).ToNot(Equal(prevURL))
					prevURL = newURL

					By("updating pull secret")
					mockSecretValidator.EXPECT().ValidatePullSecret("mypullsecret", gomock.Any()).Return(nil)
					params.PullSecret = "mypullsecret"
					newURL = updateInfraEnv(params)
					Expect(newURL).ToNot(Equal(prevURL))
				})
			})
		})

		Context("Update Network", func() {
			BeforeEach(func() {
				infraEnvID = strfmt.UUID(uuid.New().String())
				err := db.Create(&common.InfraEnv{
					PullSecret: "PULL_SECRET",
					InfraEnv: models.InfraEnv{
						ID:               &infraEnvID,
						PullSecretSet:    true,
						CPUArchitecture:  common.TestDefaultConfig.CPUArchitecture,
						OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
						Type:             models.ImageTypeFullIso.Pointer(),
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
	gomega_format.CharactersAroundMismatchToInclude = 80

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
			ID:                   &infraEnvID,
			OpenshiftVersion:     common.TestDefaultConfig.OpenShiftVersion,
			Href:                 swag.String(HREF),
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

		var (
			hostID    strfmt.UUID
			clusterID strfmt.UUID
			host      *models.Host
		)

		var (
			// Disks that go in the inventory by default
			diskID1 = "/dev/sda"
			diskID2 = "/dev/sdb"

			// Other disk constants to be used by tests (not in inventory)
			diskID3 = "/dev/sdc"
			diskID4 = "/dev/sdc"
		)

		BeforeEach(func() {
			hostID = strfmt.UUID(uuid.New().String())
			clusterID = strfmt.UUID(uuid.New().String())

			err := db.Create(&common.Cluster{
				Cluster: models.Cluster{ID: &clusterID},
			}).Error
			Expect(err).ShouldNot(HaveOccurred())
			host = &models.Host{
				ID:         &hostID,
				InfraEnvID: infraEnvID,
				ClusterID:  &clusterID,
				Inventory: common.GenerateTestInventoryWithMutate(
					func(inventory *models.Inventory) {
						inventory.Disks = []*models.Disk{}

						inventory.Disks = append(inventory.Disks, &models.Disk{
							ID: diskID1,
						})
						inventory.Disks = append(inventory.Disks, &models.Disk{
							ID: diskID2,
						})
					},
				),
			}
			Expect(db.Create(host).Error).ToNot(HaveOccurred())
			host = &hostutil.GetHostFromDB(*host.ID, host.InfraEnvID, db).Host
			mockHostApi.EXPECT().RefreshInventory(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		})

		It("update host role success", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), models.HostRole("master"), gomock.Any()).Return(nil).Times(1)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
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
			mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
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

		Context("Hostname", func() {

			postUpdateCalls := func() {
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockUsage.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
				mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			}

			invalidHostnameCheck := func(hostname string) {
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), hostname, gomock.Any()).Times(0)
				resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
					InfraEnvID: infraEnvID,
					HostID:     hostID,
					HostUpdateParams: &models.HostUpdateParams{
						HostName: swag.String(hostname),
					},
				})
				Expect(resp).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
				Expect(resp.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
			}

			BeforeEach(func() {
				mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			})

			It("update host name success", func() {
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), "somehostname", gomock.Any()).Return(nil).Times(1)
				postUpdateCalls()
				resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
					InfraEnvID: infraEnvID,
					HostID:     hostID,
					HostUpdateParams: &models.HostUpdateParams{
						HostName: swag.String("somehostname"),
					},
				})
				Expect(resp).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
			})

			It("Valid splitted hostname", func() {
				hostname := "foobar.foobar.123456789"
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), hostname, gomock.Any()).Return(nil).Times(1)
				postUpdateCalls()
				resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
					InfraEnvID: infraEnvID,
					HostID:     hostID,
					HostUpdateParams: &models.HostUpdateParams{
						HostName: swag.String(hostname),
					},
				})
				Expect(resp).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
			})

			It("update host name failure", func() {
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), "somehostname", gomock.Any()).Return(fmt.Errorf("some error")).Times(1)
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
				hostname := "somehostnamei@dflkh"
				invalidHostnameCheck(hostname)
			})

			It("Too long part", func() {
				hostname := "abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij"
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), hostname, gomock.Any()).Times(0)
				invalidHostnameCheck(hostname)
			})

			It("Too long", func() {
				hostname := "abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.abcdefghijabcdefghijabcdefghijabcdefghijabcdefghijabcdefghij.1234567890"
				invalidHostnameCheck(hostname)

			})

			It("Preceding hyphen", func() {
				hostname := "-abc"
				invalidHostnameCheck(hostname)
			})
		})

		Context("Installation Disk Path", func() {
			It("update disks config success", func() {
				mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), diskID1).Return(nil).Times(1)
				mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
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
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
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
		})

		Context("Node Labels", func() {
			It("update node labels success", func() {
				mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateNodeLabels(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
				resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
					InfraEnvID: infraEnvID,
					HostID:     hostID,
					HostUpdateParams: &models.HostUpdateParams{
						NodeLabels: []*models.NodeLabelParams{{Key: swag.String("key"), Value: swag.String("Value")}},
					},
				})
				Expect(resp).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
			})

			It("update node labels invalid", func() {
				mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateNodeLabels(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(0)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(0)
				mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(0)
				resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
					InfraEnvID: infraEnvID,
					HostID:     hostID,
					HostUpdateParams: &models.HostUpdateParams{
						NodeLabels: []*models.NodeLabelParams{{Key: swag.String("key"), Value: swag.String("nospecialchars%^=@")}},
					},
				})
				Expect(resp).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
				Expect(resp.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
			})
		})

		Context("Update host skip disks", func() {
			verifyFunctionDidntMatch := func(diskID string) func(responder middleware.Responder) {
				return func(responder middleware.Responder) {
					response, ok := responder.(*common.ApiErrorResponse)
					Expect(ok).Should(BeTrue())
					Expect(response.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
					Expect(response.Error()).To(Equal(fmt.Sprintf("Disk identifier %s doesn't match any disk in the inventory, it cannot be skipped. Inventory disk identifiers are: /dev/sda, /dev/sdb", diskID)))
				}
			}

			verifyFunctionSuccess := func() func(responder middleware.Responder) {
				return func(responder middleware.Responder) {
					Expect(responder).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
				}
			}

			for _, test := range []struct {
				name                     string
				diskSkipFormattingParams []*models.DiskSkipFormattingParams
				originalSkippedDisks     []string
				expectedSkippedDisks     []string
				responseVerification     func(responder middleware.Responder)
				expectedNumOfUpdateCalls int
			}{
				{
					name: "Empty list, skip a single disk",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID1, SkipFormatting: swag.Bool(true)},
					},
					originalSkippedDisks:     []string{},
					expectedSkippedDisks:     []string{diskID1},
					responseVerification:     verifyFunctionSuccess(),
					expectedNumOfUpdateCalls: 1,
				},
				{
					name: "Empty list, skip a non-existing disk",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID3, SkipFormatting: swag.Bool(true)},
					},
					originalSkippedDisks:     []string{},
					expectedSkippedDisks:     []string{},
					responseVerification:     verifyFunctionDidntMatch(diskID3),
					expectedNumOfUpdateCalls: 0,
				},
				{
					name: "Empty list, skip multiple non-existing disks",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID3, SkipFormatting: swag.Bool(true)},
						{DiskID: &diskID4, SkipFormatting: swag.Bool(true)},
					},
					originalSkippedDisks:     []string{},
					expectedSkippedDisks:     []string{},
					responseVerification:     verifyFunctionDidntMatch(diskID3),
					expectedNumOfUpdateCalls: 0,
				},
				{
					name: "Empty list, skip one existing one non-existing disks",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID2, SkipFormatting: swag.Bool(true)},
						{DiskID: &diskID3, SkipFormatting: swag.Bool(true)},
					},
					originalSkippedDisks:     []string{},
					expectedSkippedDisks:     []string{},
					responseVerification:     verifyFunctionDidntMatch(diskID3),
					expectedNumOfUpdateCalls: 0,
				},
				{
					name: "One disk in list, skip a single different disk",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID2, SkipFormatting: swag.Bool(true)},
					},
					originalSkippedDisks:     []string{diskID1},
					expectedSkippedDisks:     []string{diskID1, diskID2},
					responseVerification:     verifyFunctionSuccess(),
					expectedNumOfUpdateCalls: 1,
				},
				{
					name: "One disk in list, skip the existing disk",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID1, SkipFormatting: swag.Bool(true)},
					},
					originalSkippedDisks:     []string{diskID1},
					expectedSkippedDisks:     []string{diskID1},
					responseVerification:     verifyFunctionSuccess(),
					expectedNumOfUpdateCalls: 1,
				},
				{
					name: "One disk in list, skip a non-existing disk",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID3, SkipFormatting: swag.Bool(true)},
					},
					originalSkippedDisks:     []string{diskID1},
					expectedSkippedDisks:     []string{diskID1},
					responseVerification:     verifyFunctionDidntMatch(diskID3),
					expectedNumOfUpdateCalls: 0,
				},
				{
					name: "One disk in list, skip multiple non-existing disks",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID3, SkipFormatting: swag.Bool(true)},
						{DiskID: &diskID4, SkipFormatting: swag.Bool(true)},
					},
					originalSkippedDisks:     []string{diskID1},
					expectedSkippedDisks:     []string{diskID1},
					responseVerification:     verifyFunctionDidntMatch(diskID3),
					expectedNumOfUpdateCalls: 0,
				},
				{
					name: "One disk in list, skip one existing one non-existing disks",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID2, SkipFormatting: swag.Bool(true)},
						{DiskID: &diskID3, SkipFormatting: swag.Bool(true)},
					},
					originalSkippedDisks:     []string{diskID1},
					expectedSkippedDisks:     []string{diskID1},
					responseVerification:     verifyFunctionDidntMatch(diskID3),
					expectedNumOfUpdateCalls: 0,
				},
				{
					name: "One disk in list, remove non-existing disk",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID3, SkipFormatting: swag.Bool(false)},
					},
					originalSkippedDisks:     []string{diskID1},
					expectedSkippedDisks:     []string{diskID1},
					responseVerification:     verifyFunctionSuccess(),
					expectedNumOfUpdateCalls: 1,
				},
				{
					name: "One disk in list, remove it",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID1, SkipFormatting: swag.Bool(false)},
					},
					originalSkippedDisks:     []string{diskID1},
					expectedSkippedDisks:     []string{},
					responseVerification:     verifyFunctionSuccess(),
					expectedNumOfUpdateCalls: 1,
				},
				{
					name: "One disk in list, remove it and non-existing one",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID1, SkipFormatting: swag.Bool(false)},
						{DiskID: &diskID3, SkipFormatting: swag.Bool(false)},
					},
					originalSkippedDisks:     []string{diskID1},
					expectedSkippedDisks:     []string{},
					responseVerification:     verifyFunctionSuccess(),
					expectedNumOfUpdateCalls: 1,
				},
				{
					name: "Two disks in list, remove first one",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID1, SkipFormatting: swag.Bool(false)},
					},
					originalSkippedDisks:     []string{diskID1, diskID2},
					expectedSkippedDisks:     []string{diskID2},
					responseVerification:     verifyFunctionSuccess(),
					expectedNumOfUpdateCalls: 1,
				},
				{
					name: "Two disks in list, remove second one",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID2, SkipFormatting: swag.Bool(false)},
					},
					originalSkippedDisks:     []string{diskID1, diskID2},
					expectedSkippedDisks:     []string{diskID1},
					responseVerification:     verifyFunctionSuccess(),
					expectedNumOfUpdateCalls: 1,
				},
				{
					name: "Two disks in list, add non-existing one",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID3, SkipFormatting: swag.Bool(true)},
					},
					originalSkippedDisks:     []string{diskID1, diskID2},
					expectedSkippedDisks:     []string{diskID1, diskID2},
					responseVerification:     verifyFunctionDidntMatch(diskID3),
					expectedNumOfUpdateCalls: 0,
				},
				{
					name: "One disk in list, remove it even though it's not in the inventory",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID3, SkipFormatting: swag.Bool(false)},
					},
					originalSkippedDisks:     []string{diskID3},
					expectedSkippedDisks:     []string{},
					responseVerification:     verifyFunctionSuccess(),
					expectedNumOfUpdateCalls: 1,
				},
				{
					name: "Two disks in list, remove them even though they're not in the inventory",
					diskSkipFormattingParams: []*models.DiskSkipFormattingParams{
						{DiskID: &diskID3, SkipFormatting: swag.Bool(false)},
						{DiskID: &diskID4, SkipFormatting: swag.Bool(false)},
					},
					originalSkippedDisks:     []string{diskID3, diskID4},
					expectedSkippedDisks:     []string{},
					responseVerification:     verifyFunctionSuccess(),
					expectedNumOfUpdateCalls: 1,
				},
			} {
				test := test
				It(test.name, func() {
					mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
					mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), "somehostname", gomock.Any()).Times(0)
					mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
					mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
					mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
					mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
					mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
					mockHostApi.EXPECT().UpdateNodeSkipDiskFormatting(gomock.Any(), gomock.Any(), strings.Join(test.expectedSkippedDisks, ","),
						gomock.Any()).Return(nil).Times(test.expectedNumOfUpdateCalls)

					originalSkippedDisks := ""
					if test.originalSkippedDisks != nil {
						originalSkippedDisks = strings.Join(test.originalSkippedDisks, ",")
					}
					Expect(db.Model(host).Update("skip_formatting_disks", originalSkippedDisks).Error).Should(Succeed())
					responder := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
						InfraEnvID: infraEnvID,
						HostID:     hostID,
						HostUpdateParams: &models.HostUpdateParams{
							DisksSkipFormatting: test.diskSkipFormattingParams,
						},
					})
					test.responseVerification(responder)
				})
			}
		})

		Context("MachineConfigPoolName", func() {
			It("update machine pool success", func() {
				mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), "machinepool").Return(nil).Times(1)
				mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
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
				mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
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

		It("update ignition endpoint token success", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), "mytoken").Return(nil).Times(1)
			mockHostApi.EXPECT().UpdateIgnitionEndpointHTTPHeaders(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(0)
			mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
			mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					IgnitionEndpointToken: swag.String("mytoken"),
				},
			})
			Expect(resp).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostCreated()))
		})

		It("update ignition endpoint token failure", func() {
			mockHostApi.EXPECT().UpdateRole(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateHostname(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateInstallationDisk(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateMachineConfigPoolName(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockHostApi.EXPECT().UpdateIgnitionEndpointToken(gomock.Any(), gomock.Any(), gomock.Any(), "mytoken").Return(fmt.Errorf("some error")).Times(1)
			resp := bm.V2UpdateHost(ctx, installer.V2UpdateHostParams{
				InfraEnvID: infraEnvID,
				HostID:     hostID,
				HostUpdateParams: &models.HostUpdateParams{
					IgnitionEndpointToken: swag.String("mytoken"),
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
			APIVips:          []*models.APIVip{{IP: "10.11.12.13"}},
		}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	It("V2 kubeconfig presigned backend not aws", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(false)
		generateReply := bm.V2GetPresignedForClusterFiles(ctx, installer.V2GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  constants.Kubeconfig,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
	})

	It("V2 kubeconfig presigned cluster is not in installed state", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		generateReply := bm.V2GetPresignedForClusterFiles(ctx, installer.V2GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  constants.Kubeconfig,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
	})

	It("V2 kubeconfig presigned happy flow", func() {
		status := models.ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		fileName := fmt.Sprintf("%s/%s", clusterID, constants.Kubeconfig)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, constants.Kubeconfig, gomock.Any()).Return("url", nil)
		generateReply := bm.V2GetPresignedForClusterFiles(ctx, installer.V2GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  constants.Kubeconfig,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.V2GetPresignedForClusterFilesOK{}))
		replyPayload := generateReply.(*installer.V2GetPresignedForClusterFilesOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})

})

var _ = Describe("DownloadMinimalInitrd", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		id         strfmt.UUID
		dbName     string
		httpProxy  = "http://10.10.1.1:3128"
		httpsProxy = "https://10.10.1.1:3128"
		noProxy    = "quay.io"
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		id = strfmt.UUID(uuid.New().String())
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	applyProxy := func(infraEnv common.InfraEnv) common.InfraEnv {
		infraEnv.Proxy = &models.Proxy{
			HTTPProxy:  &httpProxy,
			HTTPSProxy: &httpsProxy,
			NoProxy:    &noProxy,
		}
		return infraEnv
	}

	createInfraEnv := func(imageType models.ImageType) common.InfraEnv {
		result := common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:            &id,
				PullSecretSet: true,
				Type:          common.ImageTypePtr(imageType),
			},
			PullSecret: fakePullSecret,
		}
		return result
	}

	validateArchive := func(infraEnv common.InfraEnv) {
		params := installer.DownloadMinimalInitrdParams{InfraEnvID: id}
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
				rootfsServiceConfigBytes, err := io.ReadAll(r)
				Expect(err).ToNot(HaveOccurred())
				rootfsServiceConfigContent = string(rootfsServiceConfigBytes)
			}
		}

		rootfsServiceConfig := fmt.Sprintf("[Service]\n"+
			"Environment=http_proxy=%s\nEnvironment=https_proxy=%s\nEnvironment=no_proxy=%s\n"+
			"Environment=HTTP_PROXY=%s\nEnvironment=HTTPS_PROXY=%s\nEnvironment=NO_PROXY=%s",
			httpProxy, httpsProxy, noProxy,
			httpProxy, httpsProxy, noProxy)
		Expect(rootfsServiceConfigContent).To(Equal(rootfsServiceConfig))
	}

	It("returns not found with a non-existant cluster", func() {
		infraEnv := createInfraEnv(models.ImageTypeFullIso)
		Expect(db.Create(&infraEnv).Error).NotTo(HaveOccurred())
		params := installer.DownloadMinimalInitrdParams{InfraEnvID: strfmt.UUID(uuid.New().String())}
		response := bm.DownloadMinimalInitrd(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns no content without network customizations", func() {
		infraEnv := createInfraEnv(models.ImageTypeFullIso)
		Expect(db.Create(&infraEnv).Error).NotTo(HaveOccurred())
		params := installer.DownloadMinimalInitrdParams{InfraEnvID: id}
		response := bm.DownloadMinimalInitrd(ctx, params)
		Expect(response).Should(BeAssignableToTypeOf(&installer.DownloadMinimalInitrdNoContent{}))
	})

	It("returns non-empty archive when full ISO is requested", func() {
		infraEnv := createInfraEnv(models.ImageTypeFullIso)
		infraEnv = applyProxy(infraEnv)
		Expect(db.Create(&infraEnv).Error).NotTo(HaveOccurred())
		validateArchive(infraEnv)
	})

	It("returns non-empty archive when minimal ISO is requested", func() {
		infraEnv := createInfraEnv(models.ImageTypeMinimalIso)
		infraEnv = applyProxy(infraEnv)
		Expect(db.Create(&infraEnv).Error).NotTo(HaveOccurred())
		validateArchive(infraEnv)
	})
})

var _ = Describe("V2UploadClusterIngressCert test", func() {
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
			db, commontesting.GetDummyNotificationStream(ctrl), nil, nil, nil, nil, nil, nil, mockOperators, nil, nil, nil, nil, nil, false)
		c = common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			APIVips:          []*models.APIVip{{IP: "10.11.12.13"}},
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

	It("V2UploadClusterIngressCert no cluster id", func() {
		clusterId := strToUUID(uuid.New().String())
		generateReply := bm.V2UploadClusterIngressCert(ctx, installer.V2UploadClusterIngressCertParams{
			ClusterID:         *clusterId,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertNotFound()))
	})
	It("V2UploadClusterIngressCert cluster is not in installed state", func() {
		generateReply := bm.V2UploadClusterIngressCert(ctx, installer.V2UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertBadRequest()))

	})
	It("V2UploadClusterIngressCert kubeconfig already exists, return ok", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		mockS3Client.EXPECT().DoesObjectExist(ctx, kubeconfigObject).Return(true, nil).Times(1)
		generateReply := bm.V2UploadClusterIngressCert(ctx, installer.V2UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertCreated()))
	})
	It("V2UploadClusterIngressCert DoesObjectExist fails ", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		mockS3Client.EXPECT().DoesObjectExist(ctx, kubeconfigObject).Return(true, errors.Errorf("dummy")).Times(1)
		generateReply := bm.V2UploadClusterIngressCert(ctx, installer.V2UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertInternalServerError()))
	})
	It("V2UploadClusterIngressCert s3download failure", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		objectExists()
		mockS3Client.EXPECT().Download(ctx, kubeconfigNoingress).Return(nil, int64(0), errors.Errorf("dummy")).Times(1)
		generateReply := bm.V2UploadClusterIngressCert(ctx, installer.V2UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertInternalServerError()))
	})
	It("V2UploadClusterIngressCert bad kubeconfig, mergeIngressCaIntoKubeconfig failure", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		r := io.NopCloser(bytes.NewReader([]byte("test")))
		objectExists()
		mockS3Client.EXPECT().Download(ctx, kubeconfigNoingress).Return(r, int64(0), nil).Times(1)
		generateReply := bm.V2UploadClusterIngressCert(ctx, installer.V2UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertInternalServerError()))
	})
	It("V2UploadClusterIngressCert bad ingressCa, mergeIngressCaIntoKubeconfig failure", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		objectExists()
		mockS3Client.EXPECT().Download(ctx, kubeconfigNoingress).Return(kubeconfigFile, int64(0), nil)
		generateReply := bm.V2UploadClusterIngressCert(ctx, installer.V2UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: "bad format",
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertInternalServerError()))
	})

	It("V2UploadClusterIngressCert push fails", func() {
		status := models.ClusterStatusFinalizing
		c.Status = &status
		db.Save(&c)
		data, err := os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())
		kubeConfigAsBytes, err := io.ReadAll(data)
		Expect(err).ShouldNot(HaveOccurred())
		log := logrus.New()
		merged, err := mergeIngressCaIntoKubeconfig(kubeConfigAsBytes, []byte(ingressCa), log)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(merged).ShouldNot(Equal(kubeConfigAsBytes))
		Expect(merged).ShouldNot(Equal([]byte(ingressCa)))
		objectExists()
		mockS3Client.EXPECT().Download(ctx, kubeconfigNoingress).Return(kubeconfigFile, int64(0), nil).Times(1)
		mockS3Client.EXPECT().Upload(ctx, merged, kubeconfigObject).Return(errors.Errorf("Dummy"))
		generateReply := bm.V2UploadClusterIngressCert(ctx, installer.V2UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertInternalServerError()))
	})

	It("V2UploadClusterIngressCert download happy flow", func() {
		status := models.ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)

		data, err := os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())
		kubeConfigAsBytes, err := io.ReadAll(data)
		Expect(err).ShouldNot(HaveOccurred())
		log := logrus.New()
		merged, err := mergeIngressCaIntoKubeconfig(kubeConfigAsBytes, []byte(ingressCa), log)
		Expect(err).ShouldNot(HaveOccurred())
		objectExists()
		mockS3Client.EXPECT().Download(ctx, kubeconfigNoingress).Return(kubeconfigFile, int64(0), nil).Times(1)
		mockS3Client.EXPECT().Upload(ctx, merged, kubeconfigObject).Return(nil)

		// validate triggeredMonitored timestamp is zero
		Expect(c.TriggerMonitorTimestamp.IsZero()).To(Equal(true))

		generateReply := bm.V2UploadClusterIngressCert(ctx, installer.V2UploadClusterIngressCertParams{
			ClusterID:         clusterID,
			IngressCertParams: ingressCa,
		})
		Expect(generateReply).Should(Equal(installer.NewV2UploadClusterIngressCertCreated()))

		// validate triggeredMonitored was updated
		updated, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ToNot(HaveOccurred())
		Expect(updated.TriggerMonitorTimestamp.IsZero()).To(Equal(false))

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
		orgID1             = "300F3CE2-F122-4DA5-A845-2A4BC5956996"
		orgID2             = "DD71FD12-57FC-4480-917E-6F1900826543"
		userName1          = "test_user_1"
		userName2          = "test_user_2"
		userName3          = "test_user_3"
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
			APIVips:            []*models.APIVip{{IP: "10.11.12.13"}},
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
			resp := bm.V2ListClusters(ctx, installer.V2ListClustersParams{GetUnregisteredClusters: swag.Bool(true)})
			payload := resp.(*installer.V2ListClustersOK).Payload
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
			resp := bm.V2ListClusters(ctx, installer.V2ListClustersParams{GetUnregisteredClusters: swag.Bool(true), WithHosts: true})
			clusterList := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(clusterList)).Should(Equal(1))
			Expect(clusterList[0].ID.String()).Should(Equal(clusterID.String()))
			Expect(len(clusterList[0].Hosts)).Should(Equal(1))
			Expect(clusterList[0].Hosts[0].ID.String()).Should(Equal(hostID.String()))
		})

		It("failure - cluster was permanently deleted", func() {
			Expect(db.Unscoped().Delete(&c).Error).ShouldNot(HaveOccurred())
			Expect(db.Unscoped().Delete(&host1).Error).ShouldNot(HaveOccurred())
			resp := bm.V2ListClusters(ctx, installer.V2ListClustersParams{GetUnregisteredClusters: swag.Bool(true)})
			payload := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(0))
		})

		It("failure - not an admin user", func() {
			payload := &ocm.AuthPayload{}
			payload.Role = ocm.UserRole
			authCtx := context.WithValue(ctx, restapi.AuthKey, payload)
			Expect(db.Unscoped().Delete(&c).Error).ShouldNot(HaveOccurred())
			Expect(db.Unscoped().Delete(&host1).Error).ShouldNot(HaveOccurred())
			authCfg := auth.GetConfigRHSSO()
			bm.authzHandler = auth.NewAuthzHandler(authCfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)
			resp := bm.V2ListClusters(authCtx, installer.V2ListClustersParams{GetUnregisteredClusters: swag.Bool(true)})
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
					resp := bm.V2ListClusters(authCtx, installer.V2ListClustersParams{OpenshiftClusterID: openshiftClusterID})
					payload := resp.(*installer.V2ListClustersOK).Payload
					Expect(len(payload)).Should(Equal(1))
				})

				By("discarding cluster ID field", func() {
					resp := bm.V2ListClusters(authCtx, installer.V2ListClustersParams{})
					payload := resp.(*installer.V2ListClustersOK).Payload
					Expect(len(payload)).Should(Equal(1))
				})

				By("searching for a non-existing openshift cluster ID", func() {
					resp := bm.V2ListClusters(authCtx, installer.V2ListClustersParams{OpenshiftClusterID: strToUUID("00000000-0000-0000-0000-000000000000")})
					payload := resp.(*installer.V2ListClustersOK).Payload
					Expect(len(payload)).Should(Equal(0))
				})
			})
		}
	})

	It("filters based on AMS subscription ID", func() {
		payload := &ocm.AuthPayload{Role: ocm.UserRole}
		authCtx := context.WithValue(ctx, restapi.AuthKey, payload)

		By("searching for a single existing AMS subscription ID", func() {
			resp := bm.V2ListClusters(authCtx, installer.V2ListClustersParams{AmsSubscriptionIds: []string{amsSubscriptionID.String()}})
			payload := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(1))
		})

		By("discarding AMS subscription ID field", func() {
			resp := bm.V2ListClusters(authCtx, installer.V2ListClustersParams{})
			payload := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(1))
		})

		By("searching for a non-existing AMS subscription ID", func() {
			resp := bm.V2ListClusters(authCtx, installer.V2ListClustersParams{AmsSubscriptionIds: []string{"1sOMjCKRmEHYanIsp1bPbplbXXX"}})
			payload := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(0))
		})

		By("searching for both existing and non-existing AMS subscription IDs", func() {
			resp := bm.V2ListClusters(authCtx, installer.V2ListClustersParams{
				AmsSubscriptionIds: []string{
					amsSubscriptionID.String(),
					"1sOMjCKRmEHYanIsp1bPbplbXXX",
				},
			})
			payload := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(1))
		})
	})

	Context("Filter based on organization ID", func() {
		var authCtx context.Context

		BeforeEach(func() {
			cfg := auth.GetConfigRHSSO()
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)
			bm.authzHandler = auth.NewAuthzHandler(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)

			payload := &ocm.AuthPayload{Role: ocm.UserRole}
			payload.Username = userName1
			payload.Organization = orgID1
			authCtx = context.WithValue(ctx, restapi.AuthKey, payload)
		})

		It("multiple users in a single organization", func() {
			clusterID = strfmt.UUID(uuid.New().String())
			c = common.Cluster{Cluster: models.Cluster{
				ID:       &clusterID,
				OrgID:    orgID1,
				UserName: userName1,
			}}
			err := db.Create(&c).Error
			Expect(err).ShouldNot(HaveOccurred())

			clusterID = strfmt.UUID(uuid.New().String())
			c = common.Cluster{Cluster: models.Cluster{
				ID:       &clusterID,
				OrgID:    orgID1,
				UserName: userName2,
			}}
			err = db.Create(&c).Error
			Expect(err).ShouldNot(HaveOccurred())

			resp := bm.V2ListClusters(authCtx, installer.V2ListClustersParams{})
			payload := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(2))
			Expect(payload[0].OrgID).Should(Equal(orgID1))
			Expect(payload[1].OrgID).Should(Equal(orgID1))
		})

		It("multiple organizations", func() {
			clusterID = strfmt.UUID(uuid.New().String())
			c = common.Cluster{Cluster: models.Cluster{
				ID:       &clusterID,
				OrgID:    orgID1,
				UserName: userName1,
			}}
			err := db.Create(&c).Error
			Expect(err).ShouldNot(HaveOccurred())

			clusterID = strfmt.UUID(uuid.New().String())
			c = common.Cluster{Cluster: models.Cluster{
				ID:       &clusterID,
				OrgID:    orgID2,
				UserName: userName2,
			}}
			err = db.Create(&c).Error
			Expect(err).ShouldNot(HaveOccurred())

			resp := bm.V2ListClusters(authCtx, installer.V2ListClustersParams{})
			payload := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(1))
			Expect(payload[0].OrgID).Should(Equal(orgID1))
		})
	})

	Context("Filter by owner query param", func() {
		var authCtx context.Context

		BeforeEach(func() {
			cfg := auth.GetConfigRHSSO()
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)
			bm.authzHandler = auth.NewAuthzHandler(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)

			payload := &ocm.AuthPayload{Role: ocm.UserRole}
			payload.Username = userName1
			payload.Organization = orgID1
			authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

			clusterID = strfmt.UUID(uuid.New().String())
			c = common.Cluster{Cluster: models.Cluster{
				ID:       &clusterID,
				OrgID:    orgID1,
				UserName: userName1,
			}}
			err := db.Create(&c).Error
			Expect(err).ShouldNot(HaveOccurred())

			clusterID = strfmt.UUID(uuid.New().String())
			c = common.Cluster{Cluster: models.Cluster{
				ID:       &clusterID,
				OrgID:    orgID1,
				UserName: userName2,
			}}
			err = db.Create(&c).Error
			Expect(err).ShouldNot(HaveOccurred())

			clusterID = strfmt.UUID(uuid.New().String())
			c = common.Cluster{Cluster: models.Cluster{
				ID:       &clusterID,
				OrgID:    orgID2,
				UserName: userName3,
			}}
			err = db.Create(&c).Error
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("Owner query param is not specified", func() {
			params := installer.V2ListClustersParams{}
			resp := bm.V2ListClusters(authCtx, params)
			payload := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(2))
		})

		It("Owner query param is specified - user in org", func() {
			params := installer.V2ListClustersParams{}
			params.Owner = &userName1
			resp := bm.V2ListClusters(authCtx, params)
			payload := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(1))
			Expect(payload[0].UserName).Should(Equal(userName1))
		})

		It("Owner query param is specified - user not in org", func() {
			params := installer.V2ListClustersParams{}
			params.Owner = &userName3
			resp := bm.V2ListClusters(authCtx, params)
			payload := resp.(*installer.V2ListClustersOK).Payload
			Expect(len(payload)).Should(Equal(0))
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
			APIVips:          []*models.APIVip{{IP: "10.11.12.13"}},
		}}
		err := db.Create(&c).Error
		Expect(err).ShouldNot(HaveOccurred())
		kubeconfigFile, err = os.Open("../../subsystem/test_kubeconfig")
		Expect(err).ShouldNot(HaveOccurred())
		hostID = strfmt.UUID(uuid.New().String())
		host1 = addHost(hostID, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, "{}", db)

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
		params := installer.V2UploadLogsParams{
			ClusterID:   *clusterId,
			HostID:      &hostID,
			LogsType:    string(models.LogsTypeHost),
			InfraEnvID:  clusterId,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
		}

		verifyApiError(bm.V2UploadLogs(ctx, params), http.StatusNotFound)
	})
	It("Upload logs host not exits", func() {
		hostId := strToUUID(uuid.New().String())
		params := installer.V2UploadLogsParams{
			ClusterID:   clusterID,
			HostID:      hostId,
			LogsType:    string(models.LogsTypeHost),
			InfraEnvID:  &clusterID,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
		}

		verifyApiError(bm.V2UploadLogs(ctx, params), http.StatusNotFound)
	})

	It("Upload host logs to S3 - upload fails", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		params := installer.V2UploadLogsParams{
			ClusterID:   clusterID,
			HostID:      host.ID,
			LogsType:    string(models.LogsTypeHost),
			InfraEnvID:  &clusterID,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
		}

		fileName := bm.getLogsFullName(clusterID.String(), host.ID.String())
		mockS3Client.EXPECT().UploadStream(gomock.Any(), gomock.Any(), fileName).Return(errors.Errorf("Dummy")).Times(1)
		verifyApiError(bm.V2UploadLogs(ctx, params), http.StatusInternalServerError)
	})

	It("Upload Hosts logs Happy flow", func() {

		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		params := installer.V2UploadLogsParams{
			ClusterID:   clusterID,
			HostID:      host.ID,
			LogsType:    string(models.LogsTypeHost),
			InfraEnvID:  &clusterID,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
		}
		fileName := bm.getLogsFullName(clusterID.String(), host.ID.String())
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostLogsUploadedEventName),
			eventstest.WithClusterIdMatcher(clusterID.String()),
			eventstest.WithHostIdMatcher(host.ID.String()))).Times(1)
		mockS3Client.EXPECT().UploadStream(gomock.Any(), gomock.Any(), fileName).Return(nil).Times(1)
		mockHostApi.EXPECT().SetUploadLogsAt(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().UpdateLogsProgress(gomock.Any(), gomock.Any(), string(models.LogsStateCollecting)).Return(nil).Times(1)
		reply := bm.V2UploadLogs(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UploadLogsNoContent()))
	})

	It(" Start collecting hosts logs indication", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, "{}", db)
		params := installer.V2UpdateHostLogsProgressParams{
			InfraEnvID:  infraEnvID,
			HostID:      *host.ID,
			HTTPRequest: request,
			LogsProgressParams: &models.LogsProgressParams{
				LogsState: common.LogStatePtr(models.LogsStateRequested),
			},
		}
		mockHostApi.EXPECT().UpdateLogsProgress(gomock.Any(), gomock.Any(), string(models.LogsStateRequested)).Return(nil).Times(1)
		reply := bm.V2UpdateHostLogsProgress(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostLogsProgressNoContent()))
	})

	It("Complete collecting hosts logs indication", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, infraEnvID, clusterID, "{}", db)
		params := installer.V2UpdateHostLogsProgressParams{
			InfraEnvID:  infraEnvID,
			HostID:      *host.ID,
			HTTPRequest: request,
			LogsProgressParams: &models.LogsProgressParams{
				LogsState: common.LogStatePtr(models.LogsStateCompleted),
			},
		}
		mockHostApi.EXPECT().UpdateLogsProgress(gomock.Any(), gomock.Any(), string(models.LogsStateCompleted)).Return(nil).Times(1)
		reply := bm.V2UpdateHostLogsProgress(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateHostLogsProgressNoContent()))
	})

	It("Upload Controller logs Happy flow", func() {
		params := installer.V2UploadLogsParams{
			ClusterID:   clusterID,
			Upfile:      kubeconfigFile,
			HTTPRequest: request,
			LogsType:    string(models.LogsTypeController),
		}
		fileName := bm.getLogsFullName(clusterID.String(), string(models.LogsTypeController))
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterLogsUploadedEventName),
			eventstest.WithClusterIdMatcher(clusterID.String()))).Times(1)
		mockS3Client.EXPECT().UploadStream(gomock.Any(), gomock.Any(), fileName).Return(nil).Times(2)
		mockClusterApi.EXPECT().SetUploadControllerLogsAt(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(2)
		mockClusterApi.EXPECT().UpdateLogsProgress(gomock.Any(), gomock.Any(), string(models.LogsStateCollecting)).Return(nil).Times(2)
		By("Upload cluster logs for the first time")
		reply := bm.V2UploadLogs(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UploadLogsNoContent()))
		By("Upload cluster logs for the second time - expect no additional event to be published")
		err := db.Model(c).Update("controller_logs_collected_at", strfmt.DateTime(time.Now())).Error
		Expect(err).ShouldNot(HaveOccurred())
		reply = bm.V2UploadLogs(ctx, params)
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UploadLogsNoContent()))
	})
	It("Download controller log where not uploaded yet", func() {
		logsType := string(models.LogsTypeController)
		params := installer.V2DownloadClusterLogsParams{
			ClusterID: clusterID,
			LogsType:  &logsType,
		}
		verifyApiError(bm.V2DownloadClusterLogs(ctx, params), http.StatusConflict)
	})

	It("Download S3 host logs where not uploaded yet", func() {
		logsType := string(models.LogsTypeHost)
		params := installer.V2DownloadClusterLogsParams{
			ClusterID:   clusterID,
			HostID:      &hostID,
			LogsType:    &logsType,
			HTTPRequest: request,
		}
		verifyApiError(bm.V2DownloadClusterLogs(ctx, params), http.StatusConflict)
	})

	It("Download S3 host logs - object not found", func() {
		logsType := string(models.LogsTypeHost)
		params := installer.V2DownloadClusterLogsParams{
			ClusterID:   clusterID,
			HostID:      &hostID,
			LogsType:    &logsType,
			HTTPRequest: request,
		}
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		mockS3Client.EXPECT().Download(ctx, fileName).Return(nil, int64(0), common.NotFound(fileName))
		mockClusterApi.EXPECT().PrepareHostLogFile(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(fileName, nil).Times(1)
		verifyApiError(bm.V2DownloadClusterLogs(ctx, params), http.StatusNotFound)
	})

	It("Download S3 host logs -object failed", func() {
		logsType := string(models.LogsTypeHost)
		params := installer.V2DownloadClusterLogsParams{
			ClusterID:   clusterID,
			HostID:      &hostID,
			LogsType:    &logsType,
			HTTPRequest: request,
		}
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		mockClusterApi.EXPECT().PrepareHostLogFile(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(fileName, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, fileName).Return(nil, int64(0), errors.Errorf("dummy"))
		verifyApiError(bm.V2DownloadClusterLogs(ctx, params), http.StatusInternalServerError)
	})

	It("Download Hosts logs happy flow", func() {
		newHostID := strfmt.UUID(uuid.New().String())
		host := addHost(newHostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		logsType := string(models.LogsTypeHost)
		params := installer.V2DownloadClusterLogsParams{
			ClusterID:   clusterID,
			HostID:      host.ID,
			LogsType:    &logsType,
			HTTPRequest: request,
		}
		host.Bootstrap = true
		fileName := bm.getLogsFullName(clusterID.String(), host.ID.String())
		host.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host)
		r := io.NopCloser(bytes.NewReader([]byte("test")))
		mockClusterApi.EXPECT().PrepareHostLogFile(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(fileName, nil).Times(1)
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.V2DownloadClusterLogs(ctx, params)
		downloadFileName := fmt.Sprintf("mycluster_bootstrap_%s.tar.gz", newHostID.String())
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewV2DownloadClusterLogsOK().WithPayload(r), downloadFileName, 4, nil)))
	})

	It("Download Controller logs happy flow", func() {
		logsType := string(models.LogsTypeController)
		params := installer.V2DownloadClusterLogsParams{
			ClusterID: clusterID,
			LogsType:  &logsType,
		}
		fileName := bm.getLogsFullName(clusterID.String(), logsType)
		c.ControllerLogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&c)
		r := io.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.V2DownloadClusterLogs(ctx, params)
		downloadFileName := fmt.Sprintf("mycluster_%s_%s.tar.gz", clusterID, logsType)
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewV2DownloadClusterLogsOK().WithPayload(r), downloadFileName, 4, nil)))
	})

	It("Host logs presigned host not found", func() {
		hostID := strfmt.UUID(uuid.New().String())
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		generateReply := bm.V2GetPresignedForClusterFiles(ctx, installer.V2GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
			LogsType:  &hostLogsType,
		})
		verifyApiError(generateReply, http.StatusNotFound)
	})

	It("Host logs presigned no logs found", func() {
		hostID := strfmt.UUID(uuid.New().String())
		_ = addHost(hostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		generateReply := bm.V2GetPresignedForClusterFiles(ctx, installer.V2GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
			LogsType:  &hostLogsType,
		})
		verifyApiError(generateReply, http.StatusConflict)
	})

	It("Host logs presigned s3 error", func() {
		hostID := strfmt.UUID(uuid.New().String())
		host1 = addHost(hostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		mockClusterApi.EXPECT().PrepareHostLogFile(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(fileName, nil).Times(1)
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, fmt.Sprintf("mycluster_master_%s.tar.gz", hostID.String()), gomock.Any()).Return("",
			errors.Errorf("Dummy"))
		generateReply := bm.V2GetPresignedForClusterFiles(ctx, installer.V2GetPresignedForClusterFilesParams{
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
		mockClusterApi.EXPECT().PrepareHostLogFile(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(fileName, nil).Times(1)
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, fmt.Sprintf("mycluster_master_%s.tar.gz", hostID.String()), gomock.Any()).Return("url", nil)
		generateReply := bm.V2GetPresignedForClusterFiles(ctx, installer.V2GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
			LogsType:  &hostLogsType,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.V2GetPresignedForClusterFilesOK{}))
		replyPayload := generateReply.(*installer.V2GetPresignedForClusterFilesOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})

	It("host logs presigned happy flow without log type", func() {
		hostID := strfmt.UUID(uuid.New().String())
		host1 = addHost(hostID, models.HostRoleMaster, "known", models.HostKindHost, clusterID, clusterID, "{}", db)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		fileName := bm.getLogsFullName(clusterID.String(), hostID.String())
		mockClusterApi.EXPECT().PrepareHostLogFile(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(fileName, nil).Times(1)
		host1.LogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&host1)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, fileName, fmt.Sprintf("mycluster_master_%s.tar.gz", hostID.String()), gomock.Any()).Return("url", nil)
		generateReply := bm.V2GetPresignedForClusterFiles(ctx, installer.V2GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
			HostID:    &hostID,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.V2GetPresignedForClusterFilesOK{}))
		replyPayload := generateReply.(*installer.V2GetPresignedForClusterFilesOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})

	It("download cluster logs no cluster", func() {
		clusterId := strToUUID(uuid.New().String())
		params := installer.V2DownloadClusterLogsParams{
			ClusterID: *clusterId,
		}
		verifyApiError(bm.V2DownloadClusterLogs(ctx, params), http.StatusNotFound)
	})

	It("download cluster logs PrepareClusterLogFile failed", func() {
		params := installer.V2DownloadClusterLogsParams{
			ClusterID: clusterID,
		}
		mockClusterApi.EXPECT().PrepareClusterLogFile(ctx, gomock.Any(), gomock.Any()).Return("", errors.Errorf("dummy"))
		verifyApiError(bm.V2DownloadClusterLogs(ctx, params), http.StatusInternalServerError)
	})

	It("download cluster logs Download failed", func() {
		params := installer.V2DownloadClusterLogsParams{
			ClusterID: clusterID,
		}
		fileName := fmt.Sprintf("%s_logs.zip", clusterID)
		mockClusterApi.EXPECT().PrepareClusterLogFile(ctx, gomock.Any(), gomock.Any()).Return(fileName, nil)
		mockS3Client.EXPECT().Download(ctx, fileName).Return(nil, int64(0), errors.Errorf("dummy"))
		verifyApiError(bm.V2DownloadClusterLogs(ctx, params), http.StatusInternalServerError)
	})

	It("download cluster logs happy flow", func() {
		params := installer.V2DownloadClusterLogsParams{
			ClusterID: clusterID,
		}
		fileName := fmt.Sprintf("%s/logs/cluster_logs.tar", clusterID)
		mockClusterApi.EXPECT().PrepareClusterLogFile(ctx, gomock.Any(), gomock.Any()).Return(fileName, nil)
		r := io.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.V2DownloadClusterLogs(ctx, params)
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewV2DownloadClusterLogsOK().WithPayload(r),
			fmt.Sprintf("mycluster_%s.tar", clusterID), 4, nil)))
	})

	It("Logs presigned cluster logs failed", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockClusterApi.EXPECT().PrepareClusterLogFile(ctx, gomock.Any(), gomock.Any()).Return("", errors.Errorf("dummy"))
		generateReply := bm.V2GetPresignedForClusterFiles(ctx, installer.V2GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
		})
		verifyApiError(generateReply, http.StatusInternalServerError)
	})

	It("Logs presigned cluster logs happy flow", func() {
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockClusterApi.EXPECT().PrepareClusterLogFile(ctx, gomock.Any(), gomock.Any()).Return("tarred", nil)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(ctx, "tarred", fmt.Sprintf("mycluster_%s.tar", clusterID.String()), gomock.Any()).Return("url", nil)
		generateReply := bm.V2GetPresignedForClusterFiles(ctx, installer.V2GetPresignedForClusterFilesParams{
			ClusterID: clusterID,
			FileName:  "logs",
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.V2GetPresignedForClusterFilesOK{}))
		replyPayload := generateReply.(*installer.V2GetPresignedForClusterFilesOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})

	It("Download unregistered cluster controller log success", func() {
		logsType := string(models.LogsTypeController)
		params := installer.V2DownloadClusterLogsParams{
			ClusterID: clusterID,
			LogsType:  &logsType,
		}
		c.ControllerLogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&c)
		dbReply := db.Delete(&host1)
		Expect(int(dbReply.RowsAffected)).Should(Equal(1))
		dbReply = db.Where("id = ?", clusterID).Delete(&common.Cluster{})
		Expect(int(dbReply.RowsAffected)).Should(Equal(1))
		r := io.NopCloser(bytes.NewReader([]byte("test")))
		fileName := bm.getLogsFullName(clusterID.String(), logsType)
		mockS3Client.EXPECT().Download(ctx, fileName).Return(r, int64(4), nil)
		generateReply := bm.V2DownloadClusterLogs(ctx, params)
		downloadFileName := fmt.Sprintf("mycluster_%s_%s.tar.gz", clusterID, logsType)
		Expect(generateReply).Should(Equal(filemiddleware.NewResponder(installer.NewV2DownloadClusterLogsOK().WithPayload(r), downloadFileName, 4, nil)))
	})

	It("Download unregistered cluster controller log failure - permanently deleted", func() {
		logsType := string(models.LogsTypeController)
		params := installer.V2DownloadClusterLogsParams{
			ClusterID: clusterID,
			LogsType:  &logsType,
		}
		c.ControllerLogsCollectedAt = strfmt.DateTime(time.Now())
		db.Save(&c)
		dbReply := db.Unscoped().Delete(&host1)
		Expect(int(dbReply.RowsAffected)).Should(Equal(1))
		dbReply = db.Unscoped().Where("id = ?", clusterID).Delete(&common.Cluster{})
		Expect(int(dbReply.RowsAffected)).Should(Equal(1))
		verifyApiError(bm.V2DownloadClusterLogs(ctx, params), http.StatusNotFound)
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
		params := installer.V2GetClusterInstallConfigParams{ClusterID: clusterID}
		mockInstallConfigBuilder.EXPECT().GetInstallConfig(gomock.Any(), gomock.Any(), "").Return([]byte("some string"), nil).Times(1)
		response := bm.V2GetClusterInstallConfig(ctx, params)
		_, ok := response.(*installer.V2GetClusterInstallConfigOK)
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
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("saves the given string to the cluster", func() {
		override := `{"controlPlane": {"hyperthreading": "Disabled"}}`
		params := installer.V2UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: override,
		}
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.InstallConfigAppliedEventName),
			eventstest.WithClusterIdMatcher(params.ClusterID.String())))
		mockInstallConfigBuilder.EXPECT().ValidateInstallConfigPatch(gomock.Any(), gomock.Any(), params.InstallConfigParams).Return(nil).Times(1)
		mockGetInstallConfigSuccess(mockInstallConfigBuilder)
		mockUsageReports()
		response := bm.V2UpdateClusterInstallConfig(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2UpdateClusterInstallConfigCreated{}))

		var updated common.Cluster
		err := db.First(&updated, "id = ?", clusterID).Error
		Expect(err).ShouldNot(HaveOccurred())
		Expect(updated.InstallConfigOverrides).To(Equal(override))
	})

	It("returns not found with a non-existant cluster", func() {
		override := `{"controlPlane": {"hyperthreading": "Disabled"}}`
		params := installer.V2UpdateClusterInstallConfigParams{
			ClusterID:           strfmt.UUID(uuid.New().String()),
			InstallConfigParams: override,
		}
		response := bm.V2UpdateClusterInstallConfig(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns bad request when validation fails", func() {
		override := `{"controlPlane": {"hyperthreading": "Disabled"`
		params := installer.V2UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: override,
		}
		mockInstallConfigBuilder.EXPECT().ValidateInstallConfigPatch(gomock.Any(), gomock.Any(), params.InstallConfigParams).Return(fmt.Errorf("some error")).Times(1)
		response := bm.V2UpdateClusterInstallConfig(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})

	It("updates the install config overrides feature usage", func() {
		override := `{"controlPlane": {"hyperthreading": "Disabled"}}`
		params := installer.V2UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: override,
		}
		usages := map[string]models.Usage{}
		overrideUsage := make(map[string]interface{})
		overrideUsage["controlPlane hyperthreading"] = true
		mockUsage.EXPECT().Add(usages, usage.InstallConfigOverrides, gomock.Any()).Times(1)
		mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.InstallConfigAppliedEventName),
			eventstest.WithClusterIdMatcher(params.ClusterID.String())))
		mockInstallConfigBuilder.EXPECT().ValidateInstallConfigPatch(gomock.Any(), gomock.Any(), params.InstallConfigParams).Return(nil).Times(1)
		mockGetInstallConfigSuccess(mockInstallConfigBuilder)
		bm.V2UpdateClusterInstallConfig(ctx, params)
	})

	It("doesn't update the install config overrides feature usage if it's empty", func() {
		params := installer.V2UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: "",
		}
		mockUsage.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.InstallConfigAppliedEventName),
			eventstest.WithClusterIdMatcher(params.ClusterID.String())))
		mockInstallConfigBuilder.EXPECT().ValidateInstallConfigPatch(gomock.Any(), gomock.Any(), params.InstallConfigParams).Return(nil).Times(1)
		mockGetInstallConfigSuccess(mockInstallConfigBuilder)
		bm.V2UpdateClusterInstallConfig(ctx, params)
		var updated common.Cluster
		err := db.First(&updated, "id = ?", clusterID).Error
		Expect(err).ShouldNot(HaveOccurred())
		Expect(updated.Cluster.FeatureUsage).To(Equal(""))
	})

	DescribeTable(
		"Removes the console from the list of monitored operators for 4.12 or newer",
		func(version string) {
			err := db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update("openshift_version", version).Error
			Expect(err).ToNot(HaveOccurred())
			operator := &models.MonitoredOperator{
				ClusterID: clusterID,
				Name:      "console",
			}
			err = db.FirstOrCreate(operator).Error
			Expect(err).ToNot(HaveOccurred())
			installConfig := installcfg.InstallerConfigBaremetal{
				Capabilities: &installcfg.Capabilities{
					BaselineCapabilitySet: "None",
					AdditionalEnabledCapabilities: []installcfg.ClusterVersionCapability{
						"baremetal",
					},
				},
			}
			installConfigData, err := json.Marshal(installConfig)
			Expect(err).ToNot(HaveOccurred())
			mockEvents.EXPECT().SendClusterEvent(gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallConfigBuilder.EXPECT().ValidateInstallConfigPatch(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			mockInstallConfigBuilder.EXPECT().GetInstallConfig(gomock.Any(), gomock.Any(), gomock.Any()).Return(installConfigData, nil).AnyTimes()
			params := installer.V2UpdateClusterInstallConfigParams{
				ClusterID:           clusterID,
				InstallConfigParams: "{}",
			}
			bm.V2UpdateClusterInstallConfig(ctx, params)
			err = db.First(&operator).Error
			Expect(err).To(Equal(gorm.ErrRecordNotFound))
		},
		Entry("Release 4.12", "4.12.7"),
		Entry("Release 4.13", "4.13.0"),
		Entry("Prerelease 4.13", "4.13.0-ec.5"),
	)

	DescribeTable(
		"Doesn't remove the console from the list of monitored operators for 4.11 or older",
		func(version string) {
			err := db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update("openshift_version", version).Error
			Expect(err).ToNot(HaveOccurred())
			operator := &models.MonitoredOperator{
				ClusterID: clusterID,
				Name:      "console",
			}
			err = db.FirstOrCreate(operator).Error
			Expect(err).ToNot(HaveOccurred())
			installConfig := installcfg.InstallerConfigBaremetal{
				Capabilities: &installcfg.Capabilities{
					BaselineCapabilitySet: "None",
					AdditionalEnabledCapabilities: []installcfg.ClusterVersionCapability{
						"baremetal",
					},
				},
			}
			installConfigData, err := json.Marshal(installConfig)
			Expect(err).ToNot(HaveOccurred())
			mockEvents.EXPECT().SendClusterEvent(gomock.Any(), gomock.Any()).AnyTimes()
			mockInstallConfigBuilder.EXPECT().ValidateInstallConfigPatch(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			mockInstallConfigBuilder.EXPECT().GetInstallConfig(gomock.Any(), gomock.Any(), gomock.Any()).Return(installConfigData, nil).AnyTimes()
			params := installer.V2UpdateClusterInstallConfigParams{
				ClusterID:           clusterID,
				InstallConfigParams: "{}",
			}
			bm.V2UpdateClusterInstallConfig(ctx, params)
			err = db.First(&operator).Error
			Expect(err).ToNot(HaveOccurred())
		},
		Entry("Release 4.10", "4.10.2"),
		Entry("Release 4.11", "4.11.3"),
		Entry("Prerelease 4.11", "4.11.0-ec.2"),
	)

	It("Adds the console from the list of monitored operators", func() {
		err := db.Model(&common.Cluster{}).Where("id = ?", clusterID).Update("openshift_version", "4.12.7").Error
		Expect(err).ToNot(HaveOccurred())
		operator := &models.MonitoredOperator{
			ClusterID: clusterID,
			Name:      "console",
		}
		err = db.Delete(operator).Error
		Expect(err).ToNot(HaveOccurred())
		installConfig := installcfg.InstallerConfigBaremetal{
			Capabilities: &installcfg.Capabilities{
				BaselineCapabilitySet: "None",
				AdditionalEnabledCapabilities: []installcfg.ClusterVersionCapability{
					"Console",
				},
			},
		}
		installConfigData, err := json.Marshal(installConfig)
		Expect(err).ToNot(HaveOccurred())
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), gomock.Any()).AnyTimes()
		mockInstallConfigBuilder.EXPECT().ValidateInstallConfigPatch(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		mockInstallConfigBuilder.EXPECT().GetInstallConfig(gomock.Any(), gomock.Any(), gomock.Any()).Return(installConfigData, nil).AnyTimes()
		params := installer.V2UpdateClusterInstallConfigParams{
			ClusterID:           clusterID,
			InstallConfigParams: "{}",
		}
		bm.V2UpdateClusterInstallConfig(ctx, params)
		err = db.First(&operator).Error
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("V2DownloadInfraEnvFiles", func() {
	var (
		bm           *bareMetalInventory
		cfg          Config
		db           *gorm.DB
		ctx          = context.Background()
		dbName       string
		infraEnvID   strfmt.UUID
		testTokenKey = "6aa03bd3b328d44ddf9a9fefc1290a01a3d52294b51d2b54b61819010206c917" // #nosec
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		cfg.ServiceBaseURL = "https://test.com"
		bm = createInventory(db, cfg)
		var err error
		bm.ImageExpirationTime, err = time.ParseDuration("4h")
		Expect(err).NotTo(HaveOccurred())

		infraEnvID = strfmt.UUID(uuid.New().String())
		infraEnv := common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:               &infraEnvID,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
				PullSecretSet:    true,
				Type:             common.ImageTypePtr(models.ImageTypeFullIso),
			},
			ImageTokenKey: testTokenKey,
			PullSecret:    fakePullSecret,
		}
		Expect(db.Create(&infraEnv).Error).To(Succeed())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	getResponse := func(fileName string, withMac bool, ipxeScriptType *string, discoveryIsoType string) middleware.Responder {
		params := installer.V2DownloadInfraEnvFilesParams{InfraEnvID: infraEnvID, FileName: fileName, DiscoveryIsoType: &discoveryIsoType}
		if withMac {
			params.Mac = toMac("f8:75:a4:a4:00:fe")
		}
		params.IpxeScriptType = ipxeScriptType
		return bm.V2DownloadInfraEnvFiles(ctx, params)
	}

	getResponseData := func(fileName string, withMac bool, ipxeScriptType *string, discoveryIsoType string) []byte {
		response := getResponse(fileName, withMac, ipxeScriptType, discoveryIsoType)
		fileMw, ok := response.(*filemiddleware.FileMiddlewareResponder)
		Expect(ok).To(BeTrue())
		innerType, ok := fileMw.GetNext().(*installer.V2DownloadInfraEnvFilesOK)
		Expect(ok).To(BeTrue())

		body, err := io.ReadAll(innerType.Payload)
		Expect(err).ToNot(HaveOccurred())
		return body
	}

	strArrayToKernelArguments := func(args []string) (kargs models.KernelArguments) {
		for _, s := range args {
			kargs = append(kargs, &models.KernelArgument{
				Operation: models.KernelArgumentOperationAppend,
				Value:     s,
			})
		}
		return
	}

	updateKernelArgs := func(kernelArgs ...string) {
		params := installer.UpdateInfraEnvParams{
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{},
			InfraEnvID:           infraEnvID,
		}
		params.InfraEnvUpdateParams.KernelArguments = append(params.InfraEnvUpdateParams.KernelArguments, strArrayToKernelArguments(kernelArgs)...)
		response := bm.UpdateInfraEnv(ctx, params)
		_, ok := response.(*installer.UpdateInfraEnvCreated)
		Expect(ok).To(BeTrue())
	}

	It("should ensure the correct discovery iso type is passed to the ignition builder", func() {
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), "").Return(discovery_ignition_3_1, nil).Times(1)
		body := getResponseData("discovery.ign", false, nil, "")
		config, report, err := ign_3_1.Parse(body)
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
	})

	It("returns discovery.ign successfully when asked to use full-iso", func() {
		discoveryIsoType := string(models.ImageTypeFullIso)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), discoveryIsoType).Return(discovery_ignition_3_1, nil).Times(1)
		body := getResponseData("discovery.ign", false, nil, discoveryIsoType)
		config, report, err := ign_3_1.Parse(body)
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
	})

	It("returns discovery.ign successfully when asked to use minimal-iso", func() {
		discoveryIsoType := string(models.ImageTypeMinimalIso)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), discoveryIsoType).Return(discovery_ignition_3_1, nil).Times(1)
		body := getResponseData("discovery.ign", false, nil, discoveryIsoType)
		config, report, err := ign_3_1.Parse(body)
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
	})

	It("returns not found with a non-existant InfraEnv", func() {
		params := installer.V2DownloadInfraEnvFilesParams{InfraEnvID: strfmt.UUID(uuid.New().String()), FileName: "discovery.ign"}
		response := bm.V2DownloadInfraEnvFiles(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns bad request when provided an invalid filename", func() {
		params := installer.V2DownloadInfraEnvFilesParams{InfraEnvID: infraEnvID, FileName: "otherfile"}
		response := bm.V2DownloadInfraEnvFiles(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})

	It("returns ipxe-script successfully", func() {
		mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
		content := getResponseData("ipxe-script", false, nil, "")
		lines := strings.Split(string(content), "\n")

		Expect(lines[0]).To(Equal("#!ipxe"))

		By("validating the kernel line")
		kernelRegex := regexp.MustCompile(`^kernel (\S+) initrd=initrd coreos.live.rootfs_url=(\S+) (.+)`)
		match := kernelRegex.FindStringSubmatch(lines[2])
		fmt.Fprintf(GinkgoWriter, "lines[2]: %s\n", lines[2])
		fmt.Fprintf(GinkgoWriter, "match: %s\n", match)
		Expect(match).NotTo(BeNil())

		kernelURL, err := url.Parse(match[1])
		Expect(err).NotTo(HaveOccurred())
		Expect(kernelURL.Host).To(Equal(imageServiceHost))
		Expect(kernelURL.Path).To(Equal(imageServicePath + "/boot-artifacts/kernel"))
		Expect(kernelURL.Query().Get("version")).To(Equal(*common.TestDefaultConfig.OsImage.OpenshiftVersion))
		Expect(kernelURL.Query().Get("arch")).To(Equal(*common.TestDefaultConfig.OsImage.CPUArchitecture))

		rootfsURL, err := url.Parse(match[2])
		Expect(err).NotTo(HaveOccurred())
		Expect(rootfsURL.Host).To(Equal(imageServiceHost))
		Expect(rootfsURL.Path).To(Equal(imageServicePath + "/boot-artifacts/rootfs"))
		Expect(rootfsURL.Query().Get("version")).To(Equal(*common.TestDefaultConfig.OsImage.OpenshiftVersion))
		Expect(rootfsURL.Query().Get("arch")).To(Equal(*common.TestDefaultConfig.OsImage.CPUArchitecture))

		Expect(match[3]).To(Equal(`random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal console=tty1 console=ttyS1,115200n8 coreos.inst.persistent-kargs="console=tty1 console=ttyS1,115200n8"`))

		By("validating the initrd line")
		initrdRegex := regexp.MustCompile(`^initrd --name initrd (.+)`)
		match = initrdRegex.FindStringSubmatch(lines[1])
		Expect(match).NotTo(BeNil())

		initrdURL, err := url.Parse(match[1])
		Expect(err).NotTo(HaveOccurred())
		Expect(initrdURL.Scheme).To(Equal("http"))
		Expect(initrdURL.Host).To(Equal(imageServiceHost))
		Expect(initrdURL.Path).To(Equal(fmt.Sprintf("%s/images/%s/pxe-initrd", imageServicePath, infraEnvID)))
		Expect(initrdURL.Query().Get("version")).To(Equal(*common.TestDefaultConfig.OsImage.OpenshiftVersion))
		Expect(initrdURL.Query().Get("arch")).To(Equal(*common.TestDefaultConfig.OsImage.CPUArchitecture))

		Expect(lines[3]).To(Equal("boot"))
	})

	It("returns ipxe-script successfully - with kernel arguments", func() {
		kernelArguments := []string{
			"p1",
			"p2=argument",
			"p3=\"this is an argument\"",
		}
		initialKernelArguments := `random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal console=tty1 console=ttyS1,115200n8 coreos.inst.persistent-kargs="console=tty1 console=ttyS1,115200n8"`
		mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(3)
		content := getResponseData("ipxe-script", false, nil, "")
		lines := strings.Split(string(content), "\n")

		Expect(lines[0]).To(Equal("#!ipxe"))

		By("validating the kernel line")
		kernelRegex := regexp.MustCompile(`^kernel (\S+) initrd=initrd coreos.live.rootfs_url=(\S+) (.+)`)
		match := kernelRegex.FindStringSubmatch(lines[2])
		Expect(match).NotTo(BeNil())

		Expect(match[3]).To(Equal(initialKernelArguments))

		By("update infra env with kernel arguments")
		mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String())))
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			gomock.Any()).Return(discovery_ignition_3_1, nil).Times(1)
		updateKernelArgs(kernelArguments...)
		By("get ipxe script again")
		content = getResponseData("ipxe-script", false, nil, "")
		lines = strings.Split(string(content), "\n")

		Expect(lines[0]).To(Equal("#!ipxe"))

		By("validating the kernel line")
		match = kernelRegex.FindStringSubmatch(lines[2])
		Expect(match).NotTo(BeNil())

		strToCompare := initialKernelArguments + " " + strings.Join(kernelArguments, " ")
		Expect(match[3]).To(Equal(strToCompare))
	})

	It("returns ipxe-script successfully with mac", func() {
		mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
		content := getResponseData("ipxe-script", true, nil, "")
		lines := strings.Split(string(content), "\n")

		Expect(lines[0]).To(Equal("#!ipxe"))

		By("validating the kernel line")
		kernelRegex := regexp.MustCompile(`^kernel (\S+) initrd=initrd coreos.live.rootfs_url=(\S+) (.+)`)
		match := kernelRegex.FindStringSubmatch(lines[2])
		fmt.Fprintf(GinkgoWriter, "lines[2]: %s\n", lines[2])
		fmt.Fprintf(GinkgoWriter, "match: %s\n", match)
		Expect(match).NotTo(BeNil())

		kernelURL, err := url.Parse(match[1])
		Expect(err).NotTo(HaveOccurred())
		Expect(kernelURL.Host).To(Equal(imageServiceHost))
		Expect(kernelURL.Path).To(Equal(imageServicePath + "/boot-artifacts/kernel"))
		Expect(kernelURL.Query().Get("version")).To(Equal(*common.TestDefaultConfig.OsImage.OpenshiftVersion))
		Expect(kernelURL.Query().Get("arch")).To(Equal(*common.TestDefaultConfig.OsImage.CPUArchitecture))

		rootfsURL, err := url.Parse(match[2])
		Expect(err).NotTo(HaveOccurred())
		Expect(rootfsURL.Host).To(Equal(imageServiceHost))
		Expect(rootfsURL.Path).To(Equal(imageServicePath + "/boot-artifacts/rootfs"))
		Expect(rootfsURL.Query().Get("version")).To(Equal(*common.TestDefaultConfig.OsImage.OpenshiftVersion))
		Expect(rootfsURL.Query().Get("arch")).To(Equal(*common.TestDefaultConfig.OsImage.CPUArchitecture))

		Expect(match[3]).To(Equal(`random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal console=tty1 console=ttyS1,115200n8 coreos.inst.persistent-kargs="console=tty1 console=ttyS1,115200n8"`))

		By("validating the initrd line")
		initrdRegex := regexp.MustCompile(`^initrd --name initrd (.+)`)
		match = initrdRegex.FindStringSubmatch(lines[1])
		Expect(match).NotTo(BeNil())

		initrdURL, err := url.Parse(match[1])
		Expect(err).NotTo(HaveOccurred())
		Expect(initrdURL.Scheme).To(Equal("http"))
		Expect(initrdURL.Host).To(Equal(imageServiceHost))
		Expect(initrdURL.Path).To(Equal(fmt.Sprintf("%s/images/%s/pxe-initrd", imageServicePath, infraEnvID)))
		Expect(initrdURL.Query().Get("version")).To(Equal(*common.TestDefaultConfig.OsImage.OpenshiftVersion))
		Expect(initrdURL.Query().Get("arch")).To(Equal(*common.TestDefaultConfig.OsImage.CPUArchitecture))

		Expect(lines[3]).To(Equal("boot"))
	})

	It("fails to return ipxe-script when openshift version is nil", func() {
		mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, gomock.Any()).Return(&models.OsImage{}, nil).Times(1)
		params := installer.V2DownloadInfraEnvFilesParams{InfraEnvID: infraEnvID, FileName: "ipxe-script"}
		response := bm.V2DownloadInfraEnvFiles(ctx, params)
		verifyApiError(response, http.StatusInternalServerError)
	})

	It("fails to return ipxe-script when openshift version can't be found", func() {
		mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, gomock.Any()).Return(nil, fmt.Errorf("some error")).Times(1)
		params := installer.V2DownloadInfraEnvFilesParams{InfraEnvID: infraEnvID, FileName: "ipxe-script"}
		response := bm.V2DownloadInfraEnvFiles(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})

	It("serve the IPXE script", func() {
		scenarios := []struct {
			name   string
			status string
			stage  models.HostStage
		}{
			{
				name:   "host in known status",
				status: models.HostStatusKnown,
			},
			{
				name:   "host is installing and waiting for control plane",
				status: models.HostStatusInstallingInProgress,
				stage:  models.HostStageWaitingForControlPlane,
			},
			{
				name:   "host is in error",
				status: models.HostStatusError,
			},
		}
		for _, s := range scenarios {
			By(s.name)
			id := strfmt.UUID(uuid.New().String())
			host := models.Host{
				ID:         &id,
				InfraEnvID: infraEnvID,
				Inventory:  `{"interfaces": [{"name": "eth0", "mac_address": "f8:75:a4:a4:00:fe", "ipv4_addresses":[], "ipv6_addresses": []}]}`,
				Status:     swag.String(s.status),
			}
			if s.stage != "" {
				host.Progress = &models.HostProgressInfo{
					CurrentStage: s.stage,
				}
			}
			Expect(db.Create(&host).Error).ToNot(HaveOccurred())
			mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
			content := getResponseData("ipxe-script", true, nil, "")
			initrdRegex := regexp.MustCompile(`^initrd --name initrd (.+)`)
			match := initrdRegex.FindStringSubmatch(strings.Split(string(content), "\n")[1])
			Expect(match).NotTo(BeNil())
			Expect(db.Delete(&models.Host{ID: &id, InfraEnvID: infraEnvID}).Error).ToNot(HaveOccurred())
		}
	})

	It("skip serving the IPXE script", func() {
		scenarios := []struct {
			name   string
			status string
			stage  models.HostStage
		}{
			{
				name:   "host is rebooting",
				status: models.HostStatusInstallingInProgress,
				stage:  models.HostStageRebooting,
			},
			{
				name:   "host is configuring",
				status: models.HostStatusInstallingInProgress,
				stage:  models.HostStageConfiguring,
			},
			{
				name:   "host is waiting for ignition",
				status: models.HostStatusInstallingInProgress,
				stage:  models.HostStageConfiguring,
			},
			{
				name:   "host is in stage done",
				status: models.HostStatusInstallingInProgress,
				stage:  models.HostStageDone,
			},
			{
				name:   "installed",
				status: models.HostStatusInstalled,
			},
		}
		for _, s := range scenarios {
			By(s.name)
			id := strfmt.UUID(uuid.New().String())
			host := models.Host{
				ID:         &id,
				InfraEnvID: infraEnvID,
				Inventory:  `{"interfaces": [{"name": "eth0", "mac_address": "f8:75:a4:a4:00:fe", "ipv4_addresses":[], "ipv6_addresses": []}]}`,
				Status:     swag.String(s.status),
			}
			if s.stage != "" {
				host.Progress = &models.HostProgressInfo{
					CurrentStage: s.stage,
				}
			}
			Expect(db.Create(&host).Error).ToNot(HaveOccurred())
			response := getResponse("ipxe-script", true, nil, "")
			verifyApiErrorString(response, http.StatusNotFound, "IPXE booting skipped")
			Expect(db.Delete(&models.Host{ID: &id, InfraEnvID: infraEnvID}).Error).ToNot(HaveOccurred())
		}
	})

	It("IPXE with mac with 2 matching hosts", func() {
		for i := 0; i != 2; i++ {
			id := strfmt.UUID(uuid.New().String())
			host := models.Host{
				ID:         &id,
				InfraEnvID: infraEnvID,
				Inventory:  `{"interfaces": [{"name": "eth0", "mac_address": "f8:75:a4:a4:00:fe", "ipv4_addresses":[], "ipv6_addresses": []}]}`,
				Status:     swag.String(models.HostStatusInsufficient),
			}
			Expect(db.Create(&host).Error).ToNot(HaveOccurred())
		}

		response := getResponse("ipxe-script", true, swag.String(BootOrderControl), "")
		verifyApiErrorString(response, http.StatusInternalServerError, "Unexpected number of hosts")
	})

	Context("with local auth", func() {
		BeforeEach(func() {
			// Use a local auth handler
			pub, priv, err := gencrypto.ECDSAKeyPairPEM()
			Expect(err).NotTo(HaveOccurred())
			os.Setenv("EC_PRIVATE_KEY_PEM", priv)
			bm.authHandler, err = auth.NewLocalAuthenticator(
				&auth.Config{AuthType: auth.TypeLocal, ECPublicKeyPEM: pub},
				common.GetTestLog().WithField("pkg", "auth"),
				db,
			)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			os.Unsetenv("EC_PRIVATE_KEY_PEM")
		})

		It("IPXE without mac with script type BootOrderControl", func() {
			content := getResponseData("ipxe-script", false, swag.String(BootOrderControl), "")
			chainRegex := regexp.MustCompile(`^chain +(.*file_name=ipxe-script&mac=[$]{net0/mac})`)
			match := chainRegex.FindStringSubmatch(strings.Split(string(content), "\n")[1])
			Expect(match).NotTo(BeNil())

			chainUrl, err := url.Parse(match[1])
			Expect(err).NotTo(HaveOccurred())
			tok := chainUrl.Query().Get("api_key")
			_, err = bm.authHandler.AuthURLAuth(tok)
			Expect(err).NotTo(HaveOccurred())
		})

		It("signs the initrd ipxe-script url correctly", func() {
			mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
			content := getResponseData("ipxe-script", false, nil, "")
			initrdRegex := regexp.MustCompile(`^initrd --name initrd (.+)`)
			match := initrdRegex.FindStringSubmatch(strings.Split(string(content), "\n")[1])
			Expect(match).NotTo(BeNil())

			initrdURL, err := url.Parse(match[1])
			Expect(err).NotTo(HaveOccurred())

			tok := initrdURL.Query().Get("api_key")
			_, err = bm.authHandler.AuthURLAuth(tok)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("with rhsso auth", func() {
		BeforeEach(func() {
			_, cert := auth.GetTokenAndCert(false)
			cfg := &auth.Config{JwkCert: string(cert)}
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)
		})

		It("signs the initrd ipxe-script url correctly", func() {
			mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
			content := getResponseData("ipxe-script", false, nil, "")
			initrdRegex := regexp.MustCompile(`^initrd --name initrd (.+)`)
			match := initrdRegex.FindStringSubmatch(strings.Split(string(content), "\n")[1])
			Expect(match).NotTo(BeNil())

			initrdURL, err := url.Parse(match[1])
			Expect(err).NotTo(HaveOccurred())
			tok := initrdURL.Query().Get("image_token")
			_, err = bm.authHandler.AuthImageAuth(tok)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

var _ = Describe("UpdateInfraEnv - Ignition", func() {
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
		infraEnv = common.InfraEnv{
			PullSecret: fakePullSecret,
			InfraEnv:   models.InfraEnv{ID: &clusterID, PullSecretSet: true, ClusterID: clusterID, Type: models.ImageTypeFullIso.Pointer()},
		}
		err = db.Create(&infraEnv).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("saves the given string to InfraEnv", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateInfraEnvParams{
			InfraEnvID:           *infraEnv.ID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: override},
		}
		mockUsageReports()
		mockOSImages.EXPECT().GetOsImageOrLatest("", gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).AnyTimes()
		mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName),
			eventstest.WithInfraEnvIdMatcher(infraEnv.ID.String())))

		response := bm.UpdateInfraEnv(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(installer.NewUpdateInfraEnvCreated()))

		var updatedInfraEnv common.InfraEnv
		err := db.First(&updatedInfraEnv, "id = ?", clusterID).Error
		Expect(err).ShouldNot(HaveOccurred())
		Expect(updatedInfraEnv.IgnitionConfigOverride).To(Equal(override))
	})

	It("returns not found with a non-existant InfraEnv", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateInfraEnvParams{
			InfraEnvID:           strfmt.UUID(uuid.New().String()),
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: override},
		}
		response := bm.UpdateInfraEnv(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("returns bad request when provided invalid json", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}}}`
		params := installer.UpdateInfraEnvParams{
			InfraEnvID:           *infraEnv.ID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: override},
		}
		response := bm.UpdateInfraEnv(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("returns bad request when provided invalid options", func() {
		// Missing the version
		override := `{"storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateInfraEnvParams{
			InfraEnvID:           *infraEnv.ID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: override},
		}
		response := bm.UpdateInfraEnv(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("returns bad request when provided an old version", func() {
		// Wrong version
		override := `{"ignition": {"version": "3.0.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateInfraEnvParams{
			InfraEnvID:           *infraEnv.ID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: override},
		}
		response := bm.UpdateInfraEnv(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("returns bad request when provided too recent version", func() {
		// Wrong version
		override := `{"ignition": {"version": "9.9.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateInfraEnvParams{
			InfraEnvID:           *infraEnv.ID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: override},
		}
		response := bm.UpdateInfraEnv(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("sets the ignition config override feature usage when given a valid override", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateInfraEnvParams{
			InfraEnvID:           *infraEnv.ID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: override},
		}
		mockOSImages.EXPECT().GetOsImageOrLatest("", gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).AnyTimes()
		mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName),
			eventstest.WithInfraEnvIdMatcher(infraEnv.ID.String())))

		mockUsage.EXPECT().Add(gomock.Any(), usage.IgnitionConfigOverrideUsage, gomock.Any()).Times(1)
		mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
		mockUsage.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(0)

		bm.UpdateInfraEnv(ctx, params)
	})

	It("doesn't set the ignition config override feature usage with a non-existant InfraEnv", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.UpdateInfraEnvParams{
			InfraEnvID:           strfmt.UUID(uuid.New().String()),
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: override},
		}

		mockUsage.EXPECT().Add(gomock.Any(), usage.IgnitionConfigOverrideUsage, gomock.Any()).Times(0)
		mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		mockUsage.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(0)

		bm.UpdateInfraEnv(ctx, params)
	})

	It("doesn't set the ignition config override feature usage when given an empty override", func() {
		override := ""
		params := installer.UpdateInfraEnvParams{
			InfraEnvID:           *infraEnv.ID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: override},
		}
		mockOSImages.EXPECT().GetOsImageOrLatest("", gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(discovery_ignition_3_1, nil).Times(1)
		mockEvents.EXPECT().SendInfraEnvEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ImageInfoUpdatedEventName),
			eventstest.WithInfraEnvIdMatcher(infraEnv.ID.String())))
		mockUsage.EXPECT().Add(gomock.Any(), usage.IgnitionConfigOverrideUsage, gomock.Any()).Times(0)
		mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		mockUsage.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(0)

		bm.UpdateInfraEnv(ctx, params)
	})

	It("returns a bad request when provided config is too large (>256 KiB)", func() {
		// Create a large content for the ignition config
		contentBytes := make([]byte, validations.IgnitionImageSizePadding)
		_, err := rand.Read(contentBytes)
		Expect(err).ToNot(HaveOccurred())
		content := base64.StdEncoding.EncodeToString(contentBytes)
		largeDiscoveryIgnition := fmt.Sprintf(`{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,%s"}}]}}`, content)
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`

		mockIgnitionBuilder.EXPECT().FormatDiscoveryIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(largeDiscoveryIgnition, nil).AnyTimes()
		mockUsage.EXPECT().Add(gomock.Any(), usage.IgnitionConfigOverrideUsage, gomock.Any()).Times(1)
		mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)

		params := installer.UpdateInfraEnvParams{
			InfraEnvID:           *infraEnv.ID,
			InfraEnvUpdateParams: &models.InfraEnvUpdateParams{IgnitionConfigOverride: override},
		}
		response := bm.UpdateInfraEnv(ctx, params)
		verifyApiErrorString(response, http.StatusBadRequest, "over the maximum allowable size")
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
			db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
		mockUsageReports()
		mockClusterRegisterSuccess(true)
		mockAMSSubscription(ctx)
		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                 swag.String("some-cluster-name"),
				OpenshiftVersion:     swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:           swag.String(fakePullSecret),
				HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
			},
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
		c = reply.(*installer.V2RegisterClusterCreated).Payload
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

	addNutanixHost := func(clusterId strfmt.UUID, role models.HostRole) {
		const vsphereInventory = "{\"system_vendor\": {\"manufacturer\": \"Nutanix\", \"product_name\": \"AHV\", \"serial_number\": \"7A8EB5A3-BB97-462B-9E65-92641F3C790F\", \"virtual\": true}}"
		addHost(clusterId, vsphereInventory, role)
	}

	validateInventory := func(host models.Host, manufacturer string) bool {
		var inventory models.Inventory
		if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
			Expect(err).ShouldNot(HaveOccurred())
		}
		return inventory.SystemVendor.Manufacturer == manufacturer
	}

	validateHostsInventory := func(vsphereHostsCount int, genericHostsCount int) {
		getReply := bm.V2GetCluster(ctx, installer.V2GetClusterParams{ClusterID: clusterID}).(*installer.V2GetClusterOK)
		Expect(len(getReply.Payload.Hosts)).Should(Equal(vsphereHostsCount + genericHostsCount))
		vsphereHosts := 0
		genericHosts := 0
		for _, h := range getReply.Payload.Hosts {
			if validateInventory(*h, vsphere.VmwareManufacturer) {
				vsphereHosts++
			}
			if validateInventory(*h, "Red Hat") {
				genericHosts++
			}
		}

		Expect(vsphereHosts).Should(Equal(vsphereHostsCount))
		Expect(genericHosts).Should(Equal(genericHostsCount))

	}

	It("no hosts", func() {
		platformReplay := bm.GetClusterSupportedPlatforms(ctx, installer.GetClusterSupportedPlatformsParams{ClusterID: clusterID})
		Expect(platformReplay).Should(BeAssignableToTypeOf(installer.NewGetClusterSupportedPlatformsOK()))
		platforms := platformReplay.(*installer.GetClusterSupportedPlatformsOK).Payload
		Expect(len(platforms)).Should(Equal(1))
		Expect(platforms[0]).Should(Equal(models.PlatformTypeNone))
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
		Expect(platforms[0]).Should(Equal(models.PlatformTypeNone))
	})

	It("HighAvailabilityMode is nil with single host", func() {
		c.HighAvailabilityMode = nil
		db.Save(c)

		addVsphereHost(clusterID, models.HostRoleMaster)
		validateHostsInventory(1, 0)
		mockProviderRegistry.EXPECT().GetSupportedProvidersByHosts(gomock.Any())
		platformReplay := bm.GetClusterSupportedPlatforms(ctx, installer.GetClusterSupportedPlatformsParams{ClusterID: clusterID})
		Expect(platformReplay).Should(BeAssignableToTypeOf(installer.NewGetClusterSupportedPlatformsOK()))
	})

	It("Unsupported platform - nutanix", func() {
		c.OpenshiftVersion = "4.10"
		db.Save(c)

		addNutanixHost(clusterID, models.HostRoleMaster)
		addNutanixHost(clusterID, models.HostRoleMaster)
		addNutanixHost(clusterID, models.HostRoleMaster)
		expectedPlatforms := []models.PlatformType{"none", "nutanix", "baremetal"}
		mockProviderRegistry.EXPECT().GetSupportedProvidersByHosts(gomock.Any()).Return(expectedPlatforms, nil)
		platformReplay := bm.GetClusterSupportedPlatforms(ctx, installer.GetClusterSupportedPlatformsParams{ClusterID: clusterID})
		platforms := platformReplay.(*installer.GetClusterSupportedPlatformsOK).Payload
		Expect(len(platforms)).Should(Equal(2))
		Expect(platforms).To(Not(ContainElement(models.PlatformTypeNutanix)))

		// Nutanix is supported platform on OCP > 4.11
		c.OpenshiftVersion = "4.12"
		db.Save(c)
		mockProviderRegistry.EXPECT().GetSupportedProvidersByHosts(gomock.Any()).Return(expectedPlatforms, nil)
		platformReplay = bm.GetClusterSupportedPlatforms(ctx, installer.GetClusterSupportedPlatformsParams{ClusterID: clusterID})
		platforms = platformReplay.(*installer.GetClusterSupportedPlatformsOK).Payload
		Expect(len(platforms)).Should(Equal(3))
		Expect(platforms).To(ContainElement(models.PlatformTypeNutanix))
	})

	It("Unsupported platform - external", func() {
		c.OpenshiftVersion = "4.13"
		db.Save(c)

		addNutanixHost(clusterID, models.HostRoleMaster)
		addNutanixHost(clusterID, models.HostRoleMaster)
		addNutanixHost(clusterID, models.HostRoleMaster)
		expectedPlatforms := []models.PlatformType{"none", "external", "baremetal"}
		mockProviderRegistry.EXPECT().GetSupportedProvidersByHosts(gomock.Any()).Return(expectedPlatforms, nil)
		platformReplay := bm.GetClusterSupportedPlatforms(ctx, installer.GetClusterSupportedPlatformsParams{ClusterID: clusterID})
		platforms := platformReplay.(*installer.GetClusterSupportedPlatformsOK).Payload
		Expect(len(platforms)).Should(Equal(2))
		Expect(platforms).To(Not(ContainElement(models.PlatformTypeExternal)))

		// external is supported platform on OCP > 4.13
		c.OpenshiftVersion = "4.14"
		db.Save(c)
		mockProviderRegistry.EXPECT().GetSupportedProvidersByHosts(gomock.Any()).Return(expectedPlatforms, nil)
		platformReplay = bm.GetClusterSupportedPlatforms(ctx, installer.GetClusterSupportedPlatformsParams{ClusterID: clusterID})
		platforms = platformReplay.(*installer.GetClusterSupportedPlatformsOK).Payload
		Expect(len(platforms)).Should(Equal(3))
		Expect(platforms).To(ContainElement(models.PlatformTypeExternal))
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

func verifyFeatureSupportApiErrorString(responder middleware.Responder, expectedHttpStatus int32, featureA, featureB models.FeatureSupportLevelID) {
	ExpectWithOffset(1, responder).To(BeAssignableToTypeOf(common.NewApiError(expectedHttpStatus, nil)))
	concreteError := responder.(*common.ApiErrorResponse)
	ExpectWithOffset(1, concreteError.StatusCode()).To(Equal(expectedHttpStatus))
	ExpectWithOffset(1, concreteError.Error()).To(ContainSubstring("cannot use"))
	ExpectWithOffset(1, concreteError.Error()).To(ContainSubstring(featuresupport.GetFeatureByID(featureA).GetName()))
	ExpectWithOffset(1, concreteError.Error()).To(ContainSubstring(featuresupport.GetFeatureByID(featureB).GetName()))
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
		clusterName   string
		apiVIPDnsname string
		request       *http.Request
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
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

	It("Create V2 AddHosts cluster", func() {
		defaultHostNetworks := make([]*models.HostNetwork, 0)
		defaultHosts := make([]*models.Host, 0)
		openshiftClusterID := strfmt.UUID(uuid.New().String())

		params := installer.V2ImportClusterParams{
			HTTPRequest: request,
			NewImportClusterParams: &models.ImportClusterParams{
				APIVipDnsname:      &apiVIPDnsname,
				Name:               &clusterName,
				OpenshiftVersion:   common.TestDefaultConfig.OpenShiftVersion,
				OpenshiftClusterID: &openshiftClusterID,
			},
		}
		mockClusterApi.EXPECT().RegisterAddHostsCluster(ctx, gomock.Any()).Return(nil).Times(1)
		mockMetric.EXPECT().ClusterRegistered().Times(1)
		res := bm.V2ImportCluster(ctx, params)
		actual := res.(*installer.V2ImportClusterCreated)

		Expect(actual.Payload.HostNetworks).To(Equal(defaultHostNetworks))
		Expect(actual.Payload.Hosts).To(Equal(defaultHosts))
		Expect(actual.Payload.OpenshiftVersion).To(BeEmpty())
		Expect(actual.Payload.OcpReleaseImage).To(BeEmpty())
		Expect(actual.Payload.OpenshiftClusterID).To(Equal(openshiftClusterID))
		Expect(res).Should(BeAssignableToTypeOf(installer.NewV2ImportClusterCreated()))
	})

	It("Create AddHosts cluster - missing release image", func() {
		defaultHostNetworks := make([]*models.HostNetwork, 0)
		defaultHosts := make([]*models.Host, 0)
		openshiftClusterID := strfmt.UUID(uuid.New().String())

		params := installer.V2ImportClusterParams{
			HTTPRequest: request,
			NewImportClusterParams: &models.ImportClusterParams{
				APIVipDnsname:      &apiVIPDnsname,
				Name:               &clusterName,
				OpenshiftVersion:   common.TestDefaultConfig.OpenShiftVersion,
				OpenshiftClusterID: &openshiftClusterID,
			},
		}
		mockClusterApi.EXPECT().RegisterAddHostsCluster(ctx, gomock.Any()).Return(nil).Times(1)
		mockMetric.EXPECT().ClusterRegistered().Times(1)
		res := bm.V2ImportCluster(ctx, params)
		actual := res.(*installer.V2ImportClusterCreated)

		Expect(actual.Payload.HostNetworks).To(Equal(defaultHostNetworks))
		Expect(actual.Payload.Hosts).To(Equal(defaultHosts))
		Expect(actual.Payload.OpenshiftVersion).To(BeEmpty())
		Expect(actual.Payload.OcpReleaseImage).To(BeEmpty())
		Expect(actual.Payload.OpenshiftClusterID).To(Equal(openshiftClusterID))
		Expect(res).Should(BeAssignableToTypeOf(installer.NewV2ImportClusterCreated()))
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

	It("[V2] Install day2 host", func() {
		params := installer.V2InstallHostParams{
			HTTPRequest: request,
			InfraEnvID:  infraEnvId,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
		res := bm.V2InstallHost(ctx, params)
		Expect(res).Should(BeAssignableToTypeOf(installer.NewV2InstallHostAccepted()))
	})

	It("[V2] Install day2 host custom ignition endpoint", func() {
		db.Model(&common.Cluster{}).Where("id = ?", clusterID.String()).Update("ignition_endpoint_url", "http://example.com")

		params := installer.V2InstallHostParams{
			HTTPRequest: request,
			InfraEnvID:  infraEnvId,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile("http://example.com/worker", gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
		res := bm.V2InstallHost(ctx, params)
		Expect(res).Should(BeAssignableToTypeOf(installer.NewV2InstallHostAccepted()))
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

	It("[V2] Install day2 host - ignition creation failed", func() {
		params := installer.V2InstallHostParams{
			HTTPRequest: request,
			InfraEnvID:  infraEnvId,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")).Times(0)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("ign failure")).Times(1)
		res := bm.V2InstallHost(ctx, params)
		verifyApiError(res, http.StatusInternalServerError)
	})

	It("[V2] Install day2 host - ignition upload failed", func() {
		params := installer.V2InstallHostParams{
			HTTPRequest: request,
			InfraEnvID:  infraEnvId,
			HostID:      hostID,
		}
		addHost(hostID, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostInstallationStartedEventName),
			eventstest.WithHostIdMatcher(hostId.String()),
			eventstest.WithInfraEnvIdMatcher(clusterID.String()),
			eventstest.WithClusterIdMatcher(clusterID.String())))
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
		res := bm.InstallSingleDay2HostInternal(ctx, clusterID, clusterID, hostId)
		Expect(res).Should(BeNil())
	})

	It("Install fail Single Day2 Host", func() {
		expectedErrMsg := "some-internal-error"
		hostId := strfmt.UUID(uuid.New().String())
		addHost(hostId, models.HostRoleWorker, models.HostStatusKnown, models.HostKindAddToExistingClusterHost, clusterID, clusterID, getInventoryStr("hostname0", "bootMode", "1.2.3.4/24", "10.11.50.90/16"), db)
		mockHostApi.EXPECT().AutoAssignRole(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).Times(1)
		mockHostApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().Install(gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New(expectedErrMsg)).Times(1)
		mockS3Client.EXPECT().Upload(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockIgnitionBuilder.EXPECT().FormatSecondDayWorkerIgnitionFile(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(secondDayWorkerIgnition, nil).Times(1)
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
		Expect(cfg.DiskEncryptionSupport).Should(BeTrue())
		bm = createInventory(db, cfg)
		bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
			db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, mockOperatorManager, nil, nil, nil, nil, nil, false)
		mockUsageReports()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	DescribeTable(
		"NetworkType/OpenShift version successful registration",
		func(openshiftVersion, networkType string) {
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, openshiftVersion)
			mockAMSSubscription(ctx)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.OpenshiftVersion = swag.String(openshiftVersion)
			clusterParams.NetworkType = &networkType

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
		},
		Entry("OVN supported for 4.15", "4.15.2", models.ClusterCreateParamsNetworkTypeOVNKubernetes),
		Entry("OVN supported for versions above 4.15", "4.18.3", models.ClusterCreateParamsNetworkTypeOVNKubernetes),
		Entry("OVN supported for versions below 4.15", "4.13.1", models.ClusterCreateParamsNetworkTypeOVNKubernetes),
		Entry("SDN supported until 4.14", "4.14.0", models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
	)

	DescribeTable(
		"NetworkType/OpenShift version failed registration",
		func(openshiftVersion, networkType string) {
			clusterParams := getDefaultClusterCreateParams()
			clusterParams.OpenshiftVersion = swag.String(openshiftVersion)
			clusterParams.NetworkType = &networkType

			cpuArchitecture := "irrelevant"
			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  swag.String(cpuArchitecture),
				CPUArchitectures: []string{cpuArchitecture},
				OpenshiftVersion: swag.String(openshiftVersion),
				URL:              swag.String("release_" + openshiftVersion),
				Version:          swag.String(openshiftVersion),
			}

			mockVersions.EXPECT().GetReleaseImage(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(releaseImage, nil).Times(1)
			mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})

			verifyApiError(reply, http.StatusBadRequest)
		},
		Entry("SDN not supported for 4.15", "4.15.2", models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
		Entry("SDN not supported for versions above 4.15", "4.17.6", models.ClusterCreateParamsNetworkTypeOpenShiftSDN),
	)

	// Testing the default network type setting during register flow, with various combinations of platforms,
	// OpenShift version and high availability modes
	// Note: SNO is supported starting OpenShift v4.8, and currently only supports Platform "none"
	It("Default Network Type Setting", func() {
		type clusterParamsForTest struct {
			OpenShiftVersion     *string
			HighAvailabilityMode *string
			DesiredNetworkType   string
			Platform             models.Platform
		}

		tests := []clusterParamsForTest{
			// OpenShift version >= 4.12.0-0.0, not SNO => OVNKubernetes
			{
				OpenShiftVersion:     swag.String("4.13"),
				HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeFull),
				DesiredNetworkType:   models.ClusterCreateParamsNetworkTypeOVNKubernetes,
				Platform:             models.Platform{Type: models.NewPlatformType(models.PlatformTypeBaremetal)},
			},
			// OpenShift version >= 4.12.0-0.0, SNO => OVNKubernetes
			{
				OpenShiftVersion:     swag.String("4.14.2"),
				HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeNone),
				DesiredNetworkType:   models.ClusterCreateParamsNetworkTypeOVNKubernetes,
				Platform:             models.Platform{Type: models.NewPlatformType(models.PlatformTypeNone)},
			},
			// OpenShift version < 4.12.0-0.0, not SNO => OpenShiftSDN
			{
				OpenShiftVersion:     swag.String("4.10.3"),
				HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeFull),
				DesiredNetworkType:   models.ClusterCreateParamsNetworkTypeOpenShiftSDN,
				Platform:             models.Platform{Type: models.NewPlatformType(models.PlatformTypeBaremetal)},
			},
			// OpenShift version < 4.12.0-0.0, SNO => OVNKubernetes
			{
				OpenShiftVersion:     swag.String("4.11.17"),
				HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeNone),
				DesiredNetworkType:   models.ClusterCreateParamsNetworkTypeOVNKubernetes,
				Platform:             models.Platform{Type: models.NewPlatformType(models.PlatformTypeNone)},
			},
			// OpenShift version >= 4.12.0-0.0, HighAvailability = nil => OVNKubernetes
			{
				OpenShiftVersion:     swag.String("4.13.2"),
				HighAvailabilityMode: nil,
				DesiredNetworkType:   models.ClusterCreateParamsNetworkTypeOVNKubernetes,
				Platform:             models.Platform{Type: models.NewPlatformType(models.PlatformTypeBaremetal)},
			},
			// OpenShift version < 4.12.0-0.0, HighAvailability = nil => OpenShiftSDN
			{
				OpenShiftVersion:     swag.String("4.8.5"),
				HighAvailabilityMode: nil,
				DesiredNetworkType:   models.ClusterCreateParamsNetworkTypeOpenShiftSDN,
				Platform:             models.Platform{Type: models.NewPlatformType(models.PlatformTypeBaremetal)},
			},
			// OpenShift version >= 4.12.0-0.0, SNO, IPv4 => OVNKubernetes
			{
				OpenShiftVersion:     swag.String("4.12.0-ec.3"),
				HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeNone),
				DesiredNetworkType:   models.ClusterCreateParamsNetworkTypeOVNKubernetes,
				Platform:             models.Platform{Type: models.NewPlatformType(models.PlatformTypeNone)},
			},
			// OpenShift version < 4.12.0-0.0, SNO => OVNKubernetes
			{
				OpenShiftVersion:     swag.String("4.11.0-rc.3"),
				HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeNone),
				DesiredNetworkType:   models.ClusterCreateParamsNetworkTypeOVNKubernetes,
				Platform:             models.Platform{Type: models.NewPlatformType(models.PlatformTypeNone)},
			},
			// OpenShift version >= 4.12.0-0.0, SNO => OVNKubernetes
			{
				OpenShiftVersion:     swag.String("4.12-fc.2"),
				HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeNone),
				DesiredNetworkType:   models.ClusterCreateParamsNetworkTypeOVNKubernetes,
				Platform:             models.Platform{Type: models.NewPlatformType(models.PlatformTypeNone)},
			},
			// OpenShift version >= 4.12.0-0.0, SNO => OVNKubernetes
			{
				OpenShiftVersion:     swag.String("4.12-rc.1"),
				HighAvailabilityMode: swag.String(models.ClusterCreateParamsHighAvailabilityModeNone),
				DesiredNetworkType:   models.ClusterCreateParamsNetworkTypeOVNKubernetes,
				Platform:             models.Platform{Type: models.NewPlatformType(models.PlatformTypeNone)},
			},
		}

		for _, t := range tests {
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, *t.OpenShiftVersion)
			mockAMSSubscription(ctx)

			NewClusterParams := getDefaultClusterCreateParams()
			NewClusterParams.OpenshiftVersion = t.OpenShiftVersion
			NewClusterParams.HighAvailabilityMode = t.HighAvailabilityMode
			NewClusterParams.Platform = &t.Platform

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: NewClusterParams,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(swag.StringValue(actual.NetworkType)).To(Equal(t.DesiredNetworkType))
		}
	})

	Context("Platform", func() {
		getClusterCreateParams := func(highAvailabilityMode *string) *models.ClusterCreateParams {

			return &models.ClusterCreateParams{
				Name:                 swag.String("some-cluster-name"),
				OpenshiftVersion:     swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:           swag.String(fakePullSecret),
				CPUArchitecture:      models.ClusterCPUArchitectureX8664,
				HighAvailabilityMode: highAvailabilityMode,
			}
		}

		Context("HighAvailabilityMode = High", func() {
			It("user-managed-networking false", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(nil)
				params.UserManagedNetworking = swag.Bool(false)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
			})

			It("user-managed-networking true", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(nil)
				params.UserManagedNetworking = swag.Bool(true)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
			})

			It("Baremetal platform", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
			})

			It("Baremetal platform and UserManagedNetworking=false", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}
				params.UserManagedNetworking = swag.Bool(false)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
			})

			It("vsphere platform and UserManagedNetworking=true", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)}
				params.UserManagedNetworking = swag.Bool(true)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))
			})

			It("external platform and UserManagedNetworking=true on OCP 4.14 - success", func() {
				mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.14")
				mockAMSSubscription(ctx)

				platformName := "platform-name"
				cloudControllerManager := models.PlatformExternalCloudControllerManagerEmpty

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           &platformName,
						CloudControllerManager: &cloudControllerManager,
					},
				}
				params.UserManagedNetworking = swag.Bool(true)
				params.OpenshiftVersion = swag.String("4.14")
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeExternal))
				Expect(*actual.Platform.External.PlatformName).Should(Equal(platformName))
				Expect(*actual.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
				Expect(actual.OpenshiftVersion).To(Equal("4.14"))
			})

			It("external platform and UserManagedNetworking=true on OCP 4.13 - failure", func() {
				cpuArchitecture := common.X86CPUArchitecture
				openshiftVersion := "4.13"
				errorMsg := "cannot use External Platform Integration because it's not compatible with the x86_64 architecture on version 4.13.0 of OpenShift"
				platformName := "platform-name"
				cloudControllerManager := models.PlatformExternalCloudControllerManagerEmpty

				mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.ReleaseImage{
					CPUArchitecture:  swag.String(cpuArchitecture),
					CPUArchitectures: []string{cpuArchitecture},
					OpenshiftVersion: swag.String(openshiftVersion),
					URL:              swag.String("release_4.13"),
					Version:          swag.String("4.13.0"),
				}, nil).Times(1)
				mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
				mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeExternal),
					External: &models.PlatformExternal{
						PlatformName:           &platformName,
						CloudControllerManager: &cloudControllerManager,
					},
				}
				params.UserManagedNetworking = swag.Bool(true)
				params.OpenshiftVersion = swag.String("4.13")
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})

				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})

			It("Baremetal platform and UserManagedNetworking=true - failed", func() {
				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}
				params.UserManagedNetworking = swag.Bool(true)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("Nutanix platform and UserManagedNetworking=true - failed", func() {

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)}
				params.UserManagedNetworking = swag.Bool(true)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("Nutanix platform and arm64 - failed", func() {
				mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return(
					[]string{minimalOpenShiftVersionForNutanix, common.ARM64CPUArchitecture}).Times(1)
				mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.ReleaseImage{
					CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture},
					OpenshiftVersion: swag.String("4.11.1"),
					URL:              swag.String("release_4.11.1"),
					Version:          swag.String("4.11.1-multi"),
				}, nil).Times(1)
				mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeNutanix),
				}
				params.OpenshiftVersion = swag.String(minimalOpenShiftVersionForNutanix)
				params.CPUArchitecture = common.ARM64CPUArchitecture
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("arm64 cluster update to Nutanix platform - failed", func() {
				mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureArm64, "4.11")
				mockAMSSubscription(ctx)
				mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return(
					[]string{minimalOpenShiftVersionForNutanix, common.ARM64CPUArchitecture}).Times(1)

				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeFull))
				params.Platform = &models.Platform{
					Type: common.PlatformTypePtr(models.PlatformTypeNone),
				}
				params.OpenshiftVersion = swag.String(minimalOpenShiftVersionForNutanix)
				params.CPUArchitecture = common.ARM64CPUArchitecture
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				c := reply.(*installer.V2RegisterClusterCreated).Payload

				infraEnvID := strfmt.UUID(uuid.New().String())
				err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
					ID:              &infraEnvID,
					ClusterID:       *c.ID,
					CPUArchitecture: models.InfraEnvCPUArchitectureArm64,
				}}).Error
				Expect(err).ShouldNot(HaveOccurred())

				reply2 := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: *c.ID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						Platform: &models.Platform{
							Type: common.PlatformTypePtr(models.PlatformTypeNutanix),
						},
						UserManagedNetworking: swag.Bool(false),
					},
				})
				verifyApiErrorString(reply2, http.StatusBadRequest, "cannot use Nutanix Platform Integration because it's not compatible with the arm64 architecture on version 4.11 of OpenShift")
			})

			It("None platform type", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
			})

			It("None platform and UserManagedNetworking=false - failed", func() {

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
				params.UserManagedNetworking = swag.Bool(false)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("None platform and UserManagedNetworking=true", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
				params.UserManagedNetworking = swag.Bool(true)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))

				var result installcfg.InstallerConfigBaremetal
				installConfig := createInstallConfigBuilder()
				mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)

				data, err := installConfig.GetInstallConfig(&common.Cluster{Cluster: *actual}, []*common.InfraEnv{}, "")
				Expect(err).ShouldNot(HaveOccurred())
				err = json.Unmarshal(data, &result)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(result.Platform.None).ShouldNot(BeNil())
			})

			It("vsphere platform and UserManagedNetworking=true", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(nil)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)}
				params.UserManagedNetworking = swag.Bool(true)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))

				var result installcfg.InstallerConfigBaremetal
				installConfig := createInstallConfigBuilder()
				mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
				data, err := installConfig.GetInstallConfig(&common.Cluster{Cluster: *actual}, []*common.InfraEnv{}, "")
				Expect(err).ShouldNot(HaveOccurred())
				err = json.Unmarshal(data, &result)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(result.Platform.Vsphere).ShouldNot(BeNil())
				Expect(result.Platform.None).Should(BeNil())
				Expect(result.Platform.Baremetal).Should(BeNil())

				Expect(result.Platform.Vsphere.DeprecatedAPIVIP).Should(Equal(""))
				Expect(result.Platform.Vsphere.DeprecatedIngressVIP).Should(Equal(""))
			})

			It("vsphere platform and UserManagedNetworking=false", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(nil)
				apiVip := "1.2.3.5"
				ingressVip := "1.2.3.6"

				params.APIVips = []*models.APIVip{{IP: models.IP(apiVip)}}
				params.IngressVips = []*models.IngressVip{{IP: models.IP(ingressVip)}}
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)}
				params.UserManagedNetworking = swag.Bool(false)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(false))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeVsphere))

				var result installcfg.InstallerConfigBaremetal
				installConfig := createInstallConfigBuilder()
				mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(2)
				data, err := installConfig.GetInstallConfig(&common.Cluster{Cluster: *actual}, []*common.InfraEnv{}, "")
				Expect(err).ShouldNot(HaveOccurred())
				err = json.Unmarshal(data, &result)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(result.Platform.Vsphere).ShouldNot(BeNil())
				Expect(result.Platform.None).Should(BeNil())
				Expect(result.Platform.Baremetal).Should(BeNil())

				Expect(result.Platform.Vsphere.DeprecatedAPIVIP).Should(Equal(apiVip))
				Expect(result.Platform.Vsphere.DeprecatedIngressVIP).Should(Equal(ingressVip))
			})
		})

		Context("HighAvailabilityMode = None", func() {
			It("None platform when HighAvailabilityMode is None", func() {
				mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.9")
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
			})

			It("Fail to disable UserManagedNetworking when HighAvailabilityMode is None", func() {
				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				params.UserManagedNetworking = swag.Bool(false)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("user-managed-networking true", func() {
				mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.9")
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				params.UserManagedNetworking = swag.Bool(true)
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
			})

			It("Fail to set baremetal platform when HighAvailabilityMode is None", func() {

				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("Fail to set baremetal platform when HighAvailabilityMode is None", func() {
				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				params.UserManagedNetworking = swag.Bool(false)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("Fail to set vsphere platform when HighAvailabilityMode is None", func() {
				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeVsphere)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("Fail to set nutanix platform when HighAvailabilityMode is None", func() {
				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.11")
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNutanix)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("Fail to set baremetal platform and enable UserManagedNetworking when HighAvailabilityMode is None", func() {

				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				params.UserManagedNetworking = swag.Bool(true)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("Set none platform when HighAvailabilityMode is None", func() {
				mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.9")
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
			})

			It("Set vsphere platform when HighAvailabilityMode is None", func() {
				mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.9")
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				params.UserManagedNetworking = swag.Bool(true)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
			})

			It("Fail to set none platform and disable UserManagedNetworking when HighAvailabilityMode is None", func() {
				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				params.UserManagedNetworking = swag.Bool(false)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				verifyApiError(reply, http.StatusBadRequest)
			})

			It("Set none platform and enable UserManagedNetworking when HighAvailabilityMode is None", func() {
				mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.9")
				mockAMSSubscription(ctx)

				params := getClusterCreateParams(swag.String(models.ClusterCreateParamsHighAvailabilityModeNone))
				params.OpenshiftVersion = swag.String("4.9")
				params.UserManagedNetworking = swag.Bool(true)
				params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
				Expect(*actual.Platform.Type).To(Equal(models.PlatformTypeNone))
			})
		})

	})

	It("success", func() {
		mockClusterRegisterSuccess(true)
		mockAMSSubscription(ctx)

		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: getDefaultClusterCreateParams(),
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
	})

	Context("ValidateIncompatibleFeatures", func() {
		It("s390x on 4.13 OCP version", func() {
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
			mockProviderRegistry.EXPECT().SetPlatformUsages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			mockMetric.EXPECT().ClusterRegistered().Times(1)
			mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterRegistrationSucceededEventName))).Times(1)
			mockAMSSubscription(ctx)
			params := getDefaultClusterCreateParams()

			params.OpenshiftVersion = swag.String("4.13")
			params.CPUArchitecture = models.ClusterCPUArchitectureS390x
			params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
			mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return([]string{common.X86CPUArchitecture, common.S390xCPUArchitecture}).Times(1)

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  &params.CPUArchitecture,
				OpenshiftVersion: params.OpenshiftVersion,
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13-s390x"),
				Version:          swag.String("4.13.0"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			}

			mockVersions.EXPECT().GetReleaseImage(ctx, *params.OpenshiftVersion, params.CPUArchitecture, *params.PullSecret).Return(releaseImage, nil).Times(1)

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
			Expect(actual.CPUArchitecture).To(Equal(models.ClusterCPUArchitectureS390x))
		})

		It("s390x on OCP version lower than 4.12", func() {
			mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return([]string{common.X86CPUArchitecture, common.S390xCPUArchitecture}).Times(1)

			params := getDefaultClusterCreateParams()
			params.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeNone),
			}
			params.OpenshiftVersion = swag.String("4.11")
			params.CPUArchitecture = models.ClusterCPUArchitectureS390x

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  &params.CPUArchitecture,
				OpenshiftVersion: params.OpenshiftVersion,
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12-x86_64"),
				Version:          swag.String("4.11.0"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			}

			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{}).Times(1)
			mockVersions.EXPECT().GetReleaseImage(ctx, *params.OpenshiftVersion, params.CPUArchitecture, *params.PullSecret).Return(releaseImage, nil).Times(1)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("ppc64le on OCP version lower than 4.12", func() {
			mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return([]string{common.X86CPUArchitecture, common.PowerCPUArchitecture}).Times(1)

			params := getDefaultClusterCreateParams()
			params.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeNone),
			}
			params.OpenshiftVersion = swag.String("4.11")
			params.CPUArchitecture = models.ClusterCPUArchitecturePpc64le

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  &params.CPUArchitecture,
				OpenshiftVersion: params.OpenshiftVersion,
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.12-x86_64"),
				Version:          swag.String("4.11.0"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			}

			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{}).Times(1)
			mockVersions.EXPECT().GetReleaseImage(ctx, *params.OpenshiftVersion, params.CPUArchitecture, *params.PullSecret).Return(releaseImage, nil).Times(1)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("Sno isn't compatible with s390x on 4.12", func() {
			mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return([]string{common.X86CPUArchitecture, common.S390xCPUArchitecture}).Times(1)

			params := getDefaultClusterCreateParams()
			params.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeNone),
			}
			params.OpenshiftVersion = swag.String("4.12")
			params.CPUArchitecture = models.ClusterCPUArchitectureS390x
			params.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)

			releaseImage := &models.ReleaseImage{
				CPUArchitectures: []string{models.ClusterCPUArchitectureX8664, params.CPUArchitecture},
				OpenshiftVersion: params.OpenshiftVersion,
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13-multi"),
				Version:          swag.String("4.12.0"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			}

			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{}).Times(1)
			mockVersions.EXPECT().GetReleaseImage(ctx, *params.OpenshiftVersion, params.CPUArchitecture, *params.PullSecret).Return(releaseImage, nil).Times(1)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})

		It("Sno is compatible with s390x on 4.13", func() {
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
			mockProviderRegistry.EXPECT().SetPlatformUsages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			mockMetric.EXPECT().ClusterRegistered().Times(1)
			mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterRegistrationSucceededEventName))).Times(1)
			mockAMSSubscription(ctx)
			params := getDefaultClusterCreateParams()

			params.OpenshiftVersion = swag.String("4.13")
			params.CPUArchitecture = models.ClusterCPUArchitectureS390x
			params.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
			params.Platform = nil

			mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return([]string{common.X86CPUArchitecture, common.S390xCPUArchitecture}).Times(1)

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  &params.CPUArchitecture,
				OpenshiftVersion: params.OpenshiftVersion,
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13-s390x"),
				Version:          swag.String("4.13.0"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			}

			mockVersions.EXPECT().GetReleaseImage(ctx, *params.OpenshiftVersion, params.CPUArchitecture, *params.PullSecret).Return(releaseImage, nil).Times(1)

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(swag.StringValue(actual.HighAvailabilityMode)).To(Equal(models.ClusterHighAvailabilityModeNone))
			Expect(actual.CPUArchitecture).To(Equal(models.ClusterCPUArchitectureS390x))
		})

		It("ClusterManagedNetworking isn't compatible with s390x", func() {
			params := getDefaultClusterCreateParams()
			params.OpenshiftVersion = swag.String("4.13")
			params.CPUArchitecture = models.ClusterCPUArchitectureS390x
			params.Platform = nil
			params.UserManagedNetworking = swag.Bool(false)

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("ClusterManagedNetworking isn't compatible with arm64", func() {
			mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return([]string{common.ARM64CPUArchitecture}).Times(1)
			params := getDefaultClusterCreateParams()
			params.OpenshiftVersion = swag.String("4.10")
			params.CPUArchitecture = models.ClusterCPUArchitectureArm64
			params.Platform = nil
			params.UserManagedNetworking = swag.Bool(false)
			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  &params.CPUArchitecture,
				OpenshiftVersion: params.OpenshiftVersion,
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.10-x86_64"),
				Version:          swag.String("4.10.0"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			}

			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{}).Times(1)
			mockVersions.EXPECT().GetReleaseImage(ctx, *params.OpenshiftVersion, params.CPUArchitecture, *params.PullSecret).Return(releaseImage, nil).Times(1)

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("Baremetal platform isn't compatible with s390x", func() {
			params := getDefaultClusterCreateParams()
			params.OpenshiftVersion = swag.String("4.13")
			params.CPUArchitecture = models.ClusterCPUArchitectureS390x
			params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)}

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
		It("CNV isn't compatible with s390x", func() {
			mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
				DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
					return operators, nil
				}).Times(1)

			mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return([]string{common.X86CPUArchitecture, common.S390xCPUArchitecture}).Times(1)
			mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			params := getDefaultClusterCreateParams()
			params.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeNone),
			}
			params.UserManagedNetworking = swag.Bool(true)
			params.OpenshiftVersion = swag.String("4.13")
			params.CPUArchitecture = models.ClusterCPUArchitectureS390x

			mockOperatorManager.EXPECT().GetOperatorByName("cnv").Return(
				&models.MonitoredOperator{
					Name:             "cnv",
					Namespace:        "openshift-cnv",
					OperatorType:     models.OperatorTypeOlm,
					SubscriptionName: "hco-operatorhub",
					TimeoutSeconds:   60 * 60,
				}, nil).Times(1)

			params.OlmOperators = []*models.OperatorCreateParams{
				{Name: "cnv"},
			}

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  &params.CPUArchitecture,
				OpenshiftVersion: params.OpenshiftVersion,
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13-x86_64"),
				Version:          swag.String("4.13.0"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			}

			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{}).Times(1)
			mockVersions.EXPECT().GetReleaseImage(ctx, *params.OpenshiftVersion, params.CPUArchitecture, *params.PullSecret).Return(releaseImage, nil).Times(1)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})

		It("s390x on 4.13 OCP version", func() {
			mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
			mockProviderRegistry.EXPECT().SetPlatformUsages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			mockMetric.EXPECT().ClusterRegistered().Times(1)
			mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterRegistrationSucceededEventName))).Times(1)
			mockAMSSubscription(ctx)
			params := getDefaultClusterCreateParams()

			params.OpenshiftVersion = swag.String("4.13")
			params.CPUArchitecture = models.ClusterCPUArchitectureS390x
			params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
			mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return([]string{common.X86CPUArchitecture, common.S390xCPUArchitecture}).Times(1)
			mockOperatorManager.EXPECT().GetOperatorByName("odf").Return(
				&models.MonitoredOperator{
					Name:           "odf",
					OperatorType:   models.OperatorTypeOlm,
					Namespace:      "openshift-storage",
					TimeoutSeconds: 30 * 60,
				}, nil).Times(1)

			params.OlmOperators = []*models.OperatorCreateParams{
				{Name: "odf"},
			}

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  &params.CPUArchitecture,
				OpenshiftVersion: params.OpenshiftVersion,
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13-s390x"),
				Version:          swag.String("4.13.0"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			}

			mockVersions.EXPECT().GetReleaseImage(ctx, *params.OpenshiftVersion, params.CPUArchitecture, *params.PullSecret).Return(releaseImage, nil).Times(1)
			mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
				DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
					return append(operators, &models.MonitoredOperator{
						Name:           "lso",
						OperatorType:   models.OperatorTypeOlm,
						Namespace:      "openshift-storage",
						TimeoutSeconds: 30 * 60,
					}), nil
				}).Times(1)
			mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(swag.BoolValue(actual.UserManagedNetworking)).To(Equal(true))
			Expect(actual.CPUArchitecture).To(Equal(models.ClusterCPUArchitectureS390x))
		})

		It("LVM isn't compatible with s390x", func() {
			mockOperatorManager.EXPECT().ResolveDependencies(gomock.Any(), gomock.Any()).
				DoAndReturn(func(commonCluster *common.Cluster, operators []*models.MonitoredOperator) ([]*models.MonitoredOperator, error) {
					return operators, nil
				}).Times(1)
			mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return([]string{common.X86CPUArchitecture, common.S390xCPUArchitecture}).Times(1)

			mockOperatorManager.EXPECT().EnsureOperatorPrerequisite(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			params := getDefaultClusterCreateParams()
			params.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeNone),
			}
			params.UserManagedNetworking = swag.Bool(true)
			params.OpenshiftVersion = swag.String("4.13")
			params.CPUArchitecture = models.ClusterCPUArchitectureS390x
			mockOperatorManager.EXPECT().GetOperatorByName("lvm").Return(
				&models.MonitoredOperator{
					Name:           "lvm",
					OperatorType:   models.OperatorTypeOlm,
					Namespace:      "openshift-storage",
					TimeoutSeconds: 30 * 60,
				}, nil).Times(1)

			params.OlmOperators = []*models.OperatorCreateParams{
				{Name: "lvm"},
			}

			releaseImage := &models.ReleaseImage{
				CPUArchitecture:  &params.CPUArchitecture,
				OpenshiftVersion: params.OpenshiftVersion,
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13-x86_64"),
				Version:          swag.String("4.13.0"),
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
			}

			mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{}).Times(1)
			mockVersions.EXPECT().GetReleaseImage(ctx, *params.OpenshiftVersion, params.CPUArchitecture, *params.PullSecret).Return(releaseImage, nil).Times(1)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			verifyApiError(reply, http.StatusBadRequest)
		})
	})

	It("SchedulableMasters default value", func() {
		mockClusterRegisterSuccess(true)
		mockAMSSubscription(ctx)

		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: getDefaultClusterCreateParams(),
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.SchedulableMasters).To(Equal(swag.Bool(false)))
	})

	It("SchedulableMasters non default value", func() {
		mockClusterRegisterSuccess(true)
		mockAMSSubscription(ctx)
		clusterParams := getDefaultClusterCreateParams()
		clusterParams.SchedulableMasters = swag.Bool(true)
		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: clusterParams,
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.SchedulableMasters).To(Equal(swag.Bool(true)))
	})

	It("SchedulableMastersForcedTrue default value", func() {
		mockClusterRegisterSuccess(true)
		mockAMSSubscription(ctx)

		clusterParams := getDefaultClusterCreateParams()
		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: clusterParams,
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.SchedulableMastersForcedTrue).To(Equal(swag.Bool(true)))
	})

	It("UserManagedNetworking default value", func() {
		mockClusterRegisterSuccess(true)
		mockAMSSubscription(ctx)

		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: getDefaultClusterCreateParams(),
		})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.UserManagedNetworking).To(Equal(swag.Bool(false)))
		Expect(*actual.Payload.Platform.Type).To(Equal(models.PlatformTypeBaremetal))
	})

	It("Fail UserManagedNetworking with VIP DHCP", func() {
		clusterParams := getDefaultClusterCreateParams()
		clusterParams.UserManagedNetworking = swag.Bool(true)
		clusterParams.VipDhcpAllocation = swag.Bool(true)
		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: clusterParams,
		})
		verifyApiError(reply, http.StatusBadRequest)
	})

	It("Fail multi-node UserManagedNetworking with API Vip and Ingress Vip", func() {
		clusterParams := getDefaultClusterCreateParams()
		clusterParams.UserManagedNetworking = swag.Bool(true)
		clusterParams.APIVips = []*models.APIVip{{IP: "10.35.10.11"}}
		clusterParams.IngressVips = []*models.IngressVip{{IP: "10.35.10.10"}}
		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: clusterParams,
		})
		verifyApiError(reply, http.StatusBadRequest)
	})

	It("Fail SNO (UserManagedNetworking) with API Vip and Ingress Vip", func() {
		clusterParams := getDefaultClusterCreateParams()
		clusterParams.UserManagedNetworking = swag.Bool(true)
		clusterParams.APIVips = []*models.APIVip{{IP: "10.35.10.11"}}
		clusterParams.IngressVips = []*models.IngressVip{{IP: "10.35.10.10"}}
		clusterParams.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
		clusterParams.OpenshiftVersion = swag.String("4.12")
		clusterParams.Platform = &models.Platform{
			Type: common.PlatformTypePtr(models.PlatformTypeNone),
		}
		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: clusterParams,
		})
		verifyApiError(reply, http.StatusBadRequest)
	})

	Context("Disk encryption", func() {

		It("Using tang mode without tang_servers", func() {
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					DiskEncryption: &models.DiskEncryption{
						EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
						Mode:     swag.String(models.DiskEncryptionModeTang),
					},
					OpenshiftVersion:     swag.String("4.12.0"),
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
				},
			})
			verifyApiErrorString(reply, http.StatusBadRequest, "Setting Tang mode but tang_servers isn't set")
		})

		It("Invalid Tang server URL", func() {
			By("URL not set", func() {
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn:    swag.String(models.DiskEncryptionEnableOnAll),
							Mode:        swag.String(models.DiskEncryptionModeTang),
							TangServers: `[{"URL":"","Thumbprint":""}]`,
						},
						OpenshiftVersion:     swag.String("4.12.0"),
						HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull)},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, "empty url")
			})

			By("URL not valid", func() {
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn:    swag.String(models.DiskEncryptionEnableOnAll),
							Mode:        swag.String(models.DiskEncryptionModeTang),
							TangServers: `[{"URL":"invalidUrl","Thumbprint":""}]`,
						},
						OpenshiftVersion:     swag.String("4.12.0"),
						HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, "invalid URI for reques")
			})
		})

		It("Tang thumbprint not set", func() {
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					DiskEncryption: &models.DiskEncryption{
						EnableOn:    swag.String(models.DiskEncryptionEnableOnAll),
						Mode:        swag.String(models.DiskEncryptionModeTang),
						TangServers: `[{"URL":"http://tang.example.com:7500","Thumbprint":""}]`,
					},
					OpenshiftVersion:     swag.String("4.12.0"),
					HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
				},
			})
			verifyApiErrorString(reply, http.StatusBadRequest, "Tang thumbprint isn't set")
		})

		It("Disabling with specifying mode", func() {
			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			}

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
		})

		It("Specifying mode without state", func() {
			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.DiskEncryption = &models.DiskEncryption{
				Mode: swag.String(models.DiskEncryptionModeTpmv2),
			}

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.DiskEncryption.EnableOn).To(Equal(swag.String(models.DiskEncryptionEnableOnNone)))
		})

		It("Enabling with explicit TPMv2 mode", func() {
			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
				Mode:     swag.String(models.DiskEncryptionModeTpmv2),
			}

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.DiskEncryption.EnableOn).To(Equal(swag.String(models.DiskEncryptionEnableOnAll)))
			Expect(actual.Payload.DiskEncryption.Mode).To(Equal(swag.String(models.DiskEncryptionModeTpmv2)))
		})

		It("Enabling with default TPMv2 mode", func() {
			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
			}

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.DiskEncryption.EnableOn).To(Equal(swag.String(models.DiskEncryptionEnableOnAll)))
			Expect(actual.Payload.DiskEncryption.Mode).To(Equal(swag.String(models.DiskEncryptionModeTpmv2)))
		})

		It("Disabling", func() {
			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.DiskEncryption = &models.DiskEncryption{
				EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
			}

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.DiskEncryption.EnableOn).To(Equal(swag.String(models.DiskEncryptionEnableOnNone)))
		})

		It("updating cluster with disk encryption", func() {

			var c *models.Cluster
			diskEncryptionBm := createInventory(db, cfg)
			diskEncryptionBm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, mockOperatorManager, nil, nil, nil, nil, nil, false)

			By("Register cluster", func() {

				mockClusterRegisterSuccess(true)
				mockUsageReports()
				mockAMSSubscription(ctx)

				reply := diskEncryptionBm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: getDefaultClusterCreateParams(),
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				c = reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(swag.StringValue(c.DiskEncryption.EnableOn)).To(Equal(models.DiskEncryptionEnableOnNone))
				Expect(swag.StringValue(c.DiskEncryption.Mode)).To(Equal(models.DiskEncryptionModeTpmv2))
			})

			By("Update cluster with full object", func() {

				mockOperatorManager.EXPECT().ValidateCluster(ctx, gomock.Any())
				mockEvents.EXPECT().SendClusterEvent(ctx, gomock.Any())

				reply := diskEncryptionBm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: *c.ID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
							Mode:     swag.String(models.DiskEncryptionModeTpmv2),
						},
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				c = reply.(*installer.V2UpdateClusterCreated).Payload
				Expect(swag.StringValue(c.DiskEncryption.EnableOn)).To(Equal(models.DiskEncryptionEnableOnAll))
				Expect(swag.StringValue(c.DiskEncryption.Mode)).To(Equal(models.DiskEncryptionModeTpmv2))
			})

			By("Update cluster with partial object", func() {

				mockOperatorManager.EXPECT().ValidateCluster(ctx, gomock.Any())

				reply := diskEncryptionBm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: *c.ID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
						},
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				c = reply.(*installer.V2UpdateClusterCreated).Payload
				Expect(swag.StringValue(c.DiskEncryption.EnableOn)).To(Equal(models.DiskEncryptionEnableOnMasters))
				Expect(swag.StringValue(c.DiskEncryption.Mode)).To(Equal(models.DiskEncryptionModeTpmv2))
			})

			By("Update cluster with emtpy object", func() {

				mockOperatorManager.EXPECT().ValidateCluster(ctx, gomock.Any())

				reply := diskEncryptionBm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: *c.ID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						DiskEncryption: &models.DiskEncryption{},
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				c = reply.(*installer.V2UpdateClusterCreated).Payload
				Expect(swag.StringValue(c.DiskEncryption.EnableOn)).To(Equal(models.DiskEncryptionEnableOnMasters))
				Expect(swag.StringValue(c.DiskEncryption.Mode)).To(Equal(models.DiskEncryptionModeTpmv2))
			})

			By("Update database to mark cluster as imported", func() {
				response := db.Model(&common.Cluster{}).Where("id = ?", c.ID.String()).Updates(map[string]interface{}{"imported": true})
				Expect(response.Error).ToNot(HaveOccurred())
			})

			By("Should not allow update of encryption settings for imported cluster", func() {
				reply := diskEncryptionBm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: *c.ID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
							Mode:     swag.String(models.DiskEncryptionModeTpmv2),
						},
					},
				})
				err := reply.(*common.ApiErrorResponse)
				Expect(err.StatusCode()).To(Equal(int32(http.StatusBadRequest)))
				expectedError := fmt.Sprintf("cannot update cluster % s, it is not permitted to change disk encryption settings for an imported cluster", c.ID)
				Expect(err.Error()).To(ContainSubstring(expectedError))
			})
		})
	})

	var _ = Describe("API and Ingress VIPs", func() {
		BeforeEach(func() {
			Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
			db, dbName = common.PrepareTestDB()
			cfg.DiskEncryptionSupport = false
			bm = createInventory(db, cfg)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
			mockUsageReports()
		})

		AfterEach(func() {
			ctrl.Finish()
			common.DeleteTestDB(db, dbName)
		})

		Context("V2 Register cluster", func() {
			It("Dual-stack cluster with VIPs - positive", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				apiVip := "1.2.3.5"
				ingressVip := "1.2.3.6"

				clusterCreateParams := getDefaultClusterCreateParams()
				clusterCreateParams.APIVips = []*models.APIVip{{IP: models.IP(apiVip)}}
				clusterCreateParams.IngressVips = []*models.IngressVip{{IP: models.IP(ingressVip)}}
				clusterCreateParams.ClusterNetworks = common.TestDualStackNetworking.ClusterNetworks
				clusterCreateParams.MachineNetworks = common.TestDualStackNetworking.MachineNetworks
				clusterCreateParams.ServiceNetworks = common.TestDualStackNetworking.ServiceNetworks
				clusterCreateParams.VipDhcpAllocation = swag.Bool(false)

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: clusterCreateParams,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(string(actual.APIVips[0].IP)).To(Equal(apiVip))
				Expect(string(actual.IngressVips[0].IP)).To(Equal(ingressVip))
				Expect(actual.VipDhcpAllocation).To(Equal(swag.Bool(false)))
				Expect(actual.ClusterNetworks).To(Equal(common.TestDualStackNetworking.ClusterNetworks))
				Expect(actual.MachineNetworks).To(Equal(common.TestDualStackNetworking.MachineNetworks))
				Expect(actual.ServiceNetworks).To(Equal(common.TestDualStackNetworking.ServiceNetworks))
			})
			It("Single-stack with VIPs - positive", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				apiVip := "1.2.3.5"
				ingressVip := "1.2.3.6"

				clusterCreateParams := getDefaultClusterCreateParams()
				clusterCreateParams.APIVips = []*models.APIVip{{IP: models.IP(apiVip)}}
				clusterCreateParams.IngressVips = []*models.IngressVip{{IP: models.IP(ingressVip)}}
				clusterCreateParams.ClusterNetworks = common.TestIPv4Networking.ClusterNetworks
				clusterCreateParams.MachineNetworks = common.TestIPv4Networking.MachineNetworks
				clusterCreateParams.ServiceNetworks = common.TestIPv4Networking.ServiceNetworks
				clusterCreateParams.VipDhcpAllocation = swag.Bool(false)

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: clusterCreateParams,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated).Payload
				Expect(string(actual.APIVips[0].IP)).To(Equal(apiVip))
				Expect(string(actual.IngressVips[0].IP)).To(Equal(ingressVip))
				Expect(actual.VipDhcpAllocation).To(Equal(swag.Bool(false)))
				Expect(actual.ClusterNetworks).To(Equal(common.TestIPv4Networking.ClusterNetworks))
				Expect(actual.MachineNetworks).To(Equal(common.TestIPv4Networking.MachineNetworks))
				Expect(actual.ServiceNetworks).To(Equal(common.TestIPv4Networking.ServiceNetworks))
			})
			It("Dual-stack with reused VIPs", func() {
				apiVip := "1.2.3.5"
				ingressVip := "1.2.3.5"

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						APIVips:           []*models.APIVip{{IP: models.IP(apiVip)}},
						IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip)}},
						ClusterNetworks:   common.TestDualStackNetworking.ClusterNetworks,
						MachineNetworks:   common.TestDualStackNetworking.MachineNetworks,
						ServiceNetworks:   common.TestDualStackNetworking.ServiceNetworks,
						VipDhcpAllocation: swag.Bool(false),
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, "The IP address \"1.2.3.5\" appears both in apiVIPs and ingressVIPs")
			})
			It("API VIP not in Machine Network", func() {
				apiVip := "10.11.12.15"
				ingressVip := "1.2.3.6"

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						APIVips:           []*models.APIVip{{IP: models.IP(apiVip)}},
						IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip)}},
						ClusterNetworks:   common.TestDualStackNetworking.ClusterNetworks,
						MachineNetworks:   common.TestDualStackNetworking.MachineNetworks,
						ServiceNetworks:   common.TestDualStackNetworking.ServiceNetworks,
						VipDhcpAllocation: swag.Bool(false),
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, "api-vip <10.11.12.15> does not belong to machine-network-cidr <1.2.3.0/24>")
			})
			It("Ingress VIP not in Machine Network", func() {
				apiVip := "1.2.3.5"
				ingressVip := "10.11.12.16"

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						APIVips:           []*models.APIVip{{IP: models.IP(apiVip)}},
						IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip)}},
						ClusterNetworks:   common.TestDualStackNetworking.ClusterNetworks,
						MachineNetworks:   common.TestDualStackNetworking.MachineNetworks,
						ServiceNetworks:   common.TestDualStackNetworking.ServiceNetworks,
						VipDhcpAllocation: swag.Bool(false),
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, "ingress-vip <10.11.12.16> does not belong to machine-network-cidr <1.2.3.0/24>")
			})
			It("API VIP and Ingress VIP with empty Machine Networks", func() {
				apiVip := "10.11.12.15"
				ingressVip := "10.11.12.16"

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						APIVips:           []*models.APIVip{{IP: models.IP(apiVip)}},
						IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip)}},
						ClusterNetworks:   common.TestDualStackNetworking.ClusterNetworks,
						ServiceNetworks:   common.TestDualStackNetworking.ServiceNetworks,
						VipDhcpAllocation: swag.Bool(false),
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, "Dual-stack cluster cannot be created with empty Machine Networks")
			})

			It("API VIP from IPv6 Machine Network", func() {
				apiVip := "1001:db8::64"
				ingressVip := "1.2.3.6"

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						APIVips:           []*models.APIVip{{IP: models.IP(apiVip)}},
						IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip)}},
						ClusterNetworks:   common.TestDualStackNetworking.ClusterNetworks,
						MachineNetworks:   common.TestDualStackNetworking.MachineNetworks,
						ServiceNetworks:   common.TestDualStackNetworking.ServiceNetworks,
						VipDhcpAllocation: swag.Bool(false),
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, "api-vip <1001:db8::64> does not belong to machine-network-cidr <1.2.3.0/24>")
			})

			It("Ingress VIP from IPv6 Machine Network", func() {
				apiVip := "1.2.3.5"
				ingressVip := "1001:db8::65"

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						APIVips:           []*models.APIVip{{IP: models.IP(apiVip)}},
						IngressVips:       []*models.IngressVip{{IP: models.IP(ingressVip)}},
						ClusterNetworks:   common.TestDualStackNetworking.ClusterNetworks,
						MachineNetworks:   common.TestDualStackNetworking.MachineNetworks,
						ServiceNetworks:   common.TestDualStackNetworking.ServiceNetworks,
						VipDhcpAllocation: swag.Bool(false),
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, "ingress-vip <1001:db8::65> does not belong to machine-network-cidr <1.2.3.0/24>")
			})
		})

		Context("V2 Update cluster", func() {
			var clusterID strfmt.UUID
			var cluster *common.Cluster
			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				cluster = &common.Cluster{Cluster: models.Cluster{
					ID:                    &clusterID,
					Kind:                  swag.String(models.ClusterKindAddHostsCluster),
					Status:                swag.String(models.ClusterStatusInsufficient),
					Platform:              &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeBaremetal)},
					UserManagedNetworking: swag.Bool(false),
					CPUArchitecture:       models.ClusterCPUArchitectureX8664,
				}}
				err := db.Create(cluster).Error
				Expect(err).ShouldNot(HaveOccurred())
				bm = createInventory(db, cfg)
			})

			It("Dual-stack cluster - positive", func() {
				mockSetConnectivityMajorityGroupsForCluster(mockClusterApi)
				mockUsageReports()
				mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).Times(1)
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				mockDetectAndStoreCollidingIPsForCluster(mockClusterApi, 1)
				apiVip := "1.2.3.5"
				ingressVip := "1.2.3.6"

				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						APIVips:         []*models.APIVip{{IP: models.IP(apiVip)}},
						IngressVips:     []*models.IngressVip{{IP: models.IP(ingressVip)}},
						ClusterNetworks: common.TestDualStackNetworking.ClusterNetworks,
						MachineNetworks: common.TestDualStackNetworking.MachineNetworks,
						ServiceNetworks: common.TestDualStackNetworking.ServiceNetworks,
					},
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				cluster = &common.Cluster{Cluster: *reply.(*installer.V2UpdateClusterCreated).Payload}
				Expect(string(cluster.APIVips[0].IP)).To(Equal(apiVip))
				Expect(network.GetApiVipById(cluster, 0)).To(Equal(apiVip))
				Expect(string(cluster.IngressVips[0].IP)).To(Equal(ingressVip))
				Expect(network.GetIngressVipById(cluster, 0)).To(Equal(ingressVip))
				Expect(cluster.ClusterNetworks).To(Equal(common.TestDualStackNetworking.ClusterNetworks))
				Expect(cluster.MachineNetworks).To(Equal(common.TestDualStackNetworking.MachineNetworks))
				Expect(cluster.ServiceNetworks).To(Equal(common.TestDualStackNetworking.ServiceNetworks))
			})
		})
	})

	var _ = Describe("Disk encryption disabled", func() {

		const errorMsg = "Disk encryption support is not enabled. Cannot apply configurations to the cluster"

		BeforeEach(func() {
			Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
			db, dbName = common.PrepareTestDB()
			cfg.DiskEncryptionSupport = false
			bm = createInventory(db, cfg)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
			mockUsageReports()
		})

		AfterEach(func() {
			common.DeleteTestDB(db, dbName)
			ctrl.Finish()
		})

		Context("V2 Register cluster", func() {

			It("Disk Encryption configuration enable on none", func() {
				mockClusterRegisterSuccess(true)
				mockAMSSubscription(ctx)

				params := getDefaultClusterCreateParams()
				params.DiskEncryption = &models.DiskEncryption{
					EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
				}

				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: params,
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated)
				Expect(actual.Payload.DiskEncryption.EnableOn).To(Equal(swag.String(models.DiskEncryptionEnableOnNone)))
				Expect(actual.Payload.DiskEncryption.Mode).To(Equal(swag.String(models.DiskEncryptionModeTpmv2)))
			})

			It("Disk Encryption configuration enable on all", func() {
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
						},
						OpenshiftVersion:     swag.String("4.12.0"),
						HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})

			It("Disk Encryption configuration enable on masters", func() {
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
						},
						OpenshiftVersion:     swag.String("4.12.0"),
						HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull)},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})

			It("Disk Encryption configuration enable on workers", func() {
				reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn: swag.String(models.DiskEncryptionEnableOnWorkers),
						},
						OpenshiftVersion:     swag.String("4.12.0"),
						HighAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})
		})

		Context("V2 Update cluster", func() {

			var clusterID strfmt.UUID

			mockSuccess := func() {
				mockClusterUpdateSuccess(1, 0)
			}

			var cluster *common.Cluster

			BeforeEach(func() {
				clusterID = strfmt.UUID(uuid.New().String())
				cluster = &common.Cluster{Cluster: models.Cluster{
					ID:     &clusterID,
					Kind:   swag.String(models.ClusterKindAddHostsCluster),
					Status: swag.String(models.ClusterStatusInsufficient),
					Platform: &models.Platform{
						Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
					},
					CPUArchitecture: common.DefaultCPUArchitecture,
				}}
				err := db.Create(cluster).Error
				Expect(err).ShouldNot(HaveOccurred())
				bm = createInventory(db, cfg)
			})

			It("Update cluster disk encryption configuration enable on all", func() {
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn: swag.String(models.DiskEncryptionEnableOnAll),
							Mode:     swag.String(models.DiskEncryptionModeTpmv2),
						},
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})

			It("Update cluster disk encryption configuration enable on masters", func() {
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn: swag.String(models.DiskEncryptionEnableOnMasters),
							Mode:     swag.String(models.DiskEncryptionModeTpmv2),
						},
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})

			It("Update cluster disk encryption configuration enable on workers", func() {
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn: swag.String(models.DiskEncryptionEnableOnWorkers),
							Mode:     swag.String(models.DiskEncryptionModeTpmv2),
						},
					},
				})
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})

			It("Update cluster disk encryption configuration enable on none", func() {
				mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(1)
				mockSuccess()
				mockUsageReports()
				reply := bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
					ClusterID: clusterID,
					ClusterUpdateParams: &models.V2ClusterUpdateParams{
						DiskEncryption: &models.DiskEncryption{
							EnableOn: swag.String(models.DiskEncryptionEnableOnNone),
							Mode:     swag.String(models.DiskEncryptionModeTpmv2),
						},
					},
				})
				Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
				c := reply.(*installer.V2UpdateClusterCreated).Payload
				Expect(swag.StringValue(c.DiskEncryption.EnableOn)).To(Equal(models.DiskEncryptionEnableOnNone))
				Expect(swag.StringValue(c.DiskEncryption.Mode)).To(Equal(models.DiskEncryptionModeTpmv2))
			})
		})

	})

	Context("NTPSource", func() {
		It("NTPSource default value", func() {
			defaultNtpSource := "clock.redhat.com"
			bm.Config.DefaultNTPSource = defaultNtpSource

			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: getDefaultClusterCreateParams(),
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.AdditionalNtpSource).To(Equal(defaultNtpSource))
		})

		It("NTPSource non default value", func() {
			newNtpSource := "new.ntp.source"

			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)
			clusterParams := getDefaultClusterCreateParams()
			clusterParams.AdditionalNtpSource = &newNtpSource

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.AdditionalNtpSource).To(Equal(newNtpSource))
		})
	})

	It("cluster api failed to register", func() {
		bm.clusterApi = mockClusterApi
		mockClusterApi.EXPECT().RegisterCluster(ctx, gomock.Any()).Return(errors.Errorf("error")).Times(1)
		mockClusterRegisterSteps()

		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: getDefaultClusterCreateParams(),
		})
		Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusInternalServerError, errors.Errorf("error"))))
	})

	It("Host Networks default value", func() {
		defaultHostNetworks := make([]*models.HostNetwork, 0)
		mockClusterRegisterSuccess(true)
		mockAMSSubscription(ctx)

		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: getDefaultClusterCreateParams(),
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.HostNetworks).To(Equal(defaultHostNetworks))
	})

	It("cluster api failed to register with invalid pull secret", func() {
		mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).
			Return(errors.New("error")).Times(1)
		mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{}).Times(1)
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.ReleaseImage, nil).Times(1)
		mockOSImages.EXPECT().GetOsImage(gomock.Any(), gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)

		clusterParams := getDefaultClusterCreateParams()
		clusterParams.PullSecret = swag.String("")
		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: clusterParams,
		})
		Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("openshift version not supported", func() {
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.Errorf("OpenShift Version is not supported")).Times(1)

		clusterParams := getDefaultClusterCreateParams()
		clusterParams.OpenshiftVersion = swag.String("999")
		clusterParams.PullSecret = swag.String("")
		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: clusterParams,
		})
		Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
	})

	It("openshift release image and version successfully defined", func() {
		mockClusterRegisterSuccess(true)
		mockAMSSubscription(ctx)
		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: getDefaultClusterCreateParams(),
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.OpenshiftVersion).To(Equal(common.TestDefaultConfig.ReleaseVersion))
		Expect(actual.Payload.OcpReleaseImage).To(Equal(common.TestDefaultConfig.ReleaseImageUrl))
	})

	It("Register cluster with default CPU architecture", func() {
		mockClusterRegisterSuccess(true)
		mockAMSSubscription(ctx)

		params := getDefaultClusterCreateParams()
		params.CPUArchitecture = common.TestDefaultConfig.CPUArchitecture

		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: params,
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.CPUArchitecture).To(Equal(common.TestDefaultConfig.CPUArchitecture))
	})

	It("Register cluster with arm64 CPU architecture", func() {
		mockClusterRegisterSuccessWithVersion(common.ARM64CPUArchitecture, "4.12")
		mockAMSSubscription(ctx)
		mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return(
			[]string{common.TestDefaultConfig.OpenShiftVersion, common.ARM64CPUArchitecture}).Times(1)

		params := getDefaultClusterCreateParams()
		params.OpenshiftVersion = swag.String("4.11")
		params.CPUArchitecture = common.ARM64CPUArchitecture
		params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
		params.UserManagedNetworking = swag.Bool(true)
		params.VipDhcpAllocation = swag.Bool(false)

		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: params,
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.CPUArchitecture).To(Equal(common.ARM64CPUArchitecture))
	})

	It("Register cluster with arm64 CPU architecture as multiarch if multiarch release image used", func() {
		mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.ReleaseImage{
			CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
			CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
			OpenshiftVersion: swag.String("4.11.1"),
			URL:              swag.String("release_4.11.1"),
			Version:          swag.String("4.11.1-multi"),
		}, nil).Times(1)
		mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
		mockProviderRegistry.EXPECT().SetPlatformUsages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		mockMetric.EXPECT().ClusterRegistered().Times(1)
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.ClusterRegistrationSucceededEventName))).Times(1)
		mockAMSSubscription(ctx)
		mockOSImages.EXPECT().GetCPUArchitectures(gomock.Any()).Return(
			[]string{common.TestDefaultConfig.OpenShiftVersion, common.ARM64CPUArchitecture}).Times(1)

		params := getDefaultClusterCreateParams()
		params.CPUArchitecture = common.ARM64CPUArchitecture
		params.Platform = &models.Platform{Type: common.PlatformTypePtr(models.PlatformTypeNone)}
		params.UserManagedNetworking = swag.Bool(true)
		params.VipDhcpAllocation = swag.Bool(false)

		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: params,
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.CPUArchitecture).To(Equal(common.MultiCPUArchitecture))
	})

	It("Register cluster with multiarch CPU architecture", func() {
		mockClusterRegisterSuccessWithVersion(common.MultiCPUArchitecture, "4.11")
		mockAMSSubscription(ctx)

		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:                  swag.String("some-cluster-name"),
				OpenshiftVersion:      swag.String("4.11"),
				CPUArchitecture:       common.MultiCPUArchitecture,
				UserManagedNetworking: swag.Bool(true),
				VipDhcpAllocation:     swag.Bool(false),
				PullSecret:            swag.String(fakePullSecret),
			},
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.CPUArchitecture).To(Equal(common.MultiCPUArchitecture))
	})

	It("Register cluster without specified CPU architecture", func() {
		mockClusterRegisterSuccess(true)
		mockAMSSubscription(ctx)

		reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
			NewClusterParams: getDefaultClusterCreateParams(),
		})
		Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
		actual := reply.(*installer.V2RegisterClusterCreated)
		Expect(actual.Payload.CPUArchitecture).To(Equal(common.TestDefaultConfig.CPUArchitecture))
	})

	Context("Cluster Tags", func() {
		It("Register cluster with tags", func() {
			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			params := getDefaultClusterCreateParams()
			params.Tags = swag.String("tag1,tag2")
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.Tags).To(Equal("tag1,tag2"))
		})

		It("Register cluster with invalid tags", func() {
			params := getDefaultClusterCreateParams()
			params.Tags = swag.String("tag,,")
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			Expect(reply).Should(BeAssignableToTypeOf(common.NewApiError(http.StatusBadRequest, errors.Errorf("error"))))
			verifyApiErrorString(reply, http.StatusBadRequest, "Invalid format for Tags")
		})
	})

	Context("Networking", func() {
		var (
			clusterNetworks = common.TestIPv4Networking.ClusterNetworks
			serviceNetworks = common.TestIPv4Networking.ServiceNetworks
			machineNetworks = common.TestIPv4Networking.MachineNetworks
		)

		registerCluster := func() *models.Cluster {
			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			params := getDefaultClusterCreateParams()
			params.ClusterNetworks = clusterNetworks
			params.ServiceNetworks = serviceNetworks
			params.MachineNetworks = machineNetworks

			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: params,
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
			return reply.(*installer.V2RegisterClusterCreated).Payload
		}

		It("Networking defaults", func() {
			defaultClusterNetwork := "1.2.3.4/14"
			bm.Config.DefaultClusterNetworkCidr = defaultClusterNetwork
			defultServiceNetwork := "1.2.3.5/14"
			bm.Config.DefaultServiceNetworkCidr = defultServiceNetwork

			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: getDefaultClusterCreateParams(),
			})
			Expect(reflect.TypeOf(reply)).Should(Equal(reflect.TypeOf(installer.NewV2RegisterClusterCreated())))
			actual := reply.(*installer.V2RegisterClusterCreated)
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

	Context("Authz and soft timeouts", func() {
		var (
			authCtx      context.Context
			mockOcmAuthz *ocm.MockOCMAuthorization
			payload      *ocm.AuthPayload
			orgID1       = "300F3CE2-F122-4DA5-A845-2A4BC5956996"
			userName1    = "test_user_1"
		)

		BeforeEach(func() {
			db, dbName = common.PrepareTestDB()
			bm = createInventory(db, Config{})
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
			cfg := auth.GetConfigRHSSO()
			cfg.EnableOrgBasedFeatureGates = true
			mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)
			mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
			bm.authzHandler = auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
			payload = &ocm.AuthPayload{Role: ocm.UserRole}
			payload.Username = userName1
			payload.Organization = orgID1
			authCtx = context.WithValue(ctx, restapi.AuthKey, payload)
			mockClusterRegisterSteps()
			mockUsageReports()
			mockAMSSubscription(authCtx)
			mockMetric.EXPECT().ClusterRegistered().Times(1)
			mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterRegistrationSucceededEventName))).Times(1)
		})
		DescribeTable("organizational soft timeouts", func(softTimeouts, withError, expected bool) {
			var err error
			if withError {
				err = errors.New("fail")
			}
			mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), userName1, ocm.SoftTimeoutsCapabilityName, ocm.OrganizationCapabilityType).Return(softTimeouts, err).Times(1)

			reply := bm.V2RegisterCluster(authCtx, installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					Name:             swag.String("some-cluster-name"),
					OpenshiftVersion: swag.String("4.12.0-someFakeFlavour"),
					CPUArchitecture:  common.X86CPUArchitecture,
					PullSecret:       swag.String(fakePullSecret),
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated)
			Expect(actual.Payload.OrgSoftTimeoutsEnabled).To(Equal(expected))
		},
			Entry("soft timeouts prohibited - no error", false, false, false),
			Entry("soft timeouts allowed - no error", true, false, true),
			Entry("soft timeouts prohibited - with error", false, true, false),
			Entry("soft timeouts allowed - with error", true, true, false),
		)
	})

	Context("Authz and Multiarch", func() {
		var (
			authCtx      context.Context
			mockOcmAuthz *ocm.MockOCMAuthorization
			payload      *ocm.AuthPayload
			orgID1       = "300F3CE2-F122-4DA5-A845-2A4BC5956996"
			userName1    = "test_user_1"
		)

		BeforeEach(func() {
			db, dbName = common.PrepareTestDB()
			bm = createInventory(db, Config{})
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
		})

		Context("with EnableOrgBasedFeatureGates true", func() {

			BeforeEach(func() {
				cfg := auth.GetConfigRHSSO()
				cfg.EnableOrgBasedFeatureGates = true
				mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)
				mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
				bm.authHandler = auth.NewRHSSOAuthenticator(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
				bm.authzHandler = auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
				payload = &ocm.AuthPayload{Role: ocm.UserRole}
				payload.Username = userName1
				payload.Organization = orgID1
				authCtx = context.WithValue(ctx, restapi.AuthKey, payload)
			})

			It("Register a cluster with multi CPU architecture - success", func() {
				mockSecretValidator.EXPECT().ValidatePullSecret(gomock.Any(), gomock.Any()).Return(nil).Times(1)
				mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.ReleaseImage{
					CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
					OpenshiftVersion: swag.String("4.12"),
					URL:              swag.String("url-of-this-release"),
					Version:          swag.String("4.12.0"),
				}, nil).Times(1)
				mockOperatorManager.EXPECT().GetSupportedOperatorsByType(models.OperatorTypeBuiltin).Return([]*models.MonitoredOperator{&common.TestDefaultConfig.MonitoredOperator}).Times(1)
				mockProviderRegistry.EXPECT().SetPlatformUsages(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				mockMetric.EXPECT().ClusterRegistered().Times(1)
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
					eventstest.WithNameMatcher(eventgen.ClusterRegistrationSucceededEventName))).Times(1)
				mockAMSSubscription(authCtx)
				mockUsageReports()
				mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), userName1, ocm.MultiarchCapabilityName, ocm.OrganizationCapabilityType).Return(true, nil).Times(1)
				mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), userName1, ocm.SoftTimeoutsCapabilityName, ocm.OrganizationCapabilityType).Return(false, nil).Times(1)

				reply := bm.V2RegisterCluster(authCtx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						Name:             swag.String("some-cluster-name"),
						OpenshiftVersion: swag.String("4.12.0-someFakeFlavour"),
						CPUArchitecture:  common.MultiCPUArchitecture,
						PullSecret:       swag.String(fakePullSecret),
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated)
				Expect(actual.Payload.CPUArchitecture).To(Equal(common.MultiCPUArchitecture))
			})

			It("Register a cluster with multi CPU architecture - fail with no capability", func() {
				mockVersions.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.ReleaseImage{
					CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.PowerCPUArchitecture},
					OpenshiftVersion: swag.String("its-just-a-mock"),
					URL:              swag.String("url-of-this-release"),
					Version:          swag.String("doesnt-really-matter"),
				}, nil).Times(1)
				mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), userName1, ocm.MultiarchCapabilityName, ocm.OrganizationCapabilityType).Return(false, nil).Times(1)

				reply := bm.V2RegisterCluster(authCtx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						Name:             swag.String("some-cluster-name"),
						OpenshiftVersion: swag.String("4.12.0-someFakeFlavour"),
						CPUArchitecture:  common.MultiCPUArchitecture,
						PullSecret:       swag.String(fakePullSecret),
					},
				})
				verifyApiError(reply, http.StatusBadRequest)
			})
		})

		Context("with EnableOrgBasedFeatureGates false", func() {
			BeforeEach(func() {
				cfg := auth.GetConfigRHSSO()
				cfg.EnableOrgBasedFeatureGates = false
				mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)
				mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
				bm.authHandler = auth.NewRHSSOAuthenticator(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
				bm.authzHandler = auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
				payload = &ocm.AuthPayload{Role: ocm.UserRole}
				payload.Username = userName1
				payload.Organization = orgID1
				authCtx = context.WithValue(ctx, restapi.AuthKey, payload)
			})

			It("Register cluster with multi CPU architecture - success", func() {
				mockClusterRegisterSuccessWithVersion(common.MultiCPUArchitecture, "4.12")
				mockAMSSubscription(authCtx)
				mockUsageReports()

				reply := bm.V2RegisterCluster(authCtx, installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						Name:             swag.String("some-cluster-name"),
						OpenshiftVersion: swag.String("4.12.0-someFakeFlavour"),
						CPUArchitecture:  common.MultiCPUArchitecture,
						PullSecret:       swag.String(fakePullSecret),
					},
				})
				Expect(reply).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
				actual := reply.(*installer.V2RegisterClusterCreated)
				Expect(actual.Payload.CPUArchitecture).To(Equal(common.MultiCPUArchitecture))
			})
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
		bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false)
		mockUsageReports()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("With AMS subscriptions", func() {

		It("register cluster happy flow", func() {
			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)
			clusterParams := getDefaultClusterCreateParams()
			clusterParams.Name = swag.String(clusterName)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated)
			c, err := bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())
			Expect(c.AmsSubscriptionID).To(Equal(strfmt.UUID("")))
		})

		It("register cluster - deregister if we failed to create AMS subscription", func() {
			bm.clusterApi = mockClusterApi
			mockClusterApi.EXPECT().RegisterCluster(ctx, gomock.Any()).Return(nil)
			mockClusterRegisterSteps()
			mockAccountsMgmt.EXPECT().CreateSubscription(ctx, gomock.Any(), clusterName).Return(nil, errors.New("dummy"))
			mockClusterApi.EXPECT().DeregisterCluster(ctx, gomock.Any())

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.Name = swag.String(clusterName)
			err := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(err).To(HaveOccurred())
		})

		It("register cluster - delete AMS subscription if we failed to patch DB with ams_subscription_id", func() {
			bm.clusterApi = mockClusterApi
			mockClusterApi.EXPECT().RegisterCluster(ctx, gomock.Any()).Return(nil)
			mockClusterRegisterSteps()
			mockAMSSubscription(ctx)
			mockClusterApi.EXPECT().UpdateAmsSubscriptionID(ctx, gomock.Any(), strfmt.UUID("")).Return(common.NewApiError(http.StatusInternalServerError, errors.New("dummy")))
			mockClusterApi.EXPECT().DeregisterCluster(ctx, gomock.Any())
			mockAccountsMgmt.EXPECT().DeleteSubscription(ctx, strfmt.UUID("")).Return(nil)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.Name = swag.String("ams-cluster")
			err := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(err).To(HaveOccurred())
		})

		It("deregister cluster that don't have 'Reserved' subscriptions", func() {
			mockS3Client = s3wrapper.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, mockS3Client, nil, nil, nil, false)
			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.Name = swag.String("ams-cluster")
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			clusterID := *reply.(*installer.V2RegisterClusterCreated).Payload.ID

			mockAccountsMgmt.EXPECT().GetSubscription(ctx, gomock.Any()).Return(&amgmtv1.Subscription{}, nil)
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterDeregisteredEventName)))

			reply = bm.V2DeregisterCluster(ctx, installer.V2DeregisterClusterParams{ClusterID: clusterID})
			Expect(reply).Should(BeAssignableToTypeOf(&installer.V2DeregisterClusterNoContent{}))
		})

		It("update cluster name happy flow", func() {
			mockOperators := operators.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, mockOperators, nil, nil, nil, nil, nil, false)

			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.Name = swag.String(clusterName)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated)
			c, err := bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())

			newClusterName := "ams-cluster-new-name"
			mockOperators.EXPECT().ValidateCluster(ctx, gomock.Any())
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName),
				eventstest.WithClusterIdMatcher(c.ID.String())))
			mockAccountsMgmt.EXPECT().UpdateSubscriptionDisplayName(ctx, c.AmsSubscriptionID, newClusterName).Return(nil)

			reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
				ClusterID: *c.ID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					Name: &newClusterName,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
		})

		It("update cluster name with same name", func() {
			mockOperators := operators.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, mockOperators, nil, nil, nil, nil, nil, false)

			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.Name = swag.String(clusterName)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated)
			c, err := bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())

			mockOperators.EXPECT().ValidateCluster(ctx, gomock.Any())
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName)))

			reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
				ClusterID: *c.ID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					Name: &clusterName,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
		})

		It("update cluster without name field", func() {
			mockOperators := operators.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, mockOperators, nil, nil, nil, nil, nil, false)

			mockClusterRegisterSuccess(true)
			mockAMSSubscription(ctx)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.Name = swag.String(clusterName)
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			actual := reply.(*installer.V2RegisterClusterCreated)
			c, err := bm.getCluster(ctx, actual.Payload.ID.String())
			Expect(err).ToNot(HaveOccurred())

			mockOperators.EXPECT().ValidateCluster(ctx, gomock.Any())
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterStatusUpdatedEventName)))

			dummyDNSDomain := "dummy.test"
			reply = bm.V2UpdateCluster(ctx, installer.V2UpdateClusterParams{
				ClusterID: *c.ID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					BaseDNSDomain: &dummyDNSDomain,
				},
			})
			Expect(reply).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterCreated()))
		})

		tests := []struct {
			status string
		}{
			{status: "succeed"},
			{status: "failed"},
		}

		for i := range tests {
			test := tests[i]

			ignitionReader := io.NopCloser(strings.NewReader(`{
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

					reply := bm.V2InstallCluster(ctx, installer.V2InstallClusterParams{
						ClusterID: clusterID,
					})
					Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2InstallClusterAccepted()))
					waitForDoneChannel()
				})
			})
		}

		It("register and deregister cluster happy flow - nil OCM client", func() {
			mockS3Client = s3wrapper.NewMockAPI(ctrl)
			bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog().WithField("pkg", "cluster-monitor"),
				db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, nil, nil, mockS3Client, nil, nil, nil, false)
			bm.ocmClient = nil
			mockClusterRegisterSuccess(true)

			clusterParams := getDefaultClusterCreateParams()
			clusterParams.Name = swag.String("ams-cluster")
			reply := bm.V2RegisterCluster(ctx, installer.V2RegisterClusterParams{
				NewClusterParams: clusterParams,
			})
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			clusterID := *reply.(*installer.V2RegisterClusterCreated).Payload.ID

			// deregister
			mockEvents.EXPECT().SendClusterEvent(ctx, eventstest.NewEventMatcher(
				eventstest.WithNameMatcher(eventgen.ClusterDeregisteredEventName)))

			reply = bm.V2DeregisterCluster(ctx, installer.V2DeregisterClusterParams{ClusterID: clusterID})
			Expect(reply).Should(BeAssignableToTypeOf(&installer.V2DeregisterClusterNoContent{}))
		})
	})
})

var _ = Describe("update image version", func() {

	var (
		ctx     = context.Background()
		cfg     Config
		bm      *bareMetalInventory
		logHook *test.Hook
		params  *installer.V2RegisterHostParams
	)

	BeforeEach(func() {
		bm = createInventory(nil, cfg)
		var testLog *logrus.Logger
		testLog, logHook = test.NewNullLogger()
		bm.log = testLog
		hostID := strfmt.UUID(uuid.New().String())
		params = &installer.V2RegisterHostParams{
			InfraEnvID: strfmt.UUID(uuid.New().String()),
			NewHostParams: &models.HostCreateParams{
				HostID: &hostID,
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("same image full name", func() {
		agentImage := fmt.Sprintf("quay.io:5000/example/agent:%s", uuid.New().String())
		bm.AgentDockerImg = agentImage
		params.NewHostParams.DiscoveryAgentVersion = agentImage
		_, err := bm.generateV2NextStepRunnerCommand(ctx, params)
		Expect(err).NotTo(HaveOccurred())
		Expect(logHook.AllEntries()).To(BeEmpty())
	})

	It("same image tag", func() {
		tag := uuid.New().String()
		agentImage := fmt.Sprintf("quay.io:5000/example/agent:%s", tag)
		bm.AgentDockerImg = agentImage
		params.NewHostParams.DiscoveryAgentVersion = tag
		_, err := bm.generateV2NextStepRunnerCommand(ctx, params)
		Expect(err).NotTo(HaveOccurred())
		Expect(logHook.AllEntries()).To(BeEmpty())
	})

	It("image tag mismatch in full name", func() {
		imageName := "quay.io:5000/example/assisted-installer-agent"
		bm.AgentDockerImg = fmt.Sprintf("%s:%s", imageName, uuid.New().String())
		params.NewHostParams.DiscoveryAgentVersion = fmt.Sprintf("%s:%s", imageName, uuid.New().String())
		_, err := bm.generateV2NextStepRunnerCommand(ctx, params)
		Expect(err).NotTo(HaveOccurred())
		Expect(logHook.LastEntry().Message).To(ContainSubstring("uses an outdated agent image"))
	})

	It("image tag mismatch when tag only", func() {
		bm.AgentDockerImg = fmt.Sprintf("quay.io:5000/example/assisted-installer-agent:%s", uuid.New().String())
		params.NewHostParams.DiscoveryAgentVersion = uuid.New().String()
		_, err := bm.generateV2NextStepRunnerCommand(ctx, params)
		Expect(err).NotTo(HaveOccurred())
		Expect(logHook.LastEntry().Message).To(ContainSubstring("uses an outdated agent image"))
	})

	It("image name mismatch", func() {
		imageTag := uuid.New().String()
		bm.AgentDockerImg = fmt.Sprintf("quay.io:5000/example/assisted-installer-agent:%s", imageTag)
		params.NewHostParams.DiscoveryAgentVersion = fmt.Sprintf("quay.io:5000/example/agent:%s", imageTag)
		_, err := bm.generateV2NextStepRunnerCommand(ctx, params)
		Expect(err).NotTo(HaveOccurred())
		Expect(logHook.LastEntry().Message).To(ContainSubstring("uses an outdated agent image"))
	})

	It("image registry mismatch", func() {
		imageTag := uuid.New().String()
		imageName := "example/assisted-installer-agent"
		bm.AgentDockerImg = fmt.Sprintf("quay.io:5000/%s:%s", imageName, imageTag)
		params.NewHostParams.DiscoveryAgentVersion = fmt.Sprintf("docker.io:5000/%s:%s", imageName, imageTag)
		_, err := bm.generateV2NextStepRunnerCommand(ctx, params)
		Expect(err).NotTo(HaveOccurred())
		Expect(logHook.LastEntry().Message).To(ContainSubstring("uses an outdated agent image"))
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

var _ = Describe("V2GetHostIgnition and V2DownloadHostIgnition", func() {
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

		downloadParams := installer.V2DownloadHostIgnitionParams{
			InfraEnvID: otherInfraEnvID,
			HostID:     hostID,
		}
		resp = bm.V2DownloadHostIgnition(ctx, downloadParams)
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

		downloadParams := installer.V2DownloadHostIgnitionParams{
			InfraEnvID: infraEnvID,
			HostID:     otherHostID,
		}
		resp = bm.V2DownloadHostIgnition(ctx, downloadParams)
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

		downloadParams := installer.V2DownloadHostIgnitionParams{
			InfraEnvID: infraEnvID,
			HostID:     hostID,
		}
		resp = bm.V2DownloadHostIgnition(ctx, downloadParams)
		verifyApiError(resp, http.StatusConflict)
	})

	It("return server error when the download fails", func() {
		mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/master-%s.ign", clusterID, hostID)).Return(nil, int64(0), errors.Errorf("download failed")).Times(2)

		getParams := installer.V2GetHostIgnitionParams{
			InfraEnvID: infraEnvID,
			HostID:     hostID,
		}
		resp := bm.V2GetHostIgnition(ctx, getParams)
		verifyApiError(resp, http.StatusInternalServerError)

		downloadParams := installer.V2DownloadHostIgnitionParams{
			InfraEnvID: clusterID,
			HostID:     hostID,
		}
		resp = bm.V2DownloadHostIgnition(ctx, downloadParams)
		verifyApiError(resp, http.StatusInternalServerError)
	})

	It("return the correct content", func() {
		r := io.NopCloser(bytes.NewReader([]byte("test")))
		mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/master-%s.ign", clusterID, hostID)).Return(r, int64(4), nil).Times(2)

		getParams := installer.V2GetHostIgnitionParams{
			InfraEnvID: infraEnvID,
			HostID:     hostID,
		}
		resp := bm.V2GetHostIgnition(ctx, getParams)
		Expect(resp).To(BeAssignableToTypeOf(&installer.V2GetHostIgnitionOK{}))
		replyPayload := resp.(*installer.V2GetHostIgnitionOK).Payload
		Expect(replyPayload.Config).Should(Equal("test"))

		downloadParams := installer.V2DownloadHostIgnitionParams{
			InfraEnvID: infraEnvID,
			HostID:     hostID,
		}
		resp = bm.V2DownloadHostIgnition(ctx, downloadParams)
		Expect(resp).Should(Equal(filemiddleware.NewResponder(installer.NewV2DownloadHostIgnitionOK().WithPayload(r),
			fmt.Sprintf("master-%s.ign", hostID), 4, nil)))
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
		mockUsageReports()
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostDiscoveryIgnitionConfigAppliedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String())))
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

	It("returns bad request when provided too recent version", func() {
		// Wrong version
		override := `{"ignition": {"version": "9.9.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		response := bm.V2UpdateHostIgnition(ctx, params)
		verifyApiError(response, http.StatusBadRequest)
	})

	It("sets the feature usage when given a valid ignition config override to the host", func() {
		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}

		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostDiscoveryIgnitionConfigAppliedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String())))
		mockUsage.EXPECT().Add(gomock.Any(), usage.IgnitionConfigOverrideUsage, gomock.Any()).Times(1)
		mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
		mockUsage.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(0)

		bm.V2UpdateHostIgnition(ctx, params)
	})

	It("removes the feature usage when given an empty ignition config override to the same host", func() {
		override := ""
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostDiscoveryIgnitionConfigAppliedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String())))
		mockUsage.EXPECT().Add(gomock.Any(), usage.IgnitionConfigOverrideUsage, gomock.Any()).Times(0)
		mockUsage.EXPECT().Save(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
		mockUsage.EXPECT().Remove(gomock.Any(), gomock.Any()).Times(1)
		bm.V2UpdateHostIgnition(ctx, params)
	})
})

var _ = Describe("V2UpdateHostIgnition - with rhsso auth", func() {
	var (
		authCtx      context.Context
		bm           *bareMetalInventory
		db           *gorm.DB
		ctx          = context.Background()
		clusterID    strfmt.UUID
		hostID       strfmt.UUID
		infraEnvID   strfmt.UUID
		dbName       string
		userName1    = "test_user_1"
		userName2    = "test_user_2"
		mockOcmAuthz *ocm.MockOCMAuthorization
		payload      *ocm.AuthPayload
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		hostID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())

		cfg := auth.GetConfigRHSSO()
		bm = createInventory(db, Config{})
		mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)
		mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
		bm.authHandler = auth.NewRHSSOAuthenticator(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
		bm.authzHandler = auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
		payload = &ocm.AuthPayload{Role: ocm.UserRole}

		cluster := &common.Cluster{
			Cluster: models.Cluster{
				ID:       &clusterID,
				Kind:     swag.String(models.ClusterKindCluster),
				UserName: userName1,
			},
		}
		Expect(db.Create(cluster).Error).ToNot(HaveOccurred())

		host := models.Host{
			ID:         &hostID,
			InfraEnvID: infraEnvID,
			ClusterID:  &clusterID,
			Kind:       swag.String(models.HostKindHost),
			Status:     swag.String(models.HostStatusKnown),
			Role:       models.HostRoleMaster,
			Inventory:  "{}",
			UserName:   userName1,
		}
		Expect(db.Create(&host).Error).ToNot(HaveOccurred())

		infraEnv := &common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:        &infraEnvID,
				ClusterID: clusterID,
				UserName:  userName1,
			},
		}
		Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("successful update host ignition - cluster owner", func() {
		payload.Username = userName1
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		mockUsageReports()
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostDiscoveryIgnitionConfigAppliedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String())))

		response := bm.V2UpdateHostIgnition(authCtx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2UpdateHostIgnitionCreated{}))

		var updated models.Host
		err := db.First(&updated, "id = ?", hostID).Error
		Expect(err).ShouldNot(HaveOccurred())
		Expect(updated.IgnitionConfigOverrides).To(Equal(override))
	})

	It("successful update host ignition - clusterEditor", func() {
		payload.Username = userName2
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}
		mockUsageReports()
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostDiscoveryIgnitionConfigAppliedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String())))

		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil)

		response := bm.V2UpdateHostIgnition(authCtx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2UpdateHostIgnitionCreated{}))

		var updated models.Host
		err := db.First(&updated, "id = ?", hostID).Error
		Expect(err).ShouldNot(HaveOccurred())
		Expect(updated.IgnitionConfigOverrides).To(Equal(override))
	})

	It("no access to specified cluster (can't update)", func() {
		payload.Username = userName2
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}

		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)

		response := bm.V2UpdateHostIgnition(authCtx, params)
		verifyApiError(response, http.StatusForbidden)
	})

	It("no access to specified cluster (can't read)", func() {
		payload.Username = userName2
		payload.Organization = "another_org"
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}

		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)

		response := bm.V2UpdateHostIgnition(authCtx, params)
		verifyApiError(response, http.StatusNotFound)
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
		err := db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterID, Kind: swag.String(models.ClusterKindCluster)}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID}}).Error
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindSucceededEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)

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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().BindHost(ctx, gomock.Any(), clusterID, gomock.Any()).Return(err).Times(1)
		response := bm.BindHost(ctx, params)
		verifyApiError(response, http.StatusInternalServerError)
	})

	It("CPU architecture mismatch", func() {
		infraEnvID = strfmt.UUID(uuid.New().String())
		err := db.Model(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).
			UpdateColumn("cpu_architecture", common.DefaultCPUArchitecture).Error
		Expect(err).ShouldNot(HaveOccurred())

		err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:              &infraEnvID,
			CPUArchitecture: common.ARM64CPUArchitecture,
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
		mockHostApi.EXPECT().BindHost(ctx, gomock.Any(), clusterID, gomock.Any()).Times(0)
		response := bm.BindHost(ctx, params)
		verifyApiErrorString(response, http.StatusBadRequest, "doesn't match")
	})

	It("multiarch CPU architecture successful bind", func() {
		// create multiarch cluster
		err := db.Model(&common.Cluster{Cluster: models.Cluster{ID: &clusterID}}).
			UpdateColumn("cpu_architecture", common.MultiCPUArchitecture).Error
		Expect(err).ShouldNot(HaveOccurred())

		// create infraenv for arm64 and bind host
		infraEnvID = strfmt.UUID(uuid.New().String())
		err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:              &infraEnvID,
			CPUArchitecture: common.ARM64CPUArchitecture,
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindSucceededEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)

		mockHostApi.EXPECT().BindHost(ctx, gomock.Any(), clusterID, gomock.Any())
		response := bm.BindHost(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.BindHostOK{}))

		// create infraenv for x86_64 and bind host
		infraEnvID = strfmt.UUID(uuid.New().String())
		err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:              &infraEnvID,
			CPUArchitecture: common.X86CPUArchitecture,
		}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		hostID = strfmt.UUID(uuid.New().String())
		err = db.Create(&common.Host{Host: models.Host{ID: &hostID, InfraEnvID: infraEnvID}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		params = installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindSucceededEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)

		mockHostApi.EXPECT().BindHost(ctx, gomock.Any(), clusterID, gomock.Any())
		response = bm.BindHost(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.BindHostOK{}))
	})

	It("Deregister cluster - bind host", func() {
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindSucceededEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().BindHost(ctx, gomock.Any(), clusterID, gomock.Any())
		response := bm.BindHost(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.BindHostOK{}))

		var hostObj models.Host
		Expect(db.First(&hostObj, "id = ?", hostID).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&hostObj).Update("cluster_id", clusterID).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&hostObj).Update("status", "known").Error).ShouldNot(HaveOccurred())
		// Deregister Cluster
		deregisterParams := installer.V2DeregisterClusterParams{
			ClusterID: clusterID,
		}
		mockAccountsMgmt.EXPECT().GetSubscription(ctx, gomock.Any()).Return(&amgmtv1.Subscription{}, nil)
		mockClusterApi.EXPECT().DeregisterCluster(ctx, gomock.Any())
		mockHostApi.EXPECT().UnbindHost(ctx, gomock.Any(), gomock.Any(), false).Times(1)
		response = bm.V2DeregisterCluster(ctx, deregisterParams)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2DeregisterClusterNoContent{}))
	})

	It("Deregister cluster - mixed bind host", func() {
		infraEnv2ID := strfmt.UUID(uuid.New().String())
		err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnv2ID, ClusterID: clusterID}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		host2ID := strfmt.UUID(uuid.New().String())
		status := models.HostStatusKnown
		err = db.Create(&common.Host{Host: models.Host{ID: &host2ID, ClusterID: &clusterID, InfraEnvID: infraEnv2ID, Status: &status}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindSucceededEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		mockHostApi.EXPECT().BindHost(ctx, gomock.Any(), clusterID, gomock.Any())
		response := bm.BindHost(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.BindHostOK{}))

		var hostObj models.Host
		Expect(db.First(&hostObj, "id = ?", hostID).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&hostObj).Update("cluster_id", clusterID).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&hostObj).Update("status", "known").Error).ShouldNot(HaveOccurred())
		// Deregister Cluster
		deregisterParams := installer.V2DeregisterClusterParams{
			ClusterID: clusterID,
		}
		mockAccountsMgmt.EXPECT().GetSubscription(ctx, gomock.Any()).Return(&amgmtv1.Subscription{}, nil)
		mockClusterApi.EXPECT().DeregisterCluster(ctx, gomock.Any())
		mockHostApi.EXPECT().UnbindHost(ctx, gomock.Any(), gomock.Any(), false).Times(1)
		mockHostApi.EXPECT().UnRegisterHost(ctx, gomock.Any()).Return(nil).Times(1)
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostDeregisteredEventName),
			eventstest.WithHostIdMatcher(host2ID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo))).Times(1)
		response = bm.V2DeregisterCluster(ctx, deregisterParams)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2DeregisterClusterNoContent{}))
	})
})

var _ = Describe("BindHost - with rhsso auth", func() {
	var (
		authCtx      context.Context
		bm           *bareMetalInventory
		db           *gorm.DB
		ctx          = context.Background()
		clusterID    strfmt.UUID
		hostID       strfmt.UUID
		infraEnvID   strfmt.UUID
		dbName       string
		userName1    = "test_user_1"
		userName2    = "test_user_2"
		mockOcmAuthz *ocm.MockOCMAuthorization
		payload      *ocm.AuthPayload
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		hostID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())

		cfg := auth.GetConfigRHSSO()
		bm = createInventory(db, Config{})
		mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)
		mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
		bm.authHandler = auth.NewRHSSOAuthenticator(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
		bm.authzHandler = auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
		payload = &ocm.AuthPayload{Role: ocm.UserRole}

		err := db.Create(&common.Cluster{
			Cluster: models.Cluster{
				ID:       &clusterID,
				Kind:     swag.String(models.ClusterKindCluster),
				UserName: userName1}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		err = db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		err = db.Create(&common.Host{Host: models.Host{ID: &hostID, InfraEnvID: infraEnvID}}).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("successful bind - cluster owner", func() {
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindSucceededEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)

		payload.Username = userName1
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

		mockHostApi.EXPECT().BindHost(authCtx, gomock.Any(), clusterID, gomock.Any())
		response := bm.BindHost(authCtx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.BindHostOK{}))
	})

	It("successful bind - clusterEditor", func() {
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindSucceededEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		mockClusterApi.EXPECT().AcceptRegistration(gomock.Any()).Return(nil).Times(1)
		mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)

		payload.Username = userName2
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil)

		mockHostApi.EXPECT().BindHost(authCtx, gomock.Any(), clusterID, gomock.Any())
		response := bm.BindHost(authCtx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.BindHostOK{}))
	})

	It("no access to specified cluster (can't update)", func() {
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}

		payload.Username = userName2
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)

		response := bm.BindHost(authCtx, params)
		verifyApiError(response, http.StatusForbidden)
	})

	It("no access to specified cluster (can't read)", func() {
		params := installer.BindHostParams{
			HostID:         hostID,
			InfraEnvID:     infraEnvID,
			BindHostParams: &models.BindHostParams{ClusterID: &clusterID},
		}

		payload.Username = userName2
		payload.Organization = "another_org"
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostBindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)

		response := bm.BindHost(authCtx, params)
		verifyApiError(response, http.StatusNotFound)
	})
})

var _ = Describe("V2DeregisterHost", func() {
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
		err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID}}).Error
		Expect(err).ShouldNot(HaveOccurred())
		err = db.Create(&common.Host{Host: models.Host{ID: &hostID, InfraEnvID: infraEnvID, ClusterID: &clusterID}}).Error
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("refresh cluster after deregister fails silently", func() {
		params := installer.V2DeregisterHostParams{
			HostID:     hostID,
			InfraEnvID: infraEnvID,
		}
		mockHostApi.EXPECT().UnRegisterHost(gomock.Any(), gomock.Any()).Return(nil)
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostDeregisteredEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		mockClusterApi.EXPECT().RefreshStatus(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("Bad Refresh Status"))
		mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		response := bm.V2DeregisterHost(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2DeregisterHostNoContent{}))
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
		err := db.Create(&common.InfraEnv{InfraEnv: models.InfraEnv{ID: &infraEnvID}}).Error
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostUnbindSucceededEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityInfo)))
		mockHostApi.EXPECT().UnbindHost(ctx, gomock.Any(), gomock.Any(), false)
		mockClusterApi.EXPECT().RefreshSchedulableMastersForcedTrue(gomock.Any(), gomock.Any()).Return(nil).Times(1)
		response := bm.UnbindHost(ctx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.UnbindHostOK{}))
	})

	It("bad infraEnv", func() {
		params := installer.UnbindHostParams{
			HostID:     hostID,
			InfraEnvID: "12345",
		}
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostUnbindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
		response := bm.UnbindHost(ctx, params)
		verifyApiError(response, http.StatusNotFound)
	})

	It("already unbound", func() {
		params := installer.UnbindHostParams{
			HostID:     hostID,
			InfraEnvID: infraEnvID,
		}
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostUnbindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
		var hostObj models.Host
		Expect(db.First(&hostObj, "id = ?", hostID).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&hostObj).Update("cluster_id", nil).Error).ShouldNot(HaveOccurred())
		response := bm.UnbindHost(ctx, params)
		verifyApiError(response, http.StatusConflict)
	})

	It("unbound when infraenv is bound", func() {
		params := installer.UnbindHostParams{
			HostID:     hostID,
			InfraEnvID: infraEnvID,
		}
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostUnbindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
		var infraEnvObj models.InfraEnv
		Expect(db.First(&infraEnvObj, "id = ?", infraEnvID).Error).ShouldNot(HaveOccurred())
		Expect(db.Model(&infraEnvObj).Update("cluster_id", clusterID).Error).ShouldNot(HaveOccurred())
		response := bm.UnbindHost(ctx, params)
		verifyApiError(response, http.StatusConflict)
	})

	It("transition failed", func() {
		params := installer.UnbindHostParams{
			HostID:     hostID,
			InfraEnvID: infraEnvID,
		}
		err := errors.Errorf("Transition failed")
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostUnbindFailedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityError)))
		mockHostApi.EXPECT().UnbindHost(ctx, gomock.Any(), gomock.Any(), false).Return(err).Times(1)
		response := bm.UnbindHost(ctx, params)
		verifyApiError(response, http.StatusInternalServerError)
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostInstallerArgsAppliedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String())))
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

var _ = Describe("V2UpdateHostInstallerArgs - with rhsso auth", func() {
	var (
		authCtx      context.Context
		bm           *bareMetalInventory
		db           *gorm.DB
		ctx          = context.Background()
		clusterID    strfmt.UUID
		hostID       strfmt.UUID
		infraEnvID   strfmt.UUID
		dbName       string
		userName1    = "test_user_1"
		userName2    = "test_user_2"
		mockOcmAuthz *ocm.MockOCMAuthorization
		payload      *ocm.AuthPayload
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		clusterID = strfmt.UUID(uuid.New().String())
		infraEnvID = strfmt.UUID(uuid.New().String())

		cfg := auth.GetConfigRHSSO()
		bm = createInventory(db, Config{})
		mockOcmAuthz = ocm.NewMockOCMAuthorization(ctrl)
		mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
		bm.authHandler = auth.NewRHSSOAuthenticator(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
		bm.authzHandler = auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
		payload = &ocm.AuthPayload{Role: ocm.UserRole}

		clusterKind := models.ClusterKindCluster
		err := db.Create(&common.Cluster{Cluster: models.Cluster{ID: &clusterID, UserName: userName1, Kind: &clusterKind}}).Error
		Expect(err).ShouldNot(HaveOccurred())

		// add a host
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusKnown, models.HostKindHost, infraEnvID, clusterID, "{}", db)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("successful update host installer args - cluster owner", func() {
		payload.Username = userName1
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n"}
		params := installer.V2UpdateHostInstallerArgsParams{
			InfraEnvID:          infraEnvID,
			HostID:              hostID,
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostInstallerArgsAppliedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String())))
		response := bm.V2UpdateHostInstallerArgs(authCtx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2UpdateHostInstallerArgsCreated{}))
	})

	It("successful update host installer args - clusterEditor", func() {
		payload.Username = userName2
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n"}
		params := installer.V2UpdateHostInstallerArgsParams{
			InfraEnvID:          infraEnvID,
			HostID:              hostID,
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}
		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil)
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostInstallerArgsAppliedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String())))
		response := bm.V2UpdateHostInstallerArgs(authCtx, params)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2UpdateHostInstallerArgsCreated{}))
	})

	It("no access to specified cluster (can't update)", func() {
		payload.Username = userName2
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n"}
		params := installer.V2UpdateHostInstallerArgsParams{
			InfraEnvID:          infraEnvID,
			HostID:              hostID,
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}

		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)

		response := bm.V2UpdateHostInstallerArgs(authCtx, params)
		verifyApiError(response, http.StatusForbidden)
	})

	It("no access to specified cluster (can't read)", func() {
		payload.Username = userName2
		payload.Organization = "another_org"
		authCtx = context.WithValue(ctx, restapi.AuthKey, payload)

		args := []string{"--append-karg", "nameserver=8.8.8.8", "-n"}
		params := installer.V2UpdateHostInstallerArgsParams{
			InfraEnvID:          infraEnvID,
			HostID:              hostID,
			InstallerArgsParams: &models.InstallerArgsParams{Args: args},
		}

		mockOcmAuthz.EXPECT().AccessReview(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)

		response := bm.V2UpdateHostInstallerArgs(authCtx, params)
		verifyApiError(response, http.StatusNotFound)
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
		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostApprovedUpdatedEventName),
			eventstest.WithHostIdMatcher(hostID.String()),
			eventstest.WithInfraEnvIdMatcher(infraEnvID.String()))).Times(1)
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
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})
	It("Duplicates and disabled IPv6 - single-stack v4 host", func() {
		// add a single-stack v4 host
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusInsufficient, "kind", clusterID, clusterID,
			getInventoryStrWithIPv6("host", "bios", []string{
				"1.1.1.1/24",
				"1.1.1.2/24",
			}, []string{}), db)

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
	It("Duplicates and disabled IPv6 - single-stack v6 host", func() {
		// add a single-stack v6 host
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusInsufficient, "kind", clusterID, clusterID,
			getInventoryStrWithIPv6("host", "bios", []string{}, []string{
				"fe80::1/64",
				"fe80::2/64",
			}), db)

		cfg.IPv6Support = false
		inventory = createInventory(db, *cfg)
		cluster, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ToNot(HaveOccurred())
		networks := inventory.calculateHostNetworks(logrus.New(), cluster)
		Expect(len(networks)).To(Equal(0))
	})
	It("Duplicates and disabled IPv6 - dual-stack host", func() {
		// add a dual-stack host
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusInsufficient, "kind", clusterID, clusterID,
			getInventoryStrWithIPv6("host", "bios", []string{
				"1.1.1.1/24",
				"1.1.1.2/24",
			}, []string{
				"fe80::1/64",
				"fe80::2/64",
			}), db)

		cfg.IPv6Support = false
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
	It("Duplicates and enabled IPv6 - single-stack v6 host", func() {
		// add a dual-stack host
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusInsufficient, "kind", clusterID, clusterID,
			getInventoryStrWithIPv6("host", "bios", []string{}, []string{
				"fe80::1/64",
				"fe80::2/64",
			}), db)

		cfg.IPv6Support = true
		inventory = createInventory(db, *cfg)
		cluster, err := common.GetClusterFromDB(db, clusterID, common.UseEagerLoading)
		Expect(err).ToNot(HaveOccurred())
		networks := inventory.calculateHostNetworks(logrus.New(), cluster)
		Expect(len(networks)).To(Equal(1))
		Expect(networks[0].Cidr).To(Equal("fe80::/64"))
		Expect(len(networks[0].HostIds)).To(Equal(1))
		Expect(networks[0].HostIds[0]).To(Equal(hostID))

	})
	It("Duplicates and enabled IPv6 - dual-stack host", func() {
		// add a dual-stack host
		hostID = strfmt.UUID(uuid.New().String())
		addHost(hostID, models.HostRoleMaster, models.HostStatusInsufficient, "kind", clusterID, clusterID,
			getInventoryStrWithIPv6("host", "bios", []string{
				"1.1.1.1/24",
				"1.1.1.2/24",
			}, []string{
				"fe80::1/64",
				"fe80::2/64",
			}), db)

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

	mockStream = commontesting.GetDummyNotificationStream(ctrl)
	mockClusterApi = cluster.NewMockAPI(ctrl)
	mockHostApi = host.NewMockAPI(ctrl)
	mockInfraEnvApi = infraenv.NewMockAPI(ctrl)
	mockGenerator = generator.NewMockISOInstallConfigGenerator(ctrl)
	mockEvents = eventsapi.NewMockHandler(ctrl)
	mockS3Client = s3wrapper.NewMockAPI(ctrl)
	mockMetric = metrics.NewMockAPI(ctrl)
	mockUsage = usage.NewMockAPI(ctrl)
	mockK8sClient = k8sclient.NewMockK8SClient(ctrl)
	mockAccountsMgmt = ocm.NewMockOCMAccountsMgmt(ctrl)
	ocmClient := &ocm.Client{AccountsMgmt: mockAccountsMgmt}
	mockSecretValidator = validations.NewMockPullSecretValidator(ctrl)
	mockVersions = versions.NewMockHandler(ctrl)
	mockOSImages = versions.NewMockOSImages(ctrl)
	mockCRDUtils = NewMockCRDUtils(ctrl)
	mockOperatorManager = operators.NewMockAPI(ctrl)
	mockIgnitionBuilder = ignition.NewMockIgnitionBuilder(ctrl)
	mockProviderRegistry = registry.NewMockProviderRegistry(ctrl)
	mockInstallConfigBuilder = installcfg_builder.NewMockInstallConfigBuilder(ctrl)
	mockHwValidator = hardware.NewMockValidator(ctrl)
	mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
	dnsApi := dns.NewDNSHandler(cfg.BaseDNSDomains, common.GetTestLog())
	gcConfig := garbagecollector.Config{DeregisterInactiveAfter: 20 * 24 * time.Hour}

	bm := NewBareMetalInventory(db, mockStream, common.GetTestLog(), mockHostApi, mockClusterApi, mockInfraEnvApi, cfg,
		mockGenerator, mockEvents, mockS3Client, mockMetric, mockUsage, mockOperatorManager,
		getTestAuthHandler(), getTestAuthzHandler(), mockK8sClient, ocmClient, nil, mockSecretValidator, mockVersions,
		mockOSImages, mockCRDUtils, mockIgnitionBuilder, mockHwValidator, dnsApi, mockInstallConfigBuilder,
		mockStaticNetworkConfig, gcConfig, mockProviderRegistry, true)

	bm.ImageServiceBaseURL = imageServiceBaseURL
	bm.ServiceBaseURL = serviceBaseURL
	return bm
}

var _ = Describe("IPv6 support disabled", func() {

	const errorMsg = "IPv6 is not supported in this setup"

	var (
		bm     *bareMetalInventory
		cfg    Config
		db     *gorm.DB
		dbName string
		ctx    = context.Background()
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		Expect(cfg.IPv6Support).Should(BeTrue())
		cfg.IPv6Support = false
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("Register cluster", func() {

		var params installer.V2RegisterClusterParams

		BeforeEach(func() {
			params = installer.V2RegisterClusterParams{
				NewClusterParams: getDefaultClusterCreateParams(),
			}
		})

		Context("IPV6 cluster", func() {
			It("IPv6 cluster network rejected", func() {
				params.NewClusterParams.ClusterNetworks = []*models.ClusterNetwork{
					{Cidr: "2001:db8::/64"},
				}
				params.NewClusterParams.MachineNetworks = []*models.MachineNetwork{
					{Cidr: "2001:db9::/64"},
				}
				reply := bm.V2RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})

			It("IPv6 service network rejected", func() {
				params.NewClusterParams.ServiceNetworks = []*models.ServiceNetwork{
					{Cidr: "2001:db8::/64"},
				}
				params.NewClusterParams.MachineNetworks = []*models.MachineNetwork{
					{Cidr: "2001:db9::/64"},
				}
				reply := bm.V2RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})

			It("IPv6 ingress VIP rejected", func() {
				params.NewClusterParams.APIVips = []*models.APIVip{{IP: "2001:db8::2"}}
				params.NewClusterParams.IngressVips = []*models.IngressVip{{IP: "2001:db8::1"}}
				reply := bm.V2RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
			})
		})
	})

	Context("Update cluster", func() {

		var params installer.V2UpdateClusterParams

		BeforeEach(func() {
			mockUsageReports()

			db, dbName = common.PrepareTestDB()
			cfg.IPv6Support = false

			bm = createInventory(db, cfg)
			clusterID := strfmt.UUID(uuid.New().String())
			err := db.Create(&common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
				Status:           swag.String(models.ClusterStatusReady),
				CPUArchitecture:  models.ClusterCPUArchitectureX8664,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			params = installer.V2UpdateClusterParams{ClusterID: clusterID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{},
			}
		})

		AfterEach(func() {
			common.DeleteTestDB(db, dbName)
			ctrl.Finish()
		})

		It("IPv6 cluster network rejected", func() {
			params.ClusterUpdateParams.ClusterNetworks = []*models.ClusterNetwork{
				{Cidr: "2001:db8::/64"},
			}
			reply := bm.V2UpdateCluster(ctx, params)
			verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
		})

		It("IPv6 service network rejected", func() {
			params.ClusterUpdateParams.ServiceNetworks = []*models.ServiceNetwork{
				{Cidr: "2001:db8::/64"},
			}
			reply := bm.V2UpdateCluster(ctx, params)
			verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
		})

		It("IPv6 machine network rejected", func() {
			params.ClusterUpdateParams.MachineNetworks = []*models.MachineNetwork{
				{Cidr: "2001:db8::/64"},
			}
			reply := bm.V2UpdateCluster(ctx, params)
			verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
		})

		It("IPv6 API VIP rejected", func() {
			params.ClusterUpdateParams.APIVips = []*models.APIVip{{IP: "2003:db8::a"}}
			params.ClusterUpdateParams.IngressVips = []*models.IngressVip{{IP: "2003:db8::b"}}

			reply := bm.V2UpdateCluster(ctx, params)
			verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
		})

		It("IPv6 ingress VIP rejected", func() {
			params.ClusterUpdateParams.APIVips = []*models.APIVip{{IP: "2002:db8::1"}}
			params.ClusterUpdateParams.IngressVips = []*models.IngressVip{{IP: "2002:db8::2"}}

			reply := bm.V2UpdateCluster(ctx, params)
			verifyApiErrorString(reply, http.StatusBadRequest, errorMsg)
		})
	})
})

var _ = Describe("Dual-stack cluster", func() {

	var (
		bm                                *bareMetalInventory
		cfg                               Config
		db                                *gorm.DB
		dbName                            string
		ctx                               = context.Background()
		TestDualStackNetworkingWrongOrder = common.TestNetworking{
			ClusterNetworks: append(common.TestIPv4Networking.ClusterNetworks, common.TestIPv6Networking.ClusterNetworks...),
			ServiceNetworks: append(common.TestIPv4Networking.ServiceNetworks, common.TestIPv6Networking.ServiceNetworks...),
			MachineNetworks: append(common.TestIPv4Networking.MachineNetworks, common.TestIPv6Networking.MachineNetworks...),
			APIVips:         common.TestIPv4Networking.APIVips,
			IngressVips:     common.TestIPv4Networking.IngressVips,
		}
	)

	clusterNetworksWrongOrder := TestDualStackNetworkingWrongOrder.ClusterNetworks
	clusterNetworksWrongOrder[0], clusterNetworksWrongOrder[1] = clusterNetworksWrongOrder[1], clusterNetworksWrongOrder[0]

	serviceNetworksWrongOrder := TestDualStackNetworkingWrongOrder.ServiceNetworks
	serviceNetworksWrongOrder[0], serviceNetworksWrongOrder[1] = serviceNetworksWrongOrder[1], serviceNetworksWrongOrder[0]

	machineNetworksWrongOrder := TestDualStackNetworkingWrongOrder.MachineNetworks
	machineNetworksWrongOrder[0], machineNetworksWrongOrder[1] = machineNetworksWrongOrder[1], machineNetworksWrongOrder[0]

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		Expect(cfg.IPv6Support).Should(BeTrue())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("Register cluster", func() {
		var params installer.V2RegisterClusterParams

		BeforeEach(func() {
			params = installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{},
			}
		})

		Context("Cluster with wrong network order", func() {
			It("v6-first in cluster networks rejected", func() {
				errStr := "First cluster network has to be IPv4 subnet"
				params.NewClusterParams.ClusterNetworks = TestDualStackNetworkingWrongOrder.ClusterNetworks
				reply := bm.V2RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errStr)
			})
			It("v6-first in service networks rejected", func() {
				errStr := "First service network has to be IPv4 subnet"
				params.NewClusterParams.ServiceNetworks = TestDualStackNetworkingWrongOrder.ServiceNetworks
				reply := bm.V2RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errStr)
			})
			It("v6-first in machine networks rejected", func() {
				errStr := "First machine network has to be IPv4 subnet"
				params.NewClusterParams.MachineNetworks = TestDualStackNetworkingWrongOrder.MachineNetworks
				reply := bm.V2RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errStr)
			})
		})

		Context("Cluster with single network when two required", func() {
			It("Single service network", func() {
				errStr := "Expected 2 service networks, found 1"
				params.NewClusterParams.ClusterNetworks = common.TestDualStackNetworking.ClusterNetworks
				params.NewClusterParams.ServiceNetworks = common.TestIPv4Networking.ServiceNetworks
				reply := bm.V2RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errStr)
			})
			It("Single cluster network", func() {
				errStr := "Expected 2 cluster networks, found 1"
				params.NewClusterParams.ClusterNetworks = common.TestIPv4Networking.ClusterNetworks
				params.NewClusterParams.ServiceNetworks = common.TestDualStackNetworking.ServiceNetworks
				reply := bm.V2RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errStr)
			})
			It("Single machine network", func() {
				errStr := "Expected 2 machine networks, found 1"
				params.NewClusterParams.ServiceNetworks = common.TestDualStackNetworking.ServiceNetworks
				params.NewClusterParams.MachineNetworks = common.TestIPv4Networking.MachineNetworks
				reply := bm.V2RegisterCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, errStr)
			})
		})
	})

	Context("Update cluster", func() {
		var (
			params    installer.V2UpdateClusterParams
			clusterID strfmt.UUID
		)

		BeforeEach(func() {
			mockUsageReports()
			clusterID = strfmt.UUID(uuid.New().String())
			err := db.Create(&common.Cluster{Cluster: models.Cluster{
				ID:               &clusterID,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
				Status:           swag.String(models.ClusterStatusReady),
				CPUArchitecture:  models.ClusterCPUArchitectureX8664,
			}}).Error
			Expect(err).ShouldNot(HaveOccurred())

			params = installer.V2UpdateClusterParams{ClusterID: clusterID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{},
			}
		})

		Context("Cluster with wrong network order", func() {
			It("v6-first in cluster networks rejected", func() {
				params.ClusterUpdateParams.ClusterNetworks = TestDualStackNetworkingWrongOrder.ClusterNetworks
				reply := bm.V2UpdateCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, "First cluster network has to be IPv4 subnet")
			})
			It("v6-first in service networks rejected", func() {
				params.ClusterUpdateParams.ServiceNetworks = TestDualStackNetworkingWrongOrder.ServiceNetworks
				reply := bm.V2UpdateCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, "First service network has to be IPv4 subnet")
			})
			It("v6-first in machine networks rejected", func() {
				params.ClusterUpdateParams.MachineNetworks = TestDualStackNetworkingWrongOrder.MachineNetworks
				reply := bm.V2UpdateCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, "First machine network has to be IPv4 subnet")
			})
		})

		Context("Cluster with single network when two required", func() {
			It("Single service network", func() {
				params.ClusterUpdateParams.ClusterNetworks = common.TestDualStackNetworking.ClusterNetworks
				params.ClusterUpdateParams.ServiceNetworks = common.TestIPv4Networking.ServiceNetworks
				reply := bm.V2UpdateCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, "Expected 2 service networks, found 1")
			})
			It("Single cluster network", func() {
				params.ClusterUpdateParams.ClusterNetworks = common.TestIPv4Networking.ClusterNetworks
				params.ClusterUpdateParams.ServiceNetworks = common.TestDualStackNetworking.ServiceNetworks
				reply := bm.V2UpdateCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, "Expected 2 cluster networks, found 1")
			})
			It("Single machine network", func() {
				params.ClusterUpdateParams.ServiceNetworks = common.TestDualStackNetworking.ServiceNetworks
				params.ClusterUpdateParams.MachineNetworks = common.TestIPv4Networking.MachineNetworks
				reply := bm.V2UpdateCluster(ctx, params)
				verifyApiErrorString(reply, http.StatusBadRequest, "Expected 2 machine networks, found 1")
			})
		})
		It("update dual machine networks", func() {
			By("setup dual stack networking")
			cluster := &common.Cluster{
				Cluster: models.Cluster{
					ID: &clusterID,
				},
			}
			mockClusterApi.EXPECT().VerifyClusterUpdatability(createClusterIdMatcher(cluster)).Return(nil).Times(2)
			params.ClusterUpdateParams.MachineNetworks = common.TestDualStackNetworking.MachineNetworks
			params.ClusterUpdateParams.ServiceNetworks = common.TestDualStackNetworking.ServiceNetworks
			params.ClusterUpdateParams.ClusterNetworks = common.TestDualStackNetworking.ClusterNetworks
			cls, err := bm.UpdateClusterNonInteractive(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(cls.MachineNetworks).To(HaveLen(2))
			for _, m := range common.TestDualStackNetworking.MachineNetworks {
				Expect(cls.MachineNetworks).To(ContainElement(&models.MachineNetwork{ClusterID: clusterID, Cidr: m.Cidr}))
			}
			By("update machine networks")
			params.ClusterUpdateParams.MachineNetworks = append([]*models.MachineNetwork{}, common.TestDualStackNetworking.MachineNetworks...)
			params.ClusterUpdateParams.MachineNetworks[1] = &models.MachineNetwork{ClusterID: clusterID, Cidr: "3001:db8::/120"}
			cls, err = bm.UpdateClusterNonInteractive(ctx, params)
			Expect(err).ToNot(HaveOccurred())
			Expect(cls.MachineNetworks).To(HaveLen(2))
			Expect(cls.MachineNetworks).To(ContainElement(&models.MachineNetwork{ClusterID: clusterID, Cidr: "3001:db8::/120"}))
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
		c      *common.Cluster
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("Console operator available", func() {
		c = createClusterWithMonitoredOperator(db, operators.OperatorConsole)
		mockClusterApi.EXPECT().IsOperatorAvailable(gomock.Any(), operators.OperatorConsole.Name).Return(true)
		objectName := fmt.Sprintf("%s/%s", *c.ID, "kubeadmin-password")
		mockS3Client.EXPECT().Download(ctx, objectName).Return(io.NopCloser(strings.NewReader("my_password")), int64(0), nil)

		reply := bm.V2GetCredentials(ctx, installer.V2GetCredentialsParams{ClusterID: *c.ID})
		Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2GetCredentialsOK()))
	})

	It("Console operator not available", func() {
		c = createClusterWithMonitoredOperator(db, operators.OperatorConsole)
		mockClusterApi.EXPECT().IsOperatorAvailable(gomock.Any(), operators.OperatorConsole.Name).Return(false)

		reply := bm.V2GetCredentials(ctx, installer.V2GetCredentialsParams{ClusterID: *c.ID})
		verifyApiError(reply, http.StatusConflict)
	})

	It("Returns credentials and no console URL if the console capability is disabled", func() {
		c = createCluster(db, models.ClusterStatusInstalled)
		objectName := fmt.Sprintf("%s/%s", *c.ID, "kubeadmin-password")
		mockS3Client.EXPECT().Download(ctx, objectName).Return(io.NopCloser(strings.NewReader("my_password")), int64(0), nil)

		reply := bm.V2GetCredentials(ctx, installer.V2GetCredentialsParams{ClusterID: *c.ID})
		recorder := httptest.NewRecorder()
		reply.WriteResponse(recorder, runtime.JSONProducer())
		Expect(recorder.Code).To(Equal(http.StatusOK))
		Expect(recorder.Body).To(MatchJSON(`{
			"password": "my_password",
			"username": "kubeadmin"
		}`))
	})

	It("Returns credentials and console URL if the console capability is enabled", func() {
		c = createClusterWithMonitoredOperator(db, operators.OperatorConsole)
		mockClusterApi.EXPECT().IsOperatorAvailable(gomock.Any(), operators.OperatorConsole.Name).Return(true)
		objectName := fmt.Sprintf("%s/%s", *c.ID, "kubeadmin-password")
		mockS3Client.EXPECT().Download(ctx, objectName).Return(io.NopCloser(strings.NewReader("my_password")), int64(0), nil)

		reply := bm.V2GetCredentials(ctx, installer.V2GetCredentialsParams{ClusterID: *c.ID})
		recorder := httptest.NewRecorder()
		reply.WriteResponse(recorder, runtime.JSONProducer())
		Expect(recorder.Code).To(Equal(http.StatusOK))
		Expect(recorder.Body).To(MatchJSON(`{
			"password": "my_password",
			"username": "kubeadmin",
			"console_url": "https://console-openshift-console.apps.my-cluster.my-domain"
		}`))
	})
})

var _ = Describe("Platform tests", func() {

	var (
		cfg                Config
		ctx                = context.Background()
		bm                 *bareMetalInventory
		db                 *gorm.DB
		dbName             string
		registerParams     *installer.V2RegisterClusterParams
		getVSpherePlatform = func() *models.Platform {
			return &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeVsphere),
			}
		}
	)

	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
		mockOperators := operators.NewMockAPI(ctrl)
		bm.clusterApi = cluster.NewManager(cluster.Config{}, common.GetTestLog(), db, commontesting.GetDummyNotificationStream(ctrl), mockEvents, nil, nil, nil, nil, nil, mockOperators, nil, nil, nil, nil, nil, false)
		bm.ocmClient = nil
		clusterParams := getDefaultClusterCreateParams()
		clusterParams.Name = swag.String("cluster")
		registerParams = &installer.V2RegisterClusterParams{
			NewClusterParams: clusterParams,
		}
		mockOperators.EXPECT().ValidateCluster(ctx, gomock.Any()).AnyTimes()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("Register cluster", func() {

		BeforeEach(func() {
			mockUsageReports()
		})

		It("default platform", func() {
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, common.OpenShiftVersion)
			reply := bm.V2RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeBaremetal))
		})

		It("vsphere platform", func() {
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, common.OpenShiftVersion)
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeVsphere),
			}
			reply := bm.V2RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeVsphere))
		})

		It("vsphere platform with credentials", func() {
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, common.OpenShiftVersion)
			registerParams.NewClusterParams.Platform = getVSpherePlatform()
			reply := bm.V2RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeVsphere))
		})

		It("nutanix platform", func() {
			MinimalOpenShiftVersionForNutanix := "4.11.0"
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, MinimalOpenShiftVersionForNutanix)
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeNutanix),
			}

			registerParams.NewClusterParams.OpenshiftVersion = swag.String(MinimalOpenShiftVersionForNutanix)
			registerParams.NewClusterParams.CPUArchitecture = common.X86CPUArchitecture

			reply := bm.V2RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeNutanix))
		})

		It("external platform - HA", func() {
			MinimalOpenShiftVersionForExternal := "4.14.0"
			platformName := "platform-name"
			cloudControllerManager := models.PlatformExternalCloudControllerManagerEmpty
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, MinimalOpenShiftVersionForExternal)
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           &platformName,
					CloudControllerManager: &cloudControllerManager,
				},
			}

			registerParams.NewClusterParams.OpenshiftVersion = swag.String(MinimalOpenShiftVersionForExternal)
			reply := bm.V2RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeExternal))
			Expect(*cluster.Platform.External.PlatformName).Should(Equal(platformName))
			Expect(*cluster.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
			Expect(swag.BoolValue(cluster.UserManagedNetworking)).Should(BeTrue())
		})

		It("external platform - default cloud controller manager - HA", func() {
			MinimalOpenShiftVersionForExternal := "4.14.0"
			platformName := "platform-name"
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, MinimalOpenShiftVersionForExternal)
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName: &platformName,
				},
			}

			registerParams.NewClusterParams.OpenshiftVersion = swag.String(MinimalOpenShiftVersionForExternal)
			reply := bm.V2RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeExternal))
			Expect(*cluster.Platform.External.PlatformName).Should(Equal(platformName))
			Expect(*cluster.Platform.External.CloudControllerManager).Should(Equal(models.PlatformExternalCloudControllerManagerEmpty))
			Expect(swag.BoolValue(cluster.UserManagedNetworking)).Should(BeTrue())
		})

		It("external platform - platformName=oci cloudControllerManager=External - HA", func() {
			MinimalOpenShiftVersionForExternal := "4.14.0"
			platformName := "oci"
			cloudControllerManager := models.PlatformExternalCloudControllerManagerExternal
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, MinimalOpenShiftVersionForExternal)
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           &platformName,
					CloudControllerManager: &cloudControllerManager,
				},
			}

			registerParams.NewClusterParams.OpenshiftVersion = swag.String(MinimalOpenShiftVersionForExternal)
			reply := bm.V2RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeExternal))
			Expect(*cluster.Platform.External.PlatformName).Should(Equal(platformName))
			Expect(*cluster.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
			Expect(swag.BoolValue(cluster.UserManagedNetworking)).Should(BeTrue())
		})

		It("external platform - SNO", func() {
			MinimalOpenShiftVersionForExternal := "4.14.0"
			platformName := "platform-name"
			cloudControllerManager := models.PlatformExternalCloudControllerManagerEmpty
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, MinimalOpenShiftVersionForExternal)
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           &platformName,
					CloudControllerManager: &cloudControllerManager,
				},
			}

			registerParams.NewClusterParams.OpenshiftVersion = swag.String(MinimalOpenShiftVersionForExternal)
			registerParams.NewClusterParams.HighAvailabilityMode = swag.String(models.ClusterCreateParamsHighAvailabilityModeNone)

			reply := bm.V2RegisterCluster(ctx, *registerParams)
			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeExternal))
			Expect(*cluster.Platform.External.PlatformName).Should(Equal(platformName))
			Expect(*cluster.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
			Expect(swag.BoolValue(cluster.UserManagedNetworking)).Should(BeTrue())
		})

		It("external platform - RHSSO - PlatformExternalCapability is set", func() {
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.14")
			platformName := "platform-name"
			cloudControllerManager := models.PlatformExternalCloudControllerManagerExternal

			cfg := auth.GetConfigRHSSO()
			cfg.EnableOrgBasedFeatureGates = true
			mockOcmAuthz := ocm.NewMockOCMAuthorization(ctrl)
			mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
			bm.authzHandler = auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
			payload := &ocm.AuthPayload{Role: ocm.UserRole}
			payload.Username = "user1"
			payload.Organization = "org1"
			authCtx := context.WithValue(ctx, restapi.AuthKey, payload)
			mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), payload.Username, ocm.PlatformExternalCapabilityName, ocm.OrganizationCapabilityType).Return(true, nil).Times(1)
			mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), payload.Username, ocm.SoftTimeoutsCapabilityName, ocm.OrganizationCapabilityType).Return(false, nil).Times(1)

			registerParams.NewClusterParams.OpenshiftVersion = swag.String("4.14")
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           &platformName,
					CloudControllerManager: &cloudControllerManager,
				},
			}
			reply := bm.V2RegisterCluster(authCtx, *registerParams)

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeExternal))
			Expect(*cluster.Platform.External.PlatformName).Should(Equal(platformName))
			Expect(*cluster.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
			Expect(swag.BoolValue(cluster.UserManagedNetworking)).Should(BeTrue())
		})

		It("external platform - PlatformName=oci - RHSSO - PlatformExternalCapability is set", func() {
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.14")
			platformName := "oci"
			cloudControllerManager := models.PlatformExternalCloudControllerManagerExternal

			cfg := auth.GetConfigRHSSO()
			cfg.EnableOrgBasedFeatureGates = true
			mockOcmAuthz := ocm.NewMockOCMAuthorization(ctrl)
			mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
			bm.authzHandler = auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
			payload := &ocm.AuthPayload{Role: ocm.UserRole}
			payload.Username = "user1"
			payload.Organization = "org1"
			authCtx := context.WithValue(ctx, restapi.AuthKey, payload)
			mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), payload.Username, ocm.PlatformExternalCapabilityName, ocm.OrganizationCapabilityType).Return(true, nil).Times(1)
			mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), payload.Username, ocm.PlatformOciCapabilityName, ocm.OrganizationCapabilityType).Return(false, nil).Times(1)
			mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), payload.Username, ocm.SoftTimeoutsCapabilityName, ocm.OrganizationCapabilityType).Return(false, nil).Times(1)

			registerParams.NewClusterParams.OpenshiftVersion = swag.String("4.14")
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           &platformName,
					CloudControllerManager: &cloudControllerManager,
				},
			}
			reply := bm.V2RegisterCluster(authCtx, *registerParams)

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeExternal))
			Expect(*cluster.Platform.External.PlatformName).Should(Equal(platformName))
			Expect(*cluster.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
			Expect(swag.BoolValue(cluster.UserManagedNetworking)).Should(BeTrue())
		})

		It("external platform - PlatformName=oci - RHSSO - PlatformOciCapability is set", func() {
			mockClusterRegisterSuccessWithVersion(models.ClusterCPUArchitectureX8664, "4.14")
			platformName := "oci"
			cloudControllerManager := models.PlatformExternalCloudControllerManagerExternal

			cfg := auth.GetConfigRHSSO()
			cfg.EnableOrgBasedFeatureGates = true
			mockOcmAuthz := ocm.NewMockOCMAuthorization(ctrl)
			mockOcmClient := &ocm.Client{Cache: cache.New(10*time.Minute, 30*time.Minute), Authorization: mockOcmAuthz}
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
			bm.authzHandler = auth.NewAuthzHandler(cfg, mockOcmClient, common.GetTestLog().WithField("pkg", "auth"), db)
			payload := &ocm.AuthPayload{Role: ocm.UserRole}
			payload.Username = "user1"
			payload.Organization = "org1"
			authCtx := context.WithValue(ctx, restapi.AuthKey, payload)
			mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), payload.Username, ocm.PlatformOciCapabilityName, ocm.OrganizationCapabilityType).Return(true, nil).Times(1)
			mockOcmAuthz.EXPECT().CapabilityReview(gomock.Any(), payload.Username, ocm.SoftTimeoutsCapabilityName, ocm.OrganizationCapabilityType).Return(false, nil).Times(1)

			registerParams.NewClusterParams.OpenshiftVersion = swag.String("4.14")
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           &platformName,
					CloudControllerManager: &cloudControllerManager,
				},
			}
			reply := bm.V2RegisterCluster(authCtx, *registerParams)

			Expect(reply).Should(BeAssignableToTypeOf(installer.NewV2RegisterClusterCreated()))
			cluster := reply.(*installer.V2RegisterClusterCreated).Payload
			Expect(cluster.Platform).ShouldNot(BeNil())
			Expect(common.PlatformTypeValue(cluster.Platform.Type)).Should(BeEquivalentTo(models.PlatformTypeExternal))
			Expect(*cluster.Platform.External.PlatformName).Should(Equal(platformName))
			Expect(*cluster.Platform.External.CloudControllerManager).Should(Equal(cloudControllerManager))
			Expect(swag.BoolValue(cluster.UserManagedNetworking)).Should(BeTrue())
		})

	})

	Context("Failed to register cluster", func() {

		It("nutanix platform - fail on old version", func() {
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeNutanix),
			}
			invalidOpenShiftVersionForNutanix := "4.10.0"
			registerParams.NewClusterParams.OpenshiftVersion = swag.String(invalidOpenShiftVersionForNutanix)

			reply := bm.V2RegisterCluster(ctx, *registerParams)
			verifyApiError(reply, http.StatusBadRequest)
		})

		It("external platform - fail if UMN is false", func() {
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
			}
			registerParams.NewClusterParams.UserManagedNetworking = swag.Bool(false)
			reply := bm.V2RegisterCluster(ctx, *registerParams)
			verifyApiErrorString(reply, http.StatusBadRequest, "Can't set external platform with user-managed-networking disabled")
		})

		It("external platform - fail if external settings are not set", func() {
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
			}
			registerParams.NewClusterParams.UserManagedNetworking = swag.Bool(true)

			reply := bm.V2RegisterCluster(ctx, *registerParams)
			verifyApiErrorString(reply, http.StatusBadRequest, "External setting must be set when using platform type external")
		})

		It("external platform - fail if platforn name is nil", func() {
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           nil,
					CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerExternal),
				},
			}
			registerParams.NewClusterParams.UserManagedNetworking = swag.Bool(true)

			reply := bm.V2RegisterCluster(ctx, *registerParams)
			verifyApiErrorString(reply, http.StatusBadRequest, "Platform name must be set to a non-empty string when using platform type external")
		})

		It("external platform - platformName=oci fail if cloudControllerManager is left unset", func() {
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName: swag.String("oci"),
				},
			}
			registerParams.NewClusterParams.UserManagedNetworking = swag.Bool(true)

			reply := bm.V2RegisterCluster(ctx, *registerParams)
			verifyApiErrorString(reply, http.StatusBadRequest, "Cloud controller manager must be enabled when using oci external integration")
		})

		It("external platform - platformName=oci fail if cloudControllerManager is disabled", func() {
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeExternal),
				External: &models.PlatformExternal{
					PlatformName:           swag.String("oci"),
					CloudControllerManager: swag.String(models.PlatformExternalCloudControllerManagerEmpty),
				},
			}
			registerParams.NewClusterParams.UserManagedNetworking = swag.Bool(true)

			reply := bm.V2RegisterCluster(ctx, *registerParams)
			verifyApiErrorString(reply, http.StatusBadRequest, "Cloud controller manager must be enabled when using oci external integration")
		})

		It("baremetal platform - fail if external settings are set", func() {
			platformName := "platform-name"
			cloudControllerManager := models.PlatformExternalCloudControllerManagerEmpty
			registerParams.NewClusterParams.Platform = &models.Platform{
				Type: common.PlatformTypePtr(models.PlatformTypeBaremetal),
				External: &models.PlatformExternal{
					PlatformName:           &platformName,
					CloudControllerManager: &cloudControllerManager,
				},
			}

			reply := bm.V2RegisterCluster(ctx, *registerParams)
			verifyApiErrorString(reply, http.StatusBadRequest, "External settings can only be set with external platform type")
		})
	})
})

var _ = Describe("DownloadClusterFiles", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		newCluster *common.Cluster
		ctx        = context.Background()
		db         *gorm.DB
		dbName     string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)

	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("allows downloading cluster owner files when not using auth", func() {
		for _, fileName := range cluster.ClusterOwnerFileNames {
			By(fmt.Sprintf("downloading %s", fileName))
			newCluster = createCluster(db, models.ClusterStatusInstalled)
			params := installer.V2DownloadClusterFilesParams{
				ClusterID: *newCluster.ID,
				FileName:  fileName,
			}

			r := io.NopCloser(bytes.NewReader([]byte("testfile")))
			expected := filemiddleware.NewResponder(installer.NewV2DownloadClusterFilesOK().WithPayload(r), fileName, int64(8), nil)
			mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/%s", *newCluster.ID, fileName)).Return(r, int64(8), nil)
			resp := bm.V2DownloadClusterFiles(ctx, params)
			Expect(resp).Should(Equal(expected))
		}
	})
	It("allows downloading kubeconfig-noingress when cluster is installing pending user action", func() {
		fileName := "kubeconfig-noingress"
		By(fmt.Sprintf("downloading %s", fileName))
		newCluster = createCluster(db, models.ClusterStatusInstallingPendingUserAction)
		params := installer.V2DownloadClusterFilesParams{
			ClusterID: *newCluster.ID,
			FileName:  fileName,
		}

		r := io.NopCloser(bytes.NewReader([]byte("testfile")))
		expected := filemiddleware.NewResponder(installer.NewV2DownloadClusterFilesOK().WithPayload(r), fileName, int64(8), nil)
		mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/%s", *newCluster.ID, fileName)).Return(r, int64(8), nil)
		resp := bm.V2DownloadClusterFiles(ctx, params)
		Expect(resp).Should(Equal(expected))
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
		c         *common.Cluster
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)

		c = &common.Cluster{Cluster: models.Cluster{
			ID:               &clusterID,
			PullSecretSet:    true,
			OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
			Status:           swag.String(models.ClusterStatusPreparingForInstallation),
		}, PullSecret: fakePullSecret}
		Expect(db.Create(c).Error).ShouldNot(HaveOccurred())
		Expect(common.CreateInfraEnvForCluster(db, c, models.ImageTypeFullIso)).ShouldNot(HaveOccurred())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("v2 blocks downloading cluster credentials files", func() {
		for _, fileName := range cluster.ClusterOwnerFileNames {
			By(fmt.Sprintf("downloading %s", fileName))

			params := installer.V2DownloadClusterCredentialsParams{
				ClusterID: clusterID,
				FileName:  fileName,
			}

			resp := bm.V2DownloadClusterCredentials(ctx, params)
			Expect(resp).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(resp.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusConflict)))
		}
	})

	It("v2 downloading cluster credentials files - no cluster id", func() {
		clusterId := strToUUID(uuid.New().String())

		for _, fileName := range cluster.ClusterOwnerFileNames {
			By(fmt.Sprintf("downloading %s", fileName))

			params := installer.V2DownloadClusterCredentialsParams{
				ClusterID: *clusterId,
				FileName:  fileName,
			}

			resp := bm.V2DownloadClusterCredentials(ctx, params)
			Expect(resp).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
			Expect(resp.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusNotFound)))

		}
	})

	It("Kubeconfig is not available", func() {
		status := models.ClusterStatusInstalling
		c.Status = &status
		db.Save(c)
		By(fmt.Sprintf("downloading %s", constants.Kubeconfig))

		params := installer.V2DownloadClusterCredentialsParams{
			ClusterID: clusterID,
			FileName:  constants.Kubeconfig,
		}

		r := io.NopCloser(bytes.NewReader([]byte("testfile")))
		expected := filemiddleware.NewResponder(installer.NewV2DownloadClusterCredentialsOK().WithPayload(r), constants.KubeconfigNoIngress, int64(8), nil)
		mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/%s", clusterID, constants.KubeconfigNoIngress)).Return(r, int64(8), nil)
		resp := bm.V2DownloadClusterCredentials(ctx, params)
		Expect(resp).Should(Equal(expected))
	})

	It("v2 allows downloading cluster credentials files", func() {
		statuses := []string{models.ClusterStatusInstalled, models.ClusterStatusCancelled, models.ClusterStatusError}

		for index := range statuses {
			c.Status = &statuses[index]
			db.Save(c)

			for _, fileName := range cluster.ClusterOwnerFileNames {
				By(fmt.Sprintf("downloading %s", fileName))

				params := installer.V2DownloadClusterCredentialsParams{
					ClusterID: clusterID,
					FileName:  fileName,
				}

				r := io.NopCloser(bytes.NewReader([]byte("testfile")))
				expected := filemiddleware.NewResponder(installer.NewV2DownloadClusterCredentialsOK().WithPayload(r), fileName, int64(8), nil)
				mockS3Client.EXPECT().Download(ctx, fmt.Sprintf("%s/%s", clusterID, fileName)).Return(r, int64(8), nil)
				resp := bm.V2DownloadClusterCredentials(ctx, params)
				Expect(resp).Should(Equal(expected))
			}
		}
	})
})

func validateNetworkConfiguration(cluster *models.Cluster, clusterNetworks *[]*models.ClusterNetwork,
	serviceNetworks *[]*models.ServiceNetwork, machineNetworks *[]*models.MachineNetwork) {
	if clusterNetworks != nil {
		ExpectWithOffset(1, cluster.ClusterNetworks).To(HaveLen(len(*clusterNetworks)))
		for index := range *clusterNetworks {
			ExpectWithOffset(1, cluster.ClusterNetworks[index].ClusterID).To(Equal(*cluster.ID))
			ExpectWithOffset(1, cluster.ClusterNetworks[index].Cidr).To(Equal((*clusterNetworks)[index].Cidr))
			ExpectWithOffset(1, cluster.ClusterNetworks[index].HostPrefix).To(Equal((*clusterNetworks)[index].HostPrefix))
		}
		// TODO(MGMT-9751-remove-single-network)
		ExpectWithOffset(1, cluster.ClusterNetworkCidr).To(Equal(""))
		ExpectWithOffset(1, cluster.ClusterNetworkHostPrefix).To(Equal(int64(0)))
	}
	if serviceNetworks != nil {
		ExpectWithOffset(1, cluster.ServiceNetworks).To(HaveLen(len(*serviceNetworks)))
		for index := range *serviceNetworks {
			ExpectWithOffset(1, cluster.ServiceNetworks[index].ClusterID).To(Equal(*cluster.ID))
			ExpectWithOffset(1, cluster.ServiceNetworks[index].Cidr).To(Equal((*serviceNetworks)[index].Cidr))
		}
		// TODO(MGMT-9751-remove-single-network)
		ExpectWithOffset(1, cluster.ServiceNetworkCidr).To(Equal(""))
	}
	if machineNetworks != nil {
		ExpectWithOffset(1, cluster.MachineNetworks).To(HaveLen(len(*machineNetworks)))
		for index := range *machineNetworks {
			ExpectWithOffset(1, cluster.MachineNetworks[index].ClusterID).To(Equal(*cluster.ID))
			ExpectWithOffset(1, cluster.MachineNetworks[index].Cidr).To(Equal((*machineNetworks)[index].Cidr))
		}
		// TODO(MGMT-9751-remove-single-network)
		ExpectWithOffset(1, cluster.MachineNetworkCidr).To(Equal(""))
	}
}

func validateHostsRequestedHostname(cluster *models.Cluster) {
	for i := range cluster.Hosts {
		Expect(cluster.Hosts[i].RequestedHostname).Should(Not(BeEmpty()))
	}
}

var _ = Describe("Update cluster - feature usage flags", func() {
	var (
		bm      *bareMetalInventory
		cfg     Config
		db      *gorm.DB
		cluster *common.Cluster
		dbName  string
		usages  = map[string]models.Usage{}
	)
	BeforeEach(func() {
		Expect(envconfig.Process("test", &cfg)).ShouldNot(HaveOccurred())
		Expect(cfg.IPv6Support).Should(BeTrue())
		cfg = Config{}
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
		cluster = createCluster(db, models.ClusterStatusPendingForInput)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("CPU Architecture usage", func() {
		It("Add feature usage of ARM64 when cluster arch match", func() {
			mockUsage.EXPECT().Add(usages, usage.CPUArchitectureARM64, nil).Times(1)
			mockUsage.EXPECT().Remove(usages, usage.CPUArchitectureARM64).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.CPUArchitectureS390x).Times(1)
			mockUsage.EXPECT().Remove(usages, usage.CPUArchitecturePpc64le).Times(1)
			cluster.CPUArchitecture = "arm64"
			bm.updateClusterCPUFeatureUsage(cluster.CPUArchitecture, usages)
		})

		It("Remove feature usage of ARM64 when cluster arch match", func() {
			mockUsage.EXPECT().Add(usages, usage.CPUArchitectureARM64, nil).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.CPUArchitectureARM64).Times(1)
			mockUsage.EXPECT().Remove(usages, usage.CPUArchitectureS390x).Times(1)
			mockUsage.EXPECT().Remove(usages, usage.CPUArchitecturePpc64le).Times(1)
			bm.updateClusterCPUFeatureUsage(cluster.CPUArchitecture, usages)
		})
	})

	Context("Cluster managed network with VMs", func() {
		It("Should Not add usage when network is not cluster managed", func() {
			userManagedNetwork := true
			mockUsage.EXPECT().Add(usages, usage.ClusterManagedNetworkWithVMs, gomock.Any()).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.ClusterManagedNetworkWithVMs).Times(1)
			bm.updateClusterNetworkVMUsage(cluster, &models.V2ClusterUpdateParams{
				UserManagedNetworking: &userManagedNetwork,
			}, usages, common.GetTestLog())
		})
		It("Should not add usage when network is cluster managed but no VM hosts", func() {
			userManagedNetwork := false
			mockUsage.EXPECT().Add(usages, usage.ClusterManagedNetworkWithVMs, gomock.Any()).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.ClusterManagedNetworkWithVMs).Times(1)
			bm.updateClusterNetworkVMUsage(cluster, &models.V2ClusterUpdateParams{
				UserManagedNetworking: &userManagedNetwork,
			}, usages, common.GetTestLog())
		})
		It("Should not add usage when network is user managed and contains VMs", func() {
			userManagedNetwork := true
			addVMToCluster(cluster, db)
			mockUsage.EXPECT().Add(usages, usage.ClusterManagedNetworkWithVMs, gomock.Any()).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.ClusterManagedNetworkWithVMs).Times(1)
			bm.updateClusterNetworkVMUsage(cluster, &models.V2ClusterUpdateParams{
				UserManagedNetworking: &userManagedNetwork,
			}, usages, common.GetTestLog())
		})
		It("Should not add usage when updating cluster userManagedNetworking nil->true", func() {
			addVMToCluster(cluster, db)
			userManagedNetwork := true
			mockUsage.EXPECT().Add(usages, usage.ClusterManagedNetworkWithVMs, gomock.Any()).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.ClusterManagedNetworkWithVMs).Times(1)
			bm.updateClusterNetworkVMUsage(cluster, &models.V2ClusterUpdateParams{
				UserManagedNetworking: &userManagedNetwork,
			}, usages, common.GetTestLog())
		})
		It("Should add usage when updating cluster userManagedNetworking false->(no update value)", func() {
			addVMToCluster(cluster, db)
			userManagedNetwork := false
			cluster.UserManagedNetworking = &userManagedNetwork
			mockUsage.EXPECT().Add(usages, usage.ClusterManagedNetworkWithVMs, gomock.Any()).Times(1)
			mockUsage.EXPECT().Remove(usages, usage.ClusterManagedNetworkWithVMs).Times(0)
			bm.updateClusterNetworkVMUsage(cluster, nil, usages, common.GetTestLog())
		})
		It("Should remove usage when updating userManagedNetworking false -> true", func() {
			addVMToCluster(cluster, db)
			userManagedNetwork := false
			updateTo := true
			cluster.UserManagedNetworking = &userManagedNetwork
			mockUsage.EXPECT().Add(usages, usage.ClusterManagedNetworkWithVMs, gomock.Any()).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.ClusterManagedNetworkWithVMs).Times(1)
			bm.updateClusterNetworkVMUsage(cluster, &models.V2ClusterUpdateParams{
				UserManagedNetworking: &updateTo,
			}, usages, common.GetTestLog())
		})

		It("Should add usage when updating userManagedNetworking true -> false", func() {
			addVMToCluster(cluster, db)
			userManagedNetwork := true
			updateTo := false
			cluster.UserManagedNetworking = &userManagedNetwork
			mockUsage.EXPECT().Add(usages, usage.ClusterManagedNetworkWithVMs, gomock.Any()).Times(1)
			mockUsage.EXPECT().Remove(usages, usage.ClusterManagedNetworkWithVMs).Times(0)
			bm.updateClusterNetworkVMUsage(cluster, &models.V2ClusterUpdateParams{
				UserManagedNetworking: &updateTo,
			}, usages, common.GetTestLog())
		})

		It("Should not add usage when platform is vsphere", func() {
			mockUsage.EXPECT().Add(usages, usage.ClusterManagedNetworkWithVMs, gomock.Any()).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.ClusterManagedNetworkWithVMs).Times(1)
			platformType := models.PlatformTypeVsphere

			bm.updateClusterNetworkVMUsage(cluster, &models.V2ClusterUpdateParams{
				Platform: &models.Platform{
					Type: &platformType,
				},
			}, usages, common.GetTestLog())
		})
	})

	Context("Static network usage", func() {
		It("Add feature usage when StaticNetworkConfig is set", func() {
			mockUsage.EXPECT().Add(usages, usage.StaticNetworkConfigUsage, nil).Times(1)
			mockUsage.EXPECT().Remove(usages, usage.StaticNetworkConfigUsage).Times(0)
			mockUsage.EXPECT().Save(gomock.Any(), *cluster.ID, usages).Times(1)
			err := bm.setStaticNetworkUsage(db, *cluster.ID, "some static network config")
			Expect(err).ToNot(HaveOccurred())
		})

		It("Remove feature usage when StaticNetworkConfig is not set", func() {
			mockUsage.EXPECT().Add(usages, usage.StaticNetworkConfigUsage, nil).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.StaticNetworkConfigUsage).Times(1)
			mockUsage.EXPECT().Save(gomock.Any(), *cluster.ID, usages).Times(1)
			err := bm.setStaticNetworkUsage(db, *cluster.ID, "")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("User Managed Network With Multi Node Usage", func() {
		It("feature usage when userManagedNetworking is true and highAvailabilityMode is Full should be on", func() {
			userManagedNetwork := true
			highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeFull
			mockUsage.EXPECT().Add(usages, usage.UserManagedNetworkingWithMultiNode, nil).Times(1)
			mockUsage.EXPECT().Remove(usages, usage.UserManagedNetworkingWithMultiNode).Times(0)
			bm.setUserManagedNetworkingAndMultiNodeUsage(userManagedNetwork, highAvailabilityMode, usages)
		})

		It("feature usage when userManagedNetworking is false and highAvailabilityMode is Full should be off", func() {
			userManagedNetwork := false
			highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeFull
			mockUsage.EXPECT().Add(usages, usage.UserManagedNetworkingWithMultiNode, nil).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.UserManagedNetworkingWithMultiNode).Times(1)
			bm.setUserManagedNetworkingAndMultiNodeUsage(userManagedNetwork, highAvailabilityMode, usages)
		})

		It("feature usage when userManagedNetworking is true and highAvailabilityMode is None should be off", func() {
			userManagedNetwork := true
			highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeNone
			mockUsage.EXPECT().Add(usages, usage.UserManagedNetworkingWithMultiNode, nil).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.UserManagedNetworkingWithMultiNode).Times(1)
			bm.setUserManagedNetworkingAndMultiNodeUsage(userManagedNetwork, highAvailabilityMode, usages)
		})

		It("feature usage when userManagedNetworking is false and highAvailabilityMode is None should be off", func() {
			userManagedNetwork := false
			highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeNone
			mockUsage.EXPECT().Add(usages, usage.UserManagedNetworkingWithMultiNode, nil).Times(0)
			mockUsage.EXPECT().Remove(usages, usage.UserManagedNetworkingWithMultiNode).Times(1)
			bm.setUserManagedNetworkingAndMultiNodeUsage(userManagedNetwork, highAvailabilityMode, usages)
		})
	})
})

var _ = Describe("Download presigned cluster credentials", func() {

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
			APIVips:          []*models.APIVip{{IP: "10.11.12.13"}},
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
		generateReply := bm.V2GetPresignedForClusterCredentials(ctx, installer.V2GetPresignedForClusterCredentialsParams{
			ClusterID: clusterID,
			FileName:  constants.Kubeconfig,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusBadRequest)))
	})

	It("kubeconfig presigned cluster is not in installed state", func() {
		fullS3Path := fmt.Sprintf("%s/%s", clusterID.String(), constants.Kubeconfig)

		mockS3Client.EXPECT().GeneratePresignedDownloadURL(
			ctx, fullS3Path, constants.Kubeconfig, gomock.Any()).Return("", errors.New("some error"))
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		mockS3Client.EXPECT().DoesObjectExist(ctx, fullS3Path).Return(true, nil)
		generateReply := bm.V2GetPresignedForClusterCredentials(ctx, installer.V2GetPresignedForClusterCredentialsParams{
			ClusterID: clusterID,
			FileName:  constants.Kubeconfig,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusInternalServerError)))
	})

	It("presigned cluster credentials  - downloading no-ingress kubeconfig", func() {
		status := models.ClusterStatusInstalling
		c.Status = &status
		db.Save(&c)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		fileName := constants.Kubeconfig
		fullS3Name := fmt.Sprintf("%s/%s", clusterID.String(), fileName)

		mockS3Client.EXPECT().DoesObjectExist(ctx, fullS3Name).Return(false, nil)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(
			ctx, fmt.Sprintf("%s/%s", clusterID.String(), constants.KubeconfigNoIngress), constants.KubeconfigNoIngress, gomock.Any()).Return("url", nil)
		generateReply := bm.V2GetPresignedForClusterCredentials(ctx, installer.V2GetPresignedForClusterCredentialsParams{
			ClusterID: clusterID,
			FileName:  constants.Kubeconfig,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.V2GetPresignedForClusterCredentialsOK{}))
		replyPayload := generateReply.(*installer.V2GetPresignedForClusterCredentialsOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})

	It("presigned cluster credentials happy flow", func() {
		status := models.ClusterStatusInstalled
		c.Status = &status
		db.Save(&c)
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		fullS3Name := fmt.Sprintf("%s/%s", clusterID.String(), constants.Kubeconfig)
		mockS3Client.EXPECT().DoesObjectExist(ctx, fullS3Name).Return(true, nil)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(
			ctx, fullS3Name, constants.Kubeconfig, gomock.Any()).Return("url", nil)
		generateReply := bm.V2GetPresignedForClusterCredentials(ctx, installer.V2GetPresignedForClusterCredentialsParams{
			ClusterID: clusterID,
			FileName:  constants.Kubeconfig,
		})
		Expect(generateReply).Should(BeAssignableToTypeOf(&installer.V2GetPresignedForClusterCredentialsOK{}))
		replyPayload := generateReply.(*installer.V2GetPresignedForClusterCredentialsOK).Payload
		Expect(*replyPayload.URL).Should(Equal("url"))
	})

	It("presigned cluster credentials download with invalid cluster id", func() {
		clusterId := strToUUID(uuid.New().String())
		mockS3Client.EXPECT().IsAwsS3().Return(true)
		fullS3Name := fmt.Sprintf("%s/%s", clusterId.String(), constants.Kubeconfig)
		mockS3Client.EXPECT().DoesObjectExist(ctx, fullS3Name).Return(true, nil)
		mockS3Client.EXPECT().GeneratePresignedDownloadURL(
			ctx, fullS3Name, constants.Kubeconfig, gomock.Any()).Return("", errors.New("some error"))
		generateReply := bm.V2GetPresignedForClusterCredentials(ctx, installer.V2GetPresignedForClusterCredentialsParams{
			ClusterID: *clusterId,
			FileName:  constants.Kubeconfig,
		})
		Expect(generateReply).To(BeAssignableToTypeOf(&common.ApiErrorResponse{}))
		Expect(generateReply.(*common.ApiErrorResponse).StatusCode()).To(Equal(int32(http.StatusInternalServerError)))
	})
})

func equalMonitoredOperators(m1, m2 *models.MonitoredOperator) bool {
	return m1.Status == m2.Status &&
		time.Time(m1.StatusUpdatedAt).Equal(time.Time(m2.StatusUpdatedAt)) &&
		m1.Name == m2.Name &&
		m1.ClusterID == m2.ClusterID &&
		m1.Status == m2.Status &&
		m1.StatusInfo == m2.StatusInfo &&
		m1.Namespace == m2.Namespace &&
		m1.OperatorType == m2.OperatorType &&
		m1.Properties == m2.Properties &&
		m1.SubscriptionName == m2.SubscriptionName &&
		m1.TimeoutSeconds == m2.TimeoutSeconds
}

func equivalentMonitoredOperators(l1, l2 []*models.MonitoredOperator) bool {
outer:
	for _, e1 := range l1 {
		for _, e2 := range l2 {
			if equalMonitoredOperators(e1, e2) {
				continue outer
			}
		}
		return false
	}
	return true
}

func containsMonitoredOperator(l []*models.MonitoredOperator, m *models.MonitoredOperator) bool {
	for _, e := range l {
		if equalMonitoredOperators(m, e) {
			return true
		}
	}
	return false
}

var _ = Describe("RegenerateInfraEnvSigningKey", func() {
	var (
		bm         *bareMetalInventory
		cfg        Config
		db         *gorm.DB
		ctx        = context.Background()
		dbName     string
		infraEnvID strfmt.UUID
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)

		infraEnvID = strfmt.UUID(uuid.New().String())
		ie := &common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID: &infraEnvID,
			},
			ImageTokenKey: "initialkeyhere",
		}
		Expect(db.Create(ie).Error).To(Succeed())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("returns NotFound for a missing infraEnv", func() {
		otherInfraEnvID := strfmt.UUID(uuid.New().String())
		params := installer.RegenerateInfraEnvSigningKeyParams{InfraEnvID: otherInfraEnvID}
		resp := bm.RegenerateInfraEnvSigningKey(ctx, params)
		verifyApiError(resp, http.StatusNotFound)
	})

	It("resets the infraEnv image_token_key", func() {
		params := installer.RegenerateInfraEnvSigningKeyParams{InfraEnvID: infraEnvID}
		resp := bm.RegenerateInfraEnvSigningKey(ctx, params)
		Expect(resp).Should(BeAssignableToTypeOf(installer.NewRegenerateInfraEnvSigningKeyNoContent()))

		var infraEnv common.InfraEnv
		Expect(db.First(&infraEnv, "id = ?", infraEnvID.String()).Error).To(Succeed())
		Expect(infraEnv.ImageTokenKey).NotTo(Equal("initialkeyhere"))
	})
})

var _ = Describe("GetInfraEnvDownloadURL", func() {
	var (
		bm           *bareMetalInventory
		cfg          Config
		db           *gorm.DB
		ctx          = context.Background()
		dbName       string
		infraEnvID   strfmt.UUID
		testTokenKey = "6aa03bd3b328d44ddf9a9fefc1290a01a3d52294b51d2b54b61819010206c917" // #nosec
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
		var err error
		bm.ImageExpirationTime, err = time.ParseDuration("4h")
		Expect(err).NotTo(HaveOccurred())

		infraEnvID = strfmt.UUID(uuid.New().String())
		ie := &common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:               &infraEnvID,
				OpenshiftVersion: common.TestDefaultConfig.OpenShiftVersion,
				Type:             common.ImageTypePtr(models.ImageTypeFullIso),
				CPUArchitecture:  common.TestDefaultConfig.CPUArchitecture,
			},
			ImageTokenKey: testTokenKey,
		}
		Expect(db.Create(ie).Error).To(Succeed())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	getNewURL := func() *models.PresignedURL {
		mockOSImages.EXPECT().GetOsImageOrLatest(common.TestDefaultConfig.OpenShiftVersion, gomock.Any()).Return(common.TestDefaultConfig.OsImage, nil).Times(1)
		params := installer.GetInfraEnvDownloadURLParams{InfraEnvID: infraEnvID}
		resp := bm.GetInfraEnvDownloadURL(ctx, params)
		Expect(resp).To(BeAssignableToTypeOf(&installer.GetInfraEnvDownloadURLOK{}))
		payload := resp.(*installer.GetInfraEnvDownloadURLOK).Payload
		Expect(payload).ToNot(BeNil())
		return payload
	}

	Context("with no auth", func() {
		It("generates a url with no token", func() {
			payload := getNewURL()

			Expect(payload.ExpiresAt.String()).To(Equal("0001-01-01T00:00:00.000Z"))
			u, err := url.Parse(*payload.URL)
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Host).To(Equal(imageServiceHost))
			Expect(u.Path).To(Equal(
				fmt.Sprintf("%s/byid/%s/%s/%s/full.iso",
					imageServicePath,
					infraEnvID,
					*common.TestDefaultConfig.OsImage.OpenshiftVersion,
					*common.TestDefaultConfig.OsImage.CPUArchitecture)))
		})
	})

	Context("with local auth", func() {
		BeforeEach(func() {
			// Use a local auth handler
			pub, priv, err := gencrypto.ECDSAKeyPairPEM()
			Expect(err).NotTo(HaveOccurred())
			os.Setenv("EC_PRIVATE_KEY_PEM", priv)
			bm.authHandler, err = auth.NewLocalAuthenticator(
				&auth.Config{AuthType: auth.TypeLocal, ECPublicKeyPEM: pub},
				common.GetTestLog().WithField("pkg", "auth"),
				db,
			)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			os.Unsetenv("EC_PRIVATE_KEY_PEM")
		})

		It("sets a valid api_key token", func() {
			payload := getNewURL()

			Expect(payload.ExpiresAt.String()).To(Equal("0001-01-01T00:00:00.000Z"))
			u, err := url.Parse(*payload.URL)
			Expect(err).ToNot(HaveOccurred())

			Expect(u.Host).To(Equal(imageServiceHost))
			pathList := strings.Split(u.Path, "/")
			Expect(pathList).To(HaveLen(8))
			Expect(pathList[3]).To(Equal("byapikey"))
			Expect(pathList[5]).To(Equal(common.TestDefaultConfig.OpenShiftVersion))

			tok := pathList[4]
			_, err = bm.authHandler.AuthURLAuth(tok)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("with rhsso auth", func() {
		BeforeEach(func() {
			_, cert := auth.GetTokenAndCert(false)
			cfg := &auth.Config{JwkCert: string(cert)}
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)
		})

		It("sets a valid image_token", func() {
			payload := getNewURL()

			Expect(payload.ExpiresAt.String()).ToNot(Equal("0001-01-01T00:00:00.000Z"))
			u, err := url.Parse(*payload.URL)
			Expect(err).ToNot(HaveOccurred())

			Expect(u.Host).To(Equal(imageServiceHost))
			pathList := strings.Split(u.Path, "/")
			Expect(pathList).To(HaveLen(8))
			Expect(pathList[3]).To(Equal("bytoken"))
			Expect(pathList[5]).To(Equal(common.TestDefaultConfig.OpenShiftVersion))

			tok := pathList[4]
			_, err = bm.authHandler.AuthImageAuth(tok)
			Expect(err).NotTo(HaveOccurred())
		})

		It("updates the infra-env expires_at time", func() {
			payload := getNewURL()

			Expect(payload.ExpiresAt.String()).ToNot(Equal("0001-01-01T00:00:00.000Z"))
			var infraEnv common.InfraEnv
			Expect(db.First(&infraEnv, "id = ?", infraEnvID.String()).Error).To(Succeed())
			Expect(infraEnv.ExpiresAt.Equal(payload.ExpiresAt)).To(BeTrue())
		})
	})
})

var _ = Describe("GetInfraEnvPresignedFileURL", func() {
	var (
		bm           *bareMetalInventory
		cfg          Config
		db           *gorm.DB
		ctx          = context.Background()
		dbName       string
		infraEnvID   strfmt.UUID
		testTokenKey = "6aa03bd3b328d44ddf9a9fefc1290a01a3d52294b51d2b54b61819010206c917" // #nosec
		serviceHost  = "assisted.example.com"
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
		var err error
		bm.ImageExpirationTime, err = time.ParseDuration("4h")
		Expect(err).NotTo(HaveOccurred())
		bm.ServiceBaseURL = fmt.Sprintf("https://%s", serviceHost)

		infraEnvID = strfmt.UUID(uuid.New().String())
		ie := &common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID: &infraEnvID,
			},
			ImageTokenKey: testTokenKey,
		}
		Expect(db.Create(ie).Error).To(Succeed())
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	tryGetUrl := func(filename string, ipxeScriptType *string) middleware.Responder {
		params := installer.GetInfraEnvPresignedFileURLParams{InfraEnvID: infraEnvID, FileName: filename}
		params.IpxeScriptType = ipxeScriptType
		return bm.GetInfraEnvPresignedFileURL(ctx, params)
	}

	getNewURL := func(filename string, ipxeScriptType *string) *models.PresignedURL {
		resp := tryGetUrl(filename, ipxeScriptType)
		Expect(resp).To(BeAssignableToTypeOf(&installer.GetInfraEnvPresignedFileURLOK{}))
		payload := resp.(*installer.GetInfraEnvPresignedFileURLOK).Payload
		Expect(payload).ToNot(BeNil())
		return payload
	}

	Context("with no auth", func() {
		It("generates a url with no token for ipxe-script", func() {
			payload := getNewURL("ipxe-script", nil)

			Expect(payload.ExpiresAt.String()).To(Equal("0001-01-01T00:00:00.000Z"))
			u, err := url.Parse(*payload.URL)
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Host).To(Equal(serviceHost))
			Expect(u.Query().Get("image_token")).To(Equal(""))
			Expect(u.Query().Get("api_key")).To(Equal(""))
			Expect(u.Query().Get("file_name")).To(Equal("ipxe-script"))
			Expect(u.Query().Has("boot_control")).To(BeFalse())
			Expect(u.Path).To(Equal(fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", infraEnvID.String())))
		})

		It("generates a url with no token for ipxe-script with boot control", func() {
			payload := getNewURL("ipxe-script", swag.String(BootOrderControl))

			Expect(payload.ExpiresAt.String()).To(Equal("0001-01-01T00:00:00.000Z"))
			u, err := url.Parse(*payload.URL)
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Host).To(Equal(serviceHost))
			Expect(u.Query().Get("image_token")).To(Equal(""))
			Expect(u.Query().Get("api_key")).To(Equal(""))
			Expect(u.Query().Get("file_name")).To(Equal("ipxe-script"))
			Expect(u.Query().Get("ipxe_script_type")).To(Equal(BootOrderControl))
			Expect(u.Path).To(Equal(fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", infraEnvID.String())))
		})

		It("generates a url with no token for discovery.ign", func() {
			payload := getNewURL("discovery.ign", nil)

			Expect(payload.ExpiresAt.String()).To(Equal("0001-01-01T00:00:00.000Z"))
			u, err := url.Parse(*payload.URL)
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Host).To(Equal(serviceHost))
			Expect(u.Query().Get("image_token")).To(Equal(""))
			Expect(u.Query().Get("api_key")).To(Equal(""))
			Expect(u.Query().Get("file_name")).To(Equal("discovery.ign"))
			Expect(u.Query().Has("boot_control")).To(BeFalse())
			Expect(u.Path).To(Equal(fmt.Sprintf("/api/assisted-install/v2/infra-envs/%s/downloads/files", infraEnvID.String())))
		})

		It("returns bad request when boot_control is used with discovery.ign", func() {
			payload := tryGetUrl("discovery.ign", swag.String(BootOrderControl))
			verifyApiError(payload, http.StatusBadRequest)
		})
	})

	Context("with local auth", func() {
		BeforeEach(func() {
			// Use a local auth handler
			pub, priv, err := gencrypto.ECDSAKeyPairPEM()
			Expect(err).NotTo(HaveOccurred())
			os.Setenv("EC_PRIVATE_KEY_PEM", priv)
			bm.authHandler, err = auth.NewLocalAuthenticator(
				&auth.Config{AuthType: auth.TypeLocal, ECPublicKeyPEM: pub},
				common.GetTestLog().WithField("pkg", "auth"),
				db,
			)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			os.Unsetenv("EC_PRIVATE_KEY_PEM")
		})

		It("sets a valid api_key token", func() {
			payload := getNewURL("ipxe-script", nil)

			Expect(payload.ExpiresAt.String()).To(Equal("0001-01-01T00:00:00.000Z"))
			u, err := url.Parse(*payload.URL)
			Expect(err).ToNot(HaveOccurred())

			_, err = bm.authHandler.AuthURLAuth(u.Query().Get("api_key"))
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("with rhsso auth", func() {
		BeforeEach(func() {
			_, cert := auth.GetTokenAndCert(false)
			cfg := &auth.Config{JwkCert: string(cert)}
			bm.authHandler = auth.NewRHSSOAuthenticator(cfg, nil, common.GetTestLog().WithField("pkg", "auth"), db)
		})

		It("sets a valid image_token", func() {
			payload := getNewURL("ipxe-script", nil)

			Expect(payload.ExpiresAt.String()).ToNot(Equal("0001-01-01T00:00:00.000Z"))
			u, err := url.Parse(*payload.URL)
			Expect(err).ToNot(HaveOccurred())

			_, err = bm.authHandler.AuthImageAuth(u.Query().Get("image_token"))
			Expect(err).NotTo(HaveOccurred())
		})
	})

	It("returns not found for a missing infra-env", func() {
		otherInfraEnvID := strfmt.UUID(uuid.New().String())
		params := installer.GetInfraEnvPresignedFileURLParams{InfraEnvID: otherInfraEnvID}
		resp := bm.GetInfraEnvPresignedFileURL(ctx, params)
		verifyApiError(resp, http.StatusNotFound)
	})
})

var _ = Describe("GetHostByKubeKey", func() {
	var (
		bm        *bareMetalInventory
		cfg       Config
		db        *gorm.DB
		dbName    string
		hostID    *strfmt.UUID
		clusterID *strfmt.UUID
		kubeKey   = types.NamespacedName{
			Namespace: "test-namespace",
			Name:      "test-host",
		}
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
		cluster := createClusterWithAvailability(db, models.ClusterStatusReady, models.ClusterCreateParamsHighAvailabilityModeNone)
		clusterID = cluster.ID
		// this doesn't need to be a VM, but any host works for this test
		addVMToCluster(cluster, db)
		hostID = cluster.Hosts[0].ID
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	var getHost = func(_ types.NamespacedName) (*common.Host, error) {
		return common.GetHostFromDBbyHostId(db, *hostID)
	}

	It("returns successfully when the cluster exists", func() {
		mockHostApi.EXPECT().GetHostByKubeKey(kubeKey).DoAndReturn(getHost)
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(host.SnoStages[:])
		h, err := bm.GetHostByKubeKey(kubeKey)
		Expect(err).NotTo(HaveOccurred())
		Expect(h.ID).To(Equal(hostID))
	})

	It("returns successfully when the cluster ID is not set", func() {
		Expect(db.Model(&common.Host{}).Where("id = ?", hostID.String()).Updates(map[string]interface{}{"cluster_id": nil}).Error).ToNot(HaveOccurred())
		mockHostApi.EXPECT().GetHostByKubeKey(kubeKey).DoAndReturn(getHost)
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(host.SnoStages[:])
		h, err := bm.GetHostByKubeKey(kubeKey)
		Expect(err).NotTo(HaveOccurred())
		Expect(h.ID).To(Equal(hostID))
	})

	It("returns successfully when the referenced cluster is deleted", func() {
		Expect(db.Delete(&common.Cluster{}, clusterID).Error).NotTo(HaveOccurred())
		mockHostApi.EXPECT().GetHostByKubeKey(kubeKey).DoAndReturn(getHost)
		mockHostApi.EXPECT().GetStagesByRole(gomock.Any(), gomock.Any()).Return(host.SnoStages[:])
		h, err := bm.GetHostByKubeKey(kubeKey)
		Expect(err).NotTo(HaveOccurred())
		Expect(h.ID).To(Equal(hostID))
	})

	It("fails when getting the host fails", func() {
		mockHostApi.EXPECT().GetHostByKubeKey(kubeKey).Return(nil, fmt.Errorf("Failed to get host"))
		_, err := bm.GetHostByKubeKey(kubeKey)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("V2UpdateHostIgnition unbound blabla", func() {
	var (
		bm     *bareMetalInventory
		cfg    Config
		db     *gorm.DB
		dbName string
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		bm = createInventory(db, cfg)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	It("unbound host ignition update", func() {
		infraEnvID := strfmt.UUID(uuid.New().String())
		infraEnv := &common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID: &infraEnvID,
			},
		}
		Expect(db.Create(infraEnv).Error).ToNot(HaveOccurred())

		hostID := strfmt.UUID(uuid.New().String())
		host := models.Host{
			ID:         &hostID,
			InfraEnvID: infraEnvID,
			Kind:       swag.String(models.HostKindHost),
			Status:     swag.String(models.HostStatusKnownUnbound),
			Role:       models.HostRoleAutoAssign,
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())

		override := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		params := installer.V2UpdateHostIgnitionParams{
			InfraEnvID:         infraEnvID,
			HostID:             hostID,
			HostIgnitionParams: &models.HostIgnitionParams{Config: override},
		}

		mockEvents.EXPECT().SendHostEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.HostDiscoveryIgnitionConfigAppliedEventName),
			eventstest.WithHostIdMatcher(params.HostID.String()),
			eventstest.WithInfraEnvIdMatcher(params.InfraEnvID.String())))

		response := bm.V2UpdateHostIgnition(context.TODO(), params)
		Expect(response).To(BeAssignableToTypeOf(&installer.V2UpdateHostIgnitionCreated{}))
	})
})

func getDummyCluster() common.Cluster {
	clusterID := strfmt.UUID(uuid.New().String())
	c := common.Cluster{
		Cluster: models.Cluster{
			ID:            &clusterID,
			Name:          "my-cluster",
			BaseDNSDomain: "my-domain",
		},
	}
	return c
}

func createClusterWithMonitoredOperator(db *gorm.DB, operator models.MonitoredOperator) *common.Cluster {
	c := getDummyCluster()
	c.MonitoredOperators = append(c.MonitoredOperators, &operator)
	Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
	return &c
}
