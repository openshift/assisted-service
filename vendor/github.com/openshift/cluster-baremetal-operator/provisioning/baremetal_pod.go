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
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsclientv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	utilnet "k8s.io/utils/net"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configv1 "github.com/openshift/api/config/v1"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
)

const (
	metal3AppName                    = "metal3"
	baremetalDeploymentName          = "metal3"
	baremetalSharedVolume            = "metal3-shared"
	metal3AuthRootDir                = "/auth"
	metal3TlsRootDir                 = "/certs"
	ironicCredentialsVolume          = "metal3-ironic-basic-auth"
	ironicTlsVolume                  = "metal3-ironic-tls"
	vmediaTlsVolume                  = "metal3-vmedia-tls"
	ironicPrometheusExporterName     = "metal3-ironic-prometheus-exporter"
	ironicInsecureEnvVar             = "IRONIC_INSECURE"
	ironicKernelParamsEnvVar         = "IRONIC_KERNEL_PARAMS"
	ironicCertEnvVar                 = "IRONIC_CACERT_FILE"
	sshKeyEnvVar                     = "IRONIC_RAMDISK_SSH_KEY"
	externalIpEnvVar                 = "IRONIC_EXTERNAL_IP"
	externalUrlEnvVar                = "IRONIC_EXTERNAL_URL_V6"
	ironicProxyEnvVar                = "IRONIC_REVERSE_PROXY_SETUP"
	ironicPrivatePortEnvVar          = "IRONIC_PRIVATE_PORT"
	ironicListenPortEnvVar           = "IRONIC_LISTEN_PORT"
	cboOwnedAnnotation               = "baremetal.openshift.io/owned"
	cboLabelName                     = "baremetal.openshift.io/cluster-baremetal-operator"
	externalTrustBundleConfigMapName = "cbo-trusted-ca"
	ironicConfigVolume               = "metal3-ironic-conf"
	ironicDataVolume                 = "metal3-ironic-data"
	ironicConfigPath                 = "/conf"
	ironicDataPath                   = "/data"
	ironicTmpVolume                  = "metal3-ironic-tmp"
	ironicTmpPath                    = "/tmp"
	bmcCACertMountPath               = "/certs/ca/bmc"
	bmcCACertConfigMapName           = "bmc-verify-ca"
	bmcCACertVolume                  = "bmc-verify-ca"
)

var podTemplateAnnotations = map[string]string{
	"target.workload.openshift.io/management": `{"effect": "PreferredDuringScheduling"}`,
}

var deploymentRolloutStartTime = time.Now()
var deploymentRolloutTimeout = 5 * time.Minute

var sharedVolumeMount = corev1.VolumeMount{
	Name:      baremetalSharedVolume,
	MountPath: "/shared",
}

var ironicCredentialsMount = corev1.VolumeMount{
	Name:      ironicCredentialsVolume,
	MountPath: metal3AuthRootDir + "/ironic",
	ReadOnly:  true,
}

var ironicTlsMount = corev1.VolumeMount{
	Name:      ironicTlsVolume,
	MountPath: metal3TlsRootDir + "/ironic",
	ReadOnly:  true,
}

var ironicConfigMount = corev1.VolumeMount{
	Name:      ironicConfigVolume,
	MountPath: ironicConfigPath,
}

var ironicDataMount = corev1.VolumeMount{
	Name:      ironicDataVolume,
	MountPath: ironicDataPath,
}

var ironicTmpMount = corev1.VolumeMount{
	Name:      ironicTmpVolume,
	MountPath: ironicTmpPath,
}

var vmediaTlsMount = corev1.VolumeMount{
	Name:      vmediaTlsVolume,
	MountPath: metal3TlsRootDir + "/vmedia",
	ReadOnly:  true,
}

var baremetalCACertMount = corev1.VolumeMount{
	Name:      bmcCACertVolume,
	ReadOnly:  true,
	MountPath: bmcCACertMountPath,
}

