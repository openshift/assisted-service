package common

import (
	"encoding/json"

	"github.com/openshift/assisted-service/models"
)

type ImageStatuses map[string]*models.ContainerImageAvailability

func UnmarshalImageStatuses(imageStatusesStr string) (ImageStatuses, error) {
	var (
		ret = make(ImageStatuses)
		err error
	)
	if imageStatusesStr != "" {
		err = json.Unmarshal([]byte(imageStatusesStr), &ret)
	}
	return ret, err
}

func MarshalImageStatuses(statuses ImageStatuses) (string, error) {
	var (
		b   []byte
		err error
	)
	if b, err = json.Marshal(&statuses); err != nil {
		return "", err
	}
	return string(b), nil
}

func ImageStatusExists(statuses ImageStatuses, name string) bool {
	_, ret := statuses[name]
	return ret
}

func GetImageStatus(statuses ImageStatuses, name string) (*models.ContainerImageAvailability, bool) {
	ret, exists := statuses[name]
	return ret, exists
}

func SetImageStatus(statuses ImageStatuses, status *models.ContainerImageAvailability) {
	statuses[status.Name] = status
}
