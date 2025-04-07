package utils_test

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/units"
	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	usageMgr "github.com/openshift/assisted-service/internal/usage"
	"github.com/openshift/assisted-service/models"
	yaml "gopkg.in/yaml.v3"
)

const (
	PullSecretName    = "pull-secret"
	ValidDiskSize     = int64(128849018880)
	MinSuccessesInRow = 2
	MinHosts          = 3
	Loop0Id           = "wwn-0x1111111111111111111111"
	SdbId             = "wwn-0x2222222222222222222222"
	DefaultCIDRv6     = "1001:db8::10/120"
	DefaultCIDRv4     = "1.2.3.10/24"
	SshPublicKey      = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC50TuHS7aYci+U+5PLe/aW/I6maBi9PBDucLje6C6gtArfjy7udWA1DCSIQd+DkHhi57/s+PmvEjzfAfzqo+L+/8/O2l2seR1pPhHDxMR/rSyo/6rZP6KIL8HwFqXHHpDUM4tLXdgwKAe1LxBevLt/yNl8kOiHJESUSl+2QSf8z4SIbo/frDD8OwOvtfKBEG4WCb8zEsEuIPNF/Vo/UxPtS9pPTecEsWKDHR67yFjjamoyLvAzMAJotYgyMoxm8PTyCgEzHk3s3S4iO956d6KVOEJVXnTVhAxrtLuubjskd7N4hVN7h2s4Z584wYLKYhrIBL0EViihOMzY4mH3YE4KZusfIx6oMcggKX9b3NHm0la7cj2zg0r6zjUn6ZCP4gXM99e5q4auc0OEfoSfQwofGi3WmxkG3tEozCB8Zz0wGbi2CzR8zlcF+BNV5I2LESlLzjPY5B4dvv5zjxsYoz94p3rUhKnnPM2zTx1kkilDK5C5fC1k9l/I/r5Qk4ebLQU= oscohen@localhost.localdomain"
	IngressCa         = "-----BEGIN CERTIFICATE-----\nMIIDozCCAougAwIBAgIULCOqWTF" +
		"aEA8gNEmV+rb7h1v0r3EwDQYJKoZIhvcNAQELBQAwYTELMAkGA1UEBhMCaXMxCzAJBgNVBAgMAmRk" +
		"MQswCQYDVQQHDAJkZDELMAkGA1UECgwCZGQxCzAJBgNVBAsMAmRkMQswCQYDVQQDDAJkZDERMA8GCSqGSIb3DQEJARYCZGQwHhcNMjAwNTI1MTYwNTAwWhcNMzA" +
		"wNTIzMTYwNTAwWjBhMQswCQYDVQQGEwJpczELMAkGA1UECAwCZGQxCzAJBgNVBAcMAmRkMQswCQYDVQQKDAJkZDELMAkGA1UECwwCZGQxCzAJBgNVBAMMAmRkMREwDwYJKoZIh" +
		"vcNAQkBFgJkZDCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBAML63CXkBb+lvrJKfdfYBHLDYfuaC6exCSqASUAosJWWrfyDiDMUbmfs06PLKyv7N8efDhza74ov0EQJ" +
		"NRhMNaCE+A0ceq6ZXmmMswUYFdLAy8K2VMz5mroBFX8sj5PWVr6rDJ2ckBaFKWBB8NFmiK7MTWSIF9n8M107/9a0QURCvThUYu+sguzbsLODFtXUxG5rtTVKBVcPZvEfRky2Tkt4AySFS" +
		"mkO6Kf4sBd7MC4mKWZm7K8k7HrZYz2usSpbrEtYGtr6MmN9hci+/ITDPE291DFkzIcDCF493v/3T+7XsnmQajh6kuI+bjIaACfo8N+twEoJf/N1PmphAQdEiC0CAwEAAaNTMFEwHQYDVR0O" +
		"BBYEFNvmSprQQ2HUUtPxs6UOuxq9lKKpMB8GA1UdIwQYMBaAFNvmSprQQ2HUUtPxs6UOuxq9lKKpMA8GA1UdEwEB/wQFMAMBAf8wDQYJKoZIhvcNAQELBQADggEBAJEWxnxtQV5IqPVRr2SM" +
		"WNNxcJ7A/wyet39l5VhHjbrQGynk5WS80psn/riLUfIvtzYMWC0IR0pIMQuMDF5sNcKp4D8Xnrd+Bl/4/Iy/iTOoHlw+sPkKv+NL2XR3iO8bSDwjtjvd6L5NkUuzsRoSkQCG2fHASqqgFoyV9Ld" +
		"RsQa1w9ZGebtEWLuGsrJtR7gaFECqJnDbb0aPUMixmpMHID8kt154TrLhVFmMEqGGC1GvZVlQ9Of3GP9y7X4vDpHshdlWotOnYKHaeu2d5cRVFHhEbrslkISgh/TRuyl7VIpnjOYUwMBpCiVH6M" +
		"2lyDI6UR3Fbz4pVVAxGXnVhBExjBE=\n-----END CERTIFICATE-----"
	DefaultWaitForHostStateTimeout          = 20 * time.Second
	DefaultWaitForClusterStateTimeout       = 40 * time.Second
	DefaultWaitForMachineNetworkCIDRTimeout = 40 * time.Second
)

