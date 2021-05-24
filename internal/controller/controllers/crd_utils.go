package controllers

import (
	"context"

	aiv1beta1 "github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
		infraEnv, err1 := getInfraEnvByClusterDeployment(ctx, log, u.client, clusterName, clusterNamespace)
		if err1 != nil {
			return errors.Wrapf(err, "Failed to search an InfraEnv for Cluster %s", clusterName)
		}
		if infraEnv == nil {
			log.Warnf("ClusterDeployment %s has no InfraEnv resources.", clusterName)
			return errors.Wrapf(err, "No InfraEnv resource for ClusterDeployment %s", clusterName)
		}

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
				Namespace: infraEnv.Namespace,
			},
		}
		log.Infof("Creating Agent CR. Namespace: %s, Cluster: %s, HostID: %s", infraEnv.Namespace, clusterName, hostId)
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
