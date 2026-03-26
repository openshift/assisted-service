# Hardware requirements

Hardware requirements are configured with the `HW_VALIDATOR_REQUIREMENTS` environment variable, which must contain a JSON array of requirement entries.

## Entry types

There are two types of entries:

### `version` — exact match

An entry with a `"version"` key applies to that exact OCP version, or acts as the catch-all `"default"` when no other entry matches. All roles (`master`, `worker`, `sno`, etc.) and their required fields must be fully specified. `arbiter` and `edge-worker` are optional and fall back to `worker` if omitted.

### `min_version` — range match

An entry with a `"min_version"` key applies to that OCP version **and all later versions**. Only the fields that differ from `"default"` need to be specified — any omitted roles or fields are inherited from the `"default"` entry. A `"default"` entry is required when `min_version` entries are present.

## Lookup order

For a given OCP version, requirements are resolved in the following order:

1. **Exact `version` match** — returned as-is
2. **Highest `min_version` ≤ requested version** — missing fields and roles inherited from `"default"`
3. **`"default"`** — returned as-is

## Example

```json
[{
  "version": "default",
  "master": {
    "cpu_cores": 4,
    "ram_mib": 16384,
    "disk_size_gb": 100,
    "installation_disk_speed_threshold_ms": 10,
    "network_latency_threshold_ms": 100,
    "packet_loss_percentage":0
  },
  "worker": {
    "cpu_cores": 2,
    "ram_mib": 8192,
    "disk_size_gb": 100,
    "installation_disk_speed_threshold_ms": 10,
    "network_latency_threshold_ms": 1000,
    "packet_loss_percentage":10
  },
  "sno": {
    "cpu_cores": 8,
    "ram_mib": 16384,
    "disk_size_gb": 100,
    "installation_disk_speed_threshold_ms": 10
  }
},
{
  "min_version": "4.22",
  "sno": {
    "cpu_cores": 4
  }
},
{
  "version": "x.y.z",
  "master": {
    "cpu_cores": 8,
    "ram_mib": 32768,
    "disk_size_gb": 150,
    "installation_disk_speed_threshold_ms": 10,
    "network_latency_threshold_ms":100,
    "packet_loss_percentage":0
  },
  "worker": {
    "cpu_cores": 4,
    "ram_mib": 16384,
    "disk_size_gb": 150,
    "installation_disk_speed_threshold_ms": 10,
    "network_latency_threshold_ms":1000,
    "packet_loss_percentage":10
  },
  "sno": {
    "cpu_cores": 8,
    "ram_mib": 16384,
    "disk_size_gb": 120,
    "installation_disk_speed_threshold_ms": 10
  }
}]
```

`default` requirements are used if version can't be found. A `min_version` entry will also be applied if one matches the requested version, with any unspecified fields inherited from `default`.

If any overrides are needed, they have to be done in that JSON. For example:

Changing disk size requirement in all versions in shell with `jq`:
```shell
HW_VALIDATOR_REQUIREMENTS=$(echo $HW_VALIDATOR_REQUIREMENTS | jq '(.[].worker.disk_size_gb, .[].master.disk_size_gb) |= 20' | tr -d "\n\t ")

```
