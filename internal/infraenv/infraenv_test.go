package infraenv

import (
	"context"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

var _ = Describe("DeregisterInfraEnv", func() {
	var (
		ctrl         *gomock.Controller
		ctx          = context.Background()
		db           *gorm.DB
		state        API
		infraEnv     common.InfraEnv
		dbName       string
		mockS3Client *s3wrapper.MockAPI
	)

	registerInfraEnv := func() common.InfraEnv {
		id := strfmt.UUID(uuid.New().String())
		ie := common.InfraEnv{InfraEnv: models.InfraEnv{
			ID: &id,
		}}
		Expect(db.Create(&ie).Error).ShouldNot(HaveOccurred())
		return ie
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		state = NewManager(common.GetTestLog(), db, mockS3Client)
		infraEnv = registerInfraEnv()
	})

	It("deletes the infraEnv", func() {
		Expect(state.DeregisterInfraEnv(ctx, *infraEnv.ID)).ShouldNot(HaveOccurred())
		_, err := common.GetInfraEnvFromDB(db, *infraEnv.ID)
		Expect(err).Should(HaveOccurred())
		Expect(errors.Is(err, gorm.ErrRecordNotFound)).Should(Equal(true))
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
})

var _ = Describe("Delete inactive infraenvs", func() {
	var (
		ctrl         *gomock.Controller
		ctx          = context.Background()
		db           *gorm.DB
		state        API
		infraEnv     common.InfraEnv
		dbName       string
		mockS3Client *s3wrapper.MockAPI
	)

	registerInfraEnv := func(clusterId strfmt.UUID) common.InfraEnv {
		id := strfmt.UUID(uuid.New().String())
		ie := common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:        &id,
			ClusterID: clusterId,
		}}
		Expect(db.Create(&ie).Error).ShouldNot(HaveOccurred())
		return ie
	}

	addHosts := func(clusterId, infraEnvId strfmt.UUID) {
		var hostId strfmt.UUID
		var host models.Host
		for i := 0; i < 3; i++ {
			hostId = strfmt.UUID(uuid.New().String())
			host = models.Host{
				ID:         &hostId,
				InfraEnvID: infraEnvId,
				ClusterID:  &clusterId,
				Role:       models.HostRoleMaster,
				Status:     swag.String("known"),
				Inventory:  common.GenerateTestDefaultInventory(),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		}
	}

	wasDeleted := func(db *gorm.DB, infraEnv strfmt.UUID) bool {
		_, err := common.GetInfraEnvFromDBWhere(db, "id = ?", infraEnv.String())
		return errors.Is(err, gorm.ErrRecordNotFound)
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, dbName = common.PrepareTestDB()
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		state = NewManager(common.GetTestLog(), db, mockS3Client)
		infraEnv = registerInfraEnv("")
	})

	// avoid races on slow/fast systems, could be any amount of time
	nowPlus5sec := func() strfmt.DateTime {
		return strfmt.DateTime(time.Now().Add(5 * time.Second))
	}

	It("Deregister inactive infraEnv", func() {
		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, nowPlus5sec())).ShouldNot(HaveOccurred())
		Expect(wasDeleted(db, *infraEnv.ID)).To(BeTrue())
	})

	It("Deregister inactive infraEnv with hosts", func() {
		addHosts("", *infraEnv.ID)
		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, nowPlus5sec())).ShouldNot(HaveOccurred())
		Expect(wasDeleted(db, *infraEnv.ID)).To(BeTrue())
		hosts, err := common.GetInfraEnvHostsFromDB(db, *infraEnv.ID)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(hosts)).Should(Equal(0))
	})

	It("Deregister inactive infraEnv with non existing cluster", func() {
		infraEnv2 := registerInfraEnv(strfmt.UUID(uuid.New().String()))
		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, nowPlus5sec())).ShouldNot(HaveOccurred())
		Expect(wasDeleted(db, *infraEnv2.ID)).To(BeTrue())
		Expect(wasDeleted(db, *infraEnv.ID)).To(BeTrue())
	})

	It("Deregister inactive infraEnv with non existing cluster with hosts", func() {
		clusterId := strfmt.UUID(uuid.New().String())
		infraEnv2 := registerInfraEnv(clusterId)
		addHosts(clusterId, *infraEnv2.ID)
		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, nowPlus5sec())).ShouldNot(HaveOccurred())
		Expect(wasDeleted(db, *infraEnv2.ID)).To(BeTrue())
		Expect(wasDeleted(db, *infraEnv.ID)).To(BeTrue())
		hosts, err := common.GetInfraEnvHostsFromDB(db, *infraEnv2.ID)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(len(hosts)).Should(Equal(0))
	})

	It("Do nothing, inactive infraEnv with existing cluster", func() {
		clusterId := strfmt.UUID(uuid.New().String())
		infraEnv2 := registerInfraEnv(clusterId)
		cluster := common.Cluster{Cluster: models.Cluster{
			ID: &clusterId,
		}}
		Expect(db.Create(&cluster).Error).ShouldNot(HaveOccurred())
		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, nowPlus5sec())).ShouldNot(HaveOccurred())
		Expect(wasDeleted(db, *infraEnv2.ID)).To(BeFalse())
		Expect(wasDeleted(db, *infraEnv.ID)).To(BeTrue())
	})

	It("Do nothing, active infraEnv", func() {
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Times(0)
		lastActive := strfmt.DateTime(time.Now().Add(-time.Hour))
		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, lastActive)).ShouldNot(HaveOccurred())
		Expect(wasDeleted(db, *infraEnv.ID)).To(BeFalse())
	})

	It("Delete inactive infraEnvs with new infraEnvs", func() {
		inactiveInfraEnv1 := registerInfraEnv("")
		inactiveInfraEnv2 := registerInfraEnv("")
		inactiveInfraEnv3 := registerInfraEnv("")

		// To verify that lastActive is greater than the updatedAt field of inactiveCluster3
		time.Sleep(time.Millisecond)
		lastActive := strfmt.DateTime(time.Now())

		activeInfraEnv1 := registerInfraEnv("")
		activeInfraEnv2 := registerInfraEnv("")
		activeInfraEnv3 := registerInfraEnv("")

		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, lastActive)).ShouldNot(HaveOccurred())

		Expect(wasDeleted(db, *inactiveInfraEnv1.ID)).To(BeTrue())
		Expect(wasDeleted(db, *inactiveInfraEnv2.ID)).To(BeTrue())
		Expect(wasDeleted(db, *inactiveInfraEnv3.ID)).To(BeTrue())

		Expect(wasDeleted(db, *activeInfraEnv1.ID)).To(BeFalse())
		Expect(wasDeleted(db, *activeInfraEnv2.ID)).To(BeFalse())
		Expect(wasDeleted(db, *activeInfraEnv3.ID)).To(BeFalse())
	})

	It("Delete inactive infraEnvs with new infraEnvs - limited", func() {
		inactiveInfraEnv1 := registerInfraEnv("")
		inactiveInfraEnv2 := registerInfraEnv("")
		inactiveInfraEnv3 := registerInfraEnv("")
		inactiveInfraEnv4 := registerInfraEnv("")
		inactiveInfraEnv5 := registerInfraEnv("")
		inactiveInfraEnv6 := registerInfraEnv("")

		// To verify that lastActive is greater than the updatedAt field of inactiveInfraEnv3
		time.Sleep(time.Millisecond)
		lastActive := strfmt.DateTime(time.Now())

		Expect(state.DeleteOrphanInfraEnvs(ctx, 3, lastActive)).ShouldNot(HaveOccurred())

		Expect(wasDeleted(db, *inactiveInfraEnv1.ID)).To(BeTrue())
		Expect(wasDeleted(db, *inactiveInfraEnv2.ID)).To(BeTrue())

		Expect(wasDeleted(db, *inactiveInfraEnv3.ID)).To(BeFalse())
		Expect(wasDeleted(db, *inactiveInfraEnv4.ID)).To(BeFalse())
		Expect(wasDeleted(db, *inactiveInfraEnv5.ID)).To(BeFalse())
		Expect(wasDeleted(db, *inactiveInfraEnv6.ID)).To(BeFalse())

	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
})
