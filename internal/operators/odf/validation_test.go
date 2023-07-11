package odf_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	clust "github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	commontesting "github.com/openshift/assisted-service/internal/common/testing"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"gorm.io/gorm"
)

type statusInfoChecker interface {
	check(statusInfo *string)
}

type valueChecker struct {
	value string
}

func (v *valueChecker) check(value *string) {
	if value == nil {
		Expect(v.value).To(Equal(""))
	} else {
		Expect(*value).To(Equal(v.value))
	}
}

func makeValueChecker(value string) statusInfoChecker {
	return &valueChecker{value: value}
}

type validationsChecker struct {
	expected map[clust.ValidationID]validationCheckResult
}

func (j *validationsChecker) check(validationsStr string) {
	validationMap := make(map[string][]clust.ValidationResult)
	Expect(json.Unmarshal([]byte(validationsStr), &validationMap)).ToNot(HaveOccurred())
next:
	for id, checkedResult := range j.expected {
		category, err := id.Category()
		Expect(err).ToNot(HaveOccurred())
		results, ok := validationMap[category]
		Expect(ok).To(BeTrue())
		for _, r := range results {
			if r.ID == id {
				Expect(r.Status).To(Equal(checkedResult.status), "id = %s", id.String())
				Expect(r.Message).To(MatchRegexp(checkedResult.messagePattern))
				continue next
			}
		}
		// Should not reach here
		Expect(false).To(BeTrue())
	}
}

type validationCheckResult struct {
	status         clust.ValidationStatus
	messagePattern string
}

func makeJsonChecker(expected map[clust.ValidationID]validationCheckResult) *validationsChecker {
	return &validationsChecker{expected: expected}
}

