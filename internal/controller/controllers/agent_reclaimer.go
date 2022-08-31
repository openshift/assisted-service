package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	spokeReclaimNamespaceName = "assisted-installer"
	spokeReclaimCMName        = "reclaim-config"
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

func (r *agentReclaimer) createNextStepRunnerPod(ctx context.Context, c client.Client, nodeName string, infraEnvID string, hostID string) error {
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

	podName := fmt.Sprintf("%s-reclaim", nodeName)
	var privileged bool = true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: spokeReclaimNamespaceName,
		},
		Spec: corev1.PodSpec{
			NodeName: node.Name,
			Volumes:  volumes,
			Containers: []corev1.Container{{
				Name:            podName,
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
		},
	}

	return c.Create(ctx, pod)
}
