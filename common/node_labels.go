package common

import (
	"encoding/json"

	"github.com/openshift/assisted-service/models"
)

func MarshalNodeLabels(nodeLabelsList []*models.NodeLabelParams) (string, error) {
	nodeLabelsMap := make(map[string]string)
	for _, nl := range nodeLabelsList {
		nodeLabelsMap[*nl.Key] = *nl.Value
	}

	nodeLabelsJson, err := json.Marshal(&nodeLabelsMap)
	if err != nil {
		return "", err
	}

	nodeLabelsStr := string(nodeLabelsJson)
	return nodeLabelsStr, nil
}
