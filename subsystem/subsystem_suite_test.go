package subsystem

import (
	"fmt"
	"net/url"
	"testing"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/filanov/bm-inventory/client"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var db *gorm.DB
var bmclient *client.AssistedInstall
var log *logrus.Logger

var Options struct {
	DBHost        string `envconfig:"DB_HOST"`
	DBPort        string `envconfig:"DB_PORT"`
	EnableAuth    bool   `envconfig:"ENABLE_AUTH"`
	InventoryHost string `envconfig:"INVENTORY"`
}

func init() {
	var err error
	agentKey := "X-Secret-Key"
	agentKeyValue := "SecretKey"
	//userKey := "Authorization"
	//userKeyValue := "userKey"
	log = logrus.New()
	log.SetReportCaller(true)
	err = envconfig.Process("subsystem", &Options)
	if err != nil {
		log.Fatal(err.Error())
	}
	cfg := client.Config{
		URL: &url.URL{
			Scheme: client.DefaultSchemes[0],
			Host:   Options.InventoryHost,
			Path:   client.DefaultBasePath,
		},
	}
	if Options.EnableAuth {
		log.Info("API Key authentication enabled for subsystem tests")
		clientAuth := func() runtime.ClientAuthInfoWriter {
			return runtime.ClientAuthInfoWriterFunc(func(r runtime.ClientRequest, _ strfmt.Registry) error {
				return r.SetHeaderParam(agentKey, agentKeyValue)
			})
		}
		cfg.AuthInfo = clientAuth()
	}
	bmclient = client.New(cfg)

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
