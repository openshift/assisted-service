package ocm

import (
	"context"

	"github.com/go-openapi/strfmt"
	amgmtv1 "github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1"
	"github.com/openshift/assisted-service/pkg/commonutils"
	"github.com/pkg/errors"
)

const (
	ProductCategoryAssistedInstall   = "AssistedInstall"
	ProductIdOCP                     = "OCP"
	SubscriptionStatusActive         = "Active"
	SubscriptionStatusReserved       = "Reserved"
	clusterAuthorizationsPostRequest = "ClusterAuthorizationsPost"
	subscriptionGetRequest           = "SubscriptionGet"
	subscriptionPatchRequest         = "SubscriptionPatch"
	subscriptionDeleteRequest        = "SubscriptionDelete"
)

//go:generate mockgen --build_flags=--mod=mod -package ocm -destination mock_accounts_mgmt.go . OCMAccountsMgmt
type OCMAccountsMgmt interface {
	CreateSubscription(ctx context.Context, clusterID strfmt.UUID, clusterName string) (*amgmtv1.Subscription, error)
	GetSubscription(ctx context.Context, subscriptionID strfmt.UUID) (*amgmtv1.Subscription, error)
	UpdateSubscriptionOpenshiftClusterID(ctx context.Context, subscriptionID, openshiftClusterID strfmt.UUID) error
	UpdateSubscriptionStatusActive(ctx context.Context, subscriptionID strfmt.UUID) error
	UpdateSubscriptionDisplayName(ctx context.Context, subscriptionID strfmt.UUID, displayName string) error
	UpdateSubscriptionConsoleUrl(ctx context.Context, subscriptionID strfmt.UUID, consoleUrl string) error
	DeleteSubscription(ctx context.Context, subscriptionID strfmt.UUID) error
}

type accountsMgmt struct {
	client *Client
}

func (a accountsMgmt) CreateSubscription(ctx context.Context, clusterID strfmt.UUID, clusterName string) (*amgmtv1.Subscription, error) {
	defer commonutils.MeasureOperation("OCM-CreateClusterAuthorization", a.client.log, a.client.metricsApi)()

	// create the request
	request, err := amgmtv1.NewClusterAuthorizationRequest().
		AccountUsername(UserNameFromContext(ctx)).
		ProductCategory(ProductCategoryAssistedInstall).
		ProductID(ProductIdOCP).
		ClusterID(clusterID.String()).
		DisplayName(clusterName).
		Managed(false).
		Resources().
		Reserve(true).
		Build()
	if err != nil {
		a.client.logger.Error(ctx, "Failed to create cluster authorization request. Error: %v", err)
		return nil, err
	}

	// send the request
	response, err := a.client.connection.AccountsMgmt().V1().ClusterAuthorizations().Post().Request(request).SendContext(ctx)
	if err = HandleOCMResponse(ctx, a.client.logger, response, clusterAuthorizationsPostRequest, err); err != nil {
		return nil, err
	}
	responseVal, ok := response.GetResponse()
	if !ok {
		return nil, errors.Errorf("Empty response from %s request", clusterAuthorizationsPostRequest)
	}

	return responseVal.Subscription(), nil
}

func (a accountsMgmt) GetSubscription(ctx context.Context, subscriptionID strfmt.UUID) (*amgmtv1.Subscription, error) {
	defer commonutils.MeasureOperation("OCM-GetSubscription", a.client.log, a.client.metricsApi)()

	// send the request
	response, err := a.client.connection.AccountsMgmt().V1().Subscriptions().Subscription(subscriptionID.String()).Get().SendContext(ctx)
	if err = HandleOCMResponse(ctx, a.client.logger, response, subscriptionGetRequest, err); err != nil {
		return nil, err
	}
	responseVal, ok := response.GetBody()
	if !ok {
		return nil, errors.Errorf("Empty response from %s request", subscriptionGetRequest)
	}

	return responseVal, nil
}

