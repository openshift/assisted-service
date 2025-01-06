package utils_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/releasesources"
	"github.com/openshift/assisted-service/models"
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
	OCMHost        string
	TestToken      string
	ReleaseSources models.ReleaseSources
}

type subscription struct {
	ID     strfmt.UUID `json:"id"`
	Status string      `json:"status"`
}

const (
	WiremockMappingsPath                 string      = "/__admin/mappings"
	CapabilityReviewPath                 string      = "/api/authorizations/v1/capability_review"
	AccessReviewPath                     string      = "/api/authorizations/v1/access_review"
	PullAuthPath                         string      = "/api/accounts_mgmt/v1/token_authorization"
	ClusterAuthzPath                     string      = "/api/accounts_mgmt/v1/cluster_authorizations"
	SubscriptionPrefix                   string      = "/api/accounts_mgmt/v1/subscriptions/"
	AccountsMgmtSearchPrefix             string      = "/api/accounts_mgmt/v1/accounts?search=username"
	SubscriptionUpdateOpenshiftClusterID string      = "subscription_update_openshift_cluster_id"
	SubscriptionUpdateStatusActive       string      = "subscription_update_status_active"
	SubscriptionUpdateDisplayName        string      = "subscription_update_display_name"
	SubscriptionUpdateConsoleUrl         string      = "subscription_update_console_url"
	TokenPath                            string      = "/token"
	FakePayloadUsername                  string      = "jdoe123@example.com"
	FakePayloadUsername2                 string      = "bob@example.com"
	FakePayloadAdmin                     string      = "admin@example.com"
	FakePayloadUnallowedUser             string      = "unallowed@example.com"
	FakePayloadClusterEditor             string      = "alice@example.com"
	FakePS                               string      = "dXNlcjpwYXNzd29yZAo="
	FakePS2                              string      = "dXNlcjI6cGFzc3dvcmQK"
	FakePS3                              string      = "dXNlcjM6cGFzc3dvcmQ="
	FakeAdminPS                          string      = "dXNlcjpwYXNzd29yZAy="
	WrongPullSecret                      string      = "wrong_secret"
	OrgId1                               string      = "1010101"
	OrgId2                               string      = "2020202"
	FakeSubscriptionID                   strfmt.UUID = "1h89fvtqeelulpo0fl5oddngj2ao7tt8"
)

var (
	subscriptionPath string = filepath.Join(SubscriptionPrefix, FakeSubscriptionID.String())
)

func (w *WireMock) CreateWiremockStubsForOCM() error {
	if err := w.CreateStubsForAccessReview(); err != nil {
		return err
	}

	if err := w.CreateStubsForCapabilityReview(); err != nil {
		return err
	}

	if err := w.CreateStubsForClusterEditor(); err != nil {
		return err
	}

	if _, err := w.CreateStubTokenAuth(FakePS, FakePayloadUsername); err != nil {
		return err
	}

	if _, err := w.CreateStubTokenAuth(FakePS2, FakePayloadUsername2); err != nil {
		return err
	}

	if _, err := w.CreateStubTokenAuth(FakePS3, FakePayloadClusterEditor); err != nil {
		return err
	}

	if _, err := w.CreateStubTokenAuth(FakeAdminPS, FakePayloadAdmin); err != nil {
		return err
	}

	if _, err := w.CreateStubToken(w.TestToken); err != nil {
		return err
	}

	if err := w.CreateStubsForCreatingAMSSubscription(http.StatusOK); err != nil {
		return err
	}

	if err := w.CreateStubsForGettingAMSSubscription(http.StatusOK, ocm.SubscriptionStatusReserved); err != nil {
		return err
	}

	if err := w.CreateStubsForUpdatingAMSSubscription(http.StatusOK, SubscriptionUpdateDisplayName); err != nil {
		return err
	}

	if err := w.CreateStubsForUpdatingAMSSubscription(http.StatusOK, SubscriptionUpdateConsoleUrl); err != nil {
		return err
	}

	if err := w.CreateStubsForUpdatingAMSSubscription(http.StatusOK, SubscriptionUpdateOpenshiftClusterID); err != nil {
		return err
	}

	if err := w.CreateStubsForUpdatingAMSSubscription(http.StatusOK, SubscriptionUpdateStatusActive); err != nil {
		return err
	}

	if err := w.CreateStubsForDeletingAMSSubscription(http.StatusOK); err != nil {
		return err
	}

	if _, err := w.CreateOpenshiftUpdateServiceStubs(); err != nil {
		return err
	}

	return nil
}

