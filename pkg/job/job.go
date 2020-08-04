package job

import (
	"context"
	"time"

	"github.com/go-openapi/swag"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	batch "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=job.go -package=job -destination=mock_job.go
type API interface {
	// Create k8s job
	Create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error
	// Monitor k8s job return error in case job fails
	Monitor(ctx context.Context, name, namespace string) error
	// Delete k8s job
	Delete(ctx context.Context, name, namespace string) error
}

type Config struct {
	MonitorLoopInterval time.Duration `envconfig:"JOB_MONITOR_INTERVAL" default:"500ms"`
	RetryInterval       time.Duration `envconfig:"JOB_RETRY_INTERVAL" default:"1s"`
	RetryAttempts       int           `envconfig:"JOB_RETRY_ATTEMPTS" default:"30"`
}

func New(log logrus.FieldLogger, kube client.Client, cfg Config) *kubeJob {
	return &kubeJob{
		Config: cfg,
		log:    log,
		kube:   kube,
	}
}

type kubeJob struct {
	Config
	log  logrus.FieldLogger
	kube client.Client
}

func (k *kubeJob) Create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error {
	return k.kube.Create(ctx, obj, opts...)
}

func (k *kubeJob) getJob(ctx context.Context, job *batch.Job, name, namespace string) error {
	retry := func(f func() error) error {
		var err error
		for i := k.RetryAttempts; i > 0; i-- {
			err = f()
			if err == nil {
				return nil
			} else if apierrors.IsNotFound(err) {
				return err
			}
			time.Sleep(k.RetryInterval)
		}
		return err
	}
	//using retry for get job api because sometimes k8s (minikube) api service is not reachable
	if err := retry(func() error {
		return k.kube.Get(ctx, client.ObjectKey{
			Namespace: namespace,
			Name:      name,
		}, job)
	}); err != nil {
		return err
	}
	return nil
}

// Monitor k8s job
func (k *kubeJob) Monitor(ctx context.Context, name, namespace string) error {
	log := logutil.FromContext(ctx, k.log)
	var job batch.Job

	if err := k.getJob(ctx, &job, name, namespace); err != nil {
		return errors.Wrapf(err, "failed to get job <%s>", name)
	}

	for job.Status.Succeeded == 0 && job.Status.Failed < swag.Int32Value(job.Spec.BackoffLimit)+1 {
		time.Sleep(k.MonitorLoopInterval)
		if err := k.getJob(ctx, &job, name, namespace); err != nil {
			return errors.Wrapf(err, "failed to get job <%s>", name)
		}
	}

	if job.Status.Failed >= swag.Int32Value(job.Spec.BackoffLimit)+1 {
		log.Errorf("Job <%s> failed %d times", name, job.Status.Failed)
		return errors.Errorf("Job <%s> failed <%d> times", name, job.Status.Failed)
	}

	// not deleting a job if it failed
	if err := k.kube.Delete(context.Background(), &job); err != nil {
		log.WithError(err).Errorf("Failed to delete job <%s>", name)
	}

	log.Infof("Job <%s> completed successfully", name)
	return nil
}

// Delete k8s job
func (k *kubeJob) Delete(ctx context.Context, name, namespace string) error {
	log := logutil.FromContext(ctx, k.log)
	var job batch.Job

	if err := k.getJob(ctx, &job, name, namespace); err != nil {
		if apierrors.IsNotFound(err) {
			log.Infof("Job <%s> was not found for deletion, probably already completed", name)
			return nil
		}
		log.WithError(err).Errorf("Failed to get job <%s> for deletion", name)
		return errors.Wrapf(err, "failed to get job <%s>", name)
	}

	// not deleting a job if it failed
	if job.Status.Failed >= swag.Int32Value(job.Spec.BackoffLimit)+1 {
		log.Infof("Job <%s> was found already failed", name)
		return nil
	}

	dp := metav1.DeletePropagationForeground
	gp := int64(0)
	log.Infof("Sending request to delete job <%s>", name)
	if err := k.kube.Delete(ctx, &job, client.PropagationPolicy(dp), client.GracePeriodSeconds(gp)); err != nil {
		log.WithError(err).Errorf("Failed to delete job <%s>", name)
	}

	// delete is async, wait for the job to not be found
	if err := k.Monitor(ctx, name, namespace); err != nil {
		if !apierrors.IsNotFound(err) {
			log.WithError(err).Errorf("Failed to delete job <%s>", name)
		}
	}
	log.Infof("Completed deletion of job <%s>", name)
	return nil
}
