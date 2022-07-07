package ovirt

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	ovirtclient "github.com/ovirt/go-ovirt-client"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

// OvirtConfig contains the required information to perform
// an HTTPS connection to an oVirt engine
type OvirtConfig struct {
	URL      string `yaml:"ovirt_url,omitempty"`
	Username string `yaml:"ovirt_username,omitempty"`
	Password string `yaml:"ovirt_password,omitempty"`
	Insecure bool   `yaml:"ovirt_insecure,omitempty"`
	CaBundle string `yaml:"ovirt_ca_bundle,omitempty"`
}

func (p ovirtProvider) createOvirtConfig(workDir string, platformParams *models.OvirtPlatform) (string, error) {
	if platformParams == nil {
		return "", nil
	}
	URL := fmt.Sprintf(EngineURLStrFmt, *platformParams.Fqdn)
	oVirtConfig := &OvirtConfig{
		URL:      URL,
		Username: swag.StringValue(platformParams.Username),
		Password: platformParams.Password.String(),
		Insecure: swag.BoolValue(platformParams.Insecure),
		CaBundle: swag.StringValue(platformParams.CaBundle),
	}
	ovirtConfigPath := filepath.Join(workDir, ".ovirt-config.yaml")
	var cfg []byte
	cfg, err := yaml.Marshal(oVirtConfig)
	if err != nil {
		return "", err
	}
	err = ioutil.WriteFile(ovirtConfigPath, cfg, 0600)
	if err != nil {
		return "", err
	}
	return ovirtConfigPath, nil
}

// PreCreateManifestHook creates the 'ovirt-config.yaml' file required by the installer
// for the oVirt platform and append the OVIRT_CONFIG to the environment variables
func (p ovirtProvider) PreCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	if common.PlatformTypeValue(cluster.Platform.Type) == models.PlatformTypeOvirt {
		ovirtConfigPath, err := p.createOvirtConfig(workDir, cluster.Platform.Ovirt)
		if err != nil {
			return errors.Wrapf(err, "unable to create the ovirt config file %s", ovirtConfigPath)
		}
		if ovirtConfigPath != "" {
			*envVars = append(*envVars, "OVIRT_CONFIG="+ovirtConfigPath)
		}
	}
	return nil
}

func (p ovirtProvider) getOvirtClient(params *models.OvirtPlatform) (ovirtclient.Client, error) {
	var URL, userName, password string
	if p.OvirtClient != nil {
		return p.OvirtClient, nil
	}
	if params == nil {
		return nil, errors.New("no ovirt platform params provided")
	}
	tls := ovirtclient.TLS()
	if params.Insecure != nil && *params.Insecure {
		tls.Insecure()
	}
	if params.CaBundle != nil {
		tls.CACertsFromMemory([]byte(*params.CaBundle))
	}
	if params.Fqdn != nil {
		URL = fmt.Sprintf(EngineURLStrFmt, *params.Fqdn)
	}
	if params.Username != nil {
		userName = *params.Username
	}
	if params.Password != nil {
		password = params.Password.String()
	}
	ovirtClient, err := ovirtclient.New(
		URL,
		userName,
		password,
		tls,
		nil,
		nil,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create a new ovirt client")
	}
	p.OvirtClient = ovirtClient
	return p.OvirtClient, nil
}

func updateHostInfoInManifest(workDir, fileName, clusterName, vmName, templateName string) error {
	vmNamePattern := fmt.Sprintf(VmNamePatternStrFmt, clusterName)
	vmNameRegexp := regexp.MustCompile(vmNamePattern)
	vmNameReplacement := fmt.Sprintf(VmNameReplacementStrFmt, vmName)
	templateNameRegexp := regexp.MustCompile(TemplateNamePatternStr)
	templateNameReplacement := fmt.Sprintf(TemplateNameReplacementStrFmt, templateName)

	content, err := readContentFromFileGlob(workDir, fileName)
	if err != nil {
		return errors.Wrapf(err, "unable to get contents from master machine file")
	}

	newContent := vmNameRegexp.ReplaceAllString(string(content), vmNameReplacement)
	newContent = templateNameRegexp.ReplaceAllString(newContent, templateNameReplacement)

	err = writeContentFromFileGlob(workDir, fileName, []byte(newContent))
	if err != nil {
		return errors.Wrapf(err, "unable to write contents to master machine file")
	}
	return nil
}

func readContentFromFileGlob(workDir, fileGlob string) ([]byte, error) {
	var content []byte

	globFullPath := filepath.Join(workDir, "openshift", fileGlob)
	fullPaths, err := filepath.Glob(globFullPath)

	if err != nil {
		return nil, errors.Wrapf(err, "error computing file glob %s", globFullPath)
	}

	if fullPaths == nil {
		return nil, errors.New(fmt.Sprintf("unable to find file '%s'", globFullPath))
	}

	if len(fullPaths) > 1 {
		return nil, errors.New(fmt.Sprintf("more than one file found for glob '%s': %v", globFullPath, fullPaths))
	}
	filePath := fullPaths[0]

	content, err = ioutil.ReadFile(filePath)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read file %s", filePath)
	}
	return content, nil
}

func writeContentFromFileGlob(workDir, fileGlob string, content []byte) error {
	globFullPath := filepath.Join(workDir, "openshift", fileGlob)
	fullPaths, _ := filepath.Glob(globFullPath)

	if fullPaths == nil {
		return errors.New(fmt.Sprintf("unable to find file '%s'", globFullPath))
	}

	if len(fullPaths) > 1 {
		return errors.New(fmt.Sprintf("more than one file found for glob '%s': %v", globFullPath, fullPaths))
	}
	filePath := fullPaths[0]

	err := ioutil.WriteFile(filePath, content, 0600)
	if err != nil {
		return errors.Wrapf(err, "unable to write file %s", filePath)
	}
	return nil
}

// PostCreateManifestsHook modifies master's Machine manifests with the actual VM and Template names
func (p ovirtProvider) PostCreateManifestsHook(cluster *common.Cluster, envVars *[]string, workDir string) error {
	if cluster == nil {
		return errors.New("no cluster provided")
	}
	if cluster.Platform == nil || common.PlatformTypeValue(cluster.Platform.Type) != models.PlatformTypeOvirt {
		return errors.New("platform type is not ovirt")
	}
	if cluster.Platform.Ovirt == nil {
		return errors.New("ovirt platform connection params not set")
	}

	retry := ovirtclient.AutoRetry()
	client, err := p.getOvirtClient(cluster.Platform.Ovirt)
	if err != nil {
		return errors.Wrap(err, "unable to get an ovirt client")
	}

	for i, host := range common.GetHostsByRole(cluster, models.HostRoleMaster) {
		vm_id := host.ID
		vm, err := client.GetVM(vm_id.String(), retry)
		if err != nil {
			return errors.Wrapf(err, "unable to retrieve VM info (%s)", vm_id)
		}
		templateID := vm.TemplateID()
		template, err := client.GetTemplate(templateID, retry)
		if err != nil {
			return err
		}
		fileName := fmt.Sprintf(MachineManifestFileNameGlobStrFmt, i)
		err = updateHostInfoInManifest(workDir, fileName, cluster.Name, vm.Name(), template.Name())
		if err != nil {
			return errors.Wrapf(err, "unable to update master '%d' with UUID '%s'", i, vm_id)
		}
	}
	return nil
}
