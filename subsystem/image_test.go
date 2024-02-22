package subsystem

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
		resp, err := userBMClient.Versions.V2ListSupportedOpenshiftVersions(
			ctx, &versions.V2ListSupportedOpenshiftVersionsParams{OnlyLatest: swag.Bool(true)},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Payload).ShouldNot(BeEmpty())
		ocpVersions = resp.Payload
	})

	assertImageGenerates := func(imageType models.ImageType) {
		It(fmt.Sprintf("[minimal-set][%s]create_and_get_image", imageType), func() {
			for ocpVersion := range ocpVersions {
				By(fmt.Sprintf("For version %s", ocpVersion))
				By("Register Cluster")

				registerResp, err := userBMClient.Installer.V2RegisterCluster(ctx, &installer.V2RegisterClusterParams{
					NewClusterParams: &models.ClusterCreateParams{
						Name:             swag.String("test-cluster"),
						OpenshiftVersion: swag.String(ocpVersion),
						PullSecret:       swag.String(pullSecret),
					},
				})
				Expect(err).NotTo(HaveOccurred())
				clusterID := *registerResp.GetPayload().ID

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
				getResp, err := userBMClient.Installer.RegisterInfraEnv(ctx, &installer.RegisterInfraEnvParams{
					InfraenvCreateParams: &models.InfraEnvCreateParams{
						Name:                swag.String("iso-test-infra-env"),
						OpenshiftVersion:    ocpVersion,
						PullSecret:          swag.String(pullSecret),
						SSHAuthorizedKey:    swag.String(sshPublicKey),
						ImageType:           imageType,
						StaticNetworkConfig: []*models.HostStaticNetworkConfig{config},
						ClusterID:           &clusterID,
					},
				})
				Expect(err).NotTo(HaveOccurred())

				By("Download ISO")
				downloadIso(ctx, getResp.Payload.DownloadURL)

				By("Download ISO Headers")
				downloadIsoHeaders(ctx, getResp.Payload.DownloadURL)

				By("Verify events")
				verifyEventExistence(clusterID, "Successfully registered cluster")
			}
		})
	}

	for _, imageType := range []models.ImageType{models.ImageTypeFullIso, models.ImageTypeMinimalIso} {
		assertImageGenerates(imageType)
	}
})

func verifyEventExistence(ClusterID strfmt.UUID, message string) {
	eventsReply, err := userBMClient.Events.V2ListEvents(context.TODO(), &events.V2ListEventsParams{
		ClusterID: &ClusterID,
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
	file, err := os.CreateTemp("", "tmp")
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
