package common

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"gorm.io/gorm"
)

const redHatIntermediateChain string = `
subject=O = Red Hat, OU = prod, CN = Certificate Authority

issuer=O = Red Hat, OU = prod, CN = Intermediate Certificate Authority

-----BEGIN CERTIFICATE-----
MIIDsjCCApqgAwIBAgIBBjANBgkqhkiG9w0BAQsFADBOMRAwDgYDVQQKDAdSZWQg
SGF0MQ0wCwYDVQQLDARwcm9kMSswKQYDVQQDDCJJbnRlcm1lZGlhdGUgQ2VydGlm
aWNhdGUgQXV0aG9yaXR5MB4XDTE1MTAxNDE3NDc1NloXDTM1MTAwOTE3NDc1Nlow
QTEQMA4GA1UECgwHUmVkIEhhdDENMAsGA1UECwwEcHJvZDEeMBwGA1UEAwwVQ2Vy
dGlmaWNhdGUgQXV0aG9yaXR5MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAzTein1EAuLAZFgvvfDL3okBqn/xg9RVGa7r3Kuw4pVPa9QnkCkFbnaipnyUd
R331/A4RAHHZmVuddrbvh6C+YtDs+P8DLRC+YDE4VkW9ZRtNbt302z3jY4Y62W1w
hmsl7IV57ISC8kUbtekLXTuVd3InEywAIc3fyiTi7FsldIngunuxuNjjQD04DGnr
RmbBAmwfKxaXaT5qciq5kcFaYKQ3P0p6wT0gaDZ7C177W1uorIXCm9J6v8P7GLXD
scAvN3pgZRJ6Ocj5Fpnkd0nP6kpWhlO8/5B1CUggKaZXdm0kR9J2KOX4wrcjh3xn
6Rby/hijEwplkgUALIBgRTe3ewIDAQABo4GnMIGkMB8GA1UdIwQYMBaAFDDeBFSh
hgKfEfpQ+QVwpyjAwE/8MBIGA1UdEwEB/wQIMAYBAf8CAQAwDgYDVR0PAQH/BAQD
AgHGMB0GA1UdDgQWBBR72gn1SV3Z11zJNvhV0huXnhEvfjA+BggrBgEFBQcBAQQy
MDAwLgYIKwYBBQUHMAGGImh0dHA6Ly9vY3NwLnJlZGhhdC5jb20vY2EvaW50b2Nz
cC8wDQYJKoZIhvcNAQELBQADggEBAJeV5FNFhLYc24NZBDTuMFGDLKuHwJmdF4uF
8Tt5g/Mj4Mi3qSbu2Y+3gk4UQ45GD6HQf+JpA4hHsxJ2L0F39oVQ39QS3MgRoSk3
LfpKYkzQntRFzSr1OHMA06tHNPlhylGRc/gdLkaLjeFYj/Fhz5Htg9vv9dF4h8bl
X6KXw/3RH9f5YgKqydtEZtZ0isA4+55gf0m7I0O5lNK3mgY/uBmIk/jSI9WqczrD
WGf78pvkTQ2PcYg/WiCv+AVsaSaiEDUf4rDj55wQ30h78Ox5J2izd4I6QylB9Lpu
fQEw+cWRxwFPJujSOTSKRHZDo1UwOIQbxqkbznSHlLCICEXxuvQ=
-----END CERTIFICATE-----

subject=O = Red Hat, OU = prod, CN = Intermediate Certificate Authority

issuer=C = US, ST = North Carolina, L = Raleigh, O = "Red Hat, Inc.",\
OU = Red Hat IT, CN = Red Hat IT Root CA, emailAddress = infosec@redhat.com

-----BEGIN CERTIFICATE-----
MIID6DCCAtCgAwIBAgIBFDANBgkqhkiG9w0BAQsFADCBpTELMAkGA1UEBhMCVVMx
FzAVBgNVBAgMDk5vcnRoIENhcm9saW5hMRAwDgYDVQQHDAdSYWxlaWdoMRYwFAYD
VQQKDA1SZWQgSGF0LCBJbmMuMRMwEQYDVQQLDApSZWQgSGF0IElUMRswGQYDVQQD
DBJSZWQgSGF0IElUIFJvb3QgQ0ExITAfBgkqhkiG9w0BCQEWEmluZm9zZWNAcmVk
aGF0LmNvbTAeFw0xNTEwMTQxNzI5MDdaFw00NTEwMDYxNzI5MDdaME4xEDAOBgNV
BAoMB1JlZCBIYXQxDTALBgNVBAsMBHByb2QxKzApBgNVBAMMIkludGVybWVkaWF0
ZSBDZXJ0aWZpY2F0ZSBBdXRob3JpdHkwggEiMA0GCSqGSIb3DQEBAQUAA4IBDwAw
ggEKAoIBAQDYpVfg+jjQ3546GHF6sxwMOjIwpOmgAXiHS4pgaCmu+AQwBs4rwxvF
S+SsDHDTVDvpxJYBwJ6h8S3LK9xk70yGsOAu30EqITj6T+ZPbJG6C/0I5ukEVIeA
xkgPeCBYiiPwoNc/te6Ry2wlaeH9iTVX8fx32xroSkl65P59/dMttrQtSuQX8jLS
5rBSjBfILSsaUywND319E/Gkqvh6lo3TEax9rhqbNh2s+26AfBJoukZstg3TWlI/
pi8v/D3ZFDDEIOXrP0JEfe8ETmm87T1CPdPIZ9+/c4ADPHjdmeBAJddmT0IsH9e6
Gea2R/fQaSrIQPVmm/0QX2wlY4JfxyLJAgMBAAGjeTB3MB0GA1UdDgQWBBQw3gRU
oYYCnxH6UPkFcKcowMBP/DAfBgNVHSMEGDAWgBR+0eMgvlHoSCD3ri/GasNz824H
GTASBgNVHRMBAf8ECDAGAQH/AgEBMA4GA1UdDwEB/wQEAwIBhjARBglghkgBhvhC
AQEEBAMCAQYwDQYJKoZIhvcNAQELBQADggEBADwaXLIOqoyQoBVck8/52AjWw1Cv
ath9NGUEFROYm15VbAaFmeY2oQ0EV3tQRm32C9qe9RxVU8DBDjBuNyYhLg3k6/1Z
JXggtSMtffr5T83bxgfh+vNxF7o5oNxEgRUYTBi4aV7v9LiDd1b7YAsUwj4NPWYZ
dbuypFSWCoV7ReNt+37muMEZwi+yGIU9ug8hLOrvriEdU3RXt5XNISMMuC8JULdE
3GVzoNtkznqv5ySEj4M9WsdBiG6bm4aBYIOE0XKE6QYtlsjTMB9UTXxmlUvDE0wC
z9YYKfC1vLxL2wAgMhOCdKZM+Qlu1stb0B/EF3oxc/iZrhDvJLjijbMpphw=
-----END CERTIFICATE-----
`

