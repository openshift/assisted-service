package subsystem

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"

	"github.com/go-openapi/strfmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/internal/cluster"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
)

const psTemplate = "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"%s\",\"email\":\"r@r.com\"}}}"

var _ = Describe("test authorization", func() {
	ctx := context.Background()

	var userClusterID, userClusterID2 strfmt.UUID

	var accessReviewUnallowedUserStubID string
	var accessReviewAdminStubID string

	var capabilityReviewUnallowedUserStubID string
	var capabilityReviewAdminStubID string

	BeforeSuite(func() {
		var err error
		if Options.AuthType != auth.TypeRHSSO {
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
			resp, err := readOnlyAdminUserBMClient.Installer.ListClusters(
				ctx,
				&installer.ListClustersParams{})
			Expect(err).ShouldNot(HaveOccurred())
			Expect(len(resp.Payload)).To(Equal(2))
		})

		It("can't register/delete with read only admin", func() {
			_, err := readOnlyAdminUserBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: userClusterID})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewDeregisterClusterForbidden()))
		})
	})

	Context("regular user", func() {
		It("can get owned cluster", func() {
			_, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can't get not owned cluster", func() {
			_, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: userClusterID2})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewGetClusterNotFound()))
		})

		It("can delete owned cluster", func() {
			_, err := userBMClient.Installer.DeregisterCluster(ctx, &installer.DeregisterClusterParams{ClusterID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can get owned infra-env", func() {
			_, err := userBMClient.Installer.GetInfraEnv(ctx, &installer.GetInfraEnvParams{InfraEnvID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can't get not owned infra-env", func() {
			_, err := userBMClient.Installer.GetInfraEnv(ctx, &installer.GetInfraEnvParams{InfraEnvID: userClusterID2})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewGetInfraEnvNotFound()))
		})
	})

	Context("agent", func() {
		It("can get owned cluster", func() {
			_, err := agentBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can't get not owned cluster", func() {
			_, err := agentBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: userClusterID2})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewGetClusterNotFound()))
		})

		It("can get owned infra-env", func() {
			_, err := agentBMClient.Installer.GetInfraEnv(ctx, &installer.GetInfraEnvParams{InfraEnvID: userClusterID})
			Expect(err).ShouldNot(HaveOccurred())
		})

		It("can't get not owned infra-env", func() {
			_, err := agentBMClient.Installer.GetInfraEnv(ctx, &installer.GetInfraEnvParams{InfraEnvID: userClusterID2})
			Expect(err).Should(HaveOccurred())
			Expect(err).To(BeAssignableToTypeOf(installer.NewGetInfraEnvNotFound()))
		})
	})
})

