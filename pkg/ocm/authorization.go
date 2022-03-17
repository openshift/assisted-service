package ocm

import (
	"context"
	"net/http"

	azv1 "github.com/openshift-online/ocm-sdk-go/authorizations/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/commonutils"
	"github.com/pkg/errors"
)

//go:generate mockgen -source=authorization.go -package=ocm -destination=mock_authorization.go
type OCMAuthorization interface {
	AccessReview(ctx context.Context, username, action, subscriptionId, resourceType string) (allowed bool, err error)
	CapabilityReview(ctx context.Context, username, capabilityName, capabilityType string) (allowed bool, err error)
}

type authorization struct {
	client *Client
}

func (a authorization) AccessReview(ctx context.Context, username, action, subscriptionId, resourceType string) (allowed bool, err error) {
	defer commonutils.MeasureOperation("OCM-AccessReview", a.client.log, a.client.metricsApi)()
	accessReview := a.client.connection.Authorizations().V1().AccessReview()

	requestBuilder := azv1.NewAccessReviewRequest().
		AccountUsername(username).
		Action(action).
		ResourceType(resourceType)

	if subscriptionId != "" {
		requestBuilder.SubscriptionID(subscriptionId)
	}

	request, err := requestBuilder.Build()
	if err != nil {
		return false, err
	}

	postResp, err := accessReview.Post().
		Request(request).
		SendContext(ctx)
	if err != nil {
		if postResp != nil {
			a.client.logger.Error(context.Background(), "Fail to send AccessReview. Response: %v", postResp)
			if postResp.Status() >= 400 && postResp.Status() < 500 {
				return false, common.NewInfraError(http.StatusUnauthorized, err)
			}
			if postResp.Status() >= 500 {
				return false, common.NewApiError(http.StatusServiceUnavailable, err)
			}
		}
		return false, common.NewApiError(http.StatusServiceUnavailable, err)
	}

	response, ok := postResp.GetResponse()
	if !ok {
		return false, errors.Errorf("Empty response from authorization post request")
	}

	return response.Allowed(), nil
}

func (a authorization) CapabilityReview(ctx context.Context, username, capabilityName, capabilityType string) (allowed bool, err error) {
	defer commonutils.MeasureOperation("OCM-CapabilityReview", a.client.log, a.client.metricsApi)()
	capabilityReview := a.client.connection.Authorizations().V1().CapabilityReview()

	request, err := azv1.NewCapabilityReviewRequest().
		AccountUsername(username).
		Capability(capabilityName).
		Type(capabilityType).
		Build()
	if err != nil {
		return false, common.NewApiError(http.StatusInternalServerError, err)
	}

	postResp, err := capabilityReview.Post().
		Request(request).
		SendContext(ctx)

	if err != nil {
		a.client.logger.Error(context.Background(), "Fail to send CapabilityReview. Error: %v", err)
		if postResp != nil {
			a.client.logger.Error(context.Background(), "Fail to send CapabilityReview. Response: %v", postResp)
			if postResp.Status() >= 400 && postResp.Status() < 500 {
				return false, common.NewInfraError(http.StatusUnauthorized, err)
			}
			if postResp.Status() >= 500 {
				return false, common.NewApiError(http.StatusServiceUnavailable, err)
			}
		}
		return false, common.NewApiError(http.StatusServiceUnavailable, err)
	}

	response, ok := postResp.GetResponse()
	if !ok {
		return false, errors.Errorf("Empty response from authorization CapabilityReview post request")
	}

	result, ok := response.GetResult()
	if !ok {
		return false, errors.Errorf("Failed to fetch result from the response CapabilityReview")
	}

	return result == "true", nil
}
