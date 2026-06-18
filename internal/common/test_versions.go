package common

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/go-version"
)

type testOsImage struct {
	Version string
	URL     string
}

type testReleaseImage struct {
	Version string
	URL     string
}

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

func (b *TestVersionBuilder) AvailableForArches(arches ...string) *TestVersionBuilder {
	b.constraints = append(b.constraints, func(v string) bool {
		for _, arch := range arches {
			archImages, ok := testOsImagesByArch[arch]
			if !ok {
				return false
			}
			if _, ok := archImages[v]; !ok {
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
	v := b.Version()
	if img, ok := b.lookupReleaseImage(b.arch, v); ok {
		return img.Version
	}
	// Fall back to multi-arch release URL if no standalone release exists for the selected arch
	if img, ok := b.lookupReleaseImage(MultiCPUArchitecture, v); ok {
		return strings.TrimSuffix(img.Version, "-"+MultiCPUArchitecture)
	}
	return v + ".0"
}

func (b *TestVersionBuilder) MultiVersion() string {
	return b.Version() + "-" + MultiCPUArchitecture
}

func (b *TestVersionBuilder) MultiReleaseVersion() string {
	v := b.Version()
	if img, ok := b.lookupReleaseImage(MultiCPUArchitecture, v); ok {
		return img.Version
	}
	return b.ReleaseVersion() + "-" + MultiCPUArchitecture
}

func (b *TestVersionBuilder) ReleaseImageURL() string {
	v := b.Version()
	if img, ok := b.lookupReleaseImage(b.arch, v); ok {
		return img.URL
	}
	// Fall back to multi-arch release URL if no standalone release exists for the selected arch
	if img, ok := b.lookupReleaseImage(MultiCPUArchitecture, v); ok {
		return img.URL
	}
	// Quay.io tags use "aarch64" while the service internally uses "arm64"
	// Mirrors getReleaseImageReference in internal/releasesources/release_sources.go
	suffix := b.arch
	if suffix == ARM64CPUArchitecture {
		suffix = AARCH64CPUArchitecture
	}
	return fmt.Sprintf("quay.io/openshift-release-dev/ocp-release:%s-%s", b.ReleaseVersion(), suffix)
}

func (b *TestVersionBuilder) MultiReleaseImageURL() string {
	v := b.Version()
	if img, ok := b.lookupReleaseImage(MultiCPUArchitecture, v); ok {
		return img.URL
	}
	return fmt.Sprintf("quay.io/openshift-release-dev/ocp-release:%s-%s", b.ReleaseVersion(), MultiCPUArchitecture)
}

func (b *TestVersionBuilder) RhcosImageURL() string {
	v := b.Version()
	if archImages, ok := testOsImagesByArch[b.arch]; ok {
		if img, ok := archImages[v]; ok {
			return img.URL
		}
	}
	return fmt.Sprintf("https://mirror.openshift.com/pub/openshift-v4/%s/dependencies/rhcos/%s/%s/rhcos-%s-%s-live.%s.iso",
		b.arch, v, b.RhcosVersion(), b.RhcosVersion(), b.arch, b.arch)
}

func (b *TestVersionBuilder) RhcosVersion() string {
	v := b.Version()
	if archImages, ok := testOsImagesByArch[b.arch]; ok {
		if img, ok := archImages[v]; ok {
			return img.Version
		}
	}
	return fmt.Sprintf("version-%s.123-0", strings.ReplaceAll(v, ".", ""))
}

func (b *TestVersionBuilder) versions() []string {
	archImages, ok := testOsImagesByArch[b.arch]
	if !ok {
		return []string{}
	}
	all := make([]string, 0, len(archImages))
	for v := range archImages {
		all = append(all, v)
	}
	sort.Slice(all, func(i, j int) bool {
		vi, _ := version.NewVersion(all[i])
		vj, _ := version.NewVersion(all[j])
		return vi.LessThan(vj)
	})
	if len(b.constraints) == 0 {
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

func (b *TestVersionBuilder) lookupReleaseImage(arch, v string) (testReleaseImage, bool) {
	if archImages, ok := testReleaseImagesByArch[arch]; ok {
		if img, ok := archImages[v]; ok {
			return img, true
		}
	}
	return testReleaseImage{}, false
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
