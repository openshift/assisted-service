package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"hash"
	"io"
	"math/big"
	"net/url"
	"sort"
	"strings"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	routev1 "github.com/openshift/api/route/v1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	restclient "github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	"github.com/openshift/assisted-service/pkg/requestid"
	metal3iov1alpha1 "github.com/openshift/cluster-baremetal-operator/api/v1alpha1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	apiregv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	mirrorRegistryRefCertKey         = "ca-bundle.crt"
	mirrorRegistryRefRegistryConfKey = "registries.conf"
	mirrorRegistryConfigVolume       = "mirror-registry-config"
	WatchResourceLabel               = "agent-install.openshift.io/watch"
	WatchResourceValue               = "true"
	BackupLabel                      = "cluster.open-cluster-management.io/backup"
	BackupLabelValue                 = "true"
	InfraEnvLabel                    = "infraenvs.agent-install.openshift.io"
)

//go:generate mockgen --build_flags=--mod=mod -package=controllers -destination=mock_sub_resource_writer.go sigs.k8s.io/controller-runtime/pkg/client SubResourceWriter

//go:generate mockgen --build_flags=--mod=mod -package=controllers -destination=mock_k8s_client.go . K8sClient
type K8sClient interface {
	client.Client
}

func getSecret(ctx context.Context, c client.Client, r client.Reader, key types.NamespacedName) (*corev1.Secret, error) {
	secret := &corev1.Secret{}
	errorMessage := fmt.Sprintf("failed to get secret %s/%s from cache", key.Namespace, key.Name)
	if err := c.Get(ctx, key, secret); err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, errors.Wrapf(err, errorMessage)
		}
		// Secret not in cache; check API directly for unlabelled Secret
		err = r.Get(ctx, key, secret)
		if err != nil {
			errorMessage = fmt.Sprintf("failed to get secret %s/%s from API", key.Namespace, key.Name)
			return nil, errors.Wrapf(err, errorMessage)
		}
	}
	return secret, nil
}

func ensureSecretIsLabelled(ctx context.Context, c client.Client, secret *corev1.Secret, key types.NamespacedName) error {

	// Exit early if secret is nil
	if secret == nil {
		return nil
	}

	// Add backup label to the secret if not present
	if !metav1.HasLabel(secret.ObjectMeta, BackupLabel) {
		metav1.SetMetaDataLabel(&secret.ObjectMeta, BackupLabel, BackupLabelValue)
		err := c.Update(ctx, secret)
		if err != nil {
			errorMessage := fmt.Sprintf("failed to set label %s:%s for secret %s/%s", BackupLabel, BackupLabelValue, key.Namespace, key.Name)
			return errors.Wrapf(err, errorMessage)
		}
	}

	// Add the label to secret if not present
	if !metav1.HasLabel(secret.ObjectMeta, WatchResourceLabel) {
		metav1.SetMetaDataLabel(&secret.ObjectMeta, WatchResourceLabel, WatchResourceValue)
		err := c.Update(ctx, secret)
		if err != nil {
			errorMessage := fmt.Sprintf("failed to set label %s:%s for secret %s/%s", WatchResourceLabel, WatchResourceValue, key.Namespace, key.Name)
			return errors.Wrapf(err, errorMessage)
		}
	}
	return nil
}

func getPullSecretKey(ns string, pullSecretRef *corev1.LocalObjectReference) types.NamespacedName {
	if pullSecretRef == nil {
		return types.NamespacedName{}
	}
	return types.NamespacedName{Namespace: ns, Name: pullSecretRef.Name}
}

func ensureConfigMapIsLabelled(ctx context.Context, c client.Client, cm *corev1.ConfigMap, key types.NamespacedName) error {

	// Exit early if config map is nil
	if cm == nil {
		return nil
	}

	// Add backup label to the config map if not present
	if !metav1.HasLabel(cm.ObjectMeta, BackupLabel) {
		metav1.SetMetaDataLabel(&cm.ObjectMeta, BackupLabel, BackupLabelValue)
		err := c.Update(ctx, cm)
		if err != nil {
			errorMessage := fmt.Sprintf("failed to set label %s:%s for configmap %s/%s", BackupLabel, BackupLabelValue, key.Namespace, key.Name)
			return errors.Wrapf(err, errorMessage)
		}
	}

	// Add the label to configmap if not present
	if !metav1.HasLabel(cm.ObjectMeta, WatchResourceLabel) {
		metav1.SetMetaDataLabel(&cm.ObjectMeta, WatchResourceLabel, WatchResourceValue)
		err := c.Update(ctx, cm)
		if err != nil {
			errorMessage := fmt.Sprintf("failed to set label %s:%s for configmap %s/%s", WatchResourceLabel, WatchResourceValue, key.Namespace, key.Name)
			return errors.Wrapf(err, errorMessage)
		}
	}
	return nil
}

