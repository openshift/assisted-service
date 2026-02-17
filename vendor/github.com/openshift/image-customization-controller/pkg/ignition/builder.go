package ignition

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	ignition_config_types_32 "github.com/coreos/ignition/v2/config/v3_2/types"
	vpath "github.com/coreos/vcontext/path"
)

type ignitionBuilder struct {
	nmStateData               []byte
	registriesConf            []byte
	ironicBaseURL             string
	ironicInspectorBaseURL    string
	ironicAgentImage          string
	ironicAgentPullSecret     string
	ironicRAMDiskSSHKey       string
	networkKeyFiles           []byte
	ipOptions                 string
	httpProxy                 string
	httpsProxy                string
	noProxy                   string
	hostname                  string
	ironicAgentVlanInterfaces string
	additionalNTPServers      []string
	caBundleFile              string
}

func New(nmStateData, registriesConf []byte, ironicBaseURL, ironicInspectorBaseURL, ironicAgentImage, ironicAgentPullSecret, ironicRAMDiskSSHKey, ipOptions string, httpProxy, httpsProxy, noProxy string, hostname string, ironicAgentVlanInterfaces string, additionalNTPServers []string, caBundleFile string) (*ignitionBuilder, error) {
	if ironicBaseURL == "" {
		return nil, errors.New("ironicBaseURL is required")
	}
	if ironicAgentImage == "" {
		return nil, errors.New("ironicAgentImage is required")
	}

	return &ignitionBuilder{
		nmStateData:               nmStateData,
		registriesConf:            registriesConf,
		ironicBaseURL:             ironicBaseURL,
		ironicInspectorBaseURL:    ironicInspectorBaseURL,
		ironicAgentImage:          ironicAgentImage,
		ironicAgentPullSecret:     ironicAgentPullSecret,
		ironicRAMDiskSSHKey:       ironicRAMDiskSSHKey,
		ipOptions:                 ipOptions,
		httpProxy:                 httpProxy,
		httpsProxy:                httpsProxy,
		noProxy:                   noProxy,
		hostname:                  hostname,
		ironicAgentVlanInterfaces: ironicAgentVlanInterfaces,
		additionalNTPServers:      additionalNTPServers,
		caBundleFile:              caBundleFile,
	}, nil
}

func (b *ignitionBuilder) ProcessNetworkState() (string, error) {
	if len(b.nmStateData) > 0 {
		nmstatectl := exec.Command("nmstatectl", "gc", "/dev/stdin")
		nmstatectl.Stdin = strings.NewReader(string(b.nmStateData))
		out, err := nmstatectl.Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return string(ee.Stderr), err
			}
			return "", err
		}
		if string(out) == "--- {}\n" {
			return "no network configuration", nil
		}
		b.networkKeyFiles = out
	}
	return "", nil
}

