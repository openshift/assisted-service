package job

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/openshift/assisted-service/internal/network"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/generator"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	batch "k8s.io/api/batch/v1"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ignitionGeneratorPrefix = "ignition-generator"

// Dummy is used to represent the ignition config for the dummy ISO that is kicked off in
// inventory.go to pull the base ISO image when the service starts up.
// It is also used to detect if the image should be uploaded to S3. The dummy image is not
// uploaded to S3.
const Dummy = "Dummy"

//go:generate mockgen -source=job.go -package=job -destination=mock_job.go
type API interface {
	// Create k8s job
	Create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error
	// Monitor k8s job return error in case job fails
	Monitor(ctx context.Context, name, namespace string) error
	// Delete k8s job
	Delete(ctx context.Context, name, namespace string) error
	generator.ISOInstallConfigGenerator
}

type Config struct {
	MonitorLoopInterval time.Duration `envconfig:"JOB_MONITOR_INTERVAL" default:"500ms"`
	RetryInterval       time.Duration `envconfig:"JOB_RETRY_INTERVAL" default:"1s"`
	RetryAttempts       int           `envconfig:"JOB_RETRY_ATTEMPTS" default:"30"`
	ImageBuilder        string        `envconfig:"IMAGE_BUILDER" default:"quay.io/ocpmetal/assisted-iso-create:latest"`
	Namespace           string        `envconfig:"NAMESPACE" default:"assisted-installer"`
	S3EndpointURL       string        `envconfig:"S3_ENDPOINT_URL" default:"http://10.35.59.36:30925"`
	S3Bucket            string        `envconfig:"S3_BUCKET" default:"test"`
	S3Region            string        `envconfig:"S3_REGION"`
	AwsAccessKeyID      string        `envconfig:"AWS_ACCESS_KEY_ID" default:"accessKey1"`
	AwsSecretAccessKey  string        `envconfig:"AWS_SECRET_ACCESS_KEY" default:"verySecretKey1"`
	JobCPULimit         string        `envconfig:"JOB_CPU_LIMIT" default:"500m"`
	JobMemoryLimit      string        `envconfig:"JOB_MEMORY_LIMIT" default:"1000Mi"`
	JobCPURequests      string        `envconfig:"JOB_CPU_REQUESTS" default:"300m"`
	JobMemoryRequests   string        `envconfig:"JOB_MEMORY_REQUESTS" default:"400Mi"`
	IgnitionGenerator   string        `envconfig:"IGNITION_GENERATE_IMAGE" default:"quay.io/ocpmetal/assisted-ignition-generator:latest"` // TODO: update the latest once the repository has git workflow
	ServiceBaseURL      string        `envconfig:"SERVICE_BASE_URL"`
	//[TODO] -  change the default of Releae image to "", once everyine wll update their environment
	SubsystemRun         bool   `envconfig:"SUBSYSTEM_RUN"`
	ReleaseImage         string `envconfig:"OPENSHIFT_INSTALL_RELEASE_IMAGE" default:"quay.io/openshift-release-dev/ocp-release@sha256:eab93b4591699a5a4ff50ad3517892653f04fb840127895bb3609b3cc68f98f3"`
	SkipCertVerification bool   `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
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

	dp := meta.DeletePropagationForeground
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

func getQuantity(s string) resource.Quantity {
	reply, _ := resource.ParseQuantity(s)
	return reply
}

// create discovery image generation job, return job name and error
func (k *kubeJob) createImageJob(jobName, imgName, ignitionConfig string, performUpload bool) *batch.Job {
	var command []string
	if !performUpload {
		command = []string{"echo", "pass"}
	}
	return &batch.Job{
		TypeMeta: meta.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: meta.ObjectMeta{
			Name:      jobName,
			Namespace: k.Config.Namespace,
		},
		Spec: batch.JobSpec{
			BackoffLimit: swag.Int32(2),
			Template: core.PodTemplateSpec{
				ObjectMeta: meta.ObjectMeta{
					Name:      jobName,
					Namespace: k.Config.Namespace,
				},
				Spec: core.PodSpec{
					Containers: []core.Container{
						{
							Resources: core.ResourceRequirements{
								Limits: core.ResourceList{
									"cpu":    getQuantity(k.Config.JobCPULimit),
									"memory": getQuantity(k.Config.JobMemoryLimit),
								},
								Requests: core.ResourceList{
									"cpu":    getQuantity(k.Config.JobCPURequests),
									"memory": getQuantity(k.Config.JobMemoryRequests),
								},
							},
							Command:         command,
							Name:            "image-creator",
							Image:           k.Config.ImageBuilder,
							ImagePullPolicy: "Always",
							Env: []core.EnvVar{
								{
									Name:  "S3_ENDPOINT_URL",
									Value: k.Config.S3EndpointURL,
								},
								{
									Name:  "IGNITION_CONFIG",
									Value: ignitionConfig,
								},
								{
									Name:  "IMAGE_NAME",
									Value: imgName,
								},
								{
									Name:  "S3_BUCKET",
									Value: k.Config.S3Bucket,
								},
								{
									Name:  "S3_REGION",
									Value: k.Config.S3Region,
								},
								{
									Name:  "AWS_ACCESS_KEY_ID",
									Value: k.Config.AwsAccessKeyID,
								},
								{
									Name:  "AWS_SECRET_ACCESS_KEY",
									Value: k.Config.AwsSecretAccessKey,
								},
							},
						},
					},
					RestartPolicy: "Never",
				},
			},
		},
	}
}

// creates iso
func (k *kubeJob) GenerateISO(ctx context.Context, cluster common.Cluster, jobName string, imageName string, ignitionConfig string, eventsHandler events.Handler) error {
	log := logutil.FromContext(ctx, k.log)
	if cluster.ID != nil {
		previousCreatedAt := time.Time(cluster.ImageInfo.CreatedAt)
		// Kill the previous job in case it's still running
		prevJobName := fmt.Sprintf("createimage-%s-%s", cluster.ID, previousCreatedAt.Format("20060102150405"))
		log.Info("Attempting to delete job %s", prevJobName)
		if err := k.Delete(ctx, prevJobName, k.Namespace); err != nil {
			log.WithError(err).Errorf("failed to kill previous job in cluster %s", cluster.ID)
			msg := "Failed to generate image: error stopping previous image generation"
			eventsHandler.AddEvent(ctx, *cluster.ID, nil, models.EventSeverityError, msg, time.Now())
			return err
		}
		log.Info("Finished attempting to delete job %s", prevJobName)
	}

	// This job name is exactly 63 characters which is the maximum for a job - be careful if modifying
	log.Infof("Creating job %s", jobName)
	performUpload := true
	if ignitionConfig == Dummy {
		performUpload = false
	}
	if err := k.Create(ctx, k.createImageJob(jobName, imageName, ignitionConfig, performUpload)); err != nil {
		log.WithError(err).Error("failed to create image job")
		msg := "Failed to generate image: error creating image generation job"
		eventsHandler.AddEvent(ctx, *cluster.ID, nil, models.EventSeverityError, msg, time.Now())
		return err
	}

	if err := k.Monitor(ctx, jobName, k.Namespace); err != nil {
		log.WithError(err).Error("image creation failed")
		msg := "Failed to generate image: error during image generation job"
		eventsHandler.AddEvent(ctx, *cluster.ID, nil, models.EventSeverityError, msg, time.Now())
		return err
	}
	return nil
}

func (k *kubeJob) createKubeconfigJob(cluster *common.Cluster, jobName string, cfg []byte, encodedDhcpFileContents string) *batch.Job {
	id := cluster.ID
	ignitionGeneratorImage := k.Config.IgnitionGenerator
	var pullPolicy core.PullPolicy = "Always"
	if k.Config.SubsystemRun {
		pullPolicy = "Never"
	}
	ret := &batch.Job{
		TypeMeta: meta.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: meta.ObjectMeta{
			Name:      jobName,
			Namespace: k.Config.Namespace,
		},
		Spec: batch.JobSpec{
			BackoffLimit: swag.Int32(2),
			Template: core.PodTemplateSpec{
				ObjectMeta: meta.ObjectMeta{
					Name:      jobName,
					Namespace: k.Config.Namespace,
				},
				Spec: core.PodSpec{
					Containers: []core.Container{
						{
							Name:            ignitionGeneratorPrefix,
							Image:           ignitionGeneratorImage,
							ImagePullPolicy: pullPolicy,
							Env: []core.EnvVar{
								{
									Name:  "S3_ENDPOINT_URL",
									Value: k.Config.S3EndpointURL,
								},
								{
									Name:  "INSTALLER_CONFIG",
									Value: string(cfg),
								},
								{
									Name:  "INVENTORY_ENDPOINT",
									Value: strings.TrimSpace(k.Config.ServiceBaseURL) + "/api/assisted-install/v1",
								},
								{
									Name:  "IMAGE_NAME",
									Value: jobName,
								},
								{
									Name:  "S3_BUCKET",
									Value: k.Config.S3Bucket,
								},
								{
									Name:  "S3_REGION",
									Value: k.Config.S3Region,
								},
								{
									Name:  "CLUSTER_ID",
									Value: id.String(),
								},
								{
									Name:  "OPENSHIFT_INSTALL_RELEASE_IMAGE_OVERRIDE",
									Value: k.ReleaseImage, //TODO: change this to match the cluster openshift version
								},
								{
									Name:  "AWS_ACCESS_KEY_ID",
									Value: k.Config.AwsAccessKeyID,
								},
								{
									Name:  "AWS_SECRET_ACCESS_KEY",
									Value: k.Config.AwsSecretAccessKey,
								},
								{
									Name:  "SKIP_CERT_VERIFICATION",
									Value: strconv.FormatBool(k.Config.SkipCertVerification),
								},
							},
							Resources: core.ResourceRequirements{
								Limits: core.ResourceList{
									"cpu":    getQuantity(k.Config.JobCPULimit),
									"memory": getQuantity(k.Config.JobMemoryLimit),
								},
								Requests: core.ResourceList{
									"cpu":    getQuantity(k.Config.JobCPURequests),
									"memory": getQuantity(k.Config.JobMemoryRequests),
								},
							},
						},
					},
					RestartPolicy: "Never",
				},
			},
		},
	}
	if encodedDhcpFileContents != "" {
		ret.Spec.Template.Spec.Containers[0].Env = append(ret.Spec.Template.Spec.Containers[0].Env,
			core.EnvVar{
				Name:  "DHCP_ALLOCATION_FILE",
				Value: encodedDhcpFileContents,
			})
	}
	return ret
}

// creates install config
func (k *kubeJob) GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte) error {
	log := logutil.FromContext(ctx, k.log)
	ctime := time.Time(cluster.CreatedAt)
	cTimestamp := strconv.FormatInt(ctime.Unix(), 10)
	jobName := fmt.Sprintf("%s-%s-%s", ignitionGeneratorPrefix, cluster.ID.String(), cTimestamp)[:63]
	encodedDhcpFileContents, err := network.GetEncodedDhcpParamFileContents(&cluster)
	if err != nil {
		wrapped := errors.Wrapf(err, "Could not create DHCP encoded file")
		log.WithError(wrapped).Errorf("GenerateInstallConfig")
		return wrapped
	}
	if err := k.Create(ctx, k.createKubeconfigJob(&cluster, jobName, cfg, encodedDhcpFileContents)); err != nil {
		log.WithError(err).Errorf("Failed to create kubeconfig generation job %s for cluster %s", jobName, cluster.ID)
		return errors.Wrapf(err, "Failed to create kubeconfig generation job %s for cluster %s", jobName, cluster.ID)
	}

	if err := k.Monitor(ctx, jobName, k.Config.Namespace); err != nil {
		log.WithError(err).Errorf("Generating kubeconfig files %s failed for cluster %s", jobName, cluster.ID)
		return errors.Wrapf(err, "Generating kubeconfig files %s failed for cluster %s", jobName, cluster.ID)
	}
	return nil
}

// abort installation files generation job
func (k *kubeJob) AbortInstallConfig(ctx context.Context, cluster common.Cluster) error {
	log := logutil.FromContext(ctx, k.log)

	ctime := time.Time(cluster.CreatedAt)
	cTimestamp := strconv.FormatInt(ctime.Unix(), 10)
	jobName := fmt.Sprintf("%s-%s-%s", ignitionGeneratorPrefix, cluster.ID.String(), cTimestamp)[:63]
	if err := k.Delete(ctx, jobName, k.Namespace); err != nil {
		log.WithError(err).Errorf("Failed to abort kubeconfig generation job %s for cluster %s", jobName, cluster.ID)
		return errors.Wrapf(err, "Failed to abort kubeconfig generation job %s for cluster %s", jobName, cluster.ID)
	}
	return nil
}
