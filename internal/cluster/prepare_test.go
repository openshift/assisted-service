package cluster

import (
	"context"
	"time"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("prepare-for-installation refresh status", func() {
	var (
		ctx       = context.Background()
		capi      API
		db        *gorm.DB
		clusterId strfmt.UUID
		cl        common.Cluster
	)
	BeforeEach(func() {
		db = prepareDB()
		cfg := Config{}
		Expect(envconfig.Process("myapp", &cfg)).NotTo(HaveOccurred())
		capi = NewManager(cfg, getTestLog(), db, nil)
		clusterId = strfmt.UUID(uuid.New().String())
		cl = common.Cluster{
			Cluster: models.Cluster{
				ID:              &clusterId,
				Status:          swag.String(models.ClusterStatusPreparingForInstallation),
				StatusUpdatedAt: strfmt.DateTime(time.Now()),
			},
		}
		Expect(db.Create(&cl).Error).NotTo(HaveOccurred())
	})

	It("no change", func() {
		Expect(db.Take(&cl, "id = ?", clusterId).Error).NotTo(HaveOccurred())
		refreshedCluster, err := capi.RefreshStatus(ctx, &cl, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(*refreshedCluster.Status).To(Equal(models.ClusterStatusPreparingForInstallation))
	})

	It("timeout", func() {
		cl.StatusUpdatedAt = strfmt.DateTime(time.Now().Add(-15 * time.Minute))
		refreshedCluster, err := capi.RefreshStatus(ctx, &cl, db)
		Expect(err).NotTo(HaveOccurred())
		Expect(*refreshedCluster.Status).To(Equal(models.ClusterStatusError))
	})
})
