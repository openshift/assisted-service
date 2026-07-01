package hostutil

import (
	"testing"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

func TestGetIgnitionEndpointAndCertUnbracketedIPv6CustomEndpoint(t *testing.T) {
	customEndpoint := "https://fd2e:6f44:5dd8:c956::14:31187"
	cluster := common.Cluster{
		Cluster: models.Cluster{
			IgnitionEndpoint: &models.IgnitionEndpoint{URL: &customEndpoint},
		},
	}
	host := models.Host{Role: models.HostRoleWorker}

	url, cert, err := GetIgnitionEndpointAndCert(&cluster, &host, logrus.New())
	if err != nil {
		t.Fatalf("GetIgnitionEndpointAndCert returned error: %v", err)
	}
	if cert != nil {
		t.Fatalf("expected no certificate, got %v", cert)
	}
	want := "https://[fd2e:6f44:5dd8:c956::14]:31187/worker"
	if url != want {
		t.Fatalf("got URL %q, want %q", url, want)
	}
}