const redHatRootCA string = `
subject=C = US, ST = North Carolina, L = Raleigh, O = "Red Hat, Inc.",\
OU = Red Hat IT, CN = Red Hat IT Root CA, emailAddress = infosec@redhat.com

issuer=C = US, ST = North Carolina, L = Raleigh, O = "Red Hat, Inc.",\
OU = Red Hat IT, CN = Red Hat IT Root CA, emailAddress = infosec@redhat.com

-----BEGIN CERTIFICATE-----
MIIENDCCAxygAwIBAgIJANunI0D662cnMA0GCSqGSIb3DQEBCwUAMIGlMQswCQYD
VQQGEwJVUzEXMBUGA1UECAwOTm9ydGggQ2Fyb2xpbmExEDAOBgNVBAcMB1JhbGVp
Z2gxFjAUBgNVBAoMDVJlZCBIYXQsIEluYy4xEzARBgNVBAsMClJlZCBIYXQgSVQx
GzAZBgNVBAMMElJlZCBIYXQgSVQgUm9vdCBDQTEhMB8GCSqGSIb3DQEJARYSaW5m
b3NlY0ByZWRoYXQuY29tMCAXDTE1MDcwNjE3MzgxMVoYDzIwNTUwNjI2MTczODEx
WjCBpTELMAkGA1UEBhMCVVMxFzAVBgNVBAgMDk5vcnRoIENhcm9saW5hMRAwDgYD
VQQHDAdSYWxlaWdoMRYwFAYDVQQKDA1SZWQgSGF0LCBJbmMuMRMwEQYDVQQLDApS
ZWQgSGF0IElUMRswGQYDVQQDDBJSZWQgSGF0IElUIFJvb3QgQ0ExITAfBgkqhkiG
9w0BCQEWEmluZm9zZWNAcmVkaGF0LmNvbTCCASIwDQYJKoZIhvcNAQEBBQADggEP
ADCCAQoCggEBALQt9OJQh6GC5LT1g80qNh0u50BQ4sZ/yZ8aETxt+5lnPVX6MHKz
bfwI6nO1aMG6j9bSw+6UUyPBHP796+FT/pTS+K0wsDV7c9XvHoxJBJJU38cdLkI2
c/i7lDqTfTcfLL2nyUBd2fQDk1B0fxrskhGIIZ3ifP1Ps4ltTkv8hRSob3VtNqSo
GxkKfvD2PKjTPxDPWYyruy9irLZioMffi3i/gCut0ZWtAyO3MVH5qWF/enKwgPES
X9po+TdCvRB/RUObBaM761EcrLSM1GqHNueSfqnho3AjLQ6dBnPWlo638Zm1VebK
BELyhkLWMSFkKwDmne0jQ02Y4g075vCKvCsCAwEAAaNjMGEwHQYDVR0OBBYEFH7R
4yC+UehIIPeuL8Zqw3PzbgcZMB8GA1UdIwQYMBaAFH7R4yC+UehIIPeuL8Zqw3Pz
bgcZMA8GA1UdEwEB/wQFMAMBAf8wDgYDVR0PAQH/BAQDAgGGMA0GCSqGSIb3DQEB
CwUAA4IBAQBDNvD2Vm9sA5A9AlOJR8+en5Xz9hXcxJB5phxcZQ8jFoG04Vshvd0e
LEnUrMcfFgIZ4njMKTQCM4ZFUPAieyLx4f52HuDopp3e5JyIMfW+KFcNIpKwCsak
oSoKtIUOsUJK7qBVZxcrIyeQV2qcYOeZhtS5wBqIwOAhFwlCET7Ze58QHmS48slj
S9K0JAcps2xdnGu0fkzhSQxY8GPQNFTlr6rYld5+ID/hHeS76gq0YG3q6RLWRkHf
4eTkRjivAlExrFzKcljC4axKQlnOvVAzz+Gm32U0xPBF4ByePVxCJUHw1TsyTmel
RxNEp7yHoXcwn+fXna+t5JWh1gxUZty3
-----END CERTIFICATE-----
`

