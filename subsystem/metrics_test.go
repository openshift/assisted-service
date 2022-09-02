package subsystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
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
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	hostValidationFailedMetric     = "assisted_installer_host_validation_is_in_failed_status_on_cluster_deletion"
	hostValidationChangedMetric    = "assisted_installer_host_validation_failed_after_success_before_installation"
	clusterValidationFailedMetric  = "assisted_installer_cluster_validation_is_in_failed_status_on_cluster_deletion"
	clusterValidationChangedMetric = "assisted_installer_cluster_validation_failed_after_success_before_installation"
)

var (
	sda1 = models.Disk{
		ID:        "wwn-0x1111111111111111111111",
		ByID:      "wwn-0x1111111111111111111111",
		DriveType: "HDD",
		Name:      "sda1",
		SizeBytes: validDiskSize,
	}
)

type hostValidationResult struct {
	ID      models.HostValidationID `json:"id"`
	Status  string                  `json:"status"`
	Message string                  `json:"message"`
}

type clusterValidationResult struct {
	ID      models.ClusterValidationID `json:"id"`
	Status  string                     `json:"status"`
	Message string                     `json:"message"`
}

func isHostValidationInStatus(clusterID, infraEnvID, hostID strfmt.UUID, validationID models.HostValidationID, expectedStatus string) (bool, error) {
	var validationRes map[string][]hostValidationResult
	h := getHostV2(infraEnvID, hostID)
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

func isClusterValidationInStatus(clusterID strfmt.UUID, validationID models.ClusterValidationID, expectedStatus string) (bool, error) {
	var validationRes map[string][]clusterValidationResult
	c := getCluster(clusterID)
	if c.ValidationsInfo == "" {
		return false, nil
	}
	err := json.Unmarshal([]byte(c.ValidationsInfo), &validationRes)
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

func waitForHostValidationStatus(clusterID, infraEnvID, hostID strfmt.UUID, expectedStatus string, hostValidationIDs ...models.HostValidationID) {

	waitFunc := func() (bool, error) {
		for _, vID := range hostValidationIDs {
			cond, _ := isHostValidationInStatus(clusterID, infraEnvID, hostID, vID, expectedStatus)
			if !cond {
				return false, nil
			}
		}
		return true, nil
	}
	err := wait.Poll(pollDefaultInterval, pollDefaultTimeout, waitFunc)
	Expect(err).NotTo(HaveOccurred())
}

func waitForClusterValidationStatus(clusterID strfmt.UUID, expectedStatus string, clusterValidationIDs ...models.ClusterValidationID) {

	waitFunc := func() (bool, error) {
		for _, vID := range clusterValidationIDs {
			cond, _ := isClusterValidationInStatus(clusterID, vID, expectedStatus)
			if !cond {
				return false, nil
			}
		}
		return true, nil
	}
	err := wait.Poll(pollDefaultInterval, pollDefaultTimeout, waitFunc)
	Expect(err).NotTo(HaveOccurred())
}

func filterMetrics(metrics []string, substrings ...string) []string {
	var res []string
	for _, m := range metrics {
		// skip metrics description
		if strings.HasPrefix(m, "#") {
			continue
		}

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

func getMetricRecords() []string {
	url := fmt.Sprintf("http://%s/metrics", Options.InventoryHost)
	resp, err := http.Get(url)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	Expect(err).NotTo(HaveOccurred())
	return strings.Split(string(body), "\n")
}

func getValidationMetricCounter(validationID, expectedMetric string) int {
	metrics := getMetricRecords()
	filteredMetrics := filterMetrics(metrics, expectedMetric, fmt.Sprintf("ValidationType=\"%s\"", validationID))
	if len(filteredMetrics) == 0 {
		return 0
	}

	totalCounter := 0
	for _, metric := range filteredMetrics {
		metricCounter, err := strconv.Atoi(strings.ReplaceAll((strings.Split(metric, "}")[1]), " ", ""))
		Expect(err).NotTo(HaveOccurred())
		totalCounter += metricCounter
	}

	return totalCounter
}

func getMetricRecord(name string) (string, error) {
	metrics := getMetricRecords()
	filteredMetrics := filterMetrics(metrics, name)
	if len(filteredMetrics) == 0 {
		return "", errors.New("metric not found")
	}
	return filteredMetrics[0], nil
}

func getMetricEvents(ctx context.Context, clusterID strfmt.UUID) []*models.Event {
	eventsReply, err := userBMClient.Events.V2ListEvents(ctx, &events.V2ListEventsParams{
		ClusterID:  &clusterID,
		Categories: []string{"metrics"},
	})
	Expect(err).NotTo(HaveOccurred())
	return eventsReply.GetPayload()
}

func filterMetricEvents(in []*models.Event, hostID strfmt.UUID, message string) []*models.Event {
	events := make([]*models.Event, 0)
	for _, ev := range in {
		if ev.HostID != nil && ev.HostID.String() == hostID.String() && *ev.Message == message {
			events = append(events, ev)
		}
	}
	return events
}

func assertHostValidationEvent(ctx context.Context, clusterID strfmt.UUID, hostName string, validationID models.HostValidationID, isFailure bool) {

	eventsReply, err := userBMClient.Events.V2ListEvents(ctx, &events.V2ListEventsParams{
		ClusterID: &clusterID,
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

func assertClusterValidationEvent(ctx context.Context, clusterID strfmt.UUID, validationID models.ClusterValidationID, isFailure bool) {

	eventsReply, err := userBMClient.Events.V2ListEvents(ctx, &events.V2ListEventsParams{
		ClusterID: &clusterID,
	})
	Expect(err).NotTo(HaveOccurred())

	var eventExist bool
	var eventMsg string
	if isFailure {
		eventMsg = fmt.Sprintf("Cluster validation '%v' that used to succeed is now failing", validationID)
	} else {
		eventMsg = fmt.Sprintf("Cluster validation '%v' is now fixed", validationID)
	}
	for _, ev := range eventsReply.Payload {
		if eventMsg == *ev.Message {
			eventExist = true
		}
	}
	Expect(eventExist).To(BeTrue())
}

func assertNoValidationEvent(ctx context.Context, clusterID strfmt.UUID, hostName string, validationID models.HostValidationID) {

	eventsReply, err := userBMClient.Events.V2ListEvents(ctx, &events.V2ListEventsParams{
		ClusterID: &clusterID,
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

func v2RegisterDay2Cluster(ctx context.Context) strfmt.UUID {
	openshiftClusterID := strfmt.UUID(uuid.New().String())

	c, err := userBMClient.Installer.V2ImportCluster(ctx, &installer.V2ImportClusterParams{
		NewImportClusterParams: &models.ImportClusterParams{
			Name:               swag.String("test-metrics-day2-cluster"),
			OpenshiftVersion:   openshiftVersion,
			APIVipDnsname:      swag.String("api-vip.redhat.com"),
			OpenshiftClusterID: &openshiftClusterID,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	clusterID := *c.GetPayload().ID

	_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
		ClusterUpdateParams: &models.V2ClusterUpdateParams{
			PullSecret: swag.String(pullSecret),
		},
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())

	return clusterID
}

func metricsDeregisterCluster(ctx context.Context, clusterID strfmt.UUID) {

	_, err := userBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{
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
		Memory:       &models.Memory{PhysicalBytes: int64(16 * units.GiB), UsableBytes: int64(16 * units.GiB)},
		Disks:        []*models.Disk{{Name: "sda1", DriveType: "HDD", SizeBytes: validDiskSize}},
		SystemVendor: &models.SystemVendor{Manufacturer: "Red Hat", ProductName: "RHEL", SerialNumber: "3534"},
		Interfaces:   []*models.Interface{{IPV4Addresses: []string{networkInterface}}},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	b, err := json.Marshal(&inventory)
	Expect(err).To(Not(HaveOccurred()))
	return string(b)
}

var _ = Describe("Metrics tests", func() {

	var (
		ctx        context.Context = context.Background()
		clusterID  strfmt.UUID
		infraEnvID *strfmt.UUID
	)

	BeforeEach(func() {
		var err error
		clusterID, err = registerCluster(ctx, userBMClient, "test-cluster", pullSecret)
		Expect(err).NotTo(HaveOccurred())
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
	})

	Context("host metrics events", func() {
		var c *models.Cluster
		var bootstrap models.Host

		var toProps = func(str string) map[string]interface{} {
			props := make(map[string]interface{})
			Expect(json.Unmarshal([]byte(str), &props)).NotTo(HaveOccurred())
			return props
		}

		BeforeEach(func() {
			//start host installation process
			registerHostsAndSetRoles(clusterID, *infraEnvID, 3, "test-cluster", "example.com")
			c = installCluster(clusterID)
			for _, host := range c.Hosts {
				waitForHostState(ctx, "installing", defaultWaitForHostStateTimeout, host)
				if host.Bootstrap {
					bootstrap = *host
				}
			}
		})

		tests := []struct {
			name     string
			dstStage models.HostStage
		}{
			{
				name:     "host metrics on host stage done",
				dstStage: models.HostStageDone,
			},
			{
				name:     "host metrics on host stage failed",
				dstStage: models.HostStageFailed,
			},
		}
		for i := range tests {
			t := tests[i]
			It(t.name, func() {
				//move the bootstrap host to the desired state
				updateProgress(*bootstrap.ID, *infraEnvID, t.dstStage)

				//read metrics events
				evs := getMetricEvents(context.TODO(), clusterID)

				host_mem_cpu_evs := filterMetricEvents(evs, *bootstrap.ID, "host.mem.cpu")
				Expect(len(host_mem_cpu_evs)).To(Equal(1))
				host_mem_cpu_props := toProps(host_mem_cpu_evs[0].Props)
				Expect(host_mem_cpu_props["host_role"]).To(Equal("bootstrap"))
				Expect(host_mem_cpu_props["host_result"]).To(Equal(string(t.dstStage)))
				Expect(host_mem_cpu_props["core_count"]).NotTo(BeNil())
				Expect(host_mem_cpu_props["mem_bytes"]).NotTo(BeNil())

				disk_size_type_evs := filterMetricEvents(evs, *bootstrap.ID, "disk.size.type")
				Expect(len(disk_size_type_evs)).To(Equal(2))
				disk_size_type_props := toProps(disk_size_type_evs[0].Props)
				Expect(disk_size_type_props["host_role"]).To(Equal("bootstrap"))
				Expect(disk_size_type_props["host_result"]).To(Equal(string(t.dstStage)))
				Expect(disk_size_type_props["disk_size"]).NotTo(BeNil())
				Expect(disk_size_type_props["disk_type"]).NotTo(BeNil())

				nic_speed_evs := filterMetricEvents(evs, *bootstrap.ID, "nic.speed")
				Expect(len(nic_speed_evs)).To(Equal(1))
				nic_speed_props := toProps(nic_speed_evs[0].Props)
				Expect(nic_speed_props["host_role"]).To(Equal("bootstrap"))
				Expect(nic_speed_props["host_result"]).To(Equal(string(t.dstStage)))
				Expect(nic_speed_props["nic_speed"]).NotTo(BeNil())
			})
		}
	})

	Context("Host validation metrics", func() {

		var hostStatusInsufficient string = models.HostStatusInsufficient
		var MaxHostDisconnectionTime = 3 * time.Minute

		It("'connected' failed before reboot", func() {

			// create a validation success
			h := &registerHost(*infraEnvID).Host
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDConnected)

			oldChangedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDConnected), hostValidationChangedMetric)
			oldFailedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDConnected), hostValidationFailedMetric)

			// create a validation failure
			checkedInAt := time.Now().Add(-2 * MaxHostDisconnectionTime)
			err := db.Model(h).UpdateColumns(&models.Host{CheckedInAt: strfmt.DateTime(checkedInAt)}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDConnected)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDConnected, true)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.HostValidationIDConnected), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounter + 1))
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.HostValidationIDConnected), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'connected' failed after reboot", func() {

			// create a validation success
			h := &registerHost(*infraEnvID).Host
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDConnected)

			// create a validation failure
			checkedInAt := time.Now().Add(-2 * MaxHostDisconnectionTime)
			err := db.Model(h).UpdateColumns(&models.Host{
				CheckedInAt: strfmt.DateTime(checkedInAt),
				Progress: &models.HostProgressInfo{
					CurrentStage: models.HostStageRebooting,
				},
			}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDConnected)

			// check no generated events
			assertNoValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDConnected)
		})

		It("'connected' got fixed", func() {

			// create a validation failure
			h := &registerHost(*infraEnvID).Host
			checkedInAt := time.Now().Add(-2 * MaxHostDisconnectionTime)
			err := db.Model(h).UpdateColumns(&models.Host{CheckedInAt: strfmt.DateTime(checkedInAt)}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDConnected)

			// create a validation success
			err = db.Model(h).UpdateColumns(&models.Host{CheckedInAt: strfmt.DateTime(time.Now())}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDConnected)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDConnected, false)
		})

		It("'has-inventory' failed", func() {

			// Inventory is sent to service or not, there is no usecase in which the service hold an inventroy
			// for the host and at a later time loose it, therefore this case isn't tested and we directly
			// test the validation failure

			oldFailedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDHasInventory), hostValidationFailedMetric)

			// create a validation failure
			h := &registerHost(*infraEnvID).Host
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDHasInventory)

			// check generated metrics
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.HostValidationIDHasInventory), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'has-inventory' got fixed", func() {

			// create a validation failure
			h := &registerHost(*infraEnvID).Host
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDHasInventory)

			// create a validation success
			generateHWPostStepReply(ctx, h, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDHasInventory)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasInventory, false)
		})

		It("'has-min-hw-capacity' failed", func() {

			// create a validation success
			h := &registerHost(*infraEnvID).Host
			err := db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventory(), Status: &hostStatusInsufficient}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success",
				models.HostValidationIDHasMinCPUCores,
				models.HostValidationIDHasMinMemory,
				models.HostValidationIDValidPlatformNetworkSettings,
				models.HostValidationIDHasCPUCoresForRole,
				models.HostValidationIDHasMemoryForRole)

			oldChangedMetricCounterHasMinCPUCores := getValidationMetricCounter(string(models.HostValidationIDHasMinCPUCores), hostValidationChangedMetric)
			oldChangedMetricCounterHasMinMemory := getValidationMetricCounter(string(models.HostValidationIDHasMinMemory), hostValidationChangedMetric)
			oldChangedMetricCounterValidPlatform := getValidationMetricCounter(string(models.HostValidationIDValidPlatformNetworkSettings), hostValidationChangedMetric)
			oldChangedMetricCounterHasCPUCoresForRole := getValidationMetricCounter(string(models.HostValidationIDHasCPUCoresForRole), hostValidationChangedMetric)
			oldChangedMetricCounterHasMemoryForRole := getValidationMetricCounter(string(models.HostValidationIDHasMemoryForRole), hostValidationChangedMetric)

			oldFailedMetricCounterHasMinCPUCores := getValidationMetricCounter(string(models.HostValidationIDHasMinCPUCores), hostValidationFailedMetric)
			oldFailedMetricCounterHasMinMemroy := getValidationMetricCounter(string(models.HostValidationIDHasMinMemory), hostValidationFailedMetric)
			oldFailedMetricCounterValidPlatform := getValidationMetricCounter(string(models.HostValidationIDValidPlatformNetworkSettings), hostValidationFailedMetric)
			oldFailedMetricCounterHasCPUCoresForRole := getValidationMetricCounter(string(models.HostValidationIDHasCPUCoresForRole), hostValidationFailedMetric)
			oldFailedMetricCounterHasMemoryForRole := getValidationMetricCounter(string(models.HostValidationIDHasMemoryForRole), hostValidationFailedMetric)

			// create a validation failure
			nonValidInventory := &models.Inventory{
				CPU:          &models.CPU{Count: 1},
				Memory:       &models.Memory{PhysicalBytes: int64(4 * units.GiB), UsableBytes: int64(4 * units.GiB)},
				Disks:        []*models.Disk{&sda1},
				SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "OpenStack Compute", SerialNumber: "3534"},
				Interfaces:   []*models.Interface{{IPV4Addresses: []string{"1.2.3.4/24"}}},
				Routes:       common.TestDefaultRouteConfiguration,
			}
			generateHWPostStepReply(ctx, h, nonValidInventory, "master-0")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure",
				models.HostValidationIDHasMinCPUCores,
				models.HostValidationIDHasMinMemory,
				models.HostValidationIDValidPlatformNetworkSettings,
				models.HostValidationIDHasCPUCoresForRole,
				models.HostValidationIDHasMemoryForRole)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMinCPUCores, true)
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMinMemory, true)
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDValidPlatformNetworkSettings, true)
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasCPUCoresForRole, true)
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMemoryForRole, true)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.HostValidationIDHasMinCPUCores), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounterHasMinCPUCores + 1))
			Expect(getValidationMetricCounter(string(models.HostValidationIDHasMinMemory), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounterHasMinMemory + 1))
			Expect(getValidationMetricCounter(string(models.HostValidationIDValidPlatformNetworkSettings), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounterValidPlatform + 1))
			Expect(getValidationMetricCounter(string(models.HostValidationIDHasCPUCoresForRole), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounterHasCPUCoresForRole + 1))
			Expect(getValidationMetricCounter(string(models.HostValidationIDHasMemoryForRole), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounterHasMemoryForRole + 1))
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.HostValidationIDHasMinCPUCores), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounterHasMinCPUCores + 1))
			Expect(getValidationMetricCounter(string(models.HostValidationIDHasMinMemory), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounterHasMinMemroy + 1))
			Expect(getValidationMetricCounter(string(models.HostValidationIDValidPlatformNetworkSettings), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounterValidPlatform + 1))
			Expect(getValidationMetricCounter(string(models.HostValidationIDHasCPUCoresForRole), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounterHasCPUCoresForRole + 1))
			Expect(getValidationMetricCounter(string(models.HostValidationIDHasMemoryForRole), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounterHasMemoryForRole + 1))

		})

		It("'has-min-hw-capacity' got fixed", func() {

			// create a validation failure
			h := &registerHost(*infraEnvID).Host
			nonValidInventory := &models.Inventory{
				CPU:          &models.CPU{Count: 1},
				Memory:       &models.Memory{PhysicalBytes: int64(4 * units.GiB), UsableBytes: int64(4 * units.GiB)},
				Disks:        []*models.Disk{&sda1},
				SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "OpenStack Compute", SerialNumber: "3534"},
				Interfaces:   []*models.Interface{{IPV4Addresses: []string{"1.2.3.4/24"}}},
				Routes:       common.TestDefaultRouteConfiguration,
			}
			generateHWPostStepReply(ctx, h, nonValidInventory, "master-0")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure",
				models.HostValidationIDHasMinCPUCores,
				models.HostValidationIDHasMinMemory,
				models.HostValidationIDValidPlatformNetworkSettings,
				models.HostValidationIDHasCPUCoresForRole,
				models.HostValidationIDHasMemoryForRole)

			// create a validation success
			generateHWPostStepReply(ctx, h, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success",
				models.HostValidationIDHasMinCPUCores,
				models.HostValidationIDHasMinMemory,
				models.HostValidationIDValidPlatformNetworkSettings,
				models.HostValidationIDHasCPUCoresForRole,
				models.HostValidationIDHasMemoryForRole)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMinCPUCores, false)
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMinMemory, false)
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDValidPlatformNetworkSettings, false)
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasCPUCoresForRole, false)
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHasMemoryForRole, false)
		})

		It("'machine-cidr-defined' failed", func() {

			// MachineCidr is sent to service or not, there is no usecase in which the service hold a MachineCidr
			// for the host and at a later time loose it, therefore this case isn't tested and we directly
			// test the validation failure

			oldFailedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDMachineCidrDefined), hostValidationFailedMetric)

			// create a validation failure
			h := &registerHost(*infraEnvID).Host
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDMachineCidrDefined)

			// check generated metrics
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.HostValidationIDMachineCidrDefined), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'machine-cidr-defined' got fixed", func() {

			// create a validation failure
			h := &registerHost(*infraEnvID).Host
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDMachineCidrDefined)

			// create a validation success
			generateHWPostStepReply(ctx, h, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDMachineCidrDefined)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDMachineCidrDefined, false)
		})

		It("'hostname-unique' failed", func() {

			// create a validation success
			h1 := &registerHost(*infraEnvID).Host
			h2 := &registerHost(*infraEnvID).Host
			generateHWPostStepReply(ctx, h1, validHwInfo, "master-0")
			generateHWPostStepReply(ctx, h2, validHwInfo, "master-1")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h1.ID, "success", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *infraEnvID, *h2.ID, "success", models.HostValidationIDHostnameUnique)

			oldChangedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDHostnameUnique), hostValidationChangedMetric)
			oldFailedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDHostnameUnique), hostValidationFailedMetric)

			// create a validation failure
			generateHWPostStepReply(ctx, h1, validHwInfo, "nonUniqName")
			generateHWPostStepReply(ctx, h2, validHwInfo, "nonUniqName")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h1.ID, "failure", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *infraEnvID, *h2.ID, "failure", models.HostValidationIDHostnameUnique)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, "nonUniqName", models.HostValidationIDHostnameUnique, true)
			assertHostValidationEvent(ctx, clusterID, "nonUniqName", models.HostValidationIDHostnameUnique, true)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.HostValidationIDHostnameUnique), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounter + 2))
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.HostValidationIDHostnameUnique), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 2))
		})

		It("'hostname-unique' got fixed", func() {

			// create a validation failure
			h1 := &registerHost(*infraEnvID).Host
			h2 := &registerHost(*infraEnvID).Host
			generateHWPostStepReply(ctx, h1, validHwInfo, "master-0")
			generateHWPostStepReply(ctx, h2, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h1.ID, "failure", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *infraEnvID, *h2.ID, "failure", models.HostValidationIDHostnameUnique)

			// create a validation success
			generateHWPostStepReply(ctx, h2, validHwInfo, "master-1")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h1.ID, "success", models.HostValidationIDHostnameUnique)
			waitForHostValidationStatus(clusterID, *infraEnvID, *h2.ID, "success", models.HostValidationIDHostnameUnique)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHostnameUnique, false)
			assertHostValidationEvent(ctx, clusterID, "master-1", models.HostValidationIDHostnameUnique, false)
		})

		It("'hostname-valid' failed", func() {

			// create a validation success
			h := &registerHost(*infraEnvID).Host
			generateHWPostStepReply(ctx, h, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDHostnameValid)

			oldChangedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDHostnameValid), hostValidationChangedMetric)
			oldFailedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDHostnameValid), hostValidationFailedMetric)

			// create a validation failure
			// 'localhost' is a forbidden host name
			generateHWPostStepReply(ctx, h, validHwInfo, "localhost")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDHostnameValid)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, "localhost", models.HostValidationIDHostnameValid, true)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.HostValidationIDHostnameValid), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounter + 1))
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.HostValidationIDHostnameValid), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'hostname-valid' got fixed", func() {

			// create a validation failure
			h := &registerHost(*infraEnvID).Host
			// 'localhost' is a forbidden host name
			generateHWPostStepReply(ctx, h, validHwInfo, "localhost")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDHostnameValid)

			// create a validation success
			generateHWPostStepReply(ctx, h, validHwInfo, "master-0")
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDHostnameValid)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, "master-0", models.HostValidationIDHostnameValid, false)
		})

		It("'belongs-to-machine-cidr' failed", func() {

			// create a validation success
			h := &registerHost(*infraEnvID).Host
			err := db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventoryWithInterface("1.2.3.4/24")}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDBelongsToMachineCidr)

			oldChangedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDBelongsToMachineCidr), hostValidationChangedMetric)
			oldFailedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDBelongsToMachineCidr), hostValidationFailedMetric)

			// create a validation failure
			err = db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventoryWithInterface("")}).Error
			Expect(err).NotTo(HaveOccurred())
			// machine-cidr doesn't change after it is set
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDBelongsToMachineCidr)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDBelongsToMachineCidr, true)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.HostValidationIDBelongsToMachineCidr), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounter + 1))
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.HostValidationIDBelongsToMachineCidr), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'belongs-to-machine-cidr' got fixed", func() {

			// create a validation failure
			h := &registerHost(*infraEnvID).Host
			err := db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventoryWithInterface("1.2.3.4/24")}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDBelongsToMachineCidr)
			err = db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventoryWithInterface("")}).Error
			Expect(err).NotTo(HaveOccurred())
			// machine-cidr removed after the network interface was deleted
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDBelongsToMachineCidr)

			// create a validation success
			err = db.Model(h).UpdateColumns(&models.Host{Inventory: generateValidInventoryWithInterface("1.2.3.4/24")}).Error
			Expect(err).NotTo(HaveOccurred())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDBelongsToMachineCidr)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDBelongsToMachineCidr, false)
		})

		It("'ignition-downloadable' failed", func() {

			day2ClusterID := v2RegisterDay2Cluster(ctx)
			day2InfraEnvID := registerInfraEnv(&day2ClusterID, models.ImageTypeMinimalIso).ID

			// create a validation success
			h := registerNode(ctx, *day2InfraEnvID, "master-0", defaultCIDRv4)
			generateApiVipPostStepReply(ctx, h, true)
			waitForHostValidationStatus(day2ClusterID, *day2InfraEnvID, *h.ID, "success", models.HostValidationIDIgnitionDownloadable)

			oldChangedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDIgnitionDownloadable), hostValidationChangedMetric)
			oldFailedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDIgnitionDownloadable), hostValidationFailedMetric)

			// create a validation failure
			generateApiVipPostStepReply(ctx, h, false)
			waitForHostValidationStatus(day2ClusterID, *day2InfraEnvID, *h.ID, "failure", models.HostValidationIDIgnitionDownloadable)

			// check generated events
			assertHostValidationEvent(ctx, day2ClusterID, "master-0", models.HostValidationIDIgnitionDownloadable, true)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.HostValidationIDIgnitionDownloadable), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounter + 1))
			metricsDeregisterCluster(ctx, day2ClusterID)
			Expect(getValidationMetricCounter(string(models.HostValidationIDIgnitionDownloadable), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'ignition-downloadable' got fixed", func() {

			day2ClusterID := v2RegisterDay2Cluster(ctx)
			day2InfraEnvID := registerInfraEnv(&day2ClusterID, models.ImageTypeMinimalIso).ID

			// create a validation failure
			h := registerNode(ctx, *day2InfraEnvID, "master-0", defaultCIDRv4)
			generateApiVipPostStepReply(ctx, h, false)
			waitForHostValidationStatus(day2ClusterID, *day2InfraEnvID, *h.ID, "failure", models.HostValidationIDIgnitionDownloadable)

			// create a validation success
			generateApiVipPostStepReply(ctx, h, true)
			waitForHostValidationStatus(day2ClusterID, *day2InfraEnvID, *h.ID, "success", models.HostValidationIDIgnitionDownloadable)

			// check generated events
			assertHostValidationEvent(ctx, day2ClusterID, "master-0", models.HostValidationIDIgnitionDownloadable, false)
		})

		It("'belongs-to-majority-group' failed", func() {

			ips := hostutil.GenerateIPv4Addresses(4, defaultCIDRv4)
			// create a validation success
			h1 := registerNode(ctx, *infraEnvID, "h1", ips[0])
			h2 := registerNode(ctx, *infraEnvID, "h2", ips[1])
			h3 := registerNode(ctx, *infraEnvID, "h3", ips[2])
			h4 := registerNode(ctx, *infraEnvID, "h4", ips[3])
			generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4)
			waitForHostValidationStatus(clusterID, *infraEnvID, *h1.ID, "success", models.HostValidationIDBelongsToMajorityGroup)

			// create a validation failure
			generateFullMeshConnectivity(ctx, ips[0], h2, h3, h4)
			waitForHostValidationStatus(clusterID, *infraEnvID, *h1.ID, "failure", models.HostValidationIDBelongsToMajorityGroup)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, "h1", models.HostValidationIDBelongsToMajorityGroup, true)

			// check generated metrics

			// this specific case can create a short timeframe in which another host is failing on that validation and will
			// be later fixed by the next refresh status cycle because generating a full mesh connectivity isn't an atomic
			// action, therefore, in this test we will check that at least the expected failing host is failing but not fail
			// the test if other hosts fails as well.
			metricCounter := getValidationMetricCounter(string(models.HostValidationIDBelongsToMajorityGroup), hostValidationChangedMetric)
			Expect(metricCounter >= 1).To(BeTrue())
			metricsDeregisterCluster(ctx, clusterID)
			metricCounter = getValidationMetricCounter(string(models.HostValidationIDBelongsToMajorityGroup), hostValidationFailedMetric)
			Expect(metricCounter >= 1).To(BeTrue())
		})

		It("'belongs-to-majority-group' got fixed", func() {
			ips := hostutil.GenerateIPv4Addresses(4, defaultCIDRv4)
			// create a validation failure
			h1 := registerNode(ctx, *infraEnvID, "h1", ips[0])
			h2 := registerNode(ctx, *infraEnvID, "h2", ips[1])
			h3 := registerNode(ctx, *infraEnvID, "h3", ips[2])
			h4 := registerNode(ctx, *infraEnvID, "h4", ips[3])
			generateFullMeshConnectivity(ctx, ips[0], h2, h3, h4)
			waitForHostValidationStatus(clusterID, *infraEnvID, *h1.ID, "failure", models.HostValidationIDBelongsToMajorityGroup)

			// create a validation success
			generateFullMeshConnectivity(ctx, ips[0], h1, h2, h3, h4)
			waitForHostValidationStatus(clusterID, *infraEnvID, *h1.ID, "success", models.HostValidationIDBelongsToMajorityGroup)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, "h1", models.HostValidationIDBelongsToMajorityGroup, false)
		})

		It("'time sync' failed", func() {

			// create a validation success
			h := &registerHost(*infraEnvID).Host
			generateGetNextStepsWithTimestamp(ctx, h, time.Now().Unix())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDTimeSyncedBetweenHostAndService)

			oldChangedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDTimeSyncedBetweenHostAndService), hostValidationChangedMetric)
			oldFailedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDTimeSyncedBetweenHostAndService), hostValidationFailedMetric)

			// create a validation failure
			generateGetNextStepsWithTimestamp(ctx, h, time.Now().Add(-2*time.Hour).Unix())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDTimeSyncedBetweenHostAndService)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.HostValidationIDTimeSyncedBetweenHostAndService), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounter + 1))
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.HostValidationIDTimeSyncedBetweenHostAndService), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'time sync' got fixed", func() {

			// create a validation failure
			h := &registerHost(*infraEnvID).Host
			generateGetNextStepsWithTimestamp(ctx, h, time.Now().Add(65*time.Minute).Unix())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDTimeSyncedBetweenHostAndService)

			// create a validation success
			generateGetNextStepsWithTimestamp(ctx, h, time.Now().Unix())
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDTimeSyncedBetweenHostAndService)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDTimeSyncedBetweenHostAndService, false)
		})

		It("'ntp-synced' failed", func() {

			// create a validation success
			h := &registerHost(*infraEnvID).Host
			generateNTPPostStepReply(ctx, h, []*models.NtpSource{common.TestNTPSourceSynced})
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDNtpSynced)

			oldChangedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDNtpSynced), hostValidationChangedMetric)
			oldFailedMetricCounter := getValidationMetricCounter(string(models.HostValidationIDNtpSynced), hostValidationFailedMetric)

			// create a validation failure
			generateNTPPostStepReply(ctx, h, nil)
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDNtpSynced)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDNtpSynced, true)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.HostValidationIDNtpSynced), hostValidationChangedMetric)).To(Equal(oldChangedMetricCounter + 1))
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.HostValidationIDNtpSynced), hostValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'ntp-synced' got fixed", func() {

			// create a validation failure
			h := &registerHost(*infraEnvID).Host
			generateNTPPostStepReply(ctx, h, nil)
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "failure", models.HostValidationIDNtpSynced)

			// create a validation success
			generateNTPPostStepReply(ctx, h, []*models.NtpSource{common.TestNTPSourceSynced})
			waitForHostValidationStatus(clusterID, *infraEnvID, *h.ID, "success", models.HostValidationIDNtpSynced)

			// check generated events
			assertHostValidationEvent(ctx, clusterID, string(*h.ID), models.HostValidationIDNtpSynced, false)
		})
	})

	Context("Cluster validation metrics", func() {

		removeHost := func(host *models.Host) {
			_, err := userBMClient.Installer.V2DeregisterHost(ctx, &installer.V2DeregisterHostParams{
				InfraEnvID: host.InfraEnvID,
				HostID:     *host.ID,
			})
			Expect(err).NotTo(HaveOccurred())
		}

		It("'all-hosts-are-ready-to-install' failed", func() {

			// create a validation success
			hosts, _ := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
			c := getCluster(clusterID)
			for _, h := range c.Hosts {
				generateDomainResolution(ctx, h, "test-cluster", "example.com")
			}
			waitForClusterValidationStatus(clusterID, "success", models.ClusterValidationIDAllHostsAreReadyToInstall)

			oldChangedMetricCounter := getValidationMetricCounter(string(models.ClusterValidationIDAllHostsAreReadyToInstall), clusterValidationChangedMetric)
			oldFailedMetricCounter := getValidationMetricCounter(string(models.ClusterValidationIDAllHostsAreReadyToInstall), clusterValidationFailedMetric)

			// create a validation failure by causing the a host to not be ready
			generateHWPostStepReply(ctx, hosts[0], validHwInfo, "localhost")
			waitForHostStateV2(ctx, models.HostStatusInsufficient, defaultWaitForHostStateTimeout, hosts[0])

			waitForClusterValidationStatus(clusterID, "failure", models.ClusterValidationIDAllHostsAreReadyToInstall)

			// check generated events
			assertClusterValidationEvent(ctx, clusterID, models.ClusterValidationIDAllHostsAreReadyToInstall, true)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.ClusterValidationIDAllHostsAreReadyToInstall), clusterValidationChangedMetric)).To(Equal(oldChangedMetricCounter + 1))
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.ClusterValidationIDAllHostsAreReadyToInstall), clusterValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'all-hosts-are-ready-to-install' got fixed", func() {

			// create a validation failure
			hosts, ips := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
			c := getCluster(clusterID)
			for _, h := range c.Hosts {
				generateDomainResolution(ctx, h, "test-cluster", "example.com")
			}

			removeHost(hosts[0])
			waitForClusterValidationStatus(clusterID, "failure", models.ClusterValidationIDSufficientMastersCount)

			// create a validation success
			h1 := registerNode(ctx, *infraEnvID, "h1-new", ips[0])
			v2UpdateVipParams(ctx, clusterID)
			generateFullMeshConnectivity(ctx, ips[0], h1, hosts[1], hosts[2])
			waitForClusterValidationStatus(clusterID, "success", models.ClusterValidationIDAllHostsAreReadyToInstall)

			// check generated events
			assertClusterValidationEvent(ctx, clusterID, models.ClusterValidationIDAllHostsAreReadyToInstall, false)
		})

		It("'sufficient-masters-count' failed", func() {

			// create a validation success
			hosts, _ := register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
			waitForClusterValidationStatus(clusterID, "success", models.ClusterValidationIDSufficientMastersCount)

			oldChangedMetricCounter := getValidationMetricCounter(string(models.ClusterValidationIDSufficientMastersCount), clusterValidationChangedMetric)
			oldFailedMetricCounter := getValidationMetricCounter(string(models.ClusterValidationIDSufficientMastersCount), clusterValidationFailedMetric)

			// create a validation failure
			removeHost(hosts[0])
			waitForClusterValidationStatus(clusterID, "failure", models.ClusterValidationIDSufficientMastersCount)

			// check generated events
			assertClusterValidationEvent(ctx, clusterID, models.ClusterValidationIDSufficientMastersCount, true)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.ClusterValidationIDSufficientMastersCount), clusterValidationChangedMetric)).To(Equal(oldChangedMetricCounter + 1))
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.ClusterValidationIDSufficientMastersCount), clusterValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'sufficient-masters-count' got fixed", func() {

			// create a validation failure
			waitForClusterValidationStatus(clusterID, "failure", models.ClusterValidationIDSufficientMastersCount)

			// create a validation success
			register3nodes(ctx, clusterID, *infraEnvID, defaultCIDRv4)
			waitForClusterValidationStatus(clusterID, "success", models.ClusterValidationIDSufficientMastersCount)

			// check generated events
			assertClusterValidationEvent(ctx, clusterID, models.ClusterValidationIDSufficientMastersCount, false)
		})

		It("'ntp-server-configured' failed", func() {

			ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
			// create a validation success
			h1 := registerNode(ctx, *infraEnvID, "h1", ips[0])
			registerNode(ctx, *infraEnvID, "h2", ips[1])
			waitForClusterValidationStatus(clusterID, "success", models.ClusterValidationIDNtpServerConfigured)

			oldChangedMetricCounter := getValidationMetricCounter(string(models.ClusterValidationIDNtpServerConfigured), clusterValidationChangedMetric)
			oldFailedMetricCounter := getValidationMetricCounter(string(models.ClusterValidationIDNtpServerConfigured), clusterValidationFailedMetric)

			// create a validation failure
			generateGetNextStepsWithTimestamp(ctx, h1, time.Now().Unix()+(common.MaximumAllowedTimeDiffMinutes+1)*60)
			Expect(db.Model(h1).Update("status", "known").Error).NotTo(HaveOccurred())
			waitForClusterValidationStatus(clusterID, "failure", models.ClusterValidationIDNtpServerConfigured)

			// check generated events
			assertClusterValidationEvent(ctx, clusterID, models.ClusterValidationIDNtpServerConfigured, true)

			// check generated metrics
			Expect(getValidationMetricCounter(string(models.ClusterValidationIDNtpServerConfigured), clusterValidationChangedMetric)).To(Equal(oldChangedMetricCounter + 1))
			metricsDeregisterCluster(ctx, clusterID)
			Expect(getValidationMetricCounter(string(models.ClusterValidationIDNtpServerConfigured), clusterValidationFailedMetric)).To(Equal(oldFailedMetricCounter + 1))
		})

		It("'ntp-server-configured' got fixed", func() {
			ips := hostutil.GenerateIPv4Addresses(2, defaultCIDRv4)
			// create a validation failure
			h1 := registerNode(ctx, *infraEnvID, "h1", ips[0])
			registerNode(ctx, *infraEnvID, "h2", ips[1])
			generateGetNextStepsWithTimestamp(ctx, h1, time.Now().Unix()+(common.MaximumAllowedTimeDiffMinutes+1)*60)
			Expect(db.Model(h1).Update("status", "known").Error).NotTo(HaveOccurred())
			waitForClusterValidationStatus(clusterID, "failure", models.ClusterValidationIDNtpServerConfigured)

			// create a validation success
			generateGetNextStepsWithTimestamp(ctx, h1, time.Now().Unix())
			generateHWPostStepReply(ctx, h1, validHwInfo, "h1")
			waitForClusterValidationStatus(clusterID, "success", models.ClusterValidationIDNtpServerConfigured)

			// check generated events
			assertClusterValidationEvent(ctx, clusterID, models.ClusterValidationIDNtpServerConfigured, false)
		})
	})

	Context("Filesystem metrics test", func() {
		if Options.Storage != "filesystem" {
			return
		}

		It("'assisted_installer_filesystem_usage_percentage' metric recorded", func() {
			By("Generate ISO for cluster")
			imageType := models.ImageTypeMinimalIso
			_ = registerInfraEnv(&clusterID, imageType).ID
			verifyEventExistence(clusterID, fmt.Sprintf("Image type is \"%s\"", imageType))

			By("Verify filesystem metrics")
			record, err := getMetricRecord("assisted_installer_filesystem_usage_percentage")
			Expect(err).NotTo(HaveOccurred())
			Expect(record).ToNot(BeEmpty())

			value, err := strconv.ParseFloat(record[strings.LastIndex(record, " ")+1:], 32)
			Expect(err).NotTo(HaveOccurred())
			Expect(value).To(BeNumerically(">", 0))
			Expect(value).To(BeNumerically("<=", 100))
		})
	})
})
