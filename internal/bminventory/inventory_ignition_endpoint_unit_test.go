package bminventory

import (
	"testing"

	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

func TestValidateIgnitionEndpointNormalizesUnbracketedIPv6(t *testing.T) {
	bm := &bareMetalInventory{}
	url := "https://fd2e:6f44:5dd8:c956::14:31187"
	ignitionEndpoint := &models.IgnitionEndpoint{URL: &url}

	if err := bm.validateIgnitionEndpoint(ignitionEndpoint, logrus.New()); err != nil {
		t.Fatalf("validateIgnitionEndpoint returned error: %v", err)
	}
	want := "https://[fd2e:6f44:5dd8:c956::14]:31187"
	if *ignitionEndpoint.URL != want {
		t.Fatalf("got URL %q, want %q", *ignitionEndpoint.URL, want)
	}
}
