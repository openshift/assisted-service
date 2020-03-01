package main

import (
	"flag"
	"fmt"
	"net/http"

	"github.com/filanov/bm-inventory/internal/bminventory"
	"github.com/filanov/bm-inventory/models"
	"github.com/filanov/bm-inventory/restapi"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/sirupsen/logrus"
)

func main() {
	port := flag.String("port", "8090", "define port that the service will listen to")
	flag.Parse()

	logrus.Println("Starting bm service")

	db, err := gorm.Open("postgres", "host=172.17.0.7 port=5432 user=postgresadmin dbname=postgresdb password=admin123 sslmode=disable")
	if err != nil {
		logrus.Error("Fail to connect to DB, ", err)
	}
	defer db.Close()

	if err := db.AutoMigrate(&models.Image{}, &models.Node{}, &models.Cluster{}).Error; err != nil {
		logrus.Fatal("failed to auto migrate, ", err)
	}

	bm := bminventory.NewBareMetalInventory(db)
	h, err := restapi.Handler(restapi.Config{
		InventoryAPI: bm,
		Logger:       logrus.Printf,
	})
	if err != nil {
		logrus.Fatal("Failed to init rest handler,", err)
	}

	logrus.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", swag.StringValue(port)), h))
}
