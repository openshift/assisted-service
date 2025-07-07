package metallb

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"

	"github.com/openshift/assisted-service/internal/common"
)

func parsePropertiesField(cluster *common.Cluster) (*Properties, error) {
	if cluster.MonitoredOperators == nil {
		return &Properties{}, nil
	}

	for _, operator := range cluster.MonitoredOperators {
		if operator.Properties == "" {
			continue
		}

		var props Properties
		err := json.Unmarshal([]byte(operator.Properties), &props)
		if err != nil {
			return nil, fmt.Errorf("failed to parse MetalLB properties: %w", err)
		}

		return &props, nil
	}

	return &Properties{}, nil
}

// Manifests returns manifests needed to deploy MetalLB
func Manifests(cluster *common.Cluster) (map[string][]byte, []byte, error) {
	metalLBSubscription, err := getSubscription()
	if err != nil {
		return nil, nil, err
	}
	metalLBNamespace, err := getNamespace()
	if err != nil {
		return nil, nil, err
	}
	metalLBOperatorGroup, err := getOperatorGroup()
	if err != nil {
		return nil, nil, err
	}
	metalLBConfig, err := getMetalLBOperand(cluster)
	if err != nil {
		return nil, nil, err
	}

	openshiftManifests := make(map[string][]byte)
	openshiftManifests["50_metallb_ns.yaml"] = metalLBNamespace
	openshiftManifests["50_metallb_operator_group.yaml"] = metalLBOperatorGroup
	openshiftManifests["50_metallb_subscription.yaml"] = metalLBSubscription

	return openshiftManifests, metalLBConfig, nil
}

func getSubscription() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         MetalLBNamespace,
		"OPERATOR_SUBSCRIPTION_NAME": MetalLBSubscriptionName,
	}
	return executeTemplate(data, "MetalLBSubscription", MetalLBSubscription)
}

func getNamespace() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": MetalLBNamespace,
	}
	return executeTemplate(data, "MetalLBNamespace", MetalLBNamespaceTemplate)
}

func getOperatorGroup() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":  MetalLBNamespace,
		"OPERATOR_GROUP_NAME": MetalLBSubscriptionName,
	}
	return executeTemplate(data, "MetalLBOperatorGroup", MetalLBOperatorGroup)
}

func getMetalLBOperand(cluster *common.Cluster) ([]byte, error) {
	properties, err := parsePropertiesField(cluster)
	if err != nil {
		return nil, err
	}

	data := map[string]string{
		"OPERATOR_NAMESPACE": MetalLBNamespace,
		"API_IP":             properties.ApiIP,
		"INGRESS_IP":         properties.IngressIP,
	}
	operand := MetalLBOperandPartA

	if properties.ApiIP != "" {
		operand += apiIpAddressPoolResource
	}
	if properties.IngressIP != "" {
		operand += ingressIpAddressPoolResource
	}

	operand += MetalLBOperandPartB
	return executeTemplate(data, "MetalLBOperand", operand)
}

func executeTemplate(data map[string]string, contentName, content string) ([]byte, error) {
	tmpl, err := template.New(contentName).Parse(content)
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

const MetalLBSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: "{{.OPERATOR_SUBSCRIPTION_NAME}}"
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  channel: "stable"
  name: metallb-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace`

const MetalLBNamespaceTemplate = `apiVersion: v1
kind: Namespace
metadata:
  name: "{{.OPERATOR_NAMESPACE}}"
  labels:
    openshift.io/cluster-monitoring: "true"
  annotations:
    workload.openshift.io/allowed: management
spec: {}`

const MetalLBOperatorGroup = `apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: "{{.OPERATOR_GROUP_NAME}}"
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  targetNamespaces:
  - "{{.OPERATOR_NAMESPACE}}"`

const MetalLBOperandPartA = `apiVersion: metallb.io/v1beta1
kind: MetalLB
metadata:
  name: metallb
  namespace: "{{.OPERATOR_NAMESPACE}}"
---
`

const apiIpAddressPoolResource = `apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: metallb-api-vip
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  autoAssign: false
  addresses:
    - {{.API_IP}}/32
---
`

const ingressIpAddressPoolResource = `apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: metallb-ingress-ip
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  autoAssign: false
  addresses:
    - {{.INGRESS_IP}}/32
---
`

const MetalLBOperandPartB = `apiVersion: metallb.io/v1beta1
kind: L2Advertisement
metadata:
  name: cluster-api-ingress
  namespace: "{{.OPERATOR_NAMESPACE}}"
spec:
  ipAddressPools:
    - metallb-ingress-ip
    - metallb-api-vip
---
apiVersion: v1
kind: Service
metadata:
  annotations:
    metallb.universe.tf/address-pool: metallb-api-vip
  name: metallb-api
  namespace: openshift-kube-apiserver
spec:
  ports:
  - name: http
    protocol: TCP
    port: 6443
    targetPort: 6443
  selector:
    app: openshift-kube-apiserver
  type: LoadBalancer
---
apiVersion: v1
kind: Service
metadata:
  annotations:
    metallb.universe.tf/address-pool: metallb-ingress-ip
  name: metallb-ingress
  namespace: openshift-ingress
spec:
  ports:
    - name: http
      protocol: TCP
      port: 80
      targetPort: 80
    - name: https
      protocol: TCP
      port: 443
      targetPort: 443
  selector:
    ingresscontroller.operator.openshift.io/deployment-ingresscontroller: default
  type: LoadBalancer`
