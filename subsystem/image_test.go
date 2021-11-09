package subsystem

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
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
		ctx         = context.Background()
		ocpVersions models.OpenshiftVersions
	)

	BeforeEach(func() {
		resp, err := userBMClient.Versions.ListSupportedOpenshiftVersions(ctx, &versions.ListSupportedOpenshiftVersionsParams{})
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Payload).ShouldNot(BeEmpty())
		ocpVersions = resp.Payload
	})

	It("live iso endpoints always return BadRequest", func() {
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
			Expect(err).To(HaveOccurred())

			By("Download ISO")
			file, err := ioutil.TempFile("", "tmp")
			if err != nil {
				log.Fatal(err)
			}
			defer os.Remove(file.Name())

			_, err = userBMClient.AssistedServiceIso.DownloadISO(ctx, &assisted_service_iso.DownloadISOParams{}, file)
			Expect(err).To(HaveOccurred())
		}
	})

	assertImageGenerates := func(imageType models.ImageType) {
		It(fmt.Sprintf("[minimal-set][%s]create_and_get_image", imageType), func() {
			for ocpVersion := range ocpVersions {
				By(fmt.Sprintf("For version %s", ocpVersion))
				By("Register Cluster")

				registerResp, err := userBMClient.Installer.RegisterCluster(ctx, &installer.RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						Name:             swag.String("test-cluster"),
						OpenshiftVersion: swag.String(ocpVersion),
						PullSecret:       swag.String(pullSecret),
					},
				})
				Expect(err).NotTo(HaveOccurred())
				clusterID := *registerResp.GetPayload().ID

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

				getResp, err := userBMClient.Installer.GetCluster(ctx, &installer.GetClusterParams{ClusterID: clusterID})
				Expect(err).NotTo(HaveOccurred())
				Expect(getResp.Payload.ImageInfo).NotTo(BeNil())

				By("Download ISO")

				downloadIso(ctx, getResp.Payload.ImageInfo.DownloadURL)

				By("Download ISO Headers")
				downloadIsoHeaders(ctx, getResp.Payload.ImageInfo.DownloadURL)

				By("Verify events")
				verifyEventExistence(clusterID, "Successfully registered cluster")
			}
		})
	}

	for _, imageType := range []models.ImageType{models.ImageTypeFullIso, models.ImageTypeMinimalIso} {
		assertImageGenerates(imageType)
	}
})

var _ = Describe("system-test proxy update tests", func() {
	var (
		ctx       = context.Background()
		cluster   *installer.RegisterClusterCreated
		clusterID strfmt.UUID
	)

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
	})

	It("[V2UpdateCluster] generate_image_after_proxy_was_set", func() {
		// Generate ISO of registered cluster without proxy configured
		_, err := userBMClient.Installer.GenerateClusterISO(ctx, &installer.GenerateClusterISOParams{
			ClusterID:         clusterID,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(err).NotTo(HaveOccurred())

		// Update cluster with proxy settings
		httpProxy := "http://proxyserver:3128"
		noProxy := "test.com"
		_, err = userBMClient.Installer.V2UpdateCluster(ctx, &installer.V2UpdateClusterParams{
			ClusterUpdateParams: &models.V2ClusterUpdateParams{
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

func downloadIso(ctx context.Context, url string) {
	file, err := ioutil.TempFile("", "tmp")
	if err != nil {
		log.Fatal(err)
	}
	defer os.Remove(file.Name())

	resp, err := http.Get(url)
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.StatusCode).To(Equal(http.StatusOK))

	_, err = io.Copy(file, resp.Body)
	Expect(err).NotTo(HaveOccurred())
	Expect(file.Sync()).To(Succeed())

	verifyFileNotEmpty(file)
}

func downloadIsoHeaders(ctx context.Context, url string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	Expect(err).NotTo(HaveOccurred())

	c := http.Client{}
	resp, err := c.Do(req)
	Expect(err).NotTo(HaveOccurred())
	Expect(resp.StatusCode).To(Equal(http.StatusOK))

	contentLength, err := strconv.Atoi(resp.Header.Get("Content-Length"))
	Expect(err).NotTo(HaveOccurred())
	Expect(contentLength).ToNot(Equal(0))
}

func verifyFileNotEmpty(file *os.File) {
	s, err := file.Stat()
	Expect(err).NotTo(HaveOccurred())
	Expect(s.Size()).ShouldNot(Equal(0))
}
