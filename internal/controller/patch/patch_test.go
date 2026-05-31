package patch

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return s
}

var _ = Describe("IsEmpty", func() {
	DescribeTable("correctly identifies empty patches",
		func(data string, expected bool) {
			Expect(IsEmpty([]byte(data))).To(Equal(expected))
		},
		Entry("empty JSON object", `{}`, true),
		Entry("metadata with only resourceVersion", `{"metadata":{"resourceVersion":"12345"}}`, true),
		Entry("metadata with empty object", `{"metadata":{}}`, true),
		Entry("metadata with resourceVersion and other fields", `{"metadata":{"resourceVersion":"12345","annotations":{"foo":"bar"}}}`, false),
		Entry("non-metadata field", `{"spec":{"online":true}}`, false),
		Entry("invalid JSON", `not-json`, false),
	)
})

var _ = Describe("IfNeeded", func() {
	var (
		ctx context.Context
		c   client.Client
		s   *runtime.Scheme
		log logrus.FieldLogger
	)

	BeforeEach(func() {
		ctx = context.Background()
		s = newScheme()
		log = common.GetTestLog()
	})

	It("skips empty patch", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Data:       map[string]string{"key": "value"},
		}
		c = fakeclient.NewClientBuilder().WithScheme(s).WithObjects(cm).Build()

		current := &corev1.ConfigMap{}
		Expect(c.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, current)).To(Succeed())

		p := client.MergeFrom(current.DeepCopy())
		Expect(IfNeeded(ctx, c, current, p, log)).To(Succeed())
	})

	It("applies patch with changes", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Data:       map[string]string{"key": "value"},
		}
		c = fakeclient.NewClientBuilder().WithScheme(s).WithObjects(cm).Build()

		current := &corev1.ConfigMap{}
		Expect(c.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, current)).To(Succeed())

		p := client.MergeFrom(current.DeepCopy())
		current.Data["key"] = "updated"

		Expect(IfNeeded(ctx, c, current, p, log)).To(Succeed())

		updated := &corev1.ConfigMap{}
		Expect(c.Get(ctx, types.NamespacedName{Name: "test", Namespace: "default"}, updated)).To(Succeed())
		Expect(updated.Data["key"]).To(Equal("updated"))
	})

	It("ignores NotFound", func() {
		c = fakeclient.NewClientBuilder().WithScheme(s).Build()

		obj := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:            "gone",
				Namespace:       "default",
				ResourceVersion: "1",
			},
		}
		p := client.MergeFrom(obj.DeepCopy())
		obj.Data = map[string]string{"new": "data"}

		Expect(IfNeeded(ctx, c, obj, p, log)).To(Succeed())
	})
})
