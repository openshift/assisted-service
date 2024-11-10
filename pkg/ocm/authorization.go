package ocm

import (
	"context"
	"fmt"
	"net/http"

	azv1 "github.com/openshift-online/ocm-sdk-go/authorizations/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/pkg/commonutils"
	"github.com/pkg/errors"
)

//go:generate mockgen -source=authorization.go -package=ocm -destination=mock_authorization.go
type OCMAuthorization interface {
	ResourceReview(ctx context.Context, username, action, resourceType string) (allowed []string, err error)
	AccessReview(ctx context.Context, username, action, subscriptionId, resourceType string) (allowed bool, err error)
	CapabilityReview(ctx context.Context, username, capabilityName, capabilityType string) (allowed bool, err error)
}

type authorization struct {
	client *Client
}

func (a authorization) ResourceReview(ctx context.Context, username, action, resourceType string) (allowed []string, err error) {
	defer commonutils.MeasureOperation("OCM-ResourceReview", a.client.log, a.client.metricsApi)()
	resourceReview := a.client.connection.Authorizations().V1().ResourceReview()

	requestBuilder := azv1.NewResourceReviewRequest().
		AccountUsername(username).
		Action(action).
		ResourceType(resourceType)

	request, err := requestBuilder.Build()
	if err != nil {
		return nil, err
	}

	postResp, err := resourceReview.Post().
		Request(request).
		SendContext(ctx)
	if err != nil {
		if postResp != nil {
			a.client.logger.Error(context.Background(), "Fail to send ResourceReview. Response: %v", postResp)
			if postResp.Status() >= 400 && postResp.Status() < 500 {
				return nil, common.NewInfraError(http.StatusUnauthorized, err)
			}
			if postResp.Status() >= 500 {
				return nil, common.NewApiError(http.StatusServiceUnavailable, err)
			}
		}
		return nil, common.NewApiError(http.StatusServiceUnavailable, err)
	}

	response, ok := postResp.GetReview()
	if !ok {
		return nil, errors.Errorf("Empty response from authorization post request")
	}

	clusterIDs, ok := response.GetClusterIDs()
	if !ok {
		return nil, errors.Errorf("Failed to get cluster IDs from the response")
	}
	return clusterIDs, nil
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

func (a authorization) getUserInternalOrgId(username string) (string, error) {
	var err error

	search := fmt.Sprintf("username='%s'", username)
	usersListRep, err := a.client.connection.AccountsMgmt().V1().Accounts().List().Search(search).Send()
	if err != nil {
		return "", err
	}

	users, ok := usersListRep.GetItems()
	if !ok {
		return "", errors.New("failed to retrieve list of accounts from response")
	}

	if users.Len() != 1 {
		return "", errors.New(fmt.Sprintf("unexpected accounts search result size. Expected 1, result size is %d", users.Len()))
	}

	return users.Get(0).Organization().ID(), nil
}

func (a authorization) CapabilityReview(ctx context.Context, username, capabilityName, capabilityType string) (allowed bool, err error) {
	defer commonutils.MeasureOperation("OCM-CapabilityReview", a.client.log, a.client.metricsApi)()
	capabilityReview := a.client.connection.Authorizations().V1().CapabilityReview()

	request := azv1.NewCapabilityReviewRequest().
		AccountUsername(username).
		Capability(capabilityName).
		Type(capabilityType)

	if capabilityType == OrganizationCapabilityType {
		orgId, err1 := a.getUserInternalOrgId(username)
		if err1 != nil {
			return false, err1
		}
		request.OrganizationID(orgId)
	}

	capabilityReviewRequest, err := request.Build()
	if err != nil {
		return false, common.NewApiError(http.StatusInternalServerError, err)
	}

	postResp, err := capabilityReview.Post().
		Request(capabilityReviewRequest).
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
