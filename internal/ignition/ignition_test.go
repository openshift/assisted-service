package ignition

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	config_31 "github.com/coreos/ignition/v2/config/v3_1"
	config_32 "github.com/coreos/ignition/v2/config/v3_2"
	config_32_types "github.com/coreos/ignition/v2/config/v3_2/types"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/installcfg"
	"github.com/openshift/assisted-service/internal/oc"
	"github.com/openshift/assisted-service/internal/operators"
	"github.com/openshift/assisted-service/internal/provider/registry"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/sirupsen/logrus"
	"github.com/vincent-petithory/dataurl"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
)

var (
	cluster              *common.Cluster
	hostInventory        string
	installerCacheDir    string
	log                  = logrus.New()
	workDir              string
	mockOperatorManager  operators.API
	mockProviderRegistry *registry.MockProviderRegistry
	ctrl                 *gomock.Controller
)

var _ = BeforeEach(func() {
	// setup temp workdir
	var err error
	workDir, err = os.MkdirTemp("", "assisted-install-test-")
	Expect(err).NotTo(HaveOccurred())
	installerCacheDir = filepath.Join(workDir, "installercache")

	// create simple cluster
	clusterID := strfmt.UUID(uuid.New().String())
	cluster = &common.Cluster{
		PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		Cluster: models.Cluster{
			ID: &clusterID,
			MachineNetworks: []*models.MachineNetwork{{
				Cidr: "192.168.126.11/24",
			}},
		},
	}
	cluster.ImageInfo = &models.ImageInfo{}

	hostInventory = `{"bmc_address":"0.0.0.0","bmc_v6address":"::/0","boot":{"current_boot_mode":"bios"},"cpu":{"architecture":"x86_64","count":4,"flags":["fpu","vme","de","pse","tsc","msr","pae","mce","cx8","apic","sep","mtrr","pge","mca","cmov","pat","pse36","clflush","mmx","fxsr","sse","sse2","ss","syscall","nx","pdpe1gb","rdtscp","lm","constant_tsc","arch_perfmon","rep_good","nopl","xtopology","cpuid","tsc_known_freq","pni","pclmulqdq","vmx","ssse3","fma","cx16","pcid","sse4_1","sse4_2","x2apic","movbe","popcnt","tsc_deadline_timer","aes","xsave","avx","f16c","rdrand","hypervisor","lahf_lm","abm","3dnowprefetch","cpuid_fault","invpcid_single","pti","ssbd","ibrs","ibpb","stibp","tpr_shadow","vnmi","flexpriority","ept","vpid","ept_ad","fsgsbase","tsc_adjust","bmi1","hle","avx2","smep","bmi2","erms","invpcid","rtm","mpx","avx512f","avx512dq","rdseed","adx","smap","clflushopt","clwb","avx512cd","avx512bw","avx512vl","xsaveopt","xsavec","xgetbv1","xsaves","arat","umip","pku","ospke","md_clear","arch_capabilities"],"frequency":2095.076,"model_name":"Intel(R) Xeon(R) Gold 6152 CPU @ 2.10GHz"},"disks":[{"by_path":"/dev/disk/by-path/pci-0000:00:06.0","drive_type":"HDD","model":"unknown","name":"vda","path":"/dev/vda","serial":"unknown","size_bytes":21474836480,"vendor":"0x1af4","wwn":"unknown"}],"hostname":"test-infra-cluster-master-1.redhat.com","interfaces":[{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["192.168.126.11/24"],"ipv6_addresses":["fe80::5054:ff:fe42:1e8d/64"],"mac_address":"52:54:00:42:1e:8d","mtu":1500,"name":"eth0","product":"0x0001","speed_mbps":-1,"vendor":"0x1af4"},{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["192.168.140.133/24"],"ipv6_addresses":["fe80::5054:ff:feca:7b16/64"],"mac_address":"52:54:00:ca:7b:16","mtu":1500,"name":"eth1","product":"0x0001","speed_mbps":-1,"vendor":"0x1af4"}],"memory":{"physical_bytes":17809014784,"usable_bytes":17378611200},"system_vendor":{"manufacturer":"Red Hat","product_name":"KVM"}}`

	ctrl = gomock.NewController(GinkgoT())
	mockOperatorManager = operators.NewMockAPI(ctrl)
	mockProviderRegistry = registry.NewMockProviderRegistry(ctrl)
})

var _ = AfterEach(func() {
	os.RemoveAll(workDir)
	ctrl.Finish()
})

var _ = Describe("Bootstrap Ignition Update", func() {
	const bootstrap1 = `{
		"ignition": {
		  "config": {},
		  "security": {
			"tls": {}
		  },
		  "timeouts": {},
		  "version": "3.2.0"
		},
		"storage": {
		  "files": [
			{
			  "filesystem": "root",
			  "path": "/opt/openshift/openshift/99_openshift-cluster-api_hosts-0.yaml",
			  "user": {
				"name": "root"
			  },
			  "contents": {
				"source": "data:text/plain;charset=utf-8;base64,YXBpVmVyc2lvbjogbWV0YWwzLmlvL3YxYWxwaGExCmtpbmQ6IEJhcmVNZXRhbEhvc3QKbWV0YWRhdGE6CiAgY3JlYXRpb25UaW1lc3RhbXA6IG51bGwKICBuYW1lOiBvcGVuc2hpZnQtbWFzdGVyLTAKICBuYW1lc3BhY2U6IG9wZW5zaGlmdC1tYWNoaW5lLWFwaQpzcGVjOgogIGJtYzoKICAgIGFkZHJlc3M6IGlwbWk6Ly8xOTIuMTY4LjExMS4xOjYyMzAKICAgIGNyZWRlbnRpYWxzTmFtZTogb3BlbnNoaWZ0LW1hc3Rlci0wLWJtYy1zZWNyZXQKICBib290TUFDQWRkcmVzczogMDA6YWE6Mzk6YjM6NTE6MTAKICBjb25zdW1lclJlZjoKICAgIGFwaVZlcnNpb246IG1hY2hpbmUub3BlbnNoaWZ0LmlvL3YxYmV0YTEKICAgIGtpbmQ6IE1hY2hpbmUKICAgIG5hbWU6IGRlbW8tbWFzdGVyLTAKICAgIG5hbWVzcGFjZTogb3BlbnNoaWZ0LW1hY2hpbmUtYXBpCiAgZXh0ZXJuYWxseVByb3Zpc2lvbmVkOiB0cnVlCiAgaGFyZHdhcmVQcm9maWxlOiB1bmtub3duCiAgb25saW5lOiB0cnVlCnN0YXR1czoKICBlcnJvck1lc3NhZ2U6ICIiCiAgZ29vZENyZWRlbnRpYWxzOiB7fQogIGhhcmR3YXJlUHJvZmlsZTogIiIKICBvcGVyYXRpb25IaXN0b3J5OgogICAgZGVwcm92aXNpb246CiAgICAgIGVuZDogbnVsbAogICAgICBzdGFydDogbnVsbAogICAgaW5zcGVjdDoKICAgICAgZW5kOiBudWxsCiAgICAgIHN0YXJ0OiBudWxsCiAgICBwcm92aXNpb246CiAgICAgIGVuZDogbnVsbAogICAgICBzdGFydDogbnVsbAogICAgcmVnaXN0ZXI6CiAgICAgIGVuZDogbnVsbAogICAgICBzdGFydDogbnVsbAogIG9wZXJhdGlvbmFsU3RhdHVzOiAiIgogIHBvd2VyZWRPbjogZmFsc2UKICBwcm92aXNpb25pbmc6CiAgICBJRDogIiIKICAgIGltYWdlOgogICAgICBjaGVja3N1bTogIiIKICAgICAgdXJsOiAiIgogICAgc3RhdGU6ICIiCiAgdHJpZWRDcmVkZW50aWFsczoge30K",
				"verification": {}
			  },
			  "mode": 420
			}
		  ]
		}
	  }`

	var (
		err          error
		examplePath  string
		db           *gorm.DB
		dbName       string
		bmh          *bmh_v1alpha1.BareMetalHost
		config       *config_32_types.Config
		mockS3Client *s3wrapper.MockAPI
	)

	BeforeEach(func() {
		var err1 error
		examplePath = filepath.Join(workDir, "example1.ign")
		err1 = os.WriteFile(examplePath, []byte(bootstrap1), 0600)
		Expect(err1).NotTo(HaveOccurred())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)

		cluster.Hosts = []*models.Host{
			{
				Inventory:         hostInventory,
				RequestedHostname: "example1",
				Role:              models.HostRoleMaster,
			},
		}
		db, dbName = common.PrepareTestDB()
		g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", mockS3Client, log,
			mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)

		err = g.updateBootstrap(context.Background(), examplePath)

		// TODO(deprecate-ignition-3.1.0)
		bootstrapBytes, _ := os.ReadFile(examplePath)
		config, err1 = ParseToLatest(bootstrapBytes)
		Expect(err1).NotTo(HaveOccurred())
		Expect(config.Ignition.Version).To(Equal("3.2.0"))
		bytes, err1 := json.Marshal(config)
		Expect(err1).ToNot(HaveOccurred())
		v32Config, _, err1 := config_32.Parse(bytes)
		Expect(err1).ToNot(HaveOccurred())
		Expect(v32Config.Ignition.Version).To(Equal("3.2.0"))

		var file *config_32_types.File
		foundNMConfig := false
		for i := range config.Storage.Files {
			if isBMHFile(&config.Storage.Files[i]) {
				file = &config.Storage.Files[i]
			}
			if config.Storage.Files[i].Node.Path == "/etc/NetworkManager/conf.d/99-kni.conf" {
				foundNMConfig = true
			}
		}
		bmh, _ = fileToBMH(file)
		Expect(foundNMConfig).To(BeTrue(), "file /etc/NetworkManager/conf.d/99-kni.conf not present in bootstrap.ign")
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Context("Identify host role", func() {
		var hosts []*models.Host

		BeforeEach(func() {
			hosts = []*models.Host{
				{
					RequestedHostname: "openshift-master-0",
				},
			}
		})
		test := func(masters, workers []*models.Host, masterExpected bool) {
			masterHostnames := getHostnames(masters)
			workerHostnames := getHostnames(workers)
			Expect(err).ToNot(HaveOccurred())
			for i := range config.Storage.Files {
				if isBMHFile(&config.Storage.Files[i]) {
					bmhFile, err2 := fileToBMH(&config.Storage.Files[i]) //nolint,shadow
					Expect(err2).ToNot(HaveOccurred())
					Expect(bmhIsMaster(bmhFile, masterHostnames, workerHostnames)).To(Equal(masterExpected))
					return
				}
			}
			Fail("No BMH file found")
		}
		It("Set as master by hostname", func() {
			test(hosts, nil, true)
		})
		It("Set as worker by hostname", func() {
			test(nil, hosts, false)
		})
		It("Set as master by backward compatibility", func() {
			test(nil, nil, true)
		})
	})

	Describe("update bootstrap.ign", func() {
		Context("with 1 master", func() {
			It("got a tmp workDir", func() {
				Expect(workDir).NotTo(Equal(""))
			})
			It("adds annotation", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(bmh.Annotations).To(HaveKey(bmh_v1alpha1.StatusAnnotation))
			})
			It("adds the marker file", func() {
				var found bool
				for _, f := range config.Storage.Files {
					if f.Path == "/opt/openshift/assisted-install-bootstrap" {
						found = true
					}
				}
				Expect(found).To(BeTrue())
			})
		})
	})
})

