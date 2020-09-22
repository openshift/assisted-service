package common

import "encoding/json"

type OpenshiftVersion struct {
	Version      string
	RhcosImage   string `json:"rhcos,omitempty"`
	McoImage     string `json:"mco,omitempty"`
	ReleaseImage string `json:"release,omitempty"`
}

func CreateOpenshiftVersionMapFromString(openshiftVersionStr string) (map[string]OpenshiftVersion, error) {
	jsonMap := make(map[string]OpenshiftVersion)
	err := json.Unmarshal([]byte(openshiftVersionStr), &jsonMap)
	if err != nil {
		return nil, err
	}
	openshiftVersions := make([]string, len(jsonMap))
	i := 0
	for k := range jsonMap {
		openshiftVersions[i] = k
		i++
	}
	return jsonMap, nil
}

func GetOpenshiftVersionsListFromMap(openshiftVersions map[string]OpenshiftVersion) []string {
	openshiftVersionsList := make([]string, len(openshiftVersions))
	i := 0
	for k := range openshiftVersions {
		openshiftVersionsList[i] = k
		i++
	}
	return openshiftVersionsList
}
