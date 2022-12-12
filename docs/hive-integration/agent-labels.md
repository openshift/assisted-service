# Agent Inventory Labels

The operator automatically adds labels to each Agent CR based on its inventory. The goal of these labels is to allow for easily filtering Agents based on these properties.

These labels describe the Agent inventory and not any higher-level usability. For example, there are labels indicating that virtualization is enabled on the CPU (VMX or SVM CPU flags), but no label indicating that the host is suitable for running CNV.

An annotation on the Agent CR indicates a version for the labels, so clients can anticipate what labels will be applied ("feature.agent-install.openshift.io/version").

## Label list

Labels marked as boolean will have either the string "true" or "false".

### v0.1

- inventory.agent-install.openshift.io/storage-hasnonrotationaldisk (boolean): Indicates if the Agent has at least one SSD
- inventory.agent-install.openshift.io/cpu-architecture (string): The CPU architecture (e.g., x86_64, arm64)
- inventory.agent-install.openshift.io/cpu-virtenabled (boolean): Indicates if the CPU has the virtualization flag (VMX or SVM)
- inventory.agent-install.openshift.io/host-manufacturer (string): The host's manufacturer
- inventory.agent-install.openshift.io/host-productname (string): The host's product name
- inventory.agent-install.openshift.io/host-isvirtual (boolean): Indicates if the host is a virtual machine

# Agent Classification labels

The AgentClassification CRD defines the API for users to classify Agents by providing a query that is run on the Agent's inventory along with a corresponding label.
The query format is defined by the [gojq](https://github.com/itchyny/gojq) library, which supports jq queries in Go.
Any Agent in the same namespace as the AgentClassification whose inventory causes the specified query to evaluate to `true` will have the specified label applied (prefixed by "agentclassification.agent-install.openshift.io/").

Some examples include:

```
spec:
  labelKey: size
  labelValue: medium
  query: ".cpu.count == 2 and .memory.physicalBytes >= 4294967296 and .memory.physicalBytes < 8589934592"
```

```
spec:
  labelKey: storage
  labelValue: large
  query: "[.disks[] | select(.sizeBytes > 1073741824000)] | length > 5"
```

The AgentClassification CRD has the following information in its Status:

- MatchedCount: shows how many Agents currently match the classification
- ErrorCount: shows how many Agents encountered errors when matching the classification
- Conditions:
  - QueryErrors: true if there were errors when processing the query

Notes:

1. The labelKey and labelValue properties are immutable.
1. If an AgentClassification is deleted, the specified label will first be removed from all Agents.