var _ = Describe("Cluster Ignitions Update", func() {
	const ignition = `{
		"ignition": {
		  "config": {},
		  "version": "3.2.0"
		},
		"storage": {
		  "files": []
		}
	  }`

	const caCert = `
-----BEGIN CERTIFICATE-----
MIIDkDCCAnigAwIBAgIUNQRERAPbVOlJoLs2N76uLZN9S1gwDQYJKoZIhvcNAQEL
BQAwTzELMAkGA1UEBhMCQ0ExCzAJBgNVBAgMAlFDMRswGQYDVQQKDBJBc3Npc3Rl
ZCBJbnN0YWxsZXIxFjAUBgNVBAMMDTE5Mi4xNjguMTIyLjUwHhcNMjAxMDAxMTUz
NDUyWhcNMjExMDAxMTUzNDUyWjBPMQswCQYDVQQGEwJDQTELMAkGA1UECAwCUUMx
GzAZBgNVBAoMEkFzc2lzdGVkIEluc3RhbGxlcjEWMBQGA1UEAwwNMTkyLjE2OC4x
MjIuNTCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALF0Oj3awX//uMSn
B7grPKSuSbLlBIIeRgHaOAvdVZFn86f2G8prG0RHA4u9anidQlhR3wCGx16bQIt0
NC3n16RSn5x9LgsV0woFrXNUs535nkE0Zg5Yex10/yF8URauzlPierq10fe1N6kB
OF1OfGBPpyUN+1zSeYcX4fyALpreLaTEhIGMnHjDqytccbupNYjrCWA5lE4uJ6a4
BBAqiWPBV5KneD5pHNb7mVbMaFGdteUwqKQtfO8uM0T9loYbXNYqVt6irOYbIowo
uHvsdGD3ryFnASGOZ4AJ0eQXSn3bFrMj5T9ojna1C82DYhK2Mbff1qrMYZG2rNE6
y6Is8gkCAwEAAaNkMGIwHQYDVR0OBBYEFK4tVRjbPL3fuId5mdKOFALaGQw6MB8G
A1UdIwQYMBaAFK4tVRjbPL3fuId5mdKOFALaGQw6MA8GA1UdEwEB/wQFMAMBAf8w
DwYDVR0RBAgwBocEwKh6BTANBgkqhkiG9w0BAQsFAAOCAQEAoeJYGcAYdrkQcOum
ph4LNyEBhnfqlcQ5gQLIGALf/tpuz66SEeR1Km9hRwsl4nqDf2IVLu9CY79VP4J3
tgu2tPcz/jpqcMdp54Pw20AfzW/zJqPV/TEYZ1CYeaRbsnTRltx8KlnF0OVDNv8M
Q6BVcoQmSTxlJeGp9hrxahCbGHjKIaLLxmEdwVt1HpEMcGXjv5E6dbil9U6Mx1Ce
nghVxZEMX1Vrnlyu1LVknfcWQT1HTK0ccMp1RRewM21C87MADYwN1ale2C6jVEyk
SV4bRR9i0uf+xQ/oYRvugQ25Q7EahO5hJIWRf4aULbk36Zpw3++v2KFnF26zqwB6
1XWdHQ==
-----END CERTIFICATE-----`

	var (
		masterPath string
		workerPath string
		caCertPath string
		dbName     string
		db         *gorm.DB
	)

	BeforeEach(func() {
		masterPath = filepath.Join(workDir, "master.ign")
		workerPath = filepath.Join(workDir, "worker.ign")
		err := os.WriteFile(masterPath, []byte(ignition), 0600)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(workerPath, []byte(ignition), 0600)
		Expect(err).NotTo(HaveOccurred())

		caCertPath = filepath.Join(workDir, "service-ca-cert.crt")
		err = os.WriteFile(caCertPath, []byte(caCert), 0600)
		Expect(err).NotTo(HaveOccurred())
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	Describe("update ignitions", func() {
		It("with ca cert file", func() {
			g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", caCertPath, "", nil, log,
				mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)

			err := g.updateIgnitions()
			Expect(err).NotTo(HaveOccurred())

			masterBytes, err := os.ReadFile(masterPath)
			Expect(err).NotTo(HaveOccurred())
			masterConfig, _, err := config_32.Parse(masterBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(masterConfig.Storage.Files).To(HaveLen(1))
			file := &masterConfig.Storage.Files[0]
			Expect(file.Path).To(Equal(common.HostCACertPath))

			workerBytes, err := os.ReadFile(workerPath)
			Expect(err).NotTo(HaveOccurred())
			workerConfig, _, err := config_32.Parse(workerBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(workerConfig.Storage.Files).To(HaveLen(1))
			file = &masterConfig.Storage.Files[0]
			Expect(file.Path).To(Equal(common.HostCACertPath))
		})
		It("with no ca cert file", func() {
			g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", nil, log,
				mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)

			err := g.updateIgnitions()
			Expect(err).NotTo(HaveOccurred())

			masterBytes, err := os.ReadFile(masterPath)
			Expect(err).NotTo(HaveOccurred())
			masterConfig, _, err := config_32.Parse(masterBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(masterConfig.Storage.Files).To(HaveLen(0))

			workerBytes, err := os.ReadFile(workerPath)
			Expect(err).NotTo(HaveOccurred())
			workerConfig, _, err := config_32.Parse(workerBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(workerConfig.Storage.Files).To(HaveLen(0))
		})
		It("with service ips", func() {
			g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", nil, log,
				mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)

			err := g.UpdateEtcHosts("10.10.10.1,10.10.10.2")
			Expect(err).NotTo(HaveOccurred())

			masterBytes, err := os.ReadFile(masterPath)
			Expect(err).NotTo(HaveOccurred())
			masterConfig, _, err := config_32.Parse(masterBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(masterConfig.Storage.Files).To(HaveLen(1))
			file := &masterConfig.Storage.Files[0]
			Expect(file.Path).To(Equal("/etc/hosts"))

			workerBytes, err := os.ReadFile(workerPath)
			Expect(err).NotTo(HaveOccurred())
			workerConfig, _, err := config_32.Parse(workerBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(workerConfig.Storage.Files).To(HaveLen(1))
			file = &masterConfig.Storage.Files[0]
			Expect(file.Path).To(Equal("/etc/hosts"))
		})
		It("with no service ips", func() {
			g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", nil, log,
				mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)

			err := g.UpdateEtcHosts("")
			Expect(err).NotTo(HaveOccurred())

			masterBytes, err := os.ReadFile(masterPath)
			Expect(err).NotTo(HaveOccurred())
			masterConfig, _, err := config_32.Parse(masterBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(masterConfig.Storage.Files).To(HaveLen(0))

			workerBytes, err := os.ReadFile(workerPath)
			Expect(err).NotTo(HaveOccurred())
			workerConfig, _, err := config_32.Parse(workerBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(workerConfig.Storage.Files).To(HaveLen(0))
		})
		It("get service ip hostnames", func() {
			content := GetServiceIPHostnames("")
			Expect(content).To(Equal(""))

			content = GetServiceIPHostnames("10.10.10.10")
			Expect(content).To(Equal("10.10.10.10 assisted-api.local.openshift.io\n"))

			content = GetServiceIPHostnames("10.10.10.1,10.10.10.2")
			Expect(content).To(Equal("10.10.10.1 assisted-api.local.openshift.io\n10.10.10.2 assisted-api.local.openshift.io\n"))
		})
		Context("DHCP generation", func() {
			It("Definitions only", func() {
				g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", nil, log,
					mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)

				g.encodedDhcpFileContents = "data:,abc"
				err := g.updateIgnitions()
				Expect(err).NotTo(HaveOccurred())

				masterBytes, err := os.ReadFile(masterPath)
				Expect(err).ToNot(HaveOccurred())
				masterConfig, _, err := config_32.Parse(masterBytes)
				Expect(err).NotTo(HaveOccurred())
				Expect(masterConfig.Storage.Files).To(HaveLen(1))
				f := masterConfig.Storage.Files[0]
				Expect(f.Mode).To(Equal(swag.Int(0o644)))
				Expect(f.Contents.Source).To(Equal(swag.String("data:,abc")))
				Expect(f.Path).To(Equal("/etc/keepalived/unsupported-monitor.conf"))
			})
		})
		It("Definitions+leases", func() {
			g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", nil, log,
				mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)

			g.encodedDhcpFileContents = "data:,abc"
			cluster.ApiVipLease = "api"
			cluster.IngressVipLease = "ingress"
			err := g.updateIgnitions()
			Expect(err).NotTo(HaveOccurred())

			masterBytes, err := os.ReadFile(masterPath)
			Expect(err).ToNot(HaveOccurred())
			masterConfig, _, err := config_32.Parse(masterBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(masterConfig.Storage.Files).To(HaveLen(3))
			f := masterConfig.Storage.Files[0]
			Expect(f.Mode).To(Equal(swag.Int(0o644)))
			Expect(f.Contents.Source).To(Equal(swag.String("data:,abc")))
			Expect(f.Path).To(Equal("/etc/keepalived/unsupported-monitor.conf"))
			f = masterConfig.Storage.Files[1]
			Expect(f.Mode).To(Equal(swag.Int(0o644)))
			Expect(f.Contents.Source).To(Equal(swag.String("data:,api")))
			Expect(f.Path).To(Equal("/etc/keepalived/lease-api"))
			f = masterConfig.Storage.Files[2]
			Expect(f.Mode).To(Equal(swag.Int(0o644)))
			Expect(f.Contents.Source).To(Equal(swag.String("data:,ingress")))
			Expect(f.Path).To(Equal("/etc/keepalived/lease-ingress"))
		})
	})
})

// TODO(deprecate-ignition-3.1.0)
var _ = Describe("createHostIgnitions", func() {
	const testMasterIgn = `{
		  "ignition": {
		    "config": {
		      "merge": [
			{
			  "source": "https://192.168.126.199:22623/config/master"
			}
		      ]
		    },
		    "security": {
		      "tls": {
			"certificateAuthorities": [
			  {
			    "source": "data:text/plain;charset=utf-8;base64,LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURFRENDQWZpZ0F3SUJBZ0lJUk90aUgvOC82ckF3RFFZSktvWklodmNOQVFFTEJRQXdKakVTTUJBR0ExVUUKQ3hNSmIzQmxibk5vYVdaME1SQXdEZ1lEVlFRREV3ZHliMjkwTFdOaE1CNFhEVEl3TURreE9ERTVORFV3TVZvWApEVE13TURreE5qRTVORFV3TVZvd0pqRVNNQkFHQTFVRUN4TUpiM0JsYm5Ob2FXWjBNUkF3RGdZRFZRUURFd2R5CmIyOTBMV05oTUlJQklqQU5CZ2txaGtpRzl3MEJBUUVGQUFPQ0FROEFNSUlCQ2dLQ0FRRUE1c1orVWtaaGsxUWQKeFU3cWI3YXArNFczaS9ZWTFzZktURC8ybDVJTjFJeVhPajlSL1N2VG5SOGYvajNJa1JHMWN5ZXR4bnNlNm1aZwpaOW1IRDJMV0srSEFlTTJSYXpuRkEwVmFwOWxVbVRrd3Vza2Z3QzhnMWJUZUVHUlEyQmFId09KekpvdjF4a0ZICmU2TUZCMlcxek1rTWxLTkwycnlzMzRTeVYwczJpNTFmTTJvTEM2SXRvWU91RVVVa2o0dnVUbThPYm5rV0t4ZnAKR1VGMThmNzVYeHJId0tVUEd0U0lYMGxpVGJNM0tiTDY2V2lzWkFIeStoN1g1dnVaaFYzYXhwTVFMdlczQ2xvcQpTaG9zSXY4SWNZbUJxc210d2t1QkN3cWxibEo2T2gzblFrelorVHhQdGhkdWsrZytzaVBUNi9va0JKU2M2cURjClBaNUNyN3FrR3dJREFRQUJvMEl3UURBT0JnTlZIUThCQWY4RUJBTUNBcVF3RHdZRFZSMFRBUUgvQkFVd0F3RUIKL3pBZEJnTlZIUTRFRmdRVWNSbHFHT1g3MWZUUnNmQ0tXSGFuV3NwMFdmRXdEUVlKS29aSWh2Y05BUUVMQlFBRApnZ0VCQU5Xc0pZMDY2RnNYdzFOdXluMEkwNUtuVVdOMFY4NVJVV2drQk9Wd0J5bHluTVRneGYyM3RaY1FsS0U4CjVHMlp4Vzl5NmpBNkwzMHdSNWhOcnBzM2ZFcUhobjg3UEM3L2tWQWlBOWx6NjBwV2ovTE5GU1hobDkyejBGMEIKcGNUQllFc1JNYU0zTFZOK0tZb3Q2cnJiamlXdmxFMU9hS0Q4dnNBdkk5YXVJREtOdTM0R2pTaUJGWXMrelRjSwphUUlTK3UzRHVYMGpVY001aUgrMmwzNGxNR0hlY2tjS1hnUWNXMGJiT28xNXY1Q2ExenJtQ2hIUHUwQ2NhMU1MCjJaM2MxMHVXZnR2OVZnbC9LcEpzSjM3b0phbTN1Mmp6MXN0K3hHby9iTmVSdHpOMjdXQSttaDZ6bXFwRldYKzUKdWFjZUY1SFRWc0FkbmtJWHpwWXBuek5qb0lFPQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg=="
			  }
			]
		      }
		    },
		    "version": "3.2.0"
		  },
		  "storage": {
		    "files": [
		      {
			"filesystem": "root",
			"path": "/etc/keepalived/unsupported-monitor.conf",
			"mode": 644,
			"contents": {
			  "source": "data:,api-vip:%0A%20%20name:%20api%0A%20%20mac-address:%2000:1a:4a:b8:a9:d6%0A%20%20ip-address:%20192.168.126.199%0Aingress-vip:%0A%20%20name:%20ingress%0A%20%20mac-address:%2000:1a:4a:09:b7:50%0A%20%20ip-address:%20192.168.126.126%0A"
			}
		      }
		    ]
		  }
		}`
	const testWorkerIgn = `{
		  "ignition": {
		    "config": {
		      "merge": [
			{
			  "source": "https://192.168.126.199:22623/config/worker"
			}
		      ]
		    },
		    "security": {
		      "tls": {
			"certificateAuthorities": [
			  {
			    "source": "data:text/plain;charset=utf-8;base64,LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURFRENDQWZpZ0F3SUJBZ0lJUk90aUgvOC82ckF3RFFZSktvWklodmNOQVFFTEJRQXdKakVTTUJBR0ExVUUKQ3hNSmIzQmxibk5vYVdaME1SQXdEZ1lEVlFRREV3ZHliMjkwTFdOaE1CNFhEVEl3TURreE9ERTVORFV3TVZvWApEVE13TURreE5qRTVORFV3TVZvd0pqRVNNQkFHQTFVRUN4TUpiM0JsYm5Ob2FXWjBNUkF3RGdZRFZRUURFd2R5CmIyOTBMV05oTUlJQklqQU5CZ2txaGtpRzl3MEJBUUVGQUFPQ0FROEFNSUlCQ2dLQ0FRRUE1c1orVWtaaGsxUWQKeFU3cWI3YXArNFczaS9ZWTFzZktURC8ybDVJTjFJeVhPajlSL1N2VG5SOGYvajNJa1JHMWN5ZXR4bnNlNm1aZwpaOW1IRDJMV0srSEFlTTJSYXpuRkEwVmFwOWxVbVRrd3Vza2Z3QzhnMWJUZUVHUlEyQmFId09KekpvdjF4a0ZICmU2TUZCMlcxek1rTWxLTkwycnlzMzRTeVYwczJpNTFmTTJvTEM2SXRvWU91RVVVa2o0dnVUbThPYm5rV0t4ZnAKR1VGMThmNzVYeHJId0tVUEd0U0lYMGxpVGJNM0tiTDY2V2lzWkFIeStoN1g1dnVaaFYzYXhwTVFMdlczQ2xvcQpTaG9zSXY4SWNZbUJxc210d2t1QkN3cWxibEo2T2gzblFrelorVHhQdGhkdWsrZytzaVBUNi9va0JKU2M2cURjClBaNUNyN3FrR3dJREFRQUJvMEl3UURBT0JnTlZIUThCQWY4RUJBTUNBcVF3RHdZRFZSMFRBUUgvQkFVd0F3RUIKL3pBZEJnTlZIUTRFRmdRVWNSbHFHT1g3MWZUUnNmQ0tXSGFuV3NwMFdmRXdEUVlKS29aSWh2Y05BUUVMQlFBRApnZ0VCQU5Xc0pZMDY2RnNYdzFOdXluMEkwNUtuVVdOMFY4NVJVV2drQk9Wd0J5bHluTVRneGYyM3RaY1FsS0U4CjVHMlp4Vzl5NmpBNkwzMHdSNWhOcnBzM2ZFcUhobjg3UEM3L2tWQWlBOWx6NjBwV2ovTE5GU1hobDkyejBGMEIKcGNUQllFc1JNYU0zTFZOK0tZb3Q2cnJiamlXdmxFMU9hS0Q4dnNBdkk5YXVJREtOdTM0R2pTaUJGWXMrelRjSwphUUlTK3UzRHVYMGpVY001aUgrMmwzNGxNR0hlY2tjS1hnUWNXMGJiT28xNXY1Q2ExenJtQ2hIUHUwQ2NhMU1MCjJaM2MxMHVXZnR2OVZnbC9LcEpzSjM3b0phbTN1Mmp6MXN0K3hHby9iTmVSdHpOMjdXQSttaDZ6bXFwRldYKzUKdWFjZUY1SFRWc0FkbmtJWHpwWXBuek5qb0lFPQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg=="
			  }
			]
		      }
		    },
		    "version": "3.2.0"
		  }
		}`

	var (
		dbName       string
		db           *gorm.DB
		mockS3Client *s3wrapper.MockAPI
	)

	BeforeEach(func() {
		masterPath := filepath.Join(workDir, "master.ign")
		err := os.WriteFile(masterPath, []byte(testMasterIgn), 0600)
		Expect(err).NotTo(HaveOccurred())

		workerPath := filepath.Join(workDir, "worker.ign")
		err = os.WriteFile(workerPath, []byte(testWorkerIgn), 0600)
		Expect(err).NotTo(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
	})

	Context("with multiple hosts with a hostname", func() {
		It("adds the hostname and boot-reporter files", func() {
			cluster.Hosts = []*models.Host{
				{
					RequestedHostname: "master0.example.com",
					Role:              models.HostRoleMaster,
				},
				{
					RequestedHostname: "master1.example.com",
					Role:              models.HostRoleMaster,
				},
				{
					RequestedHostname: "worker0.example.com",
					Role:              models.HostRoleWorker,
				},
				{
					RequestedHostname: "worker1.example.com",
					Role:              models.HostRoleWorker,
				},
			}

			// create an ID for each host
			for _, host := range cluster.Hosts {
				id := strfmt.UUID(uuid.New().String())
				host.ID = &id
			}

			g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", nil, log,
				mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)

			err := g.createHostIgnitions("http://www.example.com:6008", auth.TypeRHSSO)
			Expect(err).NotTo(HaveOccurred())

			for _, host := range cluster.Hosts {
				ignBytes, err := os.ReadFile(filepath.Join(workDir, fmt.Sprintf("%s-%s.ign", host.Role, host.ID)))
				Expect(err).NotTo(HaveOccurred())
				config, _, err := config_32.Parse(ignBytes)
				Expect(err).NotTo(HaveOccurred())

				By("Ensuring the correct role file was used")
				sourceURL := config.Ignition.Config.Merge[0].Source
				if host.Role == models.HostRoleMaster {
					Expect(*sourceURL).To(Equal("https://192.168.126.199:22623/config/master"))
				} else if host.Role == models.HostRoleWorker {
					Expect(*sourceURL).To(Equal("https://192.168.126.199:22623/config/worker"))
				}

				By("Validating the hostname file was added")
				var f *config_32_types.File
				for fileidx, file := range config.Storage.Files {
					if file.Node.Path == "/etc/hostname" {
						f = &config.Storage.Files[fileidx]
						break
					}
				}
				Expect(f).NotTo(BeNil())
				Expect(*f.Node.User.Name).To(Equal("root"))
				Expect(*f.FileEmbedded1.Contents.Source).To(Equal(fmt.Sprintf("data:,%s", host.RequestedHostname)))
				Expect(*f.FileEmbedded1.Mode).To(Equal(420))
				Expect(*f.Node.Overwrite).To(Equal(true))

			}
		})
	})

	It("applies overrides correctly", func() {
		hostID := strfmt.UUID(uuid.New().String())
		cluster.Hosts = []*models.Host{{
			ID:                      &hostID,
			RequestedHostname:       "master0.example.com",
			Role:                    models.HostRoleMaster,
			IgnitionConfigOverrides: `{"ignition": {"version": "3.2.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`,
		}}

		g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", nil, log,
			mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)

		err := g.createHostIgnitions("http://www.example.com:6008", auth.TypeNone)
		Expect(err).NotTo(HaveOccurred())

		ignBytes, err := os.ReadFile(filepath.Join(workDir, fmt.Sprintf("%s-%s.ign", models.HostRoleMaster, hostID)))
		Expect(err).NotTo(HaveOccurred())
		config, _, err := config_32.Parse(ignBytes)
		Expect(err).NotTo(HaveOccurred())

		var exampleFile *config_32_types.File
		var hostnameFile *config_32_types.File
		for fileidx, file := range config.Storage.Files {
			if file.Node.Path == "/tmp/example" {
				exampleFile = &config.Storage.Files[fileidx]
			} else if file.Node.Path == "/etc/hostname" {
				hostnameFile = &config.Storage.Files[fileidx]
			}
		}
		Expect(exampleFile).NotTo(BeNil())
		// check that we didn't overwrite the other files
		Expect(hostnameFile).NotTo(BeNil())

		Expect(*exampleFile.FileEmbedded1.Contents.Source).To(Equal("data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"))
	})
	Context("machine config pool", func() {
		const (
			mcp = `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfigPool
metadata:
  name: infra
spec:
  machineConfigSelector:
    matchExpressions:
      - {key: machineconfiguration.openshift.io/role, operator: In, values: [worker,infra]}
  maxUnavailable: null
  nodeSelector:
    matchLabels:
      node-role.kubernetes.io/infra: ""
  paused: false`

			mc = `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: infra
  name: 50-infra
spec:
  config:
    ignition:
      version: 2.2.0
    storage:
      files:
      - contents:
          source: data:,test
        filesystem: root
        mode: 0644
        path: /etc/testinfra`
		)

		It("applies machine config pool correctly", func() {
			hostID := strfmt.UUID(uuid.New().String())
			clusterID := strfmt.UUID(uuid.New().String())
			cluster.Hosts = []*models.Host{{
				ID:                    &hostID,
				ClusterID:             &clusterID,
				RequestedHostname:     "worker0.example.com",
				Role:                  models.HostRoleWorker,
				MachineConfigPoolName: "infra",
			}}

			g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", mockS3Client, log,
				mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)
			mockS3Client.EXPECT().ListObjectsByPrefix(gomock.Any(), gomock.Any()).Return([]string{"mcp.yaml"}, nil)
			mockS3Client.EXPECT().ListObjectsByPrefix(gomock.Any(), gomock.Any()).Return(nil, nil)
			mockS3Client.EXPECT().Download(gomock.Any(), gomock.Any()).Return(io.NopCloser(strings.NewReader(mcp)), int64(0), nil)
			err := g.writeSingleHostFile(cluster.Hosts[0], workerIgn, g.workDir, "http://www.example.com:6008", "", auth.TypeNone)
			Expect(err).NotTo(HaveOccurred())

			ignBytes, err := os.ReadFile(filepath.Join(workDir, fmt.Sprintf("%s-%s.ign", models.HostRoleWorker, hostID)))
			Expect(err).NotTo(HaveOccurred())
			config, _, err := config_32.Parse(ignBytes)
			Expect(err).NotTo(HaveOccurred())
			Expect(config.Ignition.Config.Merge).To(HaveLen(1))
			Expect(swag.StringValue(config.Ignition.Config.Merge[0].Source)).To(HaveSuffix("config/infra"))
		})

		It("mcp not found", func() {
			hostID := strfmt.UUID(uuid.New().String())
			clusterID := strfmt.UUID(uuid.New().String())
			cluster.Hosts = []*models.Host{{
				ID:                    &hostID,
				ClusterID:             &clusterID,
				RequestedHostname:     "worker0.example.com",
				Role:                  models.HostRoleWorker,
				MachineConfigPoolName: "infra",
			}}

			g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", mockS3Client, log,
				mockOperatorManager, mockProviderRegistry, "", "").(*installerGenerator)
			mockS3Client.EXPECT().ListObjectsByPrefix(gomock.Any(), gomock.Any()).Return([]string{"mcp.yaml"}, nil)
			mockS3Client.EXPECT().ListObjectsByPrefix(gomock.Any(), gomock.Any()).Return(nil, nil)
			mockS3Client.EXPECT().Download(gomock.Any(), gomock.Any()).Return(io.NopCloser(strings.NewReader(mc)), int64(0), nil)
			err := g.writeSingleHostFile(cluster.Hosts[0], workerIgn, g.workDir, "http://www.example.com:6008", "", auth.TypeNone)
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Openshift cluster ID extraction", func() {
	It("fails on empty ignition file", func() {
		r := io.NopCloser(strings.NewReader(""))
		_, err := ExtractClusterID(r)
		Expect(err.Error()).To(ContainSubstring("not a config (empty)"))
	})

	It("fails on invalid JSON file", func() {
		r := io.NopCloser(strings.NewReader("{"))
		_, err := ExtractClusterID(r)
		Expect(err.Error()).To(ContainSubstring("config is not valid"))
	})

	It("fails on invalid ignition file", func() {
		r := io.NopCloser(strings.NewReader(`{
				"ignition":{"version":"invalid.version"}
		}`))
		_, err := ExtractClusterID(r)
		Expect(err.Error()).To(ContainSubstring("unsupported config version"))
	})

	It("fails when there's no CVO file", func() {
		r := io.NopCloser(strings.NewReader(`{
				"ignition":{"version":"3.2.0"},
				"storage":{
					"files":[]
				},
				"systemd":{}
		}`))
		_, err := ExtractClusterID(r)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("could not find cvo-overrides file"))
	})

	It("fails when no ClusterID is embedded in cvo-overrides", func() {
		r := io.NopCloser(strings.NewReader(`{
				"ignition":{"version":"3.2.0"},
				"storage":{
					"files":[
						{
							"path":"/opt/openshift/manifests/cvo-overrides.yaml",
							"contents":{
								"source":"data:text/plain;charset=utf-8;base64,"
							}
						}
					]
				},
				"systemd":{}
		}`))
		_, err := ExtractClusterID(r)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("no ClusterID field in cvo-overrides file"))
	})

	It("fails when cvo-overrides file cannot be un-marshalled", func() {
		// embedded JSON in the base64 format is "{"
		r := io.NopCloser(strings.NewReader(`{
				"ignition":{"version":"3.2.0"},
				"storage":{
					"files":[
						{
							"path":"/opt/openshift/manifests/cvo-overrides.yaml",
							"contents":{
								"source":"data:text/plain;charset=utf-8;base64,ew=="
							}
						}
					]
				},
				"systemd":{}
		}`))
		_, err := ExtractClusterID(r)
		Expect(err).To(Equal(errors.New("yaml: line 1: did not find expected node content")))
	})

	It("is successfull on valid file", func() {
		r := io.NopCloser(strings.NewReader(`{
				"ignition":{"version":"3.2.0"},
				"storage":{
					"files":[
						{
							"path":"/opt/openshift/manifests/cvo-overrides.yaml",
							"contents":{
								"source":"data:text/plain;charset=utf-8;base64,YXBpVmVyc2lvbjogY29uZmlnLm9wZW5zaGlmdC5pby92MQpraW5kOiBDbHVzdGVyVmVyc2lvbgptZXRhZGF0YToKICBuYW1lc3BhY2U6IG9wZW5zaGlmdC1jbHVzdGVyLXZlcnNpb24KICBuYW1lOiB2ZXJzaW9uCnNwZWM6CiAgdXBzdHJlYW06IGh0dHBzOi8vYXBpLm9wZW5zaGlmdC5jb20vYXBpL3VwZ3JhZGVzX2luZm8vdjEvZ3JhcGgKICBjaGFubmVsOiBzdGFibGUtNC42CiAgY2x1c3RlcklEOiA0MTk0MGVlOC1lYzk5LTQzZGUtODc2Ni0xNzQzODFiNDkyMWQK"
							}
						}
					]
				},
				"systemd":{}
		}`))
		Expect(ExtractClusterID(r)).To(Equal("41940ee8-ec99-43de-8766-174381b4921d"))
	})

	It("only looks on cvo-overrides file", func() {
		r := io.NopCloser(strings.NewReader(`{
				"ignition":{"version":"3.2.0"},
				"storage":{
					"files":[
						{
							"path":"/opt/openshift/manifests/some-other-file.yaml",
							"contents":{
								"source":"data:text/plain;charset=utf-8;base64,YXBpVmVyc2lvbjogY29uZmlnLm9wZW5zaGlmdC5pby92MQpraW5kOiBDbHVzdGVyVmVyc2lvbgptZXRhZGF0YToKICBuYW1lc3BhY2U6IG9wZW5zaGlmdC1jbHVzdGVyLXZlcnNpb24KICBuYW1lOiB2ZXJzaW9uCnNwZWM6CiAgdXBzdHJlYW06IGh0dHBzOi8vYXBpLm9wZW5zaGlmdC5jb20vYXBpL3VwZ3JhZGVzX2luZm8vdjEvZ3JhcGgKICBjaGFubmVsOiBzdGFibGUtNC42CiAgY2x1c3RlcklEOiA0MTk0MGVlOC1lYzk5LTQzZGUtODc2Ni0xNzQzODFiNDkyMWQK"
							}
						},
						{
							"path":"/opt/openshift/manifests/cvo-overrides.yaml",
							"contents":{
								"source":"data:text/plain;charset=utf-8;base64,"
							}
						}
					]
				},
				"systemd":{}
		}`))
		_, err := ExtractClusterID(r)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(Equal("no ClusterID field in cvo-overrides file"))
	})
})

var _ = Describe("Generator UploadToS3", func() {
	var (
		ctx          = context.Background()
		ctrl         *gomock.Controller
		mockS3Client *s3wrapper.MockAPI
	)

	generator := installerGenerator{
		log:     log,
		workDir: workDir,
	}

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)

		generator.s3Client = mockS3Client
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	mockUploadFile := func() *gomock.Call {
		return mockS3Client.EXPECT().UploadFile(gomock.Any(), gomock.Any(), gomock.Any())
	}

	mockUploadObjectTimestamp := func() *gomock.Call {
		return mockS3Client.EXPECT().UpdateObjectTimestamp(gomock.Any(), gomock.Any())
	}

	Context("cluster with known hosts", func() {
		BeforeEach(func() {
			hostID1 := strfmt.UUID(uuid.New().String())
			hostID2 := strfmt.UUID(uuid.New().String())
			cluster.Hosts = []*models.Host{
				{ID: &hostID1, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
				{ID: &hostID2, Status: swag.String(models.HostStatusKnown), Role: models.HostRoleMaster},
			}
			generator.cluster = cluster
		})

		It("validate upload files names", func() {
			for _, f := range fileNames {
				fullPath := filepath.Join(generator.workDir, f)
				key := filepath.Join(cluster.ID.String(), f)
				mockS3Client.EXPECT().UploadFile(gomock.Any(), fullPath, key).Return(nil).Times(1)
				mockS3Client.EXPECT().UpdateObjectTimestamp(gomock.Any(), key).Return(true, nil).Times(1)
			}
			for i := range cluster.Hosts {
				fullPath := filepath.Join(generator.workDir, hostutil.IgnitionFileName(cluster.Hosts[i]))
				key := filepath.Join(cluster.ID.String(), hostutil.IgnitionFileName(cluster.Hosts[i]))
				mockS3Client.EXPECT().UploadFile(gomock.Any(), fullPath, key).Return(nil).Times(1)
				mockS3Client.EXPECT().UpdateObjectTimestamp(gomock.Any(), key).Return(true, nil).Times(1)
			}

			Expect(generator.UploadToS3(ctx)).Should(Succeed())
		})

		It("upload failure", func() {
			mockUploadFile().Return(nil).Times(1)
			mockUploadObjectTimestamp().Return(true, nil).Times(1)
			mockUploadFile().Return(errors.New("error")).Times(1)

			err := generator.UploadToS3(ctx)
			Expect(err).Should(HaveOccurred())
		})

		It("set timestamp failure", func() {
			mockUploadFile().Return(nil).Times(2)
			mockUploadObjectTimestamp().Return(true, nil).Times(1)
			mockUploadObjectTimestamp().Return(true, errors.New("error")).Times(1)

			err := generator.UploadToS3(ctx)
			Expect(err).Should(HaveOccurred())
		})
	})
})

var _ = Describe("downloadManifest", func() {
	var (
		ctrl         *gomock.Controller
		mockS3Client *s3wrapper.MockAPI
		generator    *installerGenerator
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		generator = &installerGenerator{
			log:      log,
			workDir:  workDir,
			s3Client: mockS3Client,
			cluster:  cluster,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("writes the correct file", func() {
		ctx := context.Background()
		manifestName := fmt.Sprintf("%s/manifests/openshift/masters-chrony-configuration.yaml", cluster.ID)
		mockS3Client.EXPECT().Download(ctx, manifestName).Return(io.NopCloser(strings.NewReader("chronyconf")), int64(10), nil)
		Expect(os.Mkdir(filepath.Join(workDir, "/openshift"), 0755)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(workDir, "/manifests"), 0755)).To(Succeed())

		Expect(generator.downloadManifest(ctx, manifestName)).To(Succeed())

		content, err := os.ReadFile(filepath.Join(workDir, "/openshift/masters-chrony-configuration.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(Equal([]byte("chronyconf")))
	})
})

var _ = Context("with test ignitions", func() {
	const v30ignition = `{"ignition": {"version": "3.0.0"},"storage": {"files": []}}`
	const v31ignition = `{"ignition": {"version": "3.1.0"},"storage": {"files": [{"path": "/tmp/chocobomb", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
	const v32ignition = `{"ignition": {"version": "3.2.0"},"storage": {"files": [{"path": "/tmp/chocobomb", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
	const v33ignition = `{"ignition": {"version": "3.3.0"},"storage": {"files": []}}`
	const v99ignition = `{"ignition": {"version": "9.9.0"},"storage": {"files": []}}`

	const v31override = `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
	const v32override = `{"ignition": {"version": "3.2.0"}, "storage": {"disks":[{"device":"/dev/sdb","partitions":[{"label":"root","number":4,"resize":true,"sizeMiB":204800}],"wipeTable":false}],"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`

	Describe("ParseToLatest", func() {
		It("parses a v32 config as 3.2.0", func() {
			config, err := ParseToLatest([]byte(v32ignition))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Ignition.Version).To(Equal("3.2.0"))

			bytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			v32Config, _, err := config_32.Parse(bytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(v32Config.Ignition.Version).To(Equal("3.2.0"))
		})

		It("parses a v31 config as 3.1.0", func() {
			config, err := ParseToLatest([]byte(v31ignition))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Ignition.Version).To(Equal("3.1.0"))

			bytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			v31Config, _, err := config_31.Parse(bytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(v31Config.Ignition.Version).To(Equal("3.1.0"))
		})

		It("does not parse v99 config", func() {
			_, err := ParseToLatest([]byte(v99ignition))
			Expect(err.Error()).To(ContainSubstring("unsupported config version"))
		})

		It("does not parse v30 config", func() {
			_, err := ParseToLatest([]byte(v30ignition))
			Expect(err.Error()).To(ContainSubstring("unsupported config version"))
		})

		It("does not parse v33 config", func() {
			_, err := ParseToLatest([]byte(v33ignition))
			Expect(err.Error()).To(ContainSubstring("unsupported config version"))
		})
	})

	Describe("MergeIgnitionConfig", func() {
		It("parses a v31 config with v31 override as 3.1.0", func() {
			merge, err := MergeIgnitionConfig([]byte(v31ignition), []byte(v31override))
			Expect(err).ToNot(HaveOccurred())

			config, err := ParseToLatest([]byte(merge))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Ignition.Version).To(Equal("3.1.0"))

			bytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			v31Config, _, err := config_31.Parse(bytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(v31Config.Ignition.Version).To(Equal("3.1.0"))
		})

		It("parses a v31 config with v32 override as 3.2.0", func() {
			merge, err := MergeIgnitionConfig([]byte(v31ignition), []byte(v32override))
			Expect(err).ToNot(HaveOccurred())

			config, err := ParseToLatest([]byte(merge))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Ignition.Version).To(Equal("3.2.0"))

			bytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			v32Config, _, err := config_32.Parse(bytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(v32Config.Ignition.Version).To(Equal("3.2.0"))
		})

		// Be aware, this combination is counterintuitive and comes from the fact that MergeStructTranscribe()
		// is not order-agnostic and prefers the field coming from the override rather from the base.
		It("parses a v32 config with v31 override as 3.1.0", func() {
			merge, err := MergeIgnitionConfig([]byte(v32ignition), []byte(v31override))
			Expect(err).ToNot(HaveOccurred())

			config, err := ParseToLatest([]byte(merge))
			Expect(err).ToNot(HaveOccurred())
			Expect(config.Ignition.Version).To(Equal("3.1.0"))

			bytes, err := json.Marshal(config)
			Expect(err).ToNot(HaveOccurred())
			v31Config, _, err := config_31.Parse(bytes)
			Expect(err).ToNot(HaveOccurred())
			Expect(v31Config.Ignition.Version).To(Equal("3.1.0"))
		})
	})
})

var _ = AfterEach(func() {
	os.RemoveAll("manifests")
})

var _ = Describe("proxySettingsForIgnition", func() {

	Context("test proxy settings in discovery ignition", func() {
		var parameters = []struct {
			httpProxy, httpsProxy, noProxy, res string
		}{
			{"", "", "", ""},
			{
				"http://proxy.proxy", "", "",
				`"proxy": { "httpProxy": "http://proxy.proxy" }`,
			},
			{
				"http://proxy.proxy", "https://proxy.proxy", "",
				`"proxy": { "httpProxy": "http://proxy.proxy", "httpsProxy": "https://proxy.proxy" }`,
			},
			{
				"http://proxy.proxy", "", ".domain",
				`"proxy": { "httpProxy": "http://proxy.proxy", "noProxy": [".domain"] }`,
			},
			{
				"http://proxy.proxy", "https://proxy.proxy", ".domain",
				`"proxy": { "httpProxy": "http://proxy.proxy", "httpsProxy": "https://proxy.proxy", "noProxy": [".domain"] }`,
			},
			{
				"", "https://proxy.proxy", ".domain,123.123.123.123",
				`"proxy": { "httpsProxy": "https://proxy.proxy", "noProxy": [".domain","123.123.123.123"] }`,
			},
			{
				"", "https://proxy.proxy", "",
				`"proxy": { "httpsProxy": "https://proxy.proxy" }`,
			},
			{
				"", "", ".domain", "",
			},
		}

		It("verify rendered proxy settings", func() {
			for _, p := range parameters {
				s, err := proxySettingsForIgnition(p.httpProxy, p.httpsProxy, p.noProxy)
				Expect(err).To(BeNil())
				Expect(s).To(Equal(p.res))
			}
		})
	})
})

var _ = Describe("IgnitionBuilder", func() {
	var (
		ctrl                              *gomock.Controller
		infraEnv                          common.InfraEnv
		log                               logrus.FieldLogger
		builder                           IgnitionBuilder
		mockStaticNetworkConfig           *staticnetworkconfig.MockStaticNetworkConfig
		mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
		infraEnvID                        strfmt.UUID
		mockOcRelease                     *oc.MockRelease
		mockVersionHandler                *versions.MockHandler
	)

	BeforeEach(func() {
		log = common.GetTestLog()
		infraEnvID = strfmt.UUID("a640ef36-dcb1-11ea-87d0-0242ac130003")
		ctrl = gomock.NewController(GinkgoT())
		mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		mockOcRelease = oc.NewMockRelease(ctrl)
		mockVersionHandler = versions.NewMockHandler(ctrl)
		infraEnv = common.InfraEnv{InfraEnv: models.InfraEnv{
			ID:            &infraEnvID,
			PullSecretSet: false,
		}, PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
		//cluster.ImageInfo = &models.ImageInfo{}
		var err error
		builder, err = NewBuilder(log, mockStaticNetworkConfig, mockMirrorRegistriesConfigBuilder, mockOcRelease, mockVersionHandler)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("with auth enabled", func() {

		It("ignition_file_fails_missing_Pull_Secret_token", func() {
			infraEnvID = strfmt.UUID("a640ef36-dcb1-11ea-87d0-0242ac130003")
			infraEnvWithoutToken := common.InfraEnv{InfraEnv: models.InfraEnv{
				ID:            &infraEnvID,
				PullSecretSet: false,
			}, PullSecret: "{\"auths\":{\"registry.redhat.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}"}
			_, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnvWithoutToken, IgnitionConfig{}, false, auth.TypeRHSSO, "")

			Expect(err).ShouldNot(BeNil())
		})

		It("ignition_file_contains_pull_secret_token", func() {
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")

			Expect(err).Should(BeNil())
			Expect(text).Should(ContainSubstring("PULL_SECRET_TOKEN"))
		})

		It("ignition_file_contains_additoinal_trust_bundle", func() {
			const magicString string = "somemagicstring"

			// Try with bundle
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(2)
			infraEnv.AdditionalTrustBundle = magicString
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
			Expect(err).Should(BeNil())
			Expect(text).Should(ContainSubstring(dataurl.EncodeBytes([]byte(magicString))))

			// Try also without bundle
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			infraEnv.AdditionalTrustBundle = ""
			text, err = builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
			Expect(err).Should(BeNil())
			Expect(text).ShouldNot(ContainSubstring(dataurl.EncodeBytes([]byte(magicString))))
		})
	})

	It("auth_disabled_no_pull_secret_token", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeNone, "")

		Expect(err).Should(BeNil())
		Expect(text).ShouldNot(ContainSubstring("PULL_SECRET_TOKEN"))
	})

	It("ignition_file_contains_url", func() {
		serviceBaseURL := "file://10.56.20.70:7878"
		config := IgnitionConfig{ServiceBaseURL: serviceBaseURL}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, config, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring(fmt.Sprintf("--url %s", serviceBaseURL)))
	})

	It("ignition_file_safe_for_logging", func() {
		serviceBaseURL := "file://10.56.20.70:7878"
		config := IgnitionConfig{ServiceBaseURL: serviceBaseURL}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, config, true, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).ShouldNot(ContainSubstring("cloud.openshift.com"))
		Expect(text).Should(ContainSubstring("data:,*****"))
	})

	It("enabled_cert_verification", func() {
		config := IgnitionConfig{SkipCertVerification: false}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, config, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("--insecure=false"))
	})

	It("disabled_cert_verification", func() {
		config := IgnitionConfig{SkipCertVerification: true}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, config, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("--insecure=true"))
	})

	It("cert_verification_enabled_by_default", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("--insecure=false"))
	})

	It("ignition_file_contains_http_proxy", func() {
		proxy := models.Proxy{
			HTTPProxy: swag.String("http://10.10.1.1:3128"),
			NoProxy:   swag.String("quay.io"),
		}
		infraEnv.Proxy = &proxy
		//cluster.HTTPProxy = "http://10.10.1.1:3128"
		//cluster.NoProxy = "quay.io"
		serviceBaseURL := "file://10.56.20.70:7878"
		config := IgnitionConfig{ServiceBaseURL: serviceBaseURL}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, config, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring(`"proxy": { "httpProxy": "http://10.10.1.1:3128", "noProxy": ["quay.io"] }`))
	})

	It("ignition_file_contains_asterisk_no_proxy", func() {
		proxy := models.Proxy{
			HTTPProxy: swag.String("http://10.10.1.1:3128"),
			NoProxy:   swag.String("*"),
		}
		infraEnv.Proxy = &proxy
		//cluster.HTTPProxy = "http://10.10.1.1:3128"
		//cluster.NoProxy = "*"
		serviceBaseURL := "file://10.56.20.70:7878"
		config := IgnitionConfig{ServiceBaseURL: serviceBaseURL}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, config, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring(`"proxy": { "httpProxy": "http://10.10.1.1:3128", "noProxy": ["*"] }`))
	})

	It("produces a valid ignition v3.1 spec by default", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
	})

	// TODO(deprecate-ignition-3.1.0)
	It("produces a valid ignition v3.1 spec with overrides", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		numOfFiles := len(config.Storage.Files)

		infraEnv.IgnitionConfigOverride = `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err = builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config, report, err = config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
		Expect(len(config.Storage.Files)).To(Equal(numOfFiles + 1))
	})

	It("produces a valid ignition spec with internal overrides", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
		numOfFiles := len(config.Storage.Files)
		numOfUnits := len(config.Systemd.Units)
		ironicIgn := `{ "ignition": { "version": "3.2.0" }, "storage": { "files": [ { "group": { }, "overwrite": false, "path": "/etc/ironic-python-agent.conf", "user": { }, "contents": { "source": "data:text/plain,%0A%5BDEFAULT%5D%0Aapi_url%20%3D%20https%3A%2F%2Fironic.redhat.com%3A6385%0Ainspection_callback_url%20%3D%20https%3A%2F%2Fironic.redhat.com%3A5050%2Fv1%2Fcontinue%0Ainsecure%20%3D%20True%0A%0Acollect_lldp%20%3D%20True%0Aenable_vlan_interfaces%20%3D%20all%0Ainspection_collectors%20%3D%20default%2Cextra-hardware%2Clogs%0Ainspection_dhcp_all_interfaces%20%3D%20True%0A", "verification": { } }, "mode": 420 } ] }, "systemd": { "units": [ { "contents": "[Unit]\nDescription=Ironic Agent\nAfter=network-online.target\nWants=network-online.target\n[Service]\nEnvironment=\"HTTP_PROXY=\"\nEnvironment=\"HTTPS_PROXY=\"\nEnvironment=\"NO_PROXY=\"\nTimeoutStartSec=0\nExecStartPre=/bin/podman pull some-ironic-image --tls-verify=false --authfile=/etc/authfile.json\nExecStart=/bin/podman run --privileged --network host --mount type=bind,src=/etc/ironic-python-agent.conf,dst=/etc/ironic-python-agent/ignition.conf --mount type=bind,src=/dev,dst=/dev --mount type=bind,src=/sys,dst=/sys --mount type=bind,src=/run/dbus/system_bus_socket,dst=/run/dbus/system_bus_socket --mount type=bind,src=/,dst=/mnt/coreos --env \"IPA_COREOS_IP_OPTIONS=ip=dhcp\" --name ironic-agent somce-ironic-image\n[Install]\nWantedBy=multi-user.target\n", "enabled": true, "name": "ironic-agent.service" } ] } }`
		infraEnv.IgnitionConfigOverride = ironicIgn
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err = builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())
		Expect(text).Should(ContainSubstring("ironic-agent.service"))
		Expect(text).Should(ContainSubstring("ironic.redhat.com"))

		config2, report, err := config_32.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config2.Ignition.Version).To(Equal("3.2.0"))
		Expect(len(config2.Storage.Files)).To(Equal(numOfFiles + 1))
		Expect(len(config2.Systemd.Units)).To(Equal(numOfUnits + 1))
	})

	It("produces a valid ignition spec with v3.2 overrides", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config.Ignition.Version).To(Equal("3.1.0"))
		numOfFiles := len(config.Storage.Files)

		infraEnv.IgnitionConfigOverride = `{"ignition": {"version": "3.2.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err = builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())

		config2, report, err := config_32.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		Expect(config2.Ignition.Version).To(Equal("3.2.0"))
		Expect(len(config2.Storage.Files)).To(Equal(numOfFiles + 1))
	})

	It("fails when given overrides with an incompatible version", func() {
		infraEnv.IgnitionConfigOverride = `{"ignition": {"version": "2.2.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		_, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")

		Expect(err).To(HaveOccurred())
	})

	It("applies day2 overrides successfuly", func() {
		hostID := strfmt.UUID(uuid.New().String())
		cluster.Hosts = []*models.Host{{
			ID:                      &hostID,
			RequestedHostname:       "day2worker.example.com",
			Role:                    models.HostRoleWorker,
			IgnitionConfigOverrides: `{"ignition": {"version": "3.2.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`,
		}}
		serviceBaseURL := "http://10.56.20.70:7878"

		text, err := builder.FormatSecondDayWorkerIgnitionFile(serviceBaseURL, nil, "", cluster.Hosts[0])

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("/tmp/example"))
	})

	It("no multipath for okd - config setting", func() {
		config := IgnitionConfig{OKDRPMsImage: "image"}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, config, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).ShouldNot(ContainSubstring("multipathd"))
	})

	It("no multipath for okd - okd payload", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		okdNewImageVersion := "4.12.0-0.okd-2022-11-20-010424"
		okdNewImageURL := "registry.ci.openshift.org/origin/release:4.12.0-0.okd-2022-11-20-010424"
		okdNewImage := &models.ReleaseImage{
			CPUArchitecture:  &common.TestDefaultConfig.CPUArchitecture,
			OpenshiftVersion: &common.TestDefaultConfig.OpenShiftVersion,
			CPUArchitectures: []string{common.TestDefaultConfig.CPUArchitecture},
			URL:              &okdNewImageURL,
			Version:          &okdNewImageVersion,
		}
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(okdNewImage, nil).Times(1)
		mockOcRelease.EXPECT().GetOKDRPMSImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("quay.io/foo/bar:okd-rpms", nil)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).ShouldNot(ContainSubstring("multipathd"))
	})

	It("multipath configured for non-okd", func() {
		config := IgnitionConfig{}
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, config, false, auth.TypeRHSSO, "")

		Expect(err).Should(BeNil())
		Expect(text).Should(ContainSubstring("multipathd"))
	})

	Context("static network config", func() {
		formattedInput := "some formated input"
		staticnetworkConfigOutput := []staticnetworkconfig.StaticNetworkConfigData{
			{
				FilePath:     "nic10.nmconnection",
				FileContents: "nic10 nmconnection content",
			},
			{
				FilePath:     "nic20.nmconnection",
				FileContents: "nic10 nmconnection content",
			},
			{
				FilePath:     "mac_interface.ini",
				FileContents: "nic10=mac10\nnic20=mac20",
			},
		}

		It("produces a valid ignition v3.1 spec with static ips paramters", func() {
			mockStaticNetworkConfig.EXPECT().GenerateStaticNetworkConfigData(gomock.Any(), formattedInput).Return(staticnetworkConfigOutput, nil).Times(1)
			infraEnv.StaticNetworkConfig = formattedInput
			infraEnv.Type = common.ImageTypePtr(models.ImageTypeFullIso)
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
			Expect(err).NotTo(HaveOccurred())
			config, report, err := config_31.Parse([]byte(text))
			Expect(err).NotTo(HaveOccurred())
			Expect(report.IsFatal()).To(BeFalse())
			count := 0
			for _, f := range config.Storage.Files {
				if strings.HasSuffix(f.Path, "nmconnection") || strings.HasSuffix(f.Path, "mac_interface.ini") {
					count += 1
				}
			}
			Expect(count).Should(Equal(3))
		})
		It("Doesn't include static network config for minimal isos", func() {
			infraEnv.StaticNetworkConfig = formattedInput
			infraEnv.Type = common.ImageTypePtr(models.ImageTypeMinimalIso)
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
			Expect(err).NotTo(HaveOccurred())
			config, report, err := config_31.Parse([]byte(text))
			Expect(err).NotTo(HaveOccurred())
			Expect(report.IsFatal()).To(BeFalse())
			count := 0
			for _, f := range config.Storage.Files {
				if strings.HasSuffix(f.Path, "nmconnection") || strings.HasSuffix(f.Path, "mac_interface.ini") {
					count += 1
				}
			}
			Expect(count).Should(Equal(0))
		})

		It("Will include static network config for minimal iso type in infraenv if overridden in call to FormatDiscoveryIgnitionFile", func() {
			mockStaticNetworkConfig.EXPECT().GenerateStaticNetworkConfigData(gomock.Any(), formattedInput).Return(staticnetworkConfigOutput, nil).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			infraEnv.StaticNetworkConfig = formattedInput
			infraEnv.Type = common.ImageTypePtr(models.ImageTypeMinimalIso)
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, string(models.ImageTypeFullIso))
			Expect(err).NotTo(HaveOccurred())
			config, report, err := config_31.Parse([]byte(text))
			Expect(err).NotTo(HaveOccurred())
			Expect(report.IsFatal()).To(BeFalse())
			count := 0
			for _, f := range config.Storage.Files {
				if strings.HasSuffix(f.Path, "nmconnection") || strings.HasSuffix(f.Path, "mac_interface.ini") {
					count += 1
				}
			}
			Expect(count).Should(Equal(3))
		})

		It("Will not include static network config for full iso type in infraenv if overridden in call to FormatDiscoveryIgnitionFile", func() {
			infraEnv.StaticNetworkConfig = formattedInput
			infraEnv.Type = common.ImageTypePtr(models.ImageTypeFullIso)
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, string(models.ImageTypeMinimalIso))
			Expect(err).NotTo(HaveOccurred())
			config, report, err := config_31.Parse([]byte(text))
			Expect(err).NotTo(HaveOccurred())
			Expect(report.IsFatal()).To(BeFalse())
			count := 0
			for _, f := range config.Storage.Files {
				if strings.HasSuffix(f.Path, "nmconnection") || strings.HasSuffix(f.Path, "mac_interface.ini") {
					count += 1
				}
			}
			Expect(count).Should(Equal(0))
		})
	})

	Context("mirror registries config", func() {

		It("produce ignition with mirror registries config", func() {
			mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(true).Times(1)
			mockMirrorRegistriesConfigBuilder.EXPECT().GetMirrorCA().Return([]byte("some ca config"), nil).Times(1)
			mockMirrorRegistriesConfigBuilder.EXPECT().GetMirrorRegistries().Return([]byte("some mirror registries config"), nil).Times(1)
			mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
			text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
			Expect(err).NotTo(HaveOccurred())
			config, report, err := config_31.Parse([]byte(text))
			Expect(err).NotTo(HaveOccurred())
			Expect(report.IsFatal()).To(BeFalse())
			count := 0
			for _, f := range config.Storage.Files {
				if strings.HasSuffix(f.Path, "registries.conf") || strings.HasSuffix(f.Path, "domain.crt") {
					count += 1
				}
			}
			Expect(count).Should(Equal(2))
		})
	})

	It("Generates permissive `policy.json` by default", func() {
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(true).AnyTimes()
		mockMirrorRegistriesConfigBuilder.EXPECT().GetMirrorCA().Return([]byte("my-ca"), nil).AnyTimes()
		mockMirrorRegistriesConfigBuilder.EXPECT().GetMirrorRegistries().Return([]byte("my-mirror"), nil).AnyTimes()
		mockVersionHandler.EXPECT().GetReleaseImage(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Return(nil, errors.New("my-error")).AnyTimes()
		text, err := builder.FormatDiscoveryIgnitionFile(
			context.Background(),
			&infraEnv,
			IgnitionConfig{},
			false,
			auth.TypeRHSSO,
			"",
		)
		Expect(err).Should(BeNil())
		config, report, err := config_32.ParseCompatibleVersion([]byte(text))
		Expect(err).ToNot(HaveOccurred())
		Expect(report.Entries).To(BeEmpty())
		var policyFile *config_32_types.File
		for i, file := range config.Storage.Files {
			if file.Path == "/etc/containers/policy.json" {
				policyFile = &config.Storage.Files[i]
				break
			}
		}
		Expect(policyFile).ToNot(BeNil())
		Expect(policyFile.Mode).ToNot(BeNil())
		Expect(*policyFile.Mode).To(Equal(0644))
		Expect(policyFile.Contents.Source).ToNot(BeNil())
		policyData, err := dataurl.DecodeString(*policyFile.Contents.Source)
		Expect(err).ToNot(HaveOccurred())
		Expect(policyData.Data).To(MatchJSON(`{
			"default": [
				{
					"type": "insecureAcceptAnything"
				}
			],
			"transports": {
				"docker-daemon": {
					"": [
						{
							"type": "insecureAcceptAnything"
						}
					]
				}
			}
		}`))
	})

	It("Honors overriden `policy.json`", func() {
		infraEnv.IgnitionConfigOverride = `{
			"ignition": {
				"version":"3.2.0"
			},
			"storage": {
				"files": [
					{
						"path":"/etc/containers/policy.json",
						"mode":420,
						"overwrite":true,
						"contents": {
							"source": "data:text/plain;charset=utf-8;base64,bXktcG9saWN5"
						}
					}
				]
			}
		}`
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(true).AnyTimes()
		mockMirrorRegistriesConfigBuilder.EXPECT().GetMirrorCA().Return([]byte("my-ca"), nil).AnyTimes()
		mockMirrorRegistriesConfigBuilder.EXPECT().GetMirrorRegistries().Return([]byte("my-mirror"), nil).AnyTimes()
		mockVersionHandler.EXPECT().GetReleaseImage(
			gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
		).Return(nil, errors.New("my-error")).AnyTimes()
		text, err := builder.FormatDiscoveryIgnitionFile(
			context.Background(),
			&infraEnv,
			IgnitionConfig{},
			false,
			auth.TypeRHSSO,
			"",
		)
		Expect(err).Should(BeNil())
		config, report, err := config_32.ParseCompatibleVersion([]byte(text))
		Expect(err).ToNot(HaveOccurred())
		Expect(report.Entries).To(BeEmpty())
		var policyFile *config_32_types.File
		for i, file := range config.Storage.Files {
			if file.Path == "/etc/containers/policy.json" {
				policyFile = &config.Storage.Files[i]
				break
			}
		}
		Expect(policyFile).ToNot(BeNil())
		Expect(policyFile.Contents.Source).ToNot(BeNil())
		policyData, err := dataurl.DecodeString(*policyFile.Contents.Source)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(policyData.Data)).To(Equal("my-policy"))
	})
})

