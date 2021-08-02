package common

import (
	"encoding/json"

	"github.com/openshift/assisted-service/models"
)

func MarshalInventory(inventory *models.Inventory) (string, error) {
	if data, err := json.Marshal(inventory); err != nil {
		return "", err
	} else {
		return string(data), nil
	}
}

func UnmarshalInventory(inventoryStr string) (*models.Inventory, error) {
	var inventory models.Inventory

	if err := json.Unmarshal([]byte(inventoryStr), &inventory); err != nil {
		return nil, err
	}
	return &inventory, nil
}
