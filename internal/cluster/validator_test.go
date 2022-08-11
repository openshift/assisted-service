package cluster

import (
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("isNetworksSameAddressFamilies", func() {

	var (
		validator         clusterValidator
		preprocessContext *clusterPreprocessContext
		clusterID         strfmt.UUID
	)

	BeforeEach(func() {
		validator = clusterValidator{logrus.New(), nil}
		preprocessContext = &clusterPreprocessContext{}
		clusterID = strfmt.UUID(uuid.New().String())
	})

	It("Returns ValidationPending when cluster and service network are unset and required", func() {
		userManagedNetworking := true
		highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeFull

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			HighAvailabilityMode:  &highAvailabilityMode,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationPending))
		Expect(message).Should(Equal("At least one of the CIDRs (Cluster Network, Service Network) is undefined."))
	})

	It("Returns ValidationPending when machine, cluster and service network are unset and required", func() {
		userManagedNetworking := true
		highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeNone

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			HighAvailabilityMode:  &highAvailabilityMode,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationPending))
		Expect(message).Should(Equal("At least one of the CIDRs (Machine Network, Cluster Network, Service Network) is undefined."))
	})

	It("Returns ValidationError when service networks contain an invalid CIDR", func() {
		userManagedNetworking := true
		highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeFull

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
			{Cidr: "notavalidcidr", ClusterID: clusterID},
			{Cidr: "192.168.40.1/24", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "10.0.4.1/24", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			HighAvailabilityMode:  &highAvailabilityMode,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationError))
		Expect(message).Should(Equal("Bad CIDR(s) appears in one of the networks"))
	})

	It("Returns ValidationError when clusterNetworks contain an invalid CIDR", func() {
		userManagedNetworking := true
		highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeFull

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
			{Cidr: "192.168.40.1/24", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "notavalidcidr", ClusterID: clusterID},
			{Cidr: "10.0.4.1/24", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			HighAvailabilityMode:  &highAvailabilityMode,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationError))
		Expect(message).Should(Equal("Bad CIDR(s) appears in one of the networks"))
	})

	It("Returns ValidationError cluster and service network address families match but there is an invalid CIDR in machine network", func() {
		userManagedNetworking := false
		highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeFull

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:101/64", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:102/64", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:103/64", ClusterID: clusterID},
		}
		machineNetworks := []*models.MachineNetwork{
			{Cidr: "20.0.2.1/24", ClusterID: clusterID},
			{Cidr: "notavalidcidr", ClusterID: clusterID},
			{Cidr: "20.0.4.1/24", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			HighAvailabilityMode:  &highAvailabilityMode,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
			MachineNetworks:       machineNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationError))
		Expect(message).Should(Equal(fmt.Sprintf("Error getting machine address families for cluster %s", clusterID)))
	})

	It("Returns ValidationFailure when the address families of service and cluster networks do not match", func() {
		userManagedNetworking := true
		highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeFull

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "192.168.10.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:101/64", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			HighAvailabilityMode:  &highAvailabilityMode,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationFailure))
		Expect(message).Should(Equal("Address families of networks (ServiceNetworks, ClusterNetworks) are not the same."))
	})

	It("Returns ValidationFailure cluster and service network address families match but mismatch for machine network families", func() {
		userManagedNetworking := false
		highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeFull

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:101/64", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:102/64", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:103/64", ClusterID: clusterID},
		}
		machineNetworks := []*models.MachineNetwork{
			{Cidr: "20.0.2.1/24", ClusterID: clusterID},
			{Cidr: "20.0.4.1/24", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			HighAvailabilityMode:  &highAvailabilityMode,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
			MachineNetworks:       machineNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationFailure))
		Expect(message).Should(Equal("Address families of networks (MachineNetworks, ServiceNetworks, ClusterNetworks) are not the same."))
	})

	It("Returns ValidationSuccess when machine, service and cluster network families match and they are all required", func() {
		userManagedNetworking := false
		highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeFull

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:101/64", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "10.0.2.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:102/64", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:103/64", ClusterID: clusterID},
		}
		machineNetworks := []*models.MachineNetwork{
			{Cidr: "20.0.2.1/24", ClusterID: clusterID},
			{Cidr: "2002::1234:abcd:ffff:c0a8:101/64", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			HighAvailabilityMode:  &highAvailabilityMode,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
			MachineNetworks:       machineNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationSuccess))
		Expect(message).Should(Equal("Same address families for all networks."))
	})

	It("Returns ValidationSuccess when service and cluster network families match and machine network is not required", func() {
		userManagedNetworking := true
		highAvailabilityMode := models.ClusterCreateParamsHighAvailabilityModeFull

		serviceNetworks := []*models.ServiceNetwork{
			{Cidr: "192.168.20.1/24", ClusterID: clusterID},
		}
		clusterNetworks := []*models.ClusterNetwork{
			{Cidr: "192.168.10.1/24", ClusterID: clusterID},
		}

		preprocessContext.cluster = &common.Cluster{Cluster: models.Cluster{
			ID:                    &clusterID,
			UserManagedNetworking: &userManagedNetworking,
			HighAvailabilityMode:  &highAvailabilityMode,
			ServiceNetworks:       serviceNetworks,
			ClusterNetworks:       clusterNetworks,
		}}

		status, message := validator.isNetworksSameAddressFamilies(preprocessContext)
		Expect(status).Should(Equal(ValidationSuccess))
		Expect(message).Should(Equal("Same address families for all networks."))
	})
})

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
