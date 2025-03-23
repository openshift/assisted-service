package fenceagentsremediation

import (
	"github.com/openshift/assisted-service/internal/common"
	operatorsCommon "github.com/openshift/assisted-service/internal/operators/common"
)

// GenerateManifests generates manifests for the operator.
func (o *operator) GenerateManifests(_ *common.Cluster) (openshiftManifests map[string][]byte, customManifests []byte, err error) {
	return operatorsCommon.GenerateManifests(
		templatesRoot, o.templates, nil, &Operator,
	)
}
