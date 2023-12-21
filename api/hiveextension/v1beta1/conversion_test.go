package v1beta1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/api/hiveextension/v1beta2"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ConvertTo", func() {
	It("sets ObjectMeta correctly", func() {
		src := &AgentClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}

		dst := &v1beta2.AgentClusterInstall{}

		Expect(src.ConvertTo(dst)).To(Succeed())
		Expect(dst.Name).To(Equal("test"))
		Expect(dst.Namespace).To(Equal("default"))
	})

	It("copies unchanged fields correctly", func() {
		all := "all"
		src := &AgentClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: AgentClusterInstallSpec{
				ImageSetRef:            &hivev1.ClusterImageSetReference{Name: "clusterimageset"},
				ClusterDeploymentRef:   corev1.LocalObjectReference{Name: "cd"},
				ClusterMetadata:        &hivev1.ClusterMetadata{ClusterID: "asdf"},
				ManifestsConfigMapRef:  &corev1.LocalObjectReference{Name: "manifests"},
				ManifestsConfigMapRefs: []ManifestsConfigMapReference{{Name: "other-manifests"}},
				Networking: Networking{
					NetworkType:    "OVNKubernetes",
					MachineNetwork: []MachineNetworkEntry{{CIDR: "192.0.2.0/24"}, {CIDR: "198.51.100.0/24"}},
				},
				SSHPublicKey:          "sshkey",
				ProvisionRequirements: ProvisionRequirements{ControlPlaneAgents: 1},
				ControlPlane:          &AgentMachinePool{Name: "cp-pool"},
				Compute:               []AgentMachinePool{{Name: "compute1"}, {Name: "compute2"}},
				HoldInstallation:      true,
				IgnitionEndpoint:      &IgnitionEndpoint{Url: "ignition.example.com"},
				DiskEncryption:        &DiskEncryption{EnableOn: &all},
				Proxy:                 &Proxy{HTTPProxy: "proxy.example.com"},
				PlatformType:          NonePlatformType,
				ExternalPlatformSpec:  &ExternalPlatformSpec{PlatformName: "good-platform"},
				MastersSchedulable:    true,
			},
		}

		dst := &v1beta2.AgentClusterInstall{}
		Expect(src.ConvertTo(dst)).To(Succeed())

		Expect(dst.Spec.ImageSetRef).To(Equal(src.Spec.ImageSetRef))
		Expect(dst.Spec.ClusterDeploymentRef).To(Equal(src.Spec.ClusterDeploymentRef))
		Expect(dst.Spec.ClusterMetadata).To(Equal(src.Spec.ClusterMetadata))
		Expect(dst.Spec.ManifestsConfigMapRef).To(Equal(src.Spec.ManifestsConfigMapRef))
		Expect(dst.Spec.Networking.NetworkType).To(Equal(src.Spec.Networking.NetworkType))
		Expect(dst.Spec.Networking.MachineNetwork).To(HaveExactElements(v1beta2.MachineNetworkEntry{CIDR: "192.0.2.0/24"}, v1beta2.MachineNetworkEntry{CIDR: "198.51.100.0/24"}))
		Expect(dst.Spec.SSHPublicKey).To(Equal(src.Spec.SSHPublicKey))
		Expect(dst.Spec.ProvisionRequirements.ControlPlaneAgents).To(Equal(src.Spec.ProvisionRequirements.ControlPlaneAgents))
		Expect(dst.Spec.ControlPlane.Name).To(Equal(src.Spec.ControlPlane.Name))
		Expect(dst.Spec.Compute).To(HaveExactElements(v1beta2.AgentMachinePool{Name: "compute1"}, v1beta2.AgentMachinePool{Name: "compute2"}))
		Expect(dst.Spec.HoldInstallation).To(Equal(src.Spec.HoldInstallation))
		Expect(dst.Spec.IgnitionEndpoint.Url).To(Equal(src.Spec.IgnitionEndpoint.Url))
		Expect(dst.Spec.DiskEncryption.EnableOn).To(Equal(src.Spec.DiskEncryption.EnableOn))
		Expect(dst.Spec.Proxy.HTTPProxy).To(Equal(src.Spec.Proxy.HTTPProxy))
		Expect(string(dst.Spec.PlatformType)).To(Equal(string(src.Spec.PlatformType)))
		Expect(dst.Spec.ExternalPlatformSpec.PlatformName).To(Equal(src.Spec.ExternalPlatformSpec.PlatformName))
		Expect(dst.Spec.MastersSchedulable).To(Equal(src.Spec.MastersSchedulable))
	})

	It("translates singular VIPs to plural fields", func() {
		apiVIP := "192.0.2.1"
		ingressVIP := "192.0.2.2"
		src := &AgentClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: AgentClusterInstallSpec{
				APIVIP:     apiVIP,
				IngressVIP: ingressVIP,
			},
		}

		dst := &v1beta2.AgentClusterInstall{}

		Expect(src.ConvertTo(dst)).To(Succeed())
		Expect(dst.Spec.APIVIPs[0]).To(Equal(apiVIP))
		Expect(dst.Spec.IngressVIPs[0]).To(Equal(ingressVIP))
	})

	It("copies plural VIP fields", func() {
		apiVIPs := []string{"192.0.2.1", "2001:db8::1"}
		ingressVIPs := []string{"192.0.2.2", "2001:db8::2"}
		src := &AgentClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: AgentClusterInstallSpec{
				APIVIPs:     apiVIPs,
				IngressVIPs: ingressVIPs,
			},
		}

		dst := &v1beta2.AgentClusterInstall{}

		Expect(src.ConvertTo(dst)).To(Succeed())
		Expect(dst.Spec.APIVIPs).To(Equal(apiVIPs))
		Expect(dst.Spec.IngressVIPs).To(Equal(ingressVIPs))
	})

	It("uses the plural fields if both are present", func() {
		apiVIPs := []string{"192.0.2.1", "2001:db8::1"}
		ingressVIPs := []string{"192.0.2.2", "2001:db8::2"}
		src := &AgentClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: AgentClusterInstallSpec{
				APIVIP:      apiVIPs[0],
				IngressVIP:  ingressVIPs[0],
				APIVIPs:     apiVIPs,
				IngressVIPs: ingressVIPs,
			},
		}

		dst := &v1beta2.AgentClusterInstall{}

		Expect(src.ConvertTo(dst)).To(Succeed())
		Expect(dst.Spec.APIVIPs).To(Equal(apiVIPs))
		Expect(dst.Spec.IngressVIPs).To(Equal(ingressVIPs))
	})
})

