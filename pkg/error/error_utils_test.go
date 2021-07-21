package error

import (
	"errors"
	"testing"

	"github.com/go-openapi/swag"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/client/client_v1/installer"
	models "github.com/openshift/assisted-service/models/v1"
)

func TestErrorUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Error Utils")
}

var _ = Describe("Error Utils", func() {

	It("AssistedServiceErrorAPI tests", func() {

		err := installer.DownloadHostIgnitionConflict{
			Payload: &models.Error{
				Href:   swag.String("href"),
				ID:     swag.Int32(555),
				Kind:   swag.String("kind"),
				Reason: swag.String("reason"),
			},
		}

		expectedFmt := "AssistedServiceError Code:  Href: href ID: 555 Kind: kind Reason: reason"

		By("test AssistedServiceError original error - expect bad error formatting", func() {
			Expect(err.Error()).ShouldNot(Equal(expectedFmt))
		})

		By("test AssistedServiceError error - expect good formatting", func() {
			ase := GetAssistedError(&err)
			Expect(ase).Should(HaveOccurred())
			Expect(ase.Error()).Should(Equal(expectedFmt))
		})
	})

	It("AssistedServiceInfraError tests", func() {

		err := installer.DownloadHostIgnitionForbidden{
			Payload: &models.InfraError{
				Code:    swag.Int32(403),
				Message: swag.String("forbidden"),
			},
		}

		expectedFmt := "AssistedServiceInfraError Code: 403 Message: forbidden"

		By("test AssistedServiceInfraError original error - expect bad error formatting", func() {
			Expect(err.Error()).ShouldNot(Equal(expectedFmt))
		})

		By("test AssistedServiceInfraError error - expect good formatting", func() {
			ase := GetAssistedError(&err)
			Expect(ase).Should(HaveOccurred())
			Expect(ase.Error()).Should(Equal(expectedFmt))
		})
	})

	It("test regular error", func() {
		err := errors.New("test error")
		Expect(err.Error()).Should(Equal("test error"))
		ase := GetAssistedError(err)
		Expect(ase).Should(HaveOccurred())
		Expect(ase.Error()).Should(Equal("test error"))
	})
})
