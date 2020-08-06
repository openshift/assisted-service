package ocm

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/patrickmn/go-cache"
	"github.com/sirupsen/logrus"
)

type OCMAuthentication interface {
	AuthenticatePullSecret(ctx context.Context, pullSecret string) (user *AuthPayload, err error)
}

type authentication struct {
	client *Client
}

func (a authentication) AuthenticatePullSecret(ctx context.Context, pullSecret string) (user *AuthPayload, err error) {

	authUser, found := a.client.Cache.Get(pullSecret)
	if found {
		return authUser.(*AuthPayload), nil
	}
	con := a.client.connection
	request := con.Post()
	request.Path("/api/accounts_mgmt/v1/token_authorization")

	type TokenAuthorizationRequest struct {
		AuthorizationToken string `json:"authorization_token"`
	}

	tokenAuthorizationRequest := TokenAuthorizationRequest{
		AuthorizationToken: pullSecret,
	}

	var jsonData []byte
	jsonData, err = json.Marshal(tokenAuthorizationRequest)
	if err != nil {
		return nil, err
	}
	request.Bytes(jsonData)

	postResp, err := request.SendContext(ctx)
	if err != nil {
		return nil, err
	}

	if postResp.Status() != 200 {
		return nil, fmt.Errorf("Failed to validate Pull Secret Token")
	}

	type TokenAuthorizationResponse struct {
		Account struct {
			ID           string `json:"id"`
			Kind         string `json:"kind"`
			Href         string `json:"href"`
			FirstName    string `json:"first_name"`
			LastName     string `json:"last_name"`
			Username     string `json:"username"`
			Email        string `json:"email"`
			Organization struct {
				ID         string `json:"id"`
				Kind       string `json:"kind"`
				Href       string `json:"href"`
				Name       string `json:"name"`
				ExternalID string `json:"external_id"`
			} `json:"organization"`
		} `json:"account"`
	}

	var tokenAuthorizationResponse TokenAuthorizationResponse
	if err := json.Unmarshal(postResp.Bytes(), &tokenAuthorizationResponse); err != nil {
		return nil, err
	}
	logrus.Error(tokenAuthorizationResponse.Account.Username)
	payload := &AuthPayload{}
	payload.Username = tokenAuthorizationResponse.Account.Username
	payload.FirstName = tokenAuthorizationResponse.Account.FirstName
	payload.LastName = tokenAuthorizationResponse.Account.LastName
	payload.Email = tokenAuthorizationResponse.Account.Email
	a.client.Cache.Set(pullSecret, payload, cache.DefaultExpiration)
	return payload, nil
}