const malformedPEM string = `
-----BEGIN CERTIFICATE-----
XXXXsjCCApqgAwIBAgIBBjANBgkqhkiG9w0BAQsFADBOMRAwDgYDVQQKDAdSZWQg
SGF0MQ0wCwYDVQQLDARwcm9kMSswKQYDVQQDDCJJbnRlcm1lZGlhdGUgQ2VydGlm
aWNhdGUgQXV0aG9yaXR5MB4XDTE1MTAxNDE3NDc1NloXDTM1MTAwOTE3NDc1Nlow
QTEQMA4GA1UECgwHUmVkIEhhdDENMAsGA1UECwwEcHJvZDEeMBwGA1UEAwwVQ2Vy
dGlmaWNhdGUgQXV0aG9yaXR5MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAzTein1EAuLAZFgvvfDL3okBqn/xg9RVGa7r3Kuw4pVPa9QnkCkFbnaipnyUd
R331/A4RAHHZmVuddrbvh6C+YtDs+P8DLRC+YDE4VkW9ZRtNbt302z3jY4Y62W1w
hmsl7IV57ISC8kUbtekLXTuVd3InEywAIc3fyiTi7FsldIngunuxuNjjQD04DGnr
RmbBAmwfKxaXaT5qciq5kcFaYKQ3P0p6wT0gaDZ7C177W1uorIXCm9J6v8P7GLXD
scAvN3pgZRJ6Ocj5Fpnkd0nP6kpWhlO8/5B1CUggKaZXdm0kR9J2KOX4wrcjh3xn
6Rby/hijEwplkgUALIBgRTe3ewIDAQABo4GnMIGkMB8GA1UdIwQYMBaAFDDeBFSh
hgKfEfpQ+QVwpyjAwE/8MBIGA1UdEwEB/wQIMAYBAf8CAQAwDgYDVR0PAQH/BAQD
AgHGMB0GA1UdDgQWBBR72gn1SV3Z11zJNvhV0huXnhEvfjA+BggrBgEFBQcBAQQy
MDAwLgYIKwYBBQUHMAGGImh0dHA6Ly9vY3NwLnJlZGhhdC5jb20vY2EvaW50b2Nz
cC8wDQYJKoZIhvcNAQELBQADggEBAJeV5FNFhLYc24NZBDTuMFGDLKuHwJmdF4uF
8Tt5g/Mj4Mi3qSbu2Y+3gk4UQ45GD6HQf+JpA4hHsxJ2L0F39oVQ39QS3MgRoSk3
LfpKYkzQntRFzSr1OHMA06tHNPlhylGRc/gdLkaLjeFYj/Fhz5Htg9vv9dF4h8bl
X6KXw/3RH9f5YgKqydtEZtZ0isA4+55gf0m7I0O5lNK3mgY/uBmIk/jSI9WqczrD
WGf78pvkTQ2PcYg/WiCv+AVsaSaiEDUf4rDj55wQ30h78Ox5J2izd4I6QylB9Lpu
fQEw+cWRxwFPJujSOTSKRHZDo1UwOIQbxqkbznSHlLCICEXxuvQ=
-----END CERTIFICATE-----
`

