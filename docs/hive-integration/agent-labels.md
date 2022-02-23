# Agent Inventory Labels

The operator automatically adds labels to each Agent CR based on its inventory.  The goal of these labels is to allow for easily filtering Agents based on these properties.

These labels describe the Agent inventory and not any higher-level usability.  For example, there are labels indicating that virtualization is enabled on the CPU (VMX or SVM CPU flags), but no label indicating that the host is suitable for running CNV.

An annotation on the Agent CR indicates a version for the labels, so clients can anticipate what labels will be applied ("feature.agent-install.openshift.io/version").

## Label list
Labels marked as boolean will have either the string "true" or "false".

### v0.1
* feature.agent-install.openshift.io/storage-hasnonrotationaldisk (boolean): Indicates if the Agent has at least one SSD
* feature.agent-install.openshift.io/cpu-architecture (string): The CPU architecture (e.g., x86_64, arm64)
* feature.agent-install.openshift.io/cpu-virtenabled (boolean): Indicates if the CPU has the virtualization flag (VMX or SVM)
* feature.agent-install.openshift.io/host-manufacturer (string): The host's manufacturer
* feature.agent-install.openshift.io/host-productname (string): The host's product name
* feature.agent-install.openshift.io/host-isvirtual (boolean): Indicates if the host is a virtual machine