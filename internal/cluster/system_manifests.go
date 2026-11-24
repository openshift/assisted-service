package cluster

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	eventgen "github.com/openshift/assisted-service/internal/common/events"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/sirupsen/logrus"
)

// AddSystemManifests loads every yaml/yml file found in SystemManifestsDir as a system
// manifest in the openshift folder. It runs when installation starts, rather than at
// registration, because ResetClusterFiles deletes system manifests on a cluster reset.
// A missing directory is a no-op. A file that fails to load, or whose name is already
// used by a user manifest, is skipped and reported with a warning event, so it never
// fails the install or overwrites user content.
func (m *Manager) AddSystemManifests(ctx context.Context, cluster *common.Cluster) {
	log := logutil.FromContext(ctx, m.log)
	dir := m.Config.SystemManifestsDir
	names, err := systemManifestFileNames(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			m.reportSystemManifestFailure(ctx, log, cluster, dir, err)
		}
		return
	}
	if len(names) == 0 {
		return
	}

	userManifests, err := m.userManifestNames(ctx, cluster)
	if err != nil {
		m.reportSystemManifestFailure(ctx, log, cluster, dir, err)
		return
	}

	for _, name := range names {
		if userManifests[name] {
			m.reportSystemManifestFailure(ctx, log, cluster, name,
				fmt.Errorf("a user manifest with the same name already exists in the %s folder", models.ManifestFolderOpenshift))
			continue
		}
		if err := m.addSystemManifest(ctx, cluster, filepath.Join(dir, name), name); err != nil {
			m.reportSystemManifestFailure(ctx, log, cluster, name, err)
			continue
		}
		log.Infof("Loaded system manifest %s", name)
	}
}

func systemManifestFileNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if entry.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		names = append(names, entry.Name())
	}
	return names, nil
}

// userManifestNames returns the file names of the user manifests in the openshift folder.
func (m *Manager) userManifestNames(ctx context.Context, cluster *common.Cluster) (map[string]bool, error) {
	manifests, err := m.manifestApi.ListClusterManifestsInternal(ctx, operations.V2ListClusterManifestsParams{ClusterID: *cluster.ID})
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, manifest := range manifests {
		if manifest.Folder == models.ManifestFolderOpenshift {
			names[manifest.FileName] = true
		}
	}
	return names, nil
}

func (m *Manager) reportSystemManifestFailure(ctx context.Context, log logrus.FieldLogger, cluster *common.Cluster, name string, err error) {
	log.WithError(err).Warnf("Failed to load system manifest %s", name)
	eventgen.SendSystemManifestLoadFailedEvent(ctx, m.eventsHandler, *cluster.ID, name, err.Error())
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
