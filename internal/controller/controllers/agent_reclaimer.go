package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kelseyhightower/envconfig"
	authzv1 "github.com/openshift/api/authorization/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	spokeReclaimNamespaceName = "assisted-installer"
	spokeReclaimCMName        = "reclaim-config"
	spokeReclaimSAName        = "privileged-sa"
	spokeReclaimCRBName       = "assisted-installer-privileged"
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

func ensureSpokeNamespace(ctx context.Context, c client.Client) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: spokeReclaimNamespaceName}}
	if err := c.Get(ctx, types.NamespacedName{Name: ns.Name}, ns); err != nil {
		if !k8serrors.IsNotFound(err) {
			return errors.Wrapf(err, "failed to get namespace %s", spokeReclaimNamespaceName)
		}
		if err := c.Create(ctx, ns); err != nil {
			return errors.Wrapf(err, "failed to create namespace %s", spokeReclaimNamespaceName)
		}
	}

	return nil
}

func ensureSpokeServiceAccount(ctx context.Context, c client.Client) error {
	key := types.NamespacedName{
		Name:      spokeReclaimSAName,
		Namespace: spokeReclaimNamespaceName,
	}

	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}}
	if err := c.Get(ctx, key, sa); err != nil {
		if !k8serrors.IsNotFound(err) {
			return errors.Wrapf(err, "failed to get ServiceAccount %s", spokeReclaimSAName)
		}
		if err := c.Create(ctx, sa); err != nil {
			return errors.Wrapf(err, "failed to create ServiceAccount %s", spokeReclaimSAName)
		}
	}

	return nil
}

func ensureSpokeClusterRoleBinding(ctx context.Context, c client.Client) error {
	crb := &authzv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: spokeReclaimCRBName}}
	if err := c.Get(ctx, types.NamespacedName{Name: crb.Name}, crb); err != nil {
		if !k8serrors.IsNotFound(err) {
			return errors.Wrapf(err, "failed to get ClusterRoleBinding %s", spokeReclaimCRBName)
		}
		crb.RoleRef = corev1.ObjectReference{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
			Name:       "system:openshift:scc:privileged",
		}
		crb.Subjects = []corev1.ObjectReference{{
			Kind:      "ServiceAccount",
			Name:      spokeReclaimSAName,
			Namespace: spokeReclaimNamespaceName,
		}}
		if err := c.Create(ctx, crb); err != nil {
			return errors.Wrapf(err, "failed to create ClusterRoleBinding %s", spokeReclaimCRBName)
		}
	}

	return nil
}

func spokeReclaimSecretName(infraEnvID string) string {
	return fmt.Sprintf("reclaim-%s-token", infraEnvID)
}

func (r *agentReclaimer) ensureSpokeAgentSecret(ctx context.Context, c client.Client, infraEnvID string) error {
	authToken := ""
	if r.AuthType == auth.TypeLocal {
		var err error
		authToken, err = gencrypto.LocalJWT(infraEnvID, gencrypto.InfraEnvKey)
		if err != nil {
			return errors.Wrapf(err, "failed to create local JWT for infraEnv %s", infraEnvID)
		}
	}

	key := types.NamespacedName{
		Name:      spokeReclaimSecretName(infraEnvID),
		Namespace: spokeReclaimNamespaceName,
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
		Type: corev1.SecretTypeOpaque,
	}
	err := c.Get(ctx, key, secret)
	if err != nil && !k8serrors.IsNotFound(err) {
		return errors.Wrapf(err, "failed to get secret %s/%s", key.Namespace, key.Name)
	}

	if k8serrors.IsNotFound(err) {
		secret.Data = map[string][]byte{"auth-token": []byte(authToken)}
		if err := c.Create(ctx, secret); err != nil {
			return errors.Wrapf(err, "failed to create secret %s", spokeReclaimNamespaceName)
		}
	}

	return nil
}

func (r *agentReclaimer) ensureSpokeAgentCertCM(ctx context.Context, c client.Client) error {
	if r.ServiceCACertPath == "" {
		return nil
	}

	key := types.NamespacedName{
		Name:      spokeReclaimCMName,
		Namespace: spokeReclaimNamespaceName,
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		},
	}

	err := c.Get(ctx, key, cm)
	if err != nil && !k8serrors.IsNotFound(err) {
		return errors.Wrapf(err, "failed to get secret %s/%s", key.Namespace, key.Name)
	}

	if k8serrors.IsNotFound(err) {
		data, err := os.ReadFile(r.ServiceCACertPath)
		if err != nil {
			return errors.Wrap(err, "failed to read service ca cert")
		}
		cm.Data = map[string]string{filepath.Base(common.HostCACertPath): string(data)}
		if err := c.Create(ctx, cm); err != nil {
			return errors.Wrapf(err, "failed to create secret %s", spokeReclaimNamespaceName)
		}
	}

	return nil
}

func (r *agentReclaimer) createNextStepRunnerDaemonSet(ctx context.Context, c client.Client, nodeName string, infraEnvID string, hostID string) error {
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

	name := fmt.Sprintf("%s-reclaim", nodeName)
	var privileged bool = true
	labels := map[string]string{"name": name}

	podSpec := corev1.PodSpec{
		NodeSelector: map[string]string{"kubernetes.io/hostname": node.Name},
		Volumes:      volumes,
		Tolerations: []corev1.Toleration{{
			Operator: corev1.TolerationOpExists,
		}},
		PriorityClassName:  "system-node-critical",
		ServiceAccountName: spokeReclaimSAName,
		Containers: []corev1.Container{{
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
		}},
	}

	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: spokeReclaimNamespaceName,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1",
				Kind:       "Node",
				Name:       node.Name,
				UID:        node.UID,
			}},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec:       podSpec,
			},
		},
	}

	return c.Create(ctx, daemonSet)
}
