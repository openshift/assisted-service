package metallb

import (
	"github.com/openshift/assisted-service/internal/common"
	operatorsCommon "github.com/openshift/assisted-service/internal/operators/common"
)

func (o *operator) GenerateManifests(_ *common.Cluster) (map[string][]byte, []byte, error) {
	return operatorsCommon.GenerateManifests(
		templatesRoot, o.templates, nil, &Operator,
	)
}
