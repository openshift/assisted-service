package subsystem

import (
	"context"
	"fmt"
	"io/ioutil"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
)

const psTemplate = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"%s\",\"email\":\"r@r.com\"}}}"

var _ = Describe("test authorization", func() {
	ctx := context.Background()

	var userClusterID, userClusterID2, userClusterID3 strfmt.UUID

	var accessReviewUnallowedUserStubID string
	var accessReviewAdminStubID string

	var capabilityReviewUnallowedUserStubID string
	var capabilityReviewAdminStubID string

	var capabilityReviewMultiarchNotallowedUserStubID string
	var capabilityReviewMultiarchAllowedUserStubID string

	BeforeSuite(func() {
		var err error
		if Options.AuthType != auth.TypeRHSSO {
			return
		}

		accessReviewUnallowedUserStubID, err = wiremock.createStubAccessReview(fakePayloadUnallowedUser, false)
		Expect(err).ShouldNot(HaveOccurred())

		accessReviewAdminStubID, err = wiremock.createStubAccessReview(fakePayloadAdmin, true)
		Expect(err).ShouldNot(HaveOccurred())

		capabilityReviewUnallowedUserStubID, err = wiremock.createStubBareMetalCapabilityReview(fakePayloadUnallowedUser, false)
		Expect(err).ShouldNot(HaveOccurred())

		capabilityReviewAdminStubID, err = wiremock.createStubBareMetalCapabilityReview(fakePayloadAdmin, true)
		Expect(err).ShouldNot(HaveOccurred())

		capabilityReviewMultiarchNotallowedUserStubID, err = wiremock.createStubMultiarchCapabilityReview(fakePayloadUsername, OrgId1, false)
		Expect(err).ShouldNot(HaveOccurred())

		capabilityReviewMultiarchAllowedUserStubID, err = wiremock.createStubMultiarchCapabilityReview(fakePayloadUsername2, OrgId2, true)
		Expect(err).ShouldNot(HaveOccurred())
	})

	AfterSuite(func() {
		if Options.AuthType != auth.TypeRHSSO {
			return
		}

		err := wiremock.DeleteStub(accessReviewUnallowedUserStubID)
		Expect(err).ShouldNot(HaveOccurred())

		err = wiremock.DeleteStub(accessReviewAdminStubID)
		Expect(err).ShouldNot(HaveOccurred())

		err = wiremock.DeleteStub(capabilityReviewUnallowedUserStubID)
		Expect(err).ShouldNot(HaveOccurred())

		err = wiremock.DeleteStub(capabilityReviewAdminStubID)
		Expect(err).ShouldNot(HaveOccurred())

		err = wiremock.DeleteStub(capabilityReviewMultiarchNotallowedUserStubID)
		Expect(err).ShouldNot(HaveOccurred())

		err = wiremock.DeleteStub(capabilityReviewMultiarchAllowedUserStubID)
		Expect(err).ShouldNot(HaveOccurred())
	})

	BeforeEach(func() {
		var err error
		if Options.AuthType == auth.TypeNone {
			Skip("auth is disabled")
		}

		userClusterID, err = registerCluster(ctx, userBMClient, "user-cluster", fmt.Sprintf(psTemplate, FakePS))
		Expect(err).ShouldNot(HaveOccurred())
		userClusterID2, err = registerCluster(ctx, user2BMClient, "user2-cluster", fmt.Sprintf(psTemplate, FakePS2))
		Expect(err).ShouldNot(HaveOccurred())
		userClusterID3, err = registerCluster(ctx, editclusterUserBMClient, "user3-cluster", fmt.Sprintf(psTemplate, FakePS3))
		Expect(err).ShouldNot(HaveOccurred())
	})

	Context("unallowed user", func() {
		It("can't list clusters", func() {
			_, err := unallowedUserBMClient.Installer.V2ListClusters(ctx, &installer.V2ListClustersParams{})
			Expect(err).Should(HaveOccurred())
		})
	})

	Context("admin user", func() {
		It("can get all clusters", func() {
			resp, err := readOnlyAdminUserBMClient.Installer.V2ListClusters(
				ctx,
				&installer.V2ListClustersParams{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(resp.Payload)).To(Equal(3))
		})

		It("can't register/delete with read only admin", func() {
			_, err := readOnlyAdminUserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: userClusterID})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2DeregisterClusterForbidden()))
		})
	})

	Context("with cluster editor role", func() {
		BeforeEach(func() {
			if !Options.EnableOrgTenancy {
				Skip("tenancy based auth is disabled")
			}
		})

		It("can delete cluster", func() {
			_, err := editclusterUserBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can update cluster", func() {
			_, err := editclusterUserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{ClusterID: userClusterID,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{Name: swag.String("update-test")}})
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("regular user", func() {
		It("can get owned cluster", func() {
			_, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can't get not owned cluster", func() {
			_, err := userBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: userClusterID2})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2GetClusterNotFound()))
		})

		It("can delete owned cluster", func() {
			_, err := userBMClient.Installer.V2DeregisterCluster(ctx, &installer.V2DeregisterClusterParams{ClusterID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can get owned infra-env", func() {
			infraEnvID := registerInfraEnv(&userClusterID, models.ImageTypeMinimalIso).ID
			_, err := userBMClient.Installer.GetInfraEnv(ctx, &installer.GetInfraEnvParams{InfraEnvID: *infraEnvID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can't get not owned infra-env", func() {

			request, err := user2BMClient.Installer.RegisterInfraEnv(context.Background(), &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("test-infra-env-2"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(fmt.Sprintf(psTemplate, FakePS2)),
					SSHAuthorizedKey: swag.String(sshPublicKey),
					ImageType:        models.ImageTypeMinimalIso,
					ClusterID:        &userClusterID2,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			infraEnvID2 := request.GetPayload().ID

			_, err = userBMClient.Installer.GetInfraEnv(ctx, &installer.GetInfraEnvParams{InfraEnvID: *infraEnvID2})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewGetInfraEnvNotFound()))
		})

		It("can't update not owned cluster", func() {
			_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{ClusterID: userClusterID2,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{Name: swag.String("update-test")}})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterNotFound()))
		})

		It("can't update not owned cluster, can only read cluster", func() {
			_, err := userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{ClusterID: userClusterID3,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{Name: swag.String("update-test")}})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2UpdateClusterForbidden()))
		})

		It("can't get non-existent infra-env", func() {
			infraEnvID := strfmt.UUID(uuid.New().String())
			_, err := userBMClient.Installer.GetInfraEnv(ctx, &installer.GetInfraEnvParams{InfraEnvID: infraEnvID})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewGetInfraEnvNotFound()))
		})
	})

	Context("agent", func() {
		It("can get owned cluster", func() {
			_, err := agentBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can't get not owned cluster", func() {
			_, err := agentBMClient.Installer.V2GetCluster(ctx, &installer.V2GetClusterParams{ClusterID: userClusterID2})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2GetClusterNotFound()))
		})

		It("can get owned infra-env", func() {
			infraEnvID := registerInfraEnv(&userClusterID, models.ImageTypeMinimalIso).ID
			_, err := agentBMClient.Installer.GetInfraEnv(ctx, &installer.GetInfraEnvParams{InfraEnvID: *infraEnvID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can't get not owned infra-env", func() {
			request, err := user2BMClient.Installer.RegisterInfraEnv(context.Background(), &installer.RegisterInfraEnvParams{
				InfraenvCreateParams: &models.InfraEnvCreateParams{
					Name:             swag.String("test-infra-env-agent-2"),
					OpenshiftVersion: openshiftVersion,
					PullSecret:       swag.String(fmt.Sprintf(psTemplate, FakePS2)),
					SSHAuthorizedKey: swag.String(sshPublicKey),
					ImageType:        models.ImageTypeMinimalIso,
					ClusterID:        &userClusterID2,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			infraEnvID2 := request.GetPayload().ID

			_, err = agentBMClient.Installer.GetInfraEnv(ctx, &installer.GetInfraEnvParams{InfraEnvID: *infraEnvID2})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewGetInfraEnvNotFound()))
		})
	})

	Context("organization based functionality", func() {
		BeforeEach(func() {
			if !Options.FeatureGate {
				Skip("organization based functionality access is disabled")
			}
		})
		It("allowed to register a multiarch cluster", func() {
			request, err := user2BMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					CPUArchitecture:  common.MultiCPUArchitecture,
					Name:             swag.String("test-multiarch-cluster"),
					OpenshiftVersion: swag.String(multiarchOpenshiftVersion),
					PullSecret:       swag.String(fmt.Sprintf(psTemplate, FakePS2)),
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(request.Payload.CPUArchitecture).To(Equal(common.MultiCPUArchitecture))
		})
		It("not allowed to register a multiarch cluster", func() {
			_, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					CPUArchitecture:  common.MultiCPUArchitecture,
					Name:             swag.String("test-multiarch-cluster"),
					OpenshiftVersion: swag.String(multiarchOpenshiftVersion),
					PullSecret:       swag.String(fmt.Sprintf(psTemplate, FakePS)),
				},
			})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2RegisterClusterBadRequest()))
		})
	})
})

