package cluster

import (
	"context"
	"net/http"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Transition tests", func() {
	var (
		ctx       = context.Background()
		capi      API
		db        *gorm.DB
		clusterId strfmt.UUID
	)

	BeforeEach(func() {
		db = prepareDB()
		capi = NewManager(defaultTestConfig, getTestLog(), db, nil)
		clusterId = strfmt.UUID(uuid.New().String())
	})

	Context("cancel_installation", func() {
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
	})
	Context("cancel_installation", func() {
		It("complete installation success", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(models.ClusterStatusFinalizing)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(capi.CompleteInstallation(ctx, &c, true, clusterStatusInstalled)).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusInstalled))
		})

		It("complete installation failed", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(models.ClusterStatusFinalizing)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			Expect(capi.CompleteInstallation(ctx, &c, false, "aaaa")).ShouldNot(HaveOccurred())
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusError))
			Expect(swag.StringValue(c.StatusInfo)).Should(Equal("aaaa"))

		})

		It("complete_installation_conflict", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(clusterStatusInstalling)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			replay := capi.CompleteInstallation(ctx, &c, true, "")
			Expect(replay).Should(HaveOccurred())
			Expect(int(replay.StatusCode())).Should(Equal(http.StatusConflict))
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusInstalling))
		})

		It("complete_installation_conflict_failed", func() {
			c := common.Cluster{
				Cluster: models.Cluster{ID: &clusterId, Status: swag.String(clusterStatusInstalling)},
			}
			Expect(db.Create(&c).Error).ShouldNot(HaveOccurred())
			replay := capi.CompleteInstallation(ctx, &c, false, "")
			Expect(replay).Should(HaveOccurred())
			Expect(int(replay.StatusCode())).Should(Equal(http.StatusConflict))
			Expect(swag.StringValue(c.Status)).Should(Equal(clusterStatusInstalling))
		})
	})
	AfterEach(func() {
		db.Close()
	})
})
