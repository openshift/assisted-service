package cbohelper

import (
	"context"
	"fmt"
	"strings"

	"github.com/coreos/go-semver/semver"
	ignition_config_types_32 "github.com/coreos/ignition/v2/config/v3_2/types"
	v1 "github.com/openshift/api/config/v1"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	iccignition "github.com/openshift/image-customization-controller/pkg/ignition"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=cbohelper.go -package=cbohelper -destination=mock_cbouhelper.go
type CBOHelperApi interface {
	GenerateIronicConfig() (ignition_config_types_32.Config, error)
	ConvergedFlowAvailable() bool
}

type Config struct {
	BaremetalIronicAgentImage      string `envconfig:"IRONIC_AGENT_IMAGE" default:"registry.ci.openshift.org/openshift:ironic-agent:latest"`
	MinimalVersionForConvergedFlow string `envconfig:"MINIMAL_VERSION_FOR_COVERGED_FLOW" default:"4.11.0"`
}

type CBOHelper struct {
	client.Client
	log                    logrus.FieldLogger
	kubeAPIEnabled         bool
	config                 Config
	convergedFlowAvailable bool
}

func New(c client.Client, log logrus.FieldLogger, kubeAPIEnabled bool, config Config) CBOHelperApi {
	ch := CBOHelper{Client: c, log: log, kubeAPIEnabled: kubeAPIEnabled, config: config}
	ch.setConvergedFlowAvailability()
	return &ch
}

func (ch *CBOHelper) getIronicServiceURL() (string, error) {
	provisioningInfo, err := ch.readProvisioningCR()
	if err != nil {
		ch.log.WithError(err).Error("unable to get provisioning CR")
		return "", err
	}
	ironicIP, err := ch.getIronicIP(provisioningInfo)
	if err != nil || ironicIP == "" {
		ch.log.WithError(err).Error("unable to determine Ironic's IP")
		return "", err
	}
	return getUrlFromIP(ironicIP), nil
}

func (ch *CBOHelper) GenerateIronicConfig() (config ignition_config_types_32.Config, err error) {
	ironicBaseURL, err := ch.getIronicServiceURL()
	if err != nil {
		return config, err
	}
	config.Ignition.Version = "3.2.0"
	// TODO: this should probably get the proxy settings as well
	ib, err := iccignition.New([]byte{}, []byte{}, ironicBaseURL, ch.config.BaremetalIronicAgentImage, "", "", "", "", "", "", "")
	if err != nil {
		return config, err
	}
	config.Storage.Files = []ignition_config_types_32.File{ib.IronicAgentConf()}
	// TODO: sort out the flags (authfile...) and copy network
	config.Systemd.Units = []ignition_config_types_32.Unit{ib.IronicAgentService(false)}
	return config, err
}

func (ch *CBOHelper) readProvisioningCR() (*metal3iov1alpha1.Provisioning, error) {
	// Fetch the Provisioning instance
	instance := &metal3iov1alpha1.Provisioning{}
	namespacedName := types.NamespacedName{Name: metal3iov1alpha1.ProvisioningSingletonName, Namespace: ""}
	if err := ch.Client.Get(context.TODO(), namespacedName, instance); err != nil {
		return nil, errors.Wrap(err, "unable to read Provisioning CR")
	}
	return instance, nil
}
func (ch *CBOHelper) getIronicIP(info *metal3iov1alpha1.Provisioning) (string, error) {
	config := info.Spec
	if config.ProvisioningNetwork != metal3iov1alpha1.ProvisioningNetworkDisabled && !config.VirtualMediaViaExternalNetwork {
		return config.ProvisioningIP, nil
	}
	return ch.getPodHostIP(info.Namespace)
}

func getUrlFromIP(ipAddr string) string {
	if strings.Contains(ipAddr, ":") {
		// This is an IPv6 addr
		return "https://" + fmt.Sprintf("[%s]", ipAddr)
	}
	if ipAddr != "" {
		// This is an IPv4 addr
		return "https://" + ipAddr
	} else {
		return ""
	}
}

func (ch *CBOHelper) getPodHostIP(targetNamespace string) (string, error) {
	metal3AppName := "metal3"
	stateService := "metal3-state"
	cboLabelName := "baremetal.openshift.io/cluster-baremetal-operator"

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"k8s-app":    metal3AppName,
			cboLabelName: stateService,
		}}

	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return "", err
	}

	listOptions := client.ListOptions{LabelSelector: selector,
		Namespace: targetNamespace,
	}
	podList := &corev1.PodList{}
	err = ch.Client.List(context.Background(), podList, &listOptions)
	if err != nil {
		return "", err
	}
	var hostIP string
	switch len(podList.Items) {
	case 0:
		err = fmt.Errorf("ironic IP not available yet")
	case 1:
		hostIP = podList.Items[0].Status.HostIP
	default:
		// We expect only one pod with the above LabelSelector
		err = fmt.Errorf("there should be only one pod listed for the given label")
	}
	return hostIP, err
}

func (ch *CBOHelper) ConvergedFlowAvailable() bool {
	return ch.convergedFlowAvailable
}

func (ch *CBOHelper) setConvergedFlowAvailability() {
	if !ch.kubeAPIEnabled {
		ch.convergedFlowAvailable = false
		return
	}
	key := types.NamespacedName{
		Name: "baremetal",
	}
	clusterOperator := &v1.ClusterOperator{}
	if err := ch.Client.Get(context.TODO(), key, clusterOperator); err != nil {
		ch.log.Errorf("Error querying api for baremetal operator status: %s", err)
		ch.convergedFlowAvailable = false
		return
	}
	if len(clusterOperator.Status.Versions) == 0 {
		ch.log.Infof("no version found for baremetal operator")
		ch.convergedFlowAvailable = false
		return
	}
	version := clusterOperator.Status.Versions[0].Version
	ch.log.Infof("The baremetal operator version is %s, the minimal version for the converged flow is %s", version, ch.config.MinimalVersionForConvergedFlow)

	available := semver.New(version).Compare(*semver.New(ch.config.MinimalVersionForConvergedFlow)) >= 0
	if available {
		ch.log.Infof("Converged flow enabled")
	}
	ch.convergedFlowAvailable = available
}
