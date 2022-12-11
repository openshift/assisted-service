---
title: ai-on-zero-worker-hypershift
authors:
  - "@slaviered"
creation-date: 2022-08-28
last-updated: yyyy-mm-dd
---
#  Installing the Infrastructure Operator on zero-node HyperShift

## Summary
To support large numbers of HyperShift hosted control planes on a single management cluster, an architecture of nested control planes has been proposed.  The management cluster, which we refer to as L0, runs hypershift hosted control planes (L1). Spoke clusters are added to L1 hosted control planes. The challenge is that we need to run the assisted-installer in L1 for adding workers to spoke clusters, but L1 hosted cluster doesn't have any nodes and therefore cannot run the installer directly.

The architecture for multiple hubs is described in the following diagram

<img src="ai-on-zero-worker-hypershift-components.jpg" alt="diagram" width="80%" height="50%"/>

The hosted control plane (L1) is responsible for managing the CRs and CRDs of cluster creation (i.e. InfraEnv, AgentClusterInstall etc.), where as the actual pods of the service and the webhook admission are located in dedicated namespaces and managed by the hosting management cluster (L0).

The deployment and cluster provisioning on such a setup is described in this [POC](https://github.com/javipolo/openshift-assisted-installer-tests/tree/main/ai-in-zero-worker-hypershift).

## Motivation
### Goals
- Automate the deployment of assisted service on zero-node hypershift clusters using the assisted infrastructure operator
- Allow the infra structure operator to install multiple instances of the service (each in its own namespace)
- Isolation: Each instance of assisted service controls the spoke cluster creation on its corresponding hypershift-based hub cluster
- Backward Compatiability: Support automatic update of the infrastructure operator and preserve the current behavior on non-hypershift installations

## Proposal
### Installation flow (user's perspective)
1. The user installs the assisted insfrastructure operator
2. The user creates a namespace on the management cluster
3. The user creates a CR in that namespace and provides the target HyperShift kubeconfig
4. The system deploys the sub-components required for provisioning clusters
5. The user creates the spoke cluster's deployment-related CRs (i.e. infraenv, aci) in the hyperhisft cluster
6. The spoke (hosted) cluster is generated

### Implementation Details
### Deployment of AI components with hostedCluster (L1) and managementCluster (L0)
In order to support the zero worker hyperhishift setup, we require some modification to the assisted-service server and the infrastructure operator that deploys and configures it. The main challenges in the transition from a single hub installed on a physical cluster to multiple hubs on a hypershift platform are:

1) Deployment of assisted-service and its subsystems is now done per namespace
2) Assisted-service controllers (i.e. infra-env controller etc.) should now watch resources using L1's api-server
3) Infrastructure operator should deploy and govern artifacts on both L1 and L0
4) Webhook deployment is spanned over 2 api-servers in the hypershift mode and is now much more elaborated to deploy

### Running in multiple namespaces
Currently, the infrastructure operator is defined as a cluster scoped resource, and the namespace in which it is deployed (usually assisted-installer) is hard wired in the operator's code. Several approaches were considered for the solution:

<b><u>Alternatives:</b></u>

1. <b>AgentServiceConfig CRD shall be converted to <i>Namespaced</i> scope (Rejected)</b>:
Reusing AgentServiceConfig while converting its scope was rejected because the operator can not update existing CRs (scope is an immutable field)<br>
2. <b>Deploy multiple AgentServiceConfig CRs on a cluster scope.</b>:
The target namespace will be part of the Spec. When the Spec does not contain the target namespace and other hypershift-related fields, then the deployment is assumed to behave as usual.
This option was rejected because it requires too much behavioral branching in the current implementation and requires taking care of edge cases such as detecting when the user deployed multiple CRDs in a non-hypershift environment and rejecting the such option. Also, having the namespace field in the Spec is a hack.

<b><u>Selected solution</b></u>
New CRD HypershiftServiceConfig on a namespace scope:
- Separation of intent
- Natural namespacing

