package metrics

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/openshift/assisted-service/models"
	"github.com/prometheus/client_golang/prometheus"

	_ "github.com/jinzhu/gorm/dialects/postgres"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Metrcis Manager", func() {

	const (
		ocpVersion  = "4.6"
		emailDomain = "redhat.com"
	)

	var (
		mm        *MetricsManager
		clusterID strfmt.UUID
	)

	BeforeEach(func() {
		clusterID = strfmt.UUID(uuid.New().String())
		prometheusRegistry := prometheus.DefaultRegisterer
		mm = NewMetricsManager(prometheusRegistry)
	})

	AfterEach(func() {
	})

	Context("Host validation metrics", func() {

		tests := []struct {
			hostValidationID models.HostValidationID
		}{
			{hostValidationID: models.HostValidationIDConnected},
			{hostValidationID: models.HostValidationIDHasInventory},
			{hostValidationID: models.HostValidationIDHasMinCPUCores},
			{hostValidationID: models.HostValidationIDHasMinValidDisks},
			{hostValidationID: models.HostValidationIDHasMinMemory},
			{hostValidationID: models.HostValidationIDMachineCidrDefined},
			{hostValidationID: models.HostValidationIDRoleDefined},
			{hostValidationID: models.HostValidationIDHasCPUCoresForRole},
			{hostValidationID: models.HostValidationIDHasMemoryForRole},
			{hostValidationID: models.HostValidationIDHostnameUnique},
			{hostValidationID: models.HostValidationIDHostnameValid},
			{hostValidationID: models.HostValidationIDBelongsToMachineCidr},
			{hostValidationID: models.HostValidationIDAPIVipConnected},
			{hostValidationID: models.HostValidationIDBelongsToMajorityGroup},
			{hostValidationID: models.HostValidationIDValidPlatform},
			{hostValidationID: models.HostValidationIDNtpSynced},
		}

		for _, t := range tests {

			It(string(t.hostValidationID), func() {

				Expect(mm.serviceLogicHostValidationFailed.WithLabelValues(ocpVersion, clusterID.String(), emailDomain, string(t.hostValidationID))).Should(Equal(0))

				mm.HostValidationFailed(ocpVersion, clusterID, emailDomain, t.hostValidationID)
				Expect(mm.serviceLogicHostValidationFailed.WithLabelValues(ocpVersion, clusterID.String(), emailDomain, string(t.hostValidationID))).Should(Equal(1))

				mm.HostValidationFailed(ocpVersion, clusterID, emailDomain, t.hostValidationID)
				Expect(mm.serviceLogicHostValidationFailed.WithLabelValues(ocpVersion, clusterID.String(), emailDomain, string(t.hostValidationID))).Should(Equal(2))
			})
		}

	})
})
