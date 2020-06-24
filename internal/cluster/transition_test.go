package cluster

import (
	"context"
	"net/http"

	"github.com/filanov/bm-inventory/internal/common"

	"github.com/go-openapi/swag"

	. "github.com/onsi/gomega"

	"github.com/filanov/bm-inventory/models"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
)

var _ = Describe("CancelInstallation", func() {
	var (
		ctx       = context.Background()
		capi      API
		db        *gorm.DB
		clusterId strfmt.UUID
	)

	BeforeEach(func() {
		db = prepareDB()
		capi = NewManager(getTestLog(), db, nil)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	It("cancel_installation", func() {
		c := common.Cluster{
			Cluster: models.Cluster{ID: &clusterId, Status: swag.String(clusterStatusInstalling)},
		}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		Expect(capi.CancelInstallation(ctx, &c, "", db)).ShouldNot(HaveOccurred())
		Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusError))
	})

	It("cancel_installation_conflict", func() {
		c := common.Cluster{
			Cluster: models.Cluster{ID: &clusterId, Status: swag.String(clusterStatusInsufficient)},
		}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		replay := capi.CancelInstallation(ctx, &c, "", db)
		Expect(replay).Should(HaveOccurred())
		Expect(int(replay.StatusCode())).Should(Equal(http.StatusConflict))
		Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusInsufficient))
	})

	It("cancel_failed_installation", func() {
		c := common.Cluster{
			Cluster: models.Cluster{
				ID:         &clusterId,
				StatusInfo: swag.String("original error"),
				Status:     swag.String(clusterStatusError)},
		}
		Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
		Expect(capi.CancelInstallation(ctx, &c, "", db)).ShouldNot(HaveOccurred())
		Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusError))
		Expect(swag.StringValue(c.StatusInfo)).Should(Equal("original error"))
	})

	AfterEach(func() {
		db.Close()
	})
})
