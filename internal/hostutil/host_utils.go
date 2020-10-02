package hostutil

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/openshift/assisted-service/internal/common"

	"github.com/pkg/errors"

	"github.com/openshift/assisted-service/models"
)

const (
	MaxHostnameLength = 253
)

func GetCurrentHostName(host *models.Host) (string, error) {
	var inventory models.Inventory
	if host.RequestedHostname != "" {
		return host.RequestedHostname, nil
	}
	err := json.Unmarshal([]byte(host.Inventory), &inventory)
	if err != nil {
		return "", err
	}
	return inventory.Hostname, nil
}

func GetHostnameForMsg(host *models.Host) string {
	hostName, err := GetCurrentHostName(host)
	// An error here probably indicates that the agent didn't send inventory yet, fall back to UUID
	if err != nil || hostName == "" {
		return host.ID.String()
	}
	return hostName
}

func GetEventSeverityFromHostStatus(status string) string {
	switch status {
	case models.HostStatusDisconnected:
		return models.EventSeverityWarning
	case models.HostStatusInstallingPendingUserAction:
		return models.EventSeverityWarning
	case models.HostStatusInsufficient:
		return models.EventSeverityWarning
	case models.HostStatusError:
		return models.EventSeverityError
	default:
		return models.EventSeverityInfo
	}
}

func ValidateHostname(hostname string) error {
	if len(hostname) > MaxHostnameLength {
		return common.NewApiError(http.StatusBadRequest, errors.New("hostname is too long"))
	}
	pattern := "^[a-z0-9][a-z0-9-]{0,62}(?:[.][a-z0-9-]{1,63})*$"
	b, err := regexp.MatchString(pattern, hostname)
	if err != nil {
		return common.NewApiError(http.StatusInternalServerError, errors.Wrapf(err, "Matching hostname"))
	}
	if !b {
		return common.NewApiError(http.StatusBadRequest, errors.Errorf("Hostname does not pass required regex validation: %s. Hostname: %s", pattern, hostname))
	}
	return nil
}

func IgnitionFileName(host *models.Host) string {
	return fmt.Sprintf("%s-%s.ign", host.Role, host.ID)
}
