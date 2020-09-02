# How to deploy OAS on OpenShift

Besides default minikube deployment, the service support deployment to OpenShift cluster using ingress as the access point to the service.

```shell
make deploy-all TARGET=oc-ingress
```

This deployment option have multiple optional parameters that should be used in case you are not the Admin of the cluster:
1. `APPLY_NAMESPACE` - True by default. Will try to deploy "assisted-installer" namespace, if you are not the Admin of the cluster or maybe you don't have permissions for this operation you may skip namespace deployment.
1. `INGRESS_DOMAIN` - By default deployment script will try to get the domain prefix from OpenShift ingress controller. If you don't have access to it then you may specify the domain yourself. For example: `apps.ocp.prod.psi.redhat.com`

To set the parameters simply add them in the end of the command, for example

```shell
make deploy-all TARGET=oc-ingress APPLY_NAMESPACE=False INGRESS_DOMAIN=apps.ocp.prod.psi.redhat.com
```

_**Note**: All deployment configurations are under the `deploy` directory in case more detailed configuration is required._


## Deploying the UI

This service support optional UI deployment.

```shell
make deploy-ui TARGET=oc-ingress
```

## Deploy Monitoring 

This will allow you to deploy Prometheus and Grafana already integrated with Assisted installer:

```shell
# Step by step
make deploy-prometheus TARGET=oc-ingress APPLY_NAMESPACE=false
make deploy-grafana TARGET=oc-ingress APPLY_NAMESPACE=false

# Or just all-in
make deploy-monitoring TARGET=oc-ingress APPLY_NAMESPACE=false
```