func (w *WireMock) CreateStubsForClusterEditor() error {
	if _, err := w.CreateStubClusterEditorRequest(FakePayloadUsername,
		FakeSubscriptionID.String(), "update", false); err != nil {
		return err
	}
	if _, err := w.CreateStubClusterEditorRequest(FakePayloadUsername,
		FakeSubscriptionID.String(), "delete", false); err != nil {
		return err
	}
	if _, err := w.CreateStubClusterEditorRequest(FakePayloadUsername2,
		FakeSubscriptionID.String(), "update", false); err != nil {
		return err
	}
	if _, err := w.CreateStubClusterEditorRequest(FakePayloadUsername2,
		FakeSubscriptionID.String(), "delete", false); err != nil {
		return err
	}
	if _, err := w.CreateStubClusterEditorRequest(FakePayloadClusterEditor,
		FakeSubscriptionID.String(), "update", true); err != nil {
		return err
	}
	if _, err := w.CreateStubClusterEditorRequest(FakePayloadClusterEditor,
		FakeSubscriptionID.String(), "delete", true); err != nil {
		return err
	}
	return nil
}

func (w *WireMock) CreateStubsForAccessReview() error {
	if _, err := w.CreateStubAccessReview(FakePayloadUsername, true); err != nil {
		return err
	}
	if _, err := w.CreateStubAccessReview(FakePayloadUsername2, true); err != nil {
		return err
	}
	if _, err := w.CreateStubAccessReview(FakePayloadClusterEditor, true); err != nil {
		return err
	}
	return nil
}

func (w *WireMock) CreateStubsForCapabilityReview() error {
	if _, err := w.CreateStubBareMetalCapabilityReview(FakePayloadUsername, false); err != nil {
		return err
	}
	if _, err := w.CreateStubBareMetalCapabilityReview(FakePayloadUsername2, false); err != nil {
		return err
	}
	if _, err := w.CreateStubBareMetalCapabilityReview(FakePayloadClusterEditor, false); err != nil {
		return err
	}
	if _, err := w.CreateStubMultiarchCapabilityReview(FakePayloadUsername, OrgId1, false); err != nil {
		return err
	}
	if _, err := w.CreateStubMultiarchCapabilityReview(FakePayloadUsername2, OrgId2, true); err != nil {
		return err
	}
	if _, err := w.CreateStubIgnoreValidationsCapabilityReview(FakePayloadUsername, OrgId1, false); err != nil {
		return err
	}
	if _, err := w.CreateStubIgnoreValidationsCapabilityReview(FakePayloadUsername2, OrgId2, true); err != nil {
		return err
	}
	if _, err := w.CreateStubAccountsMgmt(FakePayloadUsername, OrgId1); err != nil {
		return err
	}
	if _, err := w.CreateStubAccountsMgmt(FakePayloadUsername2, OrgId2); err != nil {
		return err
	}
	return nil
}

