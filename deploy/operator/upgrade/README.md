# Assisted Service Operator Upgrade
Below are the instructions to upgrade the Assisted Installer operator a.k.a. Infrastructure operator from version ocm-2.3 to ocm-2.4 on an IPV4 connected OCP hub cluster. Operator upgrade includes below main actions:

* Installation of IPV4 OCP hub cluster using dev-scripts.
* Infrastructure operator installation workflow, including installation of Local Storage Operator and Hive Operator.
* Installation of ocm-2.3 spoke cluster using ZTP workflow.
* Upgrade the Infrastructure operator through OLM to ocm-2.4 version. Verify if the previously installed ocm-2.3 spoke cluster is still accessible after the upgrade.
* Installation of ocm-2.4 spoke cluster using ZTP workflow.

## Installation of IPV4 OCP hub cluster using dev-scripts.
In order to have a workable setup, you can use dev-scripts with the configurations mentioned [here](https://github.com/openshift/assisted-service/tree/master/deploy/operator#dependencies)

## Infrastructure operator installation workflow, including installation of Local Storage Operator and Hive Operator.

```
# replace with path in your system for any eligible cluster auth:
export KUBECONFIG=/home/test/dev-scripts/ocp/ostest/auth/kubeconfig

cd deploy/operator/
source upgrade/before_upgrade.sh
./deploy.sh
```

## Installation of ocm-2.3 spoke cluster using ZTP workflow.

```
# replace with your paths:
export ASSISTED_PULLSECRET_JSON=/home/test/dev-scripts/pull_secret.json
export EXTRA_BAREMETALHOSTS_FILE=/home/test/dev-scripts/ocp/ostest/extra_baremetalhosts.json
export IP_STACK=v4

cd deploy/operator/ztp/
./deploy_spoke_cluster.sh
```

## Upgrade Infrastructure operator
Upgrade infrastructure operator to ocm-2.4 (latest) version using OLM.

```
cd deploy/operator/upgrade
source upgrade.sh
```

## Installation of ocm-2.4 spoke cluster using ZTP workflow.

```
cd deploy/operator
source upgrade/after_upgrade.sh
./ztp/deploy_spoke_cluster.sh
```
