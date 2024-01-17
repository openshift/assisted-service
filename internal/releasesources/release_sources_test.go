package releasesources

import (
	"testing"

	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

type CoreOpenshiftVersion struct {
	models.ReleaseImage
	Channel string
}

func TestHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "releasesources")
}

func setGetReleasesMock(releasesMock *MockOpenShiftReleasesAPIClientInterface, testsParams []RequestResponseParameters) {
	for _, testParams := range testsParams {
		releasesGraph, err := getExpectedReleasesGraphForValidParams(testParams.Channel, testParams.Version, testParams.CPUArchitecture)
		releasesMock.EXPECT().
			GetReleases(testParams.Channel, testParams.Version, testParams.CPUArchitecture).
			Return(releasesGraph, err).
			AnyTimes()
	}
}

func setGetSupportLevelsMock(supportLevelMock *MockOpenShiftSupportLevelAPIClientInterface, openshiftMajorVersion string) {
	supporLevelGraph, err := getExpectedSupportLevelsGraph(openshiftMajorVersion)
	supportLevelMock.EXPECT().
		GetSupportLevels(openshiftMajorVersion).
		Return(supporLevelGraph, err).
		AnyTimes()
}

func getValidReleaseSources() models.ReleaseSources {
	return models.ReleaseSources{
		{
			OpenshiftVersion: swag.String("4.10"),
			UpgradeChannels: []*models.UpgradeChannel{
				{
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					Channels:        []string{common.OpenshiftReleaseChannelStable},
				},
			},
		},
		{
			OpenshiftVersion: swag.String("4.12"),
			UpgradeChannels: []*models.UpgradeChannel{
				{
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					Channels:        []string{common.OpenshiftReleaseChannelStable},
				},
			},
		},
		{
			OpenshiftVersion: swag.String("4.13"),
			UpgradeChannels: []*models.UpgradeChannel{
				{
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					Channels:        []string{common.OpenshiftReleaseChannelStable},
				},
				{
					CPUArchitecture: swag.String(common.S390xCPUArchitecture),
					Channels:        []string{common.OpenshiftReleaseChannelStable},
				},
			},
		},
		{
			OpenshiftVersion: swag.String("4.14"),
			UpgradeChannels: []*models.UpgradeChannel{
				{
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					Channels:        []string{common.OpenshiftReleaseChannelStable, common.OpenshiftReleaseChannelCandidate},
				},
				{
					CPUArchitecture: swag.String("ppc64le"),
					Channels:        []string{common.OpenshiftReleaseChannelStable, common.OpenshiftReleaseChannelCandidate},
				},
			},
		},
		{
			OpenshiftVersion: swag.String("4.15"),
			UpgradeChannels: []*models.UpgradeChannel{
				{
					CPUArchitecture: swag.String(common.X86CPUArchitecture),
					Channels:        []string{common.OpenshiftReleaseChannelCandidate},
				},
			},
		},
		{
			OpenshiftVersion: swag.String("4.16"),
			UpgradeChannels: []*models.UpgradeChannel{
				{
					CPUArchitecture: swag.String(common.MultiCPUArchitecture),
					Channels:        []string{common.OpenshiftReleaseChannelCandidate},
				},
			},
		},
	}
}

