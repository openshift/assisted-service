package subsystem

import (
	"net/url"
	"testing"

	"github.com/filanov/bm-inventory/client"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var db *gorm.DB
var bmclient *client.BMInventory

func init() {
	var err error
	bmclient = client.New(client.Config{
		URL: &url.URL{
			Scheme: client.DefaultSchemes[0],
			Host:   "192.168.39.74:32562",
			Path:   client.DefaultBasePath,
		},
	})

	db, err = gorm.Open("postgres", "host=192.168.39.74 port=30935 user=postgresadmin dbname=postgresdb password=admin123 sslmode=disable")
	if err != nil {
		logrus.Fatal("Fail to connect to DB, ", err)
	}
}

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Subsystem Suite")
}