const inventoryWithSingleNIC string = "{\"bmc_address\":\"0.0.0.0\",\"bmc_v6address\":\"::/0\",\"boot\":{\"current_boot_mode\":\"bios\"},\"cpu\":{\"architecture\":\"x86_64\",\"count\":16,\"flags\":[\"fpu\",\"vme\",\"de\",\"pse\",\"tsc\",\"msr\",\"pae\",\"mce\",\"cx8\",\"apic\",\"sep\",\"mtrr\",\"pge\",\"mca\",\"cmov\",\"pat\",\"pse36\",\"clflush\",\"mmx\",\"fxsr\",\"sse\",\"sse2\",\"ss\",\"syscall\",\"nx\",\"pdpe1gb\",\"rdtscp\",\"lm\",\"constant_tsc\",\"arch_perfmon\",\"nopl\",\"xtopology\",\"tsc_reliable\",\"nonstop_tsc\",\"cpuid\",\"pni\",\"pclmulqdq\",\"ssse3\",\"fma\",\"cx16\",\"pcid\",\"sse4_1\",\"sse4_2\",\"x2apic\",\"movbe\",\"popcnt\",\"tsc_deadline_timer\",\"aes\",\"xsave\",\"avx\",\"f16c\",\"rdrand\",\"hypervisor\",\"lahf_lm\",\"abm\",\"3dnowprefetch\",\"cpuid_fault\",\"invpcid_single\",\"pti\",\"ssbd\",\"ibrs\",\"ibpb\",\"stibp\",\"fsgsbase\",\"tsc_adjust\",\"bmi1\",\"avx2\",\"smep\",\"bmi2\",\"invpcid\",\"rdseed\",\"adx\",\"smap\",\"xsaveopt\",\"arat\",\"md_clear\",\"flush_l1d\",\"arch_capabilities\"],\"frequency\":2194.917,\"model_name\":\"Intel(R) Xeon(R) CPU E5-2630 v4 @ 2.20GHz\"},\"disks\":[{\"by_id\":\"/dev/disk/by-id/wwn-0x6000c2911a3fb8af754385340083d09c\",\"by_path\":\"/dev/disk/by-path/pci-0000:03:00.0-scsi-0:0:0:0\",\"drive_type\":\"HDD\",\"has_uuid\":true,\"hctl\":\"0:0:0:0\",\"id\":\"/dev/disk/by-id/wwn-0x6000c2911a3fb8af754385340083d09c\",\"installation_eligibility\":{\"eligible\":true,\"not_eligible_reasons\":null},\"model\":\"Virtual_disk\",\"name\":\"sda\",\"path\":\"/dev/sda\",\"serial\":\"6000c2911a3fb8af754385340083d09c\",\"size_bytes\":128849018880,\"smart\":\"SMART support is:     Unavailable - device lacks SMART capability.\\n\",\"vendor\":\"VMware\",\"wwn\":\"0x6000c2911a3fb8af754385340083d09c\"},{\"by_path\":\"/dev/disk/by-path/pci-0000:00:07.1-ata-1\",\"drive_type\":\"ODD\",\"hctl\":\"1:0:0:0\",\"id\":\"/dev/disk/by-path/pci-0000:00:07.1-ata-1\",\"installation_eligibility\":{\"not_eligible_reasons\":[\"Disk is removable\",\"Disk is too small (disk only has 106 MB, but 100 GB are required)\",\"Drive type is ODD, it must be one of HDD, SSD, Multipath.\"]},\"is_installation_media\":true,\"model\":\"VMware_IDE_CDR00\",\"name\":\"sr0\",\"path\":\"/dev/sr0\",\"removable\":true,\"serial\":\"00000000000000000001\",\"size_bytes\":106516480,\"smart\":\"SMART support is:     Unavailable - device lacks SMART capability.\\n\",\"vendor\":\"NECVMWar\"}],\"gpus\":[{\"address\":\"0000:00:0f.0\"}],\"hostname\":\"master-2.qe1.e2e.bos.redhat.com\",\"interfaces\":[{\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"has_carrier\":true,\"ipv4_addresses\":[\"10.19.114.222/23\"],\"ipv6_addresses\":[\"2620:52:0:1372:b55f:731b:f1dc:d773/64\"],\"mac_address\":\"00:50:56:83:87:09\",\"mtu\":1500,\"name\":\"ens192\",\"product\":\"0x07b0\",\"speed_mbps\":10000,\"type\":\"physical\",\"vendor\":\"0x15ad\"}],\"memory\":{\"physical_bytes\":34359738368,\"physical_bytes_method\":\"dmidecode\",\"usable_bytes\":33711775744},\"routes\":[{\"destination\":\"0.0.0.0\",\"family\":2,\"gateway\":\"10.19.115.254\",\"interface\":\"ens192\"},{\"destination\":\"10.19.114.0\",\"family\":2,\"interface\":\"ens192\"},{\"destination\":\"10.88.0.0\",\"family\":2,\"interface\":\"cni-podman0\"},{\"destination\":\"::1\",\"family\":10,\"interface\":\"lo\"},{\"destination\":\"2620:52:0:1372::\",\"family\":10,\"interface\":\"ens192\"},{\"destination\":\"fe80::\",\"family\":10,\"interface\":\"ens192\"},{\"destination\":\"fe80::\",\"family\":10,\"interface\":\"cni-podman0\"},{\"destination\":\"::\",\"family\":10,\"gateway\":\"fe80::a81:f4ff:fea6:dc01\",\"interface\":\"ens192\"}],\"system_vendor\":{\"manufacturer\":\"VMware, Inc.\",\"product_name\":\"VMware Virtual Platform\",\"serial_number\":\"VMware-42 09 5f ea c8 4e 8c 88-f3 0c 06 65 5a 4d 32 fb\",\"virtual\":true},\"tpm_version\":\"none\"}"
const inventoryWithMultipleNICs string = "\"hostname\":\"localhost\",\"interfaces\":[{\"biosdevname\":\"em2\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"b4:7a:f1:da:fe:85\",\"mtu\":1500,\"name\":\"eno2\",\"product\":\"0x37ce\",\"speed_mbps\":-1,\"vendor\":\"0x8086\"},{\"biosdevname\":\"em1\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"has_carrier\":true,\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"b4:7a:f1:da:fe:84\",\"mtu\":1500,\"name\":\"eno1\",\"product\":\"0x1537\",\"speed_mbps\":1000,\"vendor\":\"0x8086\"},{\"biosdevname\":\"em3\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"b4:7a:f1:da:fe:86\",\"mtu\":1500,\"name\":\"eno3\",\"product\":\"0x37ce\",\"speed_mbps\":-1,\"vendor\":\"0x8086\"},{\"biosdevname\":\"em4\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"b4:7a:f1:da:fe:87\",\"mtu\":1500,\"name\":\"eno4\",\"product\":\"0x37ce\",\"speed_mbps\":-1,\"vendor\":\"0x8086\"},{\"biosdevname\":\"em5\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"b4:7a:f1:da:fe:88\",\"mtu\":1500,\"name\":\"eno5\",\"product\":\"0x37ce\",\"speed_mbps\":-1,\"vendor\":\"0x8086\"},{\"biosdevname\":\"p1p1\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"has_carrier\":true,\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"d4:f5:ef:56:35:64\",\"mtu\":8000,\"name\":\"ens1f0\",\"product\":\"0x158b\",\"speed_mbps\":25000,\"vendor\":\"0x8086\"},{\"biosdevname\":\"p1p2\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"has_carrier\":true,\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"d4:f5:ef:56:35:64\",\"mtu\":8000,\"name\":\"ens1f1\",\"product\":\"0x158b\",\"speed_mbps\":25000,\"vendor\":\"0x8086\"},{\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"has_carrier\":true,\"ipv4_addresses\":[\"10.195.70.120/24\"],\"ipv6_addresses\":[],\"mac_address\":\"d4:f5:ef:56:35:64\",\"mtu\":1500,\"name\":\"bond0\",\"speed_mbps\":25000}],\"memory\":{\"physical_bytes\":412316860416,\"physical_bytes_method\":\"dmidecode\",\"usable_bytes\":405391712256},\"routes\":[{\"destination\":\"0.0.0.0\",\"family\":2,\"gateway\":\"10.195.70.1\",\"interface\":\"bond0\"},{\"destination\":\"10.88.0.0\",\"family\":2,\"interface\":\"cni-podman0\"},{\"destination\":\"10.195.70.0\",\"family\":2,\"interface\":\"bond0\"},{\"destination\":\"::1\",\"family\":10,\"interface\":\"lo\"},{\"destination\":\"fe80::\",\"family\":10,\"interface\":\"cni-podman0\"}],\"system_vendor\":{\"manufacturer\":\"HPE\",\"product_name\":\"ProLiant e910\",\"serial_number\":\"MXQ1291HP3\"},\"timestamp\":1657725466,\"tpm_version\":\"2.0\"}"

