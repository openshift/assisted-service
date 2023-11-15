package provisioning

import (
	"context"
	"fmt"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsclientv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
)

const (
	bmoServiceName    = "metal3-baremetal-operator"
	bmoDeploymentName = "metal3-baremetal-operator"
	// Default cert directory set by kubebuilder
	baremetalWebhookCertMountPath = "/tmp/k8s-webhook-server/serving-certs"
	baremetalWebhookCertVolume    = "cert"
	baremetalWebhookSecretName    = "baremetal-operator-webhook-server-cert"
	baremetalWebhookLabelName     = "baremetal.openshift.io/metal3-validating-webhook"
	baremetalWebhookServiceLabel  = "metal3-validating-webhook"
)

var baremetalWebhookCertMount = corev1.VolumeMount{
	Name:      baremetalWebhookCertVolume,
	ReadOnly:  true,
	MountPath: baremetalWebhookCertMountPath,
}

var bmoVolumes = []corev1.Volume{
	trustedCAVolume(),
	{
		Name: baremetalWebhookCertVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: baremetalWebhookSecretName,
			},
		},
	},
	{
		Name: ironicCredentialsVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: ironicSecretName,
				Items: []corev1.KeyToPath{
					{Key: ironicUsernameKey, Path: ironicUsernameKey},
					{Key: ironicPasswordKey, Path: ironicPasswordKey},
					{Key: ironicConfigKey, Path: ironicConfigKey},
				},
			},
		},
	},
	{
		Name: inspectorCredentialsVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: inspectorSecretName,
				Items: []corev1.KeyToPath{
					{Key: ironicUsernameKey, Path: ironicUsernameKey},
					{Key: ironicPasswordKey, Path: ironicPasswordKey},
					{Key: ironicConfigKey, Path: ironicConfigKey},
				},
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
		Name: inspectorTlsVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: tlsSecretName,
			},
		},
	},
}

func createContainerBaremetalOperator(info *ProvisioningInfo) (corev1.Container, error) {
	webhookPort, _ := strconv.ParseInt(baremetalWebhookPort, 10, 32) // #nosec
	externalUrlVar, err := setIronicExternalUrl(info)
	if err != nil {
		return corev1.Container{}, err
	}

	ironicURL, inspectorURL := getControlPlaneEndpoints(info)

	container := corev1.Container{
		Name:  "metal3-baremetal-operator",
		Image: info.Images.BaremetalOperator,
		Ports: []corev1.ContainerPort{
			{
				Name:          "webhook-server",
				HostPort:      int32(webhookPort),
				ContainerPort: int32(webhookPort),
			},
		},
		Command:         []string{"/baremetal-operator"},
		Args:            []string{"--health-addr", ":9446", "-build-preprov-image"},
		ImagePullPolicy: "IfNotPresent",
		VolumeMounts: []corev1.VolumeMount{
			ironicCredentialsMount,
			inspectorCredentialsMount,
			ironicTlsMount,
			baremetalWebhookCertMount,
		},
		Env: []corev1.EnvVar{
			getWatchNamespace(&info.ProvConfig.Spec),
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			},
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.name",
					},
				},
			},
			{
				Name:  "OPERATOR_NAME",
				Value: "baremetal-operator",
			},
			{
				Name:  ironicCertEnvVar,
				Value: metal3TlsRootDir + "/ironic/" + corev1.TLSCertKey,
			},
			{
				Name:  ironicInsecureEnvVar,
				Value: "true",
			},
			buildEnvVar(deployKernelUrl, &info.ProvConfig.Spec),
			{
				Name:  ironicEndpoint,
				Value: ironicURL,
			},
			{
				Name:  ironicInspectorEndpoint,
				Value: inspectorURL,
			},
			{
				Name:  "LIVE_ISO_FORCE_PERSISTENT_BOOT_DEVICE",
				Value: "Never",
			},
			{
				Name:  "METAL3_AUTH_ROOT_DIR",
				Value: metal3AuthRootDir,
			},
			setIronicExternalIp(externalIpEnvVar, &info.ProvConfig.Spec),
			externalUrlVar,
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("20m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
		},
	}

	if !info.BaremetalWebhookEnabled {
		// Webhook dependencies are not ready, thus we disable webhook explicitly,
		// since default is enabled.
		container.Args = append(container.Args, "--webhook-port", "0")
	} else {
		container.Args = append(container.Args, "--webhook-port", baremetalWebhookPort)
	}

	return container, nil
}