func (b *ignitionBuilder) GenerateConfig() (config ignition_config_types_32.Config, err error) {
	netFiles := []ignition_config_types_32.File{}
	if len(b.nmStateData) > 0 {
		nmstatectl := exec.Command("nmstatectl", "gc", "/dev/stdin")
		nmstatectl.Stdin = strings.NewReader(string(b.nmStateData))
		out, err := nmstatectl.Output()
		if err != nil {
			return config, err
		}

		netFiles, err = nmstateOutputToFiles(out)
		if err != nil {
			return config, err
		}
	}

	var ironicInspectorVlanInterfaces string
	if strings.ToLower(b.ironicAgentVlanInterfaces) == "always" {
		ironicInspectorVlanInterfaces = "all"
	} else if strings.ToLower(b.ironicAgentVlanInterfaces) == "never" {
		ironicInspectorVlanInterfaces = ""
	} else {
		if len(b.nmStateData) > 0 {
			ironicInspectorVlanInterfaces = ""
		} else {
			ironicInspectorVlanInterfaces = "all"
		}
	}

	config.Ignition.Version = "3.2.0"
	config.Storage.Files = []ignition_config_types_32.File{b.IronicAgentConf(ironicInspectorVlanInterfaces)}
	config.Storage.Files = append(config.Storage.Files, netFiles...)
	config.Systemd.Units = []ignition_config_types_32.Unit{b.IronicAgentService(len(netFiles) > 0)}

	if b.ironicAgentPullSecret != "" {
		config.Storage.Files = append(config.Storage.Files, b.authFile())
	}

	if b.ironicRAMDiskSSHKey != "" {
		config.Passwd.Users = append(config.Passwd.Users, ignition_config_types_32.PasswdUser{
			Name: "core",
			SSHAuthorizedKeys: []ignition_config_types_32.SSHAuthorizedKey{
				ignition_config_types_32.SSHAuthorizedKey(strings.TrimSpace(b.ironicRAMDiskSSHKey)),
			},
		})
	}

	if b.caBundleFile != "" {
		data, err := os.ReadFile(b.caBundleFile)
		if err != nil {
			return config, err
		}
		config.Storage.Files = append(config.Storage.Files, ignitionFileEmbed(
			"/etc/pki/ca-trust/source/anchors/ca.crt",
			0644, false, data))
	}

	config.Storage.Files = append(config.Storage.Files, ignitionFileEmbed(
		"/etc/NetworkManager/conf.d/clientid.conf",
		0644, false,
		[]byte("[connection]\nipv6.dhcp-duid=ll\nipv6.dhcp-iaid=mac")))

	if len(b.additionalNTPServers) > 0 {
		additionalChronyConfig := strings.Builder{}
		for _, server := range b.additionalNTPServers {
			additionalChronyConfig.WriteString(fmt.Sprintf("\nserver %s iburst", server))
		}
		additionalChronyConfig.WriteString("\n")
		config.Storage.Files = append(config.Storage.Files, ignitionFileEmbedAppend(
			"/etc/chrony.conf",
			0644,
			[]byte(additionalChronyConfig.String())))
	}

	if b.hostname != "" {
		update_hostname := fmt.Sprintf(`
	    [[ "$DHCP6_FQDN_FQDN" =~ "." ]] && hostnamectl set-hostname --static --transient $DHCP6_FQDN_FQDN 
	    [[ "$(< /proc/sys/kernel/hostname)" =~ (localhost|localhost.localdomain) ]] && hostnamectl set-hostname --transient %s`, b.hostname)

		config.Storage.Files = append(config.Storage.Files, ignitionFileEmbed(
			"/etc/NetworkManager/dispatcher.d/01-hostname",
			0744, false,
			[]byte(update_hostname)))
	}

	if len(b.registriesConf) > 0 {
		registriesFile := ignitionFileEmbed("/etc/containers/registries.conf",
			0644, true,
			b.registriesConf)

		config.Storage.Files = append(config.Storage.Files, registriesFile)
	}

	// Add console identification message (shown before login)
	issueMessage := `
================================================================================
 This is the Ironic Python Agent (IPA) discovery image

 This image is used for bare metal host discovery and inspection.
 It is NOT a cluster node.
================================================================================
`
	config.Storage.Files = append(config.Storage.Files, ignitionFileEmbed(
		"/etc/issue.d/ipa.issue",
		0644, false,
		[]byte(issueMessage)))

	// Add MOTD for SSH users with debugging information
	motdMessage := fmt.Sprintf(`
================================================================================
 Welcome to the Ironic Python Agent (IPA) discovery image

 This image is used for bare metal host discovery and inspection.
 It is NOT a cluster node.

 This system will automatically run hardware inspection and report back to
 the Ironic service. Do not attempt to use this as a regular system.

 Configuration:
   Ironic API:       %s
   IPA Image:        %s`,
		b.ironicBaseURL,
		b.ironicAgentImage)

	if b.ironicInspectorBaseURL != "" {
		motdMessage += fmt.Sprintf(`
   Inspector API:    %s`, b.ironicInspectorBaseURL)
	}

	if b.hostname != "" {
		motdMessage += fmt.Sprintf(`
   Hostname:         %s`, b.hostname)
	}

	if len(b.nmStateData) > 0 {
		motdMessage += `
   Network Config:   Custom NMState configuration applied`
	}

	motdMessage += `

 Useful commands:
   journalctl -u ironic-agent.service    View IPA service logs
   systemctl status ironic-agent.service Check IPA service status
   ip addr                                Show network interfaces
   nmcli connection show                  Show network connections
================================================================================
`
	config.Storage.Files = append(config.Storage.Files, ignitionFileEmbed(
		"/etc/motd.d/ipa.motd",
		0644, false,
		[]byte(motdMessage)))

	report := config.Storage.Validate(vpath.ContextPath{})
	if report.IsFatal() {
		return config, errors.New(report.String())
	}

	return config, nil
}

func (b *ignitionBuilder) Generate() ([]byte, error) {
	config, err := b.GenerateConfig()
	if err != nil {
		return nil, err
	}

	return json.Marshal(config)
}
