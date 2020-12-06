package k8sclient

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
	v1 "k8s.io/api/core/v1"
)

func GetApiVIP(configMap *v1.ConfigMap, log logrus.FieldLogger) (string, error) {
	configStruct := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(configMap.Data["install-config"]), configStruct)
	if err != nil {
		log.WithError(err).Errorf("Failed to unmarshal confimap cluster-config-v1 data: <%s>", configMap.Data["install-config"])
		return "", err
	}
	platform, ok := configStruct["platform"].(map[interface{}]interface{})
	if !ok {
		err := fmt.Errorf("invalid or missing platform key in cluster-config-v1")
		log.WithError(err).Errorf("invalid format for cluster-config-v1")
		return "", err
	}
	baremetal, ok := platform["baremetal"].(map[interface{}]interface{})
	if !ok {
		err := fmt.Errorf("invalid or missing baremetal key in  platform in cluster-config-v1")
		log.WithError(err).Errorf("invalid format for cluster-config-v1")
		return "", err
	}
	apiVip, ok := baremetal["apiVIP"].(string)
	if !ok {
		err := fmt.Errorf("invalid or missing api vip key baremetal in cluster-config-v1")
		log.WithError(err).Errorf("invalid format for cluster-config-v1")
		return "", err
	}
	return apiVip, nil
}

func GetBaseDNSDomain(configMap *v1.ConfigMap, log logrus.FieldLogger) (string, error) {
	configStruct := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(configMap.Data["install-config"]), configStruct)
	if err != nil {
		log.WithError(err).Errorf("Failed to unmarshal confimap cluster-config-v1 data: <%s>", configMap.Data["install-config"])
		return "", err
	}
	baseDomain, ok := configStruct["baseDomain"].(string)
	if !ok {
		err := fmt.Errorf("invalid or missing baseDomain key in cluster-config-v1")
		log.WithError(err).Errorf("invalid format for cluster-config-v1")
		return "", err
	}
	return baseDomain, nil
}

func GetMachineNetworkCIDR(configMap *v1.ConfigMap, log logrus.FieldLogger) (string, error) {
	configStruct := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(configMap.Data["install-config"]), configStruct)
	if err != nil {
		log.WithError(err).Errorf("Failed to unmarshal confimap cluster-config-v1 data: <%s>", configMap.Data["install-config"])
		return "", err
	}
	networking, ok := configStruct["networking"].(map[interface{}]interface{})
	if !ok {
		err := fmt.Errorf("invalid or missing networking key in cluster-config-v1")
		log.WithError(err).Errorf("invalid format for cluster-config-v1")
		return "", err
	}
	machineNetwork, ok := networking["machineNetwork"].([]interface{})
	if !ok {
		err := fmt.Errorf("invalid or missing machineNetwork key in networking in cluster-config-v1")
		log.WithError(err).Errorf("invalid format for cluster-config-v1")
		return "", err
	}
	cidrEntry, ok := machineNetwork[0].(map[interface{}]interface{})
	if !ok {
		err := fmt.Errorf("invalid or missing cidr entry in networking in cluster-config-v1")
		log.WithError(err).Errorf("invalid format for cluster-config-v1")
		return "", err
	}
	cidr, ok := cidrEntry["cidr"].(string)
	if !ok {
		err := fmt.Errorf("invalid or missing cidr key in networking in cluster-config-v1")
		log.WithError(err).Errorf("invalid format for cluster-config-v1")
		return "", err
	}
	return cidr, nil
}

func GetSSHPublicKey(configMap *v1.ConfigMap, log logrus.FieldLogger) (string, error) {
	configStruct := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(configMap.Data["install-config"]), configStruct)
	if err != nil {
		log.WithError(err).Errorf("Failed to unmarshal confimap cluster-config-v1 data: <%s>", configMap.Data["install-config"])
		return "", err
	}
	sshKey, ok := configStruct["sshKey"].(string)
	if !ok {
		err := fmt.Errorf("invalid or missing sshKey key in cluster-config-v1")
		log.WithError(err).Errorf("invalid format for cluster-config-v1")
		return "", err
	}
	return sshKey, nil
}

func GetClusterVersion(clusterVersion *configv1.ClusterVersion) (string, error) {
	openshiftVersion := clusterVersion.Status.Desired.Version
	splits := strings.Split(openshiftVersion, ".")
	return splits[0] + "." + splits[1], nil
}

func IsNodeReady(node *v1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady {
			return condition.Status == v1.ConditionTrue
		}
	}
	return false
}

func GetNodeRole(node *v1.Node) models.HostRole {
	for label := range node.Labels {
		if label == "node-role.kubernetes.io/master" {
			return models.HostRoleMaster
		}
	}
	return models.HostRoleWorker
}

func GetNodeInternalIP(node *v1.Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == v1.NodeInternalIP {
			return address.Address
		}
	}
	return ""
}
func GetDataByKeyFromSecret(secret *v1.Secret, key string) (string, error) {
	data, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("key %s not present in secret %s data", key, secret.Name)
	}
	return string(data), nil
}
