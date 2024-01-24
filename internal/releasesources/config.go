package releasesources

import "time"

type Config struct {
	OCMBaseURL                           string        `envconfig:"OCM_BASE_URL" default:""`
	ReleaseSources                       string        `envconfig:"RELEASE_SOURCES" default:""`
	OpenShiftReleaseSyncerInterval       time.Duration `envconfig:"OPENSHIFT_RELEASE_SYNCER_INTERVAL" default:"30m"`
	RedHatProductLifeCycleDataAPIBaseURL string        `envconfig:"RED_HAT_PRODUCT_LIFE_CYCLE_DATA_API_BASE_URL" default:"https://access.redhat.com/product-life-cycles/api/v1/products"`
}