func getInfraEnvByClusterDeployment(ctx context.Context, log logrus.FieldLogger, c client.Client, name, namespace string) (*aiv1beta1.InfraEnv, error) {
	infraEnvs := &aiv1beta1.InfraEnvList{}
	if err := c.List(ctx, infraEnvs); err != nil {
		log.WithError(err).Errorf("failed to search for infraEnv for clusterDeployment %s", name)
		return nil, err
	}
	for _, infraEnv := range infraEnvs.Items {
		clusterRef := infraEnv.Spec.ClusterRef
		if clusterRef != nil && clusterRef.Name == name && clusterRef.Namespace == namespace {
			return &infraEnv, nil
		}
	}
	log.Infof("no infraEnv for the clusterDeployment %s in namespace %s", name, namespace)
	return nil, nil
}

func addAppLabel(appName string, meta *metav1.ObjectMeta) {
	if meta.Labels == nil {
		meta.Labels = make(map[string]string)
	}
	meta.Labels["app"] = appName
}

// generatePassword generates a password of a given length out of the acceptable
// ASCII characters suitable for a password
// taken from https://github.com/CrunchyData/postgres-operator/blob/383dfa95991553352623f14d3d0d4c9193795855/internal/util/secrets.go#L75
func generatePassword(length int) (string, error) {
	password := make([]byte, length)

	// passwordCharLower is the lowest ASCII character to use for generating a
	// password, which is 40
	passwordCharLower := int64(40)
	// passwordCharUpper is the highest ASCII character to use for generating a
	// password, which is 126
	passwordCharUpper := int64(126)
	// passwordCharExclude is a map of characters that we choose to exclude from
	// the password to simplify usage in the shell. There is still enough entropy
	// that exclusion of these characters is OK.
	passwordCharExclude := "`\\"

	// passwordCharSelector is a "big int" that we need to select the random ASCII
	// character for the password. Since the random integer generator looks for
	// values from [0,X), we need to force this to be [40,126]
	passwordCharSelector := big.NewInt(passwordCharUpper - passwordCharLower)

	i := 0

	for i < length {
		val, err := rand.Int(rand.Reader, passwordCharSelector)
		// if there is an error generating the random integer, return
		if err != nil {
			return "", err
		}

		char := byte(passwordCharLower + val.Int64())

		// if the character is in the exclusion list, continue
		if idx := strings.IndexAny(string(char), passwordCharExclude); idx > -1 {
			continue
		}

		password[i] = char
		i++
	}

	return string(password), nil
}

func addRequestIdIfNeeded(ctx context.Context) context.Context {
	ctxWithReqID := ctx
	if requestid.FromContext(ctx) == "" {
		ctxWithReqID = requestid.ToContext(ctx, requestid.NewID())
	}
	return ctxWithReqID
}

func GetKubeClientSchemes() *runtime.Scheme {
	var schemes = runtime.NewScheme()
	utilruntime.Must(scheme.AddToScheme(schemes))
	utilruntime.Must(corev1.AddToScheme(schemes))
	utilruntime.Must(aiv1beta1.AddToScheme(schemes))
	utilruntime.Must(hivev1.AddToScheme(schemes))
	utilruntime.Must(hiveext.AddToScheme(schemes))
	utilruntime.Must(bmh_v1alpha1.AddToScheme(schemes))
	utilruntime.Must(machinev1beta1.AddToScheme(schemes))
	utilruntime.Must(monitoringv1.AddToScheme(schemes))
	utilruntime.Must(routev1.Install(schemes))
	utilruntime.Must(apiregv1.AddToScheme(schemes))
	utilruntime.Must(configv1.Install(schemes))
	utilruntime.Must(metal3iov1alpha1.AddToScheme(schemes))
	utilruntime.Must(apiextensionsv1.AddToScheme(schemes))
	return schemes
}

func newHashAndEncoder() (hash.Hash, io.WriteCloser) {
	hash := sha256.New()
	return hash, base64.NewEncoder(base64.StdEncoding, hash)
}

