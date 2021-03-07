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
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/events"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host"
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
	err := wait.Poll(time.Millisecond, 30*time.Second, waitFunc)
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

func getValidationMetricCounter(validationID models.HostValidationID, expectedMetric string) int {

	url := fmt.Sprintf("http://%s/metrics", Options.InventoryHost)

	cmd := exec.Command("curl", "-s", url)
	output, err := cmd.Output()
	Expect(err).NotTo(HaveOccurred())

	metrics := strings.Split(string(output), "\n")
	filteredMetrics := filterMetrics(metrics, expectedMetric, string(validationID))
	Expect(len(filteredMetrics)).To(Equal(1))

	counter, err := strconv.Atoi(strings.ReplaceAll((strings.Split(filteredMetrics[0], "}")[1]), " ", ""))
	Expect(err).NotTo(HaveOccurred())
	return counter
}

//TODO Yoni will fix it
//func assertValidationMetricCounter(validationID models.HostValidationID, expectedMetric string, expectedCounter int) {
//
//	counter := getValidationMetricCounter(validationID, expectedMetric)
//	Expect(counter).To(Equal(expectedCounter))
//}

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

func assertNoValidationEvent(ctx context.Context, clusterID strfmt.UUID, hostName string, validationID models.HostValidationID) {

	eventsReply, err := userBMClient.Events.ListEvents(ctx, &events.ListEventsParams{
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())

	var eventExist bool
	eventMsg := fmt.Sprintf("Host %v: validation '%v' that used to succeed is now failing", hostName, validationID)
	for _, ev := range eventsReply.Payload {
		if eventMsg == *ev.Message {
			eventExist = true
		}
	}
	Expect(eventExist).To(BeFalse())
}

func registerDay2Cluster(ctx context.Context) strfmt.UUID {

	c, err := userBMClient.Installer.RegisterAddHostsCluster(ctx, &installer.RegisterAddHostsClusterParams{
		NewAddHostsClusterParams: &models.AddHostsClusterCreateParams{
			Name:             swag.String("test-metrics-day2-cluster"),
			OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
			APIVipDnsname:    swag.String("api_vip_dnsname"),
			ID:               strToUUID(uuid.New().String()),
		},
	})
	Expect(err).NotTo(HaveOccurred())
	clusterID := *c.GetPayload().ID

	_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
		ClusterUpdateParams: &models.ClusterUpdateParams{
			PullSecret: swag.String(pullSecret),
		},
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())

	return clusterID
}

func metricsDeregisterCluster(ctx context.Context, clusterID strfmt.UUID) {

	_, err := userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())
}

func generateValidInventory() string {
	return generateValidInventoryWithInterface("1.2.3.4/24")
}

