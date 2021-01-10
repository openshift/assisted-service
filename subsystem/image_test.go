package subsystem

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/assisted_service_iso"
	"github.com/openshift/assisted-service/client/events"
	"github.com/openshift/assisted-service/client/installer"
	"github.com/openshift/assisted-service/client/versions"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("system-test image tests", func() {
	var (
		ctx       = context.Background()
		cluster   *installer.RegisterClusterCreated
		clusterID strfmt.UUID
	)

	AfterEach(func() {
		clearDB()
	})

	versions, err := userBMClient.Versions.ListSupportedOpenshiftVersions(ctx, &versions.ListSupportedOpenshiftVersionsParams{})
	Expect(err).NotTo(HaveOccurred())
	Expect(versions.Payload).ShouldNot(BeEmpty())

	for ocpVersion := range versions.Payload {
		It(fmt.Sprintf("[only_k8s][minimal-set][ocp-%s]create_and_get_image", ocpVersion), func() {
			By("Register Cluster", func() {
				cluster, err = userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						Name:             swag.String("test-cluster"),
						OpenshiftVersion: swag.String(ocpVersion),
						PullSecret:       swag.String(pullSecret),
					},
				})
				Expect(err).NotTo(HaveOccurred())
				clusterID = *cluster.GetPayload().ID
			})

			By("Generate ISO", func() {
				_, err = userBMClient.Installer.GenerateClusterISO(ctx, &installer.GenerateClusterISOParams{
					ClusterID:         clusterID,
					ImageCreateParams: &models.ImageCreateParams{},
				})
				Expect(err).NotTo(HaveOccurred())
			})

			By("Download ISO", func() {
				downloadClusterIso(ctx, clusterID)
			})

			By("Verify events", func() {
				verifyEventExistence(clusterID, "Registered cluster")
			})
		})

		It(fmt.Sprintf("[only_k8s][ocp-%s]create_and_download_live_iso", ocpVersion), func() {
			By("Create ISO", func() {
				ignitionParams := models.AssistedServiceIsoCreateParams{
					SSHPublicKey:     sshPublicKey,
					PullSecret:       pullSecret,
					OpenshiftVersion: ocpVersion,
				}
				_, err = userBMClient.AssistedServiceIso.CreateISOAndUploadToS3(ctx, &assisted_service_iso.CreateISOAndUploadToS3Params{
					AssistedServiceIsoCreateParams: &ignitionParams,
				})
				Expect(err).NotTo(HaveOccurred())
			})

			By("Download ISO", func() {
				file, err := ioutil.TempFile("", "tmp")
				if err != nil {
					log.Fatal(err)
				}
				defer os.Remove(file.Name())

				_, err = userBMClient.AssistedServiceIso.DownloadISO(ctx, &assisted_service_iso.DownloadISOParams{}, file)
				Expect(err).NotTo(HaveOccurred())
				verifyFileNotEmpty(file)
			})
		})
	}
})

