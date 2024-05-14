package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kelseyhightower/envconfig"
	authzv1 "github.com/openshift/api/authorization/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	spokeReclaimNamespaceName = "assisted-installer"
	spokeReclaimCMName        = "reclaim-config"
	spokeRBACName             = "node-reclaim"
)

type reclaimConfig struct {
	AgentContainerImage  string        `envconfig:"AGENT_DOCKER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-agent:latest"`
	AuthType             auth.AuthType `envconfig:"AUTH_TYPE" default:""`
	ServiceBaseURL       string        `envconfig:"SERVICE_BASE_URL"`
	ServiceCACertPath    string        `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	SkipCertVerification bool          `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	hostFSMountDir       string
}

type agentReclaimer struct {
	reclaimConfig
}

func newAgentReclaimer(hostFSMountDir string) (*agentReclaimer, error) {
	config := reclaimConfig{}

	if err := envconfig.Process("", &config); err != nil {
		return nil, errors.Wrapf(err, "failed to populate reclaimConfig")
	}
	config.hostFSMountDir = hostFSMountDir
	return &agentReclaimer{reclaimConfig: config}, nil
}

func ensureSpokeNamespace(ctx context.Context, c client.Client, log logrus.FieldLogger) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: spokeReclaimNamespaceName}}
	mutate := func() error {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}
		// Newer versions of kubernetes require these labels for pods to run with escalated privileges
		ns.Labels["pod-security.kubernetes.io/enforce"] = "privileged"
		return nil
	}
	result, err := controllerutil.CreateOrUpdate(ctx, c, ns, mutate)
	if result != controllerutil.OperationResultNone {
		log.Infof("Namespace %s %s for agent reclaim", spokeReclaimNamespaceName, result)
	}
	return err
}

func ensureSpokeServiceAccount(ctx context.Context, c client.Client, log logrus.FieldLogger) error {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spokeRBACName,
			Namespace: spokeReclaimNamespaceName,
		},
	}
	result, err := controllerutil.CreateOrUpdate(ctx, c, sa, func() error { return nil })
	if result != controllerutil.OperationResultNone {
		log.Infof("ServiceAccount %s/%s %s for agent reclaim", sa.Namespace, sa.Name, result)
	}
	return err
}

func ensureSpokeRole(ctx context.Context, c client.Client, log logrus.FieldLogger) error {
	role := &authzv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spokeRBACName,
			Namespace: spokeReclaimNamespaceName,
		},
	}

	mutate := func() error {
		role.Rules = []authzv1.PolicyRule{{
			APIGroups:     []string{"security.openshift.io"},
			Resources:     []string{"securitycontextconstraints"},
			ResourceNames: []string{"privileged"},
			Verbs:         []string{"use"},
		}}
		return nil
	}
	result, err := controllerutil.CreateOrUpdate(ctx, c, role, mutate)
	if result != controllerutil.OperationResultNone {
		log.Infof("Role %s/%s %s for agent reclaim", role.Namespace, role.Name, result)
	}
	return err
}

func ensureSpokeRoleBinding(ctx context.Context, c client.Client, log logrus.FieldLogger) error {
	rb := &authzv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spokeRBACName,
			Namespace: spokeReclaimNamespaceName,
		},
	}

	mutate := func() error {
		rb.RoleRef = corev1.ObjectReference{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "Role",
			Name:       spokeRBACName,
			Namespace:  spokeReclaimNamespaceName,
		}
		rb.Subjects = []corev1.ObjectReference{{
			Kind:      "ServiceAccount",
			Name:      spokeRBACName,
			Namespace: spokeReclaimNamespaceName,
		}}
		return nil
	}
	result, err := controllerutil.CreateOrUpdate(ctx, c, rb, mutate)
	if result != controllerutil.OperationResultNone {
		log.Infof("RoleBinding %s/%s %s for agent reclaim", rb.Namespace, rb.Name, result)
	}
	return err
}

func spokeReclaimSecretName(infraEnvID string) string {
	return fmt.Sprintf("reclaim-%s-token", infraEnvID)
}

func (r *agentReclaimer) ensureSpokeAgentSecret(ctx context.Context, c client.Client, log logrus.FieldLogger, infraEnvID string) error {
	authToken := ""
	if r.AuthType == auth.TypeLocal {
		var err error
		authToken, err = gencrypto.LocalJWT(infraEnvID, gencrypto.InfraEnvKey)
		if err != nil {
			return errors.Wrapf(err, "failed to create local JWT for infraEnv %s", infraEnvID)
		}
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spokeReclaimSecretName(infraEnvID),
			Namespace: spokeReclaimNamespaceName,
		},
	}
	mutate := func() error {
		secret.Type = corev1.SecretTypeOpaque
		secret.Data = map[string][]byte{"auth-token": []byte(authToken)}
		return nil
	}
	result, err := controllerutil.CreateOrUpdate(ctx, c, secret, mutate)
	if result != controllerutil.OperationResultNone {
		log.Infof("Secret %s/%s %s for agent reclaim", secret.Namespace, secret.Name, result)
	}
	return err
}