### Assisted-service controllers working with hostedCluster (L1) control plane
From the user point of view, they define the cluster installation CRs on one of the namespaces in the hypershift cluster, and the assisted service picks them up and spins the cluster. To achieve that, we need to:
1. Have all cluster-related CRDs defined on L1 
2. Assisted service's controllers should work with L1 api-server (the hosted cluster) exclusively, even though the pods on which the controllers are running are hosted on L0 (the management cluster).

#### CRD deployment
The infrastructure operator obtains the list of CRDs according to their `operators.coreos.com/assisted-service-operator.<namespace>` label. Then, it configures them against the L1 control plane. 

Since these CRDs were placed on the management cluster (L0) by the OLM, we are ensuring compatibility with the non-hypershift installation.
### HypershiftAgentServiceConfig controller (Infrastructure operator)
#### Client configuration
The controller shall register itself to watch and manage resources with both L0 and L1 api-servers and shall instantiate 2 clients, one for each control plane.

For the assisted-service, The `KUBECONFIG` env variable points to a mounted secret containing L1's kubeconfig. This secret shall be defined by ACM. The name of the mounted secret is specified by the `KubeconfigSecretRef` field in `HypershiftAgentServiceConfig` CR.

Open Issue: How and where is that secret provisioned and become known to us (AI/ACM integration issue)

#### Separating L0 and L1 resources
The resources to watch will be divided into L0 and L1 resources. 

The infrastructure controller then uses the appropriate client to provision and update the necessary configuration.

### RBAC
On L1 we need the following permissions:
- To support admission webhook configuration, add a ClusterRole to access validating/mutating webhook configurations, subjectaccessreviews
- To support the service's controllers, add a ClusterRole to access secrets, routes, nodes, pods, endpoints, services, etc.

This is a mirroring of the RBAC state on L0 in non-hypershift setup.

On L0 we need a few permission that previously were defined by the OLM to allow the service to operate:
- To support the service's leader election mechanism a Role to access leases and configmaps.

### Webhooks
[Admission webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
 are HTTP callbacks that receive admission requests and do something with them. You can define two types of admission webhooks, validating admission webhook and mutating admission webhook. To register admission webhooks, create MutatingWebhookConfiguration or ValidatingWebhookConfiguration API objects.
These configuration objects need to be registered with L1 api-server.

The admission logic is backed by an admission webhook server which serves as an extension to the api server. The pods that host the webhook server are deployed on the management cluster (L0).

In the hypershift case, the routing between the api server on the hosted plane to the admission server on the management contorl plane is done via konnectivity agent and client and an endpoint that routes the communication from the api server to the konnectivity layer and then to the admission hook as described in the following diagram:

<img src="webhook_in_hypershift.jpg" alt="diagram" width="50%" height="30%"/>


Provisioning steps:
1. On L0: Deploy a service that exposes the webhook admission server
2. On L1: Define a headless service with endpoint that points to the L0 service's ClusterIP. Specify the L1 service as the connection point to the webhook on the webhook configuration
3. on L0: add konnectivity agent with the ClusterIP as ipv4 parameter
4. on L0: add the L1 kubeconfig as parameter to the admission server deployment


RBAC definition for the webhook need now be defined on both L0 and L1 (where in the central implementation they were defined only on L0)

## Implementation Details
### HypershiftAgentClusterConfig
```go
type HypershiftAgentServiceConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HypershiftAgentServiceConfigSpec   `json:"spec,omitempty"`
	Status HypershiftAgentServiceConfigStatus `json:"status,omitempty"`
}

type HypershiftAgentServiceConfigSpec struct {
	AgentServiceConfigSpec `json:",inline"`

	// KubeconfigSecretRef is a reference to the secret containing the kubeconfig for the destination Hypershift instance.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Hypershift kubeconfig reference"
	KubeconfigSecretRef corev1.LocalObjectReference `json:"kubeconfigSecretRef"`
}

type HypershiftAgentServiceConfigStatus struct {
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`
}
```