func trustedCAVolume() corev1.Volume {
	return corev1.Volume{
		Name: "trusted-ca",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				Items: []corev1.KeyToPath{{Key: "ca-bundle.crt", Path: "tls-ca-bundle.pem"}},
				LocalObjectReference: corev1.LocalObjectReference{
					Name: externalTrustBundleConfigMapName,
				},
				Optional: ptr.To(true),
			},
		},
	}
}

var metal3Volumes = []corev1.Volume{
	{
		Name: baremetalSharedVolume,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	},
	{
		Name: ironicConfigVolume,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	},
	{
		Name: ironicDataVolume,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	},
	{
		Name: ironicTmpVolume,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	},
	imageVolume(),
	ironicAgentPullSecretVolume(),
	caTrustDirVolume(),
	{
		Name: ironicCredentialsVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: ironicSecretName,
				Items: []corev1.KeyToPath{
					{Key: ironicUsernameKey, Path: ironicUsernameKey},
					{Key: ironicPasswordKey, Path: ironicPasswordKey},
					{Key: ironicHtpasswdKey, Path: ironicHtpasswdKey},
				},
			},
		},
	},
	{
		Name: baremetalWebhookCertVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: baremetalWebhookSecretName,
			},
		},
	},
	trustedCAVolume(),
	{
		Name: bmcCACertVolume,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: bmcCACertConfigMapName,
				},
				Optional: ptr.To(true),
			},
		},
	},
	{
		Name: ironicTlsVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: tlsSecretName,
			},
		},
	},
	{
		Name: vmediaTlsVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: tlsSecretName,
			},
		},
	},
}

func buildEnvVar(name string, baremetalProvisioningConfig *metal3iov1alpha1.ProvisioningSpec) corev1.EnvVar {
	value := getMetal3DeploymentConfig(name, baremetalProvisioningConfig)
	if value != nil {
		return corev1.EnvVar{
			Name:  name,
			Value: *value,
		}
	} else if name == provisioningIP && baremetalProvisioningConfig.ProvisioningNetwork == metal3iov1alpha1.ProvisioningNetworkDisabled &&
		baremetalProvisioningConfig.ProvisioningInterface == "" {
		return corev1.EnvVar{
			Name: name,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP",
				},
			},
		}
	}

	return corev1.EnvVar{
		Name: name,
	}
}

func getKernelParams(config *metal3iov1alpha1.ProvisioningSpec, networkStack NetworkStackType) string {
	// OCPBUGS-872: workaround for https://bugzilla.redhat.com/show_bug.cgi?id=2111675
	return fmt.Sprintf("rd.net.timeout.carrier=30 %s",
		IpOptionForProvisioning(config, networkStack))
}

func setIronicExternalIp(name string, config *metal3iov1alpha1.ProvisioningSpec) corev1.EnvVar {
	if len(config.ExternalIPs) > 0 {
		return corev1.EnvVar{
			Name:  name,
			Value: config.ExternalIPs[0],
		}
	} else if config.ProvisioningNetwork != metal3iov1alpha1.ProvisioningNetworkDisabled && config.VirtualMediaViaExternalNetwork {
		return corev1.EnvVar{
			Name: name,
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.hostIP",
				},
			},
		}
	}
	return corev1.EnvVar{
		Name: name,
	}
}

func setIronicExternalIPv6(info *ProvisioningInfo) (corev1.EnvVar, error) {
	var ironicIPv6 string

	imageServerIPs, err := GetImageServerIPs(info)
	if len(imageServerIPs) > 0 {
		for _, imgServerIP := range imageServerIPs {
			if utilnet.IsIPv6String(imgServerIP) {
				ironicIPv6 = imgServerIP
				break
			}
		}
	}
	if err != nil || ironicIPv6 == "" {
		return corev1.EnvVar{
			Name: externalUrlEnvVar,
		}, err
	}

	// protocol, host, port
	urlTemplate := "%s://[%s]:%s"

	if info.ProvConfig.Spec.DisableVirtualMediaTLS {
		return corev1.EnvVar{
			Name:  externalUrlEnvVar,
			Value: fmt.Sprintf(urlTemplate, "http", ironicIPv6, baremetalHttpPort),
		}, nil
	} else {
		return corev1.EnvVar{
			Name:  externalUrlEnvVar,
			Value: fmt.Sprintf(urlTemplate, "https", ironicIPv6, baremetalVmediaHttpsPort),
		}, nil
	}
}

