package ocm

import (
	"context"
	"fmt"

	azv1 "github.com/openshift-online/ocm-sdk-go/authorizations/v1"
)

type OCMAuthorization interface {
	AccessReview(ctx context.Context, username, action, resourceType string) (allowed bool, err error)
	CapabilityReview(ctx context.Context, username, capabilityName, capabilityType string) (allowed bool, err error)
}

type authorization struct {
	client *Client
}

func (a authorization) AccessReview(ctx context.Context, username, action, resourceType string) (allowed bool, err error) {
	connection, err := a.client.NewConnection()
	if err != nil {
		return false, err
	}
	defer connection.Close()

	accessReview := connection.Authorizations().V1().AccessReview()

	request, err := azv1.NewAccessReviewRequest().
		AccountUsername(username).
		Action(action).
		ResourceType(resourceType).
		Build()
	if err != nil {
		return false, err
	}

	postResp, err := accessReview.Post().
		Request(request).
		SendContext(ctx)
	if err != nil {
		return false, err
	}

	response, ok := postResp.GetResponse()
	if !ok {
		return false, fmt.Errorf("Empty response from authorization post request")
	}

	return response.Allowed(), nil
}

func (a authorization) CapabilityReview(ctx context.Context, username, capabilityName, capabilityType string) (allowed bool, err error) {
	connection, err := a.client.NewConnection()
	if err != nil {
		return false, err
	}
	defer connection.Close()

	capabilityReview := connection.Authorizations().V1().CapabilityReview()

	request, err := azv1.NewCapabilityReviewRequest().
		AccountUsername(username).
		Capability(capabilityName).
		Type(capabilityType).
		Build()
	if err != nil {
		return false, err
	}

	postResp, err := capabilityReview.Post().
		Request(request).
		SendContext(ctx)
	if err != nil {
		return false, err
	}

	response, ok := postResp.GetResponse()
	if !ok {
		return false, fmt.Errorf("Empty response from authorization post request")
	}

	result, ok := response.GetResult()
	if !ok {
		return false, fmt.Errorf("Failed to fetch result from the response")
	}

	return result == "true", nil
}