var _ = Describe("ConvertFrom", func() {
	It("sets ObjectMeta correctly", func() {
		src := &v1beta2.AgentClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}

		dst := &AgentClusterInstall{}

		Expect(dst.ConvertFrom(src)).To(Succeed())
		Expect(dst.Name).To(Equal("test"))
		Expect(dst.Namespace).To(Equal("default"))
	})

	It("copies unchanged fields correctly", func() {
		all := "all"
		src := &v1beta2.AgentClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: v1beta2.AgentClusterInstallSpec{
				ImageSetRef:            &hivev1.ClusterImageSetReference{Name: "clusterimageset"},
				ClusterDeploymentRef:   corev1.LocalObjectReference{Name: "cd"},
				ClusterMetadata:        &hivev1.ClusterMetadata{ClusterID: "asdf"},
				ManifestsConfigMapRef:  &corev1.LocalObjectReference{Name: "manifests"},
				ManifestsConfigMapRefs: []v1beta2.ManifestsConfigMapReference{{Name: "other-manifests"}},
				Networking: v1beta2.Networking{
					NetworkType:    "OVNKubernetes",
					MachineNetwork: []v1beta2.MachineNetworkEntry{{CIDR: "192.0.2.0/24"}, {CIDR: "198.51.100.0/24"}},
				},
				SSHPublicKey:          "sshkey",
				ProvisionRequirements: v1beta2.ProvisionRequirements{ControlPlaneAgents: 1},
				ControlPlane:          &v1beta2.AgentMachinePool{Name: "cp-pool"},
				Compute:               []v1beta2.AgentMachinePool{{Name: "compute1"}, {Name: "compute2"}},
				HoldInstallation:      true,
				IgnitionEndpoint:      &v1beta2.IgnitionEndpoint{Url: "ignition.example.com"},
				DiskEncryption:        &v1beta2.DiskEncryption{EnableOn: &all},
				Proxy:                 &v1beta2.Proxy{HTTPProxy: "proxy.example.com"},
				PlatformType:          v1beta2.NonePlatformType,
				ExternalPlatformSpec:  &v1beta2.ExternalPlatformSpec{PlatformName: "good-platform"},
				MastersSchedulable:    true,
			},
		}

		dst := &AgentClusterInstall{}
		Expect(dst.ConvertFrom(src)).To(Succeed())

		Expect(dst.Spec.ImageSetRef).To(Equal(src.Spec.ImageSetRef))
		Expect(dst.Spec.ClusterDeploymentRef).To(Equal(src.Spec.ClusterDeploymentRef))
		Expect(dst.Spec.ClusterMetadata).To(Equal(src.Spec.ClusterMetadata))
		Expect(dst.Spec.ManifestsConfigMapRef).To(Equal(src.Spec.ManifestsConfigMapRef))
		Expect(dst.Spec.Networking.NetworkType).To(Equal(src.Spec.Networking.NetworkType))
		Expect(dst.Spec.Networking.MachineNetwork).To(HaveExactElements(MachineNetworkEntry{CIDR: "192.0.2.0/24"}, MachineNetworkEntry{CIDR: "198.51.100.0/24"}))
		Expect(dst.Spec.SSHPublicKey).To(Equal(src.Spec.SSHPublicKey))
		Expect(dst.Spec.ProvisionRequirements.ControlPlaneAgents).To(Equal(src.Spec.ProvisionRequirements.ControlPlaneAgents))
		Expect(dst.Spec.ControlPlane.Name).To(Equal(src.Spec.ControlPlane.Name))
		Expect(dst.Spec.Compute).To(HaveExactElements(AgentMachinePool{Name: "compute1"}, AgentMachinePool{Name: "compute2"}))
		Expect(dst.Spec.HoldInstallation).To(Equal(src.Spec.HoldInstallation))
		Expect(dst.Spec.IgnitionEndpoint.Url).To(Equal(src.Spec.IgnitionEndpoint.Url))
		Expect(dst.Spec.DiskEncryption.EnableOn).To(Equal(src.Spec.DiskEncryption.EnableOn))
		Expect(dst.Spec.Proxy.HTTPProxy).To(Equal(src.Spec.Proxy.HTTPProxy))
		Expect(string(dst.Spec.PlatformType)).To(Equal(string(src.Spec.PlatformType)))
		Expect(dst.Spec.ExternalPlatformSpec.PlatformName).To(Equal(src.Spec.ExternalPlatformSpec.PlatformName))
		Expect(dst.Spec.MastersSchedulable).To(Equal(src.Spec.MastersSchedulable))
	})

	It("copies plural VIP fields", func() {
		apiVIPs := []string{"192.0.2.1", "2001:db8::1"}
		ingressVIPs := []string{"192.0.2.2", "2001:db8::2"}
		src := &v1beta2.AgentClusterInstall{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: v1beta2.AgentClusterInstallSpec{
				APIVIPs:     apiVIPs,
				IngressVIPs: ingressVIPs,
			},
		}

		dst := &AgentClusterInstall{}

		Expect(dst.ConvertFrom(src)).To(Succeed())
		Expect(dst.Spec.APIVIPs).To(Equal(apiVIPs))
		Expect(dst.Spec.IngressVIPs).To(Equal(ingressVIPs))
	})
})
