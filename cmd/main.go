package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/filanov/bm-inventory/internal/bminventory"
	"github.com/filanov/bm-inventory/restapi"
	"github.com/go-openapi/swag"
	"github.com/sirupsen/logrus"
)

func main() {
	port := flag.String("port", "8090", "define port that the service will listen to")
	flag.Parse()

	bm := bminventory.NewBareMetalInventory()
	h, err := restapi.Handler(restapi.Config{
		InventoryAPI: bm,
		Logger:       logrus.Printf,
	})
	if err != nil {
		logrus.Fatal("Failed to init rest handler,", err)
	}

	logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", swag.StringValue(port)), h))
}
