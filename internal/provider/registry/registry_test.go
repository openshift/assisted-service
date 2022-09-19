package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/provider/ovirt"
	"github.com/openshift/assisted-service/internal/provider/vsphere"
	"github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	ovirtclient "github.com/ovirt/go-ovirt-client"
	ovirtclientlog "github.com/ovirt/go-ovirt-client-log/v2"
	"github.com/sirupsen/logrus"
)

var (
	providerRegistry ProviderRegistry
	ctrl             *gomock.Controller
)

const invalidInventory = "{\"system_vendor\": \"invalid\"}"

const masterMachineManifestTemplate = `
apiVersion: machine.openshift.io/v1beta1
kind: Machine
metadata:
  creationTimestamp: null
  labels:
    machine.openshift.io/cluster-api-cluster: {{ .CLUSTER_NAME }}-xxxxx
    machine.openshift.io/cluster-api-machine-role: master
    machine.openshift.io/cluster-api-machine-type: master
  name: {{ .VM_NAME }}
  namespace: openshift-machine-api
spec:
  metadata: {}
  providerSpec:
    value:
      apiVersion: ovirtproviderconfig.machine.openshift.io/v1beta1
	  cluster_id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
      cpu:
        cores: 4
        sockets: 1
        threads: 1
      credentialsSecret:
        name: credentials
      id: ""
      kind: OvirtMachineProviderSpec
      memory_mb: 16348
      metadata:
        creationTimestamp: null
      name: ""
      os_disk:
        size_gb: 120
      template_name: {{ .CLUSTER_NAME }}-xxxxx
      type: high_performance
      userDataSecret:
        name: master-user-data
status: {}
`
const machineSetManifestTemplate = `
apiVersion: machine.openshift.io/v1beta1
kind: MachineSet
metadata:
  creationTimestamp: null
  labels:
    machine.openshift.io/cluster-api-cluster: {{ .CLUSTER_NAME }}-xxxxx
    machine.openshift.io/cluster-api-machine-role: worker
    machine.openshift.io/cluster-api-machine-type: worker
  name: {{ .CLUSTER_NAME }}-xxxxx-worker-0
  namespace: openshift-machine-api
spec:
  replicas: 2
  selector:
    matchLabels:
      machine.openshift.io/cluster-api-cluster: {{ .CLUSTER_NAME }}-xxxxx
      machine.openshift.io/cluster-api-machineset: {{ .CLUSTER_NAME }}-xxxxx-worker-0
  template:
    metadata:
      labels:
        machine.openshift.io/cluster-api-cluster: {{ .CLUSTER_NAME }}-xxxxx
        machine.openshift.io/cluster-api-machine-role: worker
        machine.openshift.io/cluster-api-machine-type: worker
        machine.openshift.io/cluster-api-machineset: {{ .CLUSTER_NAME }}-xxxxx-worker-0
    spec:
      metadata: {}
      providerSpec:
        value:
          apiVersion: ovirtproviderconfig.machine.openshift.io/v1beta1
          cluster_id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
          cpu:
            cores: 4
            sockets: 1
            threads: 1
          credentialsSecret:
            name: ovirt-credentials
          id: ""
          kind: OvirtMachineProviderSpec
          memory_mb: 16348
          metadata:
            creationTimestamp: null
          name: ""
          os_disk:
            size_gb: 120
          template_name: {{ .CLUSTER_NAME }}-xxxxx-rhcos
          type: server
          userDataSecret:
            name: worker-user-data
status:
  replicas: 0
`

