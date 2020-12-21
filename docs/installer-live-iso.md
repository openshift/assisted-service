# assisted-service Live ISO

The assisted-service can be deployed using a live ISO. The live ISO deploys the assisted-service
using containers on RHCOS. The assisted-service live ISO is a RHCOS live ISO that is customized with an ignition config file.

## How to create an assisted-service live ISO

### Create the ignition config

A ignition config that deploys the assisted-service is available at 
https://raw.githubusercontent.com/openshift/assisted-service/master/config/onprem-iso-config.ign.

Download this ignition config and modify it to include your ssh public key and your registry.redhat.io pull secret. The example below assumes your pull secret is saved into a file called auth.json.

````
wget https://raw.githubusercontent.com/openshift/assisted-service/master/config/onprem-iso-config.ign

export SSH_PUBLIC_KEY=$(cat ~/.ssh/id_rsa.pub)
export PULL_SECRET_ENCODED=$(export PULL_SECRET=$(cat auth.json); urlencode $PULL_SECRET)

sed -i 's#replace-with-your-ssh-public-key#'"${SSH_PUBLIC_KEY}"'#' onprem-iso-config.ign
sed -i 's#replace-with-your-urlencoded-pull-secret#'"${PULL_SECRET_ENCODED}"'#' onprem-iso-config.ign
````

Currently, the upstream assisted-service container image cannot be used with the live ISO. You will 
need to build a custom container image and push it to quay.io.

````
export SERVICE=quay.io/<your-org>/assisted-service:latest
make build
docker push ${SERVICE}
````

Then update the ignition config file to use your assisted-service container image.

````
sed -i 's#quay.io/ocpmetal/assisted-service:latest#'"${SERVICE}"'#' onprem-iso-config.ign
````

### Download the base RHCOS live ISO

````
wget https://mirror.openshift.com/pub/openshift-v4/dependencies/rhcos/4.6/latest/rhcos-live.x86_64.iso
````

### Create the assisted-service live ISO

Finally, use the ignition config (onprem-iso-config.ign) and the base live ISO (rhcos-live.x86_64.iso) to
create the assisted-service live ISO.

````
podman run --rm --privileged  -v /dev:/dev -v /run/udev:/run/udev -v .:/data  \
  quay.io/coreos/coreos-installer:release iso ignition embed -i /data/onprem-iso-config.ign -o /data/assisted-service.iso /data/rhcos-live.x86_64.iso
````

The live ISO, **assisted-service.iso** (not rhcos-live.x86_64.iso), can then be used to deploy the installer. The live ISO storage system is emphemeral and its size depends on the amount of memory installed on the host. A minimum of 10GB of memory is required to deploy the installer, generate a single discovery ISO, and install an OCP cluster.

After the live ISO boots, the UI should be accessible from the browser at

````
https://<hostname-or-ip>:8443.
````

It may take a couple of minutes for the assisted-service and UI to become ready after you see the login prompt.

## How to debug

Login to the host using your ssh private key.

The assisted-service components are deployed as systemd services.
* assisted-service-installer.service
* assisted-service-db.service
* assisted-service-ui.service

Verify that the containers deploy by those services are running.

````
sudo podman ps -a
````

Examine the assisted-service-installer.service logs:

````
sudo journalctl -f -u assisted-service-installer.service
````

The environment file used to deploy the assisted-service is located at /etc/assisted-service/environment.

Pull secrets are saved to a file located at /etc/assisted-service/auth.json.

## How to use the FCC file to generate the base ignition config file

The ignition file is created using a predefined Fedore CoreOS Config (FCC) file provided in /config/onprem-iso-fcc.yaml. FCC files are easier to read and edit than the machine readable ignition files.

The FCC file transpiles to an ignition config using:

````
podman run --rm -v ./config/onprem-iso-fcc.yaml:/config.fcc:z quay.io/coreos/fcct:release --pretty --strict /config.fcc > onprem-iso-config.ign
````

There is also a make target that you can use, which wraps the above command to generate the ignition file:

````
make generate-onprem-iso-ignition
````

