package common

import (
	json "github.com/bytedance/sonic"

	"github.com/openshift/assisted-service/models"
)

func MarshalInventory(inventory *models.Inventory) (string, error) {
	if data, err := json.ConfigStd.Marshal(inventory); err != nil {
		return "", err
	} else {
		return string(data), nil
	}
}

func UnmarshalInventory(inventoryStr string) (*models.Inventory, error) {
	var inventory models.Inventory

	if err := json.ConfigDefault.Unmarshal([]byte(inventoryStr), &inventory); err != nil {
		return nil, err
	}
	return &inventory, nil
}
