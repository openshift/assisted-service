package subsystem

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/pkg/ocm"
)

type StubDefinition struct {
	Request  *RequestDefinition  `json:"request"`
	Response *ResponseDefinition `json:"response"`
}

type RequestDefinition struct {
	URL          string              `json:"url"`
	Method       string              `json:"method"`
	BodyPatterns []map[string]string `json:"bodyPatterns"`
	Headers      map[string]string   `json:"headers"`
}

type ResponseDefinition struct {
	Status  int               `json:"status"`
	Body    string            `json:"body"`
	Headers map[string]string `json:"headers"`
}

type Mapping struct {
	ID string
}

type WireMock struct {
	OCMHost   string
	TestToken string
}

type subscription struct {
	ID     strfmt.UUID `json:"id"`
	Status string      `json:"status"`
}

const (
	wiremockMappingsPath     string      = "/__admin/mappings"
	capabilityReviewPath     string      = "/api/authorizations/v1/capability_review"
	accessReviewPath         string      = "/api/authorizations/v1/access_review"
	pullAuthPath             string      = "/api/accounts_mgmt/v1/token_authorization"
	clusterAuthzPath         string      = "/api/accounts_mgmt/v1/cluster_authorizations"
	subscriptionPrefix       string      = "/api/accounts_mgmt/v1/subscriptions/"
	tokenPath                string      = "/token"
	fakePayloadUsername      string      = "jdoe123@example.com"
	fakePayloadAdmin         string      = "admin@example.com"
	fakePayloadUnallowedUser string      = "unallowed@example.com"
	FakePS                   string      = "dXNlcjpwYXNzd29yZAo="
	FakeAdminPS              string      = "dXNlcjpwYXNzd29yZAy="
	WrongPullSecret          string      = "wrong_secret"
	FakeSubscriptionID       strfmt.UUID = "1h89fvtqeelulpo0fl5oddngj2ao7tt8"
)

var (
	subscriptionPath string = filepath.Join(subscriptionPrefix, FakeSubscriptionID.String())
)

func (w *WireMock) CreateWiremockStubsForOCM() error {
	if err := w.createStubsForAccessReview(); err != nil {
		return err
	}

	if err := w.createStubsForCapabilityReview(); err != nil {
		return err
	}

	if _, err := w.createStubTokenAuth(FakePS, fakePayloadUsername); err != nil {
		return err
	}

	if _, err := w.createStubTokenAuth(FakeAdminPS, fakePayloadAdmin); err != nil {
		return err
	}

	if _, err := w.createStubToken(w.TestToken); err != nil {
		return err
	}

	if err := w.createStubsForCreatingAMSSubscription(http.StatusOK); err != nil {
		return err
	}

	if err := w.createStubsForGettingAMSSubscription(http.StatusOK); err != nil {
		return err
	}

	if err := w.createStubsForUpdatingAMSSubscription(http.StatusOK); err != nil {
		return err
	}

	if err := w.createStubsForDeletingAMSSubscription(http.StatusOK); err != nil {
		return err
	}

	return nil
}

func (w *WireMock) createStubsForAccessReview() error {
	if _, err := w.createStubAccessReview(fakePayloadUsername, true); err != nil {
		return err
	}
	return nil
}

func (w *WireMock) createStubsForCapabilityReview() error {
	if _, err := w.createStubCapabilityReview(fakePayloadUsername, false); err != nil {
		return err
	}
	return nil
}

func (w *WireMock) createStubsForCreatingAMSSubscription(resStatus int) error {

	type reservedResource struct{}

	type clusterAuthorizationRequest struct {
		AccountUsername string              `json:"account_username"`
		ProductCategory string              `json:"product_category"`
		ProductID       string              `json:"product_id"`
		ClusterID       string              `json:"cluster_id"`
		Managed         bool                `json:"managed"`
		Resources       []*reservedResource `json:"resources"`
		Reserve         bool                `json:"reserve"`
	}

	type clusterAuthorizationResponse struct {
		Subscription subscription `json:"subscription"`
	}

	caRequest := clusterAuthorizationRequest{
		AccountUsername: "${json-unit.any-string}",
		ProductCategory: ocm.ProductCategoryAssistedInstall,
		ProductID:       ocm.ProductIdOCP,
		ClusterID:       "${json-unit.any-string}",
		Managed:         false,
		Resources:       []*reservedResource{},
		Reserve:         true,
	}

	caResponse := clusterAuthorizationResponse{
		Subscription: subscription{ID: FakeSubscriptionID},
	}

	var reqBody []byte
	reqBody, err := json.Marshal(caRequest)
	if err != nil {
		return err
	}

	var resBody []byte
	resBody, err = json.Marshal(caResponse)
	if err != nil {
		return err
	}

	amsSubscriptionStub := w.createStubDefinition(clusterAuthzPath, "POST", string(reqBody), string(resBody), resStatus)
	_, err = w.addStub(amsSubscriptionStub)
	return err
}

