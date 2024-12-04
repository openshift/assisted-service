package infraenv

import (
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

func GetInfraEnvIsoImageType(log logrus.FieldLogger, cpuArchitecture string, paramImageType, defaultImageType models.ImageType) models.ImageType {
	// set the default value in case it was not provided in the request

	if paramImageType == "" {
		if cpuArchitecture == models.ClusterCPUArchitectureS390x {
			log.Infof("Found Z architecture, updating ISO image type to %s", models.ImageTypeFullIso)
			return models.ImageTypeFullIso
		} else {
			return defaultImageType
		}
	}

	return paramImageType
}