const (
	ClusterInsufficientStateInfo                = "Cluster is not ready for install"
	ClusterReadyStateInfo                       = "Cluster ready to be installed"
	IgnoreStateInfo                             = "IgnoreStateInfo"
	ClusterCanceledInfo                         = "Canceled cluster installation"
	ClusterErrorInfo                            = "cluster has hosts in error"
	ClusterResetStateInfo                       = "cluster was reset by user"
	ClusterPendingForInputStateInfo             = "User input required"
	ClusterFinalizingStateInfo                  = "Finalizing cluster installation"
	ClusterInstallingPendingUserActionStateInfo = "Cluster has hosts with wrong boot order"
	ClusterInstallingStateInfo                  = "Installation in progress"
)

var (
	TestContext *SubsystemTestContext
	Loop0       = models.Disk{
		ID:        Loop0Id,
		ByID:      Loop0Id,
		DriveType: "SSD",
		Name:      "loop0",
		SizeBytes: ValidDiskSize,
	}

	Sda1 = models.Disk{
		ID:        "wwn-0x1111111111111111111111",
		ByID:      "wwn-0x1111111111111111111111",
		DriveType: "HDD",
		Name:      "sda1",
		SizeBytes: ValidDiskSize,
	}

	Sdb = models.Disk{
		ID:        SdbId,
		ByID:      SdbId,
		DriveType: "HDD",
		Name:      "sdb",
		SizeBytes: ValidDiskSize,
	}

	Vma = models.Disk{
		ID:        SdbId,
		ByID:      SdbId,
		DriveType: "HDD",
		Name:      "vma",
		HasUUID:   true,
		Vendor:    "VMware",
		SizeBytes: ValidDiskSize,
	}

	Vmremovable = models.Disk{
		ID:        Loop0Id,
		ByID:      Loop0Id,
		DriveType: "0DD",
		Name:      "sr0",
		Removable: true,
		SizeBytes: 106516480,
	}

	ValidHwInfoV6 = &models.Inventory{
		CPU:    &models.CPU{Count: 16},
		Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB), UsableBytes: int64(32 * units.GiB)},
		Disks:  []*models.Disk{&Loop0, &Sdb},
		Interfaces: []*models.Interface{
			{
				IPV6Addresses: []string{
					DefaultCIDRv6,
				},
				Type: "physical",
			},
		},
		SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "prod", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
	ValidHwInfo = &models.Inventory{
		CPU:    &models.CPU{Count: 16, Architecture: "x86_64"},
		Memory: &models.Memory{PhysicalBytes: int64(32 * units.GiB), UsableBytes: int64(32 * units.GiB)},
		Disks:  []*models.Disk{&Loop0, &Sdb},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: []string{
					DefaultCIDRv4,
				},
				MacAddress: "e6:53:3d:a7:77:b4",
				Type:       "physical",
			},
		},
		SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "prod", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
		TpmVersion:   models.InventoryTpmVersionNr20,
	}
	ValidFreeAddresses = models.FreeNetworksAddresses{
		{
			Network: "1.2.3.0/24",
			FreeAddresses: []strfmt.IPv4{
				"1.2.3.8",
				"1.2.3.9",
				"1.2.3.5",
				"1.2.3.6",
				"1.2.3.100",
				"1.2.3.101",
				"1.2.3.102",
				"1.2.3.103",
			},
		},
	}
)

func GinkgoLogger(s string) {
	_, _ = GinkgoWriter.Write([]byte(fmt.Sprintln(s)))
}

func GinkgoResourceLogger(kind string, resources interface{}) error {
	resList, err := json.MarshalIndent(resources, "", "  ")
	if err != nil {
		return err
	}
	GinkgoLogger(fmt.Sprintf("The failed test '%s' created the following %s resources:", GinkgoT().Name(), kind))
	GinkgoLogger(string(resList))
	return nil
}

func StrToUUID(s string) *strfmt.UUID {
	u := strfmt.UUID(s)
	return &u
}

func IsJSON(s []byte) bool {
	var js map[string]interface{}
	return json.Unmarshal(s, &js) == nil

}

func getTangResponse(url string) models.TangServerResponse {
	return models.TangServerResponse{
		TangURL: url,
		Payload: "some_fake_payload",
		Signatures: []*models.TangServerSignatures{
			{
				Signature: "some_fake_signature1",
				Protected: "foobar1",
			},
			{
				Signature: "some_fake_signature2",
				Protected: "foobar2",
			},
		},
	}
}

func GetDefaultInventory(cidr string) *models.Inventory {
	hwInfo := ValidHwInfo
	hwInfo.Interfaces[0].IPV4Addresses = []string{cidr}
	return hwInfo
}

