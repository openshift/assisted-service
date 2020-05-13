[![Actions Status](https://github.com/filanov/bm-inventory/workflows/unit-test/badge.svg)](https://github.com/filanov/bm-inventory/actions)
# bm-inventory

1. docker
2. skipper https://github.com/stratoscale/skipper
3. minikube (for tests)
4. kubectl


## First Setup

To push your build target to a docker registry you first need to change the default target, export `SERVICE` environment variable to your docker registry

```shell script
export SERVICE=<registry>/<image-name>:<tag>
```

For the first build of the build container run:
`skipper build bm-inventory-build`

## Build

`skipper make all`

### Generate code after swagger changes

After every change in the api (swagger.yaml) the code should be generated and the build must pass

`skipper make generate-from-swagger`

## Tests
Pre-configuration
1. Run minikube on your system.
2. Deploy service, DB and other requirements `skipper make deploy-all`
3. Wait for all the pods to be up.

Running the tests:

`skipper make subsystem-run`

### Update service for the subsystem tests

if you are making changes and don't want to deploy everything once again you can simple run this command

`skipper make update && kubectl get pod -o name | grep bm-inventory | xargs kubectl delete`

if will build and push a new image of the service to your docker registry, then delete the service pod from minikube, the deployment will handle the update and pull the new image to start the service again.

## Deployment

The deployment is a system deployment, it contains all the components the service need for all the operations to work (if implemented).
S3 service (scality), DB and will use the image generator to create the images in the deployed S3 and create relevant bucket in S3.

`skipper make deploy-all`

##  Linked repositories 
* #### coreos_installation_iso:
    https://github.com/oshercc/coreos_installation_iso 

    Image in charge of generating the Fedora-coreOs image used to install the host with the relevant ignition file
    
    Image is uploaded to deployed S3 under the name template "installer-image-<cluster-id>"
* #### ignition manifests and kubeconfig generate:
    
    https://github.com/oshercc/ignition-manifests-and-kubeconfig-generate
    
    Image in charge of generating the fallowing installation files:
    * kubeconfig
    * bootstrap.ign
    * master.ign
    * worker.ign
    * metadata.json
    * kubeadmin-password
    
   Files are uploaded to deployed S3 under the name template  "<cluster-id>/<filenae>"