var _ = Describe("Test GetSupportedProvidersByHosts", func() {
	bmInventory := getBaremetalInventoryStr("hostname0", "bootMode", true, false)
	vsphereInventory := getVsphereInventoryStr("hostname0", "bootMode", true, false)
	ovirtInventory := getOvirtInventoryStr("hostname0", "bootMode", true, false)
	BeforeEach(func() {
		providerRegistry = InitProviderRegistry(common.GetTestLog())
		ctrl = gomock.NewController(GinkgoT())
	})
	It("no hosts", func() {
		hosts := make([]*models.Host, 0)
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(platforms).To(BeEmpty())
	})
	It("5 baremetal hosts - 3 masters, 2 workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(2))
		Expect(platforms).Should(ContainElements(models.PlatformTypeBaremetal, models.PlatformTypeNone))
	})
	It("single vsphere host", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(3))
		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeVsphere, models.PlatformTypeNone}
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})
	It("5 vsphere hosts - 3 masters, 2 workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, vsphereInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(3))
		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeVsphere, models.PlatformTypeNone}
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})
	It("2 vsphere hosts 1 generic host", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(2))
		Expect(platforms).Should(ContainElements(models.PlatformTypeBaremetal, models.PlatformTypeNone))
	})
	It("3 vsphere masters 2 generic workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(2))
		Expect(platforms).Should(ContainElements(models.PlatformTypeBaremetal, models.PlatformTypeNone))
	})
	It("single ovirt host", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(3))
		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeOvirt, models.PlatformTypeNone}
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})
	It("5 ovirt hosts - 3 masters, 2 workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, ovirtInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(3))
		supportedPlatforms := []models.PlatformType{models.PlatformTypeBaremetal, models.PlatformTypeOvirt, models.PlatformTypeNone}
		Expect(platforms).Should(ContainElements(supportedPlatforms))
	})
	It("2 ovirt hosts 1 generic host", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(2))
		Expect(platforms).Should(ContainElements(models.PlatformTypeBaremetal, models.PlatformTypeNone))
	})
	It("3 ovirt masters 2 generic workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		hosts = append(hosts, createHost(false, models.HostStatusKnown, bmInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).To(BeNil())
		Expect(len(platforms)).Should(Equal(2))
		Expect(platforms).Should(ContainElements(models.PlatformTypeBaremetal, models.PlatformTypeNone))
	})
	It("host with an invalid inventory", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(true, models.HostStatusKnown, invalidInventory))
		platforms, err := providerRegistry.GetSupportedProvidersByHosts(hosts)
		Expect(err).ToNot(BeNil())
		Expect(len(platforms)).Should(Equal(0))
	})
})

