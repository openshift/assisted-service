package subsystem

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/sirupsen/logrus"
)

var db *gorm.DB
var bmclient *client.AssistedInstall
var log *logrus.Logger

var Options struct {
	DBHost        string `envconfig:"DB_HOST"`
	DBPort        string `envconfig:"DB_PORT"`
	InventoryHost string `envconfig:"INVENTORY"`
}

func init() {
	var err error
	log = logrus.New()
	log.SetReportCaller(true)
	err = envconfig.Process("subsystem", &Options)
	if err != nil {
		log.Fatal(err.Error())
	}

	bmclient = client.New(client.Config{
		URL: &url.URL{
			Scheme: client.DefaultSchemes[0],
			Host:   Options.InventoryHost,
			Path:   client.DefaultBasePath,
		},
	})

	db, err = gorm.Open("postgres",
		fmt.Sprintf("host=%s port=%s user=admin dbname=installer password=admin sslmode=disable",
			Options.DBHost, Options.DBPort))
	if err != nil {
		logrus.Fatal("Fail to connect to DB, ", err)
	}
}

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	clearDB()
	RunSpecs(t, "Subsystem Suite")
}