func generateValidInventoryWithInterface(networkInterface string) string {

	inventory := models.Inventory{
		CPU:          &models.CPU{Count: 4},
		Memory:       &models.Memory{PhysicalBytes: int64(16 * units.GiB)},
		Disks:        []*models.Disk{{Name: "sda1", DriveType: "HDD", SizeBytes: validDiskSize}},
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Interfaces:   []*models.Interface{{IPV4Addresses: []string{networkInterface}}},
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
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
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

		It("'connected' failed before reboot", func() {

			// create a validation success
			h := &registerHost(clusterID).Host
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDConnected)

			// create a validation failure
			checkedInAt := time.Now().Add(-host.MaxHostDisconnectionTime)
			err := db.Model(h).UpdateColumns(&models.Host{CheckedInAt: strfmt.DateTime(checkedInAt)}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDConnected)

			// check generated events
			assertValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDConnected, true)

			// check generated metrics
			//TODO Yoni will fix it
			//assertValidationMetricCounter(models.HostValidationIDConnected, hostValidationChangedMetric, 1)
			metricsDeregisterCluster(ctx, clusterID)
			//assertValidationMetricCounter(models.HostValidationIDConnected, hostValidationFailedMetric, 1)
		})

		It("'connected' failed after reboot", func() {

			// create a validation success
			h := &registerHost(clusterID).Host
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDConnected)

			// create a validation failure
			checkedInAt := time.Now().Add(-host.MaxHostDisconnectionTime)
			err := db.Model(h).UpdateColumns(&models.Host{
				CheckedInAt: strfmt.DateTime(checkedInAt),
				Progress: &models.HostProgressInfo{
					CurrentStage: models.HostStageRebooting,
				},
			}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDConnected)

			// check no generated events
			assertNoValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDConnected)
		})

		It("'connected' got fixed", func() {

			// create a validation failure
			h := &registerHost(clusterID).Host
			checkedInAt := time.Now().Add(-host.MaxHostDisconnectionTime)
			err := db.Model(h).UpdateColumns(&models.Host{CheckedInAt: strfmt.DateTime(checkedInAt)}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDConnected)

			// create a validation success
			err = db.Model(h).UpdateColumns(&models.Host{CheckedInAt: strfmt.DateTime(time.Now())}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDConnected)

			// check generated events
			assertValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDConnected, false)
		})

		It("'has-inventory' failed", func() {

			// Inventory is sent to service or not, there is no usecase in which the service hold an inventroy
			// for the host and at a later time loose it, therefore this case isn't tested and we directly
			// test the validation failure

			// create a validation failure
			h := &registerHost(clusterID).Host
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDHasInventory)

			// check generated metrics
			metricsDeregisterCluster(ctx, clusterID)
			//TODO Yoni will fix it
			//assertValidationMetricCounter(models.HostValidationIDHasInventory, hostValidationFailedMetric, 1)
		})

		It("'has-inventory' got fixed", func() {

			// create a validation failure
			h := &registerHost(clusterID).Host
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDHasInventory)

			// create a validation success
			generateHWPostStepReply(ctx, h, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDHasInventory)

			// check generated events
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasInventory, false)
		})

		It("'has-min-hw-capacity' failed", func() {

			// create a validation success
			h := &registerHost(clusterID).Host
			err := db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventory(), Status: &hostStatusInsufficient}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *h.ID, "success",
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
			generateHWPostStepReply(ctx, h, nonValidInventory, "master-0")
			waitForHostValidationStatus(clusterID, *h.ID, "failure",
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
			//TODO Yoni will fix it
			//assertValidationMetricCounter(models.HostValidationIDHasMinCPUCores, hostValidationChangedMetric, 1)
			//assertValidationMetricCounter(models.HostValidationIDHasMinMemory, hostValidationChangedMetric, 1)
			//assertValidationMetricCounter(models.HostValidationIDValidPlatform, hostValidationChangedMetric, 1)
			//assertValidationMetricCounter(models.HostValidationIDHasCPUCoresForRole, hostValidationChangedMetric, 1)
			//assertValidationMetricCounter(models.HostValidationIDHasMemoryForRole, hostValidationChangedMetric, 1)
			metricsDeregisterCluster(ctx, clusterID)
			//assertValidationMetricCounter(models.HostValidationIDHasMinCPUCores, hostValidationFailedMetric, 1)
			//assertValidationMetricCounter(models.HostValidationIDHasMinMemory, hostValidationFailedMetric, 1)
			//assertValidationMetricCounter(models.HostValidationIDValidPlatform, hostValidationFailedMetric, 1)
			//assertValidationMetricCounter(models.HostValidationIDHasCPUCoresForRole, hostValidationFailedMetric, 1)
			//assertValidationMetricCounter(models.HostValidationIDHasMemoryForRole, hostValidationFailedMetric, 1)

		})

		It("'has-min-hw-capacity' got fixed", func() {

			// create a validation failure
			h := &registerHost(clusterID).Host
			nonValidInventory := &models.Inventory{
				CPU:          &models.CPU{Count: 1},
				Memory:       &models.Memory{PhysicalBytes: int64(4 * units.GiB)},
				Disks:        []*models.Disk{{Name: "sda1", DriveType: "HDD", SizeBytes: validDiskSize}},
				SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "OpenStack Compute", SerialNumber: "3534"},
				Interfaces:   []*models.Interface{{IPV4Addresses: []string{"1.2.3.4/24"}}},
			}
			generateHWPostStepReply(ctx, h, nonValidInventory, "master-0")
			waitForHostValidationStatus(clusterID, *h.ID, "failure",
				models.HostValidationIDHasMinCPUCores,
				models.HostValidationIDHasMinMemory,
				models.HostValidationIDValidPlatform,
				models.HostValidationIDHasCPUCoresForRole,
				models.HostValidationIDHasMemoryForRole)

			// create a validation success
			generateHWPostStepReply(ctx, h, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *h.ID, "success",
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

		It("'machine-cidr-defined' failed", func() {

			// MachineCidr is sent to service or not, there is no usecase in which the service hold a MachineCidr
			// for the host and at a later time loose it, therefore this case isn't tested and we directly
			// test the validation failure

			// create a validation failure
			h := &registerHost(clusterID).Host
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDMachineCidrDefined)

			// check generated metrics
			metricsDeregisterCluster(ctx, clusterID)
			//TODO Yoni will fix it
			//assertValidationMetricCounter(models.HostValidationIDMachineCidrDefined, hostValidationFailedMetric, 1)
		})

		It("'machine-cidr-defined' got fixed", func() {

			// create a validation failure
			h := &registerHost(clusterID).Host
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDMachineCidrDefined)

			// create a validation success
			generateHWPostStepReply(ctx, h, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDMachineCidrDefined)

			// check generated events
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDMachineCidrDefined, false)
		})

		It("'hostname-unique' failed", func() {

			// create a validation success
			h1 := &registerHost(clusterID).Host
			h2 := &registerHost(clusterID).Host
			generateHWPostStepReply(ctx, h1, validHwInfo, "master-0")
			generateHWPostStepReply(ctx, h2, validHwInfo, "master-1")
			waitForHostValidationStatus(clusterID, *h1.ID, "success", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *h2.ID, "success", models.HostValidationIDHostnameUnique)

			// create a validation failure
			generateHWPostStepReply(ctx, h1, validHwInfo, "nonUniqName")
			generateHWPostStepReply(ctx, h2, validHwInfo, "nonUniqName")
			waitForHostValidationStatus(clusterID, *h1.ID, "failure", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *h2.ID, "failure", models.HostValidationIDHostnameUnique)

			// check generated events
			assertValidationEvent(ctx, clusterID, "nonUniqName", models.HostValidationIDHostnameUnique, true)
			assertValidationEvent(ctx, clusterID, "nonUniqName", models.HostValidationIDHostnameUnique, true)

			// check generated metrics
			//TODO Yoni will fix it
			//assertValidationMetricCounter(models.HostValidationIDHostnameUnique, hostValidationChangedMetric, 2)
			metricsDeregisterCluster(ctx, clusterID)
			//assertValidationMetricCounter(models.HostValidationIDHostnameUnique, hostValidationFailedMetric, 2)
		})

		It("'hostname-unique' got fixed", func() {

			// create a validation failure
			h1 := &registerHost(clusterID).Host
			h2 := &registerHost(clusterID).Host
			generateHWPostStepReply(ctx, h1, validHwInfo, "master-0")
			generateHWPostStepReply(ctx, h2, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *h1.ID, "failure", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *h2.ID, "failure", models.HostValidationIDHostnameUnique)

			// create a validation success
			generateHWPostStepReply(ctx, h2, validHwInfo, "master-1")
			waitForHostValidationStatus(clusterID, *h1.ID, "success", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *h2.ID, "success", models.HostValidationIDHostnameUnique)

			// check generated events
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHostnameUnique, false)
			assertValidationEvent(ctx, clusterID, "master-1", models.HostValidationIDHostnameUnique, false)
		})

		It("'hostname-valid' failed", func() {

			// create a validation success
			h := &registerHost(clusterID).Host
			generateHWPostStepReply(ctx, h, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDHostnameValid)

			// create a validation failure
			// 'localhost' is a forbidden host name
			generateHWPostStepReply(ctx, h, validHwInfo, "localhost")
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDHostnameValid)

			// check generated events
			assertValidationEvent(ctx, clusterID, "localhost", models.HostValidationIDHostnameValid, true)

			// check generated metrics
			//TODO Yoni will fix it
			//assertValidationMetricCounter(models.HostValidationIDHostnameValid, hostValidationChangedMetric, 1)
			metricsDeregisterCluster(ctx, clusterID)
			//assertValidationMetricCounter(models.HostValidationIDHostnameValid, hostValidationFailedMetric, 1)
		})

		It("'hostname-valid' got fixed", func() {

			// create a validation failure
			h := &registerHost(clusterID).Host
			// 'localhost' is a forbidden host name
			generateHWPostStepReply(ctx, h, validHwInfo, "localhost")
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDHostnameValid)

			// create a validation success
			generateHWPostStepReply(ctx, h, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDHostnameValid)

			// check generated events
			assertValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHostnameValid, false)
		})

		It("'belongs-to-machine-cidr' failed", func() {

			// create a validation success
			h := &registerHost(clusterID).Host
			err := db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventoryWithInterface("1.2.3.4/24")}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDBelongsToMachineCidr)

			// create a validation failure
			err = db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventoryWithInterface("1.2.2.2/24")}).Error
			Expect(err).NotTo(HaveOccurred())
			// machine-cidr doesn't change after it is set
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDBelongsToMachineCidr)

			// check generated events
			assertValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDBelongsToMachineCidr, true)

			// check generated metrics
			//TODO Yoni will fix it
			//assertValidationMetricCounter(models.HostValidationIDBelongsToMachineCidr, hostValidationChangedMetric, 1)
			metricsDeregisterCluster(ctx, clusterID)
			//assertValidationMetricCounter(models.HostValidationIDBelongsToMachineCidr, hostValidationFailedMetric, 1)
		})

		It("'belongs-to-machine-cidr' got fixed", func() {

			// create a validation failure
			h := &registerHost(clusterID).Host
			err := db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventoryWithInterface("1.2.3.4/24")}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDBelongsToMachineCidr)
			err = db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventoryWithInterface("1.2.2.2/24")}).Error
			Expect(err).NotTo(HaveOccurred())
			// machine-cidr doesn't change after it is set
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDBelongsToMachineCidr)

			// create a validation success
			err = db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventoryWithInterface("1.2.3.4/24")}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDBelongsToMachineCidr)

			// check generated events
			assertValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDBelongsToMachineCidr, false)
		})

		It("'api-vip-connected' failed", func() {

			day2ClusterID := registerDay2Cluster(ctx)

			// create a validation success
			h := registerNode(ctx, day2ClusterID, "master-0")
			generateApiVipPostStepReply(ctx, h, true)
			waitForHostValidationStatus(day2ClusterID, *h.ID, "success", models.HostValidationIDAPIVipConnected)

			// create a validation failure
			generateApiVipPostStepReply(ctx, h, false)
			waitForHostValidationStatus(day2ClusterID, *h.ID, "failure", models.HostValidationIDAPIVipConnected)

			// check generated events
			assertValidationEvent(ctx, day2ClusterID, "master-0", models.HostValidationIDAPIVipConnected, true)

			// check generated metrics
			//TODO Yoni will fix it
			//assertValidationMetricCounter(models.HostValidationIDAPIVipConnected, hostValidationChangedMetric, 1)
			metricsDeregisterCluster(ctx, day2ClusterID)
			//assertValidationMetricCounter(models.HostValidationIDAPIVipConnected, hostValidationFailedMetric, 1)
		})

		It("'api-vip-connected' got fixed", func() {

			day2ClusterID := registerDay2Cluster(ctx)

			// create a validation failure
			h := registerNode(ctx, day2ClusterID, "master-0")
			generateApiVipPostStepReply(ctx, h, false)
			waitForHostValidationStatus(day2ClusterID, *h.ID, "failure", models.HostValidationIDAPIVipConnected)

			// create a validation success
			generateApiVipPostStepReply(ctx, h, true)
			waitForHostValidationStatus(day2ClusterID, *h.ID, "success", models.HostValidationIDAPIVipConnected)

			// check generated events
			assertValidationEvent(ctx, day2ClusterID, "master-0", models.HostValidationIDAPIVipConnected, false)
		})

		It("'belongs-to-majority-group' failed", func() {

			// create a validation success
			h1 := registerNode(ctx, clusterID, "h1")
			h2 := registerNode(ctx, clusterID, "h2")
			h3 := registerNode(ctx, clusterID, "h3")
			h4 := registerNode(ctx, clusterID, "h4")
			generateFullMeshConnectivity(ctx, "1.2.3.10", h1, h2, h3, h4)
			waitForHostValidationStatus(clusterID, *h1.ID, "success", models.HostValidationIDBelongsToMajorityGroup)

			// create a validation failure
			generateFullMeshConnectivity(ctx, "1.2.3.10", h2, h3, h4)
			waitForHostValidationStatus(clusterID, *h1.ID, "failure", models.HostValidationIDBelongsToMajorityGroup)

			// check generated events
			assertValidationEvent(ctx, clusterID, "h1", models.HostValidationIDBelongsToMajorityGroup, true)

			// check generated metrics

			// this specific case can create a short timeframe in which another host is failing on that validation and will
			// be later fixed by the next refresh status cycle because generating a full mesh connectivity isn't an atomic
			// action, therefore, in this test we will check that at least the expected failing host is failing but not fail
			// the test if other hosts fails as well.
			metricCounter := getValidationMetricCounter(models.HostValidationIDBelongsToMajorityGroup, hostValidationChangedMetric)
			Expect(metricCounter >= 1).To(BeTrue())
			metricsDeregisterCluster(ctx, clusterID)
			metricCounter = getValidationMetricCounter(models.HostValidationIDBelongsToMajorityGroup, hostValidationFailedMetric)
			Expect(metricCounter >= 1).To(BeTrue())
		})

		It("'belongs-to-majority-group' got fixed", func() {

			// create a validation failure
			h1 := registerNode(ctx, clusterID, "h1")
			h2 := registerNode(ctx, clusterID, "h2")
			h3 := registerNode(ctx, clusterID, "h3")
			h4 := registerNode(ctx, clusterID, "h4")
			generateFullMeshConnectivity(ctx, "1.2.3.10", h2, h3, h4)
			waitForHostValidationStatus(clusterID, *h1.ID, "failure", models.HostValidationIDBelongsToMajorityGroup)

			// create a validation success
			generateFullMeshConnectivity(ctx, "1.2.3.10", h1, h2, h3, h4)
			waitForHostValidationStatus(clusterID, *h1.ID, "success", models.HostValidationIDBelongsToMajorityGroup)

			// check generated events
			assertValidationEvent(ctx, clusterID, "h1", models.HostValidationIDBelongsToMajorityGroup, false)
		})

		It("'ntp-synced' failed", func() {

			// create a validation success
			h := &registerHost(clusterID).Host
			generateNTPPostStepReply(ctx, h, []*models.NtpSource{common.TestNTPSourceSynced})
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDNtpSynced)

			// create a validation failure
			generateNTPPostStepReply(ctx, h, nil)
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDNtpSynced)

			// check generated events
			assertValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDNtpSynced, true)

			// check generated metrics
			//TODO Yoni will fix it
			//assertValidationMetricCounter(models.HostValidationIDNtpSynced, hostValidationChangedMetric, 1)
			metricsDeregisterCluster(ctx, clusterID)
			//assertValidationMetricCounter(models.HostValidationIDNtpSynced, hostValidationFailedMetric, 1)
		})

		It("'ntp-synced' got fixed", func() {

			// create a validation failure
			h := &registerHost(clusterID).Host
			generateNTPPostStepReply(ctx, h, nil)
			waitForHostValidationStatus(clusterID, *h.ID, "failure", models.HostValidationIDNtpSynced)

			// create a validation success
			generateNTPPostStepReply(ctx, h, []*models.NtpSource{common.TestNTPSourceSynced})
			waitForHostValidationStatus(clusterID, *h.ID, "success", models.HostValidationIDNtpSynced)

			// check generated events
			assertValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDNtpSynced, false)
		})
	})
})
