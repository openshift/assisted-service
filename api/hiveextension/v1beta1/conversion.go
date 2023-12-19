package v1beta1

import (
	"github.com/openshift/assisted-service/api/hiveextension/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this AgentClusterInstall to the Hub version (v1beta2).
func (src *AgentClusterInstall) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.AgentClusterInstall)
	dst.ObjectMeta = src.ObjectMeta
	return nil
}

// ConvertFrom converts from the Hub version (v1beta2) to this version.
func (dst *AgentClusterInstall) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.AgentClusterInstall)
	dst.ObjectMeta = src.ObjectMeta
	return nil
}
