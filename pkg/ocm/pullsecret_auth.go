package ocm

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	amgmtv1 "github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
)

const (
	sendRequestError = "can't send request"
)

//go:generate mockgen -source=pullsecret_auth.go -package=ocm -destination=mock_pullsecret_auth.go
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

	connection, err := a.client.NewConnection()
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError,
			errors.Wrap(err, "Unable to build OCM connection"))
	}
	defer connection.Close()

	accessTokenAPI := connection.AccountsMgmt().V1()
	request, err := amgmtv1.NewTokenAuthorizationRequest().AuthorizationToken(pullSecret).Build()
	if err != nil {
		return nil, common.NewApiError(http.StatusInternalServerError, err)
	}

	response, err := accessTokenAPI.TokenAuthorization().Post().Request(request).Send()
	if err != nil {
		// failed to send request
		if strings.Contains(err.Error(), sendRequestError) {
			return nil, common.NewApiError(http.StatusServiceUnavailable, err)
		}
		return nil, err
	}

	responseVal, ok := response.GetResponse()
	if !ok {
		return nil, fmt.Errorf("Failed to validate Pull Secret Token")
	}

	payload := &AuthPayload{}
	payload.Username = responseVal.Account().Username()
	payload.FirstName = responseVal.Account().FirstName()
	payload.LastName = responseVal.Account().LastName()
	payload.Email = responseVal.Account().Email()
	a.client.Cache.Set(pullSecret, payload, cache.DefaultExpiration)

	return payload, nil
}
