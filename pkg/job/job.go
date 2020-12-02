package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/ignition"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
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

// UploadBaseISOJobName is a prefix used to form the job name
var UploadBaseISOJobName = s3wrapper.BaseObjectName + "-"

type Config struct {
	MonitorLoopInterval time.Duration `envconfig:"JOB_MONITOR_INTERVAL" default:"500ms"`
	RetryInterval       time.Duration `envconfig:"JOB_RETRY_INTERVAL" default:"1s"`
	RetryAttempts       int           `envconfig:"JOB_RETRY_ATTEMPTS" default:"30"`
	ImageBuilder        string        `envconfig:"IMAGE_BUILDER" default:"quay.io/ocpmetal/assisted-iso-create:latest"`
	Namespace           string        `envconfig:"NAMESPACE" default:"assisted-installer"`
	S3SecretName        string        `envconfig:"S3_SECRET_NAME" default:"assisted-installer-s3"`
	S3EndpointURL       string        `envconfig:"S3_ENDPOINT_URL" default:"http://10.35.59.36:30925"`
	S3Bucket            string        `envconfig:"S3_BUCKET" default:"test"`
	S3Region            string        `envconfig:"S3_REGION"`
	AwsAccessKeyID      string        `envconfig:"AWS_ACCESS_KEY_ID" default:"accessKey1"`
	AwsSecretAccessKey  string        `envconfig:"AWS_SECRET_ACCESS_KEY" default:"verySecretKey1"`
	JobCPULimit         string        `envconfig:"JOB_CPU_LIMIT" default:"500m"`
	JobMemoryLimit      string        `envconfig:"JOB_MEMORY_LIMIT" default:"1000Mi"`
	JobCPURequests      string        `envconfig:"JOB_CPU_REQUESTS" default:"300m"`
	JobMemoryRequests   string        `envconfig:"JOB_MEMORY_REQUESTS" default:"400Mi"`
	ServiceBaseURL      string        `envconfig:"SERVICE_BASE_URL"`
	ServiceCACertPath   string        `envconfig:"SERVICE_CA_CERT_PATH" default:""`
	ServiceIPs          string        `envconfig:"SERVICE_IPS" default:""`
	//[TODO] -  change the default of Releae image to "", once everyine wll update their environment
	SubsystemRun         bool   `envconfig:"SUBSYSTEM_RUN"`
	ReleaseImage         string `envconfig:"OPENSHIFT_INSTALL_RELEASE_IMAGE" default:"quay.io/openshift-release-dev/ocp-release@sha256:eab93b4591699a5a4ff50ad3517892653f04fb840127895bb3609b3cc68f98f3"`
	ReleaseImageMirror   string `envconfig:"OPENSHIFT_INSTALL_RELEASE_IMAGE_MIRROR" default:""`
	SkipCertVerification bool   `envconfig:"SKIP_CERT_VERIFICATION" default:"false"`
	WorkDir              string `envconfig:"WORK_DIR" default:"/data/"`
	DummyIgnition        bool   `envconfig:"DUMMY_IGNITION"`
}

func New(log logrus.FieldLogger, kube client.Client, s3Client s3wrapper.API, cfg Config) *kubeJob {
	return &kubeJob{
		Config:   cfg,
		log:      log,
		kube:     kube,
		s3Client: s3Client,
	}
}

type kubeJob struct {
	Config
	log      logrus.FieldLogger
	kube     client.Client
	s3Client s3wrapper.API
}

