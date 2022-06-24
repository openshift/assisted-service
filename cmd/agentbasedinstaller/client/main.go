/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

/*
See docs/ephemeral-installer.md for details on how this client
is used.
*/

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/openshift/assisted-service/cmd/agentbasedinstaller"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/client"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
)

const failureOutputPath = "/var/run/agent-installer/host-config-failures"

var Options struct {
	ServiceBaseUrl string `envconfig:"SERVICE_BASE_URL" default:""`
}

var RegisterOptions struct {
	ClusterDeploymentFile   string `envconfig:"CLUSTER_DEPLOYMENT_FILE" default:"/manifests/cluster-deployment.yaml"`
	AgentClusterInstallFile string `envconfig:"AGENT_CLUSTER_INSTALL_FILE" default:"/manifests/agent-cluster-install.yaml"`
	InfraEnvFile            string `envconfig:"INFRA_ENV_FILE" default:"/manifests/infraenv.yaml"`
	PullSecretFile          string `envconfig:"PULL_SECRET_FILE" default:"/manifests/pull-secret.yaml"`
	ClusterImageSetFile     string `envconfig:"CLUSTER_IMAGE_SET_FILE" default:"/manifests/cluster-image-set.yaml"`
	NMStateConfigFile       string `envconfig:"NMSTATE_CONFIG_FILE" default:"/manifests/nmstateconfig.yaml"`
	ImageTypeISO            string `envconfig:"IMAGE_TYPE_ISO" default:"full-iso"`
	ReleaseImageMirror      string `envconfig:"OPENSHIFT_INSTALL_RELEASE_IMAGE_MIRROR" default:""`
}

var ConfigureOptions struct {
	InfraEnvID    string `envconfig:"INFRA_ENV_ID" default:""`
	HostConfigDir string `envconfig:"HOST_CONFIG_DIR" default:"/etc/assisted/hostconfig"`
}

func main() {
	err := envconfig.Process("", &Options)
	log := log.New()
	if err != nil {
		log.Fatal(err.Error())
	}

	clientConfig := client.Config{}
	u, parseErr := url.Parse(Options.ServiceBaseUrl)
	if parseErr != nil {
		log.Fatal(parseErr, "Failed parsing inventory URL")
	}
	u.Path = path.Join(u.Path, client.DefaultBasePath)
	clientConfig.URL = u
	bmInventory := client.New(clientConfig)
	ctx := context.Background()
	log.Info("SERVICE_BASE_URL: " + Options.ServiceBaseUrl)

	// TODO: This is for backward compatibility and should be removed once the
	// ephemeral ISO services are using the subcommands.
	if path.Base(os.Args[0]) == "agent-based-installer-register-cluster-and-infraenv" {
		register(ctx, log, bmInventory)
		return
	}

	if len(os.Args) < 2 {
		log.Fatal("No subcommand specified")
	}
	switch os.Args[1] {
	case "register":
		infraEnvID := register(ctx, log, bmInventory)
		os.WriteFile("/etc/assisted/client_config", []byte("INFRA_ENV_ID="+infraEnvID), 0644)
	case "configure":
		configure(ctx, log, bmInventory)
	default:
		log.Fatalf("Unknown subcommand %s", os.Args[1])
	}
}

func register(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall) string {
	err := envconfig.Process("", &RegisterOptions)
	if err != nil {
		log.Fatal(err.Error())
	}

	var secret corev1.Secret
	if secretErr := agentbasedinstaller.GetFileData(RegisterOptions.PullSecretFile, &secret); secretErr != nil {
		log.Fatal(secretErr.Error())
	}
	pullSecret := secret.StringData[".dockerconfigjson"]

	log.Info("Registering cluster")

	modelsCluster, registerClusterErr := agentbasedinstaller.RegisterCluster(ctx, log, bmInventory, pullSecret,
		RegisterOptions.ClusterDeploymentFile, RegisterOptions.AgentClusterInstallFile, RegisterOptions.ClusterImageSetFile, RegisterOptions.ReleaseImageMirror)
	if registerClusterErr != nil {
		log.Fatal(registerClusterErr, "Failed to register cluster with assisted-service")
	}

	log.Info("Registered cluster with id: " + modelsCluster.ID.String())

	log.Info("Registering infraenv")

	modelsInfraEnv, registerInfraEnvErr := agentbasedinstaller.RegisterInfraEnv(ctx, log, bmInventory, pullSecret,
		modelsCluster, RegisterOptions.InfraEnvFile, RegisterOptions.NMStateConfigFile, RegisterOptions.ImageTypeISO)
	if registerInfraEnvErr != nil {
		log.Fatal(registerInfraEnvErr, "Failed to register infraenv with assisted-service")
	}

	infraEnvID := modelsInfraEnv.ID.String()
	log.Info("Registered infraenv with id: " + infraEnvID)

	return infraEnvID
}

func configure(ctx context.Context, log *log.Logger, bmInventory *client.AssistedInstall) {
	err := envconfig.Process("", &ConfigureOptions)
	if err != nil {
		log.Fatal(err.Error())
	}

	if ConfigureOptions.InfraEnvID == "" {
		log.Fatal("No INFRA_ENV_ID specified")
	}

	hostConfigs, err := agentbasedinstaller.LoadHostConfigs(ConfigureOptions.HostConfigDir)
	if err != nil {
		log.Fatal("Failed to load host configuration: ", err)
	}

	done := false
	sleepTime := 1 * time.Second
	for !done {
		failures, err := agentbasedinstaller.ApplyHostConfigs(ctx, log, bmInventory, hostConfigs, strfmt.UUID(ConfigureOptions.InfraEnvID))
		if err != nil {
			log.Fatal("Failed to apply host configuration: ", err)
		}
		if len(failures) > 0 {
			log.Infof("Sleeping for %v", sleepTime)
			time.Sleep(sleepTime)
			if sleepTime < (30 * time.Second) {
				sleepTime = sleepTime * 2
			}
		} else {
			done = true
		}
		if err := recordFailures(failures); err != nil {
			log.Fatal("Unable to record failures to disk: ", err)
		}
	}
	log.Info("Configured all hosts")
}

func recordFailures(failures []agentbasedinstaller.Failure) error {
	if len(failures) == 0 {
		err := os.Remove(failureOutputPath)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(path.Dir(failureOutputPath), 0644); err != nil {
		return err
	}

	messages := make([]string, len(failures))
	for i, f := range failures {
		messages[i] = fmt.Sprintf("%s: %s\n", f.Hostname(), f.DescribeFailure())
	}

	return ioutil.WriteFile(
		failureOutputPath,
		[]byte(strings.Join(messages, "")),
		0644)
}
