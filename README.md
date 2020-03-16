# bm-inventory

TODO: eventually this repo will work with build containers for now using the host. So for now the requiremenst to build and test this project are:
1. docker
2. golang
3. minikube (for tests)
4. kubectl


## First Setup

To push your build target to a docker registry you first need to change the default target, replace `SERVICE` variable in the makefile to your docker registry

## Build

`make all` will only

### Generate code after swagger changes

After every change in the api (swagger.yaml) the code should be generated and the build must pass

`make generate-from-swagger`

## Tests
Pre-configuration
1. Make sure to change the image in `deploy/bm-inventory.yaml` to your docker registry (the same one from the first setup instructions).
2. Run minikube on your system.
3. Deploy service, DB and other requirements `make deploy-all`
4. Wait for all the pods to be up.

Running the tests:

`make subsystem-run`

### Update service for the subsystem tests

if you are making changes and don't want to deploy everything once again you can simple run this command

`make update && kubectl get pod -o name | grep bm-inventory | xargs kubectl delete`

if will build and push a new image of the service to your docker registry, then delete the service pod from minikube, the deployment will handle the update and pull the new image to start the service again.