func GetDefaultNutanixInventory(cidr string) *models.Inventory {
	nutanixInventory := *GetDefaultInventory(cidr)
	nutanixInventory.SystemVendor = &models.SystemVendor{Manufacturer: "Nutanix", ProductName: "AHV", Virtual: true, SerialNumber: "3534"}
	nutanixInventory.Disks = []*models.Disk{&Vma, &Vmremovable}
	return &nutanixInventory
}

func GetDefaultExternalInventory(cidr string) *models.Inventory {
	externalInventory := *GetDefaultInventory(cidr)
	externalInventory.SystemVendor = &models.SystemVendor{Manufacturer: "OracleCloud.com", ProductName: "OCI", Virtual: true, SerialNumber: "3534"}
	externalInventory.Disks = []*models.Disk{&Vma, &Vmremovable}
	return &externalInventory
}

func GetDefaultVmwareInventory(cidr string) *models.Inventory {
	vmwareInventory := *GetDefaultInventory(cidr)
	vmwareInventory.SystemVendor = &models.SystemVendor{Manufacturer: "VMware, Inc.", ProductName: "VMware Virtual", Virtual: true, SerialNumber: "3534"}
	vmwareInventory.Disks = []*models.Disk{&Vma, &Vmremovable}
	return &vmwareInventory
}

func IsStepTypeInList(steps models.Steps, sType models.StepType) bool {
	for _, step := range steps.Instructions {
		if step.StepType == sType {
			return true
		}
	}
	return false
}

func AreStepsInList(steps models.Steps, stepTypes []models.StepType) {
	for _, stepType := range stepTypes {
		Expect(IsStepTypeInList(steps, stepType)).Should(BeTrue())
	}
}

func GetStepFromListByStepType(steps models.Steps, sType models.StepType) *models.Step {
	for _, step := range steps.Instructions {
		if step.StepType == sType {
			return step
		}
	}
	return nil
}

func VerifyUsageSet(featureUsage string, candidates ...models.Usage) {
	usages := make(map[string]models.Usage)
	err := json.Unmarshal([]byte(featureUsage), &usages)
	Expect(err).NotTo(HaveOccurred())
	for _, usage := range candidates {
		usage.ID = usageMgr.UsageNameToID(usage.Name)
		Expect(usages[usage.Name]).To(Equal(usage))
	}
}

func VerifyUsageNotSet(featureUsage string, features ...string) {
	usages := make(map[string]*models.Usage)
	err := json.Unmarshal([]byte(featureUsage), &usages)
	Expect(err).NotTo(HaveOccurred())
	for _, name := range features {
		Expect(usages[name]).To(BeNil())
	}
}

func GetMinimalMasterInventory(cidr string) *models.Inventory {
	inventory := *GetDefaultInventory(cidr)
	inventory.CPU = &models.CPU{Count: 4}
	inventory.Memory = &models.Memory{PhysicalBytes: int64(16 * units.GiB), UsableBytes: int64(16 * units.GiB)}
	return &inventory
}

func GetValidWorkerHwInfoWithCIDR(cidr string) *models.Inventory {
	return &models.Inventory{
		CPU:    &models.CPU{Count: 2},
		Memory: &models.Memory{PhysicalBytes: int64(8 * units.GiB), UsableBytes: int64(8 * units.GiB)},
		Disks:  []*models.Disk{&Loop0, &Sdb},
		Interfaces: []*models.Interface{
			{
				IPV4Addresses: []string{
					cidr,
				},
				MacAddress: "e6:53:3d:a7:77:b4",
				Type:       "physical",
			},
		},
		SystemVendor: &models.SystemVendor{Manufacturer: "manu", ProductName: "prod", SerialNumber: "3534"},
		Routes:       common.TestDefaultRouteConfiguration,
	}
}

func CopyDir(src string, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(src, path)
		targetPath := filepath.Join(dst, relPath)

		if d.IsDir() {
			return os.MkdirAll(targetPath, 0755)
		}
		return copyFile(path, targetPath)
	})
}

func copyFile(srcFile string, dstFile string) error {
	in, err := os.Open(srcFile)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dstFile)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func UpdateYAMLField(filePath string, fieldPath string, oldValue string, newValue string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return err
	}

	var yamlMap map[string]interface{}
	if err = yaml.Unmarshal(data, &yamlMap); err != nil {
		return err
	}

	keys := strings.Split(fieldPath, ".")
	curr := yamlMap
	for i, key := range keys {
		if i == len(keys)-1 {
			if str, ok := curr[key].(string); ok {
				curr[key] = strings.Replace(str, oldValue, newValue, 1)
			}
		} else {
			next, ok := curr[key].(map[string]interface{})
			if !ok {
				next = make(map[string]interface{})
				curr[key] = next
			}
			curr = next
		}
	}

	modifiedData, err := yaml.Marshal(yamlMap)
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, modifiedData, 0600)
}
