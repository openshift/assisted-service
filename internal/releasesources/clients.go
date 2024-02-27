package releasesources

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
)

// ocpVersionSupportLevels maps between major.minor OCP version to its corresponding suppurt level
type ocpVersionSupportLevels map[string]string

type ocpMajorVersionSet map[string]bool

// openShiftReleasesAPIClientInterface is an interface for a client,
// whose sole purpose is to fetch release images from the OCM upgrades info API (OpenShift Updates Service or OSUS),
// according to CPU architecture and release channel, e.g. amd64, stable-4.14
//
//go:generate mockgen -source=clients.go -destination=mock_clients.go -package=releasesources
type openShiftReleasesAPIClientInterface interface {
	getReleases(channel models.ReleaseChannel, openshiftVersion, cpuArchitecture string) (*ReleaseGraph, error)
}

// openShiftSupportLevelAPIClientInterface is an interface for a client,
// whose sole purpose is to fetch support levels from Red Hat Product Life Cycle Data API,
// according to a major OCP version, e.g. 4
//
//go:generate mockgen -source=clients.go -destination=mock_clients.go -package=releasesources
type openShiftSupportLevelAPIClientInterface interface {
	getSupportLevels(majorVersion string) (ocpVersionSupportLevels, error)
}

type ReleaseGraph struct {
	Nodes []Node `json:"nodes"`
}

type Node struct {
	Version string `json:"version"`
}

type supportLevelGraph struct {
	Data []data `json:"data"`
}

type data struct {
	Versions []version `json:"versions"`
}

type version struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

const (
	OpenshiftUpdateServiceAPIURLPath                    string = "api/upgrades_info/v1/graph"
	OpenshiftUpdateServiceAPIURLQueryChannel            string = "channel"
	OpenshiftUpdateServiceAPIURLQueryArch               string = "arch"
	redHatProductLifeCycleDataAPIQueryName              string = "name"
	redHatProductLifeCycleDataAPIQueryNameValueTemplate string = "Openshift Container Platform %s"
	redHatProductLifeCycleDataAPIEndOfLife              string = "End of life"
	redHatProductLifeCycleDataAPIMaintenanceSupport     string = "Maintenance Support"
	redHatProductLifeCycleDataAPIFullSupport            string = "Full Support"
)

type openShiftReleasesAPIClient struct {
	baseURL url.URL
}

type openShiftSupportLevelAPIClient struct {
	baseURL url.URL
}

func appendURLQueryParams(
	u url.URL,
	queryMap map[string]string,
) string {
	q := url.Values{}
	for key, value := range queryMap {
		q.Add(key, value)
	}
	u.RawQuery = q.Encode()

	return u.String()
}

func requestAndDecode(rawUrl string, decodeInto any) error {
	response, err := http.Get(rawUrl)
	if err != nil {
		return errors.Wrapf(err, "an error occurred while making http request to %s", rawUrl)
	}

	err = json.NewDecoder(response.Body).Decode(&decodeInto)
	if err != nil {
		return errors.Wrapf(err, "an error occurred while decoding the response to a request made to %s", rawUrl)
	}

	return nil
}

func (o openShiftReleasesAPIClient) getReleases(
	channel models.ReleaseChannel, openshiftVersion, cpuArchitecture string,
) (*ReleaseGraph, error) {
	url := appendURLQueryParams(
		o.baseURL,
		map[string]string{
			OpenshiftUpdateServiceAPIURLQueryChannel: fmt.Sprintf("%s-%s", string(channel), openshiftVersion),
			OpenshiftUpdateServiceAPIURLQueryArch:    cpuArchitecture,
		},
	)

	var releaseGraphInstance ReleaseGraph
	err := requestAndDecode(url, &releaseGraphInstance)
	if err != nil {
		return nil, err
	}

	return &releaseGraphInstance, nil
}

func (o openShiftSupportLevelAPIClient) getSupportLevels(openshiftMajorVersion string) (ocpVersionSupportLevels, error) {
	url := appendURLQueryParams(
		o.baseURL,
		map[string]string{
			redHatProductLifeCycleDataAPIQueryName: fmt.Sprintf(
				redHatProductLifeCycleDataAPIQueryNameValueTemplate, openshiftMajorVersion,
			),
		},
	)

	var supportLevelGraphInstance supportLevelGraph
	err := requestAndDecode(url, &supportLevelGraphInstance)
	if err != nil {
		return nil, err
	}

	mapAPISupportLevelToOurSupportLevel := map[string]string{
		redHatProductLifeCycleDataAPIEndOfLife:          models.OpenshiftVersionSupportLevelEndOfLife,
		redHatProductLifeCycleDataAPIMaintenanceSupport: models.OpenshiftVersionSupportLevelMaintenance,
		redHatProductLifeCycleDataAPIFullSupport:        models.OpenshiftVersionSupportLevelProduction,
	}
	supportLevels := ocpVersionSupportLevels{}

	for _, Data := range supportLevelGraphInstance.Data {
		for _, version := range Data.Versions {
			if convertion, ok := mapAPISupportLevelToOurSupportLevel[version.Type]; ok {
				version.Type = convertion
			}
			supportLevels[version.Name] = version.Type
		}
	}

	return supportLevels, nil
}
