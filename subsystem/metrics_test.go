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
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/events"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	hostValidationFailedMetric  = "assisted_installer_host_validation_is_in_failed_status_on_cluster_deletion"
	hostValidationChangedMetric = "assisted_installer_host_validation_failed_after_success_before_installation"
)

type hostValidationResult struct {
	ID      models.HostValidationID `json:"id"`
	Status  string                  `json:"status"`
	Message string                  `json:"message"`
}

func isHostValidationInStatus(clusterID, hostID strfmt.UUID, validationID models.HostValidationID, expectedStatus string) (bool, error) {
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
			return v.Status == expectedStatus, nil
		}
	}
	return false, nil
}

func waitForHostValidationStatus(clusterID, hostID strfmt.UUID, expectedStatus string, hostValidationIDs ...models.HostValidationID) {

	waitFunc := func() (bool, error) {
		for _, vID := range hostValidationIDs {
			cond, _ := isHostValidationInStatus(clusterID, hostID, vID, expectedStatus)
			if !cond {
				return false, nil
			}
		}
		return true, nil
	}
	err := wait.Poll(time.Millisecond, 10*time.Second, waitFunc)
	Expect(err).NotTo(HaveOccurred())
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

func assertValidationMetricCounter(clusterID strfmt.UUID, validationID models.HostValidationID, expectedMetric string, expectedCounter int) {

	url := fmt.Sprintf("http://%s/metrics", Options.InventoryHost)

	cmd := exec.Command("curl", "-s", url)
	output, err := cmd.Output()
	Expect(err).NotTo(HaveOccurred())

	metrics := strings.Split(string(output), "\n")
	filteredMetrics := filterMetrics(metrics, string(clusterID), expectedMetric, string(validationID))
	Expect(len(filteredMetrics)).To(Equal(1))

	counter, err := strconv.Atoi(strings.ReplaceAll((strings.Split(filteredMetrics[0], "}")[1]), " ", ""))
	Expect(err).NotTo(HaveOccurred())
	Expect(counter).To(Equal(expectedCounter))
}

func assertValidationEvent(ctx context.Context, clusterID strfmt.UUID, hostName string, validationID models.HostValidationID, isFailure bool) {

	eventsReply, err := userBMClient.Events.ListEvents(ctx, &events.ListEventsParams{
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())

	var eventExist bool
	var eventMsg string
	if isFailure {
		eventMsg = fmt.Sprintf("Host %v: validation '%v' that used to succeed is now failing", hostName, validationID)
	} else {
		eventMsg = fmt.Sprintf("Host %v: validation '%v' is now fixed", hostName, validationID)
	}
	for _, ev := range eventsReply.Payload {
		if eventMsg == *ev.Message {
			eventExist = true
		}
	}
	Expect(eventExist).To(BeTrue())
}

func metricsDeregisterCluster(ctx context.Context, clusterID strfmt.UUID) {

	_, err := userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())
}

func generateValidInventory() string {

	inventory := models.Inventory{
		CPU:          &models.CPU{Count: 4},
		Memory:       &models.Memory{PhysicalBytes: int64(16 * units.GiB)},
		Disks:        []*models.Disk{{Name: "sda1", DriveType: "HDD", SizeBytes: validDiskSize}},
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Interfaces:   []*models.Interface{{IPV4Addresses: []string{"1.2.3.4/24"}}},
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

var _ = Describe("Metrics tests", func() {

	var (
		ctx                    context.Context = context.Background()
		clusterID              strfmt.UUID
		hostStatusInsufficient string = models.HostStatusInsufficient
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

		It("'has-min-hw-capacity' failed", func() {

			// create a validation success
			host := &registerHost(clusterID).Host
			err := db.Model(host).UpdateColumns(&models.Host{Inventory: generateValidInventory(), Status: &hostStatusInsufficient}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *host.ID, "success",
				models.HostValidationIDHasMinCPUCores,
				models.HostValidationIDHasMinMemory,
				models.HostValidationIDValidPlatform,
				models.HostValidationIDHasCPUCoresForRole,
				models.HostValidationIDHasMemoryForRole)

			// create a validation failure
			nonValidInventory := &models.Inventory{
				CPU:          &models.CPU{Count: 1},
				Memory:       &models.Memory{PhysicalBytes: int64(4 * units.GiB)},
				Disks:        []*models.Disk{{Name: "sda1", DriveType: "HDD", SizeBytes: validDiskSize}},
				SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "OpenStack Compute", SerialNumber: "3534"},
				Interfaces:   []*models.Interface{{IPV4Addresses: []string{"1.2.3.4/24"}}},
			}
			generateHWPostStepReply(ctx, host, nonValidInventory, "master-0")
			waitForHostValidationStatus(clusterID, *host.ID, "failure",
				models.HostValidationIDHasMinCPUCores,
				models.HostValidationIDHasMinMemory,
				models.HostValidationIDValidPlatform,
				models.HostValidationIDHasCPUCoresForRole,
				models.HostValidationIDHasMemoryForRole)

			// check generated events
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMinCPUCores, true)
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMinMemory, true)
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDValidPlatform, true)
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasCPUCoresForRole, true)
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMemoryForRole, true)

			// check generated metrics
			assertValidationMetricCounter(clusterID, models.HostValidationIDHasMinCPUCores, hostValidationChangedMetric, 1)
			assertValidationMetricCounter(clusterID, models.HostValidationIDHasMinMemory, hostValidationChangedMetric, 1)
			assertValidationMetricCounter(clusterID, models.HostValidationIDValidPlatform, hostValidationChangedMetric, 1)
			assertValidationMetricCounter(clusterID, models.HostValidationIDHasCPUCoresForRole, hostValidationChangedMetric, 1)
			assertValidationMetricCounter(clusterID, models.HostValidationIDHasMemoryForRole, hostValidationChangedMetric, 1)
			metricsDeregisterCluster(ctx, clusterID)
			assertValidationMetricCounter(clusterID, models.HostValidationIDHasMinCPUCores, hostValidationFailedMetric, 1)
			assertValidationMetricCounter(clusterID, models.HostValidationIDHasMinMemory, hostValidationFailedMetric, 1)
			assertValidationMetricCounter(clusterID, models.HostValidationIDValidPlatform, hostValidationFailedMetric, 1)
			assertValidationMetricCounter(clusterID, models.HostValidationIDHasCPUCoresForRole, hostValidationFailedMetric, 1)
			assertValidationMetricCounter(clusterID, models.HostValidationIDHasMemoryForRole, hostValidationFailedMetric, 1)

		})

		It("'has-min-hw-capacity' got fixed", func() {

			// create a validation failure
			host := &registerHost(clusterID).Host
			nonValidInventory := &models.Inventory{
				CPU:          &models.CPU{Count: 1},
				Memory:       &models.Memory{PhysicalBytes: int64(4 * units.GiB)},
				Disks:        []*models.Disk{{Name: "sda1", DriveType: "HDD", SizeBytes: validDiskSize}},
				SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "OpenStack Compute", SerialNumber: "3534"},
				Interfaces:   []*models.Interface{{IPV4Addresses: []string{"1.2.3.4/24"}}},
			}
			generateHWPostStepReply(ctx, host, nonValidInventory, "master-0")
			waitForHostValidationStatus(clusterID, *host.ID, "failure",
				models.HostValidationIDHasMinCPUCores,
				models.HostValidationIDHasMinMemory,
				models.HostValidationIDValidPlatform,
				models.HostValidationIDHasCPUCoresForRole,
				models.HostValidationIDHasMemoryForRole)

			// create a validation success
			generateHWPostStepReply(ctx, host, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *host.ID, "success",
				models.HostValidationIDHasMinCPUCores,
				models.HostValidationIDHasMinMemory,
				models.HostValidationIDValidPlatform,
				models.HostValidationIDHasCPUCoresForRole,
				models.HostValidationIDHasMemoryForRole)

			// check generated events
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMinCPUCores, false)
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMinMemory, false)
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDValidPlatform, false)
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasCPUCoresForRole, false)
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMemoryForRole, false)
		})

		It("'hostname-unique' failed", func() {

			// create a validation success
			host1 := &registerHost(clusterID).Host
			host2 := &registerHost(clusterID).Host
			generateHWPostStepReply(ctx, host1, validHwInfo, "master-0")
			generateHWPostStepReply(ctx, host2, validHwInfo, "master-1")
			waitForHostValidationStatus(clusterID, *host1.ID, "success", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *host2.ID, "success", models.HostValidationIDHostnameUnique)

			// create a validation failure
			generateHWPostStepReply(ctx, host1, validHwInfo, "nonUniqName")
			generateHWPostStepReply(ctx, host2, validHwInfo, "nonUniqName")
			waitForHostValidationStatus(clusterID, *host1.ID, "failure", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *host2.ID, "failure", models.HostValidationIDHostnameUnique)

			// check generated events
			assertValidationEvent(ctx, clusterID, "nonUniqName", models.HostValidationIDHostnameUnique, true)
			assertValidationEvent(ctx, clusterID, "nonUniqName", models.HostValidationIDHostnameUnique, true)

			// check generated metrics
			assertValidationMetricCounter(clusterID, models.HostValidationIDHostnameUnique, hostValidationChangedMetric, 2)
			metricsDeregisterCluster(ctx, clusterID)
			assertValidationMetricCounter(clusterID, models.HostValidationIDHostnameUnique, hostValidationFailedMetric, 2)
		})

		It("'hostname-unique' got fixed", func() {

			// create a validation failure
			host1 := &registerHost(clusterID).Host
			host2 := &registerHost(clusterID).Host
			generateHWPostStepReply(ctx, host1, validHwInfo, "master-0")
			generateHWPostStepReply(ctx, host2, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *host1.ID, "failure", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *host2.ID, "failure", models.HostValidationIDHostnameUnique)

			// create a validation success
			generateHWPostStepReply(ctx, host2, validHwInfo, "master-1")
			waitForHostValidationStatus(clusterID, *host1.ID, "success", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *host2.ID, "success", models.HostValidationIDHostnameUnique)

			// check generated events
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHostnameUnique, false)
			assertValidationEvent(ctx, clusterID, "master-1", models.HostValidationIDHostnameUnique, false)
		})

		It("'hostname-valid' failed", func() {

			// create a validation success
			host := &registerHost(clusterID).Host
			generateHWPostStepReply(ctx, host, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *host.ID, "success", models.HostValidationIDHostnameValid)

			// create a validation failure
			// 'localhost' is a forbidden host name
			generateHWPostStepReply(ctx, host, validHwInfo, "localhost")
			waitForHostValidationStatus(clusterID, *host.ID, "failure", models.HostValidationIDHostnameValid)

			// check generated events
			assertValidationEvent(ctx, clusterID, "localhost", models.HostValidationIDHostnameValid, true)

			// check generated metrics
			assertValidationMetricCounter(clusterID, models.HostValidationIDHostnameValid, hostValidationChangedMetric, 1)
			metricsDeregisterCluster(ctx, clusterID)
			assertValidationMetricCounter(clusterID, models.HostValidationIDHostnameValid, hostValidationFailedMetric, 1)
		})

		It("'hostname-valid' got fixed", func() {

			// create a validation failure
			host := &registerHost(clusterID).Host
			// 'localhost' is a forbidden host name
			generateHWPostStepReply(ctx, host, validHwInfo, "localhost")
			waitForHostValidationStatus(clusterID, *host.ID, "failure", models.HostValidationIDHostnameValid)

			// create a validation success
			generateHWPostStepReply(ctx, host, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *host.ID, "success", models.HostValidationIDHostnameValid)

			// check generated events
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHostnameValid, false)
		})

		It("'ntp-synced' failed", func() {

			// create a validation success
			host := &registerHost(clusterID).Host
			generateNTPPostStepReply(ctx, host, validNtpSources)
			waitForHostValidationStatus(clusterID, *host.ID, "success", models.HostValidationIDNtpSynced)

			// create a validation failure
			generateNTPPostStepReply(ctx, host, nil)
			waitForHostValidationStatus(clusterID, *host.ID, "failure", models.HostValidationIDNtpSynced)

			// check generated events
			assertValidationEvent(ctx, clusterID, string(*host.ID), models.HostValidationIDNtpSynced, true)

			// check generated metrics
			assertValidationMetricCounter(clusterID, models.HostValidationIDNtpSynced, hostValidationChangedMetric, 1)
			metricsDeregisterCluster(ctx, clusterID)
			assertValidationMetricCounter(clusterID, models.HostValidationIDNtpSynced, hostValidationFailedMetric, 1)
		})

		It("'ntp-synced' got fixed", func() {

			// create a validation failure
			host := &registerHost(clusterID).Host
			generateNTPPostStepReply(ctx, host, nil)
			waitForHostValidationStatus(clusterID, *host.ID, "failure", models.HostValidationIDNtpSynced)

			// create a validation success
			generateNTPPostStepReply(ctx, host, validNtpSources)
			waitForHostValidationStatus(clusterID, *host.ID, "success", models.HostValidationIDNtpSynced)

			// check generated events
			assertValidationEvent(ctx, clusterID, string(*host.ID), models.HostValidationIDNtpSynced, false)
		})
	})
})