var _ = Describe("Ignition SSH key building", func() {
	var (
		ctrl                              *gomock.Controller
		infraEnv                          common.InfraEnv
		builder                           IgnitionBuilder
		mockStaticNetworkConfig           *staticnetworkconfig.MockStaticNetworkConfig
		mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
		infraEnvID                        strfmt.UUID
		mockOcRelease                     *oc.MockRelease
		mockVersionHandler                *versions.MockHandler
	)
	buildIgnitionAndAssertSubString := func(SSHPublicKey string, shouldExist bool, subStr string) {
		infraEnv.SSHAuthorizedKey = SSHPublicKey
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, IgnitionConfig{}, false, auth.TypeRHSSO, "")
		Expect(err).NotTo(HaveOccurred())
		if shouldExist {
			Expect(text).Should(ContainSubstring(subStr))
		} else {
			Expect(text).ShouldNot(ContainSubstring(subStr))
		}
	}

	BeforeEach(func() {
		infraEnvID = strfmt.UUID("a64fff36-dcb1-11ea-87d0-0242ac130003")
		ctrl = gomock.NewController(GinkgoT())
		mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		mockOcRelease = oc.NewMockRelease(ctrl)
		mockVersionHandler = versions.NewMockHandler(ctrl)
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("some error")).Times(1)
		infraEnv = common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:            &infraEnvID,
				PullSecretSet: false,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		//cluster.ImageInfo = &models.ImageInfo{}
		var err error
		builder, err = NewBuilder(log, mockStaticNetworkConfig, mockMirrorRegistriesConfigBuilder, mockOcRelease, mockVersionHandler)
		Expect(err).ToNot(HaveOccurred())
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("when empty or invalid input", func() {
		It("white_space_string should return an empty string", func() {
			buildIgnitionAndAssertSubString("  \n  \n \t \n  ", false, "sshAuthorizedKeys")
		})
		It("Empty string should return an empty string", func() {
			buildIgnitionAndAssertSubString("", false, "sshAuthorizedKeys")
		})
	})
	Context("when ssh key exists, escape when needed", func() {
		It("Single key without needed escaping", func() {
			buildIgnitionAndAssertSubString("ssh-rsa key coyote@acme.com", true, `"sshAuthorizedKeys":["ssh-rsa key coyote@acme.com"]`)
		})
		It("Multiple keys without needed escaping", func() {
			buildIgnitionAndAssertSubString("ssh-rsa key coyote@acme.com\nssh-rsa key2 coyote@acme.com",
				true,
				`"sshAuthorizedKeys":["ssh-rsa key coyote@acme.com","ssh-rsa key2 coyote@acme.com"]`)
		})
		It("Single key with escaping", func() {
			buildIgnitionAndAssertSubString(`ssh-rsa key coyote\123@acme.com`, true, `"sshAuthorizedKeys":["ssh-rsa key coyote\\123@acme.com"]`)
		})
		It("Multiple keys with escaping", func() {
			buildIgnitionAndAssertSubString(`ssh-rsa key coyote\123@acme.com
			ssh-rsa key2 coyote@acme.com`,
				true,
				`"sshAuthorizedKeys":["ssh-rsa key coyote\\123@acme.com","ssh-rsa key2 coyote@acme.com"]`)
		})
		It("Multiple keys with escaping and white space", func() {
			buildIgnitionAndAssertSubString(`
			ssh-rsa key coyote\123@acme.com

			ssh-rsa key2 c\0899oyote@acme.com
			`, true, `"sshAuthorizedKeys":["ssh-rsa key coyote\\123@acme.com","ssh-rsa key2 c\\0899oyote@acme.com"]`)
		})
	})
})