var _ = Describe("Test AddPlatformToInstallConfig", func() {
	BeforeEach(func() {
		providerRegistry = InitProviderRegistry(common.GetTestLog())
		ctrl = gomock.NewController(GinkgoT())
	})
	Context("Unregistered Provider", func() {
		It("try to add an unregistered platform to install config", func() {
			dummyProvider := models.PlatformType("dummy")
			err := providerRegistry.AddPlatformToInstallConfig(dummyProvider, nil, nil)
			Expect(err).ToNot(BeNil())
		})
	})
	Context("baremetal", func() {
		It("test with openshift greater then 4.7", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname0", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname1", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname2", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname3", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Cluster.OpenshiftVersion = "4.8"
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeBaremetal, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Baremetal).ToNot(BeNil())
			Expect(cfg.Platform.Baremetal.APIVIP).To(Equal(cluster.Cluster.APIVip))
			Expect(cfg.Platform.Baremetal.IngressVIP).To(Equal(cluster.Cluster.IngressVip))
			Expect(cfg.Platform.Baremetal.ProvisioningNetwork).To(Equal("Disabled"))
			Expect(len(cfg.Platform.Baremetal.Hosts)).To(Equal(len(cluster.Cluster.Hosts)))
			Expect(cfg.Platform.Baremetal.Hosts[0].Name).Should(Equal("hostname0"))
			Expect(cfg.Platform.Baremetal.Hosts[1].Name).Should(Equal("hostname1"))
			Expect(cfg.Platform.Baremetal.Hosts[2].Name).Should(Equal("hostname2"))
		})
		It("test with openshift version less 4.7", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname0", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname1", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getBaremetalInventoryStr("hostname2", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname3", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getBaremetalInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Cluster.OpenshiftVersion = "4.6"
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeBaremetal, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Baremetal).ToNot(BeNil())
			Expect(cfg.Platform.Baremetal.APIVIP).To(Equal(cluster.Cluster.APIVip))
			Expect(cfg.Platform.Baremetal.IngressVIP).To(Equal(cluster.Cluster.IngressVip))
			Expect(cfg.Platform.Baremetal.ProvisioningNetwork).To(Equal("Unmanaged"))
			Expect(len(cfg.Platform.Baremetal.Hosts)).To(Equal(len(cluster.Cluster.Hosts)))
			Expect(cfg.Platform.Baremetal.Hosts[0].Name).Should(Equal("hostname0"))
			Expect(cfg.Platform.Baremetal.Hosts[1].Name).Should(Equal("hostname1"))
			Expect(cfg.Platform.Baremetal.Hosts[2].Name).Should(Equal("hostname2"))
		})
		Context("vsphere", func() {
			It("with cluster params", func() {
				cfg := getInstallerConfigBaremetal()
				hosts := make([]*models.Host, 0)
				hosts = append(hosts, createHost(true, models.HostStatusKnown, getVsphereInventoryStr("hostname0", "bootMode", true, false)))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, getVsphereInventoryStr("hostname1", "bootMode", true, false)))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, getVsphereInventoryStr("hostname2", "bootMode", true, false)))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, getVsphereInventoryStr("hostname3", "bootMode", true, false)))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, getVsphereInventoryStr("hostname4", "bootMode", true, false)))
				cluster := createClusterFromHosts(hosts)
				cluster.Platform = createVspherePlatformParams()
				err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeVsphere, &cfg, &cluster)
				Expect(err).To(BeNil())
				Expect(cfg.Platform.Vsphere).ToNot(BeNil())
				Expect(cfg.Platform.Vsphere.APIVIP).To(Equal(cluster.Cluster.APIVip))
				Expect(cfg.Platform.Vsphere.IngressVIP).To(Equal(cluster.Cluster.IngressVip))
				Expect(cfg.Platform.Vsphere.VCenter).To(Equal(vsphere.PhVcenter))
			})
			It("without cluster params", func() {
				cfg := getInstallerConfigBaremetal()
				hosts := make([]*models.Host, 0)
				vsphereInventory := getVsphereInventoryStr("hostname0", "bootMode", true, false)
				hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(true, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, vsphereInventory))
				hosts = append(hosts, createHost(false, models.HostStatusKnown, getVsphereInventoryStr("hostname4", "bootMode", true, false)))
				cluster := createClusterFromHosts(hosts)
				cluster.Platform = createVspherePlatformParams()
				err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeVsphere, &cfg, &cluster)
				Expect(err).To(BeNil())
				Expect(cfg.Platform.Vsphere).ToNot(BeNil())
				Expect(cfg.Platform.Vsphere.APIVIP).To(Equal(cluster.Cluster.APIVip))
				Expect(cfg.Platform.Vsphere.IngressVIP).To(Equal(cluster.Cluster.IngressVip))
				Expect(cfg.Platform.Vsphere.VCenter).To(Equal(vsphere.PhVcenter))
			})
		})
	})
	Context("ovirt", func() {
		It("with cluster params", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getOvirtInventoryStr("hostname0", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getOvirtInventoryStr("hostname1", "bootMode", true, false)))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, getOvirtInventoryStr("hostname2", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getOvirtInventoryStr("hostname3", "bootMode", true, false)))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getOvirtInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Platform = createOvirtPlatformParams()
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeOvirt, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Ovirt).ToNot(BeNil())
			Expect(cfg.Platform.Ovirt.APIVIP).To(Equal(cluster.Cluster.APIVip))
			Expect(cfg.Platform.Ovirt.IngressVIP).To(Equal(cluster.Cluster.IngressVip))
			Expect(cfg.Platform.Ovirt.ClusterID.String()).To(Equal(ovirt.PhOvirtClusterID))
			Expect(cfg.Platform.Ovirt.StorageDomainID.String()).To(Equal(ovirt.PhStorageDomainID))
			Expect(cfg.Platform.Ovirt.NetworkName).To(Equal(ovirt.PhNetworkName))
			Expect(cfg.Platform.Ovirt.VnicProfileID.String()).To(Equal(ovirt.PhVnicProfileID))
		})
		It("without cluster params", func() {
			cfg := getInstallerConfigBaremetal()
			hosts := make([]*models.Host, 0)
			ovirtInventory := getOvirtInventoryStr("hostname0", "bootMode", true, false)
			hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
			hosts = append(hosts, createHost(true, models.HostStatusKnown, ovirtInventory))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, ovirtInventory))
			hosts = append(hosts, createHost(false, models.HostStatusKnown, getOvirtInventoryStr("hostname4", "bootMode", true, false)))
			cluster := createClusterFromHosts(hosts)
			cluster.Platform = createOvirtPlatformParams()
			err := providerRegistry.AddPlatformToInstallConfig(models.PlatformTypeOvirt, &cfg, &cluster)
			Expect(err).To(BeNil())
			Expect(cfg.Platform.Ovirt).NotTo(BeNil())
		})
	})
})