func newMetal3InitContainers(info *ProvisioningInfo) []corev1.Container {
	initContainers := []corev1.Container{}

	// If the provisioning network is disabled, and the user hasn't requested a
	// particular provisioning IP on the machine CIDR, we have nothing for this container
	// to manage.
	if info.ProvConfig.Spec.ProvisioningIP != "" && info.ProvConfig.Spec.ProvisioningNetwork != metal3iov1alpha1.ProvisioningNetworkDisabled {
		initContainers = append(initContainers, createInitContainerStaticIpSet(info.Images, &info.ProvConfig.Spec))
	}

	// Extract the pre-provisioning images from a container in the payload
	initContainers = append(initContainers, createInitContainerMachineOSImages(info, "--pxe", imageVolumeMount, imageSharedDir))

	// If the ProvisioningOSDownloadURL is set, we download the URL specified in it
	if info.ProvConfig.Spec.ProvisioningOSDownloadURL != "" {
		initContainers = append(initContainers, createInitContainerMachineOsDownloader(info, info.ProvConfig.Spec.ProvisioningOSDownloadURL, false, true))
	}

	return injectProxyAndCA(initContainers, info.Proxy)
}

func createInitContainerMachineOsDownloader(info *ProvisioningInfo, imageURLs string, useLiveImages, setIpOptions bool) corev1.Container {
	var command string
	name := "metal3-machine-os-downloader"
	if useLiveImages {
		command = "/usr/local/bin/get-live-images.sh"
		name = name + "-live-images"
	} else {
		command = "/usr/local/bin/get-resource.sh"
	}

	env := []corev1.EnvVar{
		{
			Name:  machineImageUrl,
			Value: imageURLs,
		},
	}
	if setIpOptions {
		env = append(env,
			corev1.EnvVar{
				Name:  ipOptions,
				Value: info.NetworkStack.IpOption(),
			})
	}
	initContainer := corev1.Container{
		Name:            name,
		Image:           info.Images.MachineOsDownloader,
		Command:         []string{command},
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(true),
			// Needed for hostPath image volume mount
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		VolumeMounts: []corev1.VolumeMount{imageVolumeMount, sharedVolumeMount, ironicTmpMount},
		Env:          env,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	return initContainer
}

func createInitContainerStaticIpSet(images *Images, config *metal3iov1alpha1.ProvisioningSpec) corev1.Container {
	initContainer := corev1.Container{
		Name:            "metal3-static-ip-set",
		Image:           images.StaticIpManager,
		Command:         []string{"/set-static-ip"},
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
				Add:  []corev1.Capability{"NET_ADMIN"},
			},
		},
		Env: []corev1.EnvVar{
			buildEnvVar(provisioningIP, config),
			buildEnvVar(provisioningInterface, config),
			buildEnvVar(provisioningMacAddresses, config),
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	return initContainer
}

func newMetal3Containers(info *ProvisioningInfo) []corev1.Container {
	containers := []corev1.Container{
		createContainerMetal3Httpd(info.Images, info),
		createContainerMetal3Ironic(info.Images, info, &info.ProvConfig.Spec, info.SSHKey),
		createContainerMetal3RamdiskLogs(info.Images),
	}

	// If the provisioning network is disabled, and the user hasn't requested a
	// particular provisioning IP on the machine CIDR, we have nothing for this container
	// to manage.
	if info.ProvConfig.Spec.ProvisioningIP != "" && info.ProvConfig.Spec.ProvisioningNetwork != metal3iov1alpha1.ProvisioningNetworkDisabled {
		containers = append(containers, createContainerMetal3StaticIpManager(info.Images, &info.ProvConfig.Spec))
	}

	if info.ProvConfig.Spec.ProvisioningNetwork != metal3iov1alpha1.ProvisioningNetworkDisabled {
		containers = append(containers, createContainerMetal3Dnsmasq(info.Images, &info.ProvConfig.Spec))
	}

	// Optionally deploy IPE
	if info.ProvConfig.Spec.PrometheusExporter != nil && info.ProvConfig.Spec.PrometheusExporter.Enabled {
		containers = append(containers, createContainerIronicPrometheusExporter(info.Images))
	}

	return injectProxyAndCA(containers, info.Proxy)
}

func getWatchNamespace(config *metal3iov1alpha1.ProvisioningSpec) corev1.EnvVar {
	if config.WatchAllNamespaces {
		return corev1.EnvVar{
			Name:  "WATCH_NAMESPACE",
			Value: "",
		}
	} else {
		return corev1.EnvVar{
			Name: "WATCH_NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		}
	}
}

func buildSSHKeyEnvVar(sshKey string) corev1.EnvVar {
	return corev1.EnvVar{Name: sshKeyEnvVar, Value: sshKey}
}

func createContainerMetal3Dnsmasq(images *Images, config *metal3iov1alpha1.ProvisioningSpec) corev1.Container {
	envVars := []corev1.EnvVar{
		buildEnvVar(httpPort, config),
		buildEnvVar(provisioningInterface, config),
		buildEnvVar(dhcpRange, config),
		buildEnvVar(provisioningMacAddresses, config),
	}
	if config.ProvisioningDNS {
		envVars = append(envVars, corev1.EnvVar{
			Name:  dnsIP,
			Value: useProvisioningDNS,
		})
	}
	container := corev1.Container{
		Name:            "metal3-dnsmasq",
		Image:           images.Ironic,
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(true),
			// Needed for hostPath image volume mount
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
				Add: []corev1.Capability{
					"NET_ADMIN",
					"NET_RAW",
					"NET_BIND_SERVICE",
				},
			},
		},
		Command: []string{"/bin/rundnsmasq"},
		VolumeMounts: []corev1.VolumeMount{
			sharedVolumeMount,
			imageVolumeMount,
			ironicConfigMount,
			ironicDataMount,
		},
		Env: envVars,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("5m"),
				corev1.ResourceMemory: resource.MustParse("5Mi"),
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	return container
}

func createContainerMetal3Httpd(images *Images, info *ProvisioningInfo) corev1.Container {
	port, _ := strconv.Atoi(baremetalHttpPort)             // #nosec
	httpsPort, _ := strconv.Atoi(baremetalVmediaHttpsPort) // #nosec

	ironicPort := baremetalIronicPort
	// In the proxy mode, the ironic API is served on the private port,
	// while ironic-proxy, running as a DeamonSet on all nodes, serves on
	// 6385 and proxies the traffic.
	if UseIronicProxy(info) {
		ironicPort = ironicPrivatePort
	}
	config := &info.ProvConfig.Spec

	volumeMounts := []corev1.VolumeMount{
		sharedVolumeMount,
		ironicCredentialsMount,
		imageVolumeMount,
		ironicTlsMount,
		ironicDataMount,
		ironicConfigMount,
	}
	ports := []corev1.ContainerPort{
		{
			Name:          "ironic",
			ContainerPort: int32(ironicPort),
			HostPort:      int32(ironicPort),
		},
		{
			Name:          httpPortName,
			ContainerPort: int32(port),
			HostPort:      int32(port),
		},
	}

	if !config.DisableVirtualMediaTLS {
		volumeMounts = append(volumeMounts, vmediaTlsMount)
		ports = append(ports, corev1.ContainerPort{
			Name:          vmediaHttpsPortName,
			ContainerPort: int32(httpsPort),
			HostPort:      int32(httpsPort),
		})
	}

	container := corev1.Container{
		Name:            "metal3-httpd",
		Image:           images.Ironic,
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(true),
			// Needed for hostPath image volume mount
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		Command:      []string{"/bin/runhttpd"},
		VolumeMounts: volumeMounts,
		Env: []corev1.EnvVar{
			buildEnvVar(httpPort, config),
			buildEnvVar(provisioningIP, config),
			buildEnvVar(provisioningInterface, config),
			buildSSHKeyEnvVar(info.SSHKey),
			buildEnvVar(provisioningMacAddresses, config),
			buildEnvVar(vmediaHttpsPort, config),
			{
				Name:  ironicProxyEnvVar,
				Value: "true",
			},
			{
				Name:  ironicPrivatePortEnvVar,
				Value: useUnixSocket,
			},
			{
				Name:  ironicListenPortEnvVar,
				Value: fmt.Sprint(ironicPort),
			},
		},
		Ports: ports,
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

func createContainerMetal3Ironic(images *Images, info *ProvisioningInfo, config *metal3iov1alpha1.ProvisioningSpec, sshKey string) corev1.Container {
	volumes := []corev1.VolumeMount{
		sharedVolumeMount,
		imageVolumeMount,
		ironicTlsMount,
		ironicDataMount,
		ironicConfigMount,
		ironicTmpMount,
		baremetalCACertMount,
	}
	if !config.DisableVirtualMediaTLS {
		volumes = append(volumes, vmediaTlsMount)
	}

	container := corev1.Container{
		Name:            "metal3-ironic",
		Image:           images.Ironic,
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(true),
			// Needed for hostPath image volume mount
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
		Command:      []string{"/bin/runironic"},
		VolumeMounts: volumes,
		Env: []corev1.EnvVar{
			{
				Name:  ironicInsecureEnvVar,
				Value: "true",
			},
			{
				Name:  ironicKernelParamsEnvVar,
				Value: getKernelParams(&info.ProvConfig.Spec, info.NetworkStack),
			},
			{
				Name:  ironicProxyEnvVar,
				Value: "true",
			},
			{
				Name:  ironicPrivatePortEnvVar,
				Value: useUnixSocket,
			},
			buildEnvVar(httpPort, config),
			buildEnvVar(provisioningIP, config),
			buildEnvVar(provisioningInterface, config),
			buildSSHKeyEnvVar(sshKey),
			setIronicExternalIp(externalIpEnvVar, config),
			buildEnvVar(provisioningMacAddresses, config),
			buildEnvVar(vmediaHttpsPort, config),
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	// Append SEND_SENSOR_DATA and OS_SENSOR_DATA__INTERVAL env vars only when metrics collection is requested
	if config.PrometheusExporter != nil && config.PrometheusExporter.Enabled {
		container.Env = append(container.Env,
			corev1.EnvVar{
				Name:  sendSensorData,
				Value: "true",
			},
			corev1.EnvVar{
				Name:  sensorDataInterval,
				Value: strconv.Itoa(config.PrometheusExporter.SensorCollectionInterval),
			},
		)
	}

	return container
}

func createContainerMetal3RamdiskLogs(images *Images) corev1.Container {
	container := corev1.Container{
		Name:            "metal3-ramdisk-logs",
		Image:           images.Ironic,
		ImagePullPolicy: "IfNotPresent",
		Command:         []string{"/bin/runlogwatch.sh"},
		VolumeMounts:    []corev1.VolumeMount{sharedVolumeMount},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("5Mi"),
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
				Add: []corev1.Capability{
					"CAP_DAC_OVERRIDE",
				},
			},
		},
	}
	return container
}

// createContainerIronicPrometheusExporter creates IPE container that services the /metrics endpoint
// which Prometheus can scrape to collect generated metrics from sensors
func createContainerIronicPrometheusExporter(images *Images) corev1.Container {
	return corev1.Container{
		Name:            ironicPrometheusExporterName,
		Image:           images.Ironic,
		ImagePullPolicy: "IfNotPresent",
		Command:         []string{"/bin/runironic-exporter"},
		VolumeMounts:    []corev1.VolumeMount{sharedVolumeMount},
		Ports: []corev1.ContainerPort{
			{
				Name:          metricsPortName,
				ContainerPort: int32(baremetalMetricsPort),
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("20m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		SecurityContext: &corev1.SecurityContext{
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}
}

func createContainerMetal3StaticIpManager(images *Images, config *metal3iov1alpha1.ProvisioningSpec) corev1.Container {
	container := corev1.Container{
		Name:            "metal3-static-ip-manager",
		Image:           images.StaticIpManager,
		Command:         []string{"/refresh-static-ip"},
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(true),
			// Needed for mounting /proc to set the addr_gen_mode
			Privileged: ptr.To(true),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
				Add: []corev1.Capability{
					"NET_ADMIN",
					"FOWNER", // Needed for setting the addr_gen_mode
				},
			},
		},
		Env: []corev1.EnvVar{
			buildEnvVar(provisioningIP, config),
			buildEnvVar(provisioningInterface, config),
			buildEnvVar(provisioningMacAddresses, config),
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

func newMetal3PodTemplateSpec(info *ProvisioningInfo, labels *map[string]string) *corev1.PodTemplateSpec {
	initContainers := newMetal3InitContainers(info)
	containers := newMetal3Containers(info)
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
			Volumes:           metal3Volumes,
			InitContainers:    initContainers,
			Containers:        containers,
			HostNetwork:       true,
			DNSPolicy:         corev1.DNSClusterFirstWithHostNet,
			PriorityClassName: "system-node-critical",
			NodeSelector:      nodeSelector,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: ptr.To(false),
			},
			ServiceAccountName: "cluster-baremetal-operator",
			Tolerations:        tolerations,
		},
	}
}

func mountsWithTrustedCA(mounts []corev1.VolumeMount) []corev1.VolumeMount {
	mounts = append(mounts, corev1.VolumeMount{
		MountPath: "/etc/pki/ca-trust/extracted/pem",
		Name:      "trusted-ca",
		ReadOnly:  true,
	})

	return mounts
}

func injectProxyAndCA(containers []corev1.Container, proxy *configv1.Proxy) []corev1.Container {
	var injectedContainers []corev1.Container

	for _, container := range containers {
		container.Env = envWithProxy(proxy, container.Env, nil)
		container.VolumeMounts = mountsWithTrustedCA(container.VolumeMounts)
		injectedContainers = append(injectedContainers, container)
	}

	return injectedContainers
}

func envWithProxy(proxy *configv1.Proxy, envVars []corev1.EnvVar, noproxy []string) []corev1.EnvVar {
	if proxy == nil {
		return envVars
	}

	if proxy.Status.HTTPProxy != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "HTTP_PROXY",
			Value: proxy.Status.HTTPProxy,
		})
	}
	if proxy.Status.HTTPSProxy != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "HTTPS_PROXY",
			Value: proxy.Status.HTTPSProxy,
		})
	}
	if proxy.Status.NoProxy != "" || noproxy != nil {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "NO_PROXY",
			Value: proxy.Status.NoProxy + "," + strings.Join(noproxy, ","),
		})
	}

	return envVars
}

