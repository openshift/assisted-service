package ignition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"gorm.io/gorm"
)

var hostInventory = `{"bmc_address":"0.0.0.0","bmc_v6address":"::/0","boot":{"current_boot_mode":"bios"},"cpu":{"architecture":"x86_64","count":4,"flags":["fpu","vme","de","pse","tsc","msr","pae","mce","cx8","apic","sep","mtrr","pge","mca","cmov","pat","pse36","clflush","mmx","fxsr","sse","sse2","ss","syscall","nx","pdpe1gb","rdtscp","lm","constant_tsc","arch_perfmon","rep_good","nopl","xtopology","cpuid","tsc_known_freq","pni","pclmulqdq","vmx","ssse3","fma","cx16","pcid","sse4_1","sse4_2","x2apic","movbe","popcnt","tsc_deadline_timer","aes","xsave","avx","f16c","rdrand","hypervisor","lahf_lm","abm","3dnowprefetch","cpuid_fault","invpcid_single","pti","ssbd","ibrs","ibpb","stibp","tpr_shadow","vnmi","flexpriority","ept","vpid","ept_ad","fsgsbase","tsc_adjust","bmi1","hle","avx2","smep","bmi2","erms","invpcid","rtm","mpx","avx512f","avx512dq","rdseed","adx","smap","clflushopt","clwb","avx512cd","avx512bw","avx512vl","xsaveopt","xsavec","xgetbv1","xsaves","arat","umip","pku","ospke","md_clear","arch_capabilities"],"frequency":2095.076,"model_name":"Intel(R) Xeon(R) Gold 6152 CPU @ 2.10GHz"},"disks":[{"by_path":"/dev/disk/by-path/pci-0000:00:06.0","drive_type":"HDD","model":"unknown","name":"vda","path":"/dev/vda","serial":"unknown","size_bytes":21474836480,"vendor":"0x1af4","wwn":"unknown"}],"hostname":"test-infra-cluster-master-1.redhat.com","interfaces":[{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["192.168.126.11/24"],"ipv6_addresses":["fe80::5054:ff:fe42:1e8d/64"],"mac_address":"52:54:00:42:1e:8d","mtu":1500,"name":"eth0","product":"0x0001","speed_mbps":-1,"vendor":"0x1af4"},{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["192.168.140.133/24"],"ipv6_addresses":["fe80::5054:ff:feca:7b16/64"],"mac_address":"52:54:00:ca:7b:16","mtu":1500,"name":"eth1","product":"0x0001","speed_mbps":-1,"vendor":"0x1af4"}],"memory":{"physical_bytes":17809014784,"usable_bytes":17378611200},"system_vendor":{"manufacturer":"Red Hat","product_name":"KVM"}}`

