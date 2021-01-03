package subsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/copier"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	hostValidationMetric = "assisted_installer_host_validation_is_in_failed_status_on_cluster_deletion"
)

type hostValidationResult struct {
	ID      models.HostValidationID `json:"id"`
	Status  string                  `json:"status"`
	Message string                  `json:"message"`
}

func isHostValidationInFailedStatus(clusterID, hostID strfmt.UUID, validationID models.HostValidationID) (bool, error) {
	var validationRes map[string][]hostValidationResult
	h := getHost(clusterID, hostID)
	if h.ValidationsInfo == "" {
		return false, nil
	}
	err := json.Unmarshal([]byte(h.ValidationsInfo), &validationRes)
	Expect(err).ShouldNot(HaveOccurred())
	for _, vRes := range validationRes {
		for _, v := range vRes {
			if v.ID != validationID {
				continue
			}
			return v.Status == "failure", nil
		}
	}
	return false, nil
}

func filterMetrics(metrics []string, substrings ...string) []string {
	var res []string
	for _, m := range metrics {
		containsAll := true
		for _, ss := range substrings {
			if !strings.Contains(m, ss) {
				containsAll = false
				break
			}
		}
		if containsAll {
			res = append(res, m)
		}
	}
	return res
}

func assertValidationCounter(clusterID strfmt.UUID, validationID models.HostValidationID, expectedCounter int) {

	url := fmt.Sprintf("http://%s/metrics", Options.InventoryHost)

	cmd := exec.Command("curl", "-s", url)
	output, err := cmd.Output()
	Expect(err).NotTo(HaveOccurred())

	metrics := strings.Split(string(output), "\n")
	filteredMetrics := filterMetrics(metrics, string(clusterID), hostValidationMetric, string(validationID))
	Expect(len(filteredMetrics)).To(Equal(1))

	counter, err := strconv.Atoi(strings.ReplaceAll((strings.Split(filteredMetrics[0], "}")[1]), " ", ""))
	Expect(err).NotTo(HaveOccurred())
	Expect(counter).To(Equal(expectedCounter))
}