var _ = Describe("FormatSecondDayWorkerIgnitionFile", func() {

	var (
		ctrl                              *gomock.Controller
		log                               logrus.FieldLogger
		builder                           IgnitionBuilder
		mockStaticNetworkConfig           *staticnetworkconfig.MockStaticNetworkConfig
		mockMirrorRegistriesConfigBuilder *mirrorregistries.MockMirrorRegistriesConfigBuilder
		mockHost                          *models.Host
		mockOcRelease                     *oc.MockRelease
		mockVersionHandler                *versions.MockHandler
	)

	BeforeEach(func() {
		log = common.GetTestLog()
		ctrl = gomock.NewController(GinkgoT())
		mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		mockHost = &models.Host{Inventory: hostInventory}
		var err error
		builder, err = NewBuilder(log, mockStaticNetworkConfig, mockMirrorRegistriesConfigBuilder, mockOcRelease, mockVersionHandler)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("test custom ignition endpoint", func() {

		It("are rendered properly without ca cert and token", func() {
			ign, err := builder.FormatSecondDayWorkerIgnitionFile("http://url.com", nil, "", mockHost)
			Expect(err).NotTo(HaveOccurred())

			ignConfig, _, err := config_31.Parse(ign)
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].Source)).Should(Equal("http://url.com"))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders).Should(HaveLen(0))
			Expect(ignConfig.Ignition.Security.TLS.CertificateAuthorities).Should(HaveLen(0))
		})

		It("are rendered properly with token", func() {
			token := "xyzabc123"
			ign, err := builder.FormatSecondDayWorkerIgnitionFile("http://url.com", nil, token, mockHost)
			Expect(err).NotTo(HaveOccurred())

			ignConfig, _, err := config_31.Parse(ign)
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].Source)).Should(Equal("http://url.com"))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders).Should(HaveLen(1))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders[0].Name).Should(Equal("Authorization"))
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].HTTPHeaders[0].Value)).Should(Equal("Bearer " + token))
			Expect(ignConfig.Ignition.Security.TLS.CertificateAuthorities).Should(HaveLen(0))
		})

		It("are rendered properly with ca cert", func() {
			ca := "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
				"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
				"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
			encodedCa := base64.StdEncoding.EncodeToString([]byte(ca))
			ign, err := builder.FormatSecondDayWorkerIgnitionFile("https://url.com", &encodedCa, "", mockHost)
			Expect(err).NotTo(HaveOccurred())

			ignConfig, _, err := config_31.Parse(ign)
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].Source)).Should(Equal("https://url.com"))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders).Should(HaveLen(0))
			Expect(ignConfig.Ignition.Security.TLS.CertificateAuthorities).Should(HaveLen(1))
			Expect(swag.StringValue(ignConfig.Ignition.Security.TLS.CertificateAuthorities[0].Source)).Should(Equal("data:text/plain;base64," + encodedCa))
		})

		It("are rendered properly with ca cert and token", func() {
			token := "xyzabc123"
			ca := "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
				"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
				"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
			encodedCa := base64.StdEncoding.EncodeToString([]byte(ca))
			ign, err := builder.FormatSecondDayWorkerIgnitionFile("https://url.com", &encodedCa, token, mockHost)

			Expect(err).NotTo(HaveOccurred())

			ignConfig, _, err := config_31.Parse(ign)
			Expect(err).NotTo(HaveOccurred())
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].Source)).Should(Equal("https://url.com"))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders).Should(HaveLen(1))
			Expect(ignConfig.Ignition.Config.Merge[0].HTTPHeaders[0].Name).Should(Equal("Authorization"))
			Expect(swag.StringValue(ignConfig.Ignition.Config.Merge[0].HTTPHeaders[0].Value)).Should(Equal("Bearer " + token))
			Expect(ignConfig.Ignition.Security.TLS.CertificateAuthorities).Should(HaveLen(1))
			Expect(swag.StringValue(ignConfig.Ignition.Security.TLS.CertificateAuthorities[0].Source)).Should(Equal("data:text/plain;base64," + encodedCa))
		})
	})
})