var _ = Describe("Test SetPlatformUsages", func() {
	var (
		usageApi *usage.MockAPI
	)
	BeforeEach(func() {
		providerRegistry = InitProviderRegistry(common.GetTestLog())
		ctrl = gomock.NewController(GinkgoT())
		usageApi = usage.NewMockAPI(ctrl)
	})
	Context("Unregistered Provider", func() {
		It("try to with an unregistered provider", func() {
			dummyProvider := models.PlatformType("dummy")
			err := providerRegistry.SetPlatformUsages(dummyProvider, nil, usageApi)
			Expect(err).ToNot(BeNil())
		})
	})
	Context("baremetal", func() {
		It("success", func() {
			usageApi.EXPECT().Remove(gomock.Any(), gomock.Any()).AnyTimes()
			err := providerRegistry.SetPlatformUsages(models.PlatformTypeBaremetal, nil, usageApi)
			Expect(err).To(BeNil())
		})
	})
	Context("vsphere", func() {
		It("success", func() {
			usageApi.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			err := providerRegistry.SetPlatformUsages(models.PlatformTypeVsphere, nil, usageApi)
			Expect(err).To(BeNil())
		})
	})
	Context("ovirt", func() {
		It("success", func() {
			usageApi.EXPECT().Add(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			err := providerRegistry.SetPlatformUsages(models.PlatformTypeOvirt, nil, usageApi)
			Expect(err).To(BeNil())
		})
	})
})

var _ = Describe("Test Hooks", func() {
	var (
		vm      ovirtclient.VM
		workDir string
		envVars []string
		err     error
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		envVars = make([]string, 0)
		workDir, err = os.MkdirTemp("", "test-assisted-installer-hooks")
		Expect(err).To(BeNil())
		err = os.Mkdir(filepath.Join(workDir, "openshift"), 0755)
		Expect(err).To(BeNil())
	})
	AfterEach(func() {
		err = os.RemoveAll(workDir)
		Expect(err).To(BeNil())
	})
	Context("ovirt", func() {
		ovirtHelper := ovirtclient.NewTestHelperFromEnv(ovirtclientlog.NewNOOPLogger())
		Expect(ovirtHelper).NotTo(BeNil())
		ovirtClient := ovirtHelper.GetClient()
		ovirtClusterID := ovirtHelper.GetClusterID()
		ovirtTemplateID := ovirtHelper.GetBlankTemplateID()
		ovirtOptVMParams := ovirtclient.CreateVMParams()

		providerRegistry := NewProviderRegistry()
		ovirtProvider := ovirt.NewOvirtProvider(logrus.New(), ovirtClient)
		providerRegistry.Register(ovirtProvider)

		hosts := make([]*models.Host, 0)
		for i := 0; i <= 5; i++ {
			hostname := fmt.Sprintf("hostname%d", i)
			vm, err = ovirtClient.CreateVM(ovirtClusterID, ovirtTemplateID, hostname, ovirtOptVMParams)
			Expect(err).To(BeNil())
			hosts = append(hosts, createHostWithID(vm.ID(), i < 3, models.HostStatusKnown, getOvirtInventoryStr(hostname, "bootMode", true, false)))
			Expect(err).To(BeNil())
		}
		cluster := createClusterFromHosts(hosts)
		cluster.Platform = createOvirtPlatformParams()

		It("ovirt PostCreateManifestsHook success", func() {
			createMasterMachineManifests(workDir, "99", &cluster)
			createMachineSetManifest(workDir, "99", &cluster)
			err = providerRegistry.PostCreateManifestsHook(&cluster, &envVars, workDir)
			Expect(err.Error()).Should(Equal("ovirt platform connection params not set"))

		})
		It("ovirt PostCreateManifestsHook failure", func() {
			createMasterMachineManifests(workDir, "50", &cluster)
			createMasterMachineManifests(workDir, "99", &cluster)
			err = providerRegistry.PostCreateManifestsHook(&cluster, &envVars, workDir)
			Expect(err).To(HaveOccurred())
		})

	})
})

