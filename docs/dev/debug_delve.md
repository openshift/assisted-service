
# assisted-service runs on minikube

## Debug Setup

To debug assisted-service directly from minikube container
Install minikube setup with assisted-service with one replica

```shell
export KUBECONFIG=source /root/.kube/config
kubectl patch deployment assisted-service -n assisted-installer --type json -p="[{\"op\": \"remove\", \"path\": \"/spec/template/spec/containers/0/livenessProbe\"}]"
kubectl patch deployment assisted-service -n assisted-installer --type json -p="[{\"op\": \"remove\", \"path\": \"/spec/template/spec/containers/0/readinessProbe\"}]"
oc rollout restart deployment -n assisted-installer assisted-service
```

Login to minikube node and attach to assisted-service container

```shell
oc rsh -n assisted-installer [pod/assisted-service-name] 
# install packages git goland and telnet
dnf install procps psmisc golang delve iproute iputils telnet net-tools git -y
```

Before running dlv attach make sure assisted-service sources exists

```shell
git clone https://github.com/openshift/assisted-service.git
# git checkout to the same minikube assisted-service version/tag
```
After dlv attach we can break by func name or filepath:line
Trigger event from UI, test-infra or others till hit on requested break

Example for dlv attach to pid 1 (assisted-service)

```shell
dlv attach 1 

(dlv) b /assisted-service/internal/bminventory/inventory.go:759
Breakpoint 1 set at 0x380418e for github.com/openshift/assisted-service/internal/bminventory.verifyMinimalOpenShiftVersionForSingleNode() ./internal/bminventory/inventory.go:759
(dlv) c

> github.com/openshift/assisted-service/internal/bminventory.verifyMinimalOpenShiftVersionForSingleNode() ./internal/bminventory/inventory.go:759 (hits goroutine(45458):1 total:1) (PC: 0x380418e)
Warning: debugging optimized function
Warning: listing may not match stale executable
   756:	
   757:	func verifyMinimalOpenShiftVersionForSingleNode(requestedOpenshiftVersion string) error {
   758:		ocpVersion, err := version.NewVersion(requestedOpenshiftVersion)
=> 759:		if err != nil {
   760:			return errors.Errorf("Failed to parse OCP version %s", requestedOpenshiftVersion)
   761:		}
   762:		minimalVersionForSno, err := version.NewVersion(minimalOpenShiftVersionForSingleNode)
   763:		if err != nil {
   764:			return errors.Errorf("Failed to parse minimal OCP version %s", minimalOpenShiftVersionForSingleNode)
   765:		}
   766:		if ocpVersion.LessThan(minimalVersionForSno) {
   767:			return errors.Errorf("Invalid OCP version (%s) for Single node, Single node OpenShift is supported for version 4.8 and above", requestedOpenshiftVersion)
   768:		}
   769:		return nil


```

When break you can continue step/next/print command (Examples)

```shell
(dlv) stack
 0  0x000000000380418e in github.com/openshift/assisted-service/internal/bminventory.verifyMinimalOpenShiftVersionForSingleNode
    at ./internal/bminventory/inventory.go:759
 1  0x00000000037ffe11 in github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).validateRegisterClusterInternalParams
    at ./internal/bminventory/inventory.go:399
 2  0x00000000038010f7 in github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).RegisterClusterInternal
    at ./internal/bminventory/inventory.go:541
 3  0x000000000383f619 in github.com/openshift/assisted-service/internal/bminventory.(*bareMetalInventory).V2RegisterCluster
    at ./internal/bminventory/inventory_v2_handlers.go:46

(dlv) locals
ocpVersion = ("*github.com/hashicorp/go-version.Version")(0xc0013e8cd0)
err = error nil


(dlv) p ocpVersion
("*github.com/hashicorp/go-version.Version")(0xc0013e8cd0)
*github.com/hashicorp/go-version.Version {
	metadata: "",
	pre: "",
	segments: []int64 len: 3, cap: 4, [4,15,0],
	si: 2,
	original: "4.15",}
(dlv) whatis ocpVersion
*github.com/hashicorp/go-version.Versio
```
