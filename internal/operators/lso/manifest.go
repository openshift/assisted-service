package lso

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/sirupsen/logrus"
)

func getCatalogSource(openshiftVersion string) ([]byte, string, error) {
	v := common.GetParsedVersion(openshiftVersion)
	catalogSourceName := fmt.Sprintf("redhat-operators-v%s-%s", v[0], v[1])
	data := map[string]string{
		"MAJOR":               v[0],
		"MINOR":               v[1],
		"CATALOG_SOURCE_NAME": catalogSourceName,
	}

	const catalogSource = `kind: CatalogSource
apiVersion: operators.coreos.com/v1alpha1
metadata:
  name: {{.CATALOG_SOURCE_NAME}}
  namespace: openshift-marketplace
spec:
  displayName: Red Hat Operators v{{.MAJOR}}.{{.MINOR}}
  description: This additional catalog source is added by the assisted-installer when LSO is deployed alongside pre-release OCP installations.
  image: registry.redhat.io/redhat/redhat-operator-index:v{{.MAJOR}}.{{.MINOR}}
  priority: -100
  publisher: Red Hat
  sourceType: grpc
  updateStrategy:
    registryPoll:
      interval: 10m0s`

	tmpl, err := template.New("catalogSource").Parse(catalogSource)
	if err != nil {
		return nil, "", err
	}
	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, data)
	if err != nil {
		return nil, "", err
	}
	return buf.Bytes(), catalogSourceName, nil
}

func lsoSubscription(catalogSourceName string) ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": Operator.SubscriptionName,
		"CATALOG_SOURCE_NAME":        catalogSourceName,
	}

	const lsoSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  installPlanApproval: Automatic
  name: local-storage-operator
  source: {{.CATALOG_SOURCE_NAME}}
  sourceNamespace: openshift-marketplace`

	tmpl, err := template.New("lsoSubscription").Parse(lsoSubscription)
	if err != nil {
		return nil, err
	}
	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, data)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func addDynamicManifests(log logrus.FieldLogger, c *common.Cluster, handler versions.Handler, openshiftManifests map[string][]byte) error {
	catalogSourceName := "redhat-operators"

	// Use the latest stable LSO operator instead of the pre-release ones because the former is
	// usually not published in the beta releases' Red Hat Operators catalog.
	// Therefore, this patch enforces the usage of the previous stable version.
	defaultReleaseImage, err := handler.GetDefaultReleaseImage(c.CPUArchitecture)
	if err != nil {
		return err
	}
	if isGreater, _ := common.BaseVersionIsGreater(*defaultReleaseImage.OpenshiftVersion, c.OpenshiftVersion); isGreater {
		log.Infof("Found pre-release OCP version %s, adding %s CatalogSource", c.OpenshiftVersion, *defaultReleaseImage.OpenshiftVersion)
		catalogSource, csName, err2 := getCatalogSource(*defaultReleaseImage.OpenshiftVersion)
		if err2 != nil {
			return err2
		}
		catalogSourceName = csName
		openshiftManifests["50_openshift-lso_catalog_source.yaml"] = catalogSource
	}

	// Add LSO subscription with the given CatalogSource name
	lsoSubs, err := lsoSubscription(catalogSourceName)
	if err != nil {
		return err
	}
	openshiftManifests["50_openshift-lso_subscription.yaml"] = lsoSubs
	return nil
}

func Manifests(log logrus.FieldLogger, c *common.Cluster, handler versions.Handler) (map[string][]byte, []byte, error) {
	log.Infof("Generating cluster %s LSO manifests", c.ID.String())
	openshiftManifests := make(map[string][]byte)
	openshiftManifests["50_openshift-lso_ns.yaml"] = []byte(localStorageNamespace)
	openshiftManifests["50_openshift-lso_operator_group.yaml"] = []byte(lsoOperatorGroup)

	if err := addDynamicManifests(log, c, handler, openshiftManifests); err != nil {
		return nil, nil, err
	}

	log.Infof("Generated %d cluster %s LSO manifests successfully", len(openshiftManifests), c.ID.String())
	return openshiftManifests, []byte(localVolumeSet), nil
}

const lsoOperatorGroup = `apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  annotations:
    olm.providedAPIs: LocalVolume.v1.local.storage.openshift.io
  name: local-storage
  namespace: openshift-local-storage
spec:
  targetNamespaces:
  - openshift-local-storage`

const localStorageNamespace = `apiVersion: v1
kind: Namespace
metadata:
  name: openshift-local-storage`

const localVolumeSet = `apiVersion: "local.storage.openshift.io/v1alpha1"
kind: "LocalVolumeSet"
metadata:
  name: "local-disks"
  namespace: "openshift-local-storage"
spec:
  storageClassName: "localblock-sc"
  volumeMode: Block
  deviceInclusionSpec:
    deviceTypes:
      - "disk"`
