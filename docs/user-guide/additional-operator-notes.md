# Additional OLM operator notes

## OpenShift Virtualization (CNV)

- When deploying CNV on Single Node OpenShift (SNO), [hostpath-provisioner](https://github.com/kubevirt/hostpath-provisioner) (part of the CNV product) storage is automatically opted in and set up to use, to enable persisting VM disks.
  This is done with the thought in mind that most virtualization use cases require persistence.
  The hostpath-provisioner is set up to utilize an LSO PV as the backing storage for provisioning dynamic hostPath volumes on.
