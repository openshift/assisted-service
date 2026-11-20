package cluster

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/models"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/sirupsen/logrus"
)

// addSystemManifests loads every yaml/yml file found in SystemManifestsDir as a system
// manifest in the openshift folder. A missing directory is a no-op. A file that fails
// to load is skipped and reported with a warning event, so it never fails the install.
func (m *Manager) addSystemManifests(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster) {
	dir := m.Config.SystemManifestsDir
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.WithError(err).Warnf("Failed to read system manifests directory %s", dir)
			eventgen.SendSystemManifestLoadFailedEvent(ctx, m.eventsHandler, *cluster.ID, dir, err.Error())
		}
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		ext := filepath.Ext(name)
		if entry.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		if err := m.addSystemManifest(ctx, cluster, filepath.Join(dir, name), name); err != nil {
			log.WithError(err).Warnf("Failed to load system manifest %s", name)
			eventgen.SendSystemManifestLoadFailedEvent(ctx, m.eventsHandler, *cluster.ID, name, err.Error())
			continue
		}
		log.Infof("Loaded system manifest %s", name)
	}
}

func (m *Manager) addSystemManifest(ctx context.Context, cluster *common.Cluster, path, name string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	_, err = m.manifestApi.CreateClusterManifestInternal(ctx, operations.V2CreateClusterManifestParams{
		ClusterID: *cluster.ID,
		CreateManifestParams: &models.CreateManifestParams{
			Content:  swag.String(base64.StdEncoding.EncodeToString(content)),
			FileName: swag.String(name),
			Folder:   swag.String(models.ManifestFolderOpenshift),
		},
	}, false)
	return err
}