func createMasterMachineManifests(workDir, filePrefix string, cluster *common.Cluster) {
	tmpl, err := template.New("").Parse(masterMachineManifestTemplate)
	baseDir := filepath.Join(workDir, "openshift")
	Expect(err).To(BeNil())
	for i := 0; i < 3; i++ {
		fileName := fmt.Sprintf(strings.Replace(ovirt.MachineManifestFileNameGlobStrFmt, "*", filePrefix, -1), i)
		filePath := filepath.Join(baseDir, fileName)
		vmName := fmt.Sprintf("%s-xxxxx-master-%d", cluster.Name, i)
		manifestParams := map[string]interface{}{
			"VM_NAME":      vmName,
			"CLUSTER_NAME": cluster.Name,
		}
		buf := &bytes.Buffer{}
		err = tmpl.Execute(buf, manifestParams)
		Expect(err).To(BeNil())
		err = os.WriteFile(filePath, buf.Bytes(), 0600)
		Expect(err).To(BeNil())
	}
}

func createMachineSetManifest(workDir, filePrefix string, cluster *common.Cluster) {
	baseDir := filepath.Join(workDir, "openshift")
	tmpl, err := template.New("").Parse(machineSetManifestTemplate)
	Expect(err).To(BeNil())
	fileName := strings.Replace(ovirt.MachineSetFileNameGlobStr, "*", filePrefix, -1)
	filePath := filepath.Join(baseDir, fileName)
	manifestParams := map[string]interface{}{
		"CLUSTER_NAME": cluster.Name,
	}
	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, manifestParams)
	Expect(err).To(BeNil())
	err = os.WriteFile(filePath, buf.Bytes(), 0600)
	Expect(err).To(BeNil())
}

func createHost(isMaster bool, state string, inventory string) *models.Host {
	hostId := strfmt.UUID(uuid.New().String())
	clusterId := strfmt.UUID(uuid.New().String())
	infraEnvId := strfmt.UUID(uuid.New().String())
	hostRole := models.HostRoleWorker
	if isMaster {
		hostRole = models.HostRoleMaster
	}
	host := models.Host{
		ID:         &hostId,
		InfraEnvID: infraEnvId,
		ClusterID:  &clusterId,
		Kind:       swag.String(models.HostKindHost),
		Status:     swag.String(state),
		Role:       hostRole,
		Inventory:  inventory,
	}
	return &host
}

func createHostWithID(id string, isMaster bool, state, inventory string) *models.Host {
	hostId := strfmt.UUID(id)
	clusterId := strfmt.UUID(uuid.New().String())
	infraEnvId := strfmt.UUID(uuid.New().String())
	hostRole := models.HostRoleWorker
	if isMaster {
		hostRole = models.HostRoleMaster
	}
	host := models.Host{
		ID:         &hostId,
		InfraEnvID: infraEnvId,
		ClusterID:  &clusterId,
		Kind:       swag.String(models.HostKindHost),
		Status:     swag.String(state),
		Role:       hostRole,
		Inventory:  inventory,
	}
	return &host
}

func getInventory(hostname, bootMode string, ipv4, ipv6 bool) models.Inventory {
	inventory := models.Inventory{
		Hostname: hostname,
		Boot:     &models.Boot{CurrentBootMode: bootMode},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: []string{},
				IPV6Addresses: []string{},
				MacAddress:    "some MAC address",
				Type:          "physical",
			},
		},
	}
	if ipv4 {
		inventory.Interfaces[0].IPV4Addresses = []string{"10.35.20.10/24"}
	}
	if ipv6 {
		inventory.Interfaces[0].IPV6Addresses = []string{"fe80::1/64"}
	}
	return inventory
}

