package testing

import (
	"fmt"

	"github.com/golang/mock/gomock"
	"github.com/openshift/assisted-service/models"
)

type eqPlatformTypeMatcher struct {
	platformType models.PlatformType
}

func (e eqPlatformTypeMatcher) Matches(x interface{}) bool {
	platform, ok := x.(*models.Platform)
	if !ok {
		return false
	}

	toto := (e.platformType == *platform.Type)
	return toto
}

func (e eqPlatformTypeMatcher) String() string {
	return fmt.Sprintf("matches platform type %v", e.platformType)
}

func (e eqPlatformTypeMatcher) Got(got interface{}) string {
	platform, ok := got.(*models.Platform)
	if !ok {
		return "not a platform"
	}

	return "platform type " + string(*platform.Type)
}

// EqPlatformType returns a matcher able to match the platform type inside a platform parameter.
func EqPlatformType(platformType models.PlatformType) gomock.Matcher {
	return eqPlatformTypeMatcher{platformType}
}
