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
	if platform, ok := x.(*models.Platform); ok {
		return e.platformType == *platform.Type
	}

	return false
}

func (e eqPlatformTypeMatcher) String() string {
	return fmt.Sprintf("matches platform type %v", e.platformType)
}

func (e eqPlatformTypeMatcher) Got(got interface{}) string {
	if platform, ok := got.(*models.Platform); ok {
		return "platform type " + string(*platform.Type)
	}

	return "not a platform"
}

// EqPlatformType returns a matcher able to match the platform type inside a platform parameter.
func EqPlatformType(platformType models.PlatformType) gomock.Matcher {
	return eqPlatformTypeMatcher{platformType}
}
