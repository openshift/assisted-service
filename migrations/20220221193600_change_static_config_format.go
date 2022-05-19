package migrations

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"gorm.io/gorm"
)

const staticNetworkConfigHostsDelimeter = "ZZZZZ"
const hostStaticNetworkDelimeter = "HHHHH"

const CHANGE_STATIC_CONFIG_FORMAT_KEY = "20220221193600"

func formatMacInterfaceMap(macInterfaceMap models.MacInterfaceMap) string {
	lines := make([]string, len(macInterfaceMap))
	for i, entry := range macInterfaceMap {
		lines[i] = fmt.Sprintf("%s=%s", entry.MacAddress, entry.LogicalNicName)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func unformatMacInterfaceMap(macInterfaceMapStr string) (models.MacInterfaceMap, error) {
	lines := strings.Split(macInterfaceMapStr, "\n")
	ret := make(models.MacInterfaceMap, len(lines))
	for i := range lines {
		splitLine := strings.Split(lines[i], "=")
		if len(splitLine) != 2 {
			return nil, errors.Errorf("Split line '%s' does not have exact length of 2", lines[i])
		}
		ret[i] = &models.MacInterfaceMapItems0{
			MacAddress:     splitLine[0],
			LogicalNicName: splitLine[1],
		}
	}
	return ret, nil
}

func migrateInfraenvStaticConfigFormat(db *gorm.DB, infraEnv *models.InfraEnv) error {
	if infraEnv.StaticNetworkConfig == "" {
		return nil
	}
	var staticNetworkConfig []*models.HostStaticNetworkConfig
	hostsConfig := strings.Split(infraEnv.StaticNetworkConfig, staticNetworkConfigHostsDelimeter)
	for _, hostConfig := range hostsConfig {
		splitHostConfig := strings.Split(hostConfig, hostStaticNetworkDelimeter)
		if len(splitHostConfig) != 2 {
			return errors.Errorf("InfrEnv %s: Host config must have 2 elements, found %d", infraEnv.ID.String(), len(hostsConfig))
		}
		macInterfaceMap, err := unformatMacInterfaceMap(splitHostConfig[1])
		if err != nil {
			return err
		}
		staticNetworkConfig = append(staticNetworkConfig, &models.HostStaticNetworkConfig{
			NetworkYaml:     splitHostConfig[0],
			MacInterfaceMap: macInterfaceMap,
		})
	}
	var staticNetworkConfigStr string
	if len(staticNetworkConfig) == 0 {
		staticNetworkConfigStr = ""
	} else {
		b, err := json.Marshal(&staticNetworkConfig)
		if err != nil {
			return err
		}
		staticNetworkConfigStr = string(b)
	}
	return db.Model(&models.InfraEnv{ID: infraEnv.ID}).Update("static_network_config", staticNetworkConfigStr).Error
}

func formatStaticNetworkConfigForDB(staticNetworkConfig []*models.HostStaticNetworkConfig) string {
	lines := make([]string, len(staticNetworkConfig))
	for i, hostConfig := range staticNetworkConfig {
		hostLine := hostConfig.NetworkYaml + hostStaticNetworkDelimeter + formatMacInterfaceMap(hostConfig.MacInterfaceMap)
		lines[i] = hostLine
	}
	sort.Strings(lines)
	// delimeter between hosts config - will be used during nmconnections files generations for ISO ignition
	return strings.Join(lines, staticNetworkConfigHostsDelimeter)
}

func rollbackInfraenvStaticConfigFormat(db *gorm.DB, infraEnv *models.InfraEnv) error {
	if infraEnv.StaticNetworkConfig == "" {
		return nil
	}
	var staticNetworkConfig []*models.HostStaticNetworkConfig
	err := json.Unmarshal([]byte(infraEnv.StaticNetworkConfig), &staticNetworkConfig)
	if err != nil {
		return err
	}
	return db.Model(&models.InfraEnv{ID: infraEnv.ID}).Update("static_network_config", formatStaticNetworkConfigForDB(staticNetworkConfig)).Error
}

func migrateStaticConfigFormat(db *gorm.DB) error {
	var infraEnvs []*models.InfraEnv
	likePattern := "%" + hostStaticNetworkDelimeter + "%"
	err := db.Find(&infraEnvs, "static_network_config like ?", likePattern).Error
	if err != nil {
		return err
	}
	for _, infraEnv := range infraEnvs {
		if err = migrateInfraenvStaticConfigFormat(db, infraEnv); err != nil {
			return err
		}
	}
	return nil
}

func rollbackStaticConfigFormat(db *gorm.DB) error {
	var infraEnvs []*models.InfraEnv
	err := db.Find(&infraEnvs, "static_network_config like '[%]'").Error
	if err != nil {
		return err
	}
	for _, infraEnv := range infraEnvs {
		if err = rollbackInfraenvStaticConfigFormat(db, infraEnv); err != nil {
			return err
		}
	}
	return nil
}

func changeStaticConfigFormat() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID:       CHANGE_STATIC_CONFIG_FORMAT_KEY,
		Migrate:  migrateStaticConfigFormat,
		Rollback: rollbackStaticConfigFormat,
	}
}
