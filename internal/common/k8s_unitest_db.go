package common

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	kubernetesNamespace              = "assisted-installer"
	databaseKubernetesContainerName  = "psql"
	databaseKubernetesDeploymentName = "assisted-service-ut-db"
	databaseKubernetesAppLabel       = "assisted-service-ut-db"
	databaseKubernetesVolumeName     = "psql"
	databaseKubernetesPortName       = "psql"
	databaseKubernetesServiceName    = "assisted-service-ut-db"
)

// KubernetesDBContext is a DBContext that runs postgresql as a pod in a k8s
// cluster
type KubernetesDBContext struct {
	client *k8s.Clientset
}

func getKubernetesDBContext() (*KubernetesDBContext, error) {
	var err error
	var k8sConfig *rest.Config
	kubeConfigPath := os.Getenv("KUBECONFIG")
	if kubeConfigPath == "" {
		kubeConfigPath = filepath.Join(homedir.HomeDir(), ".kube", "config")
	}
	k8sConfig, err = clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		return nil, err
	}
	k8sClient, err := k8s.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}
	return &KubernetesDBContext{k8sClient}, nil
}

func (c *KubernetesDBContext) RunDatabase() error {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: kubernetesNamespace,
		},
	}
	// Run teardown if namespace already exists
	_, err := c.client.CoreV1().Namespaces().Get(context.TODO(), kubernetesNamespace, metav1.GetOptions{})
	if err == nil {
		c.TeardownDatabase()
	}

	_, err = c.client.CoreV1().Namespaces().Create(context.TODO(), namespace, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: databaseKubernetesDeploymentName,
			Labels: map[string]string{
				"app": databaseKubernetesAppLabel,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": databaseKubernetesAppLabel,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": databaseKubernetesAppLabel,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  databaseKubernetesContainerName,
							Image: databaseContainerImage,
							Ports: []corev1.ContainerPort{
								{
									Name:          databaseKubernetesPortName,
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: int32(databaseDefaultPort),
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "POSTGRESQL_ADMIN_PASSWORD",
									Value: databaseAdminPassword,
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(databaseDefaultPort),
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      databaseKubernetesVolumeName,
									MountPath: databaseDataDir,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: databaseKubernetesVolumeName,
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									Medium: corev1.StorageMediumMemory,
								},
							},
						},
					},
				},
			},
		},
	}
	_, err = c.client.AppsV1().Deployments(kubernetesNamespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	// Wait for deployment to rollout
	err = wait.PollImmediate(time.Second*5, time.Minute*5, func() (bool, error) {
		var deploymentErr error
		deployment, deploymentErr := c.client.AppsV1().Deployments(kubernetesNamespace).Get(
			context.TODO(), databaseKubernetesDeploymentName, metav1.GetOptions{})
		if deploymentErr != nil {
			return false, deploymentErr
		}
		return deployment.Status.ReadyReplicas > 0, nil
	})
	if err != nil {
		return err
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: databaseKubernetesServiceName,
			Labels: map[string]string{
				"app": databaseKubernetesAppLabel,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     databaseDefaultPort,
					Protocol: corev1.ProtocolTCP,
					Name:     databaseKubernetesPortName,
				},
			},
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				"app": databaseKubernetesAppLabel,
			},
		},
	}
	_, err = c.client.CoreV1().Services(kubernetesNamespace).Create(context.TODO(), service, metav1.CreateOptions{})
	return err
}

func (c *KubernetesDBContext) TeardownDatabase() {
	err := c.client.CoreV1().Namespaces().Delete(context.TODO(), kubernetesNamespace, metav1.DeleteOptions{})
	Expect(err).ShouldNot(HaveOccurred())

	// Wait for it to dissappear
	err = wait.PollImmediate(time.Second*5, time.Minute*5, func() (bool, error) {
		var namespaceErr error
		_, namespaceErr = c.client.CoreV1().Namespaces().Get(context.TODO(), kubernetesNamespace, metav1.GetOptions{})
		if errors.IsNotFound(namespaceErr) {
			return false, namespaceErr
		}
		return true, nil
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (c *KubernetesDBContext) GetDatabaseHostPort() (string, string) {
	var host string
	var svc *corev1.Service
	err := wait.PollImmediate(time.Second*5, time.Minute*5, func() (bool, error) {
		var err error
		svc, err = c.client.CoreV1().Services(kubernetesNamespace).Get(context.TODO(), databaseKubernetesServiceName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return len(svc.Status.LoadBalancer.Ingress) > 0, nil
	})
	Expect(err).ShouldNot(HaveOccurred())
	for _, ip := range svc.Status.LoadBalancer.Ingress {
		host = ip.IP
		break
	}
	if host == "" {
		for _, ip := range svc.Spec.ExternalIPs {
			host = ip
			break
		}
	}
	return host, strconv.Itoa(databaseDefaultPort)
}