var _ = Describe("Import Cluster TLS Certs for ephemeral installer", func() {
	var (
		certDir string
		dbName  string
		db      *gorm.DB
	)

	certFiles := []string{"test-cert.crt", "test-cert.key"}

	BeforeEach(func() {
		var err error
		certDir, err = os.MkdirTemp("", "assisted-install-cluster-tls-certs-test-")
		Expect(err).NotTo(HaveOccurred())

		for _, cf := range certFiles {
			err = os.WriteFile(filepath.Join(certDir, cf), []byte(cf), 0600)
			Expect(err).NotTo(HaveOccurred())
		}
		Expect(err).NotTo(HaveOccurred())
		db, dbName = common.PrepareTestDB()
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("copies the tls cert files", func() {
		g := NewGenerator("", workDir, installerCacheDir, cluster, "", "", "", "", nil, log,
			mockOperatorManager, mockProviderRegistry, "", certDir).(*installerGenerator)

		err := g.importClusterTLSCerts(context.Background())
		Expect(err).NotTo(HaveOccurred())

		for _, cf := range certFiles {
			content, err := os.ReadFile(filepath.Join(workDir, "tls", cf))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(cf))
		}
	})
})

var _ = Describe("ICSP file for oc extract", func() {

	It("valid icsp contents", func() {
		var cfg installcfg.InstallerConfigBaremetal
		expected := "apiVersion: operator.openshift.io/v1alpha1\nkind: ImageContentSourcePolicy\nmetadata:\n  creationTimestamp: null\n  name: image-policy\nspec:\n  repositoryDigestMirrors:\n  - mirrors:\n    - mirrorhost1.example.org:5000/localimages\n    - mirrorhost2.example.org:5000/localimages\n    source: registry.ci.org\n  - mirrors:\n    - mirrorhost1.example.org:5000/localimages\n    - mirrorhost2.example.org:5000/localimages\n    source: quay.io\n"

		imageContentSourceList := make([]installcfg.ImageContentSource, 2)
		imageContentSourceList[0] = installcfg.ImageContentSource{
			Source:  "registry.ci.org",
			Mirrors: []string{"mirrorhost1.example.org:5000/localimages", "mirrorhost2.example.org:5000/localimages"},
		}
		imageContentSourceList[1] = installcfg.ImageContentSource{
			Source:  "quay.io",
			Mirrors: []string{"mirrorhost1.example.org:5000/localimages", "mirrorhost2.example.org:5000/localimages"},
		}
		cfg.ImageContentSources = imageContentSourceList
		data, err := yaml.Marshal(&cfg)
		Expect(err).ShouldNot(HaveOccurred())
		contents, err := getIcsp(data)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(string(contents)).Should(Equal(expected))
	})

	It("no image source contents defined", func() {
		var cfg installcfg.InstallerConfigBaremetal
		expected := ""

		data, err := yaml.Marshal(&cfg)
		Expect(err).ShouldNot(HaveOccurred())
		contents, err := getIcsp(data)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(string(contents)).Should(Equal(expected))
	})

	It("valid file created and readable", func() {
		var cfg installcfg.InstallerConfigBaremetal
		expected := "apiVersion: operator.openshift.io/v1alpha1\nkind: ImageContentSourcePolicy\nmetadata:\n  creationTimestamp: null\n  name: image-policy\nspec:\n  repositoryDigestMirrors:\n  - mirrors:\n    - mirrorhost1.example.org:5000/localimages\n    - mirrorhost2.example.org:5000/localimages\n    source: registry.ci.org\n"

		imageContentSourceList := make([]installcfg.ImageContentSource, 1)
		imageContentSourceList[0] = installcfg.ImageContentSource{
			Source:  "registry.ci.org",
			Mirrors: []string{"mirrorhost1.example.org:5000/localimages", "mirrorhost2.example.org:5000/localimages"},
		}
		cfg.ImageContentSources = imageContentSourceList
		data, err := yaml.Marshal(&cfg)
		Expect(err).ShouldNot(HaveOccurred())
		icspFile, err := getIcspFileFromInstallConfig(data, log)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(icspFile).Should(BeARegularFile())

		contents, err := os.ReadFile(icspFile)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(string(contents)).Should(Equal(expected))
	})

	It("filename is empty string", func() {
		var cfg installcfg.InstallerConfigBaremetal
		expected := ""

		data, err := yaml.Marshal(&cfg)
		Expect(err).ShouldNot(HaveOccurred())
		icspFile, err := getIcspFileFromInstallConfig(data, log)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(icspFile).Should(Equal(expected))
	})
})

var _ = Describe("infrastructureCRPatch", func() {
	var (
		ctrl         *gomock.Controller
		mockS3Client *s3wrapper.MockAPI
		generator    *installerGenerator
		ctx          = context.Background()
	)

	BeforeEach(func() {
		cluster.Hosts = []*models.Host{
			{
				Inventory:         hostInventory,
				RequestedHostname: "example0",
				Role:              models.HostRoleMaster,
			},
			{
				Inventory:         hostInventory,
				RequestedHostname: "example1",
				Role:              models.HostRoleMaster,
			},
			{
				Inventory:         hostInventory,
				RequestedHostname: "example2",
				Role:              models.HostRoleMaster,
			},
			{
				Inventory:         hostInventory,
				RequestedHostname: "example3",
				Role:              models.HostRoleWorker,
			},
		}

		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		generator = &installerGenerator{
			log:      log,
			workDir:  workDir,
			s3Client: mockS3Client,
			cluster:  cluster,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("doesn't patch the infrastructure manifest for SNO", func() {
		base := `---
apiVersion: config.openshift.io/v1
kind: Infrastructure
metadata:
  creationTimestamp: "2022-03-29T10:42:09Z"
  generation: 1
  name: cluster
  resourceVersion: "596"
  uid: cc91565d-997f-441c-b28a-d2915c4afd84
spec:
  cloudConfig:
    name: ""
  platformSpec:
    type: None
status:
  apiServerInternalURI: https://api-int.test-cluster.redhat.com:6443
  apiServerURL: https://api.test-cluster.redhat.com:6443
  controlPlaneTopology: SingleReplica
  etcdDiscoveryDomain: ""
  infrastructureName: test-cluster-s9qbn
  infrastructureTopology: SingleReplica
  platform: None
  platformStatus:
    type: None`

		manifestsDir := filepath.Join(workDir, "/manifests")
		Expect(os.Mkdir(manifestsDir, 0755)).To(Succeed())

		err := os.WriteFile(filepath.Join(manifestsDir, "cluster-infrastructure-02-config.yml"), []byte(base), 0600)
		Expect(err).NotTo(HaveOccurred())

		// remove one host to make sure this is a 5 node cluster
		cluster.Hosts = []*models.Host{cluster.Hosts[0]}
		Expect(generator.applyInfrastructureCRPatch(ctx)).To(Succeed())

		content, err := os.ReadFile(filepath.Join(manifestsDir, "cluster-infrastructure-02-config.yml"))
		Expect(err).NotTo(HaveOccurred())

		merged := map[string]interface{}{}
		err = yaml.Unmarshal(content, &merged)
		Expect(err).NotTo(HaveOccurred())

		status := merged["status"].(map[interface{}]interface{})
		// the infra topology should now match the overwrite
		Expect(status["infrastructureTopology"].(string)).To(Equal("SingleReplica"))

		// the infra name should not have changed
		Expect(status["infrastructureName"].(string)).To(Equal("test-cluster-s9qbn"))
	})
	It("patches the infrastructure manifest correctly", func() {
		base := `---
apiVersion: config.openshift.io/v1
kind: Infrastructure
metadata:
  creationTimestamp: "2022-03-29T10:42:09Z"
  generation: 1
  name: cluster
  resourceVersion: "596"
  uid: cc91565d-997f-441c-b28a-d2915c4afd84
spec:
  cloudConfig:
    name: ""
  platformSpec:
    type: None
status:
  apiServerInternalURI: https://api-int.test-cluster.redhat.com:6443
  apiServerURL: https://api.test-cluster.redhat.com:6443
  controlPlaneTopology: SingleReplica
  etcdDiscoveryDomain: ""
  infrastructureName: test-cluster-s9qbn
  infrastructureTopology: SingleReplica
  platform: None
  platformStatus:
    type: None`

		manifestsDir := filepath.Join(workDir, "/manifests")
		Expect(os.Mkdir(manifestsDir, 0755)).To(Succeed())

		err := os.WriteFile(filepath.Join(manifestsDir, "cluster-infrastructure-02-config.yml"), []byte(base), 0600)
		Expect(err).NotTo(HaveOccurred())

		Expect(generator.applyInfrastructureCRPatch(ctx)).To(Succeed())

		content, err := os.ReadFile(filepath.Join(manifestsDir, "cluster-infrastructure-02-config.yml"))
		Expect(err).NotTo(HaveOccurred())

		merged := map[string]interface{}{}
		err = yaml.Unmarshal(content, &merged)
		Expect(err).NotTo(HaveOccurred())

		status := merged["status"].(map[interface{}]interface{})
		// the infra topology should now match the overwrite
		Expect(status["infrastructureTopology"].(string)).To(Equal("HighlyAvailable"))

		// the infra name should not have changed
		Expect(status["infrastructureName"].(string)).To(Equal("test-cluster-s9qbn"))
	})

	It("patches core manifests correctly", func() {
		base := `---
apiVersion: config.openshift.io/v1
kind: Scheduler
metadata:
  creationTimestamp: "2022-11-03T06:20:17Z"
  generation: 1
  name: cluster
  resourceVersion: "620"
  uid: b74da926-8664-41a2-8fbf-4217156a63c6
spec:
  mastersSchedulable: false
  policy:
    name: ""`

		schedulableMastersManifestPatch := `---
- op: replace
  path: /spec/mastersSchedulable
  value: true
`
		schedulerPatchCustomTest := `---
- op: add
  path: /spec/customTest
  value: true
`

		manifestsOpenshiftDir := filepath.Join(workDir, "/openshift")
		Expect(os.Mkdir(manifestsOpenshiftDir, 0755)).To(Succeed())

		manifestPatchPath := filepath.Join(manifestsOpenshiftDir, "cluster-scheduler-02-config.yml.patch")
		err := os.WriteFile(manifestPatchPath, []byte(schedulableMastersManifestPatch), 0600)
		Expect(err).NotTo(HaveOccurred())

		manifestPatchCustomTestPath := filepath.Join(manifestsOpenshiftDir, "cluster-scheduler-02-config.yml.patch_custom_test")
		err = os.WriteFile(manifestPatchCustomTestPath, []byte(schedulerPatchCustomTest), 0600)
		Expect(err).NotTo(HaveOccurred())

		manifestsDir := filepath.Join(workDir, "/manifests")
		Expect(os.Mkdir(manifestsDir, 0755)).To(Succeed())

		err = os.WriteFile(filepath.Join(manifestsDir, "cluster-scheduler-02-config.yml"), []byte(base), 0600)
		Expect(err).NotTo(HaveOccurred())

		Expect(generator.applyManifestPatches(ctx)).To(Succeed())

		content, err := os.ReadFile(filepath.Join(manifestsDir, "cluster-scheduler-02-config.yml"))
		Expect(err).NotTo(HaveOccurred())

		merged := map[string]interface{}{}
		err = yaml.Unmarshal(content, &merged)
		Expect(err).NotTo(HaveOccurred())

		status := merged["spec"].(map[interface{}]interface{})

		// Master should now be schedulable
		Expect(status["mastersSchedulable"].(bool)).To(Equal(true))
		Expect(status["customTest"].(bool)).To(Equal(true))

		_, err = os.Stat(manifestPatchPath)
		Expect(errors.Is(err, os.ErrNotExist)).To(Equal(true))

		_, err = os.Stat(manifestPatchCustomTestPath)
		Expect(errors.Is(err, os.ErrNotExist)).To(Equal(true))
	})
})

var _ = Describe("Set kubelet node ip", func() {
	var (
		ctrl         *gomock.Controller
		mockS3Client *s3wrapper.MockAPI
		generator    *installerGenerator
		basicEnvVars []string
		err          error
	)

	BeforeEach(func() {

		bootstrapInventory := `{"bmc_address":"0.0.0.0","bmc_v6address":"::/0","boot":{"current_boot_mode":"bios"},"cpu":{"architecture":"x86_64","count":4,"flags":["fpu","vme","de","pse","tsc","msr","pae","mce","cx8","apic","sep","mtrr","pge","mca","cmov","pat","pse36","clflush","mmx","fxsr","sse","sse2","ss","syscall","nx","pdpe1gb","rdtscp","lm","constant_tsc","arch_perfmon","rep_good","nopl","xtopology","cpuid","tsc_known_freq","pni","pclmulqdq","vmx","ssse3","fma","cx16","pcid","sse4_1","sse4_2","x2apic","movbe","popcnt","tsc_deadline_timer","aes","xsave","avx","f16c","rdrand","hypervisor","lahf_lm","abm","3dnowprefetch","cpuid_fault","invpcid_single","pti","ssbd","ibrs","ibpb","stibp","tpr_shadow","vnmi","flexpriority","ept","vpid","ept_ad","fsgsbase","tsc_adjust","bmi1","hle","avx2","smep","bmi2","erms","invpcid","rtm","mpx","avx512f","avx512dq","rdseed","adx","smap","clflushopt","clwb","avx512cd","avx512bw","avx512vl","xsaveopt","xsavec","xgetbv1","xsaves","arat","umip","pku","ospke","md_clear","arch_capabilities"],"frequency":2095.076,"model_name":"Intel(R) Xeon(R) Gold 6152 CPU @ 2.10GHz"},"disks":[{"by_path":"/dev/disk/by-path/pci-0000:00:06.0","drive_type":"HDD","model":"unknown","name":"vda","path":"/dev/vda","serial":"unknown","size_bytes":21474836480,"vendor":"0x1af4","wwn":"unknown"}],"hostname":"test-infra-cluster-master-1.redhat.com","interfaces":[{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["192.168.126.10/24"],"ipv6_addresses":["fe80::5054:ff:fe42:1e8d/64"],"mac_address":"52:54:00:42:1e:8d","mtu":1500,"name":"eth0","product":"0x0001","speed_mbps":-1,"vendor":"0x1af4"},{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["192.168.140.133/24"],"ipv6_addresses":["fe80::5054:ff:feca:7b16/64"],"mac_address":"52:54:00:ca:7b:16","mtu":1500,"name":"eth1","product":"0x0001","speed_mbps":-1,"vendor":"0x1af4"}],"memory":{"physical_bytes":17809014784,"usable_bytes":17378611200},"system_vendor":{"manufacturer":"Red Hat","product_name":"KVM"}}`
		cluster.MachineNetworks = []*models.MachineNetwork{{Cidr: "192.168.126.0/24"}, {Cidr: "192.168.140.0/24"}}
		cluster.Hosts = []*models.Host{
			{
				Inventory:         bootstrapInventory,
				RequestedHostname: "example0",
				Role:              models.HostRoleMaster,
				Bootstrap:         true,
			},
			{
				Inventory:         hostInventory,
				RequestedHostname: "example1",
				Role:              models.HostRoleMaster,
			},
		}
		basicEnvVars = []string{"OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE=test"}

		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		generator = &installerGenerator{
			log:      log,
			workDir:  workDir,
			s3Client: mockS3Client,
			cluster:  cluster,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("multi node bootstrap kubelet ip", func() {
		basicEnvVars, err = generator.addBootstrapKubeletIpIfRequired(generator.log, basicEnvVars)
		Expect(err).NotTo(HaveOccurred())
		Expect(basicEnvVars).Should(ContainElement("OPENSHIFT_INSTALL_BOOTSTRAP_NODE_IP=192.168.126.10"))
	})
	It("sno bootstrap kubelet ip", func() {
		cluster.UserManagedNetworking = swag.Bool(true)
		cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
		basicEnvVars, err = generator.addBootstrapKubeletIpIfRequired(generator.log, basicEnvVars)
		Expect(err).NotTo(HaveOccurred())
		Expect(basicEnvVars).Should(ContainElement("OPENSHIFT_INSTALL_BOOTSTRAP_NODE_IP=192.168.126.10"))
	})
	It("UMN platform bootstrap kubelet ip should not be set", func() {
		cluster.UserManagedNetworking = swag.Bool(true)
		cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeFull)
		basicEnvVars, err = generator.addBootstrapKubeletIpIfRequired(generator.log, basicEnvVars)
		Expect(err).NotTo(HaveOccurred())
		Expect(basicEnvVars).ShouldNot(ContainElement("OPENSHIFT_INSTALL_BOOTSTRAP_NODE_IP=192.168.126.10"))
		Expect(len(basicEnvVars)).Should(Equal(1))
	})
	It("should fail if no machine networks exists", func() {
		cluster.MachineNetworks = []*models.MachineNetwork{}
		basicEnvVars, err = generator.addBootstrapKubeletIpIfRequired(generator.log, basicEnvVars)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("OKD overrides", func() {
	var (
		ctrl                               *gomock.Controller
		infraEnv                           common.InfraEnv
		builder                            IgnitionBuilder
		mockStaticNetworkConfig            *staticnetworkconfig.MockStaticNetworkConfig
		mockMirrorRegistriesConfigBuilder  *mirrorregistries.MockMirrorRegistriesConfigBuilder
		infraEnvID                         strfmt.UUID
		mockOcRelease                      *oc.MockRelease
		mockVersionHandler                 *versions.MockHandler
		ocpImage, okdOldImage, okdNewImage *models.ReleaseImage
		defaultCfg, okdCfg                 IgnitionConfig
	)

	BeforeEach(func() {
		infraEnvID = strfmt.UUID("a64fff36-dcb1-11ea-87d0-0242ac130003")
		ctrl = gomock.NewController(GinkgoT())
		mockStaticNetworkConfig = staticnetworkconfig.NewMockStaticNetworkConfig(ctrl)
		mockMirrorRegistriesConfigBuilder = mirrorregistries.NewMockMirrorRegistriesConfigBuilder(ctrl)
		mockVersionHandler = versions.NewMockHandler(ctrl)
		mockOperatorManager = operators.NewMockAPI(ctrl)
		mockOcRelease = oc.NewMockRelease(ctrl)
		infraEnv = common.InfraEnv{
			InfraEnv: models.InfraEnv{
				ID:            &infraEnvID,
				PullSecretSet: false,
			},
			PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		}
		var err error
		builder, err = NewBuilder(log, mockStaticNetworkConfig, mockMirrorRegistriesConfigBuilder, mockOcRelease, mockVersionHandler)
		Expect(err).ToNot(HaveOccurred())
		mockMirrorRegistriesConfigBuilder.EXPECT().IsMirrorRegistriesConfigured().Return(false).Times(1)
		ocpImage = common.TestDefaultConfig.ReleaseImage
		okdOldImageVersion := "4.11.0-0.okd-2022-11-19-050030"
		okdOldImageURL := "quay.io/openshift/okd:4.11.0-0.okd-2022-11-19-050030"
		okdOldImage = &models.ReleaseImage{
			CPUArchitecture:  &common.TestDefaultConfig.CPUArchitecture,
			OpenshiftVersion: &common.TestDefaultConfig.OpenShiftVersion,
			CPUArchitectures: []string{common.TestDefaultConfig.CPUArchitecture},
			URL:              &okdOldImageURL,
			Version:          &okdOldImageVersion,
		}
		okdNewImageVersion := "4.12.0-0.okd-2022-11-20-010424"
		okdNewImageURL := "registry.ci.openshift.org/origin/release:4.12.0-0.okd-2022-11-20-010424"
		okdNewImage = &models.ReleaseImage{
			CPUArchitecture:  &common.TestDefaultConfig.CPUArchitecture,
			OpenshiftVersion: &common.TestDefaultConfig.OpenShiftVersion,
			CPUArchitectures: []string{common.TestDefaultConfig.CPUArchitecture},
			URL:              &okdNewImageURL,
			Version:          &okdNewImageVersion,
		}
		defaultCfg = IgnitionConfig{}
		okdCfg = IgnitionConfig{
			OKDRPMsImage: "quay.io/okd/foo:bar",
		}
	})

	checkOKDFiles := func(text string, err error, present bool) {
		Expect(err).NotTo(HaveOccurred())
		config, report, err := config_31.Parse([]byte(text))
		Expect(err).NotTo(HaveOccurred())
		Expect(report.IsFatal()).To(BeFalse())
		count := 0
		for _, f := range config.Storage.Files {
			if f.Path == "/usr/local/bin/okd-binaries.sh" {
				count += 1
				continue
			}
			if f.Path == "/etc/systemd/system/release-image-pivot.service.d/wait-for-okd.conf" {
				count += 1
				continue
			}
			if f.Path == "/etc/systemd/system/agent.service.d/wait-for-okd.conf" {
				count += 1
				continue
			}
		}
		if present {
			Expect(count).Should(Equal(3))
		} else {
			Expect(count).Should(Equal(0))
		}
	}

	AfterEach(func() {
		ctrl.Finish()
	})

	It("OKD_RPMS config option unset", func() {
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(ocpImage, nil).Times(1)
		mockOcRelease.EXPECT().GetOKDRPMSImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("some error"))
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, defaultCfg, false, auth.TypeRHSSO, string(models.ImageTypeMinimalIso))
		checkOKDFiles(text, err, false)
	})
	It("OKD_RPMS config option not set, OKD release has no RPM image", func() {
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(okdOldImage, nil).Times(1)
		mockOcRelease.EXPECT().GetOKDRPMSImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("", errors.New("some error"))
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, defaultCfg, false, auth.TypeRHSSO, string(models.ImageTypeMinimalIso))
		checkOKDFiles(text, err, false)
	})
	It("OKD_RPMS config option set, OKD release has no RPM image", func() {
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, okdCfg, false, auth.TypeRHSSO, string(models.ImageTypeMinimalIso))
		checkOKDFiles(text, err, true)
	})
	It("OKD_RPMS config option not set, RPM image present in release payload", func() {
		mockVersionHandler.EXPECT().GetReleaseImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(okdNewImage, nil).Times(1)
		mockOcRelease.EXPECT().GetOKDRPMSImage(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return("quay.io/foo/bar:okd-rpms", nil)
		text, err := builder.FormatDiscoveryIgnitionFile(context.Background(), &infraEnv, defaultCfg, false, auth.TypeRHSSO, string(models.ImageTypeMinimalIso))
		checkOKDFiles(text, err, true)
	})
})

var _ = Describe("Bare metal host generation", func() {
	DescribeTable(
		"Selects IP addresse within cluster machine network",
		func(firstAddress, secondAddress string) {
			// Create the generator:
			generator := NewGenerator(
				"",
				workDir,
				installerCacheDir,
				cluster,
				"",
				"",
				"",
				"",
				nil,
				log,
				mockOperatorManager,
				mockProviderRegistry,
				"",
				"",
			).(*installerGenerator)

			// The default host inventory used by these tests has two NICs, each with
			// one IP address, but for this test we need one NIC with two IP addresses,
			// so we need to update the inventory accordingly.
			var inventoryObject models.Inventory
			err := json.Unmarshal([]byte(hostInventory), &inventoryObject)
			Expect(err).ToNot(HaveOccurred())
			Expect(inventoryObject.Interfaces).ToNot(BeEmpty())
			interfaceObject := inventoryObject.Interfaces[0]
			interfaceObject.IPV4Addresses = []string{
				firstAddress,
				secondAddress,
			}
			inventoryObject.Interfaces = []*models.Interface{
				interfaceObject,
			}
			inventoryJSON, err := json.Marshal(inventoryObject)
			Expect(err).ToNot(HaveOccurred())
			host := &models.Host{
				Inventory: string(inventoryJSON),
			}

			// Generate the bare metal hosts:
			inputObject := &bmh_v1alpha1.BareMetalHost{}
			outputFile := &config_32_types.File{}
			err = generator.modifyBMHFile(outputFile, inputObject, host)
			Expect(err).ToNot(HaveOccurred())
			outputObject, err := fileToBMH(outputFile)
			Expect(err).ToNot(HaveOccurred())

			// Extract the content of the status annotation:
			Expect(outputObject.Annotations).To(HaveKey(bmh_v1alpha1.StatusAnnotation))
			statusAnnotation := outputObject.Annotations[bmh_v1alpha1.StatusAnnotation]
			var outputStatus bmh_v1alpha1.BareMetalHostStatus
			err = json.Unmarshal([]byte(statusAnnotation), &outputStatus)
			Expect(err).ToNot(HaveOccurred())

			// Check that the IP address of the bare metal host is within the machine
			// network of the cluster:
			Expect(outputStatus.HardwareDetails).ToNot(BeNil())
			Expect(outputStatus.HardwareDetails.NIC).To(HaveLen(1))
			Expect(outputStatus.HardwareDetails.NIC[0].IP).To(Equal("192.168.126.11"))
		},
		Entry(
			"Lucky order in inventory",
			"192.168.126.11/24",
			"192.168.140.133/24",
		),
		Entry(
			"Unlucky oder in inventory",
			"192.168.140.133/24",
			"192.168.126.11/24",
		),
	)
})
