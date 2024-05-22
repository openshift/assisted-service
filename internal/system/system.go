package system

import (
	"os"
	"strings"
)

const (
	fipsFile = "/proc/sys/crypto/fips_enabled"
)

//go:generate mockgen -source=system.go -package=system -destination=mock_system.go
type SystemInfo interface {
	FIPSEnabled() (bool, error)
}

type fileReader func(name string) ([]byte, error)

type localSystemInfo struct {
	fileReader fileReader
}

var _ SystemInfo = &localSystemInfo{}

func NewLocalSystemInfo() SystemInfo {
	return &localSystemInfo{
		fileReader: os.ReadFile,
	}
}

func (s *localSystemInfo) FIPSEnabled() (bool, error) {
	content, err := s.fileReader(fipsFile)
	if err != nil {
		// if the FIPS file doesn't exist then it surely is not enabled
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	if strings.TrimSpace(string(content)) == "1" {
		return true, nil
	}

	return false, nil
}
