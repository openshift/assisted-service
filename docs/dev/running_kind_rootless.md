# Run kind with rootless user

To enable Running kind with non-root user you'll need to create a podman socket for the local user and point kind to user it instead of the default

## How to deploy

### create podman socket for user

```bash
echo "Cleaning up old socket"
sudo systemctl disable --now podman.socket
sudo rm -rf /run/user/${UID}/podman /run/podman
 
echo "Starting New podman socket"
sudo systemctl enable --now podman.socket
systemctl --user enable --now podman.socket
loginctl enable-linger $USER
 
sudo systemctl restart podman.socket
systemctl --user restart  podman.socket
```

### Validate the socket is created
```bash
podman -r info --format json  |  jq .host.remoteSocket
```

output should look like this
```json
{
  "path": "/run/user/1000/podman/podman.sock",
  "exists": true
}
```

### Running kind with non-root user

```bash
export DOCKER_HOST=${XDG_RUNTIME_DIR}/podman/podman.sock  # Setting user podman socket
export KIND_EXPERIMENTAL_PROVIDER=podman                  # enabling podman driver
 
systemd-run --scope --user kind create
```



## Known issue
on some fedora distribution, there is an issue where kind fails to create port forwarding rule.
with the following  error message 
```console
MARK: bad value for option "--set-mark"
```

as a workaround, we can set the firewall driver as `nftables`

```bash
sudo su -
CONF_DIR="/etc/containers/containers.conf.d"
 
sudo mkdir -p ${CONF_DIR}
tee << EOF > "${CONF_DIR}/50-fix-kind-netavark-nftables.conf"
[network]
firewall_driver="nftables"
EOF

# this setting requires reboot
```


