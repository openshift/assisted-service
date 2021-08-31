/*
Copyright 2021.

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

package controllers

import (
	"os"
)

const (
	OpenshiftVersionsEnvVar string = "OPENSHIFT_VERSIONS"
)

func ServiceImage() string {
	return getEnvVar("SERVICE_IMAGE", "quay.io/ocpmetal/assisted-service:latest")
}

func DatabaseImage() string {
	return getEnvVar("DATABASE_IMAGE", "quay.io/ocpmetal/postgresql-12-centos7:latest")
}

func AgentImage() string {
	return getEnvVar("AGENT_IMAGE", "quay.io/ocpmetal/assisted-installer-agent:latest")
}

func ControllerImage() string {
	return getEnvVar("CONTROLLER_IMAGE", "quay.io/ocpmetal/assisted-installer-controller:latest")
}

func InstallerImage() string {
	return getEnvVar("INSTALLER_IMAGE", "quay.io/ocpmetal/assisted-installer:latest")
}

func ServiceAccountName() string {
	return getEnvVar("SERVICE_ACCOUNT_NAME", "default")
}

// This is left blank so that we don't include the json string in source
// it should always be specified on the CSV (and operator deployment)
// and is enforced in cmd/operator/main.go
func OpenshiftVersions() string {
	return getEnvVar(OpenshiftVersionsEnvVar, "")
}

func OSImages() string {
	return getEnvVar("OS_IMAGES", "")
}

func MustGatherImages() string {
	return getEnvVar("MUST_GATHER_IMAGES", "")
}

func getEnvVar(key, def string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return def
}
