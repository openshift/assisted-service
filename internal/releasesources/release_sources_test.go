package releasesources

import (
	"testing"

	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/leader"
	"gorm.io/gorm"
)

func TestHandler(t *testing.T) {
	RegisterFailHandler(Fail)
	common.InitializeDBTest()
	defer common.TerminateDBTest()
	RunSpecs(t, "releasesources")
}

func setGetReleasesMock(releasesMock *MockopenShiftReleasesAPIClientInterface, testsParams []RequestResponseParameters) {
	for _, testParams := range testsParams {
		releasesGraph, err := getExpectedReleasesGraphForValidParams(testParams.Channel, testParams.Version, testParams.CPUArchitecture)
		releasesMock.EXPECT().
			getReleases(testParams.Channel, testParams.Version, testParams.CPUArchitecture).
			Return(releasesGraph, err).
			AnyTimes()
	}
}

func setGetSupportLevelsMock(supportLevelMock *MockopenShiftSupportLevelAPIClientInterface, majorVersion string) {
	supportLevels, err := getExpectedSupportLevels(majorVersion)
	Expect(err).ToNot(HaveOccurred())

	supportLevelMock.EXPECT().
		getSupportLevels(majorVersion).
		Return(supportLevels, nil).
		AnyTimes()
}

var testSupportedMultiArchitectures []string = []string{
	common.X86CPUArchitecture, common.ARM64CPUArchitecture, common.S390xCPUArchitecture, common.PowerCPUArchitecture,
}

var defaultReleaseSources = models.ReleaseSources{
	{
		OpenshiftVersion:      swag.String("4.10"),
		MultiCPUArchitectures: testSupportedMultiArchitectures,
		UpgradeChannels: []*models.UpgradeChannel{
			{
				CPUArchitecture: swag.String(common.X86CPUArchitecture),
				Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
			},
		},
	},
	{
		OpenshiftVersion:      swag.String("4.12"),
		MultiCPUArchitectures: testSupportedMultiArchitectures,
		UpgradeChannels: []*models.UpgradeChannel{
			{
				CPUArchitecture: swag.String(common.X86CPUArchitecture),
				Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
			},
		},
	},
	{
		OpenshiftVersion:      swag.String("4.13"),
		MultiCPUArchitectures: testSupportedMultiArchitectures,
		UpgradeChannels: []*models.UpgradeChannel{
			{
				CPUArchitecture: swag.String(common.X86CPUArchitecture),
				Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
			},
			{
				CPUArchitecture: swag.String(common.S390xCPUArchitecture),
				Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
			},
		},
	},
	{
		OpenshiftVersion:      swag.String("4.14"),
		MultiCPUArchitectures: testSupportedMultiArchitectures,
		UpgradeChannels: []*models.UpgradeChannel{
			{
				CPUArchitecture: swag.String(common.X86CPUArchitecture),
				Channels:        []models.ReleaseChannel{models.ReleaseChannelStable, models.ReleaseChannelCandidate},
			},
			{
				CPUArchitecture: swag.String("ppc64le"),
				Channels:        []models.ReleaseChannel{models.ReleaseChannelStable, models.ReleaseChannelCandidate},
			},
		},
	},
	{
		OpenshiftVersion:      swag.String("4.15"),
		MultiCPUArchitectures: testSupportedMultiArchitectures,
		UpgradeChannels: []*models.UpgradeChannel{
			{
				CPUArchitecture: swag.String(common.X86CPUArchitecture),
				Channels:        []models.ReleaseChannel{models.ReleaseChannelCandidate},
			},
		},
	},
	{
		OpenshiftVersion:      swag.String("4.16"),
		MultiCPUArchitectures: testSupportedMultiArchitectures,
		UpgradeChannels: []*models.UpgradeChannel{
			{
				CPUArchitecture: swag.String(common.MultiCPUArchitecture),
				Channels:        []models.ReleaseChannel{models.ReleaseChannelCandidate},
			},
		},
	},
}

