package controllers

import (
	"fmt"
	"regexp"
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

const (
	databaseImagePG13Env     = "DATABASE_IMAGE_PG13"
	databaseImagePG15Env     = "DATABASE_IMAGE_PG15"
	databaseImageVersionEnv  = "DATABASE_IMAGE_VERSION"
	defaultDatabaseImagePG13 = "quay.io/sclorg/postgresql-13-c9s:latest"
	defaultDatabaseImagePG15 = "quay.io/sclorg/postgresql-15-c9s:latest"
)

// postgresUpgradeHop is one sclorg image in the major-version path. Each image
// can only upgrade from POSTGRESQL_PREV_VERSION baked into that image.
type postgresUpgradeHop struct {
	version      string
	envVar       string
	defaultImage string
}

// postgresUpgradePath is the sclorg hop chain excluding the current DATABASE_IMAGE.
// RHEL 9 skips PG14, so the supported path is 12 → 13 → 15 → 16.
func postgresUpgradePath() []postgresUpgradeHop {
	return []postgresUpgradeHop{
		{version: "13", envVar: databaseImagePG13Env, defaultImage: defaultDatabaseImagePG13},
		{version: "15", envVar: databaseImagePG15Env, defaultImage: defaultDatabaseImagePG15},
	}
}

var postgresVersionFromImage = regexp.MustCompile(`postgresql-(\d+)`)

// DatabaseImagePG13 is the PostgreSQL 13 hop image used to upgrade PG12 data.
func DatabaseImagePG13() string {
	return getEnvVar(databaseImagePG13Env, defaultDatabaseImagePG13)
}

// DatabaseImagePG15 is the PostgreSQL 15 hop image used to upgrade PG13 data.
func DatabaseImagePG15() string {
	return getEnvVar(databaseImagePG15Env, defaultDatabaseImagePG15)
}

func (h postgresUpgradeHop) image() string {
	return getEnvVar(h.envVar, h.defaultImage)
}

// parsePostgresMajorVersion extracts N from a postgresql-N image reference.
func parsePostgresMajorVersion(image string) (int, bool) {
	m := postgresVersionFromImage.FindStringSubmatch(image)
	if len(m) < 2 {
		return 0, false
	}
	v, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return v, true
}

// databaseImageMajorVersion is the major version of DATABASE_IMAGE.
// Prefer a postgresql-N token in the image reference; fall back to
// DATABASE_IMAGE_VERSION for digest-only mirrors.
func databaseImageMajorVersion() (int, bool) {
	if v, ok := parsePostgresMajorVersion(DatabaseImage()); ok {
		return v, true
	}
	if v := getEnvVar(databaseImageVersionEnv, ""); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			return n, true
		}
	}
	return 0, false
}

// postgresHopsBeforeTarget returns sclorg hop images older than DATABASE_IMAGE
// so EUS skip-upgrades can walk 12 → 13 → 15 → 16.
func postgresHopsBeforeTarget() []postgresUpgradeHop {
	targetVer, ok := databaseImageMajorVersion()
	if !ok {
		return nil
	}
	var hops []postgresUpgradeHop
	for _, h := range postgresUpgradePath() {
		hv, err := strconv.Atoi(h.version)
		if err != nil {
			continue
		}
		if hv < targetVer {
			hops = append(hops, h)
		}
	}
	return hops
}

// postgresUpgradeInitContainers builds one-shot init containers that run each
// hop in postgresHopsBeforeTarget against the postgres PVC, then exit.
func postgresUpgradeInitContainers(env []corev1.EnvVar, mounts []corev1.VolumeMount, resources corev1.ResourceRequirements) []corev1.Container {
	hops := postgresHopsBeforeTarget()
	init := make([]corev1.Container, 0, len(hops))
	for _, h := range hops {
		init = append(init, corev1.Container{
			Name:         fmt.Sprintf("postgres-upgrade-to-%s", h.version),
			Image:        h.image(),
			Command:      []string{"/bin/bash", "-c"},
			Args:         []string{PostgresHopScript},
			Env:          env,
			VolumeMounts: mounts,
			Resources:    resources,
		})
	}
	return init
}