func (w *WireMock) CreateStubsForCreatingAMSSubscription(resStatus int) error {

	type reservedResource struct{}

	type clusterAuthorizationRequest struct {
		AccountUsername string              `json:"account_username"`
		ProductCategory string              `json:"product_category"`
		ProductID       string              `json:"product_id"`
		ClusterID       string              `json:"cluster_id"`
		Managed         bool                `json:"managed"`
		Resources       []*reservedResource `json:"resources"`
		Reserve         bool                `json:"reserve"`
		DisplayName     string              `json:"display_name"`
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
		DisplayName:     "${json-unit.any-string}",
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

	amsSubscriptionStub := w.CreateStubDefinition(ClusterAuthzPath, "POST", string(reqBody), string(resBody), resStatus)
	_, err = w.AddStub(amsSubscriptionStub)
	return err
}

func (w *WireMock) CreateStubsForGettingAMSSubscription(resStatus int, status string) error {

	subResponse := subscription{
		ID:     FakeSubscriptionID,
		Status: status,
	}

	var resBody []byte
	resBody, err := json.Marshal(subResponse)
	if err != nil {
		return err
	}

	amsSubscriptionStub := w.CreateStubDefinition(subscriptionPath, "GET", "", string(resBody), resStatus)
	_, err = w.AddStub(amsSubscriptionStub)
	return err
}

func (w *WireMock) CreateStubsForUpdatingAMSSubscription(resStatus int, updateType string) error {

	switch updateType {

	case SubscriptionUpdateDisplayName:

		type subscriptionUpdateRequest struct {
			DisplayName string `json:"display_name"`
		}

		subRequest := subscriptionUpdateRequest{
			DisplayName: "${json-unit.any-string}",
		}

		subResponse := subscription{
			ID: FakeSubscriptionID,
		}

		var reqBody []byte
		reqBody, err := json.Marshal(subRequest)
		if err != nil {
			return err
		}

		var resBody []byte
		resBody, err = json.Marshal(subResponse)
		if err != nil {
			return err
		}

		amsSubscriptionStub := w.CreateStubDefinition(subscriptionPath, "PATCH", string(reqBody), string(resBody), resStatus)
		_, err = w.AddStub(amsSubscriptionStub)
		return err

	case SubscriptionUpdateConsoleUrl:

		type subscriptionUpdateRequest struct {
			ConsoleUrl string `json:"console_url"`
		}

		subRequest := subscriptionUpdateRequest{
			ConsoleUrl: "${json-unit.any-string}",
		}

		subResponse := subscription{
			ID: FakeSubscriptionID,
		}

		var reqBody []byte
		reqBody, err := json.Marshal(subRequest)
		if err != nil {
			return err
		}

		var resBody []byte
		resBody, err = json.Marshal(subResponse)
		if err != nil {
			return err
		}

		amsSubscriptionStub := w.CreateStubDefinition(subscriptionPath, "PATCH", string(reqBody), string(resBody), resStatus)
		_, err = w.AddStub(amsSubscriptionStub)
		return err

	case SubscriptionUpdateOpenshiftClusterID:

		type subscriptionUpdateRequest struct {
			ExternalClusterID strfmt.UUID `json:"external_cluster_id"`
		}

		subRequest := subscriptionUpdateRequest{
			ExternalClusterID: "${json-unit.any-string}",
		}

		subResponse := subscription{
			ID: FakeSubscriptionID,
		}

		var reqBody []byte
		reqBody, err := json.Marshal(subRequest)
		if err != nil {
			return err
		}

		var resBody []byte
		resBody, err = json.Marshal(subResponse)
		if err != nil {
			return err
		}

		amsSubscriptionStub := w.CreateStubDefinition(subscriptionPath, "PATCH", string(reqBody), string(resBody), resStatus)
		_, err = w.AddStub(amsSubscriptionStub)
		return err

	case SubscriptionUpdateStatusActive:

		type subscriptionUpdateRequest struct {
			Status string `json:"status"`
		}

		subRequest := subscriptionUpdateRequest{
			Status: ocm.SubscriptionStatusActive,
		}

		subResponse := subscription{
			ID: FakeSubscriptionID,
		}

		var reqBody []byte
		reqBody, err := json.Marshal(subRequest)
		if err != nil {
			return err
		}

		var resBody []byte
		resBody, err = json.Marshal(subResponse)
		if err != nil {
			return err
		}

		amsSubscriptionStub := w.CreateStubDefinition(subscriptionPath, "PATCH", string(reqBody), string(resBody), resStatus)
		_, err = w.AddStub(amsSubscriptionStub)
		return err

	default:

		return errors.New("Invalid updateType arg")
	}
}

func (w *WireMock) CreateStubsForDeletingAMSSubscription(resStatus int) error {

	amsSubscriptionStub := w.CreateStubDefinition(subscriptionPath, "DELETE", "", "", resStatus)
	_, err := w.AddStub(amsSubscriptionStub)
	return err
}

func (w *WireMock) CreateStubToken(testToken string) (string, error) {
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
			URL:    TokenPath,
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

	return w.AddStub(tokenStub)
}

func (w *WireMock) CreateStubBareMetalCapabilityReview(username string, result bool) (string, error) {
	type CapabilityRequest struct {
		Name     string `json:"capability"`
		Type     string `json:"type"`
		Username string `json:"account_username"`
	}

	type CapabilityResponse struct {
		Result string `json:"result"`
	}

	capabilityRequest := CapabilityRequest{
		Name:     ocm.BareMetalCapabilityName,
		Type:     ocm.AccountCapabilityType,
		Username: username,
	}

	capabilityResponse := CapabilityResponse{
		Result: strconv.FormatBool(result),
	}

	return w.AddCapabilityReviewStub(capabilityRequest, capabilityResponse)
}

func (w *WireMock) CreateStubMultiarchCapabilityReview(username string, orgId string, result bool) (string, error) {
	type CapabilityRequest struct {
		Name     string `json:"capability"`
		Type     string `json:"type"`
		Username string `json:"account_username"`
		Org      string `json:"organization_id"`
	}

	type CapabilityResponse struct {
		Result string `json:"result"`
	}

	capabilityRequest := CapabilityRequest{
		Name:     ocm.MultiarchCapabilityName,
		Type:     ocm.OrganizationCapabilityType,
		Username: username,
		Org:      orgId,
	}

	capabilityResponse := CapabilityResponse{
		Result: strconv.FormatBool(result),
	}
	return w.AddCapabilityReviewStub(capabilityRequest, capabilityResponse)
}

func (w *WireMock) CreateStubIgnoreValidationsCapabilityReview(username string, orgId string, result bool) (string, error) {
	type CapabilityRequest struct {
		Name     string `json:"capability"`
		Type     string `json:"type"`
		Username string `json:"account_username"`
		Org      string `json:"organization_id"`
	}

	type CapabilityResponse struct {
		Result string `json:"result"`
	}

	capabilityRequest := CapabilityRequest{
		Name:     ocm.IgnoreValidationsCapabilityName,
		Type:     ocm.OrganizationCapabilityType,
		Username: username,
		Org:      orgId,
	}

	capabilityResponse := CapabilityResponse{
		Result: strconv.FormatBool(result),
	}
	return w.AddCapabilityReviewStub(capabilityRequest, capabilityResponse)
}

func (w *WireMock) AddCapabilityReviewStub(capabilityRequest interface{}, capabilityResponse interface{}) (string, error) {
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

	capabilityReviewStub := w.CreateStubDefinition(CapabilityReviewPath, "POST", string(reqBody), string(resBody), 200)
	return w.AddStub(capabilityReviewStub)
}

func (w *WireMock) CreateStubAccountsMgmt(username string, orgId string) (string, error) {
	type Organization struct {
		ID string `json:"id"`
	}

	type Account struct {
		Username     string       `json:"username"`
		Email        string       `json:"email"`
		Organization Organization `json:"organization"`
	}

	type AccountsListResponse struct {
		Items []*Account `json:"items"`
	}

	account := Account{
		Email:    username,
		Username: username,
		Organization: Organization{
			ID: orgId,
		},
	}

	res := AccountsListResponse{
		Items: []*Account{
			&account,
		},
	}

	var resBody []byte
	resBody, err := json.Marshal(res)
	if err != nil {
		return "", err
	}

	accountsMgmtSearchPath := strings.Join([]string{AccountsMgmtSearchPrefix, url.QueryEscape(fmt.Sprintf("='%s'", username))}, "")
	accountsMgmtSearchStub := w.CreateStubDefinition(accountsMgmtSearchPath,
		"GET", "", string(resBody), 200)

	return w.AddStub(accountsMgmtSearchStub)
}

func (w *WireMock) CreateStubClusterEditorRequest(username string, subscriptionId string, action string, allowed bool) (string, error) {
	type AccessRequest struct {
		ResourceType   string `json:"resource_type"`
		Action         string `json:"action"`
		Username       string `json:"account_username"`
		SubscriptionId string `json:"subscription_id"`
	}

	type AccessResponse struct {
		Allowed bool `json:"allowed"`
	}

	accessRequest := AccessRequest{
		Username:       username,
		Action:         action,
		ResourceType:   "Subscription",
		SubscriptionId: subscriptionId,
	}

	accessResponse := AccessResponse{
		Allowed: allowed,
	}

	var reqBody []byte
	reqBody, err := json.Marshal(accessRequest)
	if err != nil {
		return "", err
	}

	var resBody []byte
	resBody, err = json.Marshal(accessResponse)
	if err != nil {
		return "", err
	}

	accessReviewStub := w.CreateStubDefinition(AccessReviewPath, "POST", string(reqBody), string(resBody), 200)
	return w.AddStub(accessReviewStub)
}

func (w *WireMock) CreateStubAccessReview(username string, allowed bool) (string, error) {
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

	capabilityReviewStub := w.CreateStubDefinition(AccessReviewPath, "POST", string(reqBody), string(resBody), 200)
	return w.AddStub(capabilityReviewStub)
}

func (w *WireMock) CreateStubTokenAuth(token, username string) (string, error) {
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

	tokenAuthStub := w.CreateStubDefinition(PullAuthPath, "POST", string(reqBody), string(resBody), 200)
	return w.AddStub(tokenAuthStub)
}

func (w *WireMock) CreateWrongStubTokenAuth(token string) (string, error) {
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

	tokenAuthStub := w.CreateStubDefinition(PullAuthPath, "POST", string(reqBody), string(resBody), 404)
	return w.AddStub(tokenAuthStub)
}

func (w *WireMock) CreateOpenshiftUpdateServiceStubs() (string, error) {
	// OCP releases API needs amd64, arm64 instead of x86_64, aarch64 respectively
	cpuArchMapToAPIArch := map[string]string{
		common.X86CPUArchitecture:     common.AMD64CPUArchitecture,
		common.AARCH64CPUArchitecture: common.ARM64CPUArchitecture,
	}

	for _, releaseSource := range w.ReleaseSources {
		openshiftVersion := *releaseSource.OpenshiftVersion
		for _, upgradeChannel := range releaseSource.UpgradeChannels {
			cpuArchitecture := *upgradeChannel.CPUArchitecture
			for _, channel := range upgradeChannel.Channels {
				apiCpuArchitecture, shouldSwitch := cpuArchMapToAPIArch[cpuArchitecture]
				if shouldSwitch {
					cpuArchitecture = apiCpuArchitecture
				}

				u := url.URL{
					Path: releasesources.OpenshiftUpdateServiceAPIURLPath,
				}

				q := url.Values{}
				q.Add(releasesources.OpenshiftUpdateServiceAPIURLQueryChannel, fmt.Sprintf("%s-%s", channel, openshiftVersion))
				q.Add(releasesources.OpenshiftUpdateServiceAPIURLQueryArch, cpuArchitecture)
				u.RawQuery = q.Encode()
				endpoint := "/" + u.String()

				responseStruct := releasesources.ReleaseGraph{
					Nodes: []releasesources.Node{
						{
							Version: fmt.Sprintf("%s.0", openshiftVersion),
						},
					},
				}

				var resBody []byte
				resBody, err := json.Marshal(responseStruct)
				if err != nil {
					return "", err
				}

				newStub := w.CreateStubDefinition(endpoint, "GET", "", string(resBody), 200)
				_, err = w.AddStub(newStub)
				if err != nil {
					return "", err
				}
			}
		}
	}

	return "", nil
}

func (w *WireMock) CreateStubDefinition(url, method, reqBody, resBody string, resStatus int) *StubDefinition {
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

func (w *WireMock) AddStub(stub *StubDefinition) (string, error) {
	requestBody, err := json.Marshal(stub)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	b.Write(requestBody)

	resp, err := http.Post("http://"+w.OCMHost+WiremockMappingsPath, "application/json", &b)
	if err != nil {
		return "", err
	}
	responseBody, err := io.ReadAll(resp.Body)
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
	req, err := http.NewRequest("DELETE", "http://"+w.OCMHost+WiremockMappingsPath, nil)
	if err != nil {
		return err
	}
	client := &http.Client{}
	_, err = client.Do(req)
	return err
}

func (w *WireMock) DeleteStub(stubID string) error {
	req, err := http.NewRequest("DELETE", "http://"+w.OCMHost+WiremockMappingsPath+"/"+stubID, nil)
	if err != nil {
		return err
	}
	client := &http.Client{}
	_, err = client.Do(req)
	return err
}
