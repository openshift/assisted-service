package common

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/gomega"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

/*
	  Database connection handling funcs. This supports running Postgres in:
		* k8s cluster
		* if k8s connection is not available try to run it as docker container

		When SKIP_UT_DB env var is set localhost:5432 is being used.
*/
const (
	dbDockerName  = "ut-postgres"
	dbDefaultPort = "5432"

	k8sNamespace = "assisted-installer"
)

// / DBContext is an interface for various DB implementations
type DBContext interface {
	GetHostPort() (string, string)
	Create() error
	Teardown()
}

// K8SDBContext runs postgresql as a pod in k8s cluster
type K8SDBContext struct {
	client *k8s.Clientset
}

// NoDBContext
type NoDBContext struct{}

var gDbCtx DBContext

func (c *NoDBContext) Create() error {
	return nil
}

func (c *NoDBContext) Teardown() {}

func (c *NoDBContext) GetHostPort() (string, string) {
	return "127.0.0.1", dbDefaultPort
}

func getTestContainersClient() *TestContainersDBContext {
	return &TestContainersDBContext{}
}

func getK8sClient() (*K8SDBContext, error) {
	var err error
	var k8sConfig *rest.Config
	kubeConfigPath := os.Getenv("KUBECONFIG")
	if kubeConfigPath == "" {
		kubeConfigPath = filepath.Join(homedir.HomeDir(), ".kube", "config")
	}
	k8sConfig, err = clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		return nil, err
	}
	k8sClient, err := k8s.NewForConfig(k8sConfig)
	if err != nil {
		return nil, err
	}
	return &K8SDBContext{k8sClient}, nil
}

func (c *K8SDBContext) Create() error {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: k8sNamespace,
		},
	}
	// Run teardown if namespace already exists
	_, err := c.client.CoreV1().Namespaces().Get(context.TODO(), k8sNamespace, metav1.GetOptions{})
	if err == nil {
		c.Teardown()
	}

	_, err = c.client.CoreV1().Namespaces().Create(context.TODO(), namespace, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: dbDockerName,
			Labels: map[string]string{
				"app": dbDockerName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": dbDockerName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": dbDockerName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "psql",
							Image: "quay.io/sclorg/postgresql-12-c8s",
							Ports: []corev1.ContainerPort{
								{
									Name:          "tcp-5432",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: 5432,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "POSTGRESQL_ADMIN_PASSWORD",
									Value: "admin",
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt(5432),
									},
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/var/lib/pgsql/data",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{
									Medium: corev1.StorageMediumMemory,
								},
							},
						},
					},
				},
			},
		},
	}
	_, err = c.client.AppsV1().Deployments(k8sNamespace).Create(context.TODO(), deployment, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	// Wait for deployment to rollout
	err = wait.PollUntilContextTimeout(context.TODO(), time.Second*5, time.Minute*5, true, func(ctx context.Context) (bool, error) {
		var deploymentErr error
		deployment, deploymentErr := c.client.AppsV1().Deployments(k8sNamespace).Get(ctx, dbDockerName, metav1.GetOptions{})
		if deploymentErr != nil {
			return false, deploymentErr
		}
		return deployment.Status.ReadyReplicas > 0, nil
	})
	if err != nil {
		return err
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: dbDockerName,
			Labels: map[string]string{
				"app": dbDockerName,
			},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     5432,
					Protocol: corev1.ProtocolTCP,
					Name:     "tcp-5432",
				},
			},
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				"app": dbDockerName,
			},
		},
	}
	_, err = c.client.CoreV1().Services(k8sNamespace).Create(context.TODO(), service, metav1.CreateOptions{})
	return err
}

func (c *K8SDBContext) Teardown() {
	err := c.client.CoreV1().Namespaces().Delete(context.TODO(), k8sNamespace, metav1.DeleteOptions{})
	Expect(err).ShouldNot(HaveOccurred())

	// Wait for it to dissappear
	err = wait.PollUntilContextTimeout(context.TODO(), time.Second*5, time.Minute*5, true, func(ctx context.Context) (bool, error) {
		var namespaceErr error
		_, namespaceErr = c.client.CoreV1().Namespaces().Get(ctx, k8sNamespace, metav1.GetOptions{})
		if errors.IsNotFound(namespaceErr) {
			return false, namespaceErr
		}
		return true, nil
	})
	Expect(err).ShouldNot(HaveOccurred())
}

