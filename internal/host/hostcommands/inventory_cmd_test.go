package hostcommands

import (
	"context"
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("inventory", func() {
	ctx := context.Background()
	var host models.Host
	var hostId, clusterId, infraEnvId strfmt.UUID

	BeforeEach(func() {
		hostId = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		infraEnvId = strfmt.UUID(uuid.New().String())
		host = hostutil.GenerateTestHost(hostId, infraEnvId, clusterId, models.HostStatusDiscovering)
	})

	It("returns inventory step with host id only when limits are unset", func() {
		invCmd := NewInventoryCmd(common.GetTestLog(), 0, 0)

		steps, err := invCmd.GetSteps(ctx, &host)
		Expect(err).NotTo(HaveOccurred())
		Expect(steps).To(HaveLen(1))
		Expect(steps[0].StepType).To(Equal(models.StepTypeInventory))
		Expect(steps[0].Args).To(Equal([]string{hostId.String()}))
	})

	It("appends output-max-size arg when max size is configured", func() {
		const maxSize int64 = 52_428_800
		invCmd := NewInventoryCmd(common.GetTestLog(), maxSize, 0)

		steps, err := invCmd.GetSteps(ctx, &host)
		Expect(err).NotTo(HaveOccurred())
		Expect(steps).To(HaveLen(1))
		Expect(steps[0].StepType).To(Equal(models.StepTypeInventory))
		Expect(steps[0].Args).To(Equal([]string{
			hostId.String(),
			fmt.Sprintf("--output-max-size=%d", maxSize),
		}))
	})

	It("does not append output-max-size arg for negative max size", func() {
		invCmd := NewInventoryCmd(common.GetTestLog(), -1, 0)

		steps, err := invCmd.GetSteps(ctx, &host)
		Expect(err).NotTo(HaveOccurred())
		Expect(steps[0].Args).To(Equal([]string{hostId.String()}))
	})

	It("appends disk-min-size arg when disk min size is configured", func() {
		const diskMinSize int64 = 16_106_127_360 // 15 GiB
		invCmd := NewInventoryCmd(common.GetTestLog(), 0, diskMinSize)

		steps, err := invCmd.GetSteps(ctx, &host)
		Expect(err).NotTo(HaveOccurred())
		Expect(steps).To(HaveLen(1))
		Expect(steps[0].StepType).To(Equal(models.StepTypeInventory))
		Expect(steps[0].Args).To(Equal([]string{
			hostId.String(),
			fmt.Sprintf("--disk-min-size=%d", diskMinSize),
		}))
	})

	It("does not append disk-min-size arg for negative disk min size", func() {
		invCmd := NewInventoryCmd(common.GetTestLog(), 0, -1)

		steps, err := invCmd.GetSteps(ctx, &host)
		Expect(err).NotTo(HaveOccurred())
		Expect(steps[0].Args).To(Equal([]string{hostId.String()}))
	})

	It("appends both limit args when max size and disk min size are configured", func() {
		const (
			maxSize     int64 = 52_428_800
			diskMinSize int64 = 16_106_127_360
		)
		invCmd := NewInventoryCmd(common.GetTestLog(), maxSize, diskMinSize)

		steps, err := invCmd.GetSteps(ctx, &host)
		Expect(err).NotTo(HaveOccurred())
		Expect(steps[0].Args).To(Equal([]string{
			hostId.String(),
			fmt.Sprintf("--output-max-size=%d", maxSize),
			fmt.Sprintf("--disk-min-size=%d", diskMinSize),
		}))
	})
})