func testCluster() *common.Cluster {
	clusterID := strfmt.UUID(uuid.New().String())
	return &common.Cluster{
		PullSecret: "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dG9rZW46dGVzdAo=\",\"email\":\"coyote@acme.com\"}}}",
		Cluster: models.Cluster{
			ID: &clusterID,
			MachineNetworks: []*models.MachineNetwork{{
				Cidr: "192.168.126.11/24",
			}},
		},
	}
}

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
		workDir      string
		cluster      *common.Cluster
		ctrl         *gomock.Controller
	)

	BeforeEach(func() {
		// setup temp workdir
		workDir, err = os.MkdirTemp("", "bootstrap-ignition-update-test-")
		Expect(err).NotTo(HaveOccurred())
		examplePath = filepath.Join(workDir, "example1.ign")
		var err1 error
		err1 = os.WriteFile(examplePath, []byte(bootstrap1), 0600)
		Expect(err1).NotTo(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)

		cluster = testCluster()
		cluster.Hosts = []*models.Host{
			{
				Inventory:         hostInventory,
				RequestedHostname: "example1",
				Role:              models.HostRoleMaster,
			},
		}
		db, dbName = common.PrepareTestDB()
		g := NewGenerator("", workDir, "", cluster, "", "", "", "", mockS3Client, logrus.New(), nil, "", "", 5).(*installerGenerator)

		Expect(g.updateBootstrap(context.Background(), examplePath)).To(Succeed())

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
		os.RemoveAll(workDir)
		common.DeleteTestDB(db, dbName)
		ctrl.Finish()
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
		cluster    *common.Cluster
		workDir    string
	)

	BeforeEach(func() {
		var err error
		workDir, err = os.MkdirTemp("", "cluster-ignition-update-test-")
		Expect(err).NotTo(HaveOccurred())

		masterPath = filepath.Join(workDir, "master.ign")
		workerPath = filepath.Join(workDir, "worker.ign")
		err = os.WriteFile(masterPath, []byte(ignition), 0600)
		Expect(err).NotTo(HaveOccurred())
		err = os.WriteFile(workerPath, []byte(ignition), 0600)
		Expect(err).NotTo(HaveOccurred())

		caCertPath = filepath.Join(workDir, "service-ca-cert.crt")
		err = os.WriteFile(caCertPath, []byte(caCert), 0600)
		Expect(err).NotTo(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		cluster = testCluster()
	})

	AfterEach(func() {
		os.RemoveAll(workDir)
		common.DeleteTestDB(db, dbName)
	})

	Describe("update ignitions", func() {
		It("with ca cert file", func() {
			g := NewGenerator("", workDir, "", cluster, "", "", caCertPath, "", nil, logrus.New(), nil, "", "", 5).(*installerGenerator)

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
			g := NewGenerator("", workDir, "", cluster, "", "", "", "", nil, logrus.New(), nil, "", "", 5).(*installerGenerator)

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
			g := NewGenerator("", workDir, "", cluster, "", "", "", "", nil, logrus.New(), nil, "", "", 5).(*installerGenerator)

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
			g := NewGenerator("", workDir, "", cluster, "", "", "", "", nil, logrus.New(), nil, "", "", 5).(*installerGenerator)

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
		Context("DHCP generation", func() {
			It("Definitions only", func() {
				g := NewGenerator("", workDir, "", cluster, "", "", "", "", nil, logrus.New(), nil, "", "", 5).(*installerGenerator)

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
			g := NewGenerator("", workDir, "", cluster, "", "", "", "", nil, logrus.New(), nil, "", "", 5).(*installerGenerator)

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
		cluster      *common.Cluster
		ctrl         *gomock.Controller
		workDir      string
	)

	BeforeEach(func() {
		var err error
		workDir, err = os.MkdirTemp("", "create-host-ignitions-test-")
		Expect(err).NotTo(HaveOccurred())

		masterPath := filepath.Join(workDir, "master.ign")
		err = os.WriteFile(masterPath, []byte(testMasterIgn), 0600)
		Expect(err).NotTo(HaveOccurred())

		workerPath := filepath.Join(workDir, "worker.ign")
		err = os.WriteFile(workerPath, []byte(testWorkerIgn), 0600)
		Expect(err).NotTo(HaveOccurred())
		db, dbName = common.PrepareTestDB()
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		cluster = testCluster()
	})

	AfterEach(func() {
		os.RemoveAll(workDir)
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

			g := NewGenerator("", workDir, "", cluster, "", "", "", "", nil, logrus.New(), nil, "", "", 5).(*installerGenerator)

			err := g.createHostIgnitions()
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

	Context("node ip hint", func() {
		It("SNO: adds nodeip hint file", func() {
			cluster.Hosts = []*models.Host{
				{
					RequestedHostname: "master0.example.com",
					Role:              models.HostRoleMaster,
				},
			}
			cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeNone)
			cluster.MachineNetworks = network.CreateMachineNetworksArray("3.3.3.0/24")

			// create an ID for each host
			for _, host := range cluster.Hosts {
				id := strfmt.UUID(uuid.New().String())
				host.ID = &id
			}

			g := NewGenerator("", workDir, "", cluster, "", "", "", "", nil, logrus.New(), nil, "", "", 5).(*installerGenerator)

			err := g.createHostIgnitions()
			Expect(err).NotTo(HaveOccurred())

			for _, host := range cluster.Hosts {
				ignBytes, err := os.ReadFile(filepath.Join(workDir, fmt.Sprintf("%s-%s.ign", host.Role, host.ID)))
				Expect(err).NotTo(HaveOccurred())
				config, _, err := config_32.Parse(ignBytes)
				Expect(err).NotTo(HaveOccurred())

				By("Validating nodeip hint was added")
				var f *config_32_types.File
				for fileidx, file := range config.Storage.Files {
					if file.Node.Path == nodeIpHintFile {
						f = &config.Storage.Files[fileidx]
						break
					}
				}
				Expect(f).NotTo(BeNil())
				Expect(*f.Node.User.Name).To(Equal("root"))
				Expect(*f.FileEmbedded1.Contents.Source).To(Equal(fmt.Sprintf("data:,KUBELET_NODEIP_HINT=%s", "3.3.3.0")))
				Expect(*f.FileEmbedded1.Mode).To(Equal(420))
				Expect(*f.Node.Overwrite).To(Equal(true))

			}
		})

		It("MULTI NODE: no nodeip hint file", func() {
			cluster.Hosts = []*models.Host{
				{
					RequestedHostname: "master0.example.com",
					Role:              models.HostRoleMaster,
				},
			}
			cluster.HighAvailabilityMode = swag.String(models.ClusterHighAvailabilityModeFull)
			cluster.MachineNetworks = network.CreateMachineNetworksArray("3.3.3.0/24")

			// create an ID for each host
			for _, host := range cluster.Hosts {
				id := strfmt.UUID(uuid.New().String())
				host.ID = &id
			}

			g := NewGenerator("", workDir, "", cluster, "", "", "", "", nil, logrus.New(), nil, "", "", 5).(*installerGenerator)

			err := g.createHostIgnitions()
			Expect(err).NotTo(HaveOccurred())

			for _, host := range cluster.Hosts {
				ignBytes, err := os.ReadFile(filepath.Join(workDir, fmt.Sprintf("%s-%s.ign", host.Role, host.ID)))
				Expect(err).NotTo(HaveOccurred())
				config, _, err := config_32.Parse(ignBytes)
				Expect(err).NotTo(HaveOccurred())

				By("Validating nodeip hint was not added")
				var f *config_32_types.File
				for fileidx, file := range config.Storage.Files {
					if file.Node.Path == nodeIpHintFile {
						f = &config.Storage.Files[fileidx]
						break
					}
				}
				Expect(f).To(BeNil())
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

		g := NewGenerator("", workDir, "", cluster, "", "", "", "", nil, logrus.New(), nil, "", "", 5).(*installerGenerator)

		err := g.createHostIgnitions()
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

			g := NewGenerator("", workDir, "", cluster, "", "", "", "", mockS3Client, logrus.New(), nil, "", "", 5).(*installerGenerator)
			mockS3Client.EXPECT().ListObjectsByPrefix(gomock.Any(), gomock.Any()).Return([]string{"mcp.yaml"}, nil)
			mockS3Client.EXPECT().ListObjectsByPrefix(gomock.Any(), gomock.Any()).Return(nil, nil)
			mockS3Client.EXPECT().Download(gomock.Any(), gomock.Any()).Return(io.NopCloser(strings.NewReader(mcp)), int64(0), nil)
			err := g.writeSingleHostFile(cluster.Hosts[0], workerIgn, g.workDir)
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

			g := NewGenerator("", workDir, "", cluster, "", "", "", "", mockS3Client, logrus.New(), nil, "", "", 5).(*installerGenerator)
			mockS3Client.EXPECT().ListObjectsByPrefix(gomock.Any(), gomock.Any()).Return([]string{"mcp.yaml"}, nil)
			mockS3Client.EXPECT().ListObjectsByPrefix(gomock.Any(), gomock.Any()).Return(nil, nil)
			mockS3Client.EXPECT().Download(gomock.Any(), gomock.Any()).Return(io.NopCloser(strings.NewReader(mc)), int64(0), nil)
			err := g.writeSingleHostFile(cluster.Hosts[0], workerIgn, g.workDir)
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("ExtractClusterID", func() {
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
		cluster      *common.Cluster
		log          = logrus.New()
		workDir      string
		generator    installerGenerator
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)

		var err error
		workDir, err = os.MkdirTemp("", "upload-to-s3-test-")
		Expect(err).NotTo(HaveOccurred())
		generator = installerGenerator{
			log:      log,
			workDir:  workDir,
			s3Client: mockS3Client,
		}
		cluster = testCluster()
	})

	AfterEach(func() {
		os.RemoveAll(workDir)
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
		workDir      string
		cluster      *common.Cluster
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		var err error
		workDir, err = os.MkdirTemp("", "download-manifest-test-")
		Expect(err).NotTo(HaveOccurred())
		cluster = testCluster()
		generator = &installerGenerator{
			log:      logrus.New(),
			workDir:  workDir,
			s3Client: mockS3Client,
			cluster:  cluster,
		}
	})

	AfterEach(func() {
		os.RemoveAll(workDir)
		ctrl.Finish()
	})

	It("writes the correct file", func() {
		ctx := context.Background()
		manifestName := fmt.Sprintf("%s/manifests/openshift/masters-chrony-configuration.yaml", cluster.ID)
		mockS3Client.EXPECT().Download(ctx, manifestName).Return(io.NopCloser(strings.NewReader("content:entry")), int64(10), nil)
		Expect(os.Mkdir(filepath.Join(workDir, "/openshift"), 0755)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(workDir, "/manifests"), 0755)).To(Succeed())

		Expect(generator.downloadManifest(ctx, manifestName)).To(Succeed())

		content, err := os.ReadFile(filepath.Join(workDir, "/openshift/masters-chrony-configuration.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(Equal([]byte("content:entry")))
	})

	It("If a file has empty content, it will not be written", func() {
		ctx := context.Background()
		manifestName := fmt.Sprintf("%s/manifests/openshift/masters-chrony-configuration.yaml", cluster.ID)
		mockS3Client.EXPECT().Download(ctx, manifestName).Return(io.NopCloser(strings.NewReader("")), int64(0), nil)
		Expect(os.Mkdir(filepath.Join(workDir, "/openshift"), 0755)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(workDir, "/manifests"), 0755)).To(Succeed())

		Expect(generator.downloadManifest(ctx, manifestName)).To(Succeed())

		_, err := os.Stat(filepath.Join(workDir, "/openshift/masters-chrony-configuration.yaml"))
		Expect(err).To(HaveOccurred())
		Expect(errors.Is(err, fs.ErrNotExist)).To(BeTrue())
	})
})

var _ = Describe("infrastructureCRPatch", func() {
	var (
		ctrl         *gomock.Controller
		mockS3Client *s3wrapper.MockAPI
		generator    *installerGenerator
		ctx          = context.Background()
		cluster      *common.Cluster
		workDir      string
	)

	BeforeEach(func() {
		cluster = testCluster()
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
		var err error
		workDir, err = os.MkdirTemp("", "infrastructure-cr-patch-test-")
		Expect(err).NotTo(HaveOccurred())
		generator = &installerGenerator{
			log:      logrus.New(),
			workDir:  workDir,
			s3Client: mockS3Client,
			cluster:  cluster,
		}
	})

	AfterEach(func() {
		os.RemoveAll(workDir)
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

var _ = Describe("expand multi document yamls", func() {
	var (
		ctrl         *gomock.Controller
		mockS3Client *s3wrapper.MockAPI
		generator    *installerGenerator
		ctx          = context.Background()
		cluster      *common.Cluster
		workDir      string
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockS3Client = s3wrapper.NewMockAPI(ctrl)
		cluster = testCluster()
		var err error
		workDir, err = os.MkdirTemp("", "expand-multi-document-yamls-test-")
		Expect(err).NotTo(HaveOccurred())
		generator = &installerGenerator{
			log:      logrus.New(),
			workDir:  workDir,
			s3Client: mockS3Client,
			cluster:  cluster,
		}
	})

	AfterEach(func() {
		os.RemoveAll(workDir)
		ctrl.Finish()
	})

	It("yaml file is split when contains multiple documents", func() {
		multiDocYaml := `---
first: one
---
---
- second: two
---
`
		s3Metadata := []string{
			filepath.Join(cluster.ID.String(), constants.ManifestMetadataFolder, "manifests", "multidoc.yml", constants.ManifestSourceUserSupplied),
			filepath.Join(cluster.ID.String(), constants.ManifestMetadataFolder, "manifests", "manifest.json", constants.ManifestSourceUserSupplied), // json file will be ignored
			filepath.Join(cluster.ID.String(), constants.ManifestMetadataFolder, "manifests", "manifest.yml", "other-metadata"),
		}
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, filepath.Join(cluster.ID.String(), constants.ManifestMetadataFolder)).Return(s3Metadata, nil).Times(1)

		manifestsDir := filepath.Join(workDir, "/manifests")
		Expect(os.Mkdir(manifestsDir, 0755)).To(Succeed())

		err := os.WriteFile(filepath.Join(manifestsDir, "multidoc.yml"), []byte(multiDocYaml), 0600)
		Expect(err).NotTo(HaveOccurred())

		err = generator.expandUserMultiDocYamls(ctx)
		Expect(err).To(Succeed())

		entries, err := os.ReadDir(manifestsDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(2))

		content, err := os.ReadFile(filepath.Join(manifestsDir, entries[0].Name()))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("first: one\n"))

		content, err = os.ReadFile(filepath.Join(manifestsDir, entries[1].Name()))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("- second: two\n"))
	})

	It("file names contain a unique token when multi document yaml file is split", func() {
		multiDocYaml := `---
first: one
---
- second: two
`
		manifestsDir := filepath.Join(workDir, "/manifests")
		Expect(os.Mkdir(manifestsDir, 0755)).To(Succeed())

		manifestFilename := filepath.Join(manifestsDir, "multidoc.yml")
		err := os.WriteFile(manifestFilename, []byte(multiDocYaml), 0600)
		Expect(err).NotTo(HaveOccurred())

		uniqueToken := "sometoken"
		err = generator.expandMultiDocYaml(ctx, manifestFilename, uniqueToken)
		Expect(err).To(Succeed())

		entries, err := os.ReadDir(manifestsDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(2))

		firstManifest := fmt.Sprintf("multidoc-%s-00.yml", uniqueToken)
		content, err := os.ReadFile(filepath.Join(manifestsDir, firstManifest))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("first: one\n"))

		secondManifest := fmt.Sprintf("multidoc-%s-01.yml", uniqueToken)
		content, err = os.ReadFile(filepath.Join(manifestsDir, secondManifest))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("- second: two\n"))
	})

	It("yaml file is left untouched when contains one document", func() {
		yamlDoc := `---
first: one
---
`
		s3Metadata := []string{
			filepath.Join(cluster.ID.String(), constants.ManifestMetadataFolder, "openshift", "manifest.yml", constants.ManifestSourceUserSupplied),
		}
		mockS3Client.EXPECT().ListObjectsByPrefix(ctx, filepath.Join(cluster.ID.String(), constants.ManifestMetadataFolder)).Return(s3Metadata, nil).Times(1)

		openshiftDir := filepath.Join(workDir, "/openshift")
		Expect(os.Mkdir(openshiftDir, 0755)).To(Succeed())

		manifestFilename := filepath.Join(openshiftDir, "manifest.yml")
		err := os.WriteFile(manifestFilename, []byte(yamlDoc), 0600)
		Expect(err).NotTo(HaveOccurred())

		err = generator.expandUserMultiDocYamls(ctx)
		Expect(err).To(Succeed())

		entries, err := os.ReadDir(openshiftDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(HaveLen(1))

		content, err := os.ReadFile(manifestFilename)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal(yamlDoc))
	})
})

var _ = Describe("Set kubelet node ip", func() {
	var (
		ctrl         *gomock.Controller
		mockS3Client *s3wrapper.MockAPI
		generator    *installerGenerator
		basicEnvVars []string
		err          error
		cluster      *common.Cluster
		workDir      string
	)

	BeforeEach(func() {

		bootstrapInventory := `{"bmc_address":"0.0.0.0","bmc_v6address":"::/0","boot":{"current_boot_mode":"bios"},"cpu":{"architecture":"x86_64","count":4,"flags":["fpu","vme","de","pse","tsc","msr","pae","mce","cx8","apic","sep","mtrr","pge","mca","cmov","pat","pse36","clflush","mmx","fxsr","sse","sse2","ss","syscall","nx","pdpe1gb","rdtscp","lm","constant_tsc","arch_perfmon","rep_good","nopl","xtopology","cpuid","tsc_known_freq","pni","pclmulqdq","vmx","ssse3","fma","cx16","pcid","sse4_1","sse4_2","x2apic","movbe","popcnt","tsc_deadline_timer","aes","xsave","avx","f16c","rdrand","hypervisor","lahf_lm","abm","3dnowprefetch","cpuid_fault","invpcid_single","pti","ssbd","ibrs","ibpb","stibp","tpr_shadow","vnmi","flexpriority","ept","vpid","ept_ad","fsgsbase","tsc_adjust","bmi1","hle","avx2","smep","bmi2","erms","invpcid","rtm","mpx","avx512f","avx512dq","rdseed","adx","smap","clflushopt","clwb","avx512cd","avx512bw","avx512vl","xsaveopt","xsavec","xgetbv1","xsaves","arat","umip","pku","ospke","md_clear","arch_capabilities"],"frequency":2095.076,"model_name":"Intel(R) Xeon(R) Gold 6152 CPU @ 2.10GHz"},"disks":[{"by_path":"/dev/disk/by-path/pci-0000:00:06.0","drive_type":"HDD","model":"unknown","name":"vda","path":"/dev/vda","serial":"unknown","size_bytes":21474836480,"vendor":"0x1af4","wwn":"unknown"}],"hostname":"test-infra-cluster-master-1.redhat.com","interfaces":[{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["192.168.126.10/24"],"ipv6_addresses":["fe80::5054:ff:fe42:1e8d/64"],"mac_address":"52:54:00:42:1e:8d","mtu":1500,"name":"eth0","product":"0x0001","speed_mbps":-1,"vendor":"0x1af4"},{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["192.168.140.133/24"],"ipv6_addresses":["fe80::5054:ff:feca:7b16/64"],"mac_address":"52:54:00:ca:7b:16","mtu":1500,"name":"eth1","product":"0x0001","speed_mbps":-1,"vendor":"0x1af4"}],"memory":{"physical_bytes":17809014784,"usable_bytes":17378611200},"system_vendor":{"manufacturer":"Red Hat","product_name":"KVM"}}`
		cluster = testCluster()
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
		workDir, err = os.MkdirTemp("", "kubelet-node-ip-test-")
		Expect(err).NotTo(HaveOccurred())
		generator = &installerGenerator{
			log:      logrus.New(),
			workDir:  workDir,
			s3Client: mockS3Client,
			cluster:  cluster,
		}
	})

	AfterEach(func() {
		os.RemoveAll(workDir)
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

var _ = Describe("Bare metal host generation", func() {
	var workDir string

	BeforeEach(func() {
		var err error
		workDir, err = os.MkdirTemp("", "bmh-generation-test-")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(workDir)
	})

	DescribeTable(
		"Selects IP addresse within cluster machine network",
		func(firstAddress, secondAddress string) {
			// Create the generator:
			generator := NewGenerator(
				"",
				workDir,
				"",
				testCluster(),
				"",
				"",
				"",
				"",
				nil,
				logrus.New(),
				nil,
				"",
				"",
				5,
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

var _ = Describe("Import Cluster TLS Certs for ephemeral installer", func() {
	var (
		certDir string
		dbName  string
		db      *gorm.DB
		cluster *common.Cluster
		workDir string
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
		workDir, err = os.MkdirTemp("", "ephemeral-install-tls-cert-test-")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		os.RemoveAll(workDir)
		common.DeleteTestDB(db, dbName)
	})

	It("copies the tls cert files", func() {
		g := NewGenerator("", workDir, "", cluster, "", "", "", "", nil, logrus.New(), nil, "", certDir, 5).(*installerGenerator)

		err := g.importClusterTLSCerts(context.Background())
		Expect(err).NotTo(HaveOccurred())

		for _, cf := range certFiles {
			content, err := os.ReadFile(filepath.Join(workDir, "tls", cf))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal(cf))
		}
	})
})
