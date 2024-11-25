package testing

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/openshift/assisted-service/internal/common"
)

var ValidOCPVersionForNonStandardHAOCPControlPlane = func(majorMinorOCPVersion string) string {
	splittedVersion := strings.Split(majorMinorOCPVersion, ".")
	intVersion, _ := strconv.Atoi(splittedVersion[1])
	return fmt.Sprintf("%s.%d", splittedVersion[0], intVersion-1)
}(common.MinimumVersionForNonStandardHAOCPControlPlane)
