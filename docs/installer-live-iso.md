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
export PULL_SECRET_BASE64=$(base64 -w 0 auth.json) 

sed -i 's#replace-with-your-ssh-public-key#'"${SSH_PUBLIC_KEY}"'#' onprem-iso-config.ign
sed -i 's#replace-with-your-base64-encoded-pull-secret#'"${PULL_SECRET_BASE64}"'#' onprem-iso-config.ign
````

### Download the base RHCOS live ISO

The base live ISO is extracted from a container image. Run the container
image containing the ISO, copy the ISO, and then stop and remove the container. 

````
podman run -dt --name livecdsrc quay.io/ocpmetal/livecd-iso:rhcos-livecd
podman cp livecdsrc:/root/image/livecd.iso ./livecd.iso
podman rm -f livecdsrc
````

### Create the assisted-service live ISO

Finally, use the ignition config (onprem-iso-config.ign) and the base live ISO (livecd.iso) to
create the assisted-service live ISO.

````
podman run --rm --privileged  -v /dev:/dev -v /run/udev:/run/udev -v .:/data  quay.io/coreos/coreos-installer:release iso embed -c /data/onprem-iso-config.ign -o /data/assisted-service.iso /data/livecd.iso
````

The live ISO, **assisted-service.iso** (not livecd.iso), can then be used to deploy the installer. The live ISO storage system is emphemeral and its size depends on the amount of memory installed on the host. A minimum of 10GB of memory is required to deploy the installer, generate a single discovery ISO, and install an OCP cluster.

After the live ISO boots, the UI should be accessible from the a browser at

````
http://<hostname-or-ip>:8080. 
````

It may take a couple of minutes for the assisted-service and UI to become ready after you see the login prompt.

## How to debug

Login to the host using your ssh public key.

The assisted-service components are deployed as systemd services.
* assisted-service-installer.service
* assisted-service-db.service
* assisted-service-s3.service
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

The transpiled ignition config is version 3.0.0. The RHCOS live ISO we are currently using is able to read v2.2.0. To change the ignition file to v2.2.0:

* Change version: from 3.0.0 to 2.2.0
* For each file in the storage -> files section add 

````
"filesystem": "root",
````

These two edits will not be necessary once we move to a RHCOS image that supports
ignition v3.0.0.

