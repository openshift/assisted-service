package ovirt

const (
	DbFieldFqdn     = "platform_ovirt_fqdn"
	DbFieldInsecure = "platform_ovirt_insecure"
	DbFieldUsername = "platform_ovirt_username"
	/* #nosec */
	DbFieldPassword        = "platform_ovirt_password"
	DbFieldCaBundle        = "platform_ovirt_ca_bundle"
	DbFieldClusterID       = "platform_ovirt_cluster_id"
	DbFieldStorageDomainID = "platform_ovirt_storage_domain_id"
	DbFieldNetworkName     = "platform_ovirt_network_name"
	DbFieldVnicProfileID   = "platform_ovirt_vnic_profile_id"

	OvirtManufacturer string = "oVirt"

	EngineURLStrFmt                   = "https://%s/ovirt-engine/api"
	VmNamePatternStrFmt               = "name: %s-([b-df-hj-np-tv-z0-9]){5}-master-[012]"
	VmNameReplacementStrFmt           = "name: %s"
	TemplateNamePatternStr            = "template_name: +.*"
	TemplateNameReplacementStrFmt     = "template_name: %s"
	MachineManifestFileNameGlobStrFmt = "*_openshift-cluster-api_master-machines-%d.yaml"
	ReplicasPatternStr                = "replicas: +.*"
	ReplicasReplacementStrFmt         = "replicas: %d"
	MachineSetFileNameGlobStr         = "*_openshift-cluster-api_worker-machineset-0.yaml"
)
