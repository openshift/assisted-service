package manifests

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	manifestsapi "github.com/openshift/assisted-service/internal/manifests/api"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
)

var _ manifestsapi.ManifestsAPI = &Manifests{}

// ManifestFolder represents the manifests folder on s3 per cluster
const ManifestFolder = "manifests"

// NewManifestsAPI returns manifests API
func NewManifestsAPI(db *gorm.DB, log logrus.FieldLogger, objectHandler s3wrapper.API, usageAPI usage.API) *Manifests {
	return &Manifests{
		db:            db,
		log:           log,
		objectHandler: objectHandler,
		usageAPI:      usageAPI,
	}
}

// Manifests represents manifests handler implementation
type Manifests struct {
	db            *gorm.DB
	log           logrus.FieldLogger
	objectHandler s3wrapper.API
	usageAPI      usage.API
}

func (m *Manifests) CreateClusterManifestInternal(ctx context.Context, params operations.V2CreateClusterManifestParams) (*models.Manifest, error) {
	log := logutil.FromContext(ctx, m.log)
	log.Infof("Creating manifest in cluster %s", params.ClusterID.String())

	if params.CreateManifestParams.Folder == nil {
		defaultFolder := models.CreateManifestParamsFolderManifests
		params.CreateManifestParams.Folder = &defaultFolder
	}

	// Verify that the manifests are created for a valid cluster
	// to align kube-api and ocm behavior.
	// In OCM, this is validated at the authorization layer. In other
	// authorization scheme, it does not and therefore should be checked
	// at the application level.
	if !cluster.ClusterExists(m.db, params.ClusterID) {
		return nil, common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}

	if strings.ContainsRune(*params.CreateManifestParams.FileName, os.PathSeparator) {
		log.Errorf("Cluster manifest %s for cluster %s should not include a directory in its name.", *params.CreateManifestParams.FileName, params.ClusterID)
		return nil, common.NewApiError(http.StatusBadRequest, errors.New("Manifest should not include a directory in its name"))
	}
	fileName := filepath.Join(*params.CreateManifestParams.Folder, *params.CreateManifestParams.FileName)
	manifestContent, err := base64.StdEncoding.DecodeString(*params.CreateManifestParams.Content)
	if err != nil {
		log.WithError(err).Errorf("Cluster manifest %s for cluster %s failed to base64 decode: [%s]",
			fileName, params.ClusterID.String(), *params.CreateManifestParams.Content)
		return nil, common.NewApiError(http.StatusBadRequest, errors.New("failed to base64-decode cluster manifest content"))
	}
	extension := filepath.Ext(fileName)
	if extension == ".yaml" || extension == ".yml" {
		var s map[interface{}]interface{}
		if yaml.Unmarshal(manifestContent, &s) != nil {
			return nil, common.NewApiError(http.StatusBadRequest, errors.New("Manifest content has an invalid YAML format"))
		}
	} else if extension == ".json" {
		if !json.Valid(manifestContent) {
			return nil, common.NewApiError(http.StatusBadRequest, errors.New("Manifest content has an illegal JSON format"))
		}
	} else if strings.HasPrefix(extension, ".patch") && (strings.Contains(fileName, ".yaml.patch") || strings.Contains(fileName, ".yml.patch")) {
		var s []map[interface{}]interface{}
		if yaml.Unmarshal(manifestContent, &s) != nil {
			return nil, common.NewApiError(http.StatusBadRequest, errors.New("Patch content has an invalid YAML format"))
		}
	} else {
		return nil, common.NewApiError(http.StatusBadRequest, errors.New("Unsupported manifest extension. Only json, yaml and yml extensions are supported"))
	}

	objectName := GetManifestObjectName(params.ClusterID, fileName)
	if err := m.objectHandler.Upload(ctx, manifestContent, objectName); err != nil {
		log.WithError(err).Errorf("Failed to upload %s", objectName)
		return nil, common.NewApiError(http.StatusInternalServerError, errors.Errorf("failed to upload %s", objectName))
	}

	log.Infof("Done creating manifest %s for cluster %s", fileName, params.ClusterID.String())
	manifest := models.Manifest{FileName: *params.CreateManifestParams.FileName, Folder: *params.CreateManifestParams.Folder}
	return &manifest, nil
}

func (m *Manifests) ListClusterManifestsInternal(ctx context.Context, params operations.V2ListClusterManifestsParams) (models.ListManifests, error) {
	log := logutil.FromContext(ctx, m.log)
	log.Debugf("Listing manifests in cluster %s", params.ClusterID)

	// Verify that the manifests are created for a valid cluster
	// to align kube-api and ocm behavior.
	// In OCM, this is validated at the authorization layer. In other
	// authorization scheme, it does not and therefore should be checked
	// at the application level.
	if !cluster.ClusterExists(m.db, params.ClusterID) {
		return nil, common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}

	objectName := filepath.Join(params.ClusterID.String(), ManifestFolder)
	files, err := m.objectHandler.ListObjectsByPrefix(ctx, objectName)
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	manifests := models.ListManifests{}
	for _, file := range files {
		parts := strings.Split(strings.Trim(file, string(filepath.Separator)), string(filepath.Separator))
		if len(parts) > 2 {
			manifests = append(manifests, &models.Manifest{FileName: filepath.Join(parts[3:]...), Folder: parts[2]})
		} else {
			return nil, common.NewApiError(http.StatusInternalServerError, errors.Errorf("Cannot list file %s in cluster %s", file, params.ClusterID.String()))
		}
	}

	return manifests, nil
}

