package subsystem

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/alecthomas/units"
	"github.com/filanov/bm-inventory/client/inventory"
	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster tests", func() {
	ctx := context.Background()
	var cluster *inventory.RegisterClusterCreated
	var clusterID strfmt.UUID
	var err error
	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		cluster, err = bmclient.Inventory.RegisterCluster(ctx, &inventory.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name: swag.String("test cluster"),
			},
		})
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		clusterID = *cluster.GetPayload().ID
	})

	It("cluster CRUD", func() {
		_ = registerHost(clusterID)
		Expect(err).NotTo(HaveOccurred())

		getReply, err := bmclient.Inventory.GetCluster(ctx, &inventory.GetClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		Expect(getReply.GetPayload().Hosts[0].ClusterID.String()).Should(Equal(clusterID.String()))

		list, err := bmclient.Inventory.ListClusters(ctx, &inventory.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(1))

		_, err = bmclient.Inventory.DeregisterCluster(ctx, &inventory.DeregisterClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())

		list, err = bmclient.Inventory.ListClusters(ctx, &inventory.ListClustersParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(len(list.GetPayload())).Should(Equal(0))

		_, err = bmclient.Inventory.GetCluster(ctx, &inventory.GetClusterParams{ClusterID: clusterID})
		Expect(err).Should(HaveOccurred())
	})

	It("cluster update", func() {
		host1 := registerHost(clusterID)
		host2 := registerHost(clusterID)

		publicKey := `ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQD14Gv4V5DVvyr7O6/44laYx52VYLe8yrEA3fOieWDmojRs3scqLnfeLHJWsfYA4QMjTuraLKhT8dhETSYiSR88RMM56+isLbcLshE6GkNkz3MBZE2hcdakqMDm6vucP3dJD6snuh5Hfpq7OWDaTcC0zCAzNECJv8F7LcWVa8TLpyRgpek4U022T5otE1ZVbNFqN9OrGHgyzVQLtC4xN1yT83ezo3r+OEdlSVDRQfsq73Zg26d4dyagb6lmrryUUAAbfmn/HalJTHB73LyjilKiPvJ+x2bG7AeiqyVHwtQSpt02FCdQGptmsSqqWF/b9botOO38eUsqPNppMn7LT5wzDZdDlfwTCBWkpqijPcdo/LTD9dJlNHjwXZtHETtiid6N3ZZWpA0/VKjqUeQdSnHqLEzTidswsnOjCIoIhmJFqczeP5kOty/MWdq1II/FX/EpYCJxoSWkT/hVwD6VOamGwJbLVw9LkEb0VVWFRJB5suT/T8DtPdPl+A0qUGiN4KM= oscohen@localhost.localdomain`

		c, err := bmclient.Inventory.UpdateCluster(ctx, &inventory.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				SSHPublicKey: publicKey,
				HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
					{
						ID:   *host1.ID,
						Role: "master",
					},
					{
						ID:   *host2.ID,
						Role: "worker",
					},
				},
			},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(c.GetPayload().SSHPublicKey).Should(Equal(publicKey))

		h := getHost(clusterID, *host1.ID)
		Expect(h.Role).Should(Equal("master"))

		h = getHost(clusterID, *host2.ID)
		Expect(h.Role).Should(Equal("worker"))
	})
})

