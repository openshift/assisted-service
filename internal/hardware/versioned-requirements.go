package hardware

import (
	"encoding/json"

	"github.com/openshift/assisted-service/models"
)

type VersionedRequirementsDecoder map[string]models.VersionedHostRequirements

func (d *VersionedRequirementsDecoder) Decode(value string) error {
	var requirements []models.VersionedHostRequirements
	err := json.Unmarshal([]byte(value), &requirements)
	if err != nil {
		return err
	}

	versionToRequirements := make(VersionedRequirementsDecoder)
	for _, rq := range requirements {
		versionToRequirements[rq.Version] = rq
	}
	*d = versionToRequirements
	return nil
}
