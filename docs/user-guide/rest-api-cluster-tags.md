# REST-API - Cluster Tags

The `tags` property in cluster object is a comma-separated list of tags that are associated to the cluster. Each tag is a free-text string that can consist of the following characters:
Alphanumeric (aA-zZ, 0-9), underscore (\_) and white-spaces.

Tags are not read or manipulated by the system, so they can be used for marking clusters with some filtering criteria, installation tracking indication, etc.

## Usage

- The property can be specified when creating (v2RegisterCluster) or updating (V2UpdateCluster) a cluster.
- The tags are stored in the cluster object, so the property can be fetched when getting (v2GetCluster) the cluster.
- The property can be cleared by specifying an empty string.

## Examples

### Create tags (using v2RegisterCluster)

```bash
cat register_cluster.json
{
    "name":"test",
    "pull_secret":"<pull_secret>",
    "openshift_version":"4.10",
    "tags":"tag1,tag_2,tag 3"
}
```

```bash
curl -X POST -H "Content-Type: application/json" -d @register_cluster.json \
    <HOST>:<PORT>/api/assisted-install/v2/clusters
```

### Update tags (using V2UpdateCluster)

```bash
cat update_cluster.json
{
    "tags":"tag1,tag_2,tag 3,tag4"
}
```

```bash
curl -X PATCH -H "Content-Type: application/json" -d @update_cluster.json \
    <HOST>:<PORT>/api/assisted-install/v2/clusters/<cluster_id>
```

### Get tags (using v2GetCluster)

```bash
curl <HOST>:<PORT>/api/assisted-install/v2/clusters/<cluster_id> | jq '.tags'

output: "tag1,tag_2,tag 3,tag4"
```
