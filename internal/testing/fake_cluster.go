package testing

import (
	"context"

	"github.com/bombsimon/logrusr/v3"
	"github.com/go-logr/logr"
	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	routev1 "github.com/openshift/api/route/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	wtch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	apiregv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	clnt "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// FakeClusterBuilder contains the data and logic needed to create a fake OpenShift cluster that can
// be used to test our controllers.
type FakeClusterBuilder struct {
	logger logr.Logger
}

// FakeCluster is a fake OpenShift cluster.
type FakeCluster struct {
	logger   logr.Logger
	scheme   *runtime.Scheme
	client   clnt.WithWatch
	watches  []wtch.Interface
	recorder *record.FakeRecorder
}

// NewFakeCluster creates a builder that can then be used to configure and create a fake cluster.
func NewFakeCluster() *FakeClusterBuilder {
	return &FakeClusterBuilder{}
}

// Logger sets the logger that the fake cluster will use to write messages. The default is a logger
// that writes to the Ginkgo writer.
func (b *FakeClusterBuilder) Logger(value logr.Logger) *FakeClusterBuilder {
	b.logger = value
	return b
}

// Build uses the data stored in the builder to create a new fake cluster. Remember to call the Stop
// method once the fake cluster is no longer needed.
func (b *FakeClusterBuilder) Build() *FakeCluster {
	// Prepare the logger:
	if b.logger.GetSink() == nil {
		tmp := logrus.New()
		tmp.Out = GinkgoWriter
		b.logger = logrusr.New(tmp)
	}

	// Prepare the scheme:
	adders := []func(*runtime.Scheme) error{
		aiv1beta1.AddToScheme,
		apiextensionsv1.AddToScheme,
		apiregv1.AddToScheme,
		bmh_v1alpha1.AddToScheme,
		configv1.Install,
		corev1.AddToScheme,
		hiveext.AddToScheme,
		hivev1.AddToScheme,
		machinev1beta1.AddToScheme,
		metal3iov1alpha1.AddToScheme,
		monitoringv1.AddToScheme,
		routev1.Install,
		scheme.AddToScheme,
	}
	scheme := runtime.NewScheme()
	for _, adder := range adders {
		err := adder(scheme)
		Expect(err).ToNot(HaveOccurred())
	}

	// Create the client:
	client := fake.NewClientBuilder().WithScheme(scheme).Build()

	// Create the objects that are needed by all the tests:
	b.createIngressConfig(client)

	// Create the object:
	c := &FakeCluster{
		logger: b.logger,
		scheme: scheme,
		client: client,
	}

	// Create the watches that simulate controllers:
	c.watches = append(c.watches, c.watchRoutes())
	c.watches = append(c.watches, c.watchStatefulSets())

	// Start the goroutine that drains events:
	c.recorder = record.NewFakeRecorder(0)
	go c.drainEvents()

	return c
}

// createIngressConfig creates the configmap of the ingress controller. This is needed because some
// of our reconcilers check that it exists. Note that it needs just to exist, the content is not
// relevant.
func (b *FakeClusterBuilder) createIngressConfig(client clnt.Client) {
	err := client.Create(context.Background(), &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "openshift-config-managed",
			Name:      "default-ingress-cert",
		},
	})
	Expect(err).ToNot(HaveOccurred())
}

// watchRoutes creates the watch that simulates the OpenShift route controller, so that routes will
// have host names assigned.
func (c *FakeCluster) watchRoutes() wtch.Interface {
	watch, err := c.client.Watch(context.Background(), &routev1.RouteList{})
	Expect(err).ToNot(HaveOccurred())
	go func() {
		defer GinkgoRecover()
		for event := range watch.ResultChan() {
			switch event.Type {
			case wtch.Added:
				object, ok := event.Object.(*routev1.Route)
				Expect(ok).To(BeTrue())
				if object.Spec.Host == "" {
					object.Spec.Host = object.Name
					err = c.client.Update(context.Background(), object)
					Expect(err).ToNot(HaveOccurred())
					c.logger.Info(
						"Updated route",
						"namesapce", object.Namespace,
						"name", object.Name,
						"host", object.Spec.Host,
					)
				}
			}
		}
	}()
	return watch
}

// watchStatefulSets creates the watch that simulates the stateful set controller, so that stateful
// sets will have the actual number of replicas equal to the specified number of replicas.
func (c *FakeCluster) watchStatefulSets() wtch.Interface {
	watch, err := c.client.Watch(context.Background(), &appsv1.StatefulSetList{})
	Expect(err).ToNot(HaveOccurred())
	go func() {
		defer GinkgoRecover()
		for event := range watch.ResultChan() {
			switch event.Type {
			case wtch.Added:
				object, ok := event.Object.(*appsv1.StatefulSet)
				Expect(ok).To(BeTrue())
				replicas := *object.Spec.Replicas
				object.Status.Replicas = replicas
				object.Status.ReadyReplicas = replicas
				object.Status.CurrentReplicas = replicas
				object.Status.UpdatedReplicas = replicas
				err = c.client.Update(context.Background(), object)
				Expect(err).ToNot(HaveOccurred())
				c.logger.Info(
					"Updated stateful set",
					"namespace", object.Namespace,
					"name", object.Name,
					"replicas", replicas,
				)
			}
		}
	}()
	return watch
}

// drainEvents drains the events channel, as otherwise the tests will hang when the capacity of the
// events channel is exahusted.
func (c *FakeCluster) drainEvents() {
	for event := range c.recorder.Events {
		c.logger.Info(
			"Drained event",
			"event", event,
		)
	}
}

// Scheme returns the scheme of the fake cluster.
func (c *FakeCluster) Scheme() *runtime.Scheme {
	return c.scheme
}

// Client returns a controller runtime client that can be used to interact with the fake cluster.
func (c *FakeCluster) Client() clnt.Client {
	return c.client
}

// Recorder returns the event recorder of the fake cluster.
func (c *FakeCluster) Recorder() record.EventRecorder {
	return c.recorder
}

// Stop stops the fake cluster and releases all the resources that it was using.
func (c *FakeCluster) Stop() {
	// Stop the watches:
	for _, watch := range c.watches {
		watch.Stop()
	}

	// Stop event draining:
	close(c.recorder.Events)
}