var _ = Describe("system-test cluster install", func() {
	var (
		ctx     = context.Background()
		cluster *models.Cluster
	)

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		registerClusterReply, err := bmclient.Inventory.RegisterCluster(ctx, &inventory.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				APIVip:                   "v1",
				BaseDNSDomain:            "example.com",
				ClusterNetworkCIDR:       "10.128.0.0/14",
				ClusterNetworkHostPrefix: 23,
				DNSVip:                   "",
				IngressVip:               "",
				Name:                     swag.String("test-cluster"),
				OpenshiftVersion:         "4.0",
				PullSecret:               `{"auths":{"cloud.openshift.com":{"auth":""}}}`,
				ServiceNetworkCIDR:       "172.30.0.0/16",
				SSHPublicKey:             "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC50TuHS7aYci+U+5PLe/aW/I6maBi9PBDucLje6C6gtArfjy7udWA1DCSIQd+DkHhi57/s+PmvEjzfAfzqo+L+/8/O2l2seR1pPhHDxMR/rSyo/6rZP6KIL8HwFqXHHpDUM4tLXdgwKAe1LxBevLt/yNl8kOiHJESUSl+2QSf8z4SIbo/frDD8OwOvtfKBEG4WCb8zEsEuIPNF/Vo/UxPtS9pPTecEsWKDHR67yFjjamoyLvAzMAJotYgyMoxm8PTyCgEzHk3s3S4iO956d6KVOEJVXnTVhAxrtLuubjskd7N4hVN7h2s4Z584wYLKYhrIBL0EViihOMzY4mH3YE4KZusfIx6oMcggKX9b3NHm0la7cj2zg0r6zjUn6ZCP4gXM99e5q4auc0OEfoSfQwofGi3WmxkG3tEozCB8Zz0wGbi2CzR8zlcF+BNV5I2LESlLzjPY5B4dvv5zjxsYoz94p3rUhKnnPM2zTx1kkilDK5C5fC1k9l/I/r5Qk4ebLQU= oscohen@localhost.localdomain",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		cluster = registerClusterReply.GetPayload()
	})

	generateHWPostStepReply := func(h *models.Host, hwInfo *models.Introspection) {
		hw, err := json.Marshal(&hwInfo)
		Expect(err).NotTo(HaveOccurred())
		_, err = bmclient.Inventory.PostStepReply(ctx, &inventory.PostStepReplyParams{
			ClusterID: h.ClusterID,
			HostID:    *h.ID,
			Reply: &models.StepReply{
				ExitCode: 0,
				Output:   string(hw),
				StepID:   string(models.StepTypeHardwareInfo),
			},
		})
		Expect(err).ShouldNot(HaveOccurred())
	}

	It("install cluster", func() {
		clusterID := *cluster.ID

		hwInfo := &models.Introspection{
			CPU:    &models.CPU{Cpus: 16},
			Memory: []*models.Memory{{Name: "Mem", Total: int64(32 * units.GiB)}},
		}
		h1 := registerHost(clusterID)
		generateHWPostStepReply(h1, hwInfo)
		h2 := registerHost(clusterID)
		generateHWPostStepReply(h2, hwInfo)
		h3 := registerHost(clusterID)
		generateHWPostStepReply(h3, hwInfo)
		h4 := registerHost(clusterID)
		generateHWPostStepReply(h4, hwInfo)

		_, err := bmclient.Inventory.UpdateCluster(ctx, &inventory.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h1.ID, Role: "master"},
				{ID: *h2.ID, Role: "master"},
				{ID: *h3.ID, Role: "master"},
				{ID: *h4.ID, Role: "worker"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())

		c, err := bmclient.Inventory.InstallCluster(ctx, &inventory.InstallClusterParams{ClusterID: clusterID})
		Expect(err).NotTo(HaveOccurred())
		Expect(swag.StringValue(c.GetPayload().Status)).Should(Equal("installing"))
		Expect(len(c.GetPayload().Hosts)).Should(Equal(4))
		for _, host := range c.GetPayload().Hosts {
			Expect(swag.StringValue(host.Status)).Should(Equal("installing"))
		}

		file, err := ioutil.TempFile("", "tmp")
		Expect(err).NotTo(HaveOccurred())

		defer os.Remove(file.Name())

		missingClusterId := strfmt.UUID(uuid.New().String())
		_, err = bmclient.Inventory.DownloadClusterKubeconfig(ctx, &inventory.DownloadClusterKubeconfigParams{ClusterID: missingClusterId}, file)
		Expect(err).Should(MatchError(inventory.NewDownloadClusterKubeconfigNotFound()))

		_, err = bmclient.Inventory.DownloadClusterKubeconfig(ctx, &inventory.DownloadClusterKubeconfigParams{ClusterID: clusterID}, file)
		Expect(err).NotTo(HaveOccurred())
		s, err := file.Stat()
		Expect(err).NotTo(HaveOccurred())
		Expect(s.Size()).ShouldNot(Equal(0))
	})

	It("install_cluster_insufficient_master", func() {
		clusterID := *cluster.ID

		hwInfo := &models.Introspection{
			CPU:    &models.CPU{Cpus: 2},
			Memory: []*models.Memory{{Name: "Mem", Total: int64(8 * units.GiB)}},
		}
		h1 := registerHost(clusterID)
		generateHWPostStepReply(h1, hwInfo)
		Expect(*getHost(clusterID, *h1.ID).Status).Should(Equal("known"))

		hwInfo = &models.Introspection{
			CPU:    &models.CPU{Cpus: 16},
			Memory: []*models.Memory{{Name: "Mem", Total: int64(32 * units.GiB)}},
		}
		h2 := registerHost(clusterID)
		generateHWPostStepReply(h2, hwInfo)
		h3 := registerHost(clusterID)
		generateHWPostStepReply(h3, hwInfo)
		h4 := registerHost(clusterID)
		generateHWPostStepReply(h4, hwInfo)

		_, err := bmclient.Inventory.UpdateCluster(ctx, &inventory.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{HostsRoles: []*models.ClusterUpdateParamsHostsRolesItems0{
				{ID: *h1.ID, Role: "master"},
				{ID: *h2.ID, Role: "master"},
				{ID: *h3.ID, Role: "master"},
				{ID: *h4.ID, Role: "worker"},
			}},
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		h1 = getHost(clusterID, *h1.ID)
		Expect(*h1.Status).Should(Equal("insufficient"))
	})
})
