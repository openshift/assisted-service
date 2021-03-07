package staticnetworkconfig

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/sirupsen/logrus"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v2"
)

const staticNetworkConfigHostDelimeter = "ZZZZZ"

type StaticNetworkConfigData struct {
	FilePath     string
	FileContents string
}

//go:generate mockgen -source=generator.go -package=staticnetworkconfig -destination=mock_generator.go
type StaticNetworkConfig interface {
	GenerateStaticNetworkConfigData(hostsYAMLS string) ([]StaticNetworkConfigData, error)
}

type StaticNetworkConfigGenerator struct {
	log logrus.FieldLogger
}

func New(log logrus.FieldLogger) StaticNetworkConfig {
	return &StaticNetworkConfigGenerator{log: log}
}

func (s *StaticNetworkConfigGenerator) GenerateStaticNetworkConfigData(hostsYAMLS string) ([]StaticNetworkConfigData, error) {
	hostsConfig := strings.Split(hostsYAMLS, staticNetworkConfigHostDelimeter)
	s.log.Infof("Start configuring static network for %d hosts", len(hostsConfig))
	executer := &executer.CommonExecuter{}
	filesList := []StaticNetworkConfigData{}
	for _, hostConfig := range hostsConfig {
		f, err := executer.TempFile("", "host-config")
		if err != nil {
			s.log.WithError(err).Errorf("Failed to create temp file")
			return nil, err
		}
		_, err = f.WriteString(hostConfig)
		if err != nil {
			s.log.WithError(err).Errorf("Failed to write host config to temp file")
			return nil, err
		}
		f.Close()
		stdout, stderr, retCode := executer.Execute("nmstatectl", "gc", f.Name())
		if retCode != 0 {
			msg := fmt.Sprintf("<nmstatectl gc> failed, errorCode %d, stderr %s, input yaml <%s>", retCode, stderr, hostConfig)
			s.log.Errorf("%s", msg)
			return nil, fmt.Errorf("%s", msg)
		}
		err = s.createNMConnectionFiles(stdout, &filesList)
		if err != nil {
			s.log.WithError(err).Errorf("failed to create NM connection files")
			return nil, err
		}
		os.Remove(f.Name())
	}
	return filesList, nil
}

func (s *StaticNetworkConfigGenerator) createNMConnectionFiles(nmstateOutput string, filesList *[]StaticNetworkConfigData) error {
	var hostNMConnections map[string]interface{}
	err := yaml.Unmarshal([]byte(nmstateOutput), &hostNMConnections)
	if err != nil {
		s.log.WithError(err).Errorf("Failed to unmarshal nmstate output")
		return err
	}
	connectionsList := hostNMConnections["NetworkManager"].([]interface{})
	for _, connection := range connectionsList {
		connectionElems := connection.([]interface{})
		fileName := connectionElems[0].(string)
		fileContents, err := s.formatNMConnection(connectionElems[1].(string))
		if err != nil {
			return err
		}
		s.log.Infof("Adding NMConnection file <%s>", fileName)
		newFile := StaticNetworkConfigData{
			FilePath:     fileName,
			FileContents: fileContents,
		}
		*filesList = append(*filesList, newFile)
	}
	return nil
}

func (s *StaticNetworkConfigGenerator) formatNMConnection(nmConnection string) (string, error) {
	ini.PrettyFormat = false
	cfg, err := ini.LoadSources(ini.LoadOptions{IgnoreInlineComment: true}, []byte(nmConnection))
	if err != nil {
		s.log.WithError(err).Errorf("Failed to load the ini format string %s", nmConnection)
		return "", err
	}
	connectionSection := cfg.Section("connection")
	ifaceName := connectionSection.Key("interface-name")
	if ifaceName == nil {
		msg := "interface-name key is not present in section connection"
		s.log.Errorf("%s", msg)
		return "", fmt.Errorf("%s", msg)
	}

	mac, err := s.validateAndCalculateMAC(ifaceName.String())
	if err != nil {
		return "", err
	}

	connectionSection.DeleteKey("interface-name")
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

	ethernetSection, err := cfg.NewSection("802-3-ethernet")
	if err != nil {
		s.log.WithError(err).Errorf("Failed to add 802-3-ethernet section to nm connection")
		return "", err
	}

	_, err = ethernetSection.NewKey("mac-address", mac)
	if err != nil {
		s.log.WithError(err).Errorf("Failed to add key mac-address, value %s to 802-3-ethernet connection", mac)
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

func (s *StaticNetworkConfigGenerator) validateAndCalculateMAC(ifaceName string) (string, error) {
	// check the format of the interface name
	ifaceNameRegexp := "^[0-9A-Fa-f]{12}$"
	match, err := regexp.MatchString(ifaceNameRegexp, ifaceName)
	if err != nil {
		s.log.WithError(err).Errorf("Invalid regexp expression %s", ifaceNameRegexp)
		return "", err
	}
	if !match {
		msg := "Interface name %s is in invalid format: must be mac address without\":\""
		s.log.Errorf("%s", msg)
		return "", fmt.Errorf("%s", msg)
	}
	splitMac := []string{}
	i := 0
	for i < len(ifaceName) {
		splitMac = append(splitMac, ifaceName[i:i+2])
		i += 2
	}
	return strings.Join(splitMac, ":"), nil
}

func FormatStaticNetworkConfigForDB(staticNetworkConfig []string) string {
	lines := make([]string, len(staticNetworkConfig))
	copy(lines, staticNetworkConfig)
	sort.Strings(lines)
	// delimeter between hosts config - will be used during nmconnections files generations for ISO ignition
	return strings.Join(lines, staticNetworkConfigHostDelimeter)
}
