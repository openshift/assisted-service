// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"strconv"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/go-openapi/validate"
)

// InstallCmdRequest install cmd request
//
// swagger:model install_cmd_request
type InstallCmdRequest struct {

	// Boot device to write image on
	// Required: true
	BootDevice *string `json:"boot_device"`

	// Check CVO status if needed
	CheckCvo *bool `json:"check_cvo,omitempty"`

	// Cluster id
	// Required: true
	// Format: uuid
	ClusterID *strfmt.UUID `json:"cluster_id"`

	// Specifies the required number of control plane nodes that should be part of the cluster.
	ControlPlaneCount int64 `json:"control_plane_count,omitempty"`

	// Assisted installer controller image
	// Required: true
	// Pattern: ^(([a-zA-Z0-9\-\.]+)(:[0-9]+)?\/)?[a-z0-9\._\-\/@]+[?::a-zA-Z0-9_\-.]+$
	ControllerImage *string `json:"controller_image"`

	// CoreOS container image to use if installing to the local device
	CoreosImage string `json:"coreos_image,omitempty"`

	// List of disks to format
	DisksToFormat []string `json:"disks_to_format"`

	// If true, assisted service will attempt to skip MCO reboot
	EnableSkipMcoReboot bool `json:"enable_skip_mco_reboot,omitempty"`

	// Host id
	// Required: true
	// Format: uuid
	HostID *strfmt.UUID `json:"host_id"`

	// Infra env id
	// Required: true
	// Format: uuid
	InfraEnvID *strfmt.UUID `json:"infra_env_id"`

	// Core-os installer addtional args
	InstallerArgs string `json:"installer_args,omitempty"`

	// Assisted installer image
	// Required: true
	// Pattern: ^(([a-zA-Z0-9\-\.]+)(:[0-9]+)?\/)?[a-z0-9\._\-\/@]+[?::a-zA-Z0-9_\-.]+$
	InstallerImage *string `json:"installer_image"`

	// Machine config operator image
	// Pattern: ^(([a-zA-Z0-9\-\.]+)(:[0-9]+)?\/)?[a-z0-9\._\-\/@]+[?::a-zA-Z0-9_\-.]+$
	McoImage string `json:"mco_image,omitempty"`

	// Must-gather images to use
	MustGatherImage string `json:"must_gather_image,omitempty"`

	// If true, notify number of reboots by assisted controller
	NotifyNumReboots bool `json:"notify_num_reboots,omitempty"`

	// Version of the OpenShift cluster.
	OpenshiftVersion string `json:"openshift_version,omitempty"`

	// proxy
	Proxy *Proxy `json:"proxy,omitempty" gorm:"embedded;embeddedPrefix:proxy_"`

	// role
	// Required: true
	Role *HostRole `json:"role"`

	// List of service ips
	ServiceIps []string `json:"service_ips"`

	// Skip formatting installation disk
	SkipInstallationDiskCleanup bool `json:"skip_installation_disk_cleanup,omitempty"`
}