var staticReleaseImages = models.ReleaseImages{
	{ // Standard
		OpenshiftVersion: swag.String("4.11"),
		Version:          swag.String("4.11.1"),
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
	},
	{ // With support level, major.minor version
		OpenshiftVersion: swag.String("4.14"),
		Version:          swag.String("4.14.2"),
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		SupportLevel:     models.OpenshiftVersionSupportLevelMaintenance,
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
	},
	{ // Default, full version
		OpenshiftVersion: swag.String("4.14.3"),
		Version:          swag.String("4.14.3"),
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		Default:          true,
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.3-x86_64"),
	},
	{ // pre-release
		OpenshiftVersion: swag.String("4.16"),
		Version:          swag.String("4.16.0-ec.2"),
		CPUArchitecture:  swag.String(common.X86CPUArchitecture),
		CPUArchitectures: []string{common.X86CPUArchitecture},
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.16.0-ec.2-x86_64"),
	},
	{ // multiarch
		OpenshiftVersion: swag.String("4.14.4-multi"),
		Version:          swag.String("4.14.4-multi"),
		CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
		CPUArchitectures: testSupportedMultiArchitectures,
		URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.4-multi"),
	},
}

var _ = Describe("SyncReleaseImages", func() {
	var (
		db                      *gorm.DB
		dbName                  string
		err                     error
		ctrl                    *gomock.Controller
		handler                 *releaseSourcesHandler
		releasesClientMock      *MockopenShiftReleasesAPIClientInterface
		supportLevelsClientMock *MockopenShiftSupportLevelAPIClientInterface
		leaderMock              *leader.MockElectorInterface
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		releasesClientMock = NewMockopenShiftReleasesAPIClientInterface(ctrl)
		supportLevelsClientMock = NewMockopenShiftSupportLevelAPIClientInterface(ctrl)
		leaderMock = leader.NewMockElectorInterface(ctrl)
		leaderMock.EXPECT().IsLeader().Return(true).AnyTimes()
	})

	AfterEach(func() {
		ctrl.Finish()
		common.DeleteTestDB(db, dbName)
	})
	// Testing ReleaseSources with standard ReleaseImages

	It("Should not cause an error with empty release sources", func() {
		handler, err = NewReleaseSourcesHandler(
			models.ReleaseSources{},
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).ToNot(HaveOccurred())

		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock

		setGetReleasesMock(releasesClientMock, requestResponseParams)
		setGetSupportLevelsMock(supportLevelsClientMock, "4")

		err = handler.SyncReleaseImages()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should cause an error with invalid release sources - invalid cpu architecture", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("4.12"),
				MultiCPUArchitectures: testSupportedMultiArchitectures,
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String("badCPUArchitecture"),
						Channels:        []models.ReleaseChannel{common.X86CPUArchitecture},
					},
				},
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Should cause an error with invalid release sources - invalid openshift version", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("invalidVersion"),
				MultiCPUArchitectures: testSupportedMultiArchitectures,
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
					},
				},
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Should cause an error with invalid release sources - invalid channel", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("4.12"),
				MultiCPUArchitectures: testSupportedMultiArchitectures,
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						Channels:        []models.ReleaseChannel{"invalid-channel"},
					},
				},
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Should cause an error with invalid release sources - invalid multi_cpu_architectures", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("4.12"),
				MultiCPUArchitectures: []string{"invalid arch"},
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						Channels:        []models.ReleaseChannel{"stable"},
					},
				},
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Should not cause an error with invalid release sources - empty channels", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("4.12"),
				MultiCPUArchitectures: testSupportedMultiArchitectures,
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						Channels:        []models.ReleaseChannel{},
					},
				},
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).ToNot(HaveOccurred())

		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock

		setGetReleasesMock(releasesClientMock, requestResponseParams)
		setGetSupportLevelsMock(supportLevelsClientMock, "4")

		err = handler.SyncReleaseImages()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should not cause an error with invalid release sources - empty upgrade channels", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("4.12"),
				MultiCPUArchitectures: testSupportedMultiArchitectures,
				UpgradeChannels:       []*models.UpgradeChannel{},
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).ToNot(HaveOccurred())

		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock

		setGetReleasesMock(releasesClientMock, requestResponseParams)
		setGetSupportLevelsMock(supportLevelsClientMock, "4")

		err = handler.SyncReleaseImages()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should cause an error with invalid release sources - missing openshift_version", func() {
		releaseSources := models.ReleaseSources{
			{
				MultiCPUArchitectures: testSupportedMultiArchitectures,
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						Channels:        []models.ReleaseChannel{},
					},
				},
			},
		}
		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Should cause an error with invalid release sources - missing multi_cpu_architectures", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion: swag.String("4.12"),
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						Channels:        []models.ReleaseChannel{},
					},
				},
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Should cause an error with invalid release sources - missing upgrade_channels", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("4.12"),
				MultiCPUArchitectures: testSupportedMultiArchitectures,
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Should cause an error with invalid release sources - missing cpu_architecture", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("4.12"),
				MultiCPUArchitectures: testSupportedMultiArchitectures,
				UpgradeChannels: []*models.UpgradeChannel{
					{
						Channels: []models.ReleaseChannel{models.ReleaseChannelStable},
					},
				},
			},
		}
		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Should cause an error with invalid release sources - missing channels", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("4.12"),
				MultiCPUArchitectures: testSupportedMultiArchitectures,
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
					},
				},
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	// Testing ReleaseImages with standard ReleaseSources

	It("Should not cause an error with empty ReleaseImages", func() {
		handler, err = NewReleaseSourcesHandler(
			defaultReleaseSources,
			models.ReleaseImages{},
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).ToNot(HaveOccurred())

		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock

		setGetReleasesMock(releasesClientMock, requestResponseParams)
		setGetSupportLevelsMock(supportLevelsClientMock, "4")

		err = handler.SyncReleaseImages()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should not cause an error with nil ReleaseImages", func() {
		handler, err = NewReleaseSourcesHandler(
			defaultReleaseSources,
			nil,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).ToNot(HaveOccurred())

		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock

		setGetReleasesMock(releasesClientMock, requestResponseParams)
		setGetSupportLevelsMock(supportLevelsClientMock, "4")

		err = handler.SyncReleaseImages()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should not cause an error with required fields", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.11"),
				Version:          swag.String("4.11.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
			},
		}

		handler, err = NewReleaseSourcesHandler(
			defaultReleaseSources,
			releaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).ToNot(HaveOccurred())

		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock

		setGetReleasesMock(releasesClientMock, requestResponseParams)
		setGetSupportLevelsMock(supportLevelsClientMock, "4")

		err = handler.SyncReleaseImages()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should cause an error with missing required fields", func() {
		releaseImages := models.ReleaseImages{
			{
				Version:         swag.String("4.11.1"),
				CPUArchitecture: swag.String(common.X86CPUArchitecture),
				URL:             swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
			},
		}
		handler, err = NewReleaseSourcesHandler(
			defaultReleaseSources,
			releaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())

		releaseImages = models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.11"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
			},
		}

		handler, err = NewReleaseSourcesHandler(
			defaultReleaseSources,
			releaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())

		releaseImages = models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.11"),
				Version:          swag.String("4.11.1"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
			},
		}

		handler, err = NewReleaseSourcesHandler(
			defaultReleaseSources,
			releaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())

		releaseImages = models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.11"),
				Version:          swag.String("4.11.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
			},
		}

		handler, err = NewReleaseSourcesHandler(
			defaultReleaseSources,
			releaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Should not cause an error with valid fields", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.11"),
				Version:          swag.String("4.11.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
			},
		}

		handler, err = NewReleaseSourcesHandler(
			defaultReleaseSources,
			releaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).ToNot(HaveOccurred())

		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock

		setGetReleasesMock(releasesClientMock, requestResponseParams)
		setGetSupportLevelsMock(supportLevelsClientMock, "4")

		err = handler.SyncReleaseImages()
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should cause an error with invalid fields", func() {
		releaseImages := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.11"),
				Version:          swag.String("4.11.1"),
				CPUArchitecture:  swag.String("invalidCPUArch"),
				URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
			},
		}

		handler, err = NewReleaseSourcesHandler(
			defaultReleaseSources,
			releaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).To(HaveOccurred())
	})

	It("Should not cause an error with valid release sources but no results", func() {
		releaseImages := staticReleaseImages
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("4.16"),
				MultiCPUArchitectures: testSupportedMultiArchitectures,
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
					},
				},
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			releaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).ToNot(HaveOccurred())

		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock

		setGetSupportLevelsMock(supportLevelsClientMock, "4")
		releasesClientMock.EXPECT().
			getReleases(models.ReleaseChannelStable, "4.16", common.AMD64CPUArchitecture).
			Return(&ReleaseGraph{}, nil).
			Times(1)

		err = handler.SyncReleaseImages()
		Expect(err).ToNot(HaveOccurred())
	})

	// Latest x86_64 production release
	Context("Should set the default release correctly", func() {
		It("With no default static release image", func() {
			releaseImages := models.ReleaseImages{
				{
					OpenshiftVersion: swag.String("4.11"),
					Version:          swag.String("4.11.1"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
					Default:          false,
				},
				{
					OpenshiftVersion: swag.String("4.11.2"),
					Version:          swag.String("4.11.2"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.2-x86_64"),
					Default:          false,
				},
			}

			releaseSources := models.ReleaseSources{
				{
					OpenshiftVersion:      swag.String("4.14"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
						{
							CPUArchitecture: swag.String(common.ARM64CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelCandidate},
						},
					},
				},
			}

			handler, err = NewReleaseSourcesHandler(
				releaseSources,
				releaseImages,
				common.GetTestLog(),
				db,
				Config{},
				leaderMock,
			)
			Expect(err).ToNot(HaveOccurred())

			handler.releasesClient = releasesClientMock
			handler.supportLevelClient = supportLevelsClientMock

			setGetSupportLevelsMock(supportLevelsClientMock, "4")
			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.14", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.4"},
							{Version: "4.14.2"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.14", common.ARM64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.7"},
							{Version: "4.14.5"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelCandidate, "4.14", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.6"},
							{Version: "4.14.3"},
						},
					}, nil).
				Times(1)

			err = handler.SyncReleaseImages()
			Expect(err).ToNot(HaveOccurred())

			dbReleases := models.ReleaseImages{}
			err = db.Find(&dbReleases, `"default" = ?`, true).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(*dbReleases[0].Version).To(Equal("4.14.4"))
		})

		It("With default configuration release image", func() {
			releaseImages := models.ReleaseImages{
				{
					OpenshiftVersion: swag.String("4.11"),
					Version:          swag.String("4.11.1"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
					Default:          false,
				},
				{
					OpenshiftVersion: swag.String("4.11.2"),
					Version:          swag.String("4.11.2"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.2-x86_64"),
					Default:          true,
				},
			}

			releaseSources := models.ReleaseSources{
				{
					OpenshiftVersion:      swag.String("4.14"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
						{
							CPUArchitecture: swag.String(common.ARM64CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelCandidate},
						},
					},
				},
			}

			handler, err = NewReleaseSourcesHandler(
				releaseSources,
				releaseImages,
				common.GetTestLog(),
				db,
				Config{},
				leaderMock,
			)
			Expect(err).ToNot(HaveOccurred())

			handler.releasesClient = releasesClientMock
			handler.supportLevelClient = supportLevelsClientMock

			setGetSupportLevelsMock(supportLevelsClientMock, "4")
			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.14", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.4"},
							{Version: "4.14.2"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.14", common.ARM64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.7"},
							{Version: "4.14.5"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelCandidate, "4.14", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.6"},
							{Version: "4.14.3"},
						},
					}, nil).
				Times(1)

			err = handler.SyncReleaseImages()
			Expect(err).ToNot(HaveOccurred())

			dbReleases := models.ReleaseImages{}
			err = db.Find(&dbReleases, `"default" = ?`, true).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(*dbReleases[0].Version).To(Equal("4.11.2"))
		})

		It("With no default release image at all", func() {
			releaseImages := models.ReleaseImages{
				{
					OpenshiftVersion: swag.String("4.11"),
					Version:          swag.String("4.11.1"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
					Default:          false,
				},
				{
					OpenshiftVersion: swag.String("4.11.2"),
					Version:          swag.String("4.11.2"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.2-x86_64"),
					Default:          false,
				},
			}

			releaseSources := models.ReleaseSources{
				{
					OpenshiftVersion:      swag.String("4.14"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.S390xCPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
						{
							CPUArchitecture: swag.String(common.ARM64CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
						{
							CPUArchitecture: swag.String(common.PowerCPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelCandidate},
						},
					},
				},
			}

			handler, err = NewReleaseSourcesHandler(
				releaseSources,
				releaseImages,
				common.GetTestLog(),
				db,
				Config{},
				leaderMock,
			)
			Expect(err).ToNot(HaveOccurred())

			handler.releasesClient = releasesClientMock
			handler.supportLevelClient = supportLevelsClientMock

			setGetSupportLevelsMock(supportLevelsClientMock, "4")
			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.14", common.S390xCPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.4"},
							{Version: "4.14.2"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.14", common.ARM64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.7"},
							{Version: "4.14.5"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelCandidate, "4.14", common.PowerCPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.14.6"},
							{Version: "4.14.3"},
						},
					}, nil).
				Times(1)

			err = handler.SyncReleaseImages()
			Expect(err).ToNot(HaveOccurred())

			var dbReleases models.ReleaseImages
			err = handler.db.Find(&dbReleases, `"default" = ?`, true).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(dbReleases).To(BeEmpty())
		})
	})

	Context("Should set releases support level correctly", func() {
		It("Of static release images", func() {
			releaseImages := models.ReleaseImages{
				{
					OpenshiftVersion: swag.String("5.2"),
					Version:          swag.String("5.2.0"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:5.2.0-x86_64"),
					Default:          false,
				},
				{
					OpenshiftVersion: swag.String("5.3"),
					Version:          swag.String("5.3.0-ec.2"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:5.3.0-ec.2-x86_64"),
					Default:          false,
				},
				{
					OpenshiftVersion: swag.String("4.9"),
					Version:          swag.String("4.9.1"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.9.1-x86_64"),
					Default:          false,
				},
				{
					OpenshiftVersion: swag.String("4.11"),
					Version:          swag.String("4.11.1"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.11.1-x86_64"),
					Default:          false,
				},
				{
					OpenshiftVersion: swag.String("4.13"),
					Version:          swag.String("4.13.1"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.13.1-x86_64"),
					Default:          false,
				},
				{
					OpenshiftVersion: swag.String("4.15"),
					Version:          swag.String("4.15.0-ec.2"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.15.0-ec.2-x86_64"),
					Default:          false,
				},
			}

			handler, err = NewReleaseSourcesHandler(
				models.ReleaseSources{},
				releaseImages,
				common.GetTestLog(),
				db,
				Config{},
				leaderMock,
			)
			Expect(err).ToNot(HaveOccurred())

			handler.releasesClient = releasesClientMock
			handler.supportLevelClient = supportLevelsClientMock

			setGetSupportLevelsMock(supportLevelsClientMock, "4")
			setGetSupportLevelsMock(supportLevelsClientMock, "5")

			err = handler.SyncReleaseImages()
			Expect(err).ToNot(HaveOccurred())

			dbReleases := models.ReleaseImages{}

			err = db.Find(&dbReleases, "version = ?", "5.3.0-ec.2").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelBeta))

			err = db.Find(&dbReleases, "version = ?", "5.2.0").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelEndOfLife))

			err = db.Find(&dbReleases, "version = ?", "4.15.0-ec.2").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelBeta))

			err = db.Find(&dbReleases, "version = ?", "4.13.1").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelProduction))

			err = db.Find(&dbReleases, "version = ?", "4.11.1").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelMaintenance))

			err = db.Find(&dbReleases, "version = ?", "4.9.1").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelEndOfLife))

		})

		It("Of dynamic release images", func() {
			releaseSources := models.ReleaseSources{
				{
					OpenshiftVersion:      swag.String("5.3"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
					},
				},
				{
					OpenshiftVersion:      swag.String("5.2"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
					},
				},
				{
					OpenshiftVersion:      swag.String("4.15"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
					},
				},
				{
					OpenshiftVersion:      swag.String("4.13"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
					},
				},
				{
					OpenshiftVersion:      swag.String("4.11"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
					},
				},
				{
					OpenshiftVersion:      swag.String("4.9"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
					},
				},
			}

			handler, err = NewReleaseSourcesHandler(
				releaseSources,
				models.ReleaseImages{},
				common.GetTestLog(),
				db,
				Config{},
				leaderMock,
			)
			Expect(err).ToNot(HaveOccurred())

			handler.releasesClient = releasesClientMock
			handler.supportLevelClient = supportLevelsClientMock

			setGetSupportLevelsMock(supportLevelsClientMock, "4")
			setGetSupportLevelsMock(supportLevelsClientMock, "5")
			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "5.3", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "5.3.0-ec.2"},
						},
					}, nil).
				Times(1)
			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "5.2", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "5.2.0"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.15", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.15.0-ec.3"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.13", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.13.7"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.11", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.11.6"},
						},
					}, nil).
				Times(1)

			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.9", common.AMD64CPUArchitecture).
				Return(
					&ReleaseGraph{
						Nodes: []Node{
							{Version: "4.9.3"},
						},
					}, nil).
				Times(1)

			err = handler.SyncReleaseImages()
			Expect(err).ToNot(HaveOccurred())

			dbReleases := models.ReleaseImages{}

			err = db.Find(&dbReleases, "version = ?", "5.3.0-ec.2").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelBeta))

			err = db.Find(&dbReleases, "version = ?", "5.2.0").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelEndOfLife))

			err = db.Find(&dbReleases, "version = ?", "4.15.0-ec.3").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelBeta))

			err = db.Find(&dbReleases, "version = ?", "4.13.7").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelProduction))

			err = db.Find(&dbReleases, "version = ?", "4.11.6").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelMaintenance))

			err = db.Find(&dbReleases, "version = ?", "4.9.3").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(dbReleases)).To(Equal(1))
			Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelEndOfLife))
		})
	})

	It("Should remove duplicates correctly", func() {
		releaseSources := models.ReleaseSources{
			{
				OpenshiftVersion:      swag.String("4.14"),
				MultiCPUArchitectures: testSupportedMultiArchitectures,
				UpgradeChannels: []*models.UpgradeChannel{
					{
						CPUArchitecture: swag.String(common.X86CPUArchitecture),
						Channels: []models.ReleaseChannel{
							models.ReleaseChannelStable,
							models.ReleaseChannelCandidate,
							models.ReleaseChannelFast,
							models.ReleaseChannelEus,
						},
					},
				},
			},
		}

		handler, err = NewReleaseSourcesHandler(
			releaseSources,
			models.ReleaseImages{},
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).ToNot(HaveOccurred())

		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock

		setGetSupportLevelsMock(supportLevelsClientMock, "4")
		releasesClientMock.EXPECT().
			getReleases(models.ReleaseChannelCandidate, "4.14", common.AMD64CPUArchitecture).
			Return(
				&ReleaseGraph{
					Nodes: []Node{
						{Version: "4.14.0-ec.2"},
						{Version: "4.14.2"},
						{Version: "4.14.3"},
						{Version: "4.14.4"},
						{Version: "4.14.5"},
					},
				}, nil).
			Times(1)

		releasesClientMock.EXPECT().
			getReleases(models.ReleaseChannelFast, "4.14", common.AMD64CPUArchitecture).
			Return(
				&ReleaseGraph{
					Nodes: []Node{
						{Version: "4.14.0-ec.2"},
						{Version: "4.14.2"},
						{Version: "4.14.3"},
						{Version: "4.14.4"},
					},
				}, nil).
			Times(1)

		releasesClientMock.EXPECT().
			getReleases(models.ReleaseChannelStable, "4.14", common.AMD64CPUArchitecture).
			Return(
				&ReleaseGraph{
					Nodes: []Node{
						{Version: "4.14.0-ec.2"},
						{Version: "4.14.2"},
						{Version: "4.14.3"},
					},
				}, nil).
			Times(1)

		releasesClientMock.EXPECT().
			getReleases(models.ReleaseChannelEus, "4.14", common.AMD64CPUArchitecture).
			Return(
				&ReleaseGraph{
					Nodes: []Node{
						{Version: "4.14.0-ec.2"},
						{Version: "4.14.2"},
						{Version: "4.14.3"},
					},
				}, nil).
			Times(1)

		err = handler.SyncReleaseImages()
		Expect(err).ToNot(HaveOccurred())

		var dbReleases models.ReleaseImages
		err = handler.db.Find(
			&dbReleases,
			"version = ?",
			"4.14.0-ec.2",
		).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(len(dbReleases)).To(Equal(1))
		Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelBeta))

		err = handler.db.Find(
			&dbReleases,
			"version = ?",
			"4.14.2",
		).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(len(dbReleases)).To(Equal(1))
		Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelProduction))

		err = handler.db.Find(
			&dbReleases,
			"version = ?",
			"4.14.3",
		).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(len(dbReleases)).To(Equal(1))
		Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelProduction))

		err = handler.db.Find(
			&dbReleases,
			"version = ?",
			"4.14.4",
		).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(len(dbReleases)).To(Equal(1))
		Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelProduction))
		Expect(dbReleases[0].Default).To(BeTrue())

		err = handler.db.Find(
			&dbReleases,
			"version = ?",
			"4.14.5",
		).Error
		Expect(err).ToNot(HaveOccurred())
		Expect(len(dbReleases)).To(Equal(1))
		Expect(dbReleases[0].SupportLevel).To(Equal(models.ReleaseImageSupportLevelBeta))
	})

	Context("Should merge static and dynamic releases correctly", func() {
		It("With conflicts", func() {
			releaseImages := models.ReleaseImages{
				{
					OpenshiftVersion: swag.String("4.14"),
					Version:          swag.String("4.14.1"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
					Default:          false,
					SupportLevel:     models.OpenshiftVersionSupportLevelMaintenance,
				},
				{
					OpenshiftVersion: swag.String("4.14"),
					Version:          swag.String("4.14.2"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
					Default:          true,
					SupportLevel:     models.OpenshiftVersionSupportLevelMaintenance,
				},
			}

			releaseSources := models.ReleaseSources{
				{
					OpenshiftVersion:      swag.String("4.14"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
					},
				},
			}

			handler, err = NewReleaseSourcesHandler(
				releaseSources,
				releaseImages,
				common.GetTestLog(),
				db,
				Config{},
				leaderMock,
			)
			Expect(err).ToNot(HaveOccurred())

			handler.releasesClient = releasesClientMock
			handler.supportLevelClient = supportLevelsClientMock

			setGetSupportLevelsMock(supportLevelsClientMock, "4")
			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.14", common.AMD64CPUArchitecture).
				Return(&ReleaseGraph{
					Nodes: []Node{
						{Version: "4.14.1"},
						{Version: "4.14.3"},
					},
				}, nil).
				Times(1)

			err = handler.SyncReleaseImages()
			Expect(err).ToNot(HaveOccurred())

			var resultReleaseImages models.ReleaseImages
			err = handler.db.Find(&resultReleaseImages, "version", "4.14.1").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(resultReleaseImages).ToNot(BeEmpty())
			Expect(resultReleaseImages[0].SupportLevel).To(Equal(models.OpenshiftVersionSupportLevelProduction))

			err = handler.db.First(&resultReleaseImages, "version", "4.14.2").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(resultReleaseImages).ToNot(BeEmpty())
			Expect(resultReleaseImages[0].Default).To(BeTrue())

			err = handler.db.First(&resultReleaseImages, "version", "4.14.3").Error
			Expect(err).ToNot(HaveOccurred())
			Expect(resultReleaseImages).ToNot(BeEmpty())
			Expect(resultReleaseImages[0].Default).To(BeFalse())
		})

		It("Without conflicts", func() {
			releaseImages := models.ReleaseImages{
				{
					OpenshiftVersion: swag.String("4.14"),
					Version:          swag.String("4.14.1"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.1-x86_64"),
					Default:          false,
					SupportLevel:     models.OpenshiftVersionSupportLevelMaintenance,
				},
				{
					OpenshiftVersion: swag.String("4.14"),
					Version:          swag.String("4.14.2"),
					CPUArchitecture:  swag.String(common.X86CPUArchitecture),
					CPUArchitectures: []string{common.X86CPUArchitecture},
					URL:              swag.String("quay.io/openshift-release-dev/ocp-release:4.14.2-x86_64"),
					Default:          false,
					SupportLevel:     models.OpenshiftVersionSupportLevelMaintenance,
				},
			}

			releaseSources := models.ReleaseSources{
				{
					OpenshiftVersion:      swag.String("4.14"),
					MultiCPUArchitectures: testSupportedMultiArchitectures,
					UpgradeChannels: []*models.UpgradeChannel{
						{
							CPUArchitecture: swag.String(common.X86CPUArchitecture),
							Channels:        []models.ReleaseChannel{models.ReleaseChannelStable},
						},
					},
				},
			}

			handler, err = NewReleaseSourcesHandler(
				releaseSources,
				releaseImages,
				common.GetTestLog(),
				db,
				Config{},
				leaderMock,
			)
			Expect(err).ToNot(HaveOccurred())

			handler.releasesClient = releasesClientMock
			handler.supportLevelClient = supportLevelsClientMock

			setGetSupportLevelsMock(supportLevelsClientMock, "4")
			releasesClientMock.EXPECT().
				getReleases(models.ReleaseChannelStable, "4.14", common.AMD64CPUArchitecture).
				Return(&ReleaseGraph{
					Nodes: []Node{
						{Version: "4.14.4"},
						{Version: "4.14.5"},
					},
				}, nil).
				Times(1)

			err = handler.SyncReleaseImages()
			Expect(err).ToNot(HaveOccurred())

			var resultReleaseImages models.ReleaseImages
			err = handler.db.Find(&resultReleaseImages).Error
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resultReleaseImages)).To(Equal(4))
		})
	})

	It("Should be successfull with valid static and dynamic release images extended scenario", func() {
		handler, err = NewReleaseSourcesHandler(
			defaultReleaseSources,
			staticReleaseImages,
			common.GetTestLog(),
			db,
			Config{},
			leaderMock,
		)
		Expect(err).ToNot(HaveOccurred())

		handler.releasesClient = releasesClientMock
		handler.supportLevelClient = supportLevelsClientMock

		setGetReleasesMock(releasesClientMock, requestResponseParams)
		setGetSupportLevelsMock(supportLevelsClientMock, "4")

		expectedResult := models.ReleaseImages{
			{
				OpenshiftVersion: swag.String("4.10"),
				Version:          swag.String("4.10.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelEndOfLife,
				URL:              swag.String(getReleaseImageReference("4.10.1", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.12"),
				Version:          swag.String("4.12.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelMaintenance,
				URL:              swag.String(getReleaseImageReference("4.12.1", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.13"),
				Version:          swag.String("4.13.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				URL:              swag.String(getReleaseImageReference("4.13.1", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.13"),
				Version:          swag.String("4.13.17"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				URL:              swag.String(getReleaseImageReference("4.13.17", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.13"),
				Version:          swag.String("4.13.1"),
				CPUArchitecture:  swag.String(common.S390xCPUArchitecture),
				CPUArchitectures: []string{common.S390xCPUArchitecture},
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				URL:              swag.String(getReleaseImageReference("4.13.1", common.S390xCPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.13"),
				Version:          swag.String("4.13.19"),
				CPUArchitecture:  swag.String(common.S390xCPUArchitecture),
				CPUArchitectures: []string{common.S390xCPUArchitecture},
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				URL:              swag.String(getReleaseImageReference("4.13.19", common.S390xCPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.0"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				URL:              swag.String(getReleaseImageReference("4.14.0", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.ReleaseImageSupportLevelProduction,
				URL:              swag.String(getReleaseImageReference("4.14.1", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.0-rc.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
				URL:              swag.String(getReleaseImageReference("4.14.0-rc.1", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.0-ec.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
				URL:              swag.String(getReleaseImageReference("4.14.0-ec.2", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.0"),
				CPUArchitecture:  swag.String(common.PowerCPUArchitecture),
				CPUArchitectures: []string{common.PowerCPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
				URL:              swag.String(getReleaseImageReference("4.14.0", common.PowerCPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.1"),
				CPUArchitecture:  swag.String(common.PowerCPUArchitecture),
				CPUArchitectures: []string{common.PowerCPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
				URL:              swag.String(getReleaseImageReference("4.14.1", common.PowerCPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.15"),
				Version:          swag.String("4.15.0-ec.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
				URL:              swag.String(getReleaseImageReference("4.15.0-ec.2", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.16-multi"),
				Version:          swag.String("4.16.0-ec.2-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: testSupportedMultiArchitectures,
				SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
				URL:              swag.String(getReleaseImageReference("4.16.0-ec.2", common.MultiCPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.11"),
				Version:          swag.String("4.11.1"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelMaintenance,
				URL:              swag.String(getReleaseImageReference("4.11.1", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14"),
				Version:          swag.String("4.14.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
				URL:              swag.String(getReleaseImageReference("4.14.2", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14.3"),
				Version:          swag.String("4.14.3"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
				URL:              swag.String(getReleaseImageReference("4.14.3", common.X86CPUArchitecture)),
				Default:          true,
			},
			{
				OpenshiftVersion: swag.String("4.16"),
				Version:          swag.String("4.16.0-ec.2"),
				CPUArchitecture:  swag.String(common.X86CPUArchitecture),
				CPUArchitectures: []string{common.X86CPUArchitecture},
				SupportLevel:     models.OpenshiftVersionSupportLevelBeta,
				URL:              swag.String(getReleaseImageReference("4.16.0-ec.2", common.X86CPUArchitecture)),
				Default:          false,
			},
			{
				OpenshiftVersion: swag.String("4.14.4-multi"),
				Version:          swag.String("4.14.4-multi"),
				CPUArchitecture:  swag.String(common.MultiCPUArchitecture),
				CPUArchitectures: testSupportedMultiArchitectures,
				SupportLevel:     models.OpenshiftVersionSupportLevelProduction,
				URL:              swag.String(getReleaseImageReference("4.14.4", common.MultiCPUArchitecture)),
				Default:          false,
			},
		}

		err := handler.SyncReleaseImages()
		Expect(err).ToNot(HaveOccurred())

		var releaseImages models.ReleaseImages
		err = db.Find(&releaseImages).Error
		Expect(err).ToNot(HaveOccurred())

		Expect(len(expectedResult)).To(Equal(len(releaseImages)))
		Expect(expectedResult).To(ConsistOf(releaseImages))
	})
})
