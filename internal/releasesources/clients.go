package releasesources

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/pkg/errors"
)

//go:generate mockgen -source=clients.go -destination=mock_clients.go -package=releasesources
type OpenShiftReleasesAPIClientInterface interface {
	getEndpoint(channel, openshiftVersion, cpuArchitecture string) string
	GetReleases(channel, openshiftVersion, cpuArchitecture string) (*ReleaseGraph, error)
	parseResponse(response *http.Response) (*ReleaseGraph, error)
}

//go:generate mockgen -source=clients.go -destination=mock_clients.go -package=releasesources
type OpenShiftSupportLevelAPIClientInterface interface {
	getEndpoint(openshiftMajorVersion string) string
	GetSupportLevels(openshiftMajorVersion string) (*SupportLevelGraph, error)
	parseResponse(response *http.Response) (*SupportLevelGraph, error)
}

type ReleaseGraph struct {
	Nodes []Node `json:"nodes"`
}

type Node struct {
	Version string `json:"version"`
}

type SupportLevelGraph struct {
	Data []Data `json:"data"`
}

type Data struct {
	Versions []Version `json:"versions"`
}

type Version struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

const releasesAPIQueryTemplate string = "%s?channel=%s-%s&arch=%s"
const supportLevelAPIQueryTemplate string = "%s?name=Openshift+Container+Platform+%s"

type OpenShiftReleasesAPIClient struct {
	BaseUrl string
}

func (o OpenShiftReleasesAPIClient) getEndpoint(channel, openshiftVersion, cpuArchitecture string) string {
	return fmt.Sprintf(releasesAPIQueryTemplate, o.BaseUrl, channel, openshiftVersion, cpuArchitecture)
}

func (o OpenShiftReleasesAPIClient) GetReleases(channel, openshiftVersion, cpuArchitecture string) (*ReleaseGraph, error) {
	endpoint := o.getEndpoint(channel, openshiftVersion, cpuArchitecture)
	response, err := http.Get(endpoint)
	if err != nil {
		return nil, errors.Errorf("an error occurred while making http request to %s: %s", endpoint, err)
	}

	graph, err := o.parseResponse(response)
	if err != nil {
		return nil, errors.Errorf("an error occurred while decoding the response to a request made to %s: %s", endpoint, err)
	}

	return graph, nil
}

func (o OpenShiftReleasesAPIClient) parseResponse(response *http.Response) (*ReleaseGraph, error) {
	var graph ReleaseGraph
	err := json.NewDecoder(response.Body).Decode(&graph)
	if err != nil {
		return nil, err
	}

	return &graph, nil
}

type OpenShiftSupportLevelAPIClient struct {
	BaseUrl string
}

func (o OpenShiftSupportLevelAPIClient) getEndpoint(openshiftMajorVersion string) string {
	return fmt.Sprintf(supportLevelAPIQueryTemplate, o.BaseUrl, openshiftMajorVersion)
}

func (o OpenShiftSupportLevelAPIClient) GetSupportLevels(openshiftMajorVersion string) (*SupportLevelGraph, error) {
	endpoint := o.getEndpoint(openshiftMajorVersion)
	response, err := http.Get(endpoint)
	if err != nil {
		return nil, errors.Errorf("an error occurred while making http request to %s: %s", endpoint, err)
	}

	graph, err := o.parseResponse(response)
	if err != nil {
		return nil, errors.Errorf("an error occurred while decoding the response to a request made to %s: %s", endpoint, err)
	}

	return graph, nil
}

func (o OpenShiftSupportLevelAPIClient) parseResponse(response *http.Response) (*SupportLevelGraph, error) {
	var graph SupportLevelGraph
	err := json.NewDecoder(response.Body).Decode(&graph)
	if err != nil {
		return nil, err
	}

	return &graph, nil
}
