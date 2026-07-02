package bminventory

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("validateIgnitionEndpoint", func() {
	It("normalizes unbracketed IPv6 URLs", func() {
		bm := &bareMetalInventory{}
		url := "https://fd2e:6f44:5dd8:c956::14:31187"
		ignitionEndpoint := &models.IgnitionEndpoint{URL: &url}

		Expect(bm.validateIgnitionEndpoint(ignitionEndpoint, logrus.New())).To(Succeed())
		Expect(*ignitionEndpoint.URL).To(Equal("https://[fd2e:6f44:5dd8:c956::14]:31187"))
	})
})
