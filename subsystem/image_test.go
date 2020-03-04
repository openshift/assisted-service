package subsystem

import (
	"context"
	"reflect"

	"github.com/go-openapi/strfmt"

	"github.com/go-openapi/swag"

	"github.com/filanov/bm-inventory/client/inventory"
	"github.com/filanov/bm-inventory/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var (
	images = map[string]*models.ImageCreateParams{
		"image1": {
			Description: "my image description",
			Name:        swag.String("my image"),
			Namespace:   swag.String("my namespace"),
			ProxyIP:     "some-proxy.com",
			ProxyPort:   swag.Int64(8080),
		},
		"image2": {
			Description: "second image description",
			Name:        swag.String("my second image"),
			Namespace:   swag.String("my namespace"),
			ProxyIP:     "some-proxy.com",
			ProxyPort:   swag.Int64(8089),
		},
	}
)

var _ = Describe("Image tests", func() {

	AfterEach(func() {
		clearDB()
	})

	It("create and get image", func() {
		var id *strfmt.UUID
		reply, err := bmclient.Inventory.CreateImage(context.Background(), &inventory.CreateImageParams{
			NewImageParams: images["image1"],
		})
		Expect(err).NotTo(HaveOccurred())
		rep := reply.GetPayload()
		id = rep.ID
		Expect(true).Should(Equal(reflect.DeepEqual(*images["image1"], rep.ImageCreateParams)))

		getReply, err := bmclient.Inventory.GetImage(context.Background(), &inventory.GetImageParams{ImageID: string(*id)})
		Expect(err).NotTo(HaveOccurred())
		Expect(true).Should(Equal(reflect.DeepEqual(*images["image1"], getReply.GetPayload().ImageCreateParams)))
		Expect(swag.StringValue(getReply.GetPayload().Status)).Should(Equal("ready"))
	})

	It("create multiple images", func() {
		reply, err := bmclient.Inventory.ListImages(context.Background(), inventory.NewListImagesParams())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(reply.GetPayload())).Should(Equal(0))

		_, err = bmclient.Inventory.CreateImage(context.Background(), &inventory.CreateImageParams{
			NewImageParams: images["image1"],
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = bmclient.Inventory.CreateImage(context.Background(), &inventory.CreateImageParams{
			NewImageParams: images["image2"],
		})
		Expect(err).NotTo(HaveOccurred())

		reply, err = bmclient.Inventory.ListImages(context.Background(), inventory.NewListImagesParams())
		Expect(err).NotTo(HaveOccurred())
		Expect(len(reply.GetPayload())).Should(Equal(2))
	})

})