var _ = Describe("Make sure that sensitive files are accessible only by owners of cluster", func() {
	var (
		ctx        context.Context
		clusterID  strfmt.UUID
		infraEnvID *strfmt.UUID
	)

	BeforeEach(func() {
		ctx = context.Background()
		cID, err := registerCluster(ctx, userBMClient, "test-cluster", pullSecret)
		Expect(err).ToNot(HaveOccurred())
		Expect(err).ToNot(HaveOccurred())
		clusterID = cID
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
		registerHostsAndSetRoles(clusterID, *infraEnvID, minHosts, "test-cluster", "example.com")

		setClusterAsFinalizing(ctx, clusterID)
		res, err := agentBMClient.Installer.V2UploadClusterIngressCert(ctx, &installer.V2UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: models.IngressCertParams(ingressCa)})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertCreated()))
	})

	Context("/v2/clusters/{cluster_id}/credentials", func() {
		It("Should not allow read-only-admins to download kubeconfig", func() {
			_, err := readOnlyAdminUserBMClient.Installer.V2GetCredentials(ctx, &installer.V2GetCredentialsParams{ClusterID: clusterID})
			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2GetCredentialsForbidden()))
		})
		It("Should allow 'user role' to download kubeconfig", func() {
			completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
			res, err := userBMClient.Installer.V2GetCredentials(ctx, &installer.V2GetCredentialsParams{ClusterID: clusterID})
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeAssignableToTypeOf(installer.NewV2GetCredentialsOK()))
		})
	})

	for _, name := range cluster.ClusterOwnerFileNames {
		// No access tests
		fileName := name
		it := fmt.Sprintf("Should not allow read-only-admins to download '%v' via downloads/files endpoint", fileName)
		It(it, func() {
			file, err := ioutil.TempFile("", "tmp")
			Expect(err).NotTo(HaveOccurred())
			_, err = readOnlyAdminUserBMClient.Installer.V2DownloadClusterCredentials(ctx, &installer.V2DownloadClusterCredentialsParams{ClusterID: clusterID, FileName: fileName}, file)
			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2DownloadClusterCredentialsForbidden()))
		})

		It(it, func() {
			_, err := readOnlyAdminUserBMClient.Installer.V2GetPresignedForClusterCredentials(ctx, &installer.V2GetPresignedForClusterCredentialsParams{ClusterID: clusterID, FileName: fileName})
			Expect(err).To(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewV2GetPresignedForClusterCredentialsForbidden()))
		})

		// Access granted
		it = fmt.Sprintf("Should allow cluster users to download '%v' via downloads/files endpoint", fileName)
		It(it, func() {
			file, err := ioutil.TempFile("", "tmp")
			Expect(err).NotTo(HaveOccurred())
			_, err = userBMClient.Installer.V2DownloadClusterCredentials(ctx, &installer.V2DownloadClusterCredentialsParams{ClusterID: clusterID, FileName: fileName}, file)
			Expect(err).ToNot(HaveOccurred())
		})
		it = fmt.Sprintf("Should allow cluster users to download '%v' via downloads/files-presigned endpoint", fileName)
		It(it, func() {
			_, err := userBMClient.Installer.V2GetPresignedForClusterCredentials(ctx, &installer.V2GetPresignedForClusterCredentialsParams{ClusterID: clusterID, FileName: fileName})
			Expect(err).NotTo(BeAssignableToTypeOf(installer.NewV2GetPresignedForClusterCredentialsForbidden()))
		})

	}

})

