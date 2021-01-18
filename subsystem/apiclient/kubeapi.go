package apiclient

import (
	"context"
	"fmt"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/common"
	adiiov1alpha1 "github.com/openshift/assisted-service/internal/controller/api/v1alpha1"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

type KubeAPIClient struct {
	client    runtimeclient.Client
	namespace string
	db        *gorm.DB
}

func NewKubeAPIClient(
	namespace string,
	config *rest.Config,
	db *gorm.DB,
) (*KubeAPIClient, error) {

	client, err := runtimeclient.New(config, runtimeclient.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, err
	}
	kc := &KubeAPIClient{
		client:    client,
		namespace: namespace,
		db:        db,
	}
	return kc, nil
}

func (kc *KubeAPIClient) RegisterCluster(
	ctx context.Context,
	params *installer.RegisterClusterParams,
) (*models.Cluster, error) {

	cParams := params.NewClusterParams

	clusterName := swag.StringValue(cParams.Name)

	pullSecretRef, pullSecretErr := kc.GetSecretRefAndDeployIfNotExists(
		ctx,
		"pull-secret",
		swag.StringValue(cParams.PullSecret))
	if pullSecretErr != nil {
		return nil, errors.Wrapf(pullSecretErr, "failed to deploy pull secret")
	}

	metaName := uuid.New().String()
	c := adiiov1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: getAPIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      metaName,
			Namespace: kc.namespace,
		},
		Spec: adiiov1alpha1.ClusterSpec{
			Name:                     clusterName,
			OpenshiftVersion:         swag.StringValue(cParams.OpenshiftVersion),
			BaseDNSDomain:            swag.StringValue(&cParams.BaseDNSDomain),
			ClusterNetworkCidr:       swag.StringValue(cParams.ClusterNetworkCidr),
			ClusterNetworkHostPrefix: swag.Int64Value(&cParams.ClusterNetworkHostPrefix),
			ServiceNetworkCidr:       swag.StringValue(cParams.ServiceNetworkCidr),
			IngressVip:               swag.StringValue(&cParams.IngressVip),
			SSHPublicKey:             swag.StringValue(&cParams.SSHPublicKey),
			VIPDhcpAllocation:        swag.BoolValue(cParams.VipDhcpAllocation),
			HTTPProxy:                swag.StringValue(cParams.HTTPProxy),
			HTTPSProxy:               swag.StringValue(cParams.HTTPSProxy),
			NoProxy:                  swag.StringValue(cParams.NoProxy),
			UserManagedNetworking:    swag.BoolValue(cParams.UserManagedNetworking),
			AdditionalNtpSource:      swag.StringValue(cParams.AdditionalNtpSource),
			PullSecretRef:            pullSecretRef,
		},
	}
	if deployErr := kc.client.Create(ctx, &c); deployErr != nil {
		return nil, errors.Wrapf(deployErr, "failed to deploy cluster")
	}

	key := types.NamespacedName{
		Name:      metaName,
		Namespace: kc.namespace,
	}
	return kc.getClusterWithRetries(ctx, key)
}

func (kc *KubeAPIClient) GetSecretRefAndDeployIfNotExists(
	ctx context.Context,
	secretName, pullSecret string,
) (*corev1.SecretReference, error) {

	key := types.NamespacedName{
		Name:      secretName,
		Namespace: kc.namespace,
	}
	if _, err := kc.getSecret(ctx, key); runtimeclient.IgnoreNotFound(err) != nil {
		return nil, err
	} else if err == nil {
		return &corev1.SecretReference{
			Name:      secretName,
			Namespace: kc.namespace,
		}, nil
	}
	return kc.deployPullSecret(ctx, secretName, pullSecret)
}

func (kc *KubeAPIClient) deployPullSecret(
	ctx context.Context,
	name, secret string,
) (*corev1.SecretReference, error) {

	if secret == "" {
		return nil, nil
	}

	s := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: getAPIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: kc.namespace,
		},
		StringData: map[string]string{"pullSecret": secret},
	}
	if err := kc.client.Create(ctx, s); err != nil {
		return nil, err
	}

	return &corev1.SecretReference{
		Name:      name,
		Namespace: kc.namespace,
	}, nil
}

