package common

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/go-version"
)

type TestVersionBuilder struct {
	arch        string
	oldest      bool
	constraints []func(string) bool
}

func TestVersion() *TestVersionBuilder {
	return &TestVersionBuilder{
		arch: DefaultCPUArchitecture,
	}
}

func (b *TestVersionBuilder) ForArch(arch string) *TestVersionBuilder {
	b.arch = arch
	return b
}

func (b *TestVersionBuilder) Latest() *TestVersionBuilder {
	b.oldest = false
	return b
}

func (b *TestVersionBuilder) Oldest() *TestVersionBuilder {
	b.oldest = true
	return b
}

func (b *TestVersionBuilder) LessThan(threshold string) *TestVersionBuilder {
	parsed := b.parseVersion(threshold)
	b.constraints = append(b.constraints, func(v string) bool {
		return b.parseVersion(v).LessThan(parsed)
	})
	return b
}

func (b *TestVersionBuilder) LessThanOrEqual(threshold string) *TestVersionBuilder {
	parsed := b.parseVersion(threshold)
	b.constraints = append(b.constraints, func(v string) bool {
		return !b.parseVersion(v).GreaterThan(parsed)
	})
	return b
}

func (b *TestVersionBuilder) GreaterThan(threshold string) *TestVersionBuilder {
	parsed := b.parseVersion(threshold)
	b.constraints = append(b.constraints, func(v string) bool {
		return b.parseVersion(v).GreaterThan(parsed)
	})
	return b
}

func (b *TestVersionBuilder) GreaterThanOrEqual(threshold string) *TestVersionBuilder {
	parsed := b.parseVersion(threshold)
	b.constraints = append(b.constraints, func(v string) bool {
		return !b.parseVersion(v).LessThan(parsed)
	})
	return b
}

// Constrains to versions that also exist in each of the given arches
func (b *TestVersionBuilder) AvailableForArches(arches ...string) *TestVersionBuilder {
	b.constraints = append(b.constraints, func(v string) bool {
		for _, arch := range arches {
			archVersions, ok := testVersionsByArch[arch]
			if !ok {
				return false
			}
			if !slices.Contains(archVersions, v) {
				return false
			}
		}
		return true
	})
	return b
}

func (b *TestVersionBuilder) Exact(target string) *TestVersionBuilder {
	parsed := b.parseVersion(target)
	b.constraints = append(b.constraints, func(v string) bool {
		return b.parseVersion(v).Equal(parsed)
	})
	return b
}

func (b *TestVersionBuilder) TryVersion() (string, bool) {
	v := b.versions()
	if len(v) == 0 {
		return "", false
	}
	if b.oldest {
		return v[0], true
	}
	return v[len(v)-1], true
}

func (b *TestVersionBuilder) Version() string {
	v, ok := b.TryVersion()
	if !ok {
		panic(fmt.Sprintf("no test version found for arch %q with active constraint", b.arch))
	}
	return v
}

func (b *TestVersionBuilder) ReleaseVersion() string {
	return b.Version() + ".0"
}

// Returns Version() with "-multi" appended, matching how createReleaseImage
// stores multi-arch OpenshiftVersion fields in the database.
func (b *TestVersionBuilder) MultiVersion() string {
	return b.Version() + "-" + MultiCPUArchitecture
}

// Returns ReleaseVersion() with "-multi" appended, matching how createReleaseImage
// stores multi-arch Version fields in the database.
func (b *TestVersionBuilder) MultiReleaseVersion() string {
	return b.ReleaseVersion() + "-" + MultiCPUArchitecture
}

// Mirrors getReleaseImageReference in internal/releasesources/release_sources.go
func (b *TestVersionBuilder) ReleaseImageURL() string {
	// Quay.io tags use "aarch64" while the service internally uses "arm64"
	suffix := b.arch
	if suffix == ARM64CPUArchitecture {
		suffix = AARCH64CPUArchitecture
	}
	return fmt.Sprintf("quay.io/openshift-release-dev/ocp-release:%s-%s", b.ReleaseVersion(), suffix)
}

func (b *TestVersionBuilder) RhcosImageURL() string {
	return fmt.Sprintf("https://mirror.openshift.com/pub/openshift-v4/%s/dependencies/rhcos/%s/%s/rhcos-%s-%s-live.%s.iso",
		b.arch, b.Version(), b.RhcosVersion(), b.RhcosVersion(), b.arch, b.arch)
}

func (b *TestVersionBuilder) RhcosVersion() string {
	return fmt.Sprintf("version-%s.123-0", strings.ReplaceAll(b.Version(), ".", ""))
}

func (b *TestVersionBuilder) versions() []string {
	all, ok := testVersionsByArch[b.arch]
	if !ok {
		return []string{}
	}
	if len(b.constraints) == 0 || len(all) == 0 {
		return all
	}
	var filtered []string
	for _, v := range all {
		if b.passesConstraints(v) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

func (b *TestVersionBuilder) passesConstraints(v string) bool {
	for _, c := range b.constraints {
		if !c(v) {
			return false
		}
	}
	return true
}

func (b *TestVersionBuilder) parseVersion(v string) *version.Version {
	parsed, err := version.NewVersion(v)
	if err != nil {
		panic(fmt.Sprintf("invalid version %q: %v", v, err))
	}
	return parsed
}
