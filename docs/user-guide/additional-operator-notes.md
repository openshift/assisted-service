# Additional OLM operator notes

## OpenShift Virtualization (CNV)
- When deploying CNV on Single Node OpenShift (SNO), [hostpath-provisioner](https://github.com/kubevirt/hostpath-provisioner) (part of the CNV product) storage is automatically opted in and set up to use, to enable persisting VM disks.  
This is done with the thought in mind that most virtualization use cases require persistence.  
The hostpath-provisioner is set up to utilize an LSO PV as the backing storage for provisioning dynamic hostPath volumes on.

## Multi-Cluster Engine (MCE)

- When deploying MCE together with a storage operator (ODF or LVM) Infrastructure Operator will be automatically
enabled. This will require extra disk space availability requirements for the storage operator.
When selecting MCE with both ODF and LVM, ODF will have priority and its storage class will be selected to provision Infrastructure Operator's PVCs.
