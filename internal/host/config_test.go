package host

import (
	"os"
	"time"

	"github.com/kelseyhightower/envconfig"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Host stage timeout configuration", func() {
	DescribeTable(
		"Takes the value from the environment",
		func(stage models.HostStage, envName, envValue string, expected time.Duration) {
			oldValue, ok := os.LookupEnv(envName)
			if ok {
				defer os.Setenv(envName, oldValue)
			} else {
				defer os.Unsetenv(envName)
			}
			os.Setenv(envName, envValue)
			config := &Config{}
			err := envconfig.Process("", config)
			Expect(err).ToNot(HaveOccurred())
			err = config.Complete()
			Expect(err).ToNot(HaveOccurred())
			actual := config.HostStageTimeout(stage)
			Expect(actual).To(Equal(expected))
		},
		Entry(
			"Starting installation",
			models.HostStageStartingInstallation,
			"HOST_STAGE_STARTING_INSTALLATION_TIMEOUT",
			"42s",
			42*time.Second,
		),
		Entry(
			"Waiting for control plane",
			models.HostStageWaitingForControlPlane,
			"HOST_STAGE_WAITING_FOR_CONTROL_PLANE_TIMEOUT",
			"51m",
			51*time.Minute,
		),
	)

	DescribeTable(
		"Takes the value from the defaults",
		func(stage models.HostStage, expected time.Duration) {
			config := &Config{}
			err := envconfig.Process("", config)
			Expect(err).ToNot(HaveOccurred())
			err = config.Complete()
			Expect(err).ToNot(HaveOccurred())
			value := config.HostStageTimeout(stage)
			Expect(value).To(Equal(expected))
		},
		Entry(
			"Starting installation",
			models.HostStageStartingInstallation,
			30*time.Minute,
		),
		Entry(
			"Waiting for control plane",
			models.HostStageWaitingForControlPlane,
			60*time.Minute,
		),
	)

	It("Fails if environment value isn't valid duration", func() {
		const (
			envName  = "HOST_STAGE_STARTING_INSTALLATION_TIMEOUT"
			envValue = "junk"
		)
		oldValue, ok := os.LookupEnv(envName)
		if ok {
			defer os.Setenv(envName, oldValue)
		} else {
			defer os.Unsetenv(envName)
		}
		os.Setenv(envName, envValue)
		config := &Config{}
		err := envconfig.Process("", config)
		Expect(err).ToNot(HaveOccurred())
		err = config.Complete()
		Expect(err).To(HaveOccurred())
		msg := err.Error()
		Expect(msg).To(ContainSubstring(string(models.HostStageStartingInstallation)))
		Expect(msg).To(ContainSubstring(envName))
		Expect(msg).To(ContainSubstring(envValue))
	})

	It("Uses the default for unknown stages", func() {
		config := &Config{}
		err := envconfig.Process("", config)
		Expect(err).ToNot(HaveOccurred())
		value := config.HostStageTimeout(models.HostStage("Doing something"))
		Expect(value).To(Equal(60 * time.Minute))
	})
})
