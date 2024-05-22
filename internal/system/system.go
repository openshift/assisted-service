package system

import "os"

const (
	fipsFile = "/proc/sys/crypto/fips_enabled"
)

//go:generate mockgen -source=system.go -package=system -destination=mock_system.go
type SystemInfo interface {
	FIPSEnabled() (bool, error)
}

type localSystemInfo struct{}

var _ SystemInfo = &localSystemInfo{}

func NewLocalSystemInfo() SystemInfo {
	return &localSystemInfo{}
}

func (s *localSystemInfo) FIPSEnabled() (bool, error) {
	content, err := os.ReadFile(fipsFile)
	if err != nil {
		return false, err
	}

	if string(content) == "1" {
		return true, nil
	}

	return false, nil
}
