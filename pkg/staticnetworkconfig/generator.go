package staticnetworkconfig

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v2"
)

const staticNetworkConfigHostsDelimeter = "ZZZZZ"
const hostStaticNetworkDelimeter = "HHHHH"

type Config struct {
	MaxConcurrentGenerations int64 `envconfig:"MAX_CONCURRENT_NMSTATECTL_GENERATIONS" default:"30"`
}

type StaticNetworkConfigData struct {
	FilePath     string
	FileContents string
}

//go:generate mockgen -source=generator.go -package=staticnetworkconfig -destination=mock_generator.go
type StaticNetworkConfig interface {
	GenerateStaticNetworkConfigData(ctx context.Context, hostsYAMLS string) ([]StaticNetworkConfigData, error)
	FormatStaticNetworkConfigForDB(staticNetworkConfig []*models.HostStaticNetworkConfig) string
	ValidateStaticConfigParams(ctx context.Context, staticNetworkConfig []*models.HostStaticNetworkConfig) error
}

type StaticNetworkConfigGenerator struct {
	Config
	log logrus.FieldLogger
	sem *semaphore.Weighted
}

func New(log logrus.FieldLogger, cfg Config) StaticNetworkConfig {
	return &StaticNetworkConfigGenerator{
		Config: cfg,
		log:    log,
		sem:    semaphore.NewWeighted(cfg.MaxConcurrentGenerations)}
}

func (s *StaticNetworkConfigGenerator) GenerateStaticNetworkConfigData(ctx context.Context, hostsYAMLS string) ([]StaticNetworkConfigData, error) {
	hostsConfig := strings.Split(hostsYAMLS, staticNetworkConfigHostsDelimeter)
	s.log.Infof("Start configuring static network for %d hosts", len(hostsConfig))
	filesList := []StaticNetworkConfigData{}
	for i, hostConfig := range hostsConfig {
		hostFileList, err := s.generateHostStaticNetworkConfigData(ctx, hostConfig, fmt.Sprintf("host%d", i))
		if err != nil {
			s.log.WithError(err).Errorf("Failed to create static config for host")
			return nil, err
		}
		filesList = append(filesList, hostFileList...)
	}
	return filesList, nil
}

func (s *StaticNetworkConfigGenerator) generateHostStaticNetworkConfigData(ctx context.Context, hostConfigString, hostDir string) ([]StaticNetworkConfigData, error) {
	hostConfig := strings.Split(hostConfigString, hostStaticNetworkDelimeter)
	if len(hostConfig) != 2 {
		msg := fmt.Sprintf("Invalid format of the host config string %s", hostConfig)
		s.log.Errorf("%s", msg)
		return nil, fmt.Errorf("%s", msg)
	}
	hostYAML := hostConfig[0]
	macInterfaceMapping := hostConfig[1]
	result, err := s.executeNMStatectl(ctx, hostYAML)
	if err != nil {
		return nil, err
	}
	filesList, err := s.createNMConnectionFiles(result, hostDir)
	if err != nil {
		s.log.WithError(err).Errorf("failed to create NM connection files")
		return nil, err
	}
	mapConfigData := StaticNetworkConfigData{
		FilePath:     filepath.Join(hostDir, "mac_interface.ini"),
		FileContents: macInterfaceMapping,
	}
	filesList = append(filesList, mapConfigData)
	return filesList, nil
}

func (s *StaticNetworkConfigGenerator) executeNMStatectl(ctx context.Context, hostYAML string) (string, error) {
	err := s.sem.Acquire(ctx, 1)
	if err != nil {
		s.log.WithError(err).Errorf("Failed to lock semaphore for nmstatectl execution")
		return "", err
	}
	defer s.sem.Release(1)

	executer := &executer.CommonExecuter{}
	f, err := executer.TempFile("", "host-config")
	if err != nil {
		s.log.WithError(err).Errorf("Failed to create temp file")
		return "", err
	}
	defer func() {
		f.Close()
		os.Remove(f.Name())
	}()
	_, err = f.WriteString(hostYAML)
	if err != nil {
		s.log.WithError(err).Errorf("Failed to write host config to temp file")
		return "", err
	}
	stdout, stderr, retCode := executer.ExecuteWithContext(ctx, "nmstatectl", "gc", f.Name())
	if retCode != 0 {
		msg := fmt.Sprintf("<nmstatectl gc> failed, errorCode %d, stderr %s, input yaml <%s>", retCode, stderr, hostYAML)
		s.log.Errorf("%s", msg)
		return "", fmt.Errorf("%s", msg)
	}
	return stdout, nil
}

