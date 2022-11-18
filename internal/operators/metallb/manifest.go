package metallb

import (
	"bytes"
	"text/template"
)

// Manifests returns manifests needed to deploy LVM
func Manifests() (map[string][]byte, []byte, error) {
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
	metalLBConfigs, err := getMetalLBOperand()
	if err != nil {
		return nil, nil, err
	}

	openshiftManifests := make(map[string][]byte)

	openshiftManifests["50_openshift-metallb_ns.yaml"] = metalLBNamespace
	openshiftManifests["50_openshift-metallb_operator_group.yaml"] = metalLBOperatorGroup
	openshiftManifests["50_openshift-metallb_subscription.yaml"] = metalLBSubscription

	return openshiftManifests, metalLBConfigs, nil
}

func getSubscription() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":         Operator.Namespace,
		"OPERATOR_SUBSCRIPTION_NAME": Operator.SubscriptionName,
	}
	return executeTemplate(data, "MetalLBSubscription", MetalLBSubscription)
}

func getNamespace() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": Operator.Namespace,
	}
	return executeTemplate(data, "MetalLBNamespace", MetalLBNamespace)
}

func getOperatorGroup() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE":  Operator.Namespace,
		"OPERATOR_GROUP_NAME": Operator.SubscriptionName,
	}
	return executeTemplate(data, "MetalLBOperatorGroup", MetalLBOperatorGroup)
}

func getMetalLBOperand() ([]byte, error) {
	data := map[string]string{
		"OPERATOR_NAMESPACE": Operator.Namespace,
	}
	return executeTemplate(data, "MetalLBOperand", MetalLBOperand)
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

const MetalLBNamespace = `apiVersion: v1
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
  namespace: "{{.OPERATOR_NAMESPACE}}"`

const MetalLBOperand = `apiVersion: metallb.io/v1beta1
kind: MetalLB
metadata:
  name: metallb
  namespace: metallb
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: metallb-api-vip
  namespace: metallb
spec:
  protocol: layer2
  autoAssign: false
  addresses:
    - $METALLB_IP/32
---
apiVersion: metallb.io/v1beta1
kind: IPAddressPool
metadata:
  name: metallb-ingress-ip
  namespace: metallb
spec:
  protocol: layer2
  autoAssign: false
  addresses:
    - $METALLB_IP/32
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
