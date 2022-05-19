package cluster

import (
	"fmt"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("Network type matches high availability mode", func() {
	tests := []struct {
		highAvailabilityMode *string
		networkType          *string
		invalid              bool
	}{
		{
			highAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeNone),
			networkType:          swag.String(models.ClusterNetworkTypeOVNKubernetes),
			invalid:              false,
		},
		{
			highAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeNone),
			networkType:          swag.String("CalicoOrWhatever"),
			invalid:              false,
		},
		{
			highAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeNone),
			networkType:          swag.String(models.ClusterNetworkTypeOpenShiftSDN),
			invalid:              true,
		},
		{
			highAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
			networkType:          swag.String(models.ClusterNetworkTypeOVNKubernetes),
			invalid:              false,
		},
		{
			highAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
			networkType:          swag.String(models.ClusterNetworkTypeOpenShiftSDN),
			invalid:              false,
		},
		{
			highAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
			networkType:          swag.String("CalicoOrWhatever"),
			invalid:              false,
		},
		{
			highAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeNone),
			networkType:          nil,
			invalid:              false,
		},
		{
			highAvailabilityMode: swag.String(models.ClusterHighAvailabilityModeFull),
			networkType:          nil,
			invalid:              false,
		},
	}
	for _, test := range tests {
		t := test
		It(fmt.Sprintf("Availability mode: %s, network type: %s", swag.StringValue(t.highAvailabilityMode), swag.StringValue(t.networkType)),
			func() {
				cluster := common.Cluster{
					Cluster: models.Cluster{
						HighAvailabilityMode: t.highAvailabilityMode,
						NetworkType:          t.networkType,
					},
				}
				Expect(isHighAvailabilityModeUnsupportedByNetworkType(&cluster)).To(Equal(t.invalid))
			})
	}
})
