package v1beta1

import (
	"encoding/json"

	"github.com/openshift/assisted-service/api/hiveextension/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

// ConvertTo converts this AgentClusterInstall to the Hub version (v1beta2).
func (src *AgentClusterInstall) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta2.AgentClusterInstall)
	dst.ObjectMeta = src.ObjectMeta

	// direct copies for non-struct fields
	dst.Spec.ImageSetRef = src.Spec.ImageSetRef
	dst.Spec.ClusterDeploymentRef = src.Spec.ClusterDeploymentRef
	dst.Spec.ClusterMetadata = src.Spec.ClusterMetadata
	dst.Spec.ManifestsConfigMapRef = src.Spec.ManifestsConfigMapRef
	dst.Spec.SSHPublicKey = src.Spec.SSHPublicKey
	dst.Spec.PlatformType = v1beta2.PlatformType(string(src.Spec.PlatformType))
	dst.Spec.HoldInstallation = src.Spec.HoldInstallation
	dst.Spec.MastersSchedulable = src.Spec.MastersSchedulable

	// json marshal/unmarshal for identical struct types
	if l := len(src.Spec.ManifestsConfigMapRefs); l != 0 {
		tmp, err := json.Marshal(src.Spec.ManifestsConfigMapRefs)
		if err != nil {
			return err
		}
		dst.Spec.ManifestsConfigMapRefs = make([]v1beta2.ManifestsConfigMapReference, l)
		if err = json.Unmarshal(tmp, &dst.Spec.ManifestsConfigMapRefs); err != nil {
			return err
		}
	}

	tmp, err := json.Marshal(src.Spec.Networking)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(tmp, &dst.Spec.Networking); err != nil {
		return err
	}

	tmp, err = json.Marshal(src.Spec.ProvisionRequirements)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(tmp, &dst.Spec.ProvisionRequirements); err != nil {
		return err
	}

	if src.Spec.ControlPlane != nil {
		tmp, err = json.Marshal(*src.Spec.ControlPlane)
		if err != nil {
			return err
		}
		dst.Spec.ControlPlane = &v1beta2.AgentMachinePool{}
		if err = json.Unmarshal(tmp, dst.Spec.ControlPlane); err != nil {
			return err
		}
	}

	if l := len(src.Spec.Compute); l != 0 {
		tmp, err = json.Marshal(src.Spec.Compute)
		if err != nil {
			return err
		}
		dst.Spec.Compute = make([]v1beta2.AgentMachinePool, l)
		if err = json.Unmarshal(tmp, &dst.Spec.Compute); err != nil {
			return err
		}
	}

	if src.Spec.IgnitionEndpoint != nil {
		tmp, err = json.Marshal(*src.Spec.IgnitionEndpoint)
		if err != nil {
			return err
		}
		dst.Spec.IgnitionEndpoint = &v1beta2.IgnitionEndpoint{}
		if err = json.Unmarshal(tmp, dst.Spec.IgnitionEndpoint); err != nil {
			return err
		}
	}

	if src.Spec.DiskEncryption != nil {
		tmp, err = json.Marshal(*src.Spec.DiskEncryption)
		if err != nil {
			return err
		}
		dst.Spec.DiskEncryption = &v1beta2.DiskEncryption{}
		if err = json.Unmarshal(tmp, dst.Spec.DiskEncryption); err != nil {
			return err
		}
	}

	if src.Spec.Proxy != nil {
		tmp, err = json.Marshal(*src.Spec.Proxy)
		if err != nil {
			return err
		}
		dst.Spec.Proxy = &v1beta2.Proxy{}
		if err = json.Unmarshal(tmp, dst.Spec.Proxy); err != nil {
			return err
		}
	}

	if src.Spec.ExternalPlatformSpec != nil {
		tmp, err = json.Marshal(*src.Spec.ExternalPlatformSpec)
		if err != nil {
			return err
		}
		dst.Spec.ExternalPlatformSpec = &v1beta2.ExternalPlatformSpec{}
		if err = json.Unmarshal(tmp, dst.Spec.ExternalPlatformSpec); err != nil {
			return err
		}
	}

	if l := len(src.Spec.APIVIPs); l != 0 {
		dst.Spec.APIVIPs = make([]string, l)
		copy(dst.Spec.APIVIPs, src.Spec.APIVIPs)
	} else if src.Spec.APIVIP != "" {
		dst.Spec.APIVIPs = []string{src.Spec.APIVIP}
	}

	if l := len(src.Spec.IngressVIPs); l != 0 {
		dst.Spec.IngressVIPs = make([]string, l)
		copy(dst.Spec.IngressVIPs, src.Spec.IngressVIPs)
	} else if src.Spec.IngressVIP != "" {
		dst.Spec.IngressVIPs = []string{src.Spec.IngressVIP}
	}

	return nil
}

