package common

import (
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

var _ = Describe("GetHostCountByRole", func() {
	var (
		db        *gorm.DB
		dbName    string
		clusterID = strfmt.UUID(uuid.New().String())
		HostID1   = strfmt.UUID(uuid.New().String())
		HostID2   = strfmt.UUID(uuid.New().String())
		HostID3   = strfmt.UUID(uuid.New().String())
		HostID4   = strfmt.UUID(uuid.New().String())
		HostID5   = strfmt.UUID(uuid.New().String())
		HostID6   = strfmt.UUID(uuid.New().String())
		HostID7   = strfmt.UUID(uuid.New().String())
		HostID8   = strfmt.UUID(uuid.New().String())
	)

	BeforeEach(func() {
		db, dbName = PrepareTestDB()
	})

	AfterEach(func() {
		DeleteTestDB(db, dbName)
	})

	Context("should succeed", func() {
		Context("with suggested role", func() {
			It("and some records found", func() {
				cluster := Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
						Hosts: []*models.Host{
							{
								ID:            &HostID1,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
							{
								ID:            &HostID2,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID3,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID4,
								Role:          models.HostRoleMaster,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID5,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID6,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID7,
								Role:          models.HostRoleWorker,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID8,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
						},
					},
				}

				err := db.Create(&cluster).Error
				Expect(err).ToNot(HaveOccurred())

				masterCount, err := GetHostCountByRole(db, clusterID, models.HostRoleMaster, true)
				Expect(err).ToNot(HaveOccurred())

				workerCount, err := GetHostCountByRole(db, clusterID, models.HostRoleWorker, true)
				Expect(err).ToNot(HaveOccurred())

				autoAssignCount, err := GetHostCountByRole(db, clusterID, models.HostRoleAutoAssign, true)
				Expect(err).ToNot(HaveOccurred())

				Expect(*masterCount).To(BeEquivalentTo(3))
				Expect(*workerCount).To(BeEquivalentTo(3))
				Expect(*autoAssignCount).To(BeEquivalentTo(2))
			})

			It("no records found", func() {
				cluster := Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
						Hosts: []*models.Host{
							{
								ID:            &HostID1,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
							{
								ID:            &HostID2,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID3,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID4,
								Role:          models.HostRoleMaster,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID5,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID6,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID7,
								Role:          models.HostRoleWorker,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID8,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
						},
					},
				}

				err := db.Create(&cluster).Error
				Expect(err).ToNot(HaveOccurred())

				bootstrapHostCount, err := GetHostCountByRole(db, clusterID, models.HostRoleBootstrap, true)
				Expect(err).ToNot(HaveOccurred())

				Expect(*bootstrapHostCount).To(BeEquivalentTo(0))
			})
		})

		Context("with non-suggested role", func() {
			It("and some records found", func() {
				cluster := Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
						Hosts: []*models.Host{
							{
								ID:            &HostID1,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
							{
								ID:            &HostID2,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID3,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID4,
								Role:          models.HostRoleMaster,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID5,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID6,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID7,
								Role:          models.HostRoleWorker,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID8,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
						},
					},
				}

				err := db.Create(&cluster).Error
				Expect(err).ToNot(HaveOccurred())

				masterCount, err := GetHostCountByRole(db, clusterID, models.HostRoleMaster, false)
				Expect(err).ToNot(HaveOccurred())

				workerCount, err := GetHostCountByRole(db, clusterID, models.HostRoleWorker, false)
				Expect(err).ToNot(HaveOccurred())

				autoAssignCount, err := GetHostCountByRole(db, clusterID, models.HostRoleAutoAssign, false)
				Expect(err).ToNot(HaveOccurred())

				Expect(*masterCount).To(BeEquivalentTo(1))
				Expect(*workerCount).To(BeEquivalentTo(1))
				Expect(*autoAssignCount).To(BeEquivalentTo(6))
			})

			It("no records found", func() {
				cluster := Cluster{
					Cluster: models.Cluster{
						ID: &clusterID,
						Hosts: []*models.Host{
							{
								ID:            &HostID1,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
							{
								ID:            &HostID2,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID3,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID4,
								Role:          models.HostRoleMaster,
								SuggestedRole: models.HostRoleMaster,
							},
							{
								ID:            &HostID5,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID6,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID7,
								Role:          models.HostRoleWorker,
								SuggestedRole: models.HostRoleWorker,
							},
							{
								ID:            &HostID8,
								Role:          models.HostRoleAutoAssign,
								SuggestedRole: models.HostRoleAutoAssign,
							},
						},
					},
				}

				err := db.Create(&cluster).Error
				Expect(err).ToNot(HaveOccurred())

				bootstrapHostCount, err := GetHostCountByRole(db, clusterID, models.HostRoleBootstrap, false)
				Expect(err).ToNot(HaveOccurred())

				Expect(*bootstrapHostCount).To(BeEquivalentTo(0))
			})
		})
	})
})