func (w *WireMock) createStubsForGettingAMSSubscription(resStatus int) error {

	subResponse := subscription{
		ID:     FakeSubscriptionID,
		Status: ocm.SubscriptionStatusReserved,
	}

	var resBody []byte
	resBody, err := json.Marshal(subResponse)
	if err != nil {
		return err
	}

	amsSubscriptionStub := w.createStubDefinition(subscriptionPath, "GET", "", string(resBody), resStatus)
	_, err = w.addStub(amsSubscriptionStub)
	return err
}

func (w *WireMock) createStubsForUpdatingAMSSubscription(resStatus int) error {

	type subscriptionUpdateRequest struct {
		ExternalClusterID strfmt.UUID `json:"external_cluster_id"`
		Status            string      `json:"status"`
	}

	suRequest := subscriptionUpdateRequest{
		ExternalClusterID: "${json-unit.any-string}",
		Status:            ocm.SubscriptionStatusActive,
	}

	subResponse := subscription{
		ID: FakeSubscriptionID,
	}

	var reqBody []byte
	reqBody, err := json.Marshal(suRequest)
	if err != nil {
		return err
	}

	var resBody []byte
	resBody, err = json.Marshal(subResponse)
	if err != nil {
		return err
	}

	amsSubscriptionStub := w.createStubDefinition(subscriptionPath, "PATCH", string(reqBody), string(resBody), resStatus)
	_, err = w.addStub(amsSubscriptionStub)
	return err
}

func (w *WireMock) createStubsForDeletingAMSSubscription(resStatus int) error {

	amsSubscriptionStub := w.createStubDefinition(subscriptionPath, "DELETE", "", "", resStatus)
	_, err := w.addStub(amsSubscriptionStub)
	return err
}

func (w *WireMock) createStubToken(testToken string) (string, error) {
	type TokenResponse struct {
		AccessToken      string `json:"access_token,omitempty"`
		Error            string `json:"error,omitempty"`
		ErrorDescription string `json:"error_description,omitempty"`
		RefreshToken     string `json:"refresh_token,omitempty"`
		TokenType        string `json:"token_type,omitempty"`
	}
	tokenResponse := TokenResponse{
		AccessToken:  testToken,
		RefreshToken: testToken,
		TokenType:    "bearer",
	}

	var resBody []byte
	resBody, err := json.Marshal(tokenResponse)
	if err != nil {
		return "", err
	}

	tokenStub := &StubDefinition{
		Request: &RequestDefinition{
			URL:    tokenPath,
			Method: "POST",
		},
		Response: &ResponseDefinition{
			Status: 200,
			Body:   string(resBody),
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		},
	}

	return w.addStub(tokenStub)
}

func (w *WireMock) createStubCapabilityReview(username string, result bool) (string, error) {
	type CapabilityRequest struct {
		Name     string `json:"capability"`
		Type     string `json:"type"`
		Username string `json:"account_username"`
	}

	type CapabilityResponse struct {
		Result string `json:"result"`
	}

	capabilityRequest := CapabilityRequest{
		Name:     ocm.CapabilityName,
		Type:     ocm.CapabilityType,
		Username: username,
	}

	capabilityResponse := CapabilityResponse{
		Result: strconv.FormatBool(result),
	}

	var reqBody []byte
	reqBody, err := json.Marshal(capabilityRequest)
	if err != nil {
		return "", err
	}

	var resBody []byte
	resBody, err = json.Marshal(capabilityResponse)
	if err != nil {
		return "", err
	}

	capabilityReviewStub := w.createStubDefinition(capabilityReviewPath, "POST", string(reqBody), string(resBody), 200)
	return w.addStub(capabilityReviewStub)
}

func (w *WireMock) createStubAccessReview(username string, allowed bool) (string, error) {
	type CapabilityRequest struct {
		ResourceType string `json:"resource_type"`
		Action       string `json:"action"`
		Username     string `json:"account_username"`
	}

	type CapabilityResponse struct {
		Allowed bool `json:"allowed"`
	}

	capabilityRequest := CapabilityRequest{
		Username:     username,
		Action:       ocm.AMSActionCreate,
		ResourceType: ocm.BareMetalClusterResource,
	}

	capabilityResponse := CapabilityResponse{
		Allowed: allowed,
	}

	var reqBody []byte
	reqBody, err := json.Marshal(capabilityRequest)
	if err != nil {
		return "", err
	}

	var resBody []byte
	resBody, err = json.Marshal(capabilityResponse)
	if err != nil {
		return "", err
	}

	capabilityReviewStub := w.createStubDefinition(accessReviewPath, "POST", string(reqBody), string(resBody), 200)
	return w.addStub(capabilityReviewStub)
}