func newMetal3Deployment(info *ProvisioningInfo) *appsv1.Deployment {
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"k8s-app":    metal3AppName,
			cboLabelName: stateService,
		},
	}
	podSpecLabels := map[string]string{
		"k8s-app":    metal3AppName,
		cboLabelName: stateService,
	}
	template := newMetal3PodTemplateSpec(info, &podSpecLabels)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      baremetalDeploymentName,
			Namespace: info.Namespace,
			Annotations: map[string]string{
				cboOwnedAnnotation: "",
			},
			Labels: map[string]string{
				"k8s-app":    metal3AppName,
				cboLabelName: stateService,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: selector,
			Template: *template,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
		},
	}
}

func getMetal3DeploymentSelector(client appsclientv1.DeploymentsGetter, targetNamespace string) (*metav1.LabelSelector, error) {
	existing, err := client.Deployments(targetNamespace).Get(context.Background(), baremetalDeploymentName, metav1.GetOptions{})
	if existing != nil && err == nil {
		return existing.Spec.Selector, nil
	}
	return nil, err
}

func EnsureMetal3Deployment(info *ProvisioningInfo) (updated bool, err error) {
	// Create metal3 deployment object based on current baremetal configuration
	// It will be created with the cboOwnedAnnotation

	metal3Deployment := newMetal3Deployment(info)

	expectedGeneration := resourcemerge.ExpectedDeploymentGeneration(metal3Deployment, info.ProvConfig.Status.Generations)

	err = controllerutil.SetControllerReference(info.ProvConfig, metal3Deployment, info.Scheme)
	if err != nil {
		err = fmt.Errorf("unable to set controllerReference on deployment: %w", err)
		return
	}

	deploymentRolloutStartTime = time.Now()
	deployment, updated, err := resourceapply.ApplyDeployment(context.Background(),
		info.Client.AppsV1(), info.EventRecorder, metal3Deployment, expectedGeneration)
	if err != nil {
		err = fmt.Errorf("unable to apply Metal3 deployment: %w", err)
		// Check if ApplyDeployment failed because the existing Pod had an outdated
		// Pod Selector.
		selector, get_err := getMetal3DeploymentSelector(info.Client.AppsV1(), info.Namespace)
		if get_err != nil || equality.Semantic.DeepEqual(selector, metal3Deployment.Spec.Selector) {
			return
		}
		// This is an older deployment with the incorrect Pod Selector.
		// Delete deployment now and re-create in the next reconcile.
		// The operator is watching deployments so the reconcile should be triggered when metal3 deployment
		// is deleted.
		if delete_err := DeleteMetal3Deployment(info); delete_err != nil {
			err = fmt.Errorf("unable to delete Metal3 deployment with incorrect Pod Selector: %w", delete_err)
			return
		}
	}
	if updated {
		resourcemerge.SetDeploymentGeneration(&info.ProvConfig.Status.Generations, deployment)
	}
	return updated, nil
}

