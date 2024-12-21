package spoke_k8s_client

import (
	"net/http"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/ghttp"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/assisted-service/internal/common"
)

var _ = Describe("Factory", func() {
	Describe("Creation", func() {
		It("Can't be created without a logger", func() {
			client, err := NewFactory(nil, hubClient, nil)
			Expect(err).To(MatchError("logger is mandatory"))
			Expect(client).To(BeNil())
		})

		It("Can't be created without a hub client", func() {
			client, err := NewFactory(logger, nil, nil)
			Expect(err).To(MatchError("hub client is mandatory"))
			Expect(client).To(BeNil())
		})

		It("Can be created with logger and hub client", func() {
			client, err := NewFactory(logger, hubClient, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(client).ToNot(BeNil())
		})
	})

	Describe("Create spoke client from secret", func() {
		DescribeTable(
			"Fails if secret doesn't contain a valid kubeconfig",
			func(data map[string][]byte, matcher OmegaMatcher) {
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
				factory, err := NewFactory(logger, hubClient, nil)
				Expect(err).ToNot(HaveOccurred())

				// Check the error:
				_, err = factory.CreateFromSecret(ctx, secret)
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
			var kubeconfigSecret *corev1.Secret

			BeforeEach(func() {
				// Create the kubeconfig:
				kubeconfig := common.Dedent(`
					apiVersion: v1
					kind: Config
					clusters:
					- name: mycluster
					  cluster:
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
				err := hubClient.Create(ctx, kubeconfigSecret)
				Expect(err).ToNot(HaveOccurred())

				// Create the cluster deployment:
				clusterDeployment := &hivev1.ClusterDeployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNamespace.Name,
						Name:      "mycluster",
					},
					Spec: hivev1.ClusterDeploymentSpec{
						ClusterMetadata: &hivev1.ClusterMetadata{
							AdminKubeconfigSecretRef: corev1.LocalObjectReference{
								Name: kubeconfigSecret.Name,
							},
						},
					},
				}
				err = hubClient.Create(ctx, clusterDeployment)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Replaces API server address for hosted clusters", func() {
				// Add the 'agentClusterRef' label to the cluster deployment, as that is what marks it
				// as a hosted cluster.
				clusterDeployment := &hivev1.ClusterDeployment{}
				clusterDeploymentKey := types.NamespacedName{
					Namespace: hubNamespace.Name,
					Name:      "mycluster",
				}
				err := hubClient.Get(ctx, clusterDeploymentKey, clusterDeployment)
				Expect(err).ToNot(HaveOccurred())
				clusterDeploymentUpdate := clusterDeployment.DeepCopy()
				if clusterDeploymentUpdate.Labels == nil {
					clusterDeploymentUpdate.Labels = map[string]string{}
				}
				clusterDeploymentUpdate.Labels["agentClusterRef"] = "mycluster"
				err = hubClient.Patch(
					ctx,
					clusterDeploymentUpdate,
					ctrlclient.MergeFrom(clusterDeployment),
				)
				Expect(err).ToNot(HaveOccurred())

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
					hubClient,
					[]func(http.RoundTripper) http.RoundTripper{
						func(http.RoundTripper) http.RoundTripper {
							return transport
						},
					})
				Expect(err).ToNot(HaveOccurred())

				// Get the client:
				client, err := factory.CreateFromSecret(ctx, kubeconfigSecret)
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
				// Remove the 'agentClusterRef' label from the cluster deployment, as that is what marks
				// it as a hosted cluste.:
				clusterDeployment := &hivev1.ClusterDeployment{}
				clusterDeploymentKey := types.NamespacedName{
					Namespace: hubNamespace.Name,
					Name:      "mycluster",
				}
				err := hubClient.Get(ctx, clusterDeploymentKey, clusterDeployment)
				Expect(err).ToNot(HaveOccurred())
				clusterDeploymentUpdate := clusterDeployment.DeepCopy()
				delete(clusterDeploymentUpdate.Labels, "agentClusterRef")
				err = hubClient.Patch(
					ctx,
					clusterDeploymentUpdate,
					ctrlclient.MergeFrom(clusterDeployment),
				)
				Expect(err).ToNot(HaveOccurred())

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
					hubClient,
					[]func(http.RoundTripper) http.RoundTripper{
						func(http.RoundTripper) http.RoundTripper {
							return transport
						},
					})
				Expect(err).ToNot(HaveOccurred())

				// Get the client:
				client, err := factory.CreateFromSecret(ctx, kubeconfigSecret)
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

			It("Doesn't replace API server address if the cluster deployment doesn't exist", func() {
				// Delete the cluster deployment:
				clusterDeployment := &hivev1.ClusterDeployment{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: hubNamespace.Name,
						Name:      "mycluster",
					},
				}
				err := hubClient.Delete(ctx, clusterDeployment)
				Expect(err).ToNot(HaveOccurred())

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
					logger, hubClient,
					[]func(http.RoundTripper) http.RoundTripper{
						func(http.RoundTripper) http.RoundTripper {
							return transport
						},
					})
				Expect(err).ToNot(HaveOccurred())

				// Get the client:
				client, err := factory.CreateFromSecret(ctx, kubeconfigSecret)
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
		})
	})
})