var _ = Describe("image tests", func() {
	var (
		ctx     = context.Background()
		file    *os.File
		err     error
		cluster *installer.RegisterClusterCreated
	)

	AfterEach(func() {
		clearDB()
		os.Remove(file.Name())
	})

	BeforeEach(func() {
		file, err = ioutil.TempFile("", "tmp")
		Expect(err).To(BeNil())
	})

	It("Image is removed after patching ignition", func() {
		cluster, err = userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(common.DefaultTestOpenShiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterID := *cluster.GetPayload().ID

		_, err = userBMClient.Installer.GenerateClusterISO(ctx, &installer.GenerateClusterISOParams{
			ClusterID:         clusterID,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(err).NotTo(HaveOccurred())

		params := &installer.UpdateDiscoveryIgnitionParams{
			ClusterID:               clusterID,
			DiscoveryIgnitionParams: &models.DiscoveryIgnitionParams{Config: "{\"ignition\": {\"version\": \"3.1.0\"}, \"storage\": {\"files\": [{\"path\": \"/tmp/example\", \"contents\": {\"source\": \"data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj\"}}]}}"},
		}
		_, err = userBMClient.Installer.UpdateDiscoveryIgnition(ctx, params)
		Expect(err).NotTo(HaveOccurred())

		// test that the iso is no-longer available
		_, err = userBMClient.Installer.DownloadClusterISO(ctx, &installer.DownloadClusterISOParams{ClusterID: clusterID}, file)
		Expect(err).To(BeAssignableToTypeOf(installer.NewDownloadClusterISONotFound()))

		// test that an event was added
		msg := "Deleted image from backend because its ignition was updated. The image may be regenerated at any time."
		verifyEventExistence(clusterID, msg)
	})

	It("download_non_existing_cluster", func() {
		_, err = userBMClient.Installer.DownloadClusterISO(ctx, &installer.DownloadClusterISOParams{ClusterID: *strToUUID(uuid.New().String())}, file)
		Expect(err).Should(HaveOccurred())
	})

	It("[only_k8s]download_non_existing_image", func() {
		cluster, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(common.DefaultTestOpenShiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.DownloadClusterISO(ctx, &installer.DownloadClusterISOParams{
			ClusterID: *cluster.GetPayload().ID,
		}, file)
		Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewDownloadClusterISONotFound())))
	})
})

var _ = Describe("system-test proxy update tests", func() {
	var (
		ctx       = context.Background()
		cluster   *installer.RegisterClusterCreated
		clusterID strfmt.UUID
	)

	AfterEach(func() {
		clearDB()
	})

	BeforeEach(func() {
		var err error
		cluster, err = userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(common.DefaultTestOpenShiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterID = *cluster.GetPayload().ID
	})

	It("[only_k8s]generate_image_after_proxy_was_set", func() {
		// Generate ISO of registered cluster without proxy configured
		_, err := userBMClient.Installer.GenerateClusterISO(ctx, &installer.GenerateClusterISOParams{
			ClusterID:         clusterID,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(err).NotTo(HaveOccurred())

		// fetch cluster proxy hash for generated image
		msg := "Generated image (SSH public key is not set)"
		verifyEventExistence(clusterID, msg)

		// Update cluster with proxy settings
		httpProxy := "http://proxyserver:3128"
		noProxy := "test.com"
		_, err = userBMClient.Installer.UpdateCluster(ctx, &installer.UpdateClusterParams{
			ClusterUpdateParams: &models.ClusterUpdateParams{
				HTTPProxy: &httpProxy,
				NoProxy:   &noProxy,
			},
			ClusterID: clusterID,
		})
		Expect(err).ShouldNot(HaveOccurred())

		// Verify proxy settings changed event emitted
		verifyEventExistence(clusterID, "Proxy settings changed")

		// at least 10s must elapse between requests to generate the same ISO
		time.Sleep(time.Second * 10)

		// Generate ISO of registered cluster with proxy configured
		_, err = userBMClient.Installer.GenerateClusterISO(ctx, &installer.GenerateClusterISOParams{
			ClusterID:         clusterID,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(err).NotTo(HaveOccurred())

		// fetch cluster proxy hash for generated image
		msg = fmt.Sprintf("Generated image (proxy URL is \"%s\", SSH public key is not set)", httpProxy)
		verifyEventExistence(clusterID, msg)
	})
})

func verifyEventExistence(ClusterID strfmt.UUID, message string) {
	eventsReply, err := userBMClient.Events.ListEvents(context.TODO(), &events.ListEventsParams{
		ClusterID: ClusterID,
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(eventsReply.Payload).ShouldNot(HaveLen(0))
	nEvents := 0
	for _, ev := range eventsReply.Payload {
		fmt.Printf("EntityID:%s, Message:%s\n", ev.ClusterID, *ev.Message)
		Expect(ev.ClusterID.String()).Should(Equal(ClusterID.String()))
		if strings.Contains(*ev.Message, message) {
			nEvents++
		}
	}
	Expect(nEvents).ShouldNot(Equal(0))
}

func downloadClusterIso(ctx context.Context, clusterID strfmt.UUID) {
	file, err := ioutil.TempFile("", "tmp")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(file.Name())

	_, err = userBMClient.Installer.DownloadClusterISO(ctx, &installer.DownloadClusterISOParams{
		ClusterID: clusterID,
	}, file)
	Expect(err).NotTo(HaveOccurred())
	verifyFileNotEmpty(file)
}

func verifyFileNotEmpty(file *os.File) {
	s, err := file.Stat()
	Expect(err).NotTo(HaveOccurred())
	Expect(s.Size()).ShouldNot(Equal(0))
}
