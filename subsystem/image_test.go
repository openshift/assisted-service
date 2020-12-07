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
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

var _ = Describe("system-test image tests", func() {
	ctx := context.Background()
	var cluster *installer.RegisterClusterCreated
	var clusterID strfmt.UUID
	pullSecret := "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}" // #nosec

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

	It("[only_k8s][minimal-set]create_and_get_image", func() {
		file, err := ioutil.TempFile("", "tmp")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(file.Name())

		_, err = userBMClient.Installer.GenerateClusterISO(ctx, &installer.GenerateClusterISOParams{
			ClusterID:         clusterID,
			ImageCreateParams: &models.ImageCreateParams{},
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userBMClient.Installer.DownloadClusterISO(ctx, &installer.DownloadClusterISOParams{
			ClusterID: clusterID,
		}, file)
		Expect(err).NotTo(HaveOccurred())
		s, err := file.Stat()
		Expect(err).NotTo(HaveOccurred())
		Expect(s.Size()).ShouldNot(Equal(0))
		eventsReply, err := userBMClient.Events.ListEvents(context.TODO(), &events.ListEventsParams{
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(eventsReply.Payload).ShouldNot(HaveLen(0))
		nRegisteredEvents := 0
		for _, ev := range eventsReply.Payload {
			fmt.Printf("EntityID:%s, Message:%s\n", ev.ClusterID, *ev.Message)
			Expect(ev.ClusterID.String()).Should(Equal(clusterID.String()))
			if strings.Contains(*ev.Message, "Registered cluster") {
				nRegisteredEvents++
			}
		}
		Expect(nRegisteredEvents).ShouldNot(Equal(0))
	})

	It("Image is removed after patching ignition", func() {
		file, err := ioutil.TempFile("", "tmp")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(file.Name())

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
		eventsReply, err := userBMClient.Events.ListEvents(context.TODO(), &events.ListEventsParams{
			ClusterID: clusterID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(eventsReply.Payload).ShouldNot(HaveLen(0))
		nRegisteredEvents := 0
		for _, ev := range eventsReply.Payload {
			Expect(ev.ClusterID.String()).Should(Equal(clusterID.String()))
			if strings.Contains(*ev.Message, "Deleted image from backend because its ignition was updated. The image may be regenerated at any time.") {
				nRegisteredEvents++
			}
		}
		Expect(nRegisteredEvents).ShouldNot(Equal(0))
	})
})

var _ = Describe("system-test assisted-service live ISO image tests", func() {
	ctx := context.Background()
	pullSecret := "{\"auths\": {\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}" // #nosec

	AfterEach(func() {
		clearDB()
	})

	It("[only_k8s]create_and_download_live_iso", func() {
		file, err := ioutil.TempFile("", "tmp")
		if err != nil {
			log.Fatal(err)
		}
		defer os.Remove(file.Name())

		ignitionParams := models.AssistedServiceIsoCreateParams{
			SSHPublicKey: "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDgj9Pc6dmIAZvxvC1q4K05lUqd/Qy73JEGP/THZEdlLif825SPyMe9NGe8UxNiS4AvYJoLcplMVztQjInVf6s3C0EtlvyrfzdoCCONNBtgItU0gxG+GxneNJs/MKhlUBh6QWg52cBwiaTIxrGlbM/qLfzSX6k5WtZV/yH1TVVrFOpDxtOfR5RZ/GmI97pJIOhxEdw9aT3FydbFtuNwTyNxo0YGMk6Mp89qlUx20u4aK1HXn67I3+2xtpzPSiH6TwRPX3vb/qdWJ4/YaKOHwf/FnIg3FXQXVxRCBijDF0cCUmKWcdrs59JopGMFKDXwHHCdfMjtnfBvA/WOlBs0NKpoFIEuufL3gBuahBRvMKnOXD1gwD8WkaOa+B5BxutZ+/zXAPX3faXRdMGPfHRDam+rNR8KkbYl+3Y2C/W1APMLopLt5kKit64E4rHTwbYwB1Si770O+I/KTcAwnRo1j0K9m7ahz2YXK3fiqieh7awhkiosTsDHLAZDs+YTi9tfBQ8= me@tester",
			PullSecret:   pullSecret,
		}
		_, err = userBMClient.AssistedServiceIso.CreateISOAndUploadToS3(ctx, &assisted_service_iso.CreateISOAndUploadToS3Params{
			AssistedServiceIsoCreateParams: &ignitionParams,
		})

		Expect(err).NotTo(HaveOccurred())

		_, err = userBMClient.AssistedServiceIso.DownloadISO(ctx, &assisted_service_iso.DownloadISOParams{}, file)
		Expect(err).NotTo(HaveOccurred())
		s, err := file.Stat()
		Expect(err).NotTo(HaveOccurred())
		Expect(s.Size()).ShouldNot(Equal(0))
	})
})

var _ = Describe("image tests", func() {
	ctx := context.Background()
	var file *os.File
	var err error

	AfterEach(func() {
		clearDB()
		os.Remove(file.Name())
	})

	BeforeEach(func() {
		file, err = ioutil.TempFile("", "tmp")
		Expect(err).To(BeNil())
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
	ctx := context.Background()
	var cluster *installer.RegisterClusterCreated
	var clusterID strfmt.UUID
	pullSecret := "{\"auths\":{\"cloud.openshift.com\":{\"auth\":\"dXNlcjpwYXNzd29yZAo=\",\"email\":\"r@r.com\"}}}" // #nosec

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
