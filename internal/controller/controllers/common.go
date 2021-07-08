package controllers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"
	"sort"
	"strings"

	bmh_v1alpha1 "github.com/metal3-io/baremetal-operator/apis/metal3.io/v1alpha1"
	hiveext "github.com/openshift/assisted-service/api/hiveextension/v1beta1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/pkg/requestid"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	machinev1beta1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/scale/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	mirrorRegistryRefCertKey         = "ca-bundle.crt"
	mirrorRegistryRefRegistryConfKey = "registries.conf"
	mirrorRegistryConfigVolume       = "mirror-registry-config"
)

func getPullSecret(ctx context.Context, c client.Client, ref *corev1.LocalObjectReference, namespace string) (string, error) {
	if ref == nil {
		return "", newInputError("Missing reference to pull secret")
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      ref.Name,
	}
	if err := c.Get(ctx, key, secret); err != nil {
		return "", errors.Wrapf(err, "failed to get pull secret %s", key)
	}

	data, ok := secret.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return "", errors.Errorf("secret %s did not contain key %s", ref.Name, corev1.DockerConfigJsonKey)
	}

	return string(data), nil
}

func getInfraEnvByClusterDeployment(ctx context.Context, log logrus.FieldLogger, c client.Client, name, namespace string) (*aiv1beta1.InfraEnv, error) {
	infraEnvs := &aiv1beta1.InfraEnvList{}
	if err := c.List(ctx, infraEnvs); err != nil {
		log.WithError(err).Errorf("failed to search for infraEnv for clusterDeployment %s", name)
		return nil, err
	}
	for _, infraEnv := range infraEnvs.Items {
		clusterRef := infraEnv.Spec.ClusterRef
		if clusterRef.Name == name && clusterRef.Namespace == namespace {
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

func getReleaseImage(ctx context.Context, c client.Client, imageSetName string) (string, error) {
	clusterImageSet := &hivev1.ClusterImageSet{}
	key := types.NamespacedName{
		Namespace: "",
		Name:      imageSetName,
	}
	if err := c.Get(ctx, key, clusterImageSet); err != nil {
		return "", errors.Wrapf(err, "failed to get cluster image set %s", key)
	}

	return clusterImageSet.Spec.ReleaseImage, nil
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
	return schemes
}

// checksumMap produces a checksum of a ConfigMap's Data attribute. The checksum
// can be used to detect when the contents of a ConfigMap have changed.
func checksumMap(m map[string]string) (string, error) {
	keys := sort.StringSlice([]string{})
	for k := range m {
		keys = append(keys, k)
	}
	keys.Sort()

	hash := sha256.New()
	encoder := base64.NewEncoder(base64.StdEncoding, hash)

	for _, k := range keys {
		for _, data := range [][]byte{
			[]byte(k),
			[]byte(m[k]),
		} {
			// We base64 encode the data to limit the character set and then use
			// ":" as a separator.
			_, err := encoder.Write(data)
			if err != nil {
				return "", err
			}
			_, err = hash.Write([]byte(":"))
			if err != nil {
				return "", err
			}
		}
	}
	encoder.Close()

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