func TestCommon(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Common test suite")
}

var _ = Describe("test PEM CA bundle verification", func() {
	It("full CA chain", func() {
		certs := []byte(redHatIntermediateChain + redHatRootCA)
		err := VerifyCaBundle(certs)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("full CA chain in reverse order", func() {
		certs := []byte(redHatRootCA + redHatIntermediateChain)
		err := VerifyCaBundle(certs)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("only root CA in the chain", func() {
		certs := []byte(redHatRootCA)
		err := VerifyCaBundle(certs)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("incomplete CA chain (no Root CA)", func() {
		certs := []byte(redHatIntermediateChain)
		err := VerifyCaBundle(certs)
		Expect(err).Should(HaveOccurred())
	})
	It("malformed PEM", func() {
		certs := []byte(malformedPEM)
		err := VerifyCaBundle(certs)
		Expect(err).Should(HaveOccurred())
	})
})

var _ = Describe("get hosts by role", func() {
	It("no hosts", func() {
		hosts := make([]*models.Host, 0)
		cluster := createClusterFromHosts(hosts)
		masters := GetHostsByRole(&cluster, models.HostRoleMaster)
		Expect(masters).Should(HaveLen(0))
	})
	It("3 masters 2 workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleWorker, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleWorker, models.HostStatusKnown))
		cluster := createClusterFromHosts(hosts)
		masters := GetHostsByRole(&cluster, models.HostRoleMaster)
		workers := GetHostsByRole(&cluster, models.HostRoleWorker)
		Expect(masters).Should(HaveLen(3))
		Expect(workers).Should(HaveLen(2))
	})
	It("5 workers", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(models.HostRoleWorker, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleWorker, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleWorker, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleWorker, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleWorker, models.HostStatusKnown))
		cluster := createClusterFromHosts(hosts)
		masters := GetHostsByRole(&cluster, models.HostRoleMaster)
		workers := GetHostsByRole(&cluster, models.HostRoleWorker)
		Expect(masters).Should(HaveLen(0))
		Expect(workers).Should(HaveLen(5))
	})
	It("5 masters", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusKnown))
		cluster := createClusterFromHosts(hosts)
		masters := GetHostsByRole(&cluster, models.HostRoleMaster)
		workers := GetHostsByRole(&cluster, models.HostRoleWorker)
		Expect(masters).Should(HaveLen(5))
		Expect(workers).Should(HaveLen(0))
	})
	It("5 nodes autoassign", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(models.HostRoleAutoAssign, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleAutoAssign, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleAutoAssign, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleAutoAssign, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleAutoAssign, models.HostStatusKnown))
		cluster := createClusterFromHosts(hosts)
		autoassigns := GetHostsByRole(&cluster, models.HostRoleAutoAssign)
		Expect(autoassigns).Should(HaveLen(5))
	})
})

