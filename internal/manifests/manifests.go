package manifests

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/constants"
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

func (m *Manifests) CreateClusterManifestInternal(ctx context.Context, params operations.V2CreateClusterManifestParams, isCustomManifest bool) (*models.Manifest, error) {
	log := logutil.FromContext(ctx, m.log)
	log.Infof("Creating manifest in cluster %s", params.ClusterID.String())

	folder, fileName, path := m.getManifestPathsFromParameters(ctx, params.CreateManifestParams.Folder, params.CreateManifestParams.FileName)
	log.Infof("Folder = '%s' and filename = '%s' and path = '%s'", folder, fileName, path)

	// Verify that the manifests are created for a valid cluster
	// to align kube-api and ocm behavior.
	// In OCM, this is validated at the authorization layer. In other
	// authorization scheme, it does not and therefore should be checked
	// at the application level.
	if _, err := common.GetClusterFromDB(m.db, params.ClusterID, false); err != nil {
		return nil, common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}

	err := m.validateManifestFileNames(ctx, params.ClusterID, []string{fileName})
	if err != nil {
		return nil, err
	}

	var manifestContent []byte
	manifestContent, err = m.decodeUserSuppliedManifest(ctx, params.ClusterID, *params.CreateManifestParams.Content)
	if err != nil {
		return nil, err
	}

	err = m.validateUserSuppliedManifest(ctx, params.ClusterID, manifestContent, path)
	if err != nil {
		return nil, err
	}

	err = m.uploadManifest(ctx, manifestContent, params.ClusterID, path)
	if err != nil {
		return nil, err
	}

	if isCustomManifest {
		err = m.markUserSuppliedManifest(ctx, params.ClusterID, path)
		if err != nil {
			return nil, err
		}
	}

	log.Infof("Done creating manifest %s for cluster %s", path, params.ClusterID.String())
	manifest := models.Manifest{FileName: fileName, Folder: folder}
	return &manifest, nil
}

func (m *Manifests) unmarkUserSuppliedManifest(ctx context.Context, clusterID strfmt.UUID, path string) error {
	objectName := GetManifestMetadataObjectName(clusterID, path, constants.ManifestSourceUserSupplied)
	exists := false
	var err error
	if exists, err = m.objectHandler.DoesObjectExist(ctx, objectName); err != nil {
		return errors.Wrapf(err, "Failed to delete metadata object %s from storage for cluster %s", objectName, clusterID)
	}
	if exists {
		if _, err = m.objectHandler.DeleteObject(ctx, objectName); err != nil {
			return errors.Wrapf(err, "Failed to delete metadata object %s from storage for cluster %s", objectName, clusterID)
		}
	}
	return nil
}

func (m *Manifests) markUserSuppliedManifest(ctx context.Context, clusterID strfmt.UUID, path string) error {
	objectName := GetManifestMetadataObjectName(clusterID, path, constants.ManifestSourceUserSupplied)
	if err := m.objectHandler.Upload(ctx, []byte{}, objectName); err != nil {
		return err
	}
	return nil
}

func (m *Manifests) IsUserManifest(ctx context.Context, clusterID strfmt.UUID, folder string, fileName string) (bool, error) {
	path := filepath.Join(folder, fileName)
	isCustomManifest, err := m.objectHandler.DoesObjectExist(ctx, GetManifestMetadataObjectName(clusterID, path, constants.ManifestSourceUserSupplied))
	return isCustomManifest, err
}

func IsManifest(file string) bool {
	parts := strings.Split(strings.Trim(file, string(filepath.Separator)), string(filepath.Separator))
	return len(parts) > 2 && parts[1] == models.ManifestFolderManifests
}

func ParsePath(file string) (folder string, filename string, err error) {
	parts := strings.Split(strings.Trim(file, string(filepath.Separator)), string(filepath.Separator))
	if !(len(parts) > 2 && parts[1] == "manifests") {
		return "", "", errors.Errorf("Filepath %s is not a manifest path", file)
	}
	return parts[2], parts[3], nil
}

