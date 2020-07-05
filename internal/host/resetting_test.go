package host

import (
	"context"
	"time"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("resetting_state", func() {
	ctx := context.Background()
	var state API
	var db *gorm.DB
	currentState := HostStatusResetting
	var host models.Host
	var id, clusterId strfmt.UUID
	var updateReply *UpdateReply
	var updateErr error
	var expectedReply *expect

	BeforeEach(func() {
		db = prepareDB()
		state = &Manager{resetting: NewResettingState(getTestLog(), db)}

		id = strfmt.UUID(uuid.New().String())
		clusterId = strfmt.UUID(uuid.New().String())
		host = getTestHost(id, clusterId, currentState)
		Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		expectedReply = &expect{expectedState: currentState}
	})

	It("update_hw_info", func() {
		updateReply, updateErr = state.UpdateHwInfo(ctx, &host, "some hw info")
		expectedReply.expectError = true
		expectedReply.postCheck = func() {
			h := getHost(id, clusterId, db)
			Expect(h.HardwareInfo).Should(Equal(defaultHwInfo))
		}
	})

	Context("refresh_status", func() {
		It("keep_alive", func() {
			updateReply, updateErr = state.RefreshStatus(ctx, &host, nil)
		})
		It("keep_alive_timeout", func() {
			host.UpdatedAt = strfmt.DateTime(time.Now().Add(-time.Hour))
			updateReply, updateErr = state.RefreshStatus(ctx, &host, nil)
		})
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
