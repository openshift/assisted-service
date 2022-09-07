package controllers

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	authzv1 "github.com/openshift/api/authorization/v1"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/pkg/auth"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
		schemes := runtime.NewScheme()
		Expect(scheme.AddToScheme(schemes)).To(Succeed())
		Expect(authzv1.AddToScheme(schemes)).To(Succeed())
		c = fakeclient.NewClientBuilder().WithScheme(schemes).Build()
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

		It("updates if the namespace already exists", func() {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: spokeReclaimNamespaceName}}
			Expect(c.Create(ctx, ns)).To(Succeed())

			Expect(ensureSpokeNamespace(ctx, c)).To(Succeed())

			key := types.NamespacedName{Name: spokeReclaimNamespaceName}
			Expect(c.Get(ctx, key, ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/enforce", "privileged"))
		})
	})

	Describe("ensureSpokeServiceAccount", func() {
		It("creates the serviceaccount", func() {
			Expect(ensureSpokeServiceAccount(ctx, c)).To(Succeed())

			key := types.NamespacedName{Name: spokeRBACName, Namespace: spokeReclaimNamespaceName}
			Expect(c.Get(ctx, key, &corev1.ServiceAccount{})).To(Succeed())
		})

		It("succeeds if the serviceaccount already exists", func() {
			sa := &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name:      spokeRBACName,
					Namespace: spokeReclaimNamespaceName,
				},
			}
			Expect(c.Create(ctx, sa)).To(Succeed())

			Expect(ensureSpokeServiceAccount(ctx, c)).To(Succeed())
		})
	})

	Describe("ensureSpokeRole", func() {
		It("creates the role", func() {
			Expect(ensureSpokeRole(ctx, c)).To(Succeed())

			key := types.NamespacedName{Name: spokeRBACName, Namespace: spokeReclaimNamespaceName}
			role := &authzv1.Role{}
			Expect(c.Get(ctx, key, role)).To(Succeed())

			rule := authzv1.PolicyRule{
				APIGroups:     []string{"security.openshift.io"},
				Resources:     []string{"securitycontextconstraints"},
				ResourceNames: []string{"privileged"},
				Verbs:         []string{"use"},
			}
			Expect(role.Rules).To(ConsistOf(rule))
		})

		It("succeeds if the role already exists", func() {
			role := &authzv1.Role{
				ObjectMeta: metav1.ObjectMeta{
					Name:      spokeRBACName,
					Namespace: spokeReclaimNamespaceName,
				},
			}
			Expect(c.Create(ctx, role)).To(Succeed())

			Expect(ensureSpokeRole(ctx, c)).To(Succeed())
		})
	})

	Describe("ensureSpokeRoleBinding", func() {
		It("creates the role binding", func() {
			Expect(ensureSpokeRoleBinding(ctx, c)).To(Succeed())

			key := types.NamespacedName{Name: spokeRBACName, Namespace: spokeReclaimNamespaceName}
			rb := &authzv1.RoleBinding{}
			Expect(c.Get(ctx, key, rb)).To(Succeed())

			Expect(rb.Subjects[0]).To(Equal(corev1.ObjectReference{
				Kind:      "ServiceAccount",
				Name:      spokeRBACName,
				Namespace: spokeReclaimNamespaceName,
			}))
			Expect(rb.RoleRef).To(Equal(corev1.ObjectReference{
				APIVersion: "rbac.authorization.k8s.io/v1",
				Kind:       "Role",
				Name:       spokeRBACName,
				Namespace:  spokeReclaimNamespaceName,
			}))
		})

		It("succeeds if the role binding already exists", func() {
			rb := &authzv1.RoleBinding{
				ObjectMeta: metav1.ObjectMeta{Name: spokeRBACName},
			}
			Expect(c.Create(ctx, rb)).To(Succeed())

			Expect(ensureSpokeRoleBinding(ctx, c)).To(Succeed())
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

	Describe("createNextStepRunnerDaemonSet", func() {
		var (
			nodeName      = "node.example.com"
			daemonSetName = "node.example.com-reclaim"
			infraEnvID    string
			hostID        string
			nodeUID       string
		)

		BeforeEach(func() {
			infraEnvID = uuid.New().String()
			hostID = uuid.New().String()
			nodeUID = uuid.New().String()
		})

		withANode := func(hostname string) {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: hostname,
					UID:  types.UID(nodeUID),
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeHostName, Address: hostname},
					},
				},
			}
			Expect(c.Create(ctx, node)).To(Succeed())
		}

		It("creates a daemon set correctly on the spoke node", func() {
			withANode(nodeName)
			Expect(reclaimer.createNextStepRunnerDaemonSet(ctx, c, nodeName, infraEnvID, hostID)).To(Succeed())

			ds := &appsv1.DaemonSet{}
			daemonSetNsName := types.NamespacedName{
				Name:      daemonSetName,
				Namespace: spokeReclaimNamespaceName,
			}
			Expect(c.Get(ctx, daemonSetNsName, ds)).To(Succeed())

			nodeOwnerRef := metav1.OwnerReference{
				APIVersion: "v1",
				Kind:       "Node",
				Name:       nodeName,
				UID:        types.UID(nodeUID),
			}
			Expect(ds.OwnerReferences).To(ContainElement(nodeOwnerRef))

			Expect(ds.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue("kubernetes.io/hostname", nodeName))
			Expect(ds.Spec.Template.Spec.ServiceAccountName).To(Equal(spokeRBACName))
			Expect(ds.Spec.Template.Spec.PriorityClassName).To(Equal("system-node-critical"))

			container := ds.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal(agentImage))
			Expect(container.SecurityContext.Privileged).To(HaveValue(Equal(true)))
			Expect(container.Command).To(Equal([]string{"next_step_runner"}))

			// no cacert should be provided by default
			Expect(container.Args).NotTo(ContainElement(ContainSubstring("-cacert")))
		})

		It("adds cert configuration when CA cert path is set", func() {
			withANode(nodeName)
			reclaimer.ServiceCACertPath = "/etc/assisted/cert.crt"
			Expect(reclaimer.createNextStepRunnerDaemonSet(ctx, c, nodeName, infraEnvID, hostID)).To(Succeed())

			ds := &appsv1.DaemonSet{}
			daemonSetNsName := types.NamespacedName{
				Name:      daemonSetName,
				Namespace: spokeReclaimNamespaceName,
			}
			Expect(c.Get(ctx, daemonSetNsName, ds)).To(Succeed())

			Expect(ds.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-cacert=/etc/assisted-service/service-ca-cert.crt"))
			foundVolume := false
			for _, vol := range ds.Spec.Template.Spec.Volumes {
				if vol.Name == "ca-cert" && vol.VolumeSource.ConfigMap.LocalObjectReference.Name == spokeReclaimCMName {
					foundVolume = true
				}
			}
			Expect(foundVolume).To(BeTrue(), "Pod should have ca-cert volume")
			foundMount := false
			for _, mount := range ds.Spec.Template.Spec.Containers[0].VolumeMounts {
				if mount.Name == "ca-cert" && mount.MountPath == "/etc/assisted-service" {
					foundMount = true
				}
			}
			Expect(foundMount).To(BeTrue(), "Pod should have ca-cert volume mount")
		})

		It("fails when the node doesn't exist", func() {
			Expect(reclaimer.createNextStepRunnerDaemonSet(ctx, c, nodeName, infraEnvID, hostID)).ToNot(Succeed())
		})
	})
})