func (m *Manifests) ListClusterManifestsInternal(ctx context.Context, params operations.V2ListClusterManifestsParams) (models.ListManifests, error) {
	log := logutil.FromContext(ctx, m.log)
	log.Debugf("Listing manifests in cluster %s", params.ClusterID)

	// Verify that the manifests are created for a valid cluster
	// to align kube-api and ocm behavior.
	// In OCM, this is validated at the authorization layer. In other
	// authorization scheme, it does not and therefore should be checked
	// at the application level.
	if _, err := common.GetClusterFromDB(m.db, params.ClusterID, false); err != nil {
		return nil, common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}

	objectName := filepath.Join(params.ClusterID.String(), constants.ManifestFolder)
	files, err := m.objectHandler.ListObjectsByPrefix(ctx, objectName)
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	manifests := models.ListManifests{}
	for _, file := range files {
		folder, filename, err := ParsePath(file)
		if err != nil {
			return nil, err
		}
		isUserManifest, err := m.IsUserManifest(ctx, params.ClusterID, folder, filename)
		if err != nil {
			return nil, err
		}
		if isUserManifest {
			manifests = append(manifests, &models.Manifest{FileName: filename, Folder: folder})
		}
	}
	return manifests, nil
}

func (m *Manifests) DeleteClusterManifestInternal(ctx context.Context, params operations.V2DeleteClusterManifestParams) error {
	log := logutil.FromContext(ctx, m.log)
	log.Infof("Deleting manifest from cluster %s", params.ClusterID.String())

	cluster, err := common.GetClusterFromDB(m.db, params.ClusterID, common.SkipEagerLoading)
	if err != nil {
		return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}

	err = m.validateAllowedToModifyManifests(ctx, cluster)
	if err != nil {
		log.WithError(err).Errorf("Not allowed to modify manifest for cluster id %s", params.ClusterID.String())
		return err
	}

	_, _, path := m.getManifestPathsFromParameters(ctx, params.Folder, &params.FileName)

	err = m.deleteManifest(ctx, params.ClusterID, path)
	if err != nil {
		return err
	}

	log.Infof("Done deleting cluster manifest %s for cluster %s", path, params.ClusterID.String())
	return nil
}

func (m *Manifests) UpdateClusterManifestInternal(ctx context.Context, params operations.V2UpdateClusterManifestParams) (*models.Manifest, error) {
	if params.UpdateManifestParams.UpdatedFolder == nil {
		params.UpdateManifestParams.UpdatedFolder = &params.UpdateManifestParams.Folder
	}
	if params.UpdateManifestParams.UpdatedFileName == nil {
		params.UpdateManifestParams.UpdatedFileName = &params.UpdateManifestParams.FileName
	}
	srcFolder, srcFileName, srcPath := m.getManifestPathsFromParameters(ctx, &params.UpdateManifestParams.Folder, &params.UpdateManifestParams.FileName)
	destFolder, destFileName, destPath := m.getManifestPathsFromParameters(ctx, params.UpdateManifestParams.UpdatedFolder, params.UpdateManifestParams.UpdatedFileName)
	cluster, err := common.GetClusterFromDB(m.db, params.ClusterID, common.SkipEagerLoading)
	if err != nil {
		err = fmt.Errorf("Object Not Found")
		m.log.Infof(err.Error())
		return nil, common.NewApiError(http.StatusNotFound, err)
	}

	err = m.validateAllowedToModifyManifests(ctx, cluster)
	if err != nil {
		return nil, err
	}

	err = m.validateManifestFileNames(ctx, params.ClusterID, []string{srcFileName, destFileName})
	if err != nil {
		return nil, err
	}

	var content []byte
	if params.UpdateManifestParams.UpdatedContent != nil {
		content, err = m.decodeUserSuppliedManifest(ctx, params.ClusterID, *params.UpdateManifestParams.UpdatedContent)
		if err != nil {
			return nil, err
		}
		err = m.validateUserSuppliedManifest(ctx, params.ClusterID, content, srcFileName)
		if err != nil {
			return nil, err
		}
	} else {
		content, err = m.fetchManifestContent(ctx, params.ClusterID, srcFolder, srcFileName)
		if err != nil {
			return nil, err
		}
	}

	err = m.uploadManifest(ctx, content, params.ClusterID, destPath)
	if err != nil {
		return nil, err
	}

	isCustomManifest, err := m.IsUserManifest(ctx, params.ClusterID, srcFolder, srcFileName)
	if err != nil {
		return nil, err
	}
	if isCustomManifest {
		err = m.markUserSuppliedManifest(ctx, params.ClusterID, destPath)
		if err != nil {
			return nil, err
		}
	}
	if srcPath != destPath {
		err = m.deleteManifest(ctx, params.ClusterID, srcPath)
		if err != nil {
			return nil, err
		}
	}
	manifest := models.Manifest{FileName: destFileName, Folder: destFolder}
	return &manifest, nil
}

