package odf

import (
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
	"sigs.k8s.io/yaml"
)

var _ = Describe("OCS manifest generation", func() {
	type StorageCluster struct {
		Spec struct {
			StorageDeviceSets []struct {
				Count int `yaml:"count"`
			} `yaml:"storageDeviceSets"`
		} `yaml:"spec"`
	}

	operator := NewOdfOperator(common.GetTestLog())

	Context("Create OCS Manifests for all deployment modes with openshiftVersion as 4.8.X", func() {
		It("Check YAMLs of OCS in Compact Mode", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.8.0",
				Hosts: []*models.Host{
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
				},
			}}

			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-ocs_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-ocs_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-ocs_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())

			var storageCluster StorageCluster
			err = yaml.Unmarshal(manifest, &storageCluster)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(len(storageCluster.Spec.StorageDeviceSets)).To(BeNumerically(">", 0))
			Expect(storageCluster.Spec.StorageDeviceSets[0].Count).To(Equal(3))
		})

		It("Check YAMLs of OCS in Standard Mode", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.8.0",
				Hosts: []*models.Host{
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleWorker, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleWorker, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID3, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleWorker, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
				},
			}}

			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-ocs_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-ocs_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-ocs_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())

			var storageCluster StorageCluster
			err = yaml.Unmarshal(manifest, &storageCluster)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(len(storageCluster.Spec.StorageDeviceSets)).To(BeNumerically(">", 0))
			Expect(storageCluster.Spec.StorageDeviceSets[0].Count).To(Equal(4))
		})
	})

	Context("Create ODF Manifests for all deployment modes with openshiftVersion as 4.9.X or above", func() {
		It("Check YAMLs of ODF in Compact Mode", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.9.0",
				Hosts: []*models.Host{
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID3, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID3, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
				},
			}}

			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-odf_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-odf_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-odf_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())

			yamls := strings.Split(string(manifest), "\n---\n")
			Expect(yamls).To(HaveLen(2))

			var storageCluster StorageCluster
			err = yaml.Unmarshal([]byte(yamls[1]), &storageCluster)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(len(storageCluster.Spec.StorageDeviceSets)).To(BeNumerically(">", 0))
			Expect(storageCluster.Spec.StorageDeviceSets[0].Count).To(Equal(5))
		})

		It("Check YAMLs of ODF in Standard Mode", func() {
			cluster := common.Cluster{Cluster: models.Cluster{
				OpenshiftVersion: "4.9.0",
				Hosts: []*models.Host{
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleMaster, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleWorker, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleWorker, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
					{Role: models.HostRoleWorker, InstallationDiskID: diskID1, Inventory: Inventory(&InventoryResources{Disks: []*models.Disk{
						{ID: diskID1, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
						{ID: diskID2, SizeBytes: conversions.GbToBytes(30), DriveType: models.DriveTypeHDD},
					},
					})},
				},
			}}

			openshiftManifests, manifest, err := operator.GenerateManifests(&cluster)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(openshiftManifests).To(HaveLen(3))
			Expect(openshiftManifests["50_openshift-odf_ns.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-odf_subscription.yaml"]).NotTo(HaveLen(0))
			Expect(openshiftManifests["50_openshift-odf_operator_group.yaml"]).NotTo(HaveLen(0))

			for _, manifest := range openshiftManifests {
				_, err = yaml.YAMLToJSON(manifest)
				Expect(err).ShouldNot(HaveOccurred())
			}

			_, err = yaml.YAMLToJSON(manifest)
			Expect(err).ShouldNot(HaveOccurred())

			yamls := strings.Split(string(manifest), "\n---\n")
			Expect(yamls).To(HaveLen(2))

			var storageCluster StorageCluster
			err = yaml.Unmarshal([]byte(yamls[1]), &storageCluster)
			Expect(err).ShouldNot(HaveOccurred())

			Expect(len(storageCluster.Spec.StorageDeviceSets)).To(BeNumerically(">", 0))
			Expect(storageCluster.Spec.StorageDeviceSets[0].Count).To(Equal(3))
		})

	})
})