func getVsphereInventoryStr(hostname, bootMode string, ipv4, ipv6 bool) string {
	inventory := getInventory(hostname, bootMode, ipv4, ipv6)
	inventory.SystemVendor = &models.SystemVendor{
		Manufacturer: "VMware, Inc.",
		ProductName:  "Mware7,1",
		SerialNumber: "VMware-12 34 56 78 90 12 ab cd-ef gh 12 34 56 67 89 90",
		Virtual:      true,
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

func getOvirtInventoryStr(hostname, bootMode string, ipv4, ipv6 bool) string {
	inventory := getInventory(hostname, bootMode, ipv4, ipv6)
	inventory.SystemVendor = &models.SystemVendor{
		Manufacturer: "oVirt",
		ProductName:  "oVirt",
		SerialNumber: "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
		Virtual:      true,
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

func getBaremetalInventoryStr(hostname, bootMode string, ipv4, ipv6 bool) string {
	inventory := getInventory(hostname, bootMode, ipv4, ipv6)
	inventory.SystemVendor = &models.SystemVendor{
		Manufacturer: "Red Hat",
		ProductName:  "KVM",
		SerialNumber: "",
		Virtual:      false,
	}
	ret, _ := json.Marshal(&inventory)
	return string(ret)
}

func createVspherePlatformParams() *models.Platform {
	return &models.Platform{
		Type: common.PlatformTypePtr(models.PlatformTypeVsphere),
	}
}

func createOvirtPlatformParams() *models.Platform {
	return &models.Platform{
		Type: common.PlatformTypePtr(models.PlatformTypeOvirt),
	}
}

func createClusterFromHosts(hosts []*models.Host) common.Cluster {
	return common.Cluster{
		Cluster: models.Cluster{
			Name:             "cluster",
			APIVip:           "192.168.10.10",
			Hosts:            hosts,
			IngressVip:       "192.168.10.11",
			OpenshiftVersion: "4.7",
		},
	}
}

func getInstallerConfigBaremetal() installcfg.InstallerConfigBaremetal {
	return installcfg.InstallerConfigBaremetal{
		APIVersion: "v1",
		BaseDomain: "test.base.domain",
		Networking: struct {
			NetworkType    string                      `yaml:"networkType"`
			ClusterNetwork []installcfg.ClusterNetwork `yaml:"clusterNetwork"`
			MachineNetwork []installcfg.MachineNetwork `yaml:"machineNetwork,omitempty"`
			ServiceNetwork []string                    `yaml:"serviceNetwork"`
		}{
			NetworkType:    "OpenShiftSDN",
			ClusterNetwork: []installcfg.ClusterNetwork{{Cidr: "10.128.0.0/14", HostPrefix: 23}},
			MachineNetwork: []installcfg.MachineNetwork{{Cidr: "10.0.0.0/16"}},
			ServiceNetwork: []string{"172.30.0.0/16"},
		},
		Metadata: struct {
			Name string `yaml:"name"`
		}{Name: "dummy"},
		Compute: []struct {
			Hyperthreading string `yaml:"hyperthreading,omitempty"`
			Name           string `yaml:"name"`
			Replicas       int    `yaml:"replicas"`
		}{{
			Name:     "worker-test",
			Replicas: 2,
		}},
		ControlPlane: struct {
			Hyperthreading string `yaml:"hyperthreading,omitempty"`
			Name           string `yaml:"name"`
			Replicas       int    `yaml:"replicas"`
		}{
			Name:     "master-test",
			Replicas: 3,
		},
		Platform:              installcfg.Platform{},
		BootstrapInPlace:      installcfg.BootstrapInPlace{},
		FIPS:                  false,
		PullSecret:            "{\"auths\": fake}",
		SSHKey:                "ssh-rsa fake",
		AdditionalTrustBundle: "",
		ImageContentSources:   nil,
	}
}

func TestProviderRegistry(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ProviderRegistry test")
}
