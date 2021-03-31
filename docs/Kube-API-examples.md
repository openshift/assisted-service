#Assisted Installer Kube API CR examples

[docs/crds](https://github.com/openshift/assisted-service/tree/master/docs/crds) stores working examples of various resources we spawn via kube-api in assisted-installer, for Hive integration.
Those examples are here for reference.

You will likely need to adapt those for your own needs.

* [InstalllEnv](crds/installEnv.yaml)
* [NMState Config](crds/nmstate.yaml)
* [Hive PullSecret Secret](crds/pullsecret.yaml)
* [Hive ClusterDeployment](crds/clusterDeployment.yaml)
* [Hive ClusterDeployment-SNO](crds/clusterDeployment-SNO.yaml)



###Creating InstallConfig overrides

In order to alter the default install config yaml used when running `openshift-install create` commands.
More information about install-config overrides is available [here](user/guide/install-customization.md#Install-Config)
In case of failure to apply the overrides the clusterdeployment conditions will reflect the error and show the relevant error message. 

Add an annotation with the desired options, the clusterdeployment controller will update the install config yaml with the annotation value.
Note that this configuration must be applied prior to starting the installation
```
$kubectl annotate clusterdeployments.hive.openshift.io test-cluster -n assisted-installer adi.io.my.domain/install-config-overrides="{\"controlPlane\":{\"hyperthreading\":\"Disabled\"}}"
clusterdeployment.hive.openshift.io/test-cluster annotated

$kubectl get clusterdeployments.hive.openshift.io test-cluster -n assisted-installer -o yaml
apiVersion: hive.openshift.io/v1
kind: ClusterDeployment
metadata:
  annotations:
    adi.io.my.domain/install-config-overrides: '{"controlPlane":{"hyperthreading":"Disabled"}}'
  creationTimestamp: "2021-04-01T07:04:49Z"
  generation: 1
  name: test-cluster
  namespace: assisted-installer
  resourceVersion: "183201"
  selfLink: /apis/hive.openshift.io/v1/namespaces/assisted-installer/clusterdeployments/test-cluster
  uid: 25769614-52db-448d-8366-05cb38c776fa
spec:
```