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

var _ = PDescribe("system-test image tests", func() {
	var (
		ctx         = context.Background()
		ocpVersions models.OpenshiftVersions
	)

	BeforeEach(func() {
		resp, err := userBMClient.Versions.ListSupportedOpenshiftVersions(ctx, &versions.ListSupportedOpenshiftVersionsParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Payload).ShouldNot(BeEmpty())
		ocpVersions = resp.Payload
	})

	AfterEach(func() {
		clearDB()
	})

	It("create_and_download_live_iso", func() {
		for ocpVersion := range ocpVersions {
			By(fmt.Sprintf("For version %s", ocpVersion))
			By("Create ISO")
			ignitionParams := models.AssistedServiceIsoCreateParams{
				SSHPublicKey:     sshPublicKey,
				PullSecret:       pullSecret,
				OpenshiftVersion: ocpVersion,
			}
			_, err := userBMClient.AssistedServiceIso.CreateISOAndUploadToS3(ctx, &assisted_service_iso.CreateISOAndUploadToS3Params{
				AssistedServiceIsoCreateParams: &ignitionParams,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Download ISO")
			file, err := ioutil.TempFile("", "tmp")
			if err != nil {
				log.Fatal(err)
			}
			defer os.Remove(file.Name())

			_, err = userBMClient.AssistedServiceIso.DownloadISO(ctx, &assisted_service_iso.DownloadISOParams{}, file)
			Expect(err).NotTo(HaveOccurred())
			verifyFileNotEmpty(file)
		}
	})

	assertImageGenerates := func(imageType models.ImageType) {
		It(fmt.Sprintf("[minimal-set][%s]create_and_get_image", imageType), func() {
			for ocpVersion := range ocpVersions {
				By(fmt.Sprintf("For version %s", ocpVersion))
				By("Register Cluster")

				cluster, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						Name:             swag.String("test-cluster"),
						OpenshiftVersion: swag.String(ocpVersion),
						PullSecret:       swag.String(pullSecret),
					},
				})
				Expect(err).NotTo(HaveOccurred())
				clusterID := *cluster.GetPayload().ID

				By("Generate ISO")
				macInterfaceMap := models.MacInterfaceMap{
					&models.MacInterfaceMapItems0{
						LogicalNicName: "eth0",
						MacAddress:     "00:00:5E:00:53:EE",
					},
					&models.MacInterfaceMapItems0{
						LogicalNicName: "eth1",
						MacAddress:     "00:00:5E:00:53:EF",
					},
				}

				config := common.FormatStaticConfigHostYAML("nic10", "02000048ba38", "192.0.2.155", "192.0.2.156", "192.0.2.1", macInterfaceMap)

				_, err = userBMClient.Installer.GenerateClusterISO(ctx, &installer.GenerateClusterISOParams{
					ClusterID: clusterID,
					ImageCreateParams: &models.ImageCreateParams{
						ImageType:           imageType,
						StaticNetworkConfig: []*models.HostStaticNetworkConfig{config},
					},
				})
				Expect(err).NotTo(HaveOccurred())

				By("Download ISO")

				downloadClusterIso(ctx, clusterID)

				By("Download ISO Headers")
				downloadClusterIsoHeaders(ctx, clusterID)

				By("Verify events")
				verifyEventExistence(clusterID, "Registered cluster")
				verifyEventExistence(clusterID, fmt.Sprintf("Image type is \"%s\"", imageType))
			}
		})
	}

	for _, imageType := range []models.ImageType{models.ImageTypeFullIso, models.ImageTypeMinimalIso} {
		assertImageGenerates(imageType)
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
				OpenshiftVersion: swag.String(openshiftVersion),
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

		_, err = userBMClient.Installer.DownloadClusterISOHeaders(ctx, &installer.DownloadClusterISOHeadersParams{ClusterID: clusterID})
		Expect(err).To(BeAssignableToTypeOf(installer.NewDownloadClusterISOHeadersNotFound()))

		// test that an event was added
		msg := "Deleted image from backend because its ignition was updated. The image may be regenerated at any time."
		verifyEventExistence(clusterID, msg)
	})

	It("download_non_existing_cluster", func() {
		_, err = userBMClient.Installer.DownloadClusterISO(ctx, &installer.DownloadClusterISOParams{ClusterID: *strToUUID(uuid.New().String())}, file)
		Expect(err).Should(HaveOccurred())
	})

	It("download_headers_non_existing_cluster", func() {
		_, err = userBMClient.Installer.DownloadClusterISOHeaders(ctx, &installer.DownloadClusterISOHeadersParams{ClusterID: *strToUUID(uuid.New().String())})
		Expect(err).Should(HaveOccurred())
	})

	It("download_non_existing_image", func() {
		cluster, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.DownloadClusterISO(ctx, &installer.DownloadClusterISOParams{
			ClusterID: *cluster.GetPayload().ID,
		}, file)
		Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewDownloadClusterISONotFound())))
	})

	It("download_headers_non_existing_image", func() {
		cluster, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
			NewClusterParams: &models.ClusterCreateParams{
				Name:             swag.String("test-cluster"),
				OpenshiftVersion: swag.String(common.TestDefaultConfig.OpenShiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.DownloadClusterISOHeaders(ctx, &installer.DownloadClusterISOHeadersParams{
			ClusterID: *cluster.GetPayload().ID})
		Expect(reflect.TypeOf(err)).Should(Equal(reflect.TypeOf(installer.NewDownloadClusterISOHeadersNotFound())))
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
				OpenshiftVersion: swag.String(openshiftVersion),
				PullSecret:       swag.String(pullSecret),
			},
		})
		Expect(err).NotTo(HaveOccurred())
		clusterID = *cluster.GetPayload().ID
	})

	It("generate_image_after_proxy_was_set", func() {
		// Generate ISO of registered cluster without proxy configured
		_, err := userBMClient.Installer.GenerateClusterISO(ctx, &installer.GenerateClusterISOParams{
			ClusterID:         clusterID,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(err).NotTo(HaveOccurred())

		// fetch cluster proxy hash for generated image
		msg := "Generated image (Image type is \"full-iso\", SSH public key is not set)"
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
		msg = fmt.Sprintf("Generated image (proxy URL is \"%s\", Image type is \"full-iso\", SSH public key is not set)", httpProxy)
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

func downloadClusterIsoHeaders(ctx context.Context, clusterID strfmt.UUID) {
	file, err := ioutil.TempFile("", "tmp")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(file.Name())

	_, err = userBMClient.Installer.DownloadClusterISOHeaders(ctx, &installer.DownloadClusterISOHeadersParams{
		ClusterID: clusterID,
	})
	Expect(err).NotTo(HaveOccurred())
	verifyFileNotEmpty(file)
}

func verifyFileNotEmpty(file *os.File) {
	s, err := file.Stat()
	Expect(err).NotTo(HaveOccurred())
	Expect(s.Size()).ShouldNot(Equal(0))
}
