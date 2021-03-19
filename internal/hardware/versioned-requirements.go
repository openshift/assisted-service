package hardware

import "encoding/json"

type Requirements struct {
	// Required number of CPU cores
	CPUCores int64 `json:"cpu_cores,omitempty"`

	// Required disk size in GB
	DiskSizeGb int64 `json:"disk_size_gb,omitempty"`

	// Required number of RAM in GiB
	RAMGib int64 `json:"ram_gib,omitempty"`
}

type VersionedRequirements struct {
	// OCP Version
	Version string `json:"version"`

	// Master node requirements
	MasterRequirements *Requirements `json:"master,omitempty"`

	// Worker node requirements
	WorkerRequirements *Requirements `json:"worker,omitempty"`
}

type VersionedRequirementsDecoder map[string]VersionedRequirements

func (d *VersionedRequirementsDecoder) Decode(value string) error {
	var requirements []VersionedRequirements
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
