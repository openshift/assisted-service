package domains

import (
	"context"
	"regexp"

	"github.com/go-openapi/runtime/middleware"
	"github.com/openshift/assisted-service/internal/common"
	models "github.com/openshift/assisted-service/models/v1"
	restapi "github.com/openshift/assisted-service/restapi/restapi_v1"
	operations "github.com/openshift/assisted-service/restapi/restapi_v1/operations/managed_domains"
	"github.com/pkg/errors"
)

// NewHandler returns managed domains handler
func NewHandler(baseDNSDomains map[string]string) *Handler {
	return &Handler{baseDNSDomains: baseDNSDomains}
}

var _ restapi.ManagedDomainsAPI = (*Handler)(nil)

// Handler represents managed domains handler
type Handler struct {
	baseDNSDomains map[string]string
}

func (h *Handler) parseDomainProvider(val string) (string, error) {
	re := regexp.MustCompile("/")
	if !re.MatchString(val) {
		return "", errors.Errorf("Invalid format: %s", val)
	}
	s := re.Split(val, 2)
	return s[1], nil
}

func (h *Handler) ListManagedDomains(ctx context.Context, params operations.ListManagedDomainsParams) middleware.Responder {
	managedDomains := models.ListManagedDomains{}
	for k, v := range h.baseDNSDomains {
		provider, err := h.parseDomainProvider(v)
		if err != nil {
			return operations.NewListManagedDomainsInternalServerError().
				WithPayload(common.GenerateInternalFromError(err))
		}
		managedDomains = append(managedDomains, &models.ManagedDomain{
			Domain:   k,
			Provider: provider,
		})
	}
	return operations.NewListManagedDomainsOK().WithPayload(managedDomains)
}