func (m *Manifests) DeleteClusterManifestInternal(ctx context.Context, params operations.V2DeleteClusterManifestParams) error {
	log := logutil.FromContext(ctx, m.log)
	log.Infof("Deleting manifest from cluster %s", params.ClusterID.String())

	// This call both verifies that the manifests are created for a valid cluster
	// to align kube-api and ocm behavior, and get the cluster object to check that
	// it is in valid state.
	cluster, err := common.GetClusterFromDB(m.db, params.ClusterID, common.SkipEagerLoading)
	if err != nil {
		return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}

	//Deletion of manifests is not allowed after installation has started.
	preInstallationStates := []string{
		models.ClusterStatusPendingForInput,
		models.ClusterStatusInsufficient,
		models.ClusterStatusReady,
	}
	if !funk.ContainsString(preInstallationStates, swag.StringValue(cluster.Status)) {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("cluster %s is not in pre-installation states, "+
			"can't remove manifests after installation has been started",
			params.ClusterID.String()))
	}

	if params.Folder == nil {
		defaultFolder := models.CreateManifestParamsFolderManifests
		params.Folder = &defaultFolder
	}
	fileName := filepath.Join(*params.Folder, params.FileName)
	objectName := GetManifestObjectName(params.ClusterID, fileName)
	exists, err := m.objectHandler.DoesObjectExist(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("Failed to delete cluster manifest %s", objectName)
		return common.NewApiError(http.StatusInternalServerError, err)
	}

	if !exists {
		log.Infof("Cluster manifest %s doesn't exists for cluster %s", fileName, params.ClusterID.String())
		return nil
	}

	_, err = m.objectHandler.DeleteObject(ctx, objectName)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, errors.Errorf("failed to delete %s from s3", objectName))
	}

	log.Infof("Done deleting cluster manifest %s for cluster %s", fileName, params.ClusterID.String())
	return nil
}

func (m *Manifests) V2DownloadClusterManifest(ctx context.Context, params operations.V2DownloadClusterManifestParams) middleware.Responder {
	log := logutil.FromContext(ctx, m.log)
	if params.Folder == nil {
		defaultFolder := models.CreateManifestParamsFolderManifests
		params.Folder = &defaultFolder
	}
	fileName := filepath.Join(*params.Folder, params.FileName)
	log.Infof("Downloading manifest %s from cluster %s", fileName, params.ClusterID)

	// Verify that the manifests are created for a valid cluster
	// to align kube-api and ocm behavior.
	// In OCM, this is validated at the authorization layer. In other
	// authorization scheme, it does not and therefore should be checked
	// at the application level.
	if !cluster.ClusterExists(m.db, params.ClusterID) {
		return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}

	objectName := GetManifestObjectName(params.ClusterID, fileName)
	exists, err := m.objectHandler.DoesObjectExist(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("Failed to download cluster manifest")
		return common.GenerateErrorResponder(err)
	}

	if !exists {
		msg := fmt.Sprintf("Cluster manifest %s doesn't exist in cluster %s", fileName, params.ClusterID.String())
		log.Warn(msg)
		return common.GenerateErrorResponderWithDefault(errors.New(msg), http.StatusNotFound)
	}

	respBody, contentLength, err := m.objectHandler.Download(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("failed to download file %s from cluster: %s", params.FileName, params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}

	return filemiddleware.NewResponder(operations.NewV2DownloadClusterManifestOK().WithPayload(respBody), params.FileName, contentLength, nil)
}

func (m *Manifests) setUsage(active bool, manifest *models.Manifest, clusterID strfmt.UUID) error {
	err := m.db.Transaction(func(tx *gorm.DB) error {
		cluster, err := common.GetClusterFromDB(tx, clusterID, common.SkipEagerLoading)
		if err != nil {
			return err
		}
		if usages, uerr := usage.Unmarshal(cluster.Cluster.FeatureUsage); uerr == nil {
			if active {
				m.usageAPI.Add(usages, usage.CustomManifest, nil)
			} else {
				m.usageAPI.Remove(usages, usage.CustomManifest)
			}
			m.usageAPI.Save(tx, clusterID, usages)
		}
		return nil
	})
	return err
}

// GetManifestObjectName returns the manifest object name as stored in S3
func GetManifestObjectName(clusterID strfmt.UUID, fileName string) string {
	return filepath.Join(string(clusterID), ManifestFolder, fileName)
}

// GetClusterManifests returns a list of cluster manifests
func GetClusterManifests(ctx context.Context, clusterID *strfmt.UUID, s3Client s3wrapper.API) ([]string, error) {
	manifestFiles := []string{}
	files, err := listManifests(ctx, clusterID, models.CreateManifestParamsFolderManifests, s3Client)
	if err != nil {
		return []string{}, err
	}
	manifestFiles = append(manifestFiles, files...)
	files, err = listManifests(ctx, clusterID, models.CreateManifestParamsFolderOpenshift, s3Client)
	if err != nil {
		return []string{}, err
	}
	manifestFiles = append(manifestFiles, files...)
	return manifestFiles, nil
}

func listManifests(ctx context.Context, clusterID *strfmt.UUID, folder string, s3Client s3wrapper.API) ([]string, error) {
	key := GetManifestObjectName(*clusterID, folder)
	files, err := s3Client.ListObjectsByPrefix(ctx, key)
	if err != nil {
		return []string{}, err
	}
	return files, nil
}
