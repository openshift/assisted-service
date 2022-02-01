package ocs_test

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	clust "github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/openshift/assisted-service/internal/metrics"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/operators/ocs"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
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
		ocsLabel                                      = "cluster.ocs.openshift.io/openshift-storage"
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
		clusterApi = clust.NewManager(cfg, common.GetTestLog().WithField("pkg", "cluster-monitor"), db,
			mockEvents, mockHostAPI, mockMetric, nil, nil, operatorsManager, nil, nil, nil)

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
		// Compact mode validations
		{
			name:          "ocs enabled, cluster of two master hosts with both hosts labeled for ocs",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 10, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 7, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationFailure, messagePattern: "Clusters must have exactly 3 dedicated masters"},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "A minimum of 3 hosts is required to deploy OCS."},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster of three master hosts with all hosts labeled for ocs",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "OCS Requirements for Compact Mode are satisfied"},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster of three master hosts with two hosts labeled for ocs",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "Insufficient Resources to deploy OCS in Compact Mode. OCS requires a minimum of 3 hosts with one non-bootable disk on each host of size at least 25 GB."},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster of two master and one worker host with all hosts labeled for ocs",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationFailure, messagePattern: "Clusters must have exactly 3 dedicated masters and if workers are added, there should be at least 2 workers. Please check your configuration and add or remove hosts as to meet the above requirement."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "Insufficient Resources to deploy OCS in Compact Mode. OCS requires a minimum of 3 hosts with one non-bootable disk on each host of size at least 25 GB."},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster of three master hosts of which one has empty inventory with all hosts labeled for ocs",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: "", Role: models.HostRoleMaster, Labels: ocsLabel},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationPending, messagePattern: "Missing Inventory in some of the hosts"},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster of three master hosts with no disk on one host with all hosts labeled for ocs",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
					{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
					{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster, Labels: ocsLabel},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "Insufficient Resources to deploy OCS in Compact Mode. OCS requires a minimum of 3 hosts with one non-bootable disk on each host of size at least 25 GB."},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster of three master hosts with insufficient disk size on one host",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 1 * conversions.GB, DriveType: "SSD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "OCS requires all the non-bootable disks to be more than 25 GB"},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster of three master hosts with total disks not a multiple of 3 with all hosts labeled for ocs",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID3}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID3}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "OCS Requirements for Compact Mode are satisfied"},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster with two master and one auto-assign host",
			srcState:      models.ClusterStatusPendingForInput,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleAutoAssign, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "OCS Requirements for Compact Mode are satisfied"},
			}),
			errorExpected: false,
		},

		// Standard mode validations
		{
			name:          "ocs enabled, cluster of three master and two worker hosts with both worker hosts labeled for ocs",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "A cluster with only 3 masters or with a minimum of 3 workers is required."},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster of three master and three worker hosts with all worker hosts labeled for ocs",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 10, Ram: 15 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 10, Ram: 32 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 9, Ram: 60 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "OCS Requirements for Standard Deployment are satisfied"},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster of three master and three worker hosts with two worker hosts labeled for ocs",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 10, Ram: 15 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 10, Ram: 32 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 9, Ram: 60 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "Insufficient Resources to deploy OCS in Standard Mode. OCS requires a minimum of 3 hosts with one non-bootable disk on each host of size at least 25 GB."},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster with three master and three worker hosts with no inventory on one worker host",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown), Inventory: "", Role: models.HostRoleWorker, Labels: ocsLabel},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationPending, messagePattern: "Missing Inventory in some of the hosts"},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster with three master and three worker hosts with insufficient disk size on one worker host",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID3}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 12, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID3}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 12, Ram: 64, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 1 * conversions.GB, DriveType: "SSD", ID: diskID3}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "OCS requires all the non-bootable disks to be more than 25 GB"},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster with three master and three worker hosts with total disks not a multiple of 3",
			srcState:      models.ClusterStatusReady,
			dstState:      models.ClusterStatusReady,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown), Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB}), Role: models.HostRoleMaster},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 8, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID3}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 8, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 8, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 20 * conversions.GB, DriveType: "HDD", ID: diskID1},
						{SizeBytes: 40 * conversions.GB, DriveType: "HDD", ID: diskID2}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoReady),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationSuccess, messagePattern: "OCS Requirements for Standard Deployment are satisfied"},
			}),
			errorExpected: false,
		},
		{
			name:          "ocs enabled, cluster with three master, two worker and one auto-assign host",
			srcState:      models.ClusterStatusPendingForInput,
			dstState:      models.ClusterStatusInsufficient,
			pullSecretSet: true,
			hosts: []models.Host{
				{ID: &hid1, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleAutoAssign, InstallationDiskID: diskID1},
				{ID: &hid2, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid3, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleMaster, InstallationDiskID: diskID1},
				{ID: &hid4, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid5, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
				{ID: &hid6, Status: swag.String(models.HostStatusKnown),
					Inventory: ocs.Inventory(&ocs.InventoryResources{Cpus: 16, Ram: 64 * conversions.GiB, Disks: []*models.Disk{
						{SizeBytes: 25 * conversions.GB},
						{SizeBytes: 40 * conversions.GB}}}),
					Role: models.HostRoleWorker, Labels: ocsLabel, InstallationDiskID: diskID1},
			},
			statusInfoChecker: makeValueChecker(clust.StatusInfoInsufficient),
			validationsChecker: makeJsonChecker(map[clust.ValidationID]validationCheckResult{
				clust.IsMachineCidrDefined:                {status: clust.ValidationSuccess, messagePattern: "The Machine Network CIDR is defined"},
				clust.IsMachineCidrEqualsToCalculatedCidr: {status: clust.ValidationSuccess, messagePattern: "The Cluster Machine CIDR is equivalent to the calculated CIDR"},
				clust.IsApiVipDefined:                     {status: clust.ValidationSuccess, messagePattern: "The API virtual IP is defined"},
				clust.IsApiVipValid:                       {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.IsIngressVipDefined:                 {status: clust.ValidationSuccess, messagePattern: "The Ingress virtual IP is defined"},
				clust.IsIngressVipValid:                   {status: clust.ValidationSuccess, messagePattern: "belongs to the Machine CIDR and is not in use."},
				clust.AllHostsAreReadyToInstall:           {status: clust.ValidationSuccess, messagePattern: "All hosts in the cluster are ready to install"},
				clust.IsDNSDomainDefined:                  {status: clust.ValidationSuccess, messagePattern: "The base domain is defined"},
				clust.IsPullSecretSet:                     {status: clust.ValidationSuccess, messagePattern: "The pull secret is set"},
				clust.SufficientMastersCount:              {status: clust.ValidationSuccess, messagePattern: "The cluster has a sufficient number of master candidates."},
				clust.IsOcsRequirementsSatisfied:          {status: clust.ValidationFailure, messagePattern: "For OCS Standard Mode, all host roles must be assigned to master or worker."},
			}),
			errorExpected: false,
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			operators := []*models.MonitoredOperator{
				&ocs.Operator,
			}

			cluster = common.Cluster{
				Cluster: models.Cluster{
					ID:                 &clusterId,
					ClusterNetworks:    common.TestIPv4Networking.ClusterNetworks,
					ServiceNetworks:    common.TestIPv4Networking.ServiceNetworks,
					MachineNetworks:    common.TestIPv4Networking.MachineNetworks,
					APIVip:             common.TestIPv4Networking.APIVip,
					IngressVip:         common.TestIPv4Networking.IngressVip,
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