var _ = Describe("SyncReleaseImages", func() {
	var (
		db                      *gorm.DB
		tx                      *gorm.DB
		dbName                  string
		ctrl                    *gomock.Controller
		handler                 releaseSourcesHandler
		releasesClientMock      *MockOpenShiftReleasesAPIClientInterface
		supportLevelsClientMock *MockOpenShiftSupportLevelAPIClientInterface
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		releasesClientMock = NewMockOpenShiftReleasesAPIClientInterface(ctrl)
		supportLevelsClientMock = NewMockOpenShiftSupportLevelAPIClientInterface(ctrl)
		tx = db.Begin().Debug()
		handler = NewReleaseSourcesHandler(
			getValidReleaseSources(),
			common.GetTestLog(),
			db,
			Config{OpenshiftMajorVersion: "4"},
		)
		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})

	Context("SyncReleaseImages", func() {
		// Latest x86_64 stable release
		It("Should set the default release correctly", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: swag.String("4.14"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelStable},
						},
						{
							CPUArchitecture: swag.String(common.ARM64CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelStable},
						},
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelCandidate},
						},
					},
				},
			}

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelStable, "4.14", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.4"},
							{Version: "4.14.2"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelStable, "4.14", common.ARM64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.7"},
							{Version: "4.14.5"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelCandidate, "4.14", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.6"},
							{Version: "4.14.3"},
						},
					}, nil).
				Times(1)

			setGetSupportLevelsMock(supportLevelsClientMock, "4")

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).ToNot(HaveOccurred())

			dbReleases := []*common.ReleaseImage{}
			err = db.Find(&dbReleases, `"default" = ?`, true).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(*dbReleases[0].Version).To(Equal("4.14.4"))
		})

		It("Should set releases support level correctly", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: swag.String("4.15"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelStable},
						},
					},
				},
				{
					OpenshiftVersion: swag.String("4.13"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelStable},
						},
					},
				},
				{
					OpenshiftVersion: swag.String("4.11"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelStable},
						},
					},
				},
				{
					OpenshiftVersion: swag.String("4.9"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelStable},
						},
					},
				},
			}

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelStable, "4.15", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.15.0-ec.3"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelStable, "4.13", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.13.7"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelStable, "4.11", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.11.6"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelStable, "4.9", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.9.3"},
						},
					}, nil).
				Times(1)

			setGetSupportLevelsMock(supportLevelsClientMock, "4")

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).ToNot(HaveOccurred())

			dbReleases := []*common.ReleaseImage{}

			err = db.Find(&dbReleases, "support_level = ?", "beta").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(*dbReleases[0].Version).To(Equal("4.15.0-ec.3"))

			err = db.Find(&dbReleases, "support_level = ?", "production").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(*dbReleases[0].Version).To(Equal("4.13.7"))

			err = db.Find(&dbReleases, "support_level = ?", "maintenance").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(*dbReleases[0].Version).To(Equal("4.11.6"))

			err = db.Find(&dbReleases, "support_level = ?", "end of life").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(*dbReleases[0].Version).To(Equal("4.9.3"))
		})

		It("Should not have the same release in different channels", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: swag.String("4.14"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.ARM64CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelStable, common.OpenshiftReleaseChannelCandidate},
						},
					},
				},
			}

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelStable, "4.14", common.ARM64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.2"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelCandidate, "4.14", common.ARM64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.2"},
							{Version: "4.14.3"},
						},
					}, nil).
				Times(1)

			setGetSupportLevelsMock(supportLevelsClientMock, "4")

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).ToNot(HaveOccurred())

			dbReleases, err := common.GetReleaseImagesFromDBWhere(
				db,
				"version = ? AND channel = ? AND cpu_architecture = ?",
				"4.14.2",
				common.OpenshiftReleaseChannelCandidate,
				common.ARM64CPUArchitecture,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(dbReleases).To(BeEmpty())

			dbReleases, err = common.GetReleaseImagesFromDBWhere(
				db,
				"version = ? AND channel = ? AND cpu_architecture = ?",
				"4.14.2",
				common.OpenshiftReleaseChannelStable,
				common.ARM64CPUArchitecture,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))

			dbReleases, err = common.GetReleaseImagesFromDBWhere(
				db,
				"version = ? AND channel = ? AND cpu_architecture = ?",
				"4.14.3",
				common.OpenshiftReleaseChannelCandidate,
				common.ARM64CPUArchitecture,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
		})

		It("Should not have the same release twice", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: swag.String("4.14"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.ARM64CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelStable},
						},
					},
				},
			}

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelStable, "4.14", common.ARM64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.2"},
							{Version: "4.14.2"},
						},
					}, nil).
				Times(1)

			setGetSupportLevelsMock(supportLevelsClientMock, "4")

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).ToNot(HaveOccurred())

			dbReleases, err := common.GetReleaseImagesFromDBWhere(
				db,
				"version = ? AND channel = ? AND cpu_architecture = ?",
				"4.14.2",
				common.OpenshiftReleaseChannelStable,
				common.ARM64CPUArchitecture,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
		})

		It("Should be successfull with valid release images and valid response complex scenario", func() {
			setGetReleasesMock(releasesClientMock, getValidRequestResponseParameters())
			setGetSupportLevelsMock(supportLevelsClientMock, "4")

			expectedResult := []CoreOpenshiftVersion{
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.10"),
						Version:          swag.String("4.10.1"),
						CPUArchitecture:  swag.String(common.X86CPUArchitecture),
						SupportLevel:     models.OpenshiftVersionSupportLevelEndOfLife,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.10.1", common.X86CPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelStable,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.12"),
						Version:          swag.String("4.12.1"),
						CPUArchitecture:  swag.String(common.X86CPUArchitecture),
						SupportLevel:     models.OpenshiftVersionSupportLevelMaintenance,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.12.1", common.X86CPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelStable,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.13"),
						Version:          swag.String("4.13.1"),
						CPUArchitecture:  swag.String(common.X86CPUArchitecture),
						SupportLevel:     models.ReleaseImageSupportLevelProduction,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.13.1", common.X86CPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelStable,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.13"),
						Version:          swag.String("4.13.17"),
						CPUArchitecture:  swag.String(common.X86CPUArchitecture),
						SupportLevel:     models.ReleaseImageSupportLevelProduction,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.13.17", common.X86CPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelStable,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.13"),
						Version:          swag.String("4.13.1"),
						CPUArchitecture:  swag.String(common.S390xCPUArchitecture),
						SupportLevel:     models.ReleaseImageSupportLevelProduction,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.13.1", common.S390xCPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelStable,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.13"),
						Version:          swag.String("4.13.19"),
						CPUArchitecture:  swag.String(common.S390xCPUArchitecture),
						SupportLevel:     models.ReleaseImageSupportLevelProduction,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.13.19", common.S390xCPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelStable,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.14"),
						Version:          swag.String("4.14.0"),
						CPUArchitecture:  swag.String(common.X86CPUArchitecture),
						SupportLevel:     models.ReleaseImageSupportLevelProduction,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.14.0", common.X86CPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelStable,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.14"),
						Version:          swag.String("4.14.1"),
						CPUArchitecture:  swag.String(common.X86CPUArchitecture),
						SupportLevel:     models.ReleaseImageSupportLevelProduction,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.14.1", common.X86CPUArchitecture)),
						Default:          true,
					},
					Channel: common.OpenshiftReleaseChannelStable,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.14"),
						Version:          swag.String("4.14.0-rc.1"),
						CPUArchitecture:  swag.String(common.X86CPUArchitecture),
						SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.14.0-rc.1", common.X86CPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelCandidate,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.14"),
						Version:          swag.String("4.14.0-ec.2"),
						CPUArchitecture:  swag.String(common.X86CPUArchitecture),
						SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.14.0-ec.2", common.X86CPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelCandidate,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.14"),
						Version:          swag.String("4.14.0"),
						CPUArchitecture:  swag.String(common.PowerCPUArchitecture),
						SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.14.0", common.PowerCPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelCandidate,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.14"),
						Version:          swag.String("4.14.1"),
						CPUArchitecture:  swag.String(common.PowerCPUArchitecture),
						SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.14.1", common.PowerCPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelCandidate,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.15"),
						Version:          swag.String("4.15.0-ec.2"),
						CPUArchitecture:  swag.String(common.X86CPUArchitecture),
						SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.15.0-ec.2", common.X86CPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelCandidate,
				},
				{
					ReleaseImage: models.ReleaseImage{
						OpenshiftVersion: swag.String("4.16-multi"),
						Version:          swag.String("4.16.0-ec.2-multi"),
						CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
						SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
						URL:              swag.String(common.GetURLForReleaseImageInSaaS("4.16.0-ec.2", common.MultiCPUArchitecture)),
						Default:          false,
					},
					Channel: common.OpenshiftReleaseChannelCandidate,
				},
			}

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).ToNot(HaveOccurred())

			var results []CoreOpenshiftVersion
			err = db.Model(&common.ReleaseImage{}).
				Omit("id").Omit("created_at").
				Find(&results).Error

			Expect(err).ToNot(HaveOccurred())

			for _, expectedResult := range expectedResult {
				Expect(results).To(ContainElement(expectedResult))
			}
			Expect(len(expectedResult)).To(Equal(len(results)))
		})

		It("Should cause an error with invalid release sources - invalid cpu architecture", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: swag.String("4.12"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String("badCPUArchitecture"),
							Channels:        []string{common.X86CPUArchitecture},
						},
					},
				},
			}

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).To(HaveOccurred())
			tx.Rollback()
		})

		It("Should not cause an error with empty release sources", func() {
			handler.releaseSources = models.ReleaseSources{}

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should cause an error with invalid release sources - invalid openshift version", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: swag.String("invalidVersion"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelStable},
						},
					},
				},
			}

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).To(HaveOccurred())
			tx.Rollback()
		})

		It("Should not cause an error with valid release sources but no results", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: swag.String("4.16"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []string{common.OpenshiftReleaseChannelStable},
						},
					},
				},
			}

			releasesClientMock.EXPECT().
				GetReleases(common.OpenshiftReleaseChannelStable, "4.16", common.AMD64CPUArchitecture).
				Return(&ReleaseGraph{}, nil).
				Times(1)
			setGetSupportLevelsMock(supportLevelsClientMock, "4")

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).ToNot(HaveOccurred())
			tx.Rollback()
		})

		It("Should cause an error with invalid release sources - invalid channel", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: swag.String("4.12"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []string{"invalid-channel"},
						},
					},
				},
			}

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).To(HaveOccurred())
			tx.Rollback()
		})

		It("Should not cause an error with invalid release sources - empty channels", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: swag.String("4.12"),
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []string{},
						},
					},
				},
			}

			setGetSupportLevelsMock(supportLevelsClientMock, "4")

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should not cause an error with invalid release sources - empty upgrade channels", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: swag.String("4.12"),
					UpgradeChannels:  []*models.UpgradeChannel{},
				},
			}

			setGetSupportLevelsMock(supportLevelsClientMock, "4")

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).ToNot(HaveOccurred())
		})

		It("Should cause an error with invalid release sources - nils", func() {
			handler.releaseSources = models.ReleaseSources{
				{
					OpenshiftVersion: nil,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: nil,
							Channels:        nil,
						},
					},
				},
			}

			err := handler.syncReleaseImagesWithErr(tx)
			Expect(err).To(HaveOccurred())
			tx.Rollback()
		})
	})
})