// ConvertFrom converts from the Hub version (v1beta2) to this version.
func (dst *AgentClusterInstall) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta2.AgentClusterInstall)
	dst.ObjectMeta = src.ObjectMeta

	// direct copies for non-struct fields
	dst.Spec.ImageSetRef = src.Spec.ImageSetRef
	dst.Spec.ClusterDeploymentRef = src.Spec.ClusterDeploymentRef
	dst.Spec.ClusterMetadata = src.Spec.ClusterMetadata
	dst.Spec.ManifestsConfigMapRef = src.Spec.ManifestsConfigMapRef
	dst.Spec.SSHPublicKey = src.Spec.SSHPublicKey
	dst.Spec.PlatformType = PlatformType(string(src.Spec.PlatformType))
	dst.Spec.HoldInstallation = src.Spec.HoldInstallation
	dst.Spec.MastersSchedulable = src.Spec.MastersSchedulable

	// json marshal/unmarshal for identical struct types
	if l := len(src.Spec.ManifestsConfigMapRefs); l != 0 {
		tmp, err := json.Marshal(src.Spec.ManifestsConfigMapRefs)
		if err != nil {
			return err
		}
		dst.Spec.ManifestsConfigMapRefs = make([]ManifestsConfigMapReference, l)
		if err = json.Unmarshal(tmp, &dst.Spec.ManifestsConfigMapRefs); err != nil {
			return err
		}
	}

	tmp, err := json.Marshal(src.Spec.Networking)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(tmp, &dst.Spec.Networking); err != nil {
		return err
	}

	tmp, err = json.Marshal(src.Spec.ProvisionRequirements)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(tmp, &dst.Spec.ProvisionRequirements); err != nil {
		return err
	}

	if src.Spec.ControlPlane != nil {
		tmp, err = json.Marshal(*src.Spec.ControlPlane)
		if err != nil {
			return err
		}
		dst.Spec.ControlPlane = &AgentMachinePool{}
		if err = json.Unmarshal(tmp, dst.Spec.ControlPlane); err != nil {
			return err
		}
	}

	if l := len(src.Spec.Compute); l != 0 {
		tmp, err = json.Marshal(src.Spec.Compute)
		if err != nil {
			return err
		}
		dst.Spec.Compute = make([]AgentMachinePool, l)
		if err = json.Unmarshal(tmp, &dst.Spec.Compute); err != nil {
			return err
		}
	}

	if src.Spec.IgnitionEndpoint != nil {
		tmp, err = json.Marshal(*src.Spec.IgnitionEndpoint)
		if err != nil {
			return err
		}
		dst.Spec.IgnitionEndpoint = &IgnitionEndpoint{}
		if err = json.Unmarshal(tmp, dst.Spec.IgnitionEndpoint); err != nil {
			return err
		}
	}

	if src.Spec.DiskEncryption != nil {
		tmp, err = json.Marshal(*src.Spec.DiskEncryption)
		if err != nil {
			return err
		}
		dst.Spec.DiskEncryption = &DiskEncryption{}
		if err = json.Unmarshal(tmp, dst.Spec.DiskEncryption); err != nil {
			return err
		}
	}

	if src.Spec.Proxy != nil {
		tmp, err = json.Marshal(*src.Spec.Proxy)
		if err != nil {
			return err
		}
		dst.Spec.Proxy = &Proxy{}
		if err = json.Unmarshal(tmp, dst.Spec.Proxy); err != nil {
			return err
		}
	}

	if src.Spec.ExternalPlatformSpec != nil {
		tmp, err = json.Marshal(*src.Spec.ExternalPlatformSpec)
		if err != nil {
			return err
		}
		dst.Spec.ExternalPlatformSpec = &ExternalPlatformSpec{}
		if err = json.Unmarshal(tmp, dst.Spec.ExternalPlatformSpec); err != nil {
			return err
		}
	}

	if l := len(src.Spec.APIVIPs); l != 0 {
		dst.Spec.APIVIPs = make([]string, l)
		copy(dst.Spec.APIVIPs, src.Spec.APIVIPs)
	}

	if l := len(src.Spec.IngressVIPs); l != 0 {
		dst.Spec.IngressVIPs = make([]string, l)
		copy(dst.Spec.IngressVIPs, src.Spec.IngressVIPs)
	}

	return nil
}
