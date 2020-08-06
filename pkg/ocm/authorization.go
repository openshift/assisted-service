package ocm

import (
	"context"
	"encoding/json"
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
	con := a.client.connection
	accessReview := con.Authorizations().V1().AccessReview()

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
	// CapabilityReview is not available yet in ocm-sdk-go:
	// https://github.com/openshift-online/ocm-sdk-go/blob/master/authorizations/v1/root_client.go
	// Sending a simple POST request for now.
	con := a.client.connection
	request := con.Post()
	request.Path("/api/authorizations/v1/capability_review")

	type CapabilityRequest struct {
		Name     string `json:"capability"`
		Type     string `json:"type"`
		Username string `json:"account_username"`
	}

	capabilityRequest := CapabilityRequest{
		Name:     capabilityName,
		Type:     capabilityType,
		Username: username,
	}

	var jsonData []byte
	jsonData, err = json.Marshal(capabilityRequest)
	if err != nil {
		return false, err
	}
	request.Bytes(jsonData)

	postResp, err := request.SendContext(ctx)
	if err != nil {
		return false, err
	}

	var respJSON map[string]interface{}
	if err := json.Unmarshal(postResp.Bytes(), &respJSON); err != nil {
		return false, err
	}

	return respJSON["result"] == "true", nil
}