var _ = Describe("compare OCP 4.10 versions", func() {
	It("GA release", func() {
		is410Version, _ := VersionGreaterOrEqual("4.10.0", "4.10.0-0.alpha")
		Expect(is410Version).Should(BeTrue())
	})
	It("pre-release", func() {
		is410Version, _ := VersionGreaterOrEqual("4.10.0-fc.1", "4.10.0-0.alpha")
		Expect(is410Version).Should(BeTrue())
	})
	It("pre-release z-stream", func() {
		is410Version, _ := VersionGreaterOrEqual("4.10.1-fc.1", "4.10.0-0.alpha")
		Expect(is410Version).Should(BeTrue())
	})
	It("nightly release", func() {
		is410Version, _ := VersionGreaterOrEqual("4.10.0-0.nightly-2022-01-23-013716", "4.10.0-0.alpha")
		Expect(is410Version).Should(BeTrue())
	})
})

var _ = Describe("Test AreMastersSchedulable", func() {
	Context("for every combination of schedulableMastersForcedTrue and schedulableMasters", func() {
		for _, test := range []struct {
			schedulableMastersForcedTrue bool
			schedulableMasters           bool
			expectedSchedulableMasters   bool
		}{
			{schedulableMastersForcedTrue: false, schedulableMasters: false, expectedSchedulableMasters: false},
			{schedulableMastersForcedTrue: false, schedulableMasters: true, expectedSchedulableMasters: true},
			{schedulableMastersForcedTrue: true, schedulableMasters: false, expectedSchedulableMasters: true},
			{schedulableMastersForcedTrue: true, schedulableMasters: true, expectedSchedulableMasters: true},
		} {
			test := test
			It(fmt.Sprintf("schedulableMastersForcedTrue=%v schedulableMasters=%v AreMastersSchedulable? %v", test.schedulableMastersForcedTrue, test.schedulableMasters, test.expectedSchedulableMasters), func() {
				cluster := &Cluster{
					Cluster: models.Cluster{
						SchedulableMastersForcedTrue: &test.schedulableMastersForcedTrue,
						SchedulableMasters:           &test.schedulableMasters,
					},
				}
				Expect(AreMastersSchedulable(cluster)).Should(Equal(test.expectedSchedulableMasters))
			})
		}
	})
})

var _ = DescribeTable(
	"Get tag from image reference",
	func(image, expected string) {
		actual := GetTagFromImageRef(image)
		Expect(actual).To(Equal(expected))
	},
	Entry("Empty", "", ""),
	Entry("No tag", "quay.io/my/image", ""),
	Entry("Latest tag", "quay.io/my/image:latest", "latest"),
	Entry("Numeric tag", "quay.io/my/image:1.2.3", "1.2.3"),
	Entry("Alphabetic tag", "quay.io/my/image:old", "old"),
	Entry("Version tag", "quay.io/my/image:v1.2.3", "v1.2.3"),
	Entry("Digest", "quay.io/image/agent@sha256:e7d2b565a30757833c911cf623b3d834804b21a67fbb37844e0071a08159afa5", ""),
	Entry("Incorrect", "a:b:c:d", ""),
)

