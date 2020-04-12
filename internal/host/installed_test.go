package host

import (
	"context"
	"time"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("installed_state", func() {
	ctx := context.Background()
	var state API
	var db *gorm.DB
	currentState := hostStatusInstalled
	var host models.Host
	var id, clusterId strfmt.UUID
	var updateReply *UpdateReply
	var updateErr error
	var expectedReply *expect

	BeforeEach(func() {
		db = prepareDB()
		state = &State{installed: NewInstalledState(getTestLog(), db)}

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = models.Host{
			Base: models.Base{
				ID: &id,
			},
			ClusterID:    clusterId,
			Status:       swag.String(currentState),
			HardwareInfo: defaultHwInfo,
		}
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		expectedReply = &expect{expectedState: currentState}
	})

	It("register_host", func() {
		updateReply, updateErr = state.RegisterHost(ctx, &host)
		expectedReply.expectError = true
	})

	It("update_hw_info", func() {
		updateReply, updateErr = state.UpdateHwInfo(ctx, &host, "some hw info")
		expectedReply.expectError = true
		expectedReply.postCheck = func() {
			h := getHost(id, clusterId, db)
			Expect(h.HardwareInfo).Should(Equal(defaultHwInfo))
		}
	})

	It("update_role", func() {
		updateReply, updateErr = state.UpdateRole(ctx, &host, "master", nil)
		expectedReply.expectError = true
	})

	Context("refresh_status", func() {
		It("keep_alive", func() {
			updateReply, updateErr = state.RefreshStatus(ctx, &host)
		})
		It("keep_alive_timeout", func() {
			host.UpdatedAt = strfmt.DateTime(time.Now().Add(-time.Hour))
			updateReply, updateErr = state.RefreshStatus(ctx, &host)
		})
	})

	It("install", func() {
		updateReply, updateErr = state.Install(ctx, &host, nil)
		expectedReply.expectError = true
	})

	It("enable_host", func() {
		updateReply, updateErr = state.EnableHost(ctx, &host)
	})

	It("disable_host", func() {
		updateReply, updateErr = state.DisableHost(ctx, &host)
		expectedReply.expectError = true
	})

	AfterEach(func() {
		postValidation(expectedReply, currentState, db, id, clusterId, updateReply, updateErr)

		// cleanup
		db.Close()
		expectedReply = nil
		updateReply = nil
		updateErr = nil
	})
})