func (a accountsMgmt) updateSubscription(ctx context.Context, subscriptionID strfmt.UUID, sub *amgmtv1.Subscription, err error) error {
	if err != nil {
		a.client.logger.Error(ctx, "Failed to create subscription request. Error: %v", err)
		return err
	}
	return a.sendSubscriptionUpdateRequest(ctx, subscriptionID, sub)
}

func (a accountsMgmt) UpdateSubscriptionOpenshiftClusterID(ctx context.Context, subscriptionID, openshiftClusterID strfmt.UUID) error {
	defer commonutils.MeasureOperation("OCM-UpdateSubscriptionOpenshiftClusterID", a.client.log, a.client.metricsApi)()

	sub, err := amgmtv1.NewSubscription().ExternalClusterID(openshiftClusterID.String()).Build()
	err = a.updateSubscription(ctx, subscriptionID, sub, err)
	if err == nil {
		a.client.logger.Info(ctx, "Updated openshift cluster ID in subscription %s", subscriptionID)
	}
	return err
}

func (a accountsMgmt) UpdateSubscriptionStatusActive(ctx context.Context, subscriptionID strfmt.UUID) error {
	defer commonutils.MeasureOperation("OCM-UpdateSubscriptionStatusActive", a.client.log, a.client.metricsApi)()

	sub, err := amgmtv1.NewSubscription().Status(SubscriptionStatusActive).Build()
	err = a.updateSubscription(ctx, subscriptionID, sub, err)
	if err == nil {
		a.client.logger.Info(ctx, "Updated status 'Active' in subscription %s", subscriptionID)
	}
	return err
}

func (a accountsMgmt) UpdateSubscriptionDisplayName(ctx context.Context, subscriptionID strfmt.UUID, displayName string) error {
	defer commonutils.MeasureOperation("OCM-UpdateSubscriptionDisplayName", a.client.log, a.client.metricsApi)()

	sub, err := amgmtv1.NewSubscription().DisplayName(displayName).Build()
	err = a.updateSubscription(ctx, subscriptionID, sub, err)
	if err == nil {
		a.client.logger.Info(ctx, "Updated display-name in subscription %s", subscriptionID)
	}
	return err
}

func (a accountsMgmt) UpdateSubscriptionConsoleUrl(ctx context.Context, subscriptionID strfmt.UUID, consoleUrl string) error {
	defer commonutils.MeasureOperation("OCM-UpdateSubscriptionConsoleUrl", a.client.log, a.client.metricsApi)()

	sub, err := amgmtv1.NewSubscription().ConsoleURL(consoleUrl).Build()
	err = a.updateSubscription(ctx, subscriptionID, sub, err)
	if err == nil {
		a.client.logger.Info(ctx, "Updated console-url in subscription %s", subscriptionID)
	}
	return err
}

func (a accountsMgmt) DeleteSubscription(ctx context.Context, subscriptionID strfmt.UUID) error {
	defer commonutils.MeasureOperation("OCM-DeleteSubscription", a.client.log, a.client.metricsApi)()

	// send the request
	response, err := a.client.connection.AccountsMgmt().V1().Subscriptions().Subscription(subscriptionID.String()).Delete().SendContext(ctx)
	if err = HandleOCMResponse(ctx, a.client.logger, response, subscriptionDeleteRequest, err); err != nil {
		return err
	}

	return nil
}

func (a accountsMgmt) sendSubscriptionUpdateRequest(ctx context.Context, subscriptionID strfmt.UUID, sub *amgmtv1.Subscription) error {

	response, err := a.client.connection.AccountsMgmt().V1().Subscriptions().Subscription(subscriptionID.String()).Update().Body(sub).SendContext(ctx)
	if err = HandleOCMResponse(ctx, a.client.logger, response, subscriptionPatchRequest, err); err != nil {
		return err
	}
	if _, ok := response.GetBody(); !ok {
		return errors.Errorf("Empty response from %s request", subscriptionPatchRequest)
	}
	return nil
}
