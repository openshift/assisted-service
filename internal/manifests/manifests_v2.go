package manifests

import (
	"context"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/usage"
	logutil "github.com/openshift/assisted-service/pkg/log"
	operations "github.com/openshift/assisted-service/restapi/operations/manifests"
)

func (m *Manifests) V2CreateClusterManifest(ctx context.Context, params operations.V2CreateClusterManifestParams) middleware.Responder {
	log := logutil.FromContext(ctx, m.log)
	manifest, err := m.CreateClusterManifestInternal(ctx, params, true)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	err = m.setUsage(true, params.ClusterID)
	if err != nil {
		// We don't want to return the error - the requested manifest was set successfully,  setting the feature usage failed.
		log.Errorf("Failed to set feature usage '%s' Error: %v. Manifest %v created by user successfully.", usage.CustomManifest, err, manifest)
	}
	return operations.NewV2CreateClusterManifestCreated().WithPayload(manifest)
}

func (m *Manifests) V2UpdateClusterManifest(ctx context.Context, params operations.V2UpdateClusterManifestParams) middleware.Responder {
	manifest, err := m.UpdateClusterManifestInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return operations.NewV2UpdateClusterManifestOK().WithPayload(manifest)
}

func (m *Manifests) V2ListClusterManifests(ctx context.Context, params operations.V2ListClusterManifestsParams) middleware.Responder {
	manifests, err := m.ListClusterManifestsInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return operations.NewV2ListClusterManifestsOK().WithPayload(manifests)
}

func (m *Manifests) V2DeleteClusterManifest(ctx context.Context, params operations.V2DeleteClusterManifestParams) middleware.Responder {
	log := logutil.FromContext(ctx, m.log)
	err := m.DeleteClusterManifestInternal(ctx, params)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}

	// Unset feature usage if this was the last manifest. Don't fail on error because we successfully deleted the manifest as requested.
	manifests, err := m.ListClusterManifestsInternal(ctx, operations.V2ListClusterManifestsParams{ClusterID: params.ClusterID})
	if err != nil {
		log.Errorf("Failed to unset feature usage '%s' Error: %v. Manifest %v deleted by user successfully.", usage.CustomManifest, err, params.FileName)
	} else {
		if len(manifests) == 0 {
			err = m.setUsage(false, params.ClusterID)
			if err != nil {
				log.Errorf("Failed to unset feature usage '%s' Error: %v. Manifest %v deleted by user successfully.", usage.CustomManifest, err, params.FileName)
			}
		}
	}

	return operations.NewV2DeleteClusterManifestOK()
}