func (c *K8SDBContext) GetHostPort() (string, string) {
	var host string
	var svc *corev1.Service
	err := wait.PollUntilContextTimeout(context.TODO(), time.Second*5, time.Minute*5, true, func(ctx context.Context) (bool, error) {
		var err error
		svc, err = c.client.CoreV1().Services(k8sNamespace).Get(ctx, dbDockerName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return len(svc.Status.LoadBalancer.Ingress) > 0, nil
	})
	Expect(err).ShouldNot(HaveOccurred())
	for _, ip := range svc.Status.LoadBalancer.Ingress {
		host = ip.IP
		break
	}
	if host == "" {
		for _, ip := range svc.Spec.ExternalIPs {
			host = ip
			break
		}
	}
	return host, dbDefaultPort
}

func getDBContext() DBContext {
	if gDbCtx != nil {
		return gDbCtx
	}

	if os.Getenv("SKIP_UT_DB") != "" {
		return &NoDBContext{}
	}

	k8sContext, err := getK8sClient()
	if err == nil {
		err = k8sContext.Create()
		if err == nil {
			gDbCtx = k8sContext
			return k8sContext
		}
	}

	testContainersContext := getTestContainersClient()
	err = testContainersContext.Create()
	Expect(err).ShouldNot(HaveOccurred())
	gDbCtx = testContainersContext
	return testContainersContext
}

func InitializeDBTest() {
	var dbTemp *gorm.DB
	var err error
	dbTemp, err = openTopTestDBConn()
	Expect(err).To(BeNil())
	CloseDB(dbTemp)
}

func TerminateDBTest() {
	getDBContext().Teardown()
}

// Creates a valid postgresql db name from a random uuid
// DB names (and all identifiers) must begin with a letter or '_'
// Additionally using underscores rather than hyphens reduces the chance of quoting bugs
func randomDBName() string {
	return fmt.Sprintf("_%s", strings.ReplaceAll(uuid.New().String(), "-", "_"))
}

func PrepareTestDB(extrasSchemas ...interface{}) (*gorm.DB, string) {
	dbName := randomDBName()
	dbTemp, err := openTopTestDBConn()
	Expect(err).ShouldNot(HaveOccurred())
	defer CloseDB(dbTemp)

	dbTemp = dbTemp.Exec(fmt.Sprintf("CREATE DATABASE %s;", dbName))
	Expect(dbTemp.Error).ShouldNot(HaveOccurred())

	db, err := OpenTestDBConn(dbName)
	Expect(err).ShouldNot(HaveOccurred())
	// db = db.Debug()
	err = AutoMigrate(db)
	Expect(err).ShouldNot(HaveOccurred())

	if len(extrasSchemas) > 0 {
		for _, schema := range extrasSchemas {
			Expect(db.AutoMigrate(schema)).ToNot(HaveOccurred())
		}
	}
	return db, dbName
}

func DeleteTestDB(db *gorm.DB, dbName string) {
	CloseDB(db)

	db, err := openTopTestDBConn()
	Expect(err).ShouldNot(HaveOccurred())
	defer CloseDB(db)
	db = db.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s;", dbName))

	Expect(db.Error).ShouldNot(HaveOccurred())
}

func getDBDSN(dbName string) string {
	host, port := getDBContext().GetHostPort()
	dsn := fmt.Sprintf("host=%s port=%s user=postgres password=admin sslmode=disable", host, port)
	if dbName != "" {
		dsn = dsn + fmt.Sprintf(" database=%s", dbName)
	}
	return dsn
}

func openTestDB(dbName string) (*gorm.DB, error) {
	dsn := getDBDSN(dbName)
	newLogger := logger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags), // io writer
		logger.Config{
			SlowThreshold:             time.Second, // Slow SQL threshold
			IgnoreRecordNotFoundError: true,        // Ignore ErrRecordNotFound error for logger
		},
	)

	open := func() (*gorm.DB, error) {
		return gorm.Open(postgres.Open(dsn), &gorm.Config{
			DisableForeignKeyConstraintWhenMigrating: true,
			Logger:                                   newLogger,
		})
	}

	for attempts := 0; attempts < 30; attempts++ {
		db, err := open()
		if err == nil {
			return db, nil
		}
		time.Sleep(time.Second)
	}
	return open()
}

func openTopTestDBConn() (*gorm.DB, error) {
	return openTestDB("")
}

func OpenTestDBConn(dbName string) (*gorm.DB, error) {
	return openTestDB(dbName)
}