var _ = Describe("Test GetInventoryInterfaces", func() {
	It("inventory with multiple interfaces", func() {
		expected := `[{"biosdevname":"em2","flags":["up","broadcast","multicast"],"ipv4_addresses":[],"ipv6_addresses":[],"mac_address":"b4:7a:f1:da:fe:85","mtu":1500,"name":"eno2","product":"0x37ce","speed_mbps":-1,"vendor":"0x8086"},{"biosdevname":"em1","flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":[],"ipv6_addresses":[],"mac_address":"b4:7a:f1:da:fe:84","mtu":1500,"name":"eno1","product":"0x1537","speed_mbps":1000,"vendor":"0x8086"},{"biosdevname":"em3","flags":["up","broadcast","multicast"],"ipv4_addresses":[],"ipv6_addresses":[],"mac_address":"b4:7a:f1:da:fe:86","mtu":1500,"name":"eno3","product":"0x37ce","speed_mbps":-1,"vendor":"0x8086"},{"biosdevname":"em4","flags":["up","broadcast","multicast"],"ipv4_addresses":[],"ipv6_addresses":[],"mac_address":"b4:7a:f1:da:fe:87","mtu":1500,"name":"eno4","product":"0x37ce","speed_mbps":-1,"vendor":"0x8086"},{"biosdevname":"em5","flags":["up","broadcast","multicast"],"ipv4_addresses":[],"ipv6_addresses":[],"mac_address":"b4:7a:f1:da:fe:88","mtu":1500,"name":"eno5","product":"0x37ce","speed_mbps":-1,"vendor":"0x8086"},{"biosdevname":"p1p1","flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":[],"ipv6_addresses":[],"mac_address":"d4:f5:ef:56:35:64","mtu":8000,"name":"ens1f0","product":"0x158b","speed_mbps":25000,"vendor":"0x8086"},{"biosdevname":"p1p2","flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":[],"ipv6_addresses":[],"mac_address":"d4:f5:ef:56:35:64","mtu":8000,"name":"ens1f1","product":"0x158b","speed_mbps":25000,"vendor":"0x8086"},{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["10.195.70.120/24"],"ipv6_addresses":[],"mac_address":"d4:f5:ef:56:35:64","mtu":1500,"name":"bond0","speed_mbps":25000}]`
		res, err := GetInventoryInterfaces(inventoryWithMultipleNICs)
		Expect(res).To(Equal(expected))
		Expect(err).ToNot(HaveOccurred())
	})

	It("inventory with single interface", func() {
		expected := `[{"flags":["up","broadcast","multicast"],"has_carrier":true,"ipv4_addresses":["10.19.114.222/23"],"ipv6_addresses":["2620:52:0:1372:b55f:731b:f1dc:d773/64"],"mac_address":"00:50:56:83:87:09","mtu":1500,"name":"ens192","product":"0x07b0","speed_mbps":10000,"type":"physical","vendor":"0x15ad"}]`
		res, err := GetInventoryInterfaces(inventoryWithSingleNIC)
		Expect(res).To(Equal(expected))
		Expect(err).ToNot(HaveOccurred())
	})

	It("empty inventory", func() {
		res, err := GetInventoryInterfaces("")
		Expect(res).To(Equal(""))
		Expect(err.Error()).Should(Equal("unable to find interfaces in the inventory"))
	})

	// Tests inventory containing start of interfaces section but cut in the middle
	// so that the section is not closed correctly.
	It("malformed inventory", func() {
		res, err := GetInventoryInterfaces("\"hostname\":\"localhost\",\"interfaces\":[{\"biosdevname\":\"em2\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"b4:7a:f1:da:fe:85\",\"mtu\":1500,\"name\":\"eno2\",\"product\":\"0x37ce\",\"speed_mbps\":-1,\"vendor\":\"0x8086\"},{\"biosdevname\":\"em1\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"has_carrier\":true,\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"b4:7a:f1:da:fe:84\",\"mtu\":1500,\"name\":\"eno1\",\"product\":\"0x1537\",\"speed_mbps\":1000,\"vendor\":\"0x8086\"},{\"biosdevname\":\"em3\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"b4:7a:f1:da:fe:86\",\"mtu\":1500,\"name\":\"eno3\",\"product\":\"0x37ce\",\"speed_mbps\":-1,\"vendor\":\"0x8086\"},{\"biosdevname\":\"em4\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"b4:7a:f1:da:fe:87\",\"mtu\":1500,\"name\":\"eno4\",\"product\":\"0x37ce\",\"speed_mbps\":-1,\"vendor\":\"0x8086\"},{\"biosdevname\":\"em5\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"b4:7a:f1:da:fe:88\",\"mtu\":1500,\"name\":\"eno5\",\"product\":\"0x37ce\",\"speed_mbps\":-1,\"vendor\":\"0x8086\"},{\"biosdevname\":\"p1p1\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"has_carrier\":true,\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"d4:f5:ef:56:35:64\",\"mtu\":8000,\"name\":\"ens1f0\",\"product\":\"0x158b\",\"speed_mbps\":25000,\"vendor\":\"0x8086\"},{\"biosdevname\":\"p1p2\",\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"has_carrier\":true,\"ipv4_addresses\":[],\"ipv6_addresses\":[],\"mac_address\":\"d4:f5:ef:56:35:64\",\"mtu\":8000,\"name\":\"ens1f1\",\"product\":\"0x158b\",\"speed_mbps\":25000,\"vendor\":\"0x8086\"},{\"flags\":[\"up\",\"broadcast\",\"multicast\"],\"has_carrier\":true,\"ipv4_addresses\":[\"10.195.70.120/24\"],\"ipv6_addresses\":[],\"mac_address\":\"d4:f5:ef:5")
		Expect(res).To(Equal(""))
		Expect(err.Error()).Should(Equal("inventory is malformed"))
	})
})

