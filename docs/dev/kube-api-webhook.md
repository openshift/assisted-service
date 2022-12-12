# Webhook validation

Read about admission webhooks [here](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/).

Webhook validation enable adding validations based on the state of the CRDs.

For example, changing the Spec of AgentClusterInstall during the installation is not permitted as these changes cannot be applied to the cluster installation configuration once it started.

## Implementation

The [generic-admission-server](https://github.com/openshift/generic-admission-server) library is used to run the webhooks as an [Kubernetes aggregated API server](https://github.com/kubernetes/apiserver).

The admission web hook is deployed with the (operator)[../operator.md].

The following components will be deployed:

- ValidatingWebhookConfiguration: The configuration defining on which CRD the webhook is called, on which operations, what is the URL path that will be called, etc...
- Deployment: The configuration of the pod running the HTTPS server accepting admission requests.
- Service and Service Account: Expose the HTTPS server.
- APIService: Register the above service as an aggregated API server.
- ClusterRole and ClusterRoleBinding: Assign needed permission to the Service.

In OpenShift deployment, the certificates are handled by the [OpenShift Service CA Operator](https://github.com/openshift/service-ca-operator) by annotations on the Service and APIService.

In order to check if the APIService is available, run:

```sh
# kubectl get apiservice v1.admission.agentinstall.openshift.io
NAME                                     SERVICE                                    AVAILABLE   AGE
v1.admission.agentinstall.openshift.io   assisted-installer/agentinstalladmission   True        22h
```

## Adding a new webhook

In order to add a new webhook, the following steps are needed:

- Add a new `ValidatingWebhookConfiguration` yaml in the deploy [dir](../../deploy/webhooks/) with the required CRD resource, group, version and define the URL path. Also add it to the web hook deploy [script](../../tools/deploy_webhooks.py).
- Create an admission hook: see example for [ACI](../../pkg/webhooks/hiveextension/v1beta1/agentclusterinstall_admission_hook.go). The needed functions are:
  - `Validate(admissionSpec *admissionv1.AdmissionRequest) *admissionv1.AdmissionResponse`: Handle AdmissionRequests
  - `ValidatingResource() (plural schema.GroupVersionResource, singular string)`: Declare the CRD this hook wants to handle
- Add the new hook to the Admission Server [main](../../cmd/webadmission/main.go).