func (w *WireMock) createStubTokenAuth(token, username string) (string, error) {
	type TokenAuthorizationRequest struct {
		AuthorizationToken string `json:"authorization_token"`
	}

	type Account struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Username  string `json:"username"`
		Email     string `json:"email"`
	}

	type TokenAuthorizationResponse struct {
		Account Account `json:"account"`
	}

	tokenAuthorizationRequest := TokenAuthorizationRequest{
		AuthorizationToken: token,
	}

	tokenAuthorizationResponse := TokenAuthorizationResponse{
		Account: Account{
			FirstName: "UserFirstName",
			LastName:  "UserLastName",
			Username:  username,
			Email:     "user@myorg.com",
		},
	}

	var reqBody []byte
	reqBody, err := json.Marshal(tokenAuthorizationRequest)
	if err != nil {
		return "", err
	}

	var resBody []byte
	resBody, err = json.Marshal(tokenAuthorizationResponse)
	if err != nil {
		return "", err
	}

	tokenAuthStub := w.createStubDefinition(pullAuthPath, "POST", string(reqBody), string(resBody), 200)
	return w.addStub(tokenAuthStub)
}

func (w *WireMock) createWrongStubTokenAuth(token string) (string, error) {
	type TokenAuthorizationRequest struct {
		AuthorizationToken string `json:"authorization_token"`
	}

	tokenAuthorizationRequest := TokenAuthorizationRequest{
		AuthorizationToken: token,
	}

	type ErrorResponse struct {
		Code        string `json:"code"`
		Href        string `json:"href"`
		ID          string `json:"id"`
		Kind        string `json:"kind"`
		OperationID string `json:"operation_id"`
		Reason      string `json:"reason"`
	}

	errorResponse := ErrorResponse{
		Code:        "ACCT-MGMT-7",
		Href:        "/api/accounts_mgmt/v1/errors/7",
		ID:          "7",
		Kind:        "Error",
		OperationID: "op_id",
		Reason:      "Unable to find credential with specified authorization token",
	}

	var reqBody []byte
	reqBody, err := json.Marshal(tokenAuthorizationRequest)
	if err != nil {
		return "", err
	}

	var resBody []byte
	resBody, err = json.Marshal(errorResponse)
	if err != nil {
		return "", err
	}

	tokenAuthStub := w.createStubDefinition(pullAuthPath, "POST", string(reqBody), string(resBody), 404)
	return w.addStub(tokenAuthStub)
}

func (w *WireMock) createStubDefinition(url, method, reqBody, resBody string, resStatus int) *StubDefinition {
	sd := &StubDefinition{
		Request: &RequestDefinition{
			URL:    url,
			Method: method,
		},
		Response: &ResponseDefinition{
			Status: resStatus,
			Body:   resBody,
			Headers: map[string]string{
				"Content-Type": "application/json",
			},
		},
	}
	if reqBody != "" {
		sd.Request.BodyPatterns = []map[string]string{
			{
				"equalToJson":         reqBody,
				"ignoreExtraElements": "true",
			},
		}
	}
	return sd
}

func (w *WireMock) addStub(stub *StubDefinition) (string, error) {
	requestBody, err := json.Marshal(stub)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	b.Write(requestBody)

	resp, err := http.Post("http://"+w.OCMHost+wiremockMappingsPath, "application/json", &b)
	if err != nil {
		return "", err
	}
	responseBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	ret := Mapping{}
	err = json.Unmarshal(responseBody, &ret)
	if err != nil {
		return "", err
	}
	return ret.ID, nil
}

func (w *WireMock) DeleteAllWiremockStubs() error {
	req, err := http.NewRequest("DELETE", "http://"+w.OCMHost+wiremockMappingsPath, nil)
	if err != nil {
		return err
	}
	client := &http.Client{}
	_, err = client.Do(req)
	return err
}

func (w *WireMock) DeleteStub(stubID string) error {
	req, err := http.NewRequest("DELETE", "http://"+w.OCMHost+wiremockMappingsPath+"/"+stubID, nil)
	if err != nil {
		return err
	}
	client := &http.Client{}
	_, err = client.Do(req)
	return err
}
