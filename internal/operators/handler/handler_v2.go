package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-openapi/runtime/middleware"
	"github.com/hashicorp/go-version"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/featuresupport"
	"github.com/openshift/assisted-service/models"
	logutil "github.com/openshift/assisted-service/pkg/log"
	restoperators "github.com/openshift/assisted-service/restapi/operations/operators"
	"github.com/pkg/errors"
)

// validateBundleParameters validates the parameters for bundle operations
func (h *Handler) validateBundleParameters(openshiftVersion *string, cpuArchitecture *string, platformType *models.PlatformType, externalPlatformName *string) middleware.Responder {
	if openshiftVersion == nil || *openshiftVersion == "" {
		// No filtering
		return nil
	}

	if cpuArchitecture == nil || *cpuArchitecture == "" {
		// This should never happen
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf("cpu architecture is required"))
	}

	// Validate OpenShift version format
	_, err := version.NewVersion(*openshiftVersion)
	if err != nil {
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf("invalid openshift version: %w", err))
	}

	// Validate CPU architecture support
	archSupported, err := featuresupport.IsArchitectureSupported(*cpuArchitecture, *openshiftVersion)
	if err != nil {
		h.log.Errorf("Failed to validate CPU architecture support: %v", err)

		return common.NewApiError(http.StatusInternalServerError, errors.New("failed to validate CPU architecture support"))
	}

	if !archSupported {
		return common.NewApiError(http.StatusBadRequest, fmt.Errorf("cpu architecture %s is not supported for openshift version %s", *cpuArchitecture, *openshiftVersion))
	}

	// Validate platform support if platform type is provided
	if platformType != nil {
		platformSupported, err := featuresupport.IsPlatformSupported(*platformType, externalPlatformName, *openshiftVersion, *cpuArchitecture)
		if err != nil {
			h.log.Errorf("Failed to validate platform support: %v", err)

			return common.NewApiError(http.StatusBadRequest, errors.New("failed to validate platform support"))
		}

		if !platformSupported {
			return common.NewApiError(http.StatusBadRequest, fmt.Errorf("platform %s is not supported for openshift version %s and cpu architecture %s", *platformType, *openshiftVersion, *cpuArchitecture))
		}
	}

	return nil
}

// V2ListOfClusterOperators Lists operators to be monitored for a cluster.
func (h *Handler) V2ListOfClusterOperators(ctx context.Context, params restoperators.V2ListOfClusterOperatorsParams) middleware.Responder {
	operatorsList, err := h.GetMonitoredOperators(ctx, params.ClusterID, params.OperatorName, h.db)
	if err != nil {
		return common.GenerateErrorResponder(err)
	}
	return restoperators.NewV2ListOfClusterOperatorsOK().WithPayload(operatorsList)
}

// V2ListOperatorProperties Lists properties for an operator name.
func (h *Handler) V2ListOperatorProperties(ctx context.Context, params restoperators.V2ListOperatorPropertiesParams) middleware.Responder {
	log := logutil.FromContext(ctx, h.log)

	properties, err := h.operatorsAPI.GetOperatorProperties(params.OperatorName)
	if err != nil {
		log.Errorf("%s operator has not been found", params.OperatorName)

		return restoperators.NewV2ListOperatorPropertiesNotFound()
	}

	return restoperators.NewV2ListOperatorPropertiesOK().WithPayload(properties)
}

// V2ListSupportedOperators Retrieves the list of supported operators.
func (h *Handler) V2ListSupportedOperators(_ context.Context, _ restoperators.V2ListSupportedOperatorsParams) middleware.Responder {
	return restoperators.NewV2ListSupportedOperatorsOK().WithPayload(h.operatorsAPI.GetSupportedOperators())
}

// V2ListBundles Retrieves the list of supported bundles filtered by feature support.
func (h *Handler) V2ListBundles(_ context.Context, params restoperators.V2ListBundlesParams) middleware.Responder {
	// Convert platform type to models.PlatformType if provided
	var platformType *models.PlatformType
	if params.PlatformType != nil {
		pt := models.PlatformType(*params.PlatformType)
		platformType = &pt
	}

	// Validate parameters
	err := h.validateBundleParameters(params.OpenshiftVersion, params.CPUArchitecture, platformType, params.ExternalPlatformName)
	if err != nil {
		return err
	}

	// Create SupportLevelFilters struct
	var filters *featuresupport.SupportLevelFilters

	if params.OpenshiftVersion != nil && *params.OpenshiftVersion != "" {
		filters = &featuresupport.SupportLevelFilters{
			OpenshiftVersion:     *params.OpenshiftVersion,
			CPUArchitecture:      params.CPUArchitecture,
			PlatformType:         platformType,
			ExternalPlatformName: params.ExternalPlatformName,
		}
	}

	// Convert string slice to FeatureSupportLevelID slice
	var featureIDs []models.FeatureSupportLevelID
	for _, featureID := range params.FeatureIds {
		featureIDs = append(featureIDs, models.FeatureSupportLevelID(featureID))
	}

	// Get filtered bundles using featuresupport API
	filteredBundles := h.operatorsAPI.ListBundles(filters, featureIDs)

	return restoperators.NewV2ListBundlesOK().WithPayload(filteredBundles)
}

// V2GetBundle Retrieves the Bundle object for a specific bundleName.
func (h *Handler) V2GetBundle(ctx context.Context, params restoperators.V2GetBundleParams) middleware.Responder {
	log := logutil.FromContext(ctx, h.log)

	// Convert string slice to FeatureSupportLevelID slice
	var featureIDs []models.FeatureSupportLevelID
	for _, featureID := range params.FeatureIds {
		featureIDs = append(featureIDs, models.FeatureSupportLevelID(featureID))
	}

	bundle, err := h.operatorsAPI.GetBundle(params.ID, featureIDs)
	if err != nil {
		log.Errorf("Failed to get operators for bundle %s: %v", params.ID, err)
		return common.GenerateErrorResponder(err)
	}

	return restoperators.NewV2GetBundleOK().WithPayload(bundle)
}
