package staticnetworkconfig

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const staticNetworkConfigHostDelimeter = "ZZZZZ"

type StaticNetworkConfigData struct {
	FilePath     string
	FileContents string
}

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

// TODO - in case we will need additional formating, consider using https://github.com/go-ini/ini
func (s *StaticNetworkConfigGenerator) formatNMConnection(nmConnection string) (string, error) {
	start := strings.Index(nmConnection, "interface-name=")
	if start == -1 {
		msg := "Failed to find interface-name label in nmconnection string"
		s.log.Errorf("%s", msg)
		return "", fmt.Errorf("%s", msg)
	}
	end := strings.Index(nmConnection[start:], "\n")
	mac := nmConnection[start+len("interface-name=") : start+end]
	splitMac := []string{}
	i := 0
	for i < len(mac) {
		splitMac = append(splitMac, mac[i:i+2])
		i += 2
	}
	mac = strings.Join(splitMac, ":")
	output := nmConnection[:start] + nmConnection[start+end+1:] + "\n[802-3-ethernet]\nmac-address=" + mac + "\n"
	return output, nil
}

func FormatStaticNetworkConfigForDB(staticNetworkConfig []string) string {
	lines := make([]string, len(staticNetworkConfig))
	copy(lines, staticNetworkConfig)
	sort.Strings(lines)
	// delimeter between hosts config - will be used during nmconnections files generations for ISO ignition
	return strings.Join(lines, staticNetworkConfigHostDelimeter)
}