var _ = Describe("Cluster credentials should be accessed only by cluster owner", func() {
	var ctx context.Context
	var clusterID strfmt.UUID
	var infraEnvID *strfmt.UUID

	BeforeEach(func() {
		ctx = context.Background()
		cID, err := registerCluster(ctx, userBMClient, "test-cluster", pullSecret)
		Expect(err).ToNot(HaveOccurred())
		clusterID = cID
		infraEnvID = registerInfraEnv(&clusterID, models.ImageTypeMinimalIso).ID
		registerHostsAndSetRoles(clusterID, *infraEnvID, minHosts, "test-cluster", "example.com")
		setClusterAsFinalizing(ctx, clusterID)
		res, err := agentBMClient.Installer.V2UploadClusterIngressCert(ctx, &installer.V2UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: models.IngressCertParams(ingressCa)})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(BeAssignableToTypeOf(installer.NewV2UploadClusterIngressCertCreated()))
		completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)
	})

	It("Should not allow read-only-admins to get credentials", func() {
		_, err := readOnlyAdminUserBMClient.Installer.V2GetCredentials(ctx, &installer.V2GetCredentialsParams{ClusterID: clusterID})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeAssignableToTypeOf(installer.NewV2GetCredentialsForbidden()))
	})
	It("Should allow cluster user to get credentials", func() {
		_, err := userBMClient.Installer.V2GetCredentials(ctx, &installer.V2GetCredentialsParams{ClusterID: clusterID})
		Expect(err).ToNot(HaveOccurred())
	})
})