func hashForKey(data []byte, hash hash.Hash, encoder io.WriteCloser) (hash.Hash, error) {
	// We base64 encode the data to limit the character set and then use
	// ":" as a separator.
	_, err := encoder.Write(data)
	if err != nil {
		return nil, err
	}
	_, err = hash.Write([]byte(":"))
	if err != nil {
		return nil, err
	}
	return hash, nil
}

// checksumMap produces a checksum of a ConfigMap's Data attribute. The checksum
// can be used to detect when the contents of a ConfigMap have changed.
func checksumMap(m map[string]string) (string, error) {
	keys := sort.StringSlice([]string{})
	for k := range m {
		keys = append(keys, k)
	}
	keys.Sort()

	hash, encoder := newHashAndEncoder()
	for _, k := range keys {
		for _, data := range [][]byte{
			[]byte(k),
			[]byte(m[k]),
		} {
			var err error
			hash, err = hashForKey(data, hash, encoder)
			if err != nil {
				return "", nil
			}
		}
	}
	encoder.Close()

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// checksumSecret produces a checksum of a Secret's Data attribute. The checksum
// can be used to detect when the contents of a Secret have changed.
func checksumSecret(m map[string][]byte) (string, error) {
	keys := sort.StringSlice([]string{})
	for k := range m {
		keys = append(keys, k)
	}
	keys.Sort()
	hash, encoder := newHashAndEncoder()
	for _, k := range keys {
		for _, data := range [][]byte{
			[]byte(k),
			m[k],
		} {
			var err error
			hash, err = hashForKey(data, hash, encoder)
			if err != nil {
				return "", nil
			}
		}
	}
	encoder.Close()
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func clusterNetworksArrayToEntries(networks []*models.ClusterNetwork) []hiveext.ClusterNetworkEntry {
	return funk.Map(networks, func(net *models.ClusterNetwork) hiveext.ClusterNetworkEntry {
		return hiveext.ClusterNetworkEntry{CIDR: string(net.Cidr), HostPrefix: int32(net.HostPrefix)}
	}).([]hiveext.ClusterNetworkEntry)
}

func clusterNetworksEntriesToArray(entries []hiveext.ClusterNetworkEntry) []*models.ClusterNetwork {
	return funk.Map(entries, func(entry hiveext.ClusterNetworkEntry) *models.ClusterNetwork {
		return &models.ClusterNetwork{Cidr: models.Subnet(entry.CIDR), HostPrefix: int64(entry.HostPrefix)}
	}).([]*models.ClusterNetwork)
}

func serviceNetworksArrayToStrings(networks []*models.ServiceNetwork) []string {
	return funk.Map(networks, func(net *models.ServiceNetwork) string {
		return string(net.Cidr)
	}).([]string)
}

func serviceNetworksEntriesToArray(entries []string) []*models.ServiceNetwork {
	return funk.Map(entries, func(entry string) *models.ServiceNetwork {
		return &models.ServiceNetwork{Cidr: models.Subnet(entry)}
	}).([]*models.ServiceNetwork)
}

func machineNetworksArrayToEntries(networks []*models.MachineNetwork) []hiveext.MachineNetworkEntry {
	return funk.Map(networks, func(net *models.MachineNetwork) hiveext.MachineNetworkEntry {
		return hiveext.MachineNetworkEntry{CIDR: string(net.Cidr)}
	}).([]hiveext.MachineNetworkEntry)
}

func machineNetworksEntriesToArray(entries []hiveext.MachineNetworkEntry) []*models.MachineNetwork {
	return funk.Map(entries, func(entry hiveext.MachineNetworkEntry) *models.MachineNetwork {
		return &models.MachineNetwork{Cidr: models.Subnet(entry.CIDR)}
	}).([]*models.MachineNetwork)
}

func ApiVipsArrayToStrings(vips []*models.APIVip) []string {
	return funk.Map(vips, func(vip *models.APIVip) string {
		return string(vip.IP)
	}).([]string)
}

func ApiVipsEntriesToArray(entries []string) []*models.APIVip {
	return funk.Map(entries, func(entry string) *models.APIVip {
		return &models.APIVip{IP: models.IP(entry)}
	}).([]*models.APIVip)
}

func IngressVipsArrayToStrings(vips []*models.IngressVip) []string {
	return funk.Map(vips, func(vip *models.IngressVip) string {
		return string(vip.IP)
	}).([]string)
}

func IngressVipsEntriesToArray(entries []string) []*models.IngressVip {
	return funk.Map(entries, func(entry string) *models.IngressVip {
		return &models.IngressVip{IP: models.IP(entry)}
	}).([]*models.IngressVip)
}

func signURL(urlString string, authType auth.AuthType, id string, keyType gencrypto.LocalJWTKeyType) (string, error) {
	// might need to add agent-install-local
	if authType != auth.TypeLocal {
		return urlString, nil
	}
	return gencrypto.SignURL(urlString, id, keyType)
}

func generateEventsURL(baseURL string, authType auth.AuthType, signParams gencrypto.CryptoPair, filterBy ...string) (string, error) {
	path := fmt.Sprintf("%s%s/v2/events", baseURL, restclient.DefaultBasePath)
	u, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	queryParams := u.Query()
	for i := 0; i < len(filterBy)-1; i += 2 {
		queryParams.Set(filterBy[i], filterBy[i+1])
	}
	u.RawQuery = queryParams.Encode()
	return signURL(u.String(), authType, signParams.JWTKeyValue, signParams.JWTKeyType)
}

// In assisted installer, UserManagedNetworking implicates none platform.  This flag is part of AgentClusterInstall spec.
func isNonePlatformCluster(ctx context.Context, client client.Client, cd *hivev1.ClusterDeployment) (isNone, propagateError bool, err error) {
	if cd.Spec.ClusterInstallRef == nil {
		return false, false, errors.Errorf("Cluster Install Reference is null for cluster deployment ns=%s name=%s", cd.Namespace, cd.Name)
	}
	clusterInstall := hiveext.AgentClusterInstall{}
	namespacedName := types.NamespacedName{
		Namespace: cd.Namespace,
		Name:      cd.Spec.ClusterInstallRef.Name,
	}
	if err = client.Get(ctx, namespacedName, &clusterInstall); err != nil {
		return false, !k8serrors.IsNotFound(err), errors.Wrapf(err, "Could not get AgentClusterInstall %s for ClusterDeployment %s", cd.Spec.ClusterInstallRef.Name, cd.Name)
	}
	return isUserManagedNetwork(&clusterInstall), false, nil
}

// We get first agent's cluster deployment and then we query if it belongs to none platform cluster
func isAgentInNonePlatformCluster(ctx context.Context, client client.Client, agent *aiv1beta1.Agent) (isNone bool, err error) {
	var cd hivev1.ClusterDeployment
	if agent.Spec.ClusterDeploymentName == nil {
		return false, errors.Errorf("No cluster deployment for agent %s/%s", agent.Namespace, agent.Name)
	}
	namespacedName := types.NamespacedName{
		Namespace: agent.Spec.ClusterDeploymentName.Namespace,
		Name:      agent.Spec.ClusterDeploymentName.Name,
	}
	if err = client.Get(ctx, namespacedName, &cd); err != nil {
		return false, errors.Wrapf(err, "Failed to get cluster deployment %s/%s", namespacedName.Namespace, namespacedName.Name)
	}
	isNone, _, err = isNonePlatformCluster(ctx, client, &cd)
	return
}

func setAnnotation(meta *metav1.ObjectMeta, key string, value string) {
	if meta.Annotations == nil {
		meta.Annotations = make(map[string]string)
	}
	meta.Annotations[key] = value
}

func spokeKubeconfigSecret(ctx context.Context, log logrus.FieldLogger, c client.Client, r client.Reader, clusterRef *aiv1beta1.ClusterReference) (*corev1.Secret, error) {
	clusterDeployment := &hivev1.ClusterDeployment{}
	cdKey := types.NamespacedName{
		Namespace: clusterRef.Namespace,
		Name:      clusterRef.Name,
	}
	err := r.Get(ctx, cdKey, clusterDeployment)
	if err != nil {
		// set this so it can be used by the following call
		clusterDeployment.Name = cdKey.Name
	}
	adminKubeConfigSecretName := getClusterDeploymentAdminKubeConfigSecretName(clusterDeployment)

	namespacedName := types.NamespacedName{
		Namespace: clusterRef.Namespace,
		Name:      adminKubeConfigSecretName,
	}

	secret, err := getSecret(ctx, c, r, namespacedName)
	if err != nil {
		log.WithError(err).Errorf("failed to get kubeconfig secret %s", namespacedName)
		return nil, err
	}
	if err = ensureSecretIsLabelled(ctx, c, secret, namespacedName); err != nil {
		log.WithError(err).Errorf("failed to label kubeconfig secret %s", namespacedName)
		return nil, err
	}

	return secret, nil
}
