package cnv

import (
	"strings"
)

type SupportedGPUsDecoder map[string]bool

type Config struct {
	// List of supported GPUs: https://issues.redhat.com/browse/CNV-7749
	SupportedGPUs SupportedGPUsDecoder `envconfig:"CNV_SUPPORTED_GPUS" default:"10de:1db6,10de:1eb8"`
}

func (d *SupportedGPUsDecoder) Decode(value string) error {
	supportedGPUsSet := make(SupportedGPUsDecoder)
	*d = supportedGPUsSet

	if strings.TrimSpace(value) == "" {
		return nil
	}
	devices := strings.Split(value, ",")

	for _, device := range devices {
		supportedGPUsSet[strings.ToLower(device)] = true
	}
	return nil
}