func (r *agentReclaimer) ensureSpokeAgentCertCM(ctx context.Context, c client.Client, log logrus.FieldLogger) error {
	if r.ServiceCACertPath == "" {
		return nil
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spokeReclaimCMName,
			Namespace: spokeReclaimNamespaceName,
		},
	}
	mutate := func() error {
		data, err := os.ReadFile(r.ServiceCACertPath)
		if err != nil {
			return errors.Wrap(err, "failed to read service ca cert")
		}
		cm.Data = map[string]string{filepath.Base(common.HostCACertPath): string(data)}
		return nil
	}
	result, err := controllerutil.CreateOrUpdate(ctx, c, cm, mutate)
	if result != controllerutil.OperationResultNone {
		log.Infof("ConfigMap %s/%s %s for agent reclaim", cm.Namespace, cm.Name, result)
	}
	return err
}

func (r *agentReclaimer) createNextStepRunnerDaemonSet(ctx context.Context, c client.Client, log logrus.FieldLogger, nodeName string, infraEnvID string, hostID string) error {
	node := &corev1.Node{}
	if err := c.Get(ctx, types.NamespacedName{Name: nodeName}, node); err != nil {
		return errors.Wrapf(err, "failed to find node %s", nodeName)
	}

	cliArgs := []string{
		fmt.Sprintf("-url=%s", r.ServiceBaseURL),
		fmt.Sprintf("-infra-env-id=%s", infraEnvID),
		fmt.Sprintf("-host-id=%s", hostID),
		fmt.Sprintf("-agent-version=%s", r.AgentContainerImage),
		fmt.Sprintf("-insecure=%t", r.SkipCertVerification),
		"-with-journal-logging=false",
		"-with-stdout-logging=true",
	}
	volumes := []corev1.Volume{{
		Name: "host",
		VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{
				Path: "/",
			},
		},
	}}

	volumeMounts := []corev1.VolumeMount{{Name: "host", MountPath: r.hostFSMountDir}}
	if r.ServiceCACertPath != "" {
		cliArgs = append(cliArgs, fmt.Sprintf("-cacert=%s", common.HostCACertPath))
		volumes = append(volumes, corev1.Volume{
			Name: "ca-cert",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: spokeReclaimCMName,
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "ca-cert",
			MountPath: filepath.Dir(common.HostCACertPath),
		})
	}

	name := fmt.Sprintf("%s-reclaim", strings.ReplaceAll(nodeName, ".", "-"))
	var privileged bool = true
	containers := []corev1.Container{{
		Name:            name,
		Image:           r.AgentContainerImage,
		Command:         []string{"next_step_runner"},
		Args:            cliArgs,
		SecurityContext: &corev1.SecurityContext{Privileged: &privileged},
		VolumeMounts:    volumeMounts,
		Env: []corev1.EnvVar{{
			Name: "PULL_SECRET_TOKEN",
			ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
				Key: "auth-token",
				LocalObjectReference: corev1.LocalObjectReference{
					Name: spokeReclaimSecretName(infraEnvID),
				},
			}},
		}},
	}}

	labels := map[string]string{"name": name}
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: spokeReclaimNamespaceName,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
			},
		},
	}

	mutate := func() error {
		daemonSet.ObjectMeta.OwnerReferences = []metav1.OwnerReference{{
			APIVersion: "v1",
			Kind:       "Node",
			Name:       node.Name,
			UID:        node.UID,
		}}
		daemonSet.Spec.Template.Spec.NodeSelector = map[string]string{"kubernetes.io/hostname": node.Name}
		daemonSet.Spec.Template.Spec.Volumes = volumes
		daemonSet.Spec.Template.Spec.Tolerations = []corev1.Toleration{{
			Operator: corev1.TolerationOpExists,
		}}
		daemonSet.Spec.Template.Spec.PriorityClassName = "system-node-critical"
		daemonSet.Spec.Template.Spec.ServiceAccountName = spokeRBACName
		daemonSet.Spec.Template.Spec.Containers = containers
		return nil
	}

	result, err := controllerutil.CreateOrUpdate(ctx, c, daemonSet, mutate)
	if result != controllerutil.OperationResultNone {
		log.Infof("DaemonSet %s/%s %s for agent reclaim", daemonSet.Namespace, daemonSet.Name, result)
	}
	return err
}