func getDeploymentCondition(deployment *appsv1.Deployment) appsv1.DeploymentConditionType {
	var progressing, available, replicaFailure bool
	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionTrue {
			progressing = true
		}
		if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
			available = true
		}
		if cond.Type == appsv1.DeploymentReplicaFailure && cond.Status == corev1.ConditionTrue {
			replicaFailure = true
		}
	}
	switch {
	case replicaFailure && !progressing:
		return appsv1.DeploymentReplicaFailure
	case available && !replicaFailure:
		return appsv1.DeploymentAvailable
	default:
		return appsv1.DeploymentProgressing
	}
}

// Provide the current state of metal3 deployment
func GetDeploymentState(client appsclientv1.DeploymentsGetter, targetNamespace string, config *metal3iov1alpha1.Provisioning) (appsv1.DeploymentConditionType, error) {
	existing, err := client.Deployments(targetNamespace).Get(context.Background(), baremetalDeploymentName, metav1.GetOptions{})
	if err != nil || existing == nil {
		// There were errors accessing the deployment.
		return appsv1.DeploymentReplicaFailure, err
	}
	deploymentState := getDeploymentCondition(existing)
	if deploymentState == appsv1.DeploymentProgressing && deploymentRolloutTimeout <= time.Since(deploymentRolloutStartTime) {
		return appsv1.DeploymentReplicaFailure, nil
	}
	return deploymentState, nil
}

func DeleteMetal3Deployment(info *ProvisioningInfo) error {
	return client.IgnoreNotFound(info.Client.AppsV1().Deployments(info.Namespace).Delete(context.Background(), baremetalDeploymentName, metav1.DeleteOptions{}))
}
