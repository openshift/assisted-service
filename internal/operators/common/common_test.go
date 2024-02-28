package common_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/operators/common"
	"github.com/openshift/assisted-service/internal/operators/mce"
	"github.com/openshift/assisted-service/internal/operators/odf"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

var _ = DescribeTable(
	"Get valid disk count",
	func(disks []*models.Disk, diskID string, minSize, expectedEligibleDisks, expectedAvailableDisks int64) {
		eligibleDisks, availableDisks := common.NonInstallationDiskCount(disks, diskID, minSize)
		Expect(eligibleDisks).To(Equal(expectedEligibleDisks))
		Expect(availableDisks).To(Equal(expectedAvailableDisks))
	},
	Entry("no disk provided", []*models.Disk{}, "", int64(0), int64(0), int64(0)),
	Entry("no valid disk provided", []*models.Disk{
		{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeUnknown, ID: "/dev/disk/by-id/disk-1"},
		{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeVirtual, ID: "/dev/disk/by-id/disk-2"},
	}, "", int64(0), int64(0), int64(0)),
	Entry("valid disk provided, but wrong size", []*models.Disk{
		{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeSSD, ID: "/dev/disk/by-id/disk-1"},
		{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeSSD, ID: "/dev/disk/by-id/disk-2"},
	}, "", int64(25), int64(0), int64(2)),
	Entry("only one valid disk provided with the right size, but chosen for install", []*models.Disk{
		{SizeBytes: 20 * conversions.GB, DriveType: models.DriveTypeSSD, ID: "/dev/disk/by-id/disk-1"},
		{SizeBytes: 200 * conversions.GB, DriveType: models.DriveTypeSSD, ID: "/dev/disk/by-id/disk-2"},
	}, "/dev/disk/by-id/disk-2", int64(25), int64(0), int64(1)),
	Entry("only one valid disk provided with the right size", []*models.Disk{
		{SizeBytes: 50 * conversions.GB, DriveType: models.DriveTypeHDD, ID: "/dev/disk/by-id/disk-1"},
		{SizeBytes: 200 * conversions.GB, DriveType: models.DriveTypeSSD, ID: "/dev/disk/by-id/disk-2"},
	}, "/dev/disk/by-id/disk-2", int64(25), int64(1), int64(0)),
	Entry("two valid disks provided with the right size", []*models.Disk{
		{SizeBytes: 50 * conversions.GB, DriveType: models.DriveTypeHDD, ID: "/dev/disk/by-id/disk-1"},
		{SizeBytes: 200 * conversions.GB, DriveType: models.DriveTypeSSD, ID: "/dev/disk/by-id/disk-2"},
		{SizeBytes: 50 * conversions.GB, DriveType: models.DriveTypeSSD, ID: "/dev/disk/by-id/disk-3"},
	}, "/dev/disk/by-id/disk-2", int64(25), int64(2), int64(0)),
)

var _ = DescribeTable(
	"has operator",
	func(operators []*models.MonitoredOperator, operatorName string, isExpected bool) {
		found := common.HasOperator(operators, operatorName)
		Expect(found).To(Equal(isExpected))
	},
	Entry("no operators", []*models.MonitoredOperator{}, mce.Operator.Name, false),
	Entry("not matching any operator", []*models.MonitoredOperator{&odf.Operator}, mce.Operator.Name, false),
	Entry("matching a operator", []*models.MonitoredOperator{&odf.Operator, &mce.Operator}, mce.Operator.Name, true),
)

func TestHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Operators common test suite")
}