var _ = Describe("Ocs Operator use-cases", func() {
	var (
		ctx                                           = context.Background()
		db                                            *gorm.DB
		clusterId, hid1, hid2, hid3, hid4, hid5, hid6 strfmt.UUID
		cluster                                       common.Cluster
		clusterApi                                    *clust.Manager
		mockEvents                                    *eventsapi.MockHandler
		mockHostAPI                                   *host.MockAPI
		mockMetric                                    *metrics.MockAPI
		ctrl                                          *gomock.Controller
		dbName                                        string
		diskID1                                       = "/dev/disk/by-id/test-disk-1"
		diskID2                                       = "/dev/disk/by-id/test-disk-2"
		diskID3                                       = "/dev/disk/by-id/test-disk-3"
	)

	mockHostAPIIsRequireUserActionResetFalse := func() {
		mockHostAPI.EXPECT().IsRequireUserActionReset(gomock.Any()).Return(false).AnyTimes()
	}

	mockIsValidMasterCandidate := func() {
		mockHostAPI.EXPECT().IsValidMasterCandidate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil).AnyTimes()
	}
	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockHostAPI = host.NewMockAPI(ctrl)
		mockMetric = metrics.NewMockAPI(ctrl)
		operatorsManager := operators.NewManager(common.GetTestLog(), nil, operators.Options{}, nil, nil)
		var cfg clust.Config
		Expect(envconfig.Process(common.EnvConfigPrefix, &cfg)).ShouldNot(HaveOccurred())
		clusterApi = clust.NewManager(cfg, common.GetTestLog().WithField("pkg", "cluster-monitor"), db, commontesting.GetDummyNotificationStream(ctrl),
			mockEvents, nil, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil, nil, nil, nil)

		hid1 = strfmt.UUID("054e0100-f50e-4be7-874d-73861179e40d")
		hid2 = strfmt.UUID("514c8480-cda5-46e5-afce-e146def2066f")
		hid3 = strfmt.UUID(uuid.New().String())
		hid4 = strfmt.UUID("77e381eb-f464-4d28-829e-210bd26dba68")
		hid5 = strfmt.UUID("e80cb08a-e797-44f5-adc2-2826790b0307")
		hid6 = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
	})

	tests := []struct {
		name                    string
		srcState                string
		srcStatusInfo           string
		pullSecretSet           bool
		dstState                string
		hosts                   []models.Host
		statusInfoChecker       statusInfoChecker
		validationsChecker      *validationsChecker
		setMachineCidrUpdatedAt bool
		errorExpected           bool
		OpenShiftVersion        string
	}{
		{
			name:          "odf enabled, 3 sufficient nodes",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "ODF Requirements for Compact Mode are satisfied"},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 6 sufficient nodes",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 10, Ram: 15 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 10, Ram: 32 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 9, Ram: 60 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "ODF Requirements for Standard Deployment are satisfied"},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 3 nodes, one with empty inventory",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: "", Role: models.HostRoleMaster},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationPending, messagePattern: "Missing Inventory in some of the hosts"},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 3 nodes, total disks not a multiple of 3",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID3}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID3}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "ODF Requirements for Compact Mode are satisfied"},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 3 insufficient nodes with less than 3 nodes",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 10, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 7, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationFailure, messagePattern: fmt.Sprintf("Clusters must have exactly %d dedicated control plane nodes. Add or remove hosts, or change their roles configurations to meet the requirement.", common.MinMasterHostsNeededForInstallation)},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "A minimum of 3 hosts is required to deploy ODF."},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 3 nodes with less than 3 disks",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "Insufficient resources to deploy ODF in compact mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of 25 GB minimum and an installation disk."},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 3 nodes with 3 odf disk, 1 with size less than 25GB",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 1 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "Insufficient resources to deploy ODF in compact mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of 25 GB minimum and an installation disk."},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 5 unsupported nodes ( 3 masters + 2 workers )",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "A cluster with only 3 masters or with a minimum of 3 workers is required."},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 6 nodes with 3 worker nodes, one with empty inventory",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown), Inventory: "", Role: models.HostRoleWorker},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationPending, messagePattern: "Missing Inventory in some of the hosts"},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 6 nodes with 3 worker nodes, one with disk less than 25GB",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID3}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID3}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 1 * conversions.GB, DriveType: models.DriveTypeSSD, ID: diskID3}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "Insufficient resources to deploy ODF in standard mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of 25 GB minimum and an installation disk."},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 6 nodes with 3 worker nodes, total disks on workers not a multiple of 3",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 8, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID3}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 8, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 8, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "ODF Requirements for Standard Deployment are satisfied"},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 6 nodes with 3 insufficient worker nodes due to insufficient disks",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 10, Ram: 64 * conversions.GiB}), Role: models.HostRoleWorker},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 10, Ram: 64 * conversions.GiB}), Role: models.HostRoleWorker},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown), Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB}), Role: models.HostRoleWorker},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "Insufficient resources to deploy ODF in standard mode. ODF requires a minimum of 3 hosts. Each host must have at least 1 additional disk of 25 GB minimum and an installation disk."},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 6 nodes, with role of one as auto-assign (odf validation failure)",
			srcState:      models.ClusterStatusPendingForInput,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleAutoAssign, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "For ODF Standard Mode, all host roles must be assigned to master or worker."},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 3 nodes, with role of one as auto-assign (odf validation success)",
			srcState:      models.ClusterStatusPendingForInput,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleAutoAssign, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "ODF Requirements for Compact Mode are satisfied"},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 6 nodes with 3 worker nodes for standard deployment",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 12, Ram: 32 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "ODF Requirements for Standard Deployment are satisfied"},
			}),
			errorExpected: false,
		},
		{
			name:          "odf enabled, 3 sufficient nodes, Disk with empty disk size",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2},
						{SizeBytes: 0 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID3}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: odf.Inventory(&odf.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: models.DriveTypeHDD, ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.AreApiVipsDefined:                   {status: clust.ValidationSuccess, messagePattern: "API virtual IPs are defined"},
				clust.AreApiVipsValid:                     {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AreIngressVipsDefined:               {status: clust.ValidationSuccess, messagePattern: "Ingress virtual IPs are defined"},
				clust.AreIngressVipsValid:                 {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has the exact amount of dedicated control plane nodes."},
				clust.IsOdfRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "ODF Requirements for Compact Mode are satisfied"},
			}),
			errorExpected: false,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			operators := []*models.MonitoredOperator{
				&odf.Operator,
			}

			cluster = common.Cluster{
				Cluster: models.Cluster{
					ID:                 &clusterId,
					ClusterNetworks:    common.TestIPv4Networking.ClusterNetworks,
					ServiceNetworks:    common.TestIPv4Networking.ServiceNetworks,
					MachineNetworks:    common.TestIPv4Networking.MachineNetworks,
					APIVip:             common.TestIPv4Networking.APIVip,
					APIVips:            common.TestIPv4Networking.APIVips,
					IngressVip:         common.TestIPv4Networking.IngressVip,
					IngressVips:        common.TestIPv4Networking.IngressVips,
					Status:             &t.srcState,
					StatusInfo:         &t.srcStatusInfo,
					BaseDNSDomain:      "test.com",
					PullSecretSet:      t.pullSecretSet,
					MonitoredOperators: operators,
					OpenshiftVersion:   t.OpenShiftVersion,
					NetworkType:        swag.String(models.ClusterNetworkTypeOVNKubernetes),
				},
			}

			Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
			mockIsValidMasterCandidate()
			for i := range t.hosts {
				t.hosts[i].ClusterID = &clusterId
				t.hosts[i].InfraEnvID = clusterId
				Expect(db.Create(&t.hosts[i]).Error).ShouldNot(HaveOccurred())
			}

			cluster = getClusterFromDB(clusterId, db)
			if t.dstState == models.ClusterStatusInsufficient {
				mockHostAPIIsRequireUserActionResetFalse()
			}
			if t.srcState != t.dstState {
				mockEvents.EXPECT().SendClusterEvent(gomock.Any(), gomock.Any()).AnyTimes()
			}
			clusterAfterRefresh, err := clusterApi.RefreshStatus(ctx, &cluster, db)
			if t.errorExpected {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
			Expect(clusterAfterRefresh.Status).To(Equal(&t.dstState))
			t.statusInfoChecker.check(clusterAfterRefresh.StatusInfo)
			if t.validationsChecker != nil {
				t.validationsChecker.check(clusterAfterRefresh.ValidationsInfo)
			}
		})
	}

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

})

func getClusterFromDB(clusterId strfmt.UUID, db *gorm.DB) common.Cluster {
	c, err := common.GetClusterFromDB(db, clusterId, common.UseEagerLoading)
	Expect(err).ShouldNot(HaveOccurred())
	return *c
}
