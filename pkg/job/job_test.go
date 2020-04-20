package job

import (
	"context"
	"fmt"
	"io/ioutil"
	"testing"

	"github.com/filanov/bm-inventory/pkg/externalmocks"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	batch "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestJob(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Job Test")
}

var _ = Describe("job_test", func() {
	var (
		j    API
		log  = logrus.New()
		kube *externalmocks.MockClient
		ctx  = context.Background()
		ctrl *gomock.Controller
	)

	BeforeEach(func() {
		log.SetOutput(ioutil.Discard)
		ctrl = gomock.NewController(GinkgoT())
		kube = externalmocks.NewMockClient(ctrl)
	})

	mockGetSuccess := func(times int) {
		kube.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(times)
	}

	mockGetJobDone := func(times int) {
		kube.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).
			Do(func(ctx context.Context, key client.ObjectKey, obj runtime.Object) {
				var jparam = obj.(*batch.Job)
				jparam.Status.Succeeded = 1
			}).Times(times)
	}
	mockDeleteSuccess := func() {
		kube.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(nil).Times(1)
	}

	mockGetError := func(times int) {
		kube.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("error")).Times(times)
	}

	Context("simple", func() {
		BeforeEach(func() {
			j = New(log, kube, Config{
				MonitorLoopInterval: 0,
				RetryInterval:       0,
				RetryAttempts:       1,
			})
		})

		It("create_job_failure", func() {
			kube.EXPECT().Create(gomock.Any(), gomock.Any()).Return(fmt.Errorf("err")).Times(1)
			Expect(j.Create(ctx, &batch.Job{})).Should(HaveOccurred())
		})

		It("create_job_success", func() {
			kube.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			Expect(j.Create(ctx, &batch.Job{})).ShouldNot(HaveOccurred())
		})

		It("monitor_with_retry", func() {
			mockGetSuccess(5)
			mockGetJobDone(1)
			mockDeleteSuccess()
			Expect(j.Monitor(ctx, "some-job", "default")).ShouldNot(HaveOccurred())
		})

		It("monitor_failure", func() {
			mockGetError(1)
			Expect(j.Monitor(ctx, "some-job", "default")).Should(HaveOccurred())
		})

		It("monitor_job_failure", func() {
			kube.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).
				Do(func(ctx context.Context, key client.ObjectKey, obj runtime.Object) {
					var jobObj = obj.(*batch.Job)
					jobObj.Spec.BackoffLimit = swag.Int32(1)
					jobObj.Status.Failed = 2
				})
			Expect(j.Monitor(ctx, "some-job", "default")).Should(HaveOccurred())
		})

		It("monitor_delete_failure", func() {
			mockGetSuccess(5)
			kube.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).
				Do(func(ctx context.Context, key client.ObjectKey, obj runtime.Object) {
					var jparam = obj.(*batch.Job)
					jparam.Status.Succeeded = 1
				}).Times(1)
			mockDeleteSuccess()
			Expect(j.Monitor(ctx, "some-job", "default")).ShouldNot(HaveOccurred())
		})
	})

	Context("monitor_retry", func() {
		BeforeEach(func() {
			j = New(log, kube, Config{
				MonitorLoopInterval: 0,
				RetryInterval:       0,
				RetryAttempts:       3,
			})
		})

		It("monitor_retry_only_failure", func() {
			mockGetError(3)
			Expect(j.Monitor(ctx, "some-job", "default")).Should(HaveOccurred())
		})

		It("monitor_retry_fail_after_several_reties", func() {
			mockGetSuccess(5)
			mockGetError(3)
			Expect(j.Monitor(ctx, "some-job", "default")).Should(HaveOccurred())
		})

	})

	AfterEach(func() {
		ctrl.Finish()
	})
})
