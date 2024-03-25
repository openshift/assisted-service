package subsystem

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client"
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

	var capabilityReviewIgnoreValidationsNotallowedUserStubID string
	var capabilityReviewIgnoreValidationsAllowedUserStubID string

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

		capabilityReviewMultiarchNotallowedUserStubID, err = wiremock.createStubMultiarchCapabilityReview(
			fakePayloadUsername, OrgId1, false,
		)
		Expect(err).ShouldNot(HaveOccurred())

		capabilityReviewMultiarchAllowedUserStubID, err = wiremock.createStubMultiarchCapabilityReview(
			fakePayloadUsername2, OrgId2, true,
		)
		Expect(err).ShouldNot(HaveOccurred())

		capabilityReviewIgnoreValidationsNotallowedUserStubID, err = wiremock.createStubIgnoreValidationsCapabilityReview(
			fakePayloadUsername, OrgId1, false,
		)
		Expect(err).ShouldNot(HaveOccurred())

		capabilityReviewIgnoreValidationsAllowedUserStubID, err = wiremock.createStubIgnoreValidationsCapabilityReview(
			fakePayloadUsername2, OrgId2, true,
		)
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

		err = wiremock.DeleteStub(capabilityReviewIgnoreValidationsNotallowedUserStubID)
		Expect(err).ShouldNot(HaveOccurred())

		err = wiremock.DeleteStub(capabilityReviewIgnoreValidationsAllowedUserStubID)
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

	Context("Ignoring validations", func() {
		It("can't ignore validations if not permitted to do so", func() {
			_, err := unallowedUserBMClient.Installer.V2SetIgnoredValidations(ctx, &installer.V2SetIgnoredValidationsParams{})
			Expect(err).Should(HaveOccurred())
		})
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

		It("can update day2 cluster", func() {
			// Install day1 cluster
			clusterId, err := registerCluster(ctx, userBMClient, "test-cluster", pullSecret)
			Expect(err).ToNot(HaveOccurred())
			infraEnvID := registerInfraEnv(&clusterId, models.ImageTypeMinimalIso).ID
			registerHostsAndSetRoles(clusterId, *infraEnvID, minHosts, "test-cluster", "example.com")
			setClusterAsFinalizing(ctx, clusterId)
			completeInstallationAndVerify(ctx, agentBMClient, clusterId, true)

			// Get day1 cluster
			c, err := common.GetClusterFromDB(db, clusterId, common.SkipEagerLoading)
			Expect(err).ShouldNot(HaveOccurred())

			// Create day2 cluster
			res, err := userBMClient.Installer.V2ImportCluster(ctx, &installer.V2ImportClusterParams{
				NewImportClusterParams: &models.ImportClusterParams{
					Name:               swag.String("test-cluster"),
					APIVipDnsname:      swag.String("api.test-cluster.example.com"),
					OpenshiftVersion:   openshiftVersion,
					OpenshiftClusterID: &c.OpenshiftClusterID,
				}})
			Expect(err).ShouldNot(HaveOccurred())

			// Update day2 cluster by an editor user
			day2ClusterId := *res.GetPayload().ID
			_, err = editclusterUserBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
				ClusterID: day2ClusterId,
				ClusterUpdateParams: &models.V2ClusterUpdateParams{
					APIVipDNSName: swag.String("some-dns-name"),
				},
			})
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

		var (
			cluster models.Cluster
		)

		marshalError := func(err error) string {
			d, _ := json.Marshal(err)
			return string(d)
		}

		createCluster := func(c *client.AssistedInstall, pullSecret string) {
			clusterCreated, err := c.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
				NewClusterParams: &models.ClusterCreateParams{
					CPUArchitecture:  common.X86CPUArchitecture,
					Name:             swag.String("test-ignore-validations"),
					OpenshiftVersion: swag.String(openshiftVersion),
					PullSecret:       swag.String(fmt.Sprintf(psTemplate, pullSecret)),
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
			cluster = *clusterCreated.Payload
		}

		BeforeEach(func() {
			if !Options.FeatureGate {
				Skip("organization based functionality access is disabled")
			}
		})

		It("try to fetch ignored validations when not allowed", func() {
			createCluster(userBMClient, FakePS)
			_, err := userBMClient.Installer.V2GetIgnoredValidations(ctx, &installer.V2GetIgnoredValidationsParams{
				ClusterID: *cluster.ID,
			})
			Expect(err).Should(HaveOccurred())
			Expect(marshalError(err)).To(ContainSubstring("the capability to ignore validations is not available"))
		})

		It("attempt to ignore validations when allowed to ignore validations and request is valid", func() {
			createCluster(user2BMClient, FakePS2)
			_, err := user2BMClient.Installer.V2SetIgnoredValidations(ctx, &installer.V2SetIgnoredValidationsParams{
				ClusterID: *cluster.ID,
				IgnoredValidations: &models.IgnoredValidations{
					ClusterValidationIds: "[\"dns-domain-defined\"]",
					HostValidationIds:    "[\"has-cpu-cores-for-role\",\"has-memory-for-role\"]",
				},
			})
			Expect(err).ShouldNot(HaveOccurred())
			ignoredValidations, err := user2BMClient.Installer.V2GetIgnoredValidations(ctx, &installer.V2GetIgnoredValidationsParams{
				ClusterID: *cluster.ID,
			})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ignoredValidations.Payload.ClusterValidationIds).To(Equal("[\"dns-domain-defined\"]"))
			Expect(ignoredValidations.Payload.HostValidationIds).To(Equal("[\"has-cpu-cores-for-role\",\"has-memory-for-role\"]"))
		})

		It("attempt to ignore validations with ID's that do not exist", func() {
			createCluster(user2BMClient, FakePS2)
			_, err := user2BMClient.Installer.V2SetIgnoredValidations(ctx, &installer.V2SetIgnoredValidationsParams{
				ClusterID: *cluster.ID,
				IgnoredValidations: &models.IgnoredValidations{
					ClusterValidationIds: "[\"all\", \"dns-domain-defined\", \"does-not-exist\"]",
					HostValidationIds:    "[\"all\", \"has-cpu-cores-for-role\", \"also-does-not-exist\",\"has-memory-for-role\"]",
				},
			})
			Expect(err).Should(HaveOccurred())
			Expect(marshalError(err)).To(ContainSubstring("Validation ID 'does-not-exist' is not a known cluster validation"))
			Expect(marshalError(err)).To(ContainSubstring("Validation ID 'also-does-not-exist' is not a known host validation"))
			Expect(marshalError(err)).ToNot(ContainSubstring("Validation ID 'all' is not a known host validation"))
			Expect(marshalError(err)).ToNot(ContainSubstring("Validation ID 'all' is not a known cluster validation"))
			c, err := common.GetClusterFromDB(db, *cluster.ID, common.SkipEagerLoading)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(c.IgnoredClusterValidations).To(Equal(""))
			Expect(c.IgnoredHostValidations).To(Equal(""))
		})

		It("attempt to ignore validations when not allowed to ignore validations", func() {
			createCluster(userBMClient, FakePS)
			_, err := userBMClient.Installer.V2SetIgnoredValidations(ctx, &installer.V2SetIgnoredValidationsParams{
				ClusterID: *cluster.ID,
				IgnoredValidations: &models.IgnoredValidations{
					ClusterValidationIds: "",
					HostValidationIds:    "",
				},
			})
			Expect(err).Should(HaveOccurred())
			Expect(marshalError(err)).To(ContainSubstring("the capability to ignore validations is not available"))
			c, err := common.GetClusterFromDB(db, *cluster.ID, common.SkipEagerLoading)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(c.IgnoredClusterValidations).To(Equal(""))
			Expect(c.IgnoredHostValidations).To(Equal(""))
		})

		It("Attempt to ignore a host validation that is not ignorable", func() {
			createCluster(user2BMClient, FakePS2)
			_, err := user2BMClient.Installer.V2SetIgnoredValidations(ctx, &installer.V2SetIgnoredValidationsParams{
				ClusterID: *cluster.ID,
				IgnoredValidations: &models.IgnoredValidations{
					ClusterValidationIds: "[\"api-vips-defined\",\"ingress-vips-defined\"]",
					HostValidationIds:    "[\"connected\",\"has-inventory\"]",
				},
			})
			Expect(err).Should(HaveOccurred())
			Expect(marshalError(err)).To(ContainSubstring("unable to ignore the following host validations (connected,has-inventory)"))
			Expect(marshalError(err)).To(ContainSubstring("unable to ignore the following cluster validations (api-vips-defined,ingress-vips-defined)"))
			ignoredValidations, err := user2BMClient.Installer.V2GetIgnoredValidations(ctx, &installer.V2GetIgnoredValidationsParams{
				ClusterID: *cluster.ID,
			})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(ignoredValidations.Payload.ClusterValidationIds).To(Equal(""))
			Expect(ignoredValidations.Payload.HostValidationIds).To(Equal(""))
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
			file, err := os.CreateTemp("", "tmp")
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
			file, err := os.CreateTemp("", "tmp")
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
