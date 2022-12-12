# Hardware requirements

Hardware requirements are configured with `HW_VALIDATOR_REQUIREMENTS` environment variable, which must contain JSON mapping OpenShift version to specific master and worker hardware requirements.
For example:

```json
[
  {
    "version": "default",
    "master": {
      "cpu_cores": 4,
      "ram_mib": 16384,
      "disk_size_gb": 100,
      "installation_disk_speed_threshold_ms": 10,
      "network_latency_threshold_ms": 100,
      "packet_loss_percentage": 0
    },
    "worker": {
      "cpu_cores": 2,
      "ram_mib": 8192,
      "disk_size_gb": 100,
      "installation_disk_speed_threshold_ms": 10,
      "network_latency_threshold_ms": 1000,
      "packet_loss_percentage": 10
    },
    "sno": {
      "cpu_cores": 8,
      "ram_mib": 16384,
      "disk_size_gb": 100,
      "installation_disk_speed_threshold_ms": 10
    }
  },
  {
    "version": "x.y.z",
    "master": {
      "cpu_cores": 8,
      "ram_mib": 32768,
      "disk_size_gb": 150,
      "installation_disk_speed_threshold_ms": 10,
      "network_latency_threshold_ms": 100,
      "packet_loss_percentage": 0
    },
    "worker": {
      "cpu_cores": 4,
      "ram_mib": 16384,
      "disk_size_gb": 150,
      "installation_disk_speed_threshold_ms": 10,
      "network_latency_threshold_ms": 1000,
      "packet_loss_percentage": 10
    },
    "sno": {
      "cpu_cores": 8,
      "ram_mib": 16384,
      "disk_size_gb": 120,
      "installation_disk_speed_threshold_ms": 10
    }
  }
]
```

`default` requirements are used if version can't be found.

If any overrides are needed, they have to be done in that JSON. For example:

Changing disk size requirement in all versions in shell with `jq`:

```shell
HW_VALIDATOR_REQUIREMENTS=$(echo $HW_VALIDATOR_REQUIREMENTS | jq '(.[].worker.disk_size_gb, .[].master.disk_size_gb) |= 20' | tr -d "\n\t ")

```
