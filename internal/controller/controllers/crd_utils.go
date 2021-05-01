package controllers

import (
	"context"
	"strings"
	"time"

	aiv1beta1 "github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/registry/generic/registry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRDUtils struct {
	client client.Client
}

func NewCRDUtils(client client.Client) *CRDUtils {
	return &CRDUtils{client: client}
}

// CreateAgentCR Creates an Agent CR representing a Host/Agent.
// If the Host is already registered, the CR creation will be skipped.
// if the Cluster was not created via K8s API, then the Cluster NameSpace will be empty and Agent CR creation will be skipped
func (u *CRDUtils) CreateAgentCR(ctx context.Context, log logrus.FieldLogger, hostId, clusterNamespace, clusterName string) error {

	if clusterNamespace == "" {
		return nil
	}

	host := &aiv1beta1.Agent{}
	namespacedName := types.NamespacedName{
		Namespace: clusterNamespace,
		Name:      hostId,
	}
	err := u.client.Get(ctx, namespacedName, host)
	if err == nil {
		log.Infof("Skip Agent CR creation. %s already exists", hostId)
		return nil
	}

	if k8serrors.IsNotFound(err) {
		host := &aiv1beta1.Agent{
			Spec: aiv1beta1.AgentSpec{
				ClusterDeploymentName: &aiv1beta1.ClusterReference{
					Name:      clusterName,
					Namespace: clusterNamespace,
				},
				Approved: false,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      hostId,
				Namespace: clusterNamespace,
			},
		}
		log.Infof("Creating Agent CR. Namespace: %s, Cluster: %s, HostID: %s", clusterNamespace, clusterName, hostId)
		return u.client.Create(ctx, host)
	}
	return err
}

type DummyCRDUtils struct{}

func NewDummyCRDUtils() *DummyCRDUtils {
	return &DummyCRDUtils{}
}

func (u *DummyCRDUtils) CreateAgentCR(ctx context.Context, log logrus.FieldLogger, hostId, clusterNamespace, clusterName string) error {
	return nil
}

func AddLabel(labels map[string]string, labelKey, labelValue string) map[string]string {
	if labelKey == "" {
		// Don't need to add a label.
		return labels
	}
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[labelKey] = labelValue
	return labels
}

type UpdateStatusFunc func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error

func UpdateStatus(
	log logrus.FieldLogger,
	updateFunc UpdateStatusFunc,
	ctx context.Context,
	obj client.Object,
	opts ...client.UpdateOption) (bool, ctrl.Result, error) {

	err := updateFunc(ctx, obj, opts...)
	if err == nil {
		return true, ctrl.Result{}, nil
	} else if strings.Contains(err.Error(), registry.OptimisticLockErrorMsg) {
		// The given resource has stale data.
		// Try to reconcile again the up-to-date resource as soon as possible.
		log.Debugf("Failed to update %s Status of %s - resource is out of date (will try again)",
			obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
		return false, ctrl.Result{RequeueAfter: time.Second}, nil
	}
	log.WithError(err).Errorf("Failed to update %s Status of %s",
		obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
	return false, ctrl.Result{Requeue: true}, err
}
