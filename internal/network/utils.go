package network

import (
	"net/http"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/sirupsen/logrus"

	"encoding/json"

	"github.com/openshift/assisted-service/models"
)

func IsIpv6OnlyHost(host *models.Host, log logrus.FieldLogger) (bool, error) {
	if host.Inventory == "" {
		return false, nil
	}
	var inventory models.Inventory
	if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
		log.WithError(err).Warn("Can't unmarshal inventory")
		return false, common.NewApiError(http.StatusBadRequest, err)
	}
	var hasIpv4, hasIpv6 bool
	for _, intf := range inventory.Interfaces {
		hasIpv4 = hasIpv4 || len(intf.IPV4Addresses) > 0
		hasIpv6 = hasIpv6 || len(intf.IPV6Addresses) > 0
	}
	return hasIpv6 && !hasIpv4, nil
}

func AreIpv6OnlyHosts(hosts []*models.Host, log logrus.FieldLogger) (bool, error) {
	if len(hosts) == 0 {
		return false, nil
	}
	for _, h := range hosts {
		ipv6Only, err := IsIpv6OnlyHost(h, log)
		if err != nil {
			return false, err
		}
		if !ipv6Only {
			return false, nil
		}
	}
	return true, nil
}
