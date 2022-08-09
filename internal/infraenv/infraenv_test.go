package infraenv

import (
	"context"
	"fmt"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
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
		db, _ = common.PrepareTestDB()
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
})

var _ = Describe("Delete inactive infraenvs", func() {
	var (
		ctrl         *gomock.Controller
		ctx          = context.Background()
		db           *gorm.DB
		state        API
		mockS3Client *s3wrapper.MockAPI
	)

	const emptyClusterID = ""

	var (
		defaultTime           time.Time = time.Date(2012, time.December, 21, 5, 1, 2, 6, time.UTC)
		timeBeforeDefaultTime time.Time = defaultTime.Add(-time.Second)
		timeAfterDefaultTime  time.Time = defaultTime.Add(time.Second)
	)

	registerInfraEnvAtTime := func(clusterId strfmt.UUID, time time.Time) common.InfraEnv {
		id := strfmt.UUID(uuid.New().String())

		ie := common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:        &id,
			ClusterID: clusterId,
			UpdatedAt: &time,
		}}
		Expect(db.Create(&ie).Error).ShouldNot(HaveOccurred())
		return ie
	}

	registerInfraEnv := func(clusterId strfmt.UUID) common.InfraEnv {
		id := strfmt.UUID(uuid.New().String())

		ie := common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:        &id,
			ClusterID: clusterId,
			UpdatedAt: &defaultTime,
		}}
		Expect(db.Create(&ie).Error).ShouldNot(HaveOccurred())
		return ie
	}

	addHosts := func(clusterId strfmt.UUID, infraEnvId *strfmt.UUID) {
		var hostId strfmt.UUID
		var host models.Host
		for i := 0; i < 3; i++ {
			hostId = strfmt.UUID(uuid.New().String())
			host = models.Host{
				ID:         &hostId,
				InfraEnvID: *infraEnvId,
				ClusterID:  &clusterId,
				Role:       models.HostRoleMaster,
				Status:     swag.String("known"),
				Inventory:  common.GenerateTestDefaultInventory(),
			}
			Expect(db.Create(&host).Error).ShouldNot(HaveOccurred())
		}
	}

	newClusterID := func() strfmt.UUID {
		return strfmt.UUID(uuid.New().String())
	}

	expectWasDeleted := func(infraEnv *strfmt.UUID) {
		Expect(common.GetInfraEnvFromDBWhere(db, "id = ?", infraEnv.String())).Error().To(
			Equal(gorm.ErrRecordNotFound),
			fmt.Sprintf("Infraenv %s is still in the database even though it should have been deleted", infraEnv.String()))
	}

	expectExists := func(infraEnv *strfmt.UUID) {
		Expect(common.GetInfraEnvFromDBWhere(db, "id = ?", infraEnv.String())).Error().Should(Succeed())
	}

	getInfraenvsWithTimestamp := func(timestamp time.Time) []*common.InfraEnv {
		infraenvs, err := common.GetInfraEnvsFromDBWhere(db, "updated_at = ?", timestamp)
		Expect(err).ShouldNot(HaveOccurred())
		return infraenvs
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		db, _ = common.PrepareTestDB()
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		state = NewManager(common.GetTestLog(), db, mockS3Client)
	})

	It("Deregister inactive infraEnv", func() {
		infraEnv := registerInfraEnv(emptyClusterID)

		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, strfmt.DateTime(timeAfterDefaultTime))).Should(Succeed())

		expectWasDeleted(infraEnv.ID)
	})

	It("Deregister inactive infraEnv with hosts", func() {
		infraEnv := registerInfraEnv(emptyClusterID)
		addHosts(emptyClusterID, infraEnv.ID)

		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, strfmt.DateTime(timeAfterDefaultTime))).Should(Succeed())

		expectWasDeleted(infraEnv.ID)
		Expect(common.GetInfraEnvHostsFromDB(db, *infraEnv.ID)).Should(BeEmpty())
	})

	It("Deregister inactive infraEnv with non existing cluster", func() {
		infraEnv := registerInfraEnv(emptyClusterID)
		infraEnv2 := registerInfraEnv(newClusterID())

		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, strfmt.DateTime(timeAfterDefaultTime))).Should(Succeed())

		expectWasDeleted(infraEnv.ID)
		expectWasDeleted(infraEnv2.ID)
	})

	It("Deregister inactive infraEnv with non existing cluster with hosts", func() {
		infraEnv := registerInfraEnv(emptyClusterID)
		clusterID := newClusterID()
		infraEnv2 := registerInfraEnv(clusterID)
		addHosts(clusterID, infraEnv2.ID)

		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, strfmt.DateTime(timeAfterDefaultTime))).Should(Succeed())

		expectWasDeleted(infraEnv2.ID)
		expectWasDeleted(infraEnv.ID)
		Expect(common.GetInfraEnvHostsFromDB(db, *infraEnv2.ID)).Should(BeEmpty())
	})

	It("Do nothing, inactive infraEnv with existing cluster", func() {
		infraEnv := registerInfraEnv(emptyClusterID)
		clusterId := newClusterID()
		cluster := common.Cluster{Cluster: models.Cluster{ID: &clusterId}}
		Expect(db.Create(&cluster).Error).Should(Succeed())
		infraEnv2 := registerInfraEnv(clusterId)

		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, strfmt.DateTime(timeAfterDefaultTime))).Should(Succeed())

		expectWasDeleted(infraEnv.ID)
		expectExists(infraEnv2.ID)
	})

	It("Do nothing, active infraEnv", func() {
		infraEnv := registerInfraEnv(emptyClusterID)
		mockS3Client.EXPECT().DoesObjectExist(gomock.Any(), gomock.Any()).Times(0)
		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, strfmt.DateTime(timeBeforeDefaultTime))).Should(Succeed())
		expectExists(infraEnv.ID)
	})

	It("Delete inactive infraEnvs with new infraEnvs", func() {
		t1 := defaultTime
		t2 := defaultTime.Add(time.Second * 1)
		t3 := defaultTime.Add(time.Second * 2)

		inactiveInfraEnv1 := registerInfraEnvAtTime(emptyClusterID, t1)
		inactiveInfraEnv2 := registerInfraEnvAtTime(emptyClusterID, t1)
		inactiveInfraEnv3 := registerInfraEnvAtTime(emptyClusterID, t1)

		activeInfraEnv1 := registerInfraEnvAtTime(emptyClusterID, t3)
		activeInfraEnv2 := registerInfraEnvAtTime(emptyClusterID, t3)
		activeInfraEnv3 := registerInfraEnvAtTime(emptyClusterID, t3)

		Expect(state.DeleteOrphanInfraEnvs(ctx, 10, strfmt.DateTime(t2))).Should(Succeed())

		expectWasDeleted(inactiveInfraEnv1.ID)
		expectWasDeleted(inactiveInfraEnv2.ID)
		expectWasDeleted(inactiveInfraEnv3.ID)

		expectExists(activeInfraEnv1.ID)
		expectExists(activeInfraEnv2.ID)
		expectExists(activeInfraEnv3.ID)
	})

	It("Delete inactive infraEnvs with new infraEnvs - limited", func() {
		t1 := defaultTime
		t2 := defaultTime.Add(time.Second * 1)
		t3 := defaultTime.Add(time.Second * 2)

		numT1 := 4
		for i := 0; i < numT1; i++ {
			registerInfraEnvAtTime(emptyClusterID, t1)
		}

		numT3 := 2
		for i := 0; i < numT3; i++ {
			registerInfraEnvAtTime(emptyClusterID, t3)
		}

		maxDelete := 3
		Expect(state.DeleteOrphanInfraEnvs(ctx, maxDelete, strfmt.DateTime(t2))).Should(Succeed())

		Expect(getInfraenvsWithTimestamp(t1)).Should(HaveLen(numT1 - maxDelete))
		Expect(getInfraenvsWithTimestamp(t3)).Should(HaveLen(numT3))
	})
})
