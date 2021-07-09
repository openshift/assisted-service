package controllers

import (
	"context"

	"github.com/go-openapi/strfmt"
	"github.com/jinzhu/gorm"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/host"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CRDUtils struct {
	client  client.Client
	hostApi host.API
}

func NewCRDUtils(client client.Client, hostApi host.API) *CRDUtils {
	return &CRDUtils{client: client, hostApi: hostApi}
}

// CreateAgentCR Creates an Agent CR representing a Host/Agent.
// If the Host is already registered, the CR creation will be skipped.
// if the Cluster was not created via K8s API, then the Cluster NameSpace will be empty and Agent CR creation will be skipped
func (u *CRDUtils) CreateAgentCR(ctx context.Context, log logrus.FieldLogger, hostId, clusterNamespace, clusterName string, clusterID *strfmt.UUID) error {

	if clusterNamespace == "" {
		return nil
	}

	infraEnv, err := getInfraEnvByClusterDeployment(ctx, log, u.client, clusterName, clusterNamespace)
	if err != nil {
		return errors.Wrapf(err, "Failed to search an InfraEnv for Cluster %s", clusterName)
	}
	if infraEnv == nil {
		log.Warnf("ClusterDeployment %s has no InfraEnv resources.", clusterName)
		return errors.Errorf("No InfraEnv resource for ClusterDeployment %s", clusterName)
	}

	host := &aiv1beta1.Agent{}
	namespacedName := types.NamespacedName{
		Name:      hostId,
		Namespace: infraEnv.Namespace,
	}

	err = u.client.Get(ctx, namespacedName, host)

	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if err != nil && k8serrors.IsNotFound(err) {
		labels := map[string]string{aiv1beta1.InfraEnvNameLabel: infraEnv.Name}
		if infraEnv.Spec.AgentLabels != nil {
			for k, v := range infraEnv.Spec.AgentLabels {
				labels[k] = v
			}
		}

		host = &aiv1beta1.Agent{
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
				Labels:    labels,
			},
		}

		// Update 'kube_key_namespace' in Host
		if err1 := u.hostApi.UpdateKubeKeyNS(ctx, hostId, infraEnv.Namespace); err1 != nil {
			return errors.Wrapf(err1, "Failed to update 'kube_key_namespace' in host %s", hostId)
		}

		log.Infof("Creating Agent CR. Namespace: %s, Cluster: %s, HostID: %s", infraEnv.Namespace, clusterName, hostId)
		return u.client.Create(ctx, host)
	}

	if err == nil {
		log.Infof("Agent CR %s already exists", hostId)
		key := types.NamespacedName{Name: hostId, Namespace: infraEnv.Namespace}
		// fetch previous by KubeKey
		h, err2 := u.hostApi.GetHostByKubeKey(key)
		if err2 != nil && !errors.Is(err2, gorm.ErrRecordNotFound) {
			return errors.Wrapf(err2, "Failed to GetHostByKubeKey Name: %s Namespace: %s", key.Name, key.Namespace)
		}

		if err2 == nil {
			if h.ClusterID == *clusterID {
				log.Infof("Agent CR %s already exists, same cluster %s", hostId, h.ClusterID)
				return nil
			}
			//delete previous host
			if err3 := u.hostApi.UnRegisterHost(ctx, h.ID.String(), h.ClusterID.String()); err3 != nil {
				return errors.Wrapf(err3, "Failed to UnRegisterHost ID: %s ClusterID: %s", h.ID.String(), h.ClusterID.String())
			}
		}
		//Reset spec
		labels := map[string]string{aiv1beta1.InfraEnvNameLabel: infraEnv.Name}
		if infraEnv.Spec.AgentLabels != nil {
			for k, v := range infraEnv.Spec.AgentLabels {
				labels[k] = v
			}
		}

		updatedhost := &aiv1beta1.Agent{
			Spec: aiv1beta1.AgentSpec{
				ClusterDeploymentName: &aiv1beta1.ClusterReference{
					Name:      clusterName,
					Namespace: clusterNamespace,
				},
				Approved:                false,
				Hostname:                "",
				MachineConfigPool:       "",
				Role:                    "",
				InstallationDiskID:      "",
				InstallerArgs:           "",
				IgnitionConfigOverrides: "",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:            hostId,
				Namespace:       infraEnv.Namespace,
				Labels:          labels,
				ResourceVersion: host.ResourceVersion,
			},
		}

		// Update 'kube_key_namespace' in Host
		if err1 := u.hostApi.UpdateKubeKeyNS(ctx, hostId, infraEnv.Namespace); err1 != nil {
			return errors.Wrapf(err, "Failed to update 'kube_key_namespace' in host %s", hostId)
		}

		log.Infof("Updating Agent CR. Namespace: %s, Cluster: %s, HostID: %s", infraEnv.Namespace, clusterName, hostId)
		return u.client.Update(ctx, updatedhost)
	}

	return nil
}

type DummyCRDUtils struct{}

func NewDummyCRDUtils() *DummyCRDUtils {
	return &DummyCRDUtils{}
}

func (u *DummyCRDUtils) CreateAgentCR(ctx context.Context, log logrus.FieldLogger, hostId, clusterNamespace, clusterName string, clusterID *strfmt.UUID) error {
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
