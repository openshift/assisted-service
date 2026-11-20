package cluster

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"

	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	eventsapi "github.com/openshift/assisted-service/internal/events/api"
	"github.com/openshift/assisted-service/internal/events/eventstest"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/models"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pkg/errors"
	"go.uber.org/mock/gomock"
)

var _ = Describe("addSystemManifests", func() {
	var (
		ctrl          *gomock.Controller
		ctx           = context.Background()
		mockEvents    *eventsapi.MockHandler
		mockManifests *manifestsapi.MockManifestsAPI
		dir           string
		cfg           Config
		manager       *Manager
		c             *common.Cluster
	)

	newManager := func() *Manager {
		return NewManager(cfg, common.GetTestLog(), nil, nil, mockEvents, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, mockManifests, false, nil)
	}

	writeFile := func(name, content string) {
		path := filepath.Join(dir, name)
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
	}

	expectManifest := func(fileName, content string, ret error) *gomock.Call {
		return mockManifests.EXPECT().CreateClusterManifestInternal(gomock.Any(), gomock.Any(), false).
			DoAndReturn(func(_ context.Context, p operations.V2CreateClusterManifestParams, _ bool) (*models.Manifest, error) {
				Expect(p.ClusterID).To(Equal(*c.ID))
				Expect(*p.CreateManifestParams.Folder).To(Equal(models.ManifestFolderOpenshift))
				Expect(*p.CreateManifestParams.FileName).To(Equal(fileName))
				Expect(*p.CreateManifestParams.Content).To(Equal(base64.StdEncoding.EncodeToString([]byte(content))))
				return &models.Manifest{}, ret
			})
	}

	expectUserManifests := func(times int, userManifests ...*models.Manifest) {
		mockManifests.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, p operations.V2ListClusterManifestsParams) (models.ListManifests, error) {
				Expect(p.ClusterID).To(Equal(*c.ID))
				Expect(p.IncludeSystemGenerated).To(BeNil())
				return userManifests, nil
			}).Times(times)
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = eventsapi.NewMockHandler(ctrl)
		mockManifests = manifestsapi.NewMockManifestsAPI(ctrl)
		var err error
		dir, err = os.MkdirTemp("", "system-manifests")
		Expect(err).NotTo(HaveOccurred())
		cfg = getDefaultConfig()
		cfg.SystemManifestsDir = dir
		manager = newManager()
		id := strfmt.UUID(uuid.New().String())
		c = &common.Cluster{Cluster: models.Cluster{ID: &id}}
	})

	AfterEach(func() {
		ctrl.Finish()
		Expect(os.RemoveAll(dir)).To(Succeed())
	})

	It("loads .yaml and .yml files as system manifests", func() {
		writeFile("a.yaml", "kind: A\n")
		writeFile("b.yml", "kind: B\n")
		expectUserManifests(1)
		gomock.InOrder(
			expectManifest("a.yaml", "kind: A\n", nil),
			expectManifest("b.yml", "kind: B\n", nil),
		)
		manager.addSystemManifests(ctx, common.GetTestLog(), c)
	})

	It("does nothing when the directory does not exist", func() {
		cfg.SystemManifestsDir = filepath.Join(dir, "missing")
		manager = newManager()
		manager.addSystemManifests(ctx, common.GetTestLog(), c)
	})

	It("sends a warning event when the directory cannot be read", func() {
		notADir := filepath.Join(dir, "file")
		writeFile("file", "x")
		cfg.SystemManifestsDir = notADir
		manager = newManager()
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.SystemManifestLoadFailedEventName),
			eventstest.WithClusterIdMatcher(c.ID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityWarning),
			eventstest.WithMessageContainsMatcher(notADir))).Times(1)
		manager.addSystemManifests(ctx, common.GetTestLog(), c)
	})

	It("does nothing when the directory is empty", func() {
		manager.addSystemManifests(ctx, common.GetTestLog(), c)
	})

	It("ignores subdirectories and non-YAML files", func() {
		writeFile("README", "text")
		writeFile("c.json", "{}")
		writeFile("sub/d.yaml", "kind: D\n")
		manager.addSystemManifests(ctx, common.GetTestLog(), c)
	})

	It("skips a manifest that fails to load and sends a warning event", func() {
		writeFile("a.yaml", "kind: A\n")
		writeFile("b.yaml", "kind: B\n")
		expectUserManifests(1)
		gomock.InOrder(
			expectManifest("a.yaml", "kind: A\n", errors.New("bad yaml")),
			expectManifest("b.yaml", "kind: B\n", nil),
		)
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.SystemManifestLoadFailedEventName),
			eventstest.WithClusterIdMatcher(c.ID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityWarning),
			eventstest.WithMessageContainsMatcher("a.yaml"))).Times(1)
		manager.addSystemManifests(ctx, common.GetTestLog(), c)
	})

	It("loads the files again on each call", func() {
		writeFile("a.yaml", "kind: A\n")
		expectUserManifests(2)
		expectManifest("a.yaml", "kind: A\n", nil).Times(2)
		manager.addSystemManifests(ctx, common.GetTestLog(), c)
		manager.addSystemManifests(ctx, common.GetTestLog(), c)
	})
	It("does not overwrite a user manifest with the same name", func() {
		writeFile("a.yaml", "kind: A\n")
		writeFile("b.yaml", "kind: B\n")
		expectUserManifests(1,
			&models.Manifest{Folder: models.ManifestFolderManifests, FileName: "b.yaml", ManifestSource: models.ManifestManifestSourceUser},
			&models.Manifest{Folder: models.ManifestFolderOpenshift, FileName: "a.yaml", ManifestSource: models.ManifestManifestSourceUser})
		expectManifest("b.yaml", "kind: B\n", nil)
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.SystemManifestLoadFailedEventName),
			eventstest.WithClusterIdMatcher(c.ID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityWarning),
			eventstest.WithMessageContainsMatcher("a.yaml"))).Times(1)
		manager.addSystemManifests(ctx, common.GetTestLog(), c)
	})

	It("loads nothing and sends a warning event when user manifests cannot be listed", func() {
		writeFile("a.yaml", "kind: A\n")
		mockManifests.EXPECT().ListClusterManifestsInternal(gomock.Any(), gomock.Any()).Return(nil, errors.New("s3 down")).Times(1)
		mockEvents.EXPECT().SendClusterEvent(gomock.Any(), eventstest.NewEventMatcher(
			eventstest.WithNameMatcher(eventgen.SystemManifestLoadFailedEventName),
			eventstest.WithClusterIdMatcher(c.ID.String()),
			eventstest.WithSeverityMatcher(models.EventSeverityWarning),
			eventstest.WithMessageContainsMatcher("s3 down"))).Times(1)
		manager.addSystemManifests(ctx, common.GetTestLog(), c)
	})
})
