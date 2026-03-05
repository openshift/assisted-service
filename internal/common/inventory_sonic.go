//go:build amd64 || arm64

package common

import (
	"github.com/bytedance/sonic"

	"github.com/openshift/assisted-service/models"
)

func MarshalInventory(inventory *models.Inventory) (string, error) {
	if data, err := sonic.Marshal(inventory); err != nil {
		return "", err
	} else {
		return string(data), nil
	}
}

func UnmarshalInventory(inventoryStr string) (*models.Inventory, error) {
	var inventory models.Inventory

	if err := sonic.Unmarshal([]byte(inventoryStr), &inventory); err != nil {
		return nil, err
	}
	return &inventory, nil
}