func (s *StaticNetworkConfigGenerator) createNMConnectionFiles(nmstateOutput, hostDir string) ([]StaticNetworkConfigData, error) {
	var hostNMConnections map[string]interface{}
	err := yaml.Unmarshal([]byte(nmstateOutput), &hostNMConnections)
	if err != nil {
		s.log.WithError(err).Errorf("Failed to unmarshal nmstate output")
		return nil, err
	}
	if _, found := hostNMConnections["NetworkManager"]; !found {
		return nil, errors.Errorf("nmstate generated an empty NetworkManager config file content")
	}
	filesList := []StaticNetworkConfigData{}
	connectionsList := hostNMConnections["NetworkManager"].([]interface{})
	for _, connection := range connectionsList {
		connectionElems := connection.([]interface{})
		fileName := connectionElems[0].(string)
		fileContents, err := s.formatNMConnection(connectionElems[1].(string))
		if err != nil {
			return nil, err
		}
		s.log.Infof("Adding NMConnection file <%s>", fileName)
		newFile := StaticNetworkConfigData{
			FilePath:     filepath.Join(hostDir, fileName),
			FileContents: fileContents,
		}
		filesList = append(filesList, newFile)
	}
	return filesList, nil
}

func (s *StaticNetworkConfigGenerator) formatNMConnection(nmConnection string) (string, error) {
	ini.PrettyFormat = false
	cfg, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, []byte(nmConnection))
	if err != nil {
		s.log.WithError(err).Errorf("Failed to load the ini format string %s", nmConnection)
		return "", err
	}
	connectionSection := cfg.Section("connection")
	_, err = connectionSection.NewKey("autoconnect", "true")
	if err != nil {
		s.log.WithError(err).Errorf("Failed to add autoconnect key to section connection")
		return "", err
	}
	_, err = connectionSection.NewKey("autoconnect-priority", "1")
	if err != nil {
		s.log.WithError(err).Errorf("Failed to add autoconnect-priority key to section connection")
		return "", err
	}

	buf := new(bytes.Buffer)
	_, err = cfg.WriteTo(buf)
	if err != nil {
		s.log.WithError(err).Errorf("Failed to output nmconnection ini file to buffer")
		return "", err
	}
	return buf.String(), nil
}

func (s *StaticNetworkConfigGenerator) ValidateStaticConfigParams(ctx context.Context, staticNetworkConfig []*models.HostStaticNetworkConfig) error {
	var err *multierror.Error
	for i, hostConfig := range staticNetworkConfig {
		err = multierror.Append(err, s.validateMacInterfaceName(i, hostConfig.MacInterfaceMap))
		err = multierror.Append(err, s.validateNMStateYaml(ctx, hostConfig.NetworkYaml))
	}
	return err.ErrorOrNil()
}

func (s *StaticNetworkConfigGenerator) validateMacInterfaceName(hostIdx int, macInterfaceMap models.MacInterfaceMap) error {
	interfaceCheck := make(map[string]struct{}, len(macInterfaceMap))
	macCheck := make(map[string]struct{}, len(macInterfaceMap))
	for _, macInterface := range macInterfaceMap {
		interfaceCheck[macInterface.LogicalNicName] = struct{}{}
		macCheck[macInterface.MacAddress] = struct{}{}
	}
	if len(interfaceCheck) < len(macInterfaceMap) || len(macCheck) < len(macInterfaceMap) {
		return fmt.Errorf("MACs and Interfaces for host %d must be unique", hostIdx)
	}
	return nil
}

func (s *StaticNetworkConfigGenerator) validateNMStateYaml(ctx context.Context, networkYaml string) error {
	_, err := s.executeNMStatectl(ctx, networkYaml)
	return err
}

func (s *StaticNetworkConfigGenerator) FormatStaticNetworkConfigForDB(staticNetworkConfig []*models.HostStaticNetworkConfig) string {
	lines := make([]string, len(staticNetworkConfig))
	for i, hostConfig := range staticNetworkConfig {
		hostLine := hostConfig.NetworkYaml + hostStaticNetworkDelimeter + s.formatMacInterfaceMap(hostConfig.MacInterfaceMap)
		lines[i] = hostLine
	}
	sort.Strings(lines)
	// delimeter between hosts config - will be used during nmconnections files generations for ISO ignition
	return strings.Join(lines, staticNetworkConfigHostsDelimeter)
}

func (s *StaticNetworkConfigGenerator) formatMacInterfaceMap(macInterfaceMap models.MacInterfaceMap) string {
	lines := make([]string, len(macInterfaceMap))
	for i, entry := range macInterfaceMap {
		lines[i] = fmt.Sprintf("%s=%s", entry.MacAddress, entry.LogicalNicName)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}
