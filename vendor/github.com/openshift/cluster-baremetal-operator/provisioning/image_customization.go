/*

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provisioning

import (
	"context"
	"fmt"
	"net"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
)

const (
	ironicBaseUrl                    = "IRONIC_BASE_URL"
	ironicAgentImage                 = "IRONIC_AGENT_IMAGE"
	imageCustomizationDeploymentName = "metal3-image-customization"
	imageCustomizationVolume         = "metal3-image-customization-volume"
	imageCustomizationPort           = 8084
	containerRegistriesConfPath      = "/etc/containers/registries.conf"
	containerRegistriesEnvVar        = "REGISTRIES_CONF_PATH"
	containerUserCaBundlePath        = "/etc/pki/ca-trust/source/anchors/openshift-config-user-ca-bundle.crt"
	containerCATrustDirPath          = "/etc/pki/ca-trust/"
	containerCATrustDirVolume        = "ca-trust"
	containerUserCaBundleEnvVar      = "CA_BUNDLE"
	imageSharedDir                   = "/shared/html/images"
	deployISOEnvVar                  = "DEPLOY_ISO"
	deployISOFile                    = imageSharedDir + "/ironic-python-agent.iso"
	deployInitrdEnvVar               = "DEPLOY_INITRD"
	deployInitrdFile                 = imageSharedDir + "/ironic-python-agent.initramfs"
	imageCustomizationConfigName     = "metal3-image-customization-config"
	ironicAgentPullSecret            = "ironic-agent-pull-secret" // #nosec G101
	ironicAgentPullSecretPath        = "/run/secrets/pull-secret" // #nosec G101
	additionalNTPServers             = "ADDITIONAL_NTP_SERVERS"
)

var (
	imageRegistriesVolumeMount = corev1.VolumeMount{
		Name:      imageCustomizationVolume,
		MountPath: containerRegistriesConfPath,
	}
	caTrustDirVolumeMount = corev1.VolumeMount{
		Name:      containerCATrustDirVolume,
		MountPath: containerCATrustDirPath,
	}
	ironicAgentPullSecretMount = corev1.VolumeMount{
		Name:      ironicAgentPullSecret,
		MountPath: ironicAgentPullSecretPath,
		SubPath:   PullSecretName,
		ReadOnly:  true,
	}
)

func imageRegistriesVolume() corev1.Volume {
	// TODO: Should this be corev1.HostPathFile?
	volType := corev1.HostPathFileOrCreate

	return corev1.Volume{
		Name: imageCustomizationVolume,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: containerRegistriesConfPath,
				Type: &volType,
			},
		},
	}
}

func ironicAgentPullSecretVolume() corev1.Volume {
	return corev1.Volume{
		Name: ironicAgentPullSecret,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: PullSecretName,
				Items: []corev1.KeyToPath{
					{Key: openshiftConfigSecretKey, Path: PullSecretName},
				},
			},
		},
	}
}

func caTrustDirVolume() corev1.Volume {
	volType := corev1.HostPathDirectory
	return corev1.Volume{
		Name: containerCATrustDirVolume,
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: containerCATrustDirPath,
				Type: &volType,
			},
		},
	}
}

func getUrlFromIP(ipAddrs []string, port int) string {
	portString := fmt.Sprint(port)
	var result []string
	for _, ipAddr := range ipAddrs {
		if ipAddr != "" {
			result = append(result,
				"https://"+net.JoinHostPort(ipAddr, portString))
		}
	}
	return strings.Join(result, ",")
}

func createImageCustomizationContainer(images *Images, info *ProvisioningInfo, ironicIPs []string) corev1.Container {
	noProxy := ironicIPs
	envVars := envWithProxy(info.Proxy, []corev1.EnvVar{}, noProxy)

	agentImage := images.IronicAgent
	if info.ProvConfig.Spec.UnsupportedConfigOverrides != nil && info.ProvConfig.Spec.UnsupportedConfigOverrides.IronicAgentImage != "" {
		agentImage = info.ProvConfig.Spec.UnsupportedConfigOverrides.IronicAgentImage
	}

	container := corev1.Container{
		Name:  "machine-image-customization-controller",
		Image: images.ImageCustomizationController,
		Command: []string{"/machine-image-customization-controller",
			"-images-bind-addr", fmt.Sprintf(":%d", imageCustomizationPort),
			"-images-publish-addr",
			fmt.Sprintf("http://%s.%s.svc.cluster.local/",
				imageCustomizationService, info.Namespace)},

		// TODO: This container does not have to run in privileged mode when the i-c-c has
		// its own volume and does not have to use the imageCacheSharedVolume
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(true),
			// Needed for hostPath image volume mount
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			imageRegistriesVolumeMount,
			imageVolumeMount,
			ironicAgentPullSecretMount,
			caTrustDirVolumeMount,
		},
		ImagePullPolicy: "IfNotPresent",
		Env: append(envVars, corev1.EnvVar{
			Name:  deployISOEnvVar,
			Value: deployISOFile,
		},
			corev1.EnvVar{
				Name:  deployInitrdEnvVar,
				Value: deployInitrdFile,
			},
			corev1.EnvVar{
				Name:  ironicBaseUrl,
				Value: getUrlFromIP(ironicIPs, baremetalIronicPort),
			},
			corev1.EnvVar{
				Name:  ironicAgentImage,
				Value: agentImage,
			},
			corev1.EnvVar{
				Name:  containerRegistriesEnvVar,
				Value: containerRegistriesConfPath,
			},
			corev1.EnvVar{
				Name:  ipOptions,
				Value: info.NetworkStack.IpOption(),
			},
			corev1.EnvVar{
				Name:  additionalNTPServers,
				Value: strings.Join(info.ProvConfig.Spec.AdditionalNTPServers, ","),
			},
			corev1.EnvVar{
				Name:  containerUserCaBundleEnvVar,
				Value: containerUserCaBundlePath,
			},
			buildSSHKeyEnvVar(info.SSHKey)),
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: imageCustomizationPort,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("5m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	return container
}

func newImageCustomizationPodTemplateSpec(info *ProvisioningInfo, labels *map[string]string, ironicIPs []string) *corev1.PodTemplateSpec {
	containers := []corev1.Container{
		createImageCustomizationContainer(info.Images, info, ironicIPs),
	}

	// Extract the pre-provisioning images from a container in the payload
	initContainers := []corev1.Container{
		// TODO(dtantsur): use --image-build instead of --all once ICC has its own isolated volume
		createInitContainerMachineOSImages(info, "--all", imageVolumeMount, imageSharedDir),
	}

	tolerations := []corev1.Toleration{
		{
			Key:      "node-role.kubernetes.io/master",
			Effect:   corev1.TaintEffectNoSchedule,
			Operator: corev1.TolerationOpExists,
		},
		{
			Key:      "CriticalAddonsOnly",
			Operator: corev1.TolerationOpExists,
		},
		{
			Key:               "node.kubernetes.io/not-ready",
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: ptr.To[int64](120),
		},
		{
			Key:               "node.kubernetes.io/unreachable",
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: ptr.To[int64](120),
		},
	}

	nodeSelector := map[string]string{}
	if !info.IsHyperShift {
		nodeSelector = map[string]string{"node-role.kubernetes.io/master": ""}
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: podTemplateAnnotations,
			Labels:      *labels,
		},
		Spec: corev1.PodSpec{
			Containers:         containers,
			InitContainers:     initContainers,
			HostNetwork:        false,
			DNSPolicy:          corev1.DNSClusterFirstWithHostNet,
			PriorityClassName:  "system-node-critical",
			NodeSelector:       nodeSelector,
			ServiceAccountName: "cluster-baremetal-operator",
			Tolerations:        tolerations,
			Volumes: []corev1.Volume{
				imageRegistriesVolume(),
				imageVolume(),
				ironicAgentPullSecretVolume(),
				caTrustDirVolume(),
			},
		},
	}
}

func newImageCustomizationDeployment(info *ProvisioningInfo, ironicIPs []string) *appsv1.Deployment {
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"k8s-app":    metal3AppName,
			cboLabelName: imageCustomizationService,
		},
	}
	podSpecLabels := map[string]string{
		"k8s-app":    metal3AppName,
		cboLabelName: imageCustomizationService,
	}
	template := newImageCustomizationPodTemplateSpec(info, &podSpecLabels, ironicIPs)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        imageCustomizationDeploymentName,
			Namespace:   info.Namespace,
			Annotations: map[string]string{},
			Labels: map[string]string{
				"k8s-app":    metal3AppName,
				cboLabelName: imageCustomizationService,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: selector,
			Template: *template,
		},
	}
}

func newImageCustomizationConfig(info *ProvisioningInfo, ironicIPs []string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        imageCustomizationConfigName,
			Namespace:   info.Namespace,
			Annotations: map[string]string{},
			Labels: map[string]string{
				"k8s-app":    metal3AppName,
				cboLabelName: imageCustomizationService,
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			ironicAgentImage: info.Images.IronicAgent,
			ironicBaseUrl:    getUrlFromIP(ironicIPs, baremetalIronicPort),
			sshKeyEnvVar:     info.SSHKey,
		},
	}
}

func EnsureImageCustomizationDeployment(info *ProvisioningInfo) (updated bool, err error) {
	ironicIPs, err := GetIronicIPs(info)
	if err != nil {
		return false, fmt.Errorf("unable to determine Ironic's IP to pass to the machine-image-customization-controller: %w", err)
	}

	configSecret := newImageCustomizationConfig(info, ironicIPs)
	err = controllerutil.SetControllerReference(info.ProvConfig, configSecret, info.Scheme)
	if err != nil {
		err = fmt.Errorf("unable to set controllerReference on machine-image-customization config: %w", err)
		return
	}
	_, _, err = resourceapply.ApplySecret(
		context.Background(), info.Client.CoreV1(), info.EventRecorder, configSecret)
	if err != nil {
		return false, fmt.Errorf("failed to create the image customization config: %w", err)
	}

	imageCustomizationDeployment := newImageCustomizationDeployment(info, ironicIPs)
	expectedGeneration := resourcemerge.ExpectedDeploymentGeneration(imageCustomizationDeployment, info.ProvConfig.Status.Generations)
	err = controllerutil.SetControllerReference(info.ProvConfig, imageCustomizationDeployment, info.Scheme)
	if err != nil {
		err = fmt.Errorf("unable to set controllerReference on machine-image-customization deployment: %w", err)
		return
	}
	deployment, updated, err := resourceapply.ApplyDeployment(context.Background(),
		info.Client.AppsV1(), info.EventRecorder, imageCustomizationDeployment, expectedGeneration)
	if err != nil {
		return updated, err
	}
	if updated {
		resourcemerge.SetDeploymentGeneration(&info.ProvConfig.Status.Generations, deployment)
	}
	return updated, nil
}

func DeleteImageCustomizationDeployment(info *ProvisioningInfo) error {
	return client.IgnoreNotFound(info.Client.AppsV1().Deployments(info.Namespace).Delete(context.Background(), imageCustomizationDeploymentName, metav1.DeleteOptions{}))
}
