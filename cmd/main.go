package main

import (
	"net/http"

	"github.com/filanov/bm-inventory/internal/bminventory"
	"github.com/filanov/bm-inventory/restapi"
	"github.com/sirupsen/logrus"
)

func main() {
	bm := bminventory.NewBareMetalInventory()
	h, err := restapi.Handler(restapi.Config{
		InventoryAPI: bm,
		Logger:       logrus.Printf,
	})
	if err != nil {
		logrus.Fatal("Failed to init rest handler,", err)
	}

	logrus.Fatal(http.ListenAndServe(":8090", h))
}