func newBMOPodTemplateSpec(info *ProvisioningInfo, labels *map[string]string) (*corev1.PodTemplateSpec, error) {
	container, err := createContainerBaremetalOperator(info)
	if err != nil {
		return nil, err
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

	containers := injectProxyAndCA([]corev1.Container{container}, info.Proxy)

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: podTemplateAnnotations,
			Labels:      *labels,
		},
		Spec: corev1.PodSpec{
			Volumes:            bmoVolumes,
			Containers:         containers,
			HostNetwork:        false,
			DNSPolicy:          corev1.DNSClusterFirstWithHostNet,
			PriorityClassName:  "system-node-critical",
			NodeSelector:       map[string]string{"node-role.kubernetes.io/master": ""},
			ServiceAccountName: "cluster-baremetal-operator",
			Tolerations:        tolerations,
		},
	}, nil
}

func newBMODeployment(info *ProvisioningInfo) (*appsv1.Deployment, error) {
	selector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"k8s-app":                 metal3AppName,
			cboLabelName:              bmoServiceName,
			baremetalWebhookLabelName: baremetalWebhookServiceLabel,
		},
	}
	podSpecLabels := map[string]string{
		"k8s-app":                 metal3AppName,
		cboLabelName:              bmoServiceName,
		baremetalWebhookLabelName: baremetalWebhookServiceLabel,
	}
	template, err := newBMOPodTemplateSpec(info, &podSpecLabels)
	if err != nil {
		return nil, err
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bmoDeploymentName,
			Namespace: info.Namespace,
			Annotations: map[string]string{
				cboOwnedAnnotation: "",
			},
			Labels: map[string]string{
				"k8s-app":                 metal3AppName,
				cboLabelName:              bmoServiceName,
				baremetalWebhookLabelName: baremetalWebhookServiceLabel,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(1),
			Selector: selector,
			Template: *template,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
		},
	}, nil
}

func EnsureBaremetalOperatorDeployment(info *ProvisioningInfo) (updated bool, err error) {
	// Create metal3 deployment object based on current baremetal configuration
	// It will be created with the cboOwnedAnnotation

	bmoDeployment, err := newBMODeployment(info)
	if err != nil {
		err = fmt.Errorf("unable to create a metal3 baremetal-operator deployment: %w", err)
		return
	}

	expectedGeneration := resourcemerge.ExpectedDeploymentGeneration(bmoDeployment, info.ProvConfig.Status.Generations)

	err = controllerutil.SetControllerReference(info.ProvConfig, bmoDeployment, info.Scheme)
	if err != nil {
		err = fmt.Errorf("unable to set controllerReference on deployment: %w", err)
		return
	}

	deploymentRolloutStartTime = time.Now()
	deployment, updated, err := resourceapply.ApplyDeployment(context.Background(),
		info.Client.AppsV1(), info.EventRecorder, bmoDeployment, expectedGeneration)
	if err != nil {
		return updated, err
	}
	if updated {
		resourcemerge.SetDeploymentGeneration(&info.ProvConfig.Status.Generations, deployment)
	}
	return updated, nil
}

func GetBaremetalOperatorDeploymentState(client appsclientv1.DeploymentsGetter, targetNamespace string, config *metal3iov1alpha1.Provisioning) (appsv1.DeploymentConditionType, error) {
	existing, err := client.Deployments(targetNamespace).Get(context.Background(), bmoDeploymentName, metav1.GetOptions{})
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

func DeleteBaremetalOperatorDeployment(info *ProvisioningInfo) error {
	return client.IgnoreNotFound(info.Client.AppsV1().Deployments(info.Namespace).Delete(context.Background(), bmoDeploymentName, metav1.DeleteOptions{}))
}
