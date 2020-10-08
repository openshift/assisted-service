package subsystem

import (
	"context"
	"fmt"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/models"
)

const psTemplate = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"%s\",\"email\":\"r@r.com\"}}}"

var _ = Describe("test authorization", func() {
	ctx := context.Background()

	var err error

	var adminClusterID strfmt.UUID
	var userClusterID strfmt.UUID

	var accessReviewUnallowedUserStubID string
	var accessReviewAdminStubID string

	var capabilityReviewUnallowedUserStubID string
	var capabilityReviewAdminStubID string

	BeforeSuite(func() {
		if !Options.EnableAuth {
			return
		}

		accessReviewUnallowedUserStubID, err = wiremock.createStubAccessReview(fakePayloadUnallowedUser, false)
		Expect(err).ShouldNot(HaveOccurred())

		accessReviewAdminStubID, err = wiremock.createStubAccessReview(fakePayloadAdmin, true)
		Expect(err).ShouldNot(HaveOccurred())

		capabilityReviewUnallowedUserStubID, err = wiremock.createStubCapabilityReview(fakePayloadUnallowedUser, false)
		Expect(err).ShouldNot(HaveOccurred())

		capabilityReviewAdminStubID, err = wiremock.createStubCapabilityReview(fakePayloadAdmin, true)
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterSuite(func() {
		if !Options.EnableAuth {
			return
		}

		err = wiremock.DeleteStub(accessReviewUnallowedUserStubID)
		Expect(err).ShouldNot(HaveOccurred())

		err = wiremock.DeleteStub(accessReviewAdminStubID)
		Expect(err).ShouldNot(HaveOccurred())

		err = wiremock.DeleteStub(capabilityReviewUnallowedUserStubID)
		Expect(err).ShouldNot(HaveOccurred())

		err = wiremock.DeleteStub(capabilityReviewAdminStubID)
		Expect(err).ShouldNot(HaveOccurred())
	})

	BeforeEach(func() {
		if !Options.EnableAuth {
			Skip("auth is disabled")
		}

		adminClusterID = registerCluster(ctx, adminUserBMClient, "admin-cluster", fmt.Sprintf(psTemplate, FakeAdminPS))
		userClusterID = registerCluster(ctx, userBMClient, "user-cluster", fmt.Sprintf(psTemplate, FakePS))
	})

	AfterEach(func() {
		clearDB()
	})

	Context("unallowed user", func() {
		It("can't list clusters", func() {
			_, err := unallowedUserBMClient.Installer.ListClusters(ctx, &installer.ListClustersParams{})
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("admin user", func() {
		It("can get all clusters", func() {
			_, err := adminUserBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())

			_, err = adminUserBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: adminClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("regular user", func() {
		It("can get owned cluster", func() {
			_, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can't get not owned cluster", func() {
			_, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: adminClusterID})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewGetClusterNotFound()))
		})

		It("can delete owned cluster", func() {
			_, err := userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can't delete not owned cluster", func() {
			_, err := userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: adminClusterID})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewDeregisterClusterNotFound()))
		})
	})
})

func registerCluster(ctx context.Context, client *client.AssistedInstall, clusterName string, pullSecret string) strfmt.UUID {
	var cluster, err = client.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
		NewClusterParams: &models.ClusterCreateParams{
			Name:             swag.String(clusterName),
			OpenshiftVersion: swag.String("4.5"),
			PullSecret:       pullSecret,
		},
	})
	Expect(err).ShouldNot(HaveOccurred())
	return *cluster.GetPayload().ID
}