func metricsDeregisterCluster(ctx context.Context, clusterID strfmt.UUID) {

	_, err := userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("Metrics tests", func() {

	var (
		ctx       context.Context = context.Background()
		clusterID strfmt.UUID
	)

	BeforeEach(func() {
		cluster, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-metrics-cluster"),
				OpenshiftVersion: swag.String(common.DefaultTestOpenShiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterID = *cluster.GetPayload().ID
	})

	AfterEach(func() {
		clearDB()
	})

	Context("Host validation metrics", func() {

		It("has-min-hw-capacity", func() {

			host := &registerHost(clusterID).Host

			var lowHwInfo models.Inventory
			err := copier.CopyWithOption(&lowHwInfo, validHwInfo, copier.Option{DeepCopy: true})
			Expect(err).NotTo(HaveOccurred())

			lowHwInfo.CPU.Count = 1
			lowHwInfo.Memory.PhysicalBytes = int64(4 * units.GiB)

			generateHWPostStepReply(ctx, host, &lowHwInfo, "master-0")

			waitFunc := func() (bool, error) {
				cpuCond, _ := isHostValidationInFailedStatus(clusterID, *host.ID, models.HostValidationIDHasMinCPUCores)
				memoryCond, _ := isHostValidationInFailedStatus(clusterID, *host.ID, models.HostValidationIDHasMinMemory)
				return cpuCond && memoryCond, nil
			}
			err = wait.Poll(time.Millisecond, 10*time.Second, waitFunc)
			Expect(err).NotTo(HaveOccurred())

			metricsDeregisterCluster(ctx, clusterID)
			assertValidationCounter(clusterID, models.HostValidationIDHasMinCPUCores, 1)
			assertValidationCounter(clusterID, models.HostValidationIDHasMinMemory, 1)
		})

		It("has-min-hw-capacity-for-role", func() {

			host := &registerHost(clusterID).Host

			var lowHwInfo models.Inventory
			err := copier.CopyWithOption(&lowHwInfo, validHwInfo, copier.Option{DeepCopy: true})
			Expect(err).NotTo(HaveOccurred())

			lowHwInfo.CPU.Count = 1
			lowHwInfo.Memory.PhysicalBytes = int64(4 * units.GiB)

			generateHWPostStepReply(ctx, host, &lowHwInfo, "master-0")

			waitFunc := func() (bool, error) {
				cpuCond, _ := isHostValidationInFailedStatus(clusterID, *host.ID, models.HostValidationIDHasCPUCoresForRole)
				memoryCond, _ := isHostValidationInFailedStatus(clusterID, *host.ID, models.HostValidationIDHasMemoryForRole)
				return cpuCond && memoryCond, nil
			}
			err = wait.Poll(time.Millisecond, 10*time.Second, waitFunc)
			Expect(err).NotTo(HaveOccurred())

			metricsDeregisterCluster(ctx, clusterID)
			assertValidationCounter(clusterID, models.HostValidationIDHasCPUCoresForRole, 1)
			assertValidationCounter(clusterID, models.HostValidationIDHasMemoryForRole, 1)
		})

		It("hostname-unique", func() {

			host1 := &registerHost(clusterID).Host
			generateHWPostStepReply(ctx, host1, validHwInfo, "nonUniqName")

			host2 := &registerHost(clusterID).Host
			generateHWPostStepReply(ctx, host2, validHwInfo, "nonUniqName")

			waitFunc := func() (bool, error) {
				host1Cond, _ := isHostValidationInFailedStatus(clusterID, *host1.ID, models.HostValidationIDHostnameUnique)
				host2Cond, _ := isHostValidationInFailedStatus(clusterID, *host2.ID, models.HostValidationIDHostnameUnique)
				return host1Cond && host2Cond, nil
			}
			err := wait.Poll(time.Millisecond, 10*time.Second, waitFunc)
			Expect(err).NotTo(HaveOccurred())

			metricsDeregisterCluster(ctx, clusterID)
			assertValidationCounter(clusterID, models.HostValidationIDHostnameUnique, 2)
		})

		It("hostname-valid", func() {

			// 'localhost' is a forbidden host name
			host := &registerHost(clusterID).Host
			generateHWPostStepReply(ctx, host, validHwInfo, "localhost")

			waitFunc := func() (bool, error) {
				return isHostValidationInFailedStatus(clusterID, *host.ID, models.HostValidationIDHostnameValid)
			}
			err := wait.Poll(time.Millisecond, 10*time.Second, waitFunc)
			Expect(err).NotTo(HaveOccurred())

			metricsDeregisterCluster(ctx, clusterID)
			assertValidationCounter(clusterID, models.HostValidationIDHostnameValid, 1)
		})

		It("valid-platform", func() {

			host := &registerHost(clusterID).Host

			var nonValidPlatformVendorInfo models.Inventory
			err := copier.CopyWithOption(&nonValidPlatformVendorInfo, validHwInfo, copier.Option{DeepCopy: true})
			Expect(err).NotTo(HaveOccurred())

			nonValidPlatformVendorInfo.SystemVendor.ProductName = "OpenStack Compute"

			generateHWPostStepReply(ctx, host, &nonValidPlatformVendorInfo, "master-0")

			waitFunc := func() (bool, error) {
				return isHostValidationInFailedStatus(clusterID, *host.ID, models.HostValidationIDValidPlatform)
			}
			err = wait.Poll(time.Millisecond, 10*time.Second, waitFunc)
			Expect(err).NotTo(HaveOccurred())

			metricsDeregisterCluster(ctx, clusterID)
			assertValidationCounter(clusterID, models.HostValidationIDValidPlatform, 1)
		})

		It("ntp-synced", func() {

			host := &registerHost(clusterID).Host

			waitFunc := func() (bool, error) {
				return isHostValidationInFailedStatus(clusterID, *host.ID, models.HostValidationIDNtpSynced)
			}
			err := wait.Poll(time.Millisecond, 10*time.Second, waitFunc)
			Expect(err).NotTo(HaveOccurred())

			metricsDeregisterCluster(ctx, clusterID)
			assertValidationCounter(clusterID, models.HostValidationIDNtpSynced, 1)
		})
	})
})