func (m *Manifests) V2DownloadClusterManifest(ctx context.Context, params operations.V2DownloadClusterManifestParams) middleware.Responder {
	log := logutil.FromContext(ctx, m.log)
	if params.Folder == nil {
		defaultFolder := models.CreateManifestParamsFolderManifests
		params.Folder = &defaultFolder
	}
	_, fileName, path := m.getManifestPathsFromParameters(ctx, params.Folder, &params.FileName)

	// Verify that the manifests are created for a valid cluster
	// to align kube-api and ocm behavior.
	// In OCM, this is validated at the authorization layer. In other
	// authorization scheme, it does not and therefore should be checked
	// at the application level.
	if _, err := common.GetClusterFromDB(m.db, params.ClusterID, false); err != nil {
		return common.NewApiError(http.StatusNotFound, fmt.Errorf("Object Not Found"))
	}

	objectName := GetManifestObjectName(params.ClusterID, path)
	exists, err := m.objectHandler.DoesObjectExist(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("Failed to download cluster manifest")
		return common.GenerateErrorResponder(err)
	}

	if !exists {
		msg := fmt.Sprintf("Cluster manifest %s doesn't exist in cluster %s", path, params.ClusterID.String())
		log.Warn(msg)
		return common.GenerateErrorResponderWithDefault(errors.New(msg), http.StatusNotFound)
	}

	respBody, contentLength, err := m.objectHandler.Download(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("failed to download file %s from cluster: %s", fileName, params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}

	return filemiddleware.NewResponder(operations.NewV2DownloadClusterManifestOK().WithPayload(respBody), fileName, contentLength, nil)
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
	return filepath.Join(string(clusterID), constants.ManifestFolder, fileName)
}

// GetManifestObjectName returns the manifest object name as stored in S3
func GetManifestMetadataObjectName(clusterID strfmt.UUID, fileName string, manifestSource string) string {
	return filepath.Join(string(clusterID), constants.ManifestMetadataFolder, fileName, manifestSource)
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

func (m *Manifests) prepareAndLogError(ctx context.Context, httpStatusCode int32, err error) error {
	log := logutil.FromContext(ctx, m.log)
	log.Error(err)
	return common.NewApiError(httpStatusCode, err)
}

func (m *Manifests) fetchManifestContent(ctx context.Context, clusterID strfmt.UUID, folderName string, fileName string) ([]byte, error) {
	path := filepath.Join(folderName, fileName)
	respBody, _, err := m.objectHandler.Download(ctx, GetManifestObjectName(clusterID, path))
	if err != nil {
		return nil, m.prepareAndLogError(ctx, http.StatusInternalServerError, errors.Wrapf(err, "Failed to fetch content from %s for cluster %s", path, clusterID))
	}
	content, err := ioutil.ReadAll(respBody)
	if err != nil {
		return nil, m.prepareAndLogError(ctx, http.StatusInternalServerError, errors.Wrapf(err, "Failed fetch response body from %s for cluster %s", path, clusterID))
	}
	return content, nil
}

func (m *Manifests) validateManifestFileNames(ctx context.Context, clusterID strfmt.UUID, fileNames []string) error {
	for _, fileName := range fileNames {
		if strings.Contains(fileName, " ") {
			return m.prepareAndLogError(
				ctx,
				http.StatusBadRequest,
				errors.Errorf("Cluster manifest %s for cluster %s should not include a space in its name.",
					fileName,
					clusterID))
		}
		if strings.ContainsRune(fileName, os.PathSeparator) {
			return m.prepareAndLogError(
				ctx,
				http.StatusBadRequest,
				errors.Errorf("Cluster manifest %s for cluster %s should not include a directory in its name.",
					fileName,
					clusterID))
		}
	}
	return nil
}

func (m *Manifests) validateAllowedToModifyManifests(ctx context.Context, cluster *common.Cluster) error {
	// Creation / deletion/ alteration of manifests is not allowed after installation has started.
	preInstallationStates := []string{
		models.ClusterStatusPendingForInput,
		models.ClusterStatusInsufficient,
		models.ClusterStatusReady,
	}
	if !funk.ContainsString(preInstallationStates, swag.StringValue(cluster.Status)) {
		return m.prepareAndLogError(ctx, http.StatusBadRequest, errors.Errorf("cluster %s is not in pre-installation states, "+
			"can't modify manifests after installation has been started",
			cluster.ID))
	}
	return nil
}

func (m *Manifests) validateUserSuppliedManifest(ctx context.Context, clusterID strfmt.UUID, manifestContent []byte, fileName string) error {
	// etcd resources in k8s are limited to 1.5 MiB as indicated here https://etcd.io/docs/v3.5/dev-guide/limit/#request-size-limit
	// however, one the the resource types that can be created from a manifest is a ConfigMap
	// which has a size limit of 1MiB as cited here https://kubernetes.io/docs/concepts/configuration/configmap
	// so this limit has been chosen based on the lowest permitted resource size (the size of the ConfigMap)
	maxFileSizeBytes := 1024 * 1024
	if len(manifestContent) > maxFileSizeBytes {
		return m.prepareAndLogError(ctx, http.StatusBadRequest, errors.Errorf("Manifest content of file %s for cluster ID %s exceeds the maximum file size of 1MiB", fileName, string(clusterID)))
	}
	extension := filepath.Ext(fileName)
	if extension == ".yaml" || extension == ".yml" {
		var s map[interface{}]interface{}
		if yaml.Unmarshal(manifestContent, &s) != nil {
			return m.prepareAndLogError(ctx, http.StatusBadRequest, errors.Errorf("Manifest content of file %s for cluster ID %s has an invalid YAML format", fileName, string(clusterID)))
		}
	} else if extension == ".json" {
		if !json.Valid(manifestContent) {
			return m.prepareAndLogError(ctx, http.StatusBadRequest, errors.Errorf("Manifest content of file %s for cluster ID %s has an illegal JSON format", fileName, string(clusterID)))
		}
	} else if strings.HasPrefix(extension, ".patch") && (strings.Contains(fileName, ".yaml.patch") || strings.Contains(fileName, ".yml.patch")) {
		var s []map[interface{}]interface{}
		if yaml.Unmarshal(manifestContent, &s) != nil {
			return m.prepareAndLogError(ctx, http.StatusBadRequest, errors.Errorf("Patch content of file %s for cluster ID %s has an invalid YAML format", fileName, string(clusterID)))
		}
	} else {
		return m.prepareAndLogError(ctx, http.StatusBadRequest, errors.Errorf("Manifest filename of file %s for cluster ID %s is invalid. Only json, yaml and yml extensions are supported", fileName, string(clusterID)))
	}
	return nil
}

func (m *Manifests) decodeUserSuppliedManifest(ctx context.Context, clusterID strfmt.UUID, manifest string) ([]byte, error) {
	manifestContent, err := base64.StdEncoding.DecodeString(manifest)
	if err != nil {
		return nil, m.prepareAndLogError(ctx, http.StatusBadRequest, errors.Errorf("failed to base64-decode cluster manifest content for cluster %s", string(clusterID)))
	}
	return manifestContent, nil
}

func (m *Manifests) getManifestPathsFromParameters(ctx context.Context, folder *string, fileName *string) (string, string, string) {
	if folder == nil {
		defaultFolder := models.CreateManifestParamsFolderManifests
		folder = &defaultFolder
	}
	return *folder, *fileName, filepath.Join(*folder, *fileName)
}

func (m *Manifests) uploadManifest(ctx context.Context, content []byte, clusterID strfmt.UUID, path string) error {
	objectName := GetManifestObjectName(clusterID, path)
	if err := m.objectHandler.Upload(ctx, content, objectName); err != nil {
		return m.prepareAndLogError(ctx, http.StatusInternalServerError, errors.Wrapf(err, "Failed to upload mainfest object %s for cluster %s", objectName, clusterID))
	}
	return nil
}

func (m *Manifests) deleteManifest(ctx context.Context, clusterID strfmt.UUID, path string) error {
	log := logutil.FromContext(ctx, m.log)
	objectName := GetManifestObjectName(clusterID, path)
	exists, err := m.objectHandler.DoesObjectExist(ctx, objectName)
	if err != nil {
		return m.prepareAndLogError(
			ctx,
			http.StatusInternalServerError,
			errors.Wrapf(err, "There was an error while determining the existence of manifest %s for cluster %s", path, clusterID))
	}
	if !exists {
		log.Infof("Cluster manifest %s doesn't exists for cluster %s", path, clusterID)
		return nil
	}
	if _, err = m.objectHandler.DeleteObject(ctx, objectName); err != nil {
		return m.prepareAndLogError(
			ctx,
			http.StatusInternalServerError,
			errors.Wrapf(err, "Failed to delete object %s from storage for cluster %s", objectName, clusterID))
	}
	err = m.unmarkUserSuppliedManifest(ctx, clusterID, path)
	if err != nil {
		return err
	}
	return nil
}
