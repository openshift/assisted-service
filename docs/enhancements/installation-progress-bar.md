# Cluster installation progress bar

## Goals

- Progress should be computed in the BE by design and not in the UI
  - that will also make it usable from other APIs as kube-api
- Installed clusters with failed non-essential workers or OLM-operators should still reach 100% progress.

## Current implementation

- Computed in UI
- `progressPercent = hostsProgressPercent * 0.75 + operatorsProgressPercent * 0.25`
  - `operatorsProgressPercent =(completedOperators / monitoredOperators) * 100`
    - A completedOperator is one of the following:
      - A built-in operator with `available` status
      - An OLM operator with `available` or `failed` status
  - `hostsProgressPercent = (ΣcompletedStages / ΣallStages) * 100`
    - For each host the data is retrieved by using the following APIs

```
GET api/assisted-install/v1/clusters/{cluster_id} | jq '.hosts[0].progress'
{
  "current_stage": "Done",
  "stage_started_at": "2021-05-31T13:37:26.224Z",
  "stage_updated_at": "2021-05-31T13:37:26.224Z"
}

GET /api/assisted-install/v1/clusters/{cluster_id} | jq '.hosts[0].progress_stages'
[
  "Starting installation",
  "Installing",
  "Writing image to disk",
  "Waiting for control plane",
  "Rebooting",
  "Configuring",
  "Joined",
  "Done"
]
```

## New implementation

- Will be computed in BE
- `progres =ΣclusterStageProgress * Wi = preparingForInstallation * Wpi + installing * Wi + finalizing * Wf` while `ΣWi=1`
  - We can manually compute an avg duration for the previous installations based on events to choose those weights and hardcode them.

#### Preparing for installation

This stage mostly generates manifests and installation configs, therefore, we will set `preparingForInstallationProgress={1, 0}` depending on if it is done or not.

#### Installing

- This stage refers to hosts' installation
- We will “assume” all host’s stages have a similar duration, therfore, they will be equally weighted.
- Hosts' progresses will be computed the same way we it is done in the current implementation, aka `hostsProgressPercent = ΣcompletedStages / ΣallStages`

#### Finalizing

- This stage refers to operators' installation
- We will “assume” all operatos have a similar installation duration.
- Operators' progresses will be computed the same way we it is done in the current implementation, aka `operatorsProgressPercent = completedOperators / monitoredOperators`

Suggested APIs:

- Note that those new percentage values should be reset on clusters' installation reset

```
diff --git a/swagger.yaml b/swagger.yaml
index d53ddf59..ddbc7e9b 100644
--- a/swagger.yaml
+++ b/swagger.yaml
@@ -4171,6 +4171,9 @@ definitions:
         format: date-time
         x-go-custom-tag: gorm:"type:timestamp with time zone"
         description: The last time that the cluster status was updated.
+      progress:
+        type: object
+        ref: '#/definitions/cluster-progress-info'
       hosts:
         x-go-custom-tag: gorm:"foreignkey:ClusterID;association_foreignkey:ID"
         type: array
@@ -4458,8 +4461,12 @@ definitions:
   host-progress-info:
     type: object
     required:
+      - installation_percentage
       - current_stage
     properties:
+      installation_percentage:
+        type: integer
       current_stage:
         type: string
         $ref: '#/definitions/host-stage'
@@ -4477,6 +4484,21 @@ definitions:
         x-go-custom-tag: gorm:"type:timestamp with time zone"
         description: Time at which the current progress stage was last updated.

+  cluster-progress-info:
+    type: object
+    required:
+      - total_percentage
+    properties:
+      total_percentage:
+        type: integer
+      preparing_for_installation_stage_percentage:
+        type: integer
+      installing_stage_percentage:
+        type: integer
+      finalizing_stage_percentage:
+        type: integer
+
   host-stage:
     type: string
     enum:
```

```
GET /api/assisted-install/v1/clusters/{cluster_id}
{
  "id": "3a745598-dde4-46dd-bdb1-ce7e1dea119b",
  "progress":                                    <-------- NEW
  {
    "total_percentage": 21,
    "preparing_for_installation_stage_percentage": 100,
    "installing_stage_percentage": 38,
    "finalizing_stage_percentage": 0,
  }
  "hosts": [
    {
      "kind": "Host",
      "id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
      "role": "master",
      "progress": {
        "installation_percentage": 15,                  <-------- NEW (but coming from hosts API)
        "current_stage": "Starting installation",
      },
    },
    {
      "kind": "Host",
      "id": "3fa85f64-5717-4562-b3fc-2c963f66afa7",
      "role": "worker",
      "progress": {
        "installation_percentage": 13,                  <-------- NEW (but coming from hosts API)
        "current_stage": "Starting installation",
      },
    },
  ]
}
```

```
GET /api/assisted-install/v1/clusters/{cluster_id}/hosts/{host_id}
{
  "id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
  "progress": {
    "installation_percentage": 15,                  <-------- NEW
    "current_stage": "Starting installation",
    "progress_info": "string",
  },
}
```