func (k *kubeJob) create(ctx context.Context, obj runtime.Object, opts ...client.CreateOption) error {
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
func (k *kubeJob) monitor(ctx context.Context, name, namespace string) error {
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
func (k *kubeJob) delete(ctx context.Context, name, namespace string, force bool) error {
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
		if !force {
			return nil
		}
	}

	dp := meta.DeletePropagationForeground
	gp := int64(0)
	log.Infof("Sending request to delete job <%s>", name)
	if err := k.kube.Delete(ctx, &job, client.PropagationPolicy(dp), client.GracePeriodSeconds(gp)); err != nil {
		log.WithError(err).Errorf("Failed to delete job <%s>", name)
	}

	// delete is async, wait for the job to not be found
	if err := k.monitor(ctx, name, namespace); err != nil {
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
func (k *kubeJob) uploadImageJob(jobName, imageName string) *batch.Job {
	var pullPolicy core.PullPolicy = "Always"
	if k.Config.SubsystemRun {
		pullPolicy = "Never"
	}
	return &batch.Job{
		TypeMeta: meta.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: meta.ObjectMeta{
			GenerateName: jobName,
			Namespace:    k.Config.Namespace,
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
							Name:            "image-creator",
							Image:           k.Config.ImageBuilder,
							ImagePullPolicy: pullPolicy,
							Env: []core.EnvVar{
								{
									Name:  "S3_ENDPOINT_URL",
									Value: k.Config.S3EndpointURL,
								},
								{
									Name:  "IMAGE_NAME",
									Value: imageName,
								},
								{
									Name: "S3_BUCKET",
									ValueFrom: &core.EnvVarSource{
										SecretKeyRef: &core.SecretKeySelector{
											LocalObjectReference: core.LocalObjectReference{
												Name: k.Config.S3SecretName,
											},
											Key: "bucket",
										},
									},
								},
								{
									Name: "S3_REGION",
									ValueFrom: &core.EnvVarSource{
										SecretKeyRef: &core.SecretKeySelector{
											LocalObjectReference: core.LocalObjectReference{
												Name: k.Config.S3SecretName,
											},
											Key: "aws_region",
										},
									},
								},
								{
									Name: "AWS_ACCESS_KEY_ID",
									ValueFrom: &core.EnvVarSource{
										SecretKeyRef: &core.SecretKeySelector{
											LocalObjectReference: core.LocalObjectReference{
												Name: k.Config.S3SecretName,
											},
											Key: "aws_access_key_id",
										},
									},
								},
								{
									Name: "AWS_SECRET_ACCESS_KEY",
									ValueFrom: &core.EnvVarSource{
										SecretKeyRef: &core.SecretKeySelector{
											LocalObjectReference: core.LocalObjectReference{
												Name: k.Config.S3SecretName,
											},
											Key: "aws_secret_access_key",
										},
									},
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

func (k *kubeJob) UploadBaseISO() error {
	ctx := context.Background()
	log := logutil.FromContext(ctx, k.log)

	log.Infof("Creating job %s", UploadBaseISOJobName)
	uploadJob := k.uploadImageJob(UploadBaseISOJobName, s3wrapper.BaseObjectName)
	if err := k.create(ctx, uploadJob); err != nil {
		log.WithError(err).Error("failed to create image job")
		return err
	}

	if err := k.monitor(ctx, uploadJob.Name, k.Namespace); err != nil {
		log.WithError(err).Error("image creation failed")
		return err
	}
	return nil
}

// GenerateInstallConfig creates install config and ignition files
func (k *kubeJob) GenerateInstallConfig(ctx context.Context, cluster common.Cluster, cfg []byte) error {
	log := logutil.FromContext(ctx, k.log)
	workDir := filepath.Join(k.Config.WorkDir, cluster.ID.String())
	installerCacheDir := filepath.Join(k.Config.WorkDir, "installercache")
	err := os.Mkdir(workDir, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}
	defer func() {
		// keep results in case of failure so a human can debug
		if err != nil {
			debugPath := filepath.Join(k.Config.WorkDir, cluster.ID.String()+"-failed")
			// remove any prior failed results
			err2 := os.RemoveAll(debugPath)
			if err2 != nil && !os.IsNotExist(err2) {
				log.WithError(err).Errorf("Could not remove previous directory with failed config results: %s", debugPath)
				return
			}
			err2 = os.Rename(workDir, debugPath)
			if err2 != nil {
				log.WithError(err).Errorf("Could not rename %s to %s", workDir, debugPath)
				return
			}
			return
		}
		err2 := os.RemoveAll(workDir)
		if err2 != nil {
			log.WithError(err).Error("Failed to clean up generated ignition directory")
		}
	}()

	// runs openshift-install to generate ignition files, then modifies them as necessary
	var generator ignition.Generator
	if k.Config.DummyIgnition {
		generator = ignition.NewDummyGenerator(workDir, &cluster, k.s3Client, log)
	} else {
		generator = ignition.NewGenerator(workDir, installerCacheDir, &cluster, k.Config.ReleaseImage, "", k.Config.ServiceCACertPath, k.s3Client, log)
	}
	err = generator.Generate(ctx, cfg)
	if err != nil {
		return err
	}

	// upload files to S3
	err = generator.UploadToS3(ctx)
	if err != nil {
		return err
	}

	return nil
}

// abort installation files generation job
// TODO check if this is needed
func (k *kubeJob) AbortInstallConfig(ctx context.Context, cluster common.Cluster) error {
	log := logutil.FromContext(ctx, k.log)

	ctime := time.Time(cluster.CreatedAt)
	cTimestamp := strconv.FormatInt(ctime.Unix(), 10)
	jobName := fmt.Sprintf("%s-%s-%s", ignitionGeneratorPrefix, cluster.ID.String(), cTimestamp)[:63]
	if err := k.delete(ctx, jobName, k.Namespace, true); err != nil {
		log.WithError(err).Errorf("Failed to abort kubeconfig generation job %s for cluster %s", jobName, cluster.ID)
		return errors.Wrapf(err, "Failed to abort kubeconfig generation job %s for cluster %s", jobName, cluster.ID)
	}
	return nil
}
