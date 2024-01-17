package releasesources

type Config struct {
	OpenshiftMajorVersion           string `envconfig:"OPENSHIFT_MAJOR_VERSION" default:"4"`
	OpenshiftReleasesAPIBaseUrl     string `envconfig:"OPENSHIFT_RELEASE_API_BASE_URL" default:"https://api.openshift.com/api/upgrades_info/v1/graph"`
	OpenshiftSupportLevelAPIBaseUrl string `envconfig:"OPENSHIFT_SUPPORT_LEVEL_API_BASE_URL" default:"https://access.redhat.com/product-life-cycles/api/v1/products"`
}
