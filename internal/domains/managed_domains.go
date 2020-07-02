package domains

import (
	"context"
	"regexp"

	"github.com/filanov/bm-inventory/internal/common"
	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/restapi"
	operations "github.com/filanov/bm-inventory/restapi/operations/managed_domains"
	"github.com/go-openapi/runtime/middleware"
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