var _ = Describe("db features", func() {
	var (
		db *gorm.DB
	)
	BeforeEach(func() {
		db, _ = PrepareTestDB()
	})
	AfterEach(func() {
		CloseDB(db)
	})
	Context("embedded struct", func() {
		type inner struct {
			String *string
			Int    int
		}
		type Outer struct {
			Inner *inner `gorm:"embedded;embeddedPrefix:inner_"`
			ID    int
		}
		BeforeEach(func() {
			Expect(db.Migrator().AutoMigrate(&Outer{})).ToNot(HaveOccurred())
		})
		It("default embedded struct is not nil", func() {
			Expect(db.Create(&Outer{ID: 1}).Error).ToNot(HaveOccurred())
			var outer Outer
			Expect(db.Where("inner_string is null and inner_int is null").Take(&outer).Error).ToNot(HaveOccurred())
			Expect(outer.Inner).ToNot(BeNil())
			Expect(outer.Inner.Int).To(Equal(0))
			Expect(outer.Inner.String).To(BeNil())
		})
		It("embedded struct with values", func() {
			Expect(db.Create(&Outer{ID: 1, Inner: &inner{String: swag.String("blah")}}).Error).ToNot(HaveOccurred())
			var outer Outer
			Expect(db.Where("inner_string = 'blah'").Take(&outer).Error).ToNot(HaveOccurred())
			Expect(outer.Inner).ToNot(BeNil())
			Expect(outer.Inner.Int).To(Equal(0))
			Expect(outer.Inner.String).To(Equal(swag.String("blah")))
		})
	})
})

/**
* This test is to ensure that our assumptions about how golang handles the serialisation of a map is consistent with how we understand this to work.
* to cover any changes in the implementation of json.Marshal that could affect the order in which items are serialized.
 */
var _ = Describe("JSON serialization checks", func() {
	It("json serialization of a map should return a consistent string for the same entries irrespective of the order in which they were added", func() {
		testMap := func() string {
			var slice []string
			for i := 0; i != 1000; i++ {
				slice = append(slice, fmt.Sprintf("value %d", i))
			}
			m := make(map[string]string)
			for i := 0; i != 1000; i++ {
				r, err := rand.Int(rand.Reader, big.NewInt(math.MaxUint32))
				Expect(err).ToNot(HaveOccurred())
				index := int(r.Int64()) % len(slice)
				value := slice[index]
				slice = append(slice[:index], slice[index+1:]...)
				m[value] = value
			}
			j, e := json.Marshal(m)
			if e != nil {
				fmt.Println("Error")
			}
			return string(j)
		}
		v := testMap()
		for i := 0; i != 100; i++ {
			Expect(testMap()).To(Equal(v))
		}
	})
})

func createHost(hostRole models.HostRole, state string) *models.Host {
	hostId := strfmt.UUID(uuid.New().String())
	clusterId := strfmt.UUID(uuid.New().String())
	infraEnvId := strfmt.UUID(uuid.New().String())
	host := models.Host{
		ID:         &hostId,
		InfraEnvID: infraEnvId,
		ClusterID:  &clusterId,
		Kind:       swag.String(models.HostKindHost),
		Status:     swag.String(state),
		Role:       hostRole,
	}
	return &host
}

func createClusterFromHosts(hosts []*models.Host) Cluster {
	return Cluster{
		Cluster: models.Cluster{
			APIVip:           "192.168.10.10",
			Hosts:            hosts,
			IngressVip:       "192.168.10.11",
			OpenshiftVersion: "4.9",
		},
	}
}
