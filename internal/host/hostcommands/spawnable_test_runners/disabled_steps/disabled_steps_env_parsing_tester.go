package main

import (
	"os"
	"strings"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/internal/host/hostcommands"
	"github.com/openshift/assisted-service/models"
)

func main() {
	var config hostcommands.InstructionConfig
	err := envconfig.Process("", &config)
	if err != nil {
		panic(err)
	}
	env, exists := os.LookupEnv("DISABLED_STEPS")
	if !exists {
		if len(config.DisabledSteps) != 0 {
			panic("DISABLED_STEPS Values are not parsed correctly. DisabledSteps should be empty")
		}
		return
	}
	var envVarParsed = strings.Split(env, ",")
	if len(envVarParsed) != len(config.DisabledSteps) {
		panic("DISABLED_STEPS Values are not parsed correctly. DisabledSteps and env variable have different length")
	}
	for i := range envVarParsed {
		if models.StepType(envVarParsed[i]) != config.DisabledSteps[i] {
			panic("DISABLED_STEPS Values are not parsed correctly")
		}
	}
}
