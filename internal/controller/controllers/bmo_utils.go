package controllers

import (
	"context"
	"fmt"

	v1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const MinimalVersionForConvergedFlow = "4.12.0-0.alpha"

type BMOUtils struct {
	// The methods of this receiver get called once before the cache is initialized hence we check the API directly
	c              client.Reader
	log            logrus.FieldLogger
	kubeAPIEnabled bool
}

func NewBMOUtils(client client.Reader, log logrus.FieldLogger, kubeAPIEnabled bool) *BMOUtils {
	return &BMOUtils{client, log, kubeAPIEnabled}
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=clusteroperators,verbs=get;list;watch
// +kubebuilder:rbac:groups=metal3.io,resources=provisionings,verbs=get

// ConvergedFlowAvailable checks the baremetal operator version and returns true if it's equal or higher than the minimal version for converged flow
func (r *BMOUtils) ConvergedFlowAvailable() bool {
	key := types.NamespacedName{
		Name: "baremetal",
	}
	clusterOperator := &v1.ClusterOperator{}
	if err := r.c.Get(context.TODO(), key, clusterOperator); err != nil {
		r.log.Errorf("Error querying api for baremetal operator status: %s", err)
		return false
	}
	if len(clusterOperator.Status.Versions) == 0 {
		r.log.Infof("no version found for baremetal operator")
		return false
	}
	version := clusterOperator.Status.Versions[0].Version
	r.log.Infof("The baremetal operator version is %s, the minimal version for the converged flow is %s", version, MinimalVersionForConvergedFlow)

	available, err := common.VersionGreaterOrEqual(version, MinimalVersionForConvergedFlow)
	if err != nil {
		r.log.WithError(err).Error("Failed to compare CBO version to minimal version for converged flow")
	}
	r.log.Infof("Converged flow enabled: %t", available)
	return available
}

// TODO: replace this with public function in BMO https://github.com/openshift/cluster-baremetal-operator/pull/261
func (r *BMOUtils) GetIronicServiceURL() (string, error) {
	provisioningInfo, err := r.readProvisioningCR()
	if err != nil {
		r.log.WithError(err).Error("unable to get provisioning CR")
		return "", err
	}
	ironicIP, err := r.getIronicIP(*provisioningInfo)
	if err != nil || ironicIP == "" {
		r.log.WithError(err).Error("unable to determine Ironic's IP")
		return "", err
	}
	ironicURL := getUrlFromIP(ironicIP)
	r.log.Infof("Ironic URL is: %s", ironicURL)
	return ironicURL, nil
}

func (r *BMOUtils) readProvisioningCR() (*metal3iov1alpha1.Provisioning, error) {
	// Fetch the Provisioning instance
	instance := &metal3iov1alpha1.Provisioning{}
	namespacedName := types.NamespacedName{Name: metal3iov1alpha1.ProvisioningSingletonName, Namespace: ""}
	if err := r.c.Get(context.TODO(), namespacedName, instance); err != nil {
		return nil, errors.Wrap(err, "unable to read Provisioning CR")
	}
	return instance, nil
}

func (r *BMOUtils) getIronicIP(info metal3iov1alpha1.Provisioning) (string, error) {
	// this is how BMO get the IronicIP
	config := info.Spec
	if config.ProvisioningNetwork != metal3iov1alpha1.ProvisioningNetworkDisabled && !config.VirtualMediaViaExternalNetwork {
		return config.ProvisioningIP, nil
	}
	return r.getPodHostIP(info.Namespace)
}

func getUrlFromIP(ipAddr string) string {
	if network.IsIPv6Addr(ipAddr) {
		return "https://" + fmt.Sprintf("[%s]", ipAddr)
	}
	if network.IsIPv4Addr(ipAddr) {
		return "https://" + ipAddr
	} else {
		return ""
	}
}

func (r *BMOUtils) getPodHostIP(targetNamespace string) (string, error) {
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
	err = r.c.List(context.Background(), podList, &listOptions)
	if err != nil {
		return "", err
	}
	var hostIP string
	switch len(podList.Items) {
	case 0:
		err = fmt.Errorf("failed to find a pod with the given label")
	case 1:
		hostIP = podList.Items[0].Status.HostIP
	default:
		// We expect only one pod with the above LabelSelector
		err = fmt.Errorf("there should be only one pod listed for the given label")
	}
	return hostIP, err
}