func (kc *KubeAPIClient) getSecret(ctx context.Context, key types.NamespacedName) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	if err := kc.client.Get(ctx, key, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func (kc *KubeAPIClient) getClusterWithRetries(
	ctx context.Context,
	key types.NamespacedName,
) (*models.Cluster, error) {

	var cluster *models.Cluster
	var getClusterErr error
	for i := 0; i < 5; i++ {
		if verifyStatusErr := kc.verifyClusterStatus(ctx, key); verifyStatusErr != nil {
			return nil, verifyStatusErr
		}
		if cluster, getClusterErr = kc.getClusterByKey(key); getClusterErr == gorm.ErrRecordNotFound {
			time.Sleep(time.Millisecond * 500)
			continue
		}
		break
	}
	if getClusterErr != nil {
		return nil, errors.Wrapf(getClusterErr, "failed to get cluster")
	}
	return cluster, nil
}

func (kc *KubeAPIClient) verifyClusterStatus(ctx context.Context, key types.NamespacedName) error {
	cluster := &adiiov1alpha1.Cluster{}
	if err := kc.client.Get(ctx, key, cluster); err != nil {
		return err
	}
	if cluster.Status.Error != "" {
		return errors.Errorf("cluster status error: %s", cluster.Status.Error)
	}
	return nil
}

func (kc *KubeAPIClient) getClusterByKey(key types.NamespacedName) (*models.Cluster, error) {
	c := &common.Cluster{}
	if res := kc.db.Take(&c, "kube_key_name = ? and kube_key_namespace = ?",
		key.Name, key.Namespace); res.Error != nil {
		return nil, res.Error
	}
	return &c.Cluster, nil
}

func (kc *KubeAPIClient) DeregisterCluster(
	ctx context.Context,
	params *installer.DeregisterClusterParams,
) error {

	cluster, getErr := kc.getClusterByID(params.ClusterID)
	if getErr != nil {
		return errors.Wrapf(getErr, "failed getting cluster from db")
	}
	c := &adiiov1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: kc.namespace,
		},
	}
	if delErr := kc.client.Delete(ctx, c); delErr != nil {
		return errors.Wrapf(delErr, "failed deleting cluster")
	}

	// todo: delete this part when db deletion will be a part of the cluster controller
	if dbErr := kc.db.Delete(&common.Cluster{}, "id = ?", params.ClusterID.String()).Error; dbErr != nil {
		return errors.Wrapf(dbErr, "failed deleting cluster from db")
	}
	return nil
}

func (kc *KubeAPIClient) getClusterByID(id strfmt.UUID) (*models.Cluster, error) {
	c := &common.Cluster{}
	if res := kc.db.Take(c, "id = ?", id.String()); res.Error != nil {
		return nil, res.Error
	} else if res.RowsAffected == 0 {
		return nil, errors.New("cluster was not found in db")
	}
	return &c.Cluster, nil
}

func (kc *KubeAPIClient) UpdateCluster(
	ctx context.Context,
	params *installer.UpdateClusterParams,
) (*models.Cluster, error) {

	cParams := params.ClusterUpdateParams

	clusterName := swag.StringValue(cParams.Name)

	pullSecretRef, pullSecretErr := kc.GetSecretRefAndDeployIfNotExists(
		ctx,
		clusterName,
		swag.StringValue(cParams.PullSecret))
	if pullSecretErr != nil {
		return nil, errors.Wrapf(pullSecretErr, "failed to deploy pull secret")
	}

	c := adiiov1alpha1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Cluster",
			APIVersion: getAPIVersion(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: kc.namespace,
		},
		Spec: adiiov1alpha1.ClusterSpec{
			Name:                     clusterName,
			BaseDNSDomain:            swag.StringValue(cParams.BaseDNSDomain),
			ClusterNetworkCidr:       swag.StringValue(cParams.ClusterNetworkCidr),
			ClusterNetworkHostPrefix: swag.Int64Value(cParams.ClusterNetworkHostPrefix),
			ServiceNetworkCidr:       swag.StringValue(cParams.ServiceNetworkCidr),
			IngressVip:               swag.StringValue(cParams.IngressVip),
			SSHPublicKey:             swag.StringValue(cParams.SSHPublicKey),
			VIPDhcpAllocation:        swag.BoolValue(cParams.VipDhcpAllocation),
			HTTPProxy:                swag.StringValue(cParams.HTTPProxy),
			HTTPSProxy:               swag.StringValue(cParams.HTTPSProxy),
			NoProxy:                  swag.StringValue(cParams.NoProxy),
			UserManagedNetworking:    swag.BoolValue(cParams.UserManagedNetworking),
			AdditionalNtpSource:      swag.StringValue(cParams.AdditionalNtpSource),
			PullSecretRef:            pullSecretRef,
		},
	}
	if deployErr := kc.client.Update(ctx, &c); deployErr != nil {
		return nil, errors.Wrapf(deployErr, "failed to deploy cluster")
	}

	key := types.NamespacedName{
		Name:      clusterName,
		Namespace: kc.namespace,
	}
	cluster, getClusterErr := kc.getClusterByKey(key)
	if getClusterErr != nil {
		return nil, errors.Wrapf(getClusterErr, "failed to get cluster after creation")
	}
	return cluster, nil
}

func (kc *KubeAPIClient) DeleteAllClusters(ctx context.Context) error {
	cl := &adiiov1alpha1.ClusterList{}
	if err := kc.client.List(ctx, cl); err != nil {
		return errors.Wrapf(err, "failed listing clusters")
	}
	for _, c := range cl.Items {
		if err := kc.client.Delete(ctx, &c); err != nil {
			return errors.Wrapf(err, "failed deleting cluster")
		}
	}
	return nil
}

func getAPIVersion() string {
	return fmt.Sprintf("%s/%s", adiiov1alpha1.GroupVersion.Group, adiiov1alpha1.GroupVersion.Version)
}
