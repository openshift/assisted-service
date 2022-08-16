package controllers

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/pkg/auth"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = It("newAgentReclaimer pulls config from env vars", func() {
	image := "registry.example.com/agent:latest"
	authType := auth.TypeLocal
	serviceURL := "https://assisted.example.com"
	certPath := "/etc/some/path/cert.crt"
	skipVerify := "true"

	os.Setenv("AGENT_DOCKER_IMAGE", image)
	os.Setenv("AUTH_TYPE", string(authType))
	os.Setenv("SERVICE_BASE_URL", serviceURL)
	os.Setenv("SERVICE_CA_CERT_PATH", certPath)
	os.Setenv("SKIP_CERT_VERIFICATION", skipVerify)

	r, err := newAgentReclaimer("/host")
	Expect(err).NotTo(HaveOccurred())
	Expect(r.AgentContainerImage).To(Equal(image))
	Expect(r.AuthType).To(Equal(authType))
	Expect(r.ServiceBaseURL).To(Equal(serviceURL))
	Expect(r.ServiceCACertPath).To(Equal(certPath))
	Expect(r.SkipCertVerification).To(BeTrue())
})

var _ = Context("with a fake client", func() {
	var (
		c               client.Client
		ctx             = context.Background()
		agentImage      = "registry.example.com/assisted-installer/agent:latest"
		assistedBaseURL = "https://assisted.example.com"
		reclaimer       *agentReclaimer
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		reclaimer = &agentReclaimer{reclaimConfig{
			AgentContainerImage: agentImage,
			ServiceBaseURL:      assistedBaseURL,
		}}
	})

	Describe("ensureSpokeNamespace", func() {
		It("creates the namespace", func() {
			Expect(ensureSpokeNamespace(ctx, c)).To(Succeed())

			key := types.NamespacedName{Name: spokeReclaimNamespaceName}
			Expect(c.Get(ctx, key, &corev1.Namespace{})).To(Succeed())
		})

		It("succeeds if the namespace already exists", func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: spokeReclaimNamespaceName}}
			Expect(c.Create(ctx, ns)).To(Succeed())

			Expect(ensureSpokeNamespace(ctx, c)).To(Succeed())
		})
	})

	Describe("ensureSpokeAgentSecret", func() {
		var infraEnvID string
		BeforeEach(func() {
			infraEnvID = uuid.New().String()
		})

		It("creates the secret with an empty value with none auth", func() {
			reclaimer.AuthType = auth.TypeNone
			Expect(reclaimer.ensureSpokeAgentSecret(ctx, c, infraEnvID)).To(Succeed())

			key := types.NamespacedName{
				Name:      fmt.Sprintf("reclaim-%s-token", infraEnvID),
				Namespace: spokeReclaimNamespaceName,
			}
			secret := &corev1.Secret{}
			Expect(c.Get(ctx, key, secret)).To(Succeed())
			tok, ok := secret.Data["auth-token"]
			Expect(ok).To(BeTrue(), "secret should have auth-token key")
			Expect(tok).To(BeEmpty())
		})

		It("creates a token with local auth", func() {
			_, priv, err := gencrypto.ECDSAKeyPairPEM()
			Expect(err).NotTo(HaveOccurred())
			os.Setenv("EC_PRIVATE_KEY_PEM", priv)
			defer os.Unsetenv("EC_PROVATE_KEY_PEM")

			reclaimer.AuthType = auth.TypeLocal
			Expect(reclaimer.ensureSpokeAgentSecret(ctx, c, infraEnvID)).To(Succeed())

			key := types.NamespacedName{
				Name:      fmt.Sprintf("reclaim-%s-token", infraEnvID),
				Namespace: spokeReclaimNamespaceName,
			}
			secret := &corev1.Secret{}
			Expect(c.Get(ctx, key, secret)).To(Succeed())
			tok, ok := secret.Data["auth-token"]
			Expect(ok).To(BeTrue(), "secret should have auth-token key")
			Expect(tok).NotTo(BeEmpty())
		})

		It("succeeds if the secret already exists", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("reclaim-%s-token", infraEnvID),
					Namespace: spokeReclaimNamespaceName,
				},
				Type: corev1.SecretTypeOpaque,
				Data: map[string][]byte{"auth-token": []byte("")},
			}
			Expect(c.Create(ctx, secret)).To(Succeed())

			reclaimer.AuthType = auth.TypeNone
			Expect(reclaimer.ensureSpokeAgentSecret(ctx, c, infraEnvID)).To(Succeed())
		})

		It("creates a second secret if one exists for a different infra-env", func() {
			reclaimer.AuthType = auth.TypeNone
			Expect(reclaimer.ensureSpokeAgentSecret(ctx, c, infraEnvID)).To(Succeed())

			otherInfraEnvID := uuid.New().String()
			Expect(reclaimer.ensureSpokeAgentSecret(ctx, c, otherInfraEnvID)).To(Succeed())

			key := types.NamespacedName{
				Name:      fmt.Sprintf("reclaim-%s-token", infraEnvID),
				Namespace: spokeReclaimNamespaceName,
			}
			Expect(c.Get(ctx, key, &corev1.Secret{})).To(Succeed())
			key.Name = fmt.Sprintf("reclaim-%s-token", otherInfraEnvID)
			Expect(c.Get(ctx, key, &corev1.Secret{})).To(Succeed())
		})
	})

	Describe("ensureSpokeAgentCertCM", func() {
		It("creates the configmap if a cert path is set", func() {
			certFile, err := os.CreateTemp("", "reclaim-cert-file")
			Expect(err).NotTo(HaveOccurred())
			fileName := certFile.Name()
			defer os.Remove(fileName)

			content := "some cert content here"
			_, err = certFile.WriteString(content)
			Expect(err).NotTo(HaveOccurred())
			Expect(certFile.Sync()).To(Succeed())

			reclaimer.ServiceCACertPath = fileName
			Expect(reclaimer.ensureSpokeAgentCertCM(ctx, c)).To(Succeed())

			key := types.NamespacedName{
				Name:      spokeReclaimCMName,
				Namespace: spokeReclaimNamespaceName,
			}
			cm := &corev1.ConfigMap{}
			Expect(c.Get(ctx, key, cm)).To(Succeed())
			cmContent, ok := cm.Data["service-ca-cert.crt"]
			Expect(ok).To(BeTrue(), "config map should include CA cert file key")
			Expect(cmContent).To(Equal(content))
		})

		It("does not create a configmap when no cert path is set", func() {
			Expect(reclaimer.ensureSpokeAgentCertCM(ctx, c)).To(Succeed())
			key := types.NamespacedName{
				Name:      spokeReclaimCMName,
				Namespace: spokeReclaimNamespaceName,
			}
			err := c.Get(ctx, key, &corev1.ConfigMap{})
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})

		It("succeeds when the configmap already exists", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      spokeReclaimCMName,
					Namespace: spokeReclaimNamespaceName,
				},
				Data: map[string]string{"service-ca-cert.crt": "some cert data"},
			}
			Expect(c.Create(ctx, cm)).To(Succeed())

			Expect(reclaimer.ensureSpokeAgentCertCM(ctx, c)).To(Succeed())
		})
	})

	Describe("createNextStepRunnerPod", func() {
		var (
			nodeName   = "node.example.com"
			podName    = "node.example.com-reclaim"
			infraEnvID string
			hostID     string
		)

		BeforeEach(func() {
			infraEnvID = uuid.New().String()
			hostID = uuid.New().String()
		})

		withANode := func(hostname string) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: hostname},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeHostName, Address: hostname},
					},
				},
			}
			Expect(c.Create(ctx, node)).To(Succeed())
		}

		It("creates a pod correctly on the spoke node", func() {
			withANode(nodeName)
			Expect(reclaimer.createNextStepRunnerPod(ctx, c, nodeName, infraEnvID, hostID)).To(Succeed())

			pod := &corev1.Pod{}
			podNsName := types.NamespacedName{
				Name:      podName,
				Namespace: spokeReclaimNamespaceName,
			}
			Expect(c.Get(ctx, podNsName, pod)).To(Succeed())

			Expect(pod.Spec.NodeName).To(Equal(nodeName))
			container := pod.Spec.Containers[0]
			Expect(container.Image).To(Equal(agentImage))
			Expect(container.SecurityContext.Privileged).To(HaveValue(Equal(true)))
			Expect(container.Command).To(Equal([]string{"next_step_runner"}))

			// no cacert should be provided by default
			Expect(container.Args).NotTo(ContainElement(ContainSubstring("-cacert")))
		})

		It("adds cert configuration when CA cert path is set", func() {
			withANode(nodeName)
			reclaimer.ServiceCACertPath = "/etc/assisted/cert.crt"
			Expect(reclaimer.createNextStepRunnerPod(ctx, c, nodeName, infraEnvID, hostID)).To(Succeed())

			pod := &corev1.Pod{}
			podNsName := types.NamespacedName{
				Name:      podName,
				Namespace: spokeReclaimNamespaceName,
			}
			Expect(c.Get(ctx, podNsName, pod)).To(Succeed())

			Expect(pod.Spec.Containers[0].Args).To(ContainElement("-cacert=/etc/assisted-service/service-ca-cert.crt"))
			foundVolume := false
			for _, vol := range pod.Spec.Volumes {
				if vol.Name == "ca-cert" && vol.VolumeSource.ConfigMap.LocalObjectReference.Name == spokeReclaimCMName {
					foundVolume = true
				}
			}
			Expect(foundVolume).To(BeTrue(), "Pod should have ca-cert volume")
			foundMount := false
			for _, mount := range pod.Spec.Containers[0].VolumeMounts {
				if mount.Name == "ca-cert" && mount.MountPath == "/etc/assisted-service" {
					foundMount = true
				}
			}
			Expect(foundMount).To(BeTrue(), "Pod should have ca-cert volume mount")
		})

		It("fails when the node doesn't exist", func() {
			Expect(reclaimer.createNextStepRunnerPod(ctx, c, nodeName, infraEnvID, hostID)).ToNot(Succeed())
		})
	})
})
