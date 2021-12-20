package host

import (
	"fmt"

	"github.com/go-openapi/swag"
	gomock "github.com/golang/mock/gomock"
	"github.com/openshift/assisted-service/models"
)

func MatchRole(role models.HostRole) gomock.Matcher {
	return hostRoleMatcher{role}
}

func MatchBootstrap() gomock.Matcher {
	return hostBootstrapMatcher{}
}

func MatchDay2Host() gomock.Matcher {
	return day2Matcher{}
}

type hostRoleMatcher struct {
	role models.HostRole
}

func (m hostRoleMatcher) String() string {
	return fmt.Sprintf("has role %v", m.role)
}
func (m hostRoleMatcher) Matches(x interface{}) bool {
	if x == nil {
		return false
	}
	h, _ := x.(*models.Host)
	return h.Role == m.role
}

type hostBootstrapMatcher struct{}

func (m hostBootstrapMatcher) String() string {
	return "is bootstrap"
}
func (m hostBootstrapMatcher) Matches(x interface{}) bool {
	if x == nil {
		return false
	}
	h, _ := x.(*models.Host)
	return h.Bootstrap
}

type day2Matcher struct{}

func (m day2Matcher) String() string {
	return "is day2 host"
}
func (m day2Matcher) Matches(x interface{}) bool {
	if x == nil {
		return false
	}
	h, _ := x.(*models.Host)
	return swag.StringValue(h.Kind) == models.HostKindAddToExistingClusterHost
}