// Validate validates this install cmd request
func (m *InstallCmdRequest) Validate(formats strfmt.Registry) error {
	var res []error

	if err := m.validateBootDevice(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateClusterID(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateControllerImage(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateHostID(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateInfraEnvID(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateInstallerImage(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateMcoImage(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateProxy(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateRole(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateServiceIps(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (m *InstallCmdRequest) validateBootDevice(formats strfmt.Registry) error {

	if err := validate.Required("boot_device", "body", m.BootDevice); err != nil {
		return err
	}

	return nil
}

func (m *InstallCmdRequest) validateClusterID(formats strfmt.Registry) error {

	if err := validate.Required("cluster_id", "body", m.ClusterID); err != nil {
		return err
	}

	if err := validate.FormatOf("cluster_id", "body", "uuid", m.ClusterID.String(), formats); err != nil {
		return err
	}

	return nil
}

func (m *InstallCmdRequest) validateControllerImage(formats strfmt.Registry) error {

	if err := validate.Required("controller_image", "body", m.ControllerImage); err != nil {
		return err
	}

	if err := validate.Pattern("controller_image", "body", *m.ControllerImage, `^(([a-zA-Z0-9\-\.]+)(:[0-9]+)?\/)?[a-z0-9\._\-\/@]+[?::a-zA-Z0-9_\-.]+$`); err != nil {
		return err
	}

	return nil
}

func (m *InstallCmdRequest) validateHostID(formats strfmt.Registry) error {

	if err := validate.Required("host_id", "body", m.HostID); err != nil {
		return err
	}

	if err := validate.FormatOf("host_id", "body", "uuid", m.HostID.String(), formats); err != nil {
		return err
	}

	return nil
}

func (m *InstallCmdRequest) validateInfraEnvID(formats strfmt.Registry) error {

	if err := validate.Required("infra_env_id", "body", m.InfraEnvID); err != nil {
		return err
	}

	if err := validate.FormatOf("infra_env_id", "body", "uuid", m.InfraEnvID.String(), formats); err != nil {
		return err
	}

	return nil
}

func (m *InstallCmdRequest) validateInstallerImage(formats strfmt.Registry) error {

	if err := validate.Required("installer_image", "body", m.InstallerImage); err != nil {
		return err
	}

	if err := validate.Pattern("installer_image", "body", *m.InstallerImage, `^(([a-zA-Z0-9\-\.]+)(:[0-9]+)?\/)?[a-z0-9\._\-\/@]+[?::a-zA-Z0-9_\-.]+$`); err != nil {
		return err
	}

	return nil
}

func (m *InstallCmdRequest) validateMcoImage(formats strfmt.Registry) error {
	if swag.IsZero(m.McoImage) { // not required
		return nil
	}

	if err := validate.Pattern("mco_image", "body", m.McoImage, `^(([a-zA-Z0-9\-\.]+)(:[0-9]+)?\/)?[a-z0-9\._\-\/@]+[?::a-zA-Z0-9_\-.]+$`); err != nil {
		return err
	}

	return nil
}

func (m *InstallCmdRequest) validateProxy(formats strfmt.Registry) error {
	if swag.IsZero(m.Proxy) { // not required
		return nil
	}

	if m.Proxy != nil {
		if err := m.Proxy.Validate(formats); err != nil {
			if ve, ok := err.(*errors.Validation); ok {
				return ve.ValidateName("proxy")
			} else if ce, ok := err.(*errors.CompositeError); ok {
				return ce.ValidateName("proxy")
			}
			return err
		}
	}

	return nil
}

func (m *InstallCmdRequest) validateRole(formats strfmt.Registry) error {

	if err := validate.Required("role", "body", m.Role); err != nil {
		return err
	}

	if err := validate.Required("role", "body", m.Role); err != nil {
		return err
	}

	if m.Role != nil {
		if err := m.Role.Validate(formats); err != nil {
			if ve, ok := err.(*errors.Validation); ok {
				return ve.ValidateName("role")
			} else if ce, ok := err.(*errors.CompositeError); ok {
				return ce.ValidateName("role")
			}
			return err
		}
	}

	return nil
}

func (m *InstallCmdRequest) validateServiceIps(formats strfmt.Registry) error {
	if swag.IsZero(m.ServiceIps) { // not required
		return nil
	}

	for i := 0; i < len(m.ServiceIps); i++ {

		if err := validate.Pattern("service_ips"+"."+strconv.Itoa(i), "body", m.ServiceIps[i], `^(?:(?:(?:[0-9]{1,3}\.){3}[0-9]{1,3})|(?:(?:[0-9a-fA-F]*:[0-9a-fA-F]*){2,}))$`); err != nil {
			return err
		}

	}

	return nil
}

// ContextValidate validate this install cmd request based on the context it is used
func (m *InstallCmdRequest) ContextValidate(ctx context.Context, formats strfmt.Registry) error {
	var res []error

	if err := m.contextValidateProxy(ctx, formats); err != nil {
		res = append(res, err)
	}

	if err := m.contextValidateRole(ctx, formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (m *InstallCmdRequest) contextValidateProxy(ctx context.Context, formats strfmt.Registry) error {

	if m.Proxy != nil {
		if err := m.Proxy.ContextValidate(ctx, formats); err != nil {
			if ve, ok := err.(*errors.Validation); ok {
				return ve.ValidateName("proxy")
			} else if ce, ok := err.(*errors.CompositeError); ok {
				return ce.ValidateName("proxy")
			}
			return err
		}
	}

	return nil
}

func (m *InstallCmdRequest) contextValidateRole(ctx context.Context, formats strfmt.Registry) error {

	if m.Role != nil {
		if err := m.Role.ContextValidate(ctx, formats); err != nil {
			if ve, ok := err.(*errors.Validation); ok {
				return ve.ValidateName("role")
			} else if ce, ok := err.(*errors.CompositeError); ok {
				return ce.ValidateName("role")
			}
			return err
		}
	}

	return nil
}

// MarshalBinary interface implementation
func (m *InstallCmdRequest) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *InstallCmdRequest) UnmarshalBinary(b []byte) error {
	var res InstallCmdRequest
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
