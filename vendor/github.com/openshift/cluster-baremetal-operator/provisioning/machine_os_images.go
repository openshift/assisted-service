package provisioning

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
)

func createInitContainerMachineOSImages(info *ProvisioningInfo, whichImages string, dest corev1.VolumeMount, destPath string) corev1.Container {
	ipOptionValue := info.NetworkStack.IpOption()
	if !info.ProvConfig.Spec.VirtualMediaViaExternalNetwork {
		ipOptionValue = IpOptionForProvisioning(&info.ProvConfig.Spec, info.NetworkStack)
	}

	container := corev1.Container{
		Name:    "machine-os-images",
		Image:   info.Images.MachineOSImages,
		Command: []string{"/bin/copy-metal", whichImages, destPath},
		VolumeMounts: []corev1.VolumeMount{
			dest,
			ironicAgentPullSecretMount,
			caTrustDirVolumeMount,
		},
		ImagePullPolicy: "IfNotPresent",
		Env: []corev1.EnvVar{
			{
				Name:  ipOptions,
				Value: ipOptionValue,
			},
			{
				Name:  "MACHINE_OS_IMAGES_IMAGE",
				Value: info.Images.MachineOSImages,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("5m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
		},
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(true),
			// Needed for hostPath image volume mount
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	return container
}
