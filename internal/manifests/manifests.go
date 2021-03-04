package manifests

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/filemiddleware"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/restapi"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var _ restapi.ManifestsAPI = &Manifests{}

// ManifestFolder represents the manifests folder on s3 per cluster
const ManifestFolder = "manifests"

// NewManifestsAPI returns manifests API
func NewManifestsAPI(db *gorm.DB, log logrus.FieldLogger, objectHandler s3wrapper.API) *Manifests {
	return &Manifests{
		db:            db,
		log:           log,
		objectHandler: objectHandler,
	}
}

// Manifests represents manifests handler implementation
type Manifests struct {
	db            *gorm.DB
	log           logrus.FieldLogger
	objectHandler s3wrapper.API
}

func (m *Manifests) CreateClusterManifest(ctx context.Context, params operations.CreateClusterManifestParams) middleware.Responder {
	log := logutil.FromContext(ctx, m.log)
	log.Infof("Creating manifest in cluster %s", params.ClusterID)

	if params.CreateManifestParams.Folder == nil {
		defaultFolder := models.CreateManifestParamsFolderManifests
		params.CreateManifestParams.Folder = &defaultFolder
	}

	cluster, apierr := cluster.GetCluster(ctx, m.log, m.db, params.ClusterID.String())
	if apierr != nil {
		return common.GenerateErrorResponder(apierr)
	}

	fileName := filepath.Join(*params.CreateManifestParams.Folder, *params.CreateManifestParams.FileName)
	manifestContent, err := base64.StdEncoding.DecodeString(*params.CreateManifestParams.Content)
	if err != nil {
		log.WithError(err).Errorf("Cluster manifest %s for cluster %s failed to base64 decode: [%s]",
			fileName, cluster.ID, *params.CreateManifestParams.Content)
		return common.GenerateErrorResponderWithDefault(errors.New("failed to base64-decode cluster manifest content"), http.StatusBadRequest)
	}

	objectName := GetManifestObjectName(*cluster.ID, fileName)
	if err := m.objectHandler.Upload(ctx, manifestContent, objectName); err != nil {
		log.WithError(err).Errorf("Failed to upload %s", objectName)
		return common.GenerateErrorResponder(errors.Errorf("failed to upload %s to s3", objectName))
	}

	log.Infof("Done creating manifest %s for cluster %s", fileName, cluster.ID)
	manifest := models.Manifest{FileName: *params.CreateManifestParams.FileName, Folder: *params.CreateManifestParams.Folder}
	return operations.NewCreateClusterManifestCreated().WithPayload(&manifest)
}

func (m *Manifests) ListClusterManifests(ctx context.Context, params operations.ListClusterManifestsParams) middleware.Responder {
	log := logutil.FromContext(ctx, m.log)
	log.Infof("Listing manifests in cluster %s", params.ClusterID)

	cluster, apierr := cluster.GetCluster(ctx, m.log, m.db, params.ClusterID.String())
	if apierr != nil {
		return apierr
	}

	objectName := filepath.Join(cluster.ID.String(), ManifestFolder)
	files, err := m.objectHandler.ListObjectsByPrefix(ctx, objectName)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	manifests := models.ListManifests{}
	for _, file := range files {
		parts := strings.Split(strings.Trim(file, string(filepath.Separator)), string(filepath.Separator))
		if len(parts) > 2 {
			manifests = append(manifests, &models.Manifest{FileName: filepath.Join(parts[3:]...), Folder: parts[2]})
		} else {
			return common.GenerateErrorResponder(errors.Errorf("Cannot list file %s in cluster %s", file, cluster.ID))
		}
	}

	return operations.NewListClusterManifestsOK().WithPayload(manifests)
}

func (m *Manifests) DeleteClusterManifest(ctx context.Context, params operations.DeleteClusterManifestParams) middleware.Responder {
	log := logutil.FromContext(ctx, m.log)
	log.Infof("Deleting manifest from cluster %s", params.ClusterID)

	cluster, apierr := cluster.GetCluster(ctx, m.log, m.db, params.ClusterID.String())
	if apierr != nil {
		return common.GenerateErrorResponder(apierr)
	}

	if params.Folder == nil {
		defaultFolder := models.CreateManifestParamsFolderManifests
		params.Folder = &defaultFolder
	}
	fileName := filepath.Join(*params.Folder, params.FileName)
	objectName := GetManifestObjectName(*cluster.ID, fileName)
	exists, err := m.objectHandler.DoesObjectExist(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("Failed to delete cluster manifest %s from s3", objectName)
		return common.GenerateErrorResponder(err)
	}

	if !exists {
		log.Infof("Cluster manifest %s doesn't exists in s3 for cluster %s", fileName, cluster.ID)
		return operations.NewDeleteClusterManifestOK()
	}

	_, err = m.objectHandler.DeleteObject(ctx, objectName)
	if err != nil {
		return common.GenerateErrorResponder(errors.Errorf("failed to delete %s from s3", objectName))
	}

	log.Infof("Done deleting cluster manifest %s for cluster %s", fileName, cluster.ID)
	return operations.NewDeleteClusterManifestOK()
}

func (m *Manifests) DownloadClusterManifest(ctx context.Context, params operations.DownloadClusterManifestParams) middleware.Responder {
	log := logutil.FromContext(ctx, m.log)
	if params.Folder == nil {
		defaultFolder := models.CreateManifestParamsFolderManifests
		params.Folder = &defaultFolder
	}
	fileName := filepath.Join(*params.Folder, params.FileName)
	log.Infof("Downloading manifest %s from cluster %s", fileName, params.ClusterID)

	cluster, apierr := cluster.GetCluster(ctx, m.log, m.db, params.ClusterID.String())
	if apierr != nil {
		return apierr
	}

	objectName := GetManifestObjectName(*cluster.ID, fileName)
	exists, err := m.objectHandler.DoesObjectExist(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("Failed to download cluster manifest")
		return common.GenerateErrorResponder(err)
	}

	if !exists {
		msg := fmt.Sprintf("Cluster manifest %s doesn't exist in cluster %s", fileName, cluster.ID)
		log.Warn(msg)
		return common.GenerateErrorResponderWithDefault(errors.New(msg), http.StatusNotFound)
	}

	respBody, contentLength, err := m.objectHandler.Download(ctx, objectName)
	if err != nil {
		log.WithError(err).Errorf("failed to download file %s from cluster: %s", params.FileName, params.ClusterID.String())
		return common.GenerateErrorResponder(err)
	}

	return filemiddleware.NewResponder(operations.NewDownloadClusterManifestOK().WithPayload(respBody), params.FileName, contentLength)
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
