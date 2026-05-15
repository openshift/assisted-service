package common

import (
	"fmt"
	"strings"

	"github.com/hashicorp/go-version"
)

type TestVersionBuilder struct {
	arch           string
	oldest         bool
	constraint     func(string) bool
	versionsByArch map[string][]string
}

func TestVersion() *TestVersionBuilder {
	return &TestVersionBuilder{
		arch:           DefaultCPUArchitecture,
		versionsByArch: testVersionsByArch,
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
	b.constraint = func(v string) bool {
		return b.parseVersion(v).LessThan(parsed)
	}
	return b
}

func (b *TestVersionBuilder) GreaterThan(threshold string) *TestVersionBuilder {
	parsed := b.parseVersion(threshold)
	b.constraint = func(v string) bool {
		return b.parseVersion(v).GreaterThan(parsed)
	}
	return b
}

func (b *TestVersionBuilder) Exact(target string) *TestVersionBuilder {
	parsed := b.parseVersion(target)
	b.constraint = func(v string) bool {
		return b.parseVersion(v).Equal(parsed)
	}
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

// Mirrors getReleaseImageReference in internal/releasesources/release_sources.go
func (b *TestVersionBuilder) ReleaseImageURL() string {
	suffix := b.arch
	if suffix == ARM64CPUArchitecture {
		suffix = AARCH64CPUArchitecture
	}
	return fmt.Sprintf("quay.io/openshift-release-dev/ocp-release:%s-%s", b.ReleaseVersion(), suffix)
}

func (b *TestVersionBuilder) RhcosImage() string {
	return fmt.Sprintf("rhcos_%s", b.ReleaseVersion())
}

func (b *TestVersionBuilder) RhcosVersion() string {
	return fmt.Sprintf("version-%s.123-0", strings.ReplaceAll(b.Version(), ".", ""))
}

func (b *TestVersionBuilder) versions() []string {
	all, ok := testVersionsByArch[b.arch]
	if !ok {
		return []string{}
	}
	if b.constraint == nil || len(all) == 0 {
		return all
	}
	var filtered []string
	for _, v := range all {
		if b.constraint(v) {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

func (b *TestVersionBuilder) parseVersion(v string) *version.Version {
	parsed, err := version.NewVersion(v)
	if err != nil {
		panic(fmt.Sprintf("invalid version %q: %v", v, err))
	}
	return parsed
}
