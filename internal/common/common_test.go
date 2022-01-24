package common

import (
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
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

var _ = Describe("test PEM CA bundle verification", func() {
	It("full CA chain", func() {
		certs := []byte(redHatIntermediateChain + redHatRootCA)
		err := VerifyCaBundle(certs)
		Expect(err).ShouldNot(HaveOccurred())
	})
	It("incomplete CA chain", func() {
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
	It("3 masters 2 workers - 1 master disabled", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusDisabled))
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleMaster, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleWorker, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleWorker, models.HostStatusKnown))
		cluster := createClusterFromHosts(hosts)
		masters := GetHostsByRole(&cluster, models.HostRoleMaster)
		workers := GetHostsByRole(&cluster, models.HostRoleWorker)
		Expect(masters).Should(HaveLen(2))
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
		masters := GetHostsByRole(&cluster, models.HostRoleMaster)
		workers := GetHostsByRole(&cluster, models.HostRoleWorker)
		Expect(masters).Should(HaveLen(3))
		Expect(workers).Should(HaveLen(2))
	})
	It("5 nodes autoassign - 1 disabled", func() {
		hosts := make([]*models.Host, 0)
		hosts = append(hosts, createHost(models.HostRoleAutoAssign, models.HostStatusDisabled))
		hosts = append(hosts, createHost(models.HostRoleAutoAssign, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleAutoAssign, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleAutoAssign, models.HostStatusKnown))
		hosts = append(hosts, createHost(models.HostRoleAutoAssign, models.HostStatusKnown))
		cluster := createClusterFromHosts(hosts)
		masters := GetHostsByRole(&cluster, models.HostRoleMaster)
		workers := GetHostsByRole(&cluster, models.HostRoleWorker)
		Expect(masters).Should(HaveLen(3))
		Expect(workers).Should(HaveLen(1))
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
