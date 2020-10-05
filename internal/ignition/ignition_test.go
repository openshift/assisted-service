package ignition

import (
	"io/ioutil"
	"os"
	"path/filepath"

	config_31 "github.com/coreos/ignition/v2/config/v3_1"
	config_31_types "github.com/coreos/ignition/v2/config/v3_1/types"
	"github.com/go-openapi/strfmt"
	"github.com/google/uuid"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/pkg/apis/metal3/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var (
	cluster           *common.Cluster
	installerCacheDir string
	log               = logrus.New()
	workDir           string
)

var _ = BeforeEach(func() {
	// setup temp workdir
	var err error
	workDir, err = ioutil.TempDir("", "assisted-install-test-")
	Expect(err).NotTo(HaveOccurred())
	installerCacheDir = filepath.Join(workDir, "installercache")

	// create simple cluster
	clusterID := strfmt.UUID(uuid.New().String())
	cluster = &common.Cluster{
		Cluster: models.Cluster{
			ID: &clusterID,
		},
	}
})

var _ = AfterEach(func() {
	os.RemoveAll(workDir)
})

var _ = Describe("Bootstrap Ignition Update", func() {
	const bootstrap1 = `{
		"ignition": {
		  "config": {},
		  "security": {
			"tls": {}
		  },
		  "timeouts": {},
		  "version": "3.1.0"
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

	const inventory1 = `{"bmc_address":"0.0.0.0","bmc_v6address":"::/0","boot":{"current_boot_mode":"bios"},"cpu":{"architecture":"x86_64","count":4,"flags":["fpu","vme","de","pse","tsc","msr","pae","mce","cx8","apic","sep","mtrr","pge","mca","cmov","pat","pse36","clflush","mmx","fxsr","sse","sse2","ss","syscall","nx","pdpe1gb","rdtscp","lm","constant_tsc","arch_perfmon","rep_good","nopl","xtopology","cpuid","tsc_known_freq","pni","pclmulqdq","vmx","ssse3","fma","cx16","pcid","sse4_1","sse4_2","x2apic","movbe","popcnt","tsc_deadline_timer","aes","xsave","avx","f16c","rdrand","hypervisor","lahf_lm","abm","3dnowprefetch","cpuid_fault","invpcid_single","pti","ssbd","ibrs","ibpb","stibp","tpr_shadow","vnmi","flexpriority","ept","vpid","ept_ad","fsgsbase","tsc_adjust","bmi1","hle","avx2","smep","bmi2","erms","invpcid","rtm","mpx","avx512f","avx512dq","rdseed","adx","smap","clflushopt","clwb","avx512cd","avx512bw","avx512vl","xsaveopt","xsavec","xgetbv1","xsaves","arat","umip","pku","ospke","md_clear","arch_capabilities"],"frequency":2095.076,"model_name":"Intel(R) Xeon(R) Gold 6152 CPU @ 2.10GHz"},"disks":[{"by_path":"/dev/disk/by-path/pci-0000:00:06.0","drive_type":"HDD","model":"unknown","name":"vda","path":"/dev/vda","serial":"unknown","size_bytes":21474836480,"vendor":"0x1af4","wwn":"unknown"}],"hostname":"test-infra-cluster-master-1.redhat.com","interfaces":[{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["192.168.126.11/24"],"ipv6_addresses":["fe80::5054:ff:fe42:1e8d/64"],"mac_address":"52:54:00:42:1e:8d","mtu":1500,"name":"eth0","product":"0x0001","speed_mbps":-1,"vendor":"0x1af4"},{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["192.168.140.133/24"],"ipv6_addresses":["fe80::5054:ff:feca:7b16/64"],"mac_address":"52:54:00:ca:7b:16","mtu":1500,"name":"eth1","product":"0x0001","speed_mbps":-1,"vendor":"0x1af4"}],"memory":{"physical_bytes":17809014784,"usable_bytes":17378611200},"system_vendor":{"manufacturer":"Red Hat","product_name":"KVM"}}`

	var (
		err         error
		examplePath string
		file        *config_31_types.File
		bmh         *bmh_v1alpha1.BareMetalHost
	)

	BeforeEach(func() {
		var err1 error
		examplePath = filepath.Join(workDir, "example1.ign")
		err1 = ioutil.WriteFile(examplePath, []byte(bootstrap1), 0600)
		Expect(err1).NotTo(HaveOccurred())

		cluster.Hosts = []*models.Host{
			{
				Inventory:         inventory1,
				RequestedHostname: "example1",
				Role:              models.HostRoleMaster,
			},
		}

		g := NewGenerator(workDir, installerCacheDir, cluster, "", log).(*installerGenerator)
		err = g.updateBootstrap(examplePath)

		bootstrapBytes, _ := ioutil.ReadFile(examplePath)
		config, _, err1 := config_31.Parse(bootstrapBytes)
		Expect(err1).NotTo(HaveOccurred())
		Expect(config.Storage.Files).To(HaveLen(1))
		file = &config.Storage.Files[0]
		bmh, _ = fileToBMH(file)
	})

	Describe("update bootstrap.ign", func() {
		Context("with 1 master", func() {
			It("got a tmp wodkDir", func() {
				Expect(workDir).NotTo(Equal(""))
			})
			It("adds annotation", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(bmh.Annotations).To(HaveKey(bmh_v1alpha1.StatusAnnotation))
			})
		})
	})
})
