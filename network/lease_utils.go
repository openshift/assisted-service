package network

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/pkg/errors"
)

func VerifyLease(lease string) error {
	matched, err := regexp.MatchString(`^(?:|\s*lease\s*[{](?:\s+[a-z-]+ [^;}]*;)*\s+[}]\s*)$`, lease)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, errors.Wrap(err, "Lease verification"))
	}
	if !matched {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Lease %s was not matched", lease))
	}
	return nil
}

func FormatLease(lease string) string {
	c := regexp.MustCompile(`(\s)(renew|rebind|expire) [^;]*;`)
	return c.ReplaceAllString(lease, "${1}${2} never;")
}

func getEncoded(input string) string {
	if input == "" {
		return ""
	}
	return fmt.Sprintf("data:,%s", url.PathEscape(input))

}

func GetEncodedApiVipLease(c *common.Cluster) string {
	return getEncoded(c.ApiVipLease)
}

func GetEncodedIngressVipLease(c *common.Cluster) string {
	return getEncoded(c.IngressVipLease)
}
