package subsystem

import (
	"context"
	"os"
	"os/user"
	"path"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/leader"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	timeout   = 10 * time.Second
	namespace = "default"
)

func buildConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}

	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func waitForPredicate(timeout time.Duration, predicate func() bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		if predicate() {
			break
		}
		if ctx.Err() != nil {
			panic("Timeout has occurred")
		}
	}
}

type Test struct {
	lead   *leader.Elector
	name   string
	ctx    context.Context
	cancel context.CancelFunc
}

func NewTest(lead *leader.Elector, name string) *Test {
	return &Test{lead: lead, name: name}
}

func (t *Test) isLeader() bool {
	return t.lead.IsLeader()
}

func (t *Test) isNotLeader() bool {
	return !t.lead.IsLeader()
}

func (t *Test) start() {
	t.ctx, t.cancel = context.WithCancel(context.Background())
	err := t.lead.StartLeaderElection(t.ctx)
	Expect(err).ShouldNot(HaveOccurred())
}

func (t *Test) stop() {
	t.cancel()
	if t.lead.IsLeader() {
		waitForPredicate(timeout, t.isNotLeader)
	}
}

func getLeader(tests []*Test) *Test {
	for _, test := range tests {
		if test.lead.IsLeader() {
			return test
		}
	}
	return nil
}

func verifySingleLeader(tests []*Test) {
	count := 0
	for _, test := range tests {
		if test.lead.IsLeader() {
			count += 1
		}
	}
	Expect(count == 1).Should(Equal(true))
}

func getKubeconfig() string {
	kcEnv := os.Getenv("KUBECONFIG")
	if kcEnv != "" {
		return kcEnv
	}
	return path.Join(getHomeDir(), ".kube/config")
}

func getHomeDir() string {
	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	return usr.HomeDir
}

var _ = Describe("Leader tests", func() {
	if Options.DeployTarget != "k8s" {
		log.Info("Leader tests are disabled for non-k8s deployments")
		return
	}

	configMapName := "leader-test"

	kubeconfig := getKubeconfig()
	if kubeconfig == "" {
		panic("--kubeconfig must be provided")
	}

	// leader election uses the Kubernetes API by writing to a
	// lock object, which can be a LeaseLock object (preferred),
	// a ConfigMap, or an Endpoints (deprecated) object.
	// Conflicting writes are detected and each client handles those actions
	// independently.
	config, err := buildConfig(kubeconfig)
	if err != nil {
		log.Fatal(err)
	}

	client := clientset.NewForConfigOrDie(config)
	cf := leader.Config{LeaseDuration: 2 * time.Second, RenewDeadline: 1900 * time.Millisecond, RetryInterval: 200 * time.Millisecond,
		Namespace: namespace}

	var tests []*Test

	AfterEach(func() {
		for _, test := range tests {
			test.stop()
		}
	})

	BeforeEach(func() {
		tests = []*Test{}
	})

	It("Leader test", func() {
		leader1 := leader.NewElector(client, cf, configMapName, log)
		leader2 := leader.NewElector(client, cf, configMapName, log)
		leader3 := leader.NewElector(client, cf, configMapName, log)

		test1 := NewTest(leader1, "leader_1")
		test2 := NewTest(leader2, "leader_2")
		test3 := NewTest(leader3, "leader_3")
		tests = []*Test{test1, test2, test3}

		By("Start leaders one by one")

		test1.start()
		waitForPredicate(timeout, test1.isLeader)
		test2.start()
		test3.start()
		// lets wait and verify that leader is not changed
		time.Sleep(5 * time.Second)
		waitForPredicate(timeout, test1.isLeader)
		verifySingleLeader(tests)
		log.Infof("Leader 1 is leader %t", leader1.IsLeader())
		log.Infof("Leader 2 is leader %t", leader2.IsLeader())
		log.Infof("Leader 3 is leader %t", leader3.IsLeader())

		oldLeader := test1
		By("Cancelling current leader and verifying another one took it")
		for i := 0; i < 2; i++ {
			oldLeader.stop()
			waitForPredicate(timeout, oldLeader.isNotLeader)
			log.Infof("Find new leader")
			waitForPredicate(timeout, func() bool {
				return getLeader(tests) != nil
			})
			newLeader := getLeader(tests)
			log.Infof("New leader is %s", newLeader.name)
			Expect(newLeader.name).ShouldNot(Equal(test1.name))
			// lets wait and verify that leader is not changed
			time.Sleep(5 * time.Second)
			waitForPredicate(timeout, newLeader.isLeader)
			verifySingleLeader(tests)
			oldLeader = newLeader
		}

		By("Cancelling current")
		oldLeader.stop()
		waitForPredicate(timeout, oldLeader.isNotLeader)

	})

	It("Bad config map name", func() {
		By("Adding leader with bad configmap name, must fail. Will be the same for any configmap create error")
		badConfigMap := leader.NewElector(client, cf, "badConfigMapName", log)
		err := badConfigMap.StartLeaderElection(context.Background())
		Expect(err).Should(HaveOccurred())
	})

	It("Test 2 leaders in parallel with different config map", func() {
		leader1 := leader.NewElector(client, cf, configMapName, log)
		test1 := NewTest(leader1, "leader_1")
		tests = append(tests, test1)
		test1.start()
		waitForPredicate(timeout, test1.isLeader)
		By("Adding leader with another configmap, must become a leader")
		anotherConfigMap := leader.NewElector(client, cf, "another-config-map", log)
		anotherConfigMapTest := NewTest(anotherConfigMap, "another-config-map")
		tests = append(tests, anotherConfigMapTest)
		anotherConfigMapTest.start()
		waitForPredicate(timeout, anotherConfigMapTest.isLeader)
		log.Infof("Verify that previous leader was not changed")
		waitForPredicate(timeout, test1.isLeader)
	})
	It("Deleting configmap in a loop", func() {
		By("Deleting configmap in a loop (it must be recreated all the time), leader will loose leader and retake it")
		leader1 := leader.NewElector(client, cf, configMapName, log)
		test1 := NewTest(leader1, "leader_1")
		tests = append(tests, test1)
		test1.start()
		wasLost := false
		for i := 0; i < 300; i++ {
			_ = client.CoreV1().ConfigMaps(namespace).Delete(context.TODO(), configMapName, metav1.DeleteOptions{})
			if !test1.isLeader() {
				wasLost = true
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		Expect(wasLost).Should(Equal(true))
		log.Infof("Verifying leader still exists")
		waitForPredicate(timeout, test1.isLeader)
	})
	It("Verify run with leader", func() {
		index := 0
		leader1 := leader.NewElector(client, cf, configMapName, log)
		leader2 := leader.NewElector(client, cf, configMapName, log)
		test1 := NewTest(leader1, "leader_1")
		tests = []*Test{test1}

		By("Start leader1")
		test1.start()
		waitForPredicate(timeout, test1.isLeader)

		By("leader2 run with leader, verify it waiting")

		go func() {
			err := leader2.RunWithLeader(context.Background(), func() error {
				index += 1
				return nil
			})
			Expect(err).NotTo(HaveOccurred())
		}()
		// lets wait and verify that leader is not changed
		time.Sleep(5 * time.Second)
		Expect(index).To(Equal(0))

		By("stopping leader1, verify leader2 runs")
		test1.stop()
		waitForPredicate(timeout, test1.isNotLeader)
		waitForPredicate(timeout, func() bool {
			return index == 1 && !leader2.IsLeader()
		})
	})
})
