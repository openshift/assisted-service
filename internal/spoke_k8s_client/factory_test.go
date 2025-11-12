package spoke_k8s_client

import (
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/system"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const testCert string = `-----BEGIN CERTIFICATE-----
MIIFPjCCAyagAwIBAgIUBCE1YX2zJ0R/3NURq2XQaciEuVQwDQYJKoZIhvcNAQEL
BQAwFjEUMBIGA1UEAwwLZXhhbXBsZS5jb20wHhcNMjIxMTI3MjM0MjAyWhcNMzIx
MTI0MjM0MjAyWjAWMRQwEgYDVQQDDAtleGFtcGxlLmNvbTCCAiIwDQYJKoZIhvcN
AQEBBQADggIPADCCAgoCggIBAKY589W+Xifs9SfxofBI1r1/NKsMUVPvg3ZtDIPQ
EeNKf5OgtSOVFcoEmkS7ZWNTIu4Kd1WBf/rG+F5lm/aTTa3j720Q+fS+gsveGQPz
7taUpU/TjHHzoCqjjhaYMr4gIJ3jkpTXUWG5/vka/oNykSxkGCuZw1gyXHNujA8L
DJYY8VNUHPl5MmXGaT++6yEN4WdB2f7R/MmEaH6KnGo/LjhMeiVmDsIxHZ/xW9OR
izPklnUi78NfZJSxiknoV6CnQShNijLEq6nQowYQ1lQuNWs6sTM28I0BYWk+gDUz
NOWkVqSHFRMzGmpqYJs7JQiv0g33VN/92dwdP/kZc9sAYRqDaI6hplOZrD/OEsbG
lmN90x/o42wotJeBDN1hHlJ1JeRjR1Vk8XUfOmaTuOPzooKIM0h9K6Ah6u3lRQtE
n68yxn0sGD8yw6EydS5FD9zzvA6rgXBSsvpMFjk/N/FmnIzD4YinLEiflfub1O0M
9thEOX9IaOh00U2eGsRa/MOJcCZ5TUOgxVlv15ATUPHo1MW8QkmYOVx4BoM/Bw0J
0HibIU8VUw2AV1tupRdQma7Qg5gyjdx2doth78IG5+LkX95fSyz60Kf9l1xBQHNA
kVyzkXlx8jmdm53CeFvHVOrVrLuA2Dk+t21TNL1uFGgQ0iLxItCf1O6F6B78QqhI
YLOdAgMBAAGjgYMwgYAwHQYDVR0OBBYEFE6DFh3+wGzA8dOYBTL9Z0CyxLJ/MB8G
A1UdIwQYMBaAFE6DFh3+wGzA8dOYBTL9Z0CyxLJ/MA8GA1UdEwEB/wQFMAMBAf8w
LQYDVR0RBCYwJIILZXhhbXBsZS5jb22CD3d3dy5leGFtcGxlLm5ldIcECgAAATAN
BgkqhkiG9w0BAQsFAAOCAgEAoj+elkYHrek6DoqOvEFZZtRp6bPvof61/VJ3kP7x
HZXp5yVxvGOHt61YRziGLpsFbuiDczk0V61ZdozHUOtZ0sWB4VeyO1pAjfd/JwDI
CK6olfkSO78WFQfdG4lNoSM9dQJyEIEZ1sbvuUL3RHDBd9oEKue+vsstlM9ahdoq
fpTTFq4ENGCAIDvaqKIlpjKsAMrsTO47CKPVh2HUpugfVGKeBRsW1KAXFoC2INS5
7BY3h60jFFW6bz0v+FnzW96Mt2VNW+i/REX6fBaR4m/QfG81rA2EEmhxCGrany+N
6DUkwiJxcqBMH9jA2yVnF7BgwG2C3geBqXTTlvVQJD8GOktkvgLjlHcYqO1pI7B3
wP9F9ZF+w39jXwGMGBg8+/aQz1RjP2bOb18n7d0bc4/pbbkVAmE4sq4qMneFZAVE
uj9S2Jna3ut08ZP05Ych5vCGX4VJ8gNNgrJju2PJVBl8NNyDfHKeHfWSOR9uOMjT
vqK6iRD9xqu/oLJyrlAuOL8ZxRpeqjxF/g8NYYV/fvv8apaX58ua9qYAFQVGf590
mmjOozzn9VBqKenVmfwzen5v78CBSgS4Hd72Qp42rLCNgqI8gyQa2qZzaNjLP/wI
pBpFC21fkybGYPkislPQ3EI69ZGRafWDBjlFFTS3YkDM98tqTZD+JG4STY+ivHhK
gmY=
-----END CERTIFICATE-----`

const testCert2 string = `-----BEGIN CERTIFICATE-----
MIIFPjCCAyagAwIBAgIUV3ZmDsSwF6/E2CPhFChz3w14OLMwDQYJKoZIhvcNAQEL
BQAwFjEUMBIGA1UEAwwLZXhhbXBsZS5jb20wHhcNMjIxMTI3MjM0MjMwWhcNMzIx
MTI0MjM0MjMwWjAWMRQwEgYDVQQDDAtleGFtcGxlLmNvbTCCAiIwDQYJKoZIhvcN
AQEBBQADggIPADCCAgoCggIBALxURtV3Wd8NEFIplXSZpIdx5I0jFU8thmb2vZON
oNxr31OsHYqkA07RpGSmyn+hv03OI9g4AzuMGs48XoPxZGtWUr0wany1LDDW8t/J
PytYeZyXAJM0zl6/AlzSzYRPk22LykdzVBQosUeRP42a2xWEdDRkJqxxBHQ0eLiC
9g32w57YomhbgCR2OnUxzVmMuQmk987WG7u3/ssSBPEuIebOoX+6G3uLaw/Ka6zQ
XGzRgFq3mskPVfw3exQ46WZfgu6PtG5zxKmty75fNPPwdyw+lwm3u8pH5jpJYvOZ
RHbk7+nxWxLxe5r3FzaNeWskb24J9x53nQzwfcF0MtuRvMycO1i/3e5Y4TanEmmu
GbUOKlJxyaFQaVa2udWAxZ8w1W5u4aKrBprXEAXXDghXbxrgRry2zPO1vqZ/aLH8
YKnHLifjdsNMxrA3nsKAViY0erwYmTF+c551gxkW7vZCtJStzDcMVM16U76jato7
fNb64VUtviVCWeHvh7aTpxENPCh6T8eGh3K4HUESTNpBggs3TXhF1yEcS+aKVJ3z
6CZcke1ph/vpMt/684xx8tICp2KMWbwk3nIBaMw84hrVZyKFgpW/gZOE+ktV91zw
LF1oFn+2F8PwGSphBwhBE0uoyFRNmUXiPsHUyEh7kF7EU5gb1sxTzM5sWCNm6nIS
QRlXAgMBAAGjgYMwgYAwHQYDVR0OBBYEFHuAjvmIDJX76uWtnfirReeBU+f2MB8G
A1UdIwQYMBaAFHuAjvmIDJX76uWtnfirReeBU+f2MA8GA1UdEwEB/wQFMAMBAf8w
LQYDVR0RBCYwJIILZXhhbXBsZS5jb22CD3d3dy5leGFtcGxlLm5ldIcECgAAATAN
BgkqhkiG9w0BAQsFAAOCAgEACn2BTzH89jDBHAy1rREJY8nYhH8GQxsPQn3MZAjA
OiAQRSqqaduYdM+Q6X3V/A8n2vtS1vjs2msQwg6uNN/yNNgdo+Nobj74FmF+kwaf
hodvMJ7z+MyeuxONYL/rbolc8N031nPWim8HTQsS/hxiiwqMHzgz6hQou1OFPwTJ
QdhsfXgqbNRiMkF/UxLfIDEP8J5VAEzVJlyrGUrUOuaMU6TZ+tx1VbNQm3Xum5GW
UgtmE36wWp/M1VeNSsm3GOQRlyWFGmE0sgA95IxLRMgL1mpd8IS3iU6TVZLx0+sA
Bly38R1z8Vcwr1vOurQ8g76Epdet2ZkQNQBwvgeVvnCsoy4CQf2AvDzKgEeTdXMM
WdO6UnG2+PgJ6YQHyfCB34mjPqrJul/0YwWo/p+PxSHRKdJZJTKzZPi1sPuxA2iO
YiJIS94ZRlkPxrD4pYdGiXPigC+0motT6cYxQ8SKTVOs7aEax/xQngrcQPLNXTgn
LtoT4hLCJpP7PTLgL91Dvu/dUMR4SEUNojUBul67D5fIjD0sZvJFZGd78apl/gdf
PxkCHm4A07Zwl/x+89Ia73mk+y8O2u+CGh7oDrO565ADxKj6/UhxhVKmV9DG1ono
AjGUGkvXVVvurf5CwGxpwT/G5UXpSK+314eMVxz5s3yDb2J2J2rvIk6ROPxBK0ws
Sj8=
-----END CERTIFICATE-----`

var (
	controller     *gomock.Controller
	mockSystemInfo *system.MockSystemInfo
)

var _ = Describe("Factory", func() {
	Describe("Creation", func() {

		BeforeEach(func() {
			controller = gomock.NewController(GinkgoT())
			mockSystemInfo = system.NewMockSystemInfo(controller)
		})

		It("Can't be created without a logger", func() {
			client, err := NewFactory(nil, nil, mockSystemInfo)
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(client).To(BeNil())
		})

		It("Can't be created without system info", func() {
			client, err := NewFactory(logger, nil, nil)
			Expect(err).To(MatchError("sys is mandatory"))
			Expect(client).To(BeNil())
		})
	})

	Describe("Create spoke client from secret", func() {
		DescribeTable(
			"Fails if secret doesn't contain a valid kubeconfig",
			func(data map[string][]byte, matcher OmegaMatcher) {
				controller = gomock.NewController(GinkgoT())
				mockSystemInfo = system.NewMockSystemInfo(controller)

				// Create the secret:
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNamespace.Name,
						Name:      "admin-kubeconfig",
					},
					Data: data,
				}
				err := hubClient.Create(ctx, secret)
				Expect(err).ToNot(HaveOccurred())

				// Create the factory:
				factory, err := NewFactory(logger, nil, mockSystemInfo)
				Expect(err).ToNot(HaveOccurred())

				// Check the error:
				_, err = factory.CreateFromSecret(nil, secret)
				Expect(err).To(MatchError(matcher))
			},
			Entry(
				"Secret data is nil",
				nil,
				MatchRegexp("secret '.*/admin-kubeconfig' is empty"),
			),
			Entry(
				"Secret data is empty",
				map[string][]byte{},
				MatchRegexp("secret '.*/admin-kubeconfig' is empty"),
			),
			Entry(
				"Secret data doesn't contain a 'kubeconfig' key",
				map[string][]byte{
					"mydata": []byte("myvalue"),
				},
				MatchRegexp("secret '.*/admin-kubeconfig' doesn't contain the 'kubeconfig' key"),
			),
			Entry(
				"Secret data contains a 'kubeconfig' data item with junk",
				map[string][]byte{
					"kubeconfig": []byte("junk"),
				},
				ContainSubstring("cannot unmarshal"),
			),
		)

		When("Secret contains a valid kubeconfig", func() {
			var (
				clusterDeployment *hivev1.ClusterDeployment
				kubeconfigSecret  *corev1.Secret
			)

			BeforeEach(func() {
				controller = gomock.NewController(GinkgoT())
				mockSystemInfo = system.NewMockSystemInfo(controller)

				// Create the kubeconfig with testCert as the CA data:
				kubeconfig := common.Dedent(`
					apiVersion: v1
					kind: Config
					clusters:
					- name: mycluster
					  cluster:
					    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZQakNDQXlhZ0F3SUJBZ0lVQkNFMVlYMnpKMFIvM05VUnEyWFFhY2lFdVZRd0RRWUpLb1pJaHZjTkFRRUwKQlFBd0ZqRVVNQklHQTFVRUF3d0xaWGhoYlhCc1pTNWpiMjB3SGhjTk1qSXhNVEkzTWpNME1qQXlXaGNOTXpJeApNVEkwTWpNME1qQXlXakFXTVJRd0VnWURWUVFEREF0bGVHRnRjR3hsTG1OdmJUQ0NBaUl3RFFZSktvWklodmNOCkFRRUJCUUFEZ2dJUEFEQ0NBZ29DZ2dJQkFLWTU4OVcrWGlmczlTZnhvZkJJMXIxL05Lc01VVlB2ZzNadERJUFEKRWVOS2Y1T2d0U09WRmNvRW1rUzdaV05USXU0S2QxV0JmL3JHK0Y1bG0vYVRUYTNqNzIwUStmUytnc3ZlR1FQego3dGFVcFUvVGpISHpvQ3FqamhhWU1yNGdJSjNqa3BUWFVXRzUvdmthL29OeWtTeGtHQ3VadzFneVhITnVqQThMCkRKWVk4Vk5VSFBsNU1tWEdhVCsrNnlFTjRXZEIyZjdSL01tRWFINktuR28vTGpoTWVpVm1Ec0l4SFoveFc5T1IKaXpQa2xuVWk3OE5mWkpTeGlrbm9WNkNuUVNoTmlqTEVxNm5Rb3dZUTFsUXVOV3M2c1RNMjhJMEJZV2srZ0RVegpOT1drVnFTSEZSTXpHbXBxWUpzN0pRaXYwZzMzVk4vOTJkd2RQL2taYzlzQVlScURhSTZocGxPWnJEL09Fc2JHCmxtTjkweC9vNDJ3b3RKZUJETjFoSGxKMUplUmpSMVZrOFhVZk9tYVR1T1B6b29LSU0waDlLNkFoNnUzbFJRdEUKbjY4eXhuMHNHRDh5dzZFeWRTNUZEOXp6dkE2cmdYQlNzdnBNRmprL04vRm1uSXpENFlpbkxFaWZsZnViMU8wTQo5dGhFT1g5SWFPaDAwVTJlR3NSYS9NT0pjQ1o1VFVPZ3hWbHYxNUFUVVBIbzFNVzhRa21ZT1Z4NEJvTS9CdzBKCjBIaWJJVThWVXcyQVYxdHVwUmRRbWE3UWc1Z3lqZHgyZG90aDc4SUc1K0xrWDk1ZlN5ejYwS2Y5bDF4QlFITkEKa1Z5emtYbHg4am1kbTUzQ2VGdkhWT3JWckx1QTJEayt0MjFUTkwxdUZHZ1EwaUx4SXRDZjFPNkY2Qjc4UXFoSQpZTE9kQWdNQkFBR2pnWU13Z1lBd0hRWURWUjBPQkJZRUZFNkRGaDMrd0d6QThkT1lCVEw5WjBDeXhMSi9NQjhHCkExVWRJd1FZTUJhQUZFNkRGaDMrd0d6QThkT1lCVEw5WjBDeXhMSi9NQThHQTFVZEV3RUIvd1FGTUFNQkFmOHcKTFFZRFZSMFJCQ1l3SklJTFpYaGhiWEJzWlM1amIyMkNEM2QzZHk1bGVHRnRjR3hsTG01bGRJY0VDZ0FBQVRBTgpCZ2txaGtpRzl3MEJBUXNGQUFPQ0FnRUFvaitlbGtZSHJlazZEb3FPdkVGWlp0UnA2YlB2b2Y2MS9WSjNrUDd4CkhaWHA1eVZ4dkdPSHQ2MVlSemlHTHBzRmJ1aURjemswVjYxWmRvekhVT3RaMHNXQjRWZXlPMXBBamZkL0p3REkKQ0s2b2xma1NPNzhXRlFmZEc0bE5vU005ZFFKeUVJRVoxc2J2dVVMM1JIREJkOW9FS3VlK3Zzc3RsTTlhaGRvcQpmcFRURnE0RU5HQ0FJRHZhcUtJbHBqS3NBTXJzVE80N0NLUFZoMkhVcHVnZlZHS2VCUnNXMUtBWEZvQzJJTlM1CjdCWTNoNjBqRkZXNmJ6MHYrRm56Vzk2TXQyVk5XK2kvUkVYNmZCYVI0bS9RZkc4MXJBMkVFbWh4Q0dyYW55K04KNkRVa3dpSnhjcUJNSDlqQTJ5Vm5GN0Jnd0cyQzNnZUJxWFRUbHZWUUpEOEdPa3RrdmdMamxIY1lxTzFwSTdCMwp3UDlGOVpGK3czOWpYd0dNR0JnOCsvYVF6MVJqUDJiT2IxOG43ZDBiYzQvcGJia1ZBbUU0c3E0cU1uZUZaQVZFCnVqOVMySm5hM3V0MDhaUDA1WWNoNXZDR1g0Vko4Z05OZ3JKanUyUEpWQmw4Tk55RGZIS2VIZldTT1I5dU9NalQKdnFLNmlSRDl4cXUvb0xKeXJsQXVPTDhaeFJwZXFqeEYvZzhOWVlWL2Z2djhhcGFYNTh1YTlxWUFGUVZHZjU5MAptbWpPb3p6bjlWQnFLZW5WbWZ3emVuNXY3OENCU2dTNEhkNzJRcDQyckxDTmdxSThneVFhMnFaemFOakxQL3dJCnBCcEZDMjFma3liR1lQa2lzbFBRM0VJNjlaR1JhZldEQmpsRkZUUzNZa0RNOTh0cVRaRCtKRzRTVFkraXZIaEsKZ21ZPQotLS0tLUVORCBDRVJUSUZJQ0FURS0tLS0tCg==
					    server: https://mylb:32132
					users:
					- name: myuser
					  user:
					    username: myuser
					    password: mypassword
					contexts:
					- name: mycontext
					  context:
					    cluster: mycluster
					    user: myuser
					current-context: mycontext
				`)

				// Create the cluster deployment:
				clusterDeployment = &hivev1.ClusterDeployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNamespace.Name,
						Name:      "mycluster",
						Labels:    map[string]string{},
					},
				}

				// Create the secret:
				kubeconfigSecret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNamespace.Name,
						Name:      "admin-kubeconfig",
					},
					Data: map[string][]byte{
						"kubeconfig": []byte(kubeconfig),
					},
				}
			})

			AfterEach(func() {
				controller.Finish()
			})

			It("Replaces API server address for hosted clusters", func() {
				mockSystemInfo.EXPECT().GetSystemCABundle().Return([]byte(testCert2), nil).Times(1)

				// Prepare a cluster deployment with the  'agentClusterRef' label, as that is what marks
				// it as a hosted cluster.
				clusterDeployment.Labels["agentClusterRef"] = "mycluster"

				// For this test we need to create the factory with a transport wrapper that verifies
				// that the address has been changed. Note also that this transport will always return
				// an error, as we dont really care about the rest of the processing.
				transport := ghttp.RoundTripperFunc(
					func(request *http.Request) (response *http.Response, err error) {
						address := request.URL.String()
						Expect(address).To(HavePrefix(
							"https://kube-apiserver.%s.svc:6443/",
							hubNamespace.Name,
						))
						err = errors.New("myerror")
						return
					},
				)
				factory, err := NewFactory(
					logger,
					func(http.RoundTripper) http.RoundTripper {
						return transport
					},
					mockSystemInfo,
				)
				Expect(err).ToNot(HaveOccurred())

				// Get the client:
				client, err := factory.CreateFromSecret(clusterDeployment, kubeconfigSecret)
				Expect(err).ToNot(HaveOccurred())

				// Send a request with the client. This will fail because our transport wrapper fails
				// all requests, but it allows us to verify that the API server address has been
				// changed.
				configMap := &corev1.ConfigMap{}
				configMapKey := types.NamespacedName{
					Namespace: hubNamespace.Name,
					Name:      "myconfig",
				}
				err = client.Get(ctx, configMapKey, configMap)
				Expect(err).To(MatchError(ContainSubstring("myerror")))
			})

			It("Doesn't replace API server address for regular clusters", func() {
				mockSystemInfo.EXPECT().GetSystemCABundle().Return([]byte(testCert2), nil).Times(1)

				// Prepare a cluster deployment without the 'agentClusterRef' label, as that is what
				// marks it as a hosted cluster:
				delete(clusterDeployment.Labels, "agentClusterRef")

				// For this test we need to create the factory with a transport wrapper that verifies
				// that the address hasn't been changed. Note also that this transport will always
				// return an error, as we dont really care about the rest of the processing.
				transport := ghttp.RoundTripperFunc(
					func(request *http.Request) (response *http.Response, err error) {
						address := request.URL.String()
						Expect(address).To(HavePrefix("https://mylb:32132/"))
						err = errors.New("myerror")
						return
					},
				)
				factory, err := NewFactory(
					logger,
					func(http.RoundTripper) http.RoundTripper {
						return transport
					},
					mockSystemInfo,
				)
				Expect(err).ToNot(HaveOccurred())

				// Get the client:
				client, err := factory.CreateFromSecret(clusterDeployment, kubeconfigSecret)
				Expect(err).ToNot(HaveOccurred())

				// Send a request with the client. This will fail because our transport wrapper fails
				// all requests, but it allows us to verify that the API server address has been
				// changed.
				configMap := &corev1.ConfigMap{}
				configMapKey := types.NamespacedName{
					Namespace: hubNamespace.Name,
					Name:      "myconfig",
				}
				err = client.Get(ctx, configMapKey, configMap)
				Expect(err).To(MatchError(ContainSubstring("myerror")))
			})

			It("Doesn't replace API server address if no cluster deployment is passed", func() {
				mockSystemInfo.EXPECT().GetSystemCABundle().Return([]byte(testCert2), nil).Times(1)

				// For this test we need to create the factory with a transport wrapper that verifies
				// that the address hasn't been changed. Note also that this transport will always
				// return an error, as we dont really care about the rest of the processing.
				transport := ghttp.RoundTripperFunc(
					func(request *http.Request) (response *http.Response, err error) {
						address := request.URL.String()
						Expect(address).To(HavePrefix("https://mylb:32132/"))
						err = errors.New("myerror")
						return
					},
				)
				factory, err := NewFactory(
					logger,
					func(http.RoundTripper) http.RoundTripper {
						return transport
					},
					mockSystemInfo,
				)
				Expect(err).ToNot(HaveOccurred())

				// Get the client:
				client, err := factory.CreateFromSecret(nil, kubeconfigSecret)
				Expect(err).ToNot(HaveOccurred())

				// Send a request with the client. This will fail because our transport wrapper fails
				// all requests, but it allows us to verify that the API server address has been
				// changed.
				configMap := &corev1.ConfigMap{}
				configMapKey := types.NamespacedName{
					Namespace: hubNamespace.Name,
					Name:      "myconfig",
				}
				err = client.Get(ctx, configMapKey, configMap)
				Expect(err).To(MatchError(ContainSubstring("myerror")))
			})

			It("System CA bundle has 1 cert that's not in the kubeconfig", func() {
				mockSystemInfo.EXPECT().GetSystemCABundle().Return([]byte(testCert2), nil).Times(1)

				// Transport wrapper to capture and assert RootCAs content
				called := false
				wrapper := func(rt http.RoundTripper) http.RoundTripper {
					called = true
					tr, ok := rt.(*http.Transport)
					Expect(ok).To(BeTrue())
					Expect(tr.TLSClientConfig).ToNot(BeNil())
					Expect(tr.TLSClientConfig.RootCAs).ToNot(BeNil())
					// Expect 2 subjects: one from kubeconfig and one from system bundle
					Expect(len(tr.TLSClientConfig.RootCAs.Subjects())).To(Equal(2))
					return rt
				}

				factory, err := NewFactory(logger, wrapper, mockSystemInfo)
				Expect(err).ToNot(HaveOccurred())

				_, err = factory.CreateFromSecret(clusterDeployment, kubeconfigSecret)
				Expect(err).ToNot(HaveOccurred())
				Expect(called).To(BeTrue())
			})

			It("System CA bundle has 1 cert that is in the kubeconfig", func() {
				mockSystemInfo.EXPECT().GetSystemCABundle().Return([]byte(testCert), nil).Times(1)

				// Transport wrapper to capture and assert RootCAs content
				called := false
				wrapper := func(rt http.RoundTripper) http.RoundTripper {
					called = true
					tr, ok := rt.(*http.Transport)
					Expect(ok).To(BeTrue())
					Expect(tr.TLSClientConfig).ToNot(BeNil())
					Expect(tr.TLSClientConfig.RootCAs).ToNot(BeNil())
					// Expect 1 subjects: one from kubeconfig
					Expect(len(tr.TLSClientConfig.RootCAs.Subjects())).To(Equal(1))
					return rt
				}

				factory, err := NewFactory(logger, wrapper, mockSystemInfo)
				Expect(err).ToNot(HaveOccurred())

				_, err = factory.CreateFromSecret(clusterDeployment, kubeconfigSecret)
				Expect(err).ToNot(HaveOccurred())
				Expect(called).To(BeTrue())
			})

			It("System CA bundle is empty", func() {
				mockSystemInfo.EXPECT().GetSystemCABundle().Return([]byte{}, nil).Times(1)

				// Transport wrapper to capture and assert RootCAs content
				called := false
				wrapper := func(rt http.RoundTripper) http.RoundTripper {
					called = true
					tr, ok := rt.(*http.Transport)
					Expect(ok).To(BeTrue())
					Expect(tr.TLSClientConfig).ToNot(BeNil())
					Expect(tr.TLSClientConfig.RootCAs).ToNot(BeNil())
					// Expect 1 subjects: one from kubeconfig
					Expect(len(tr.TLSClientConfig.RootCAs.Subjects())).To(Equal(1))
					return rt
				}

				factory, err := NewFactory(logger, wrapper, mockSystemInfo)
				Expect(err).ToNot(HaveOccurred())

				_, err = factory.CreateFromSecret(clusterDeployment, kubeconfigSecret)
				Expect(err).ToNot(HaveOccurred())
				Expect(called).To(BeTrue())
			})
		})
	})
})
