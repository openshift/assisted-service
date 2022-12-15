package controllers

import (
	"context"
	"fmt"

	v1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	"github.com/openshift/cluster-baremetal-operator/provisioning"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const MinimalVersionForConvergedFlow = "4.12.0-0.alpha"

type BMOUtils struct {
	// The methods of this receiver get called once before the cache is initialized hence we check the API directly
	c              client.Reader
	osClient       *osclientset.Clientset
	kubeClient     *kubernetes.Clientset
	log            logrus.FieldLogger
	kubeAPIEnabled bool
}

func NewBMOUtils(client client.Reader, osClient *osclientset.Clientset, kubeClient *kubernetes.Clientset, log logrus.FieldLogger, kubeAPIEnabled bool) *BMOUtils {
	return &BMOUtils{client, osClient, kubeClient, log, kubeAPIEnabled}
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=clusteroperators,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures,verbs=get;list;watch
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

func (r *BMOUtils) GetIronicServiceURLS() (string, string, error) {
	provisioningInfo, err := r.readProvisioningCR()
	if err != nil {
		r.log.WithError(err).Error("unable to get provisioning CR")
		return "", "", err
	}
	ironicIP, inspectorIP, err := provisioning.GetIronicIP(r.kubeClient, provisioningInfo.Namespace, &provisioningInfo.Spec, r.osClient)
	if err != nil || ironicIP == "" {
		r.log.WithError(err).Error("unable to determine Ironic's IP")
		return "", "", err
	}
	ironicURL := getUrlFromIP(ironicIP)
	r.log.Infof("Ironic URL is: %s", ironicURL)
	inspectorURL := getUrlFromIP(inspectorIP)
	r.log.Infof("Inspector URL is: %s", inspectorURL)
	return ironicURL, inspectorURL, nil
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