var _ = Describe("Make sure that sensitive files are accessible only by owners of cluster", func() {
	var (
		ctx       context.Context
		clusterID strfmt.UUID
		file      *os.File
	)
	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		ctx = context.Background()
		cID, err := registerCluster(ctx, userBMClient, "test-cluster", pullSecret)
		Expect(err).ToNot(HaveOccurred())
		file, err = ioutil.TempFile("", "tmp")
		Expect(err).ToNot(HaveOccurred())
		clusterID = cID
		generateClusterISO(clusterID, models.ImageTypeMinimalIso)
		registerHostsAndSetRoles(clusterID, minHosts, "test-cluster", "example.com")
		setClusterAsFinalizing(ctx, clusterID)
		res, err := agentBMClient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: models.IngressCertParams(ingressCa)})
		Expect(err).NotTo(HaveOccurred())
		Expect(reflect.TypeOf(res)).Should(Equal(reflect.TypeOf(installer.NewUploadClusterIngressCertCreated())))
	})

	Context("/v1/<cluster_id>/downloads/kubeconfig", func() {
		It("Should not allow read-only-admins to download kubeconfig", func() {
			_, err := readOnlyAdminUserBMClient.Installer.DownloadClusterKubeconfig(ctx, &installer.DownloadClusterKubeconfigParams{ClusterID: clusterID}, file)
			Expect(err).To(HaveOccurred())
			Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewDownloadClusterKubeconfigForbidden())))
		})
		It("Should allow 'user role' to download kubeconfig", func() {
			res, err := userBMClient.Installer.DownloadClusterKubeconfig(ctx, &installer.DownloadClusterKubeconfigParams{ClusterID: clusterID}, file)
			Expect(err).ToNot(HaveOccurred())
			Expect(reflect.TypeOf(res)).Should(Equal(reflect.TypeOf(installer.NewDownloadClusterKubeconfigOK(file))))
		})
	})

	for _, name := range cluster.ClusterOwnerFileNames {
		// No access tests
		fileName := name
		it := fmt.Sprintf("Should not allow read-only-admins to download '%v' via downloads/files endpoint", fileName)
		It(it, func() {
			file, err := ioutil.TempFile("", "tmp")
			Expect(err).NotTo(HaveOccurred())
			_, err = readOnlyAdminUserBMClient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: fileName}, file)
			Expect(err).To(HaveOccurred())
			Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewDownloadClusterFilesForbidden())))
		})
		it = fmt.Sprintf("Should not allow read-only-admins  to download '%v' via downloads/files-presigned endpoint", fileName)
		It(it, func() {
			_, err := readOnlyAdminUserBMClient.Installer.GetPresignedForClusterFiles(ctx, &installer.GetPresignedForClusterFilesParams{ClusterID: clusterID, FileName: fileName})
			Expect(err).To(HaveOccurred())
			Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewGetPresignedForClusterFilesForbidden())))
		})
		// Access granted
		it = fmt.Sprintf("Should allow cluster users to download '%v' via downloads/files endpoint", fileName)
		It(it, func() {
			file, err := ioutil.TempFile("", "tmp")
			Expect(err).NotTo(HaveOccurred())
			_, err = userBMClient.Installer.DownloadClusterFiles(ctx, &installer.DownloadClusterFilesParams{ClusterID: clusterID, FileName: fileName}, file)
			Expect(err).ToNot(HaveOccurred())
		})
		it = fmt.Sprintf("Should allow cluster users to download '%v' via downloads/files-presigned endpoint", fileName)
		It(it, func() {
			_, err := userBMClient.Installer.GetPresignedForClusterFiles(ctx, &installer.GetPresignedForClusterFilesParams{ClusterID: clusterID, FileName: fileName})
			Expect(reflect.TypeOf(err)).ShouldNot(Equal(reflect.TypeOf(installer.NewGetPresignedForClusterFilesForbidden())))
		})

	}

})

var _ = Describe("Cluster credentials should be accessed only by cluster owner", func() {
	var ctx context.Context
	var clusterID strfmt.UUID
	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		ctx = context.Background()
		cID, err := registerCluster(ctx, userBMClient, "test-cluster", pullSecret)
		Expect(err).ToNot(HaveOccurred())
		clusterID = cID
		generateClusterISO(clusterID, models.ImageTypeMinimalIso)
		registerHostsAndSetRoles(clusterID, minHosts, "test-cluster", "example.com")
		setClusterAsFinalizing(ctx, clusterID)
		res, err := agentBMClient.Installer.UploadClusterIngressCert(ctx, &installer.UploadClusterIngressCertParams{ClusterID: clusterID, IngressCertParams: models.IngressCertParams(ingressCa)})
		Expect(err).NotTo(HaveOccurred())
		Expect(reflect.TypeOf(res)).Should(Equal(reflect.TypeOf(installer.NewUploadClusterIngressCertCreated())))
		completeInstallationAndVerify(ctx, agentBMClient, clusterID, true)

	})
	It("Should not allow read-only-admins to get credentials", func() {
		_, err := readOnlyAdminUserBMClient.Installer.GetCredentials(ctx, &installer.GetCredentialsParams{ClusterID: clusterID})
		Expect(err).To(HaveOccurred())
		Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewGetCredentialsForbidden())))
	})
	It("Should allow cluster user to get credentials", func() {
		_, err := userBMClient.Installer.GetCredentials(ctx, &installer.GetCredentialsParams{ClusterID: clusterID})
		Expect(err).ToNot(HaveOccurred())
	})
})
