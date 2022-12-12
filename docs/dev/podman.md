# Podman socket enablement

## Clean socket if exists

```shell
systemctl disable --now podman.socket
rm -rf /run/user/${UID}/podman
rm -rf /run/podman
```

## Open/Enable the socket

```shell
systemctl enable --now podman.socket
systemctl --user enable --now podman.socket
loginctl enable-linger $USER
```

## Verify it

```shell
$ podman -r info | grep remoteSocket -A 2
  remoteSocket:
    exists: true
    path: /run/podman/podman.sock
```

## Enable minikube local registry

```shell
minikube addons enable registry
```

Expose the registry to the host machine on port 5000.
Can be done by xinet, port-forward or use [assisted-test-infra](https://github.com/openshift/assisted-test-infra) which automate the xinet method:

###xinet
Change registry service to a LoadBalancer (minikube tunnel will assign an external IP)

```shell
kubectl patch service registry -n kube-system --type json -p='[{"op": "replace", "path": "/spec/type", "value":"LoadBalancer"}]'
```

Create a new xinet configuration file at `/etc/xinetd.d/minikube-registry`:

```shell
{
type		= UNLISTED
socket_type	= stream
protocol	= tcp
user		= root
wait		= no
redirect	= EXTERNAL_IP EXTERNAL_PORT
port		= 5000
per_source	= UNLIMITED
instances	= UNLIMITED
}
```

Reboot the xinet service: `systemctl restart xinetd.service`

###port-forward

```shell
kubectl port-forward --namespace kube-system service/registry 5000:80
```

## Use local registry

```shell
export SUBSYSTEM_LOCAL_REGISTRY=localhost:5000
```
