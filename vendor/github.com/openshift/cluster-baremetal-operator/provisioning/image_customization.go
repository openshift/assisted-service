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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
)

const (
	ironicBaseUrl                    = "IRONIC_BASE_URL"
	ironicInspectorBaseUrl           = "IRONIC_INSPECTOR_BASE_URL"
	ironicAgentImage                 = "IRONIC_AGENT_IMAGE"
	imageCustomizationDeploymentName = "metal3-image-customization"
	imageCustomizationVolume         = "metal3-image-customization-volume"
	imageCustomizationPort           = 8084
	containerRegistriesConfPath      = "/etc/containers/registries.conf"
	containerRegistriesEnvVar        = "REGISTRIES_CONF_PATH"
	imageSharedDir                   = "/shared/html/images"
	deployISOEnvVar                  = "DEPLOY_ISO"
	deployISOFile                    = imageSharedDir + "/ironic-python-agent.iso"
	deployInitrdEnvVar               = "DEPLOY_INITRD"
	deployInitrdFile                 = imageSharedDir + "/ironic-python-agent.initramfs"
)

var (
	imageRegistriesVolumeMount = corev1.VolumeMount{
		Name:      imageCustomizationVolume,
		MountPath: containerRegistriesConfPath,
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

func getUrlFromIP(ipAddr string) string {
	if strings.Contains(ipAddr, ":") {
		// This is an IPv6 addr
		return "https://" + fmt.Sprintf("[%s]", ipAddr)
	}
	if ipAddr != "" {
		// This is an IPv4 addr
		return "https://" + ipAddr
	} else {
		return ""
	}
}

func createImageCustomizationContainer(images *Images, info *ProvisioningInfo, ironicIPs []string, inspectorIPs []string) corev1.Container {
	envVars := envWithProxy(info.Proxy, []corev1.EnvVar{}, ironicIPs[0]+","+inspectorIPs[0])

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
			Privileged: pointer.BoolPtr(true),
		},
		VolumeMounts: []corev1.VolumeMount{
			imageRegistriesVolumeMount,
			imageVolumeMount,
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
				Value: getUrlFromIP(ironicIPs[0]),
			},
			corev1.EnvVar{
				Name:  ironicInspectorBaseUrl,
				Value: getUrlFromIP(inspectorIPs[0]),
			},
			corev1.EnvVar{
				Name:  ironicAgentImage,
				Value: images.IronicAgent,
			},
			corev1.EnvVar{
				Name:  containerRegistriesEnvVar,
				Value: containerRegistriesConfPath,
			},
			corev1.EnvVar{
				Name:  ipOptions,
				Value: info.NetworkStack.IpOption(),
			},
			buildSSHKeyEnvVar(info.SSHKey),
			pullSecret),
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
	}
	return container
}

func newImageCustomizationPodTemplateSpec(info *ProvisioningInfo, labels *map[string]string, ironicIPs []string, inspectorIPs []string) *corev1.PodTemplateSpec {
	containers := []corev1.Container{
		createImageCustomizationContainer(info.Images, info, ironicIPs, inspectorIPs),
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
			TolerationSeconds: pointer.Int64Ptr(120),
		},
		{
			Key:               "node.kubernetes.io/unreachable",
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: pointer.Int64Ptr(120),
		},
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: podTemplateAnnotations,
			Labels:      *labels,
		},
		Spec: corev1.PodSpec{
			Containers:         containers,
			InitContainers:     injectProxyAndCA(initContainers, info.Proxy),
			HostNetwork:        false,
			DNSPolicy:          corev1.DNSClusterFirstWithHostNet,
			PriorityClassName:  "system-node-critical",
			NodeSelector:       map[string]string{"node-role.kubernetes.io/master": ""},
			ServiceAccountName: "cluster-baremetal-operator",
			Tolerations:        tolerations,
			Volumes: []corev1.Volume{
				imageRegistriesVolume(),
				imageVolume(),
				trustedCAVolume(),
			},
		},
	}
}

func newImageCustomizationDeployment(info *ProvisioningInfo, ironicIPs []string, inspectorIPs []string) *appsv1.Deployment {
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
	template := newImageCustomizationPodTemplateSpec(info, &podSpecLabels, ironicIPs, inspectorIPs)
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
			Replicas: pointer.Int32Ptr(1),
			Selector: selector,
			Template: *template,
		},
	}
}

func EnsureImageCustomizationDeployment(info *ProvisioningInfo) (updated bool, err error) {
	ironicIPs, inspectorIPs, err := GetIronicIPs(*info)
	if err != nil {
		return false, fmt.Errorf("unable to determine Ironic's IP to pass to the machine-image-customization-controller: %w", err)
	}

	imageCustomizationDeployment := newImageCustomizationDeployment(info, ironicIPs, inspectorIPs)
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
