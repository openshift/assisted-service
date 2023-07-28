package nutanix

const (
	PhUsername   = "username_placeholder"
	PhPassword   = "password_placeholder"
	PhPCAddress  = "prism.central.placeholder.address"
	PhPCPort     = int32(9440)
	PhPEAddress  = "prism.element.placeholder.address"
	PhPEPort     = int32(9440)
	PhEName      = "prism_endpoint_name_placeholder"
	PhPUUID      = "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
	PhSubnetUUID = "yyyyyyyy-yyyy-yyyy-yyyy-yyyyyyyyyyyy"

	DbFieldUsername          = "platform_nutanix_username"
	DbFieldPassword          = "platform_nutanix_password"
	DbFieldPCEndpointAddress = "platform_nutanix_pc_address"
	DbFieldPCEndpointPort    = "platform_nutanix_pc_port"
	DbFieldPElementAddress   = "platform_nutanix_pe_address"
	DbFieldPElementPort      = "platform_nutanix_pe_port"
	DbFieldPUUID             = "platform_nutanix_pe_uuid"
	DbFieldSubnetUUID        = "platform_nutanix_subnet_uuid"

	NutanixManufacturer string = "Nutanix"
)
