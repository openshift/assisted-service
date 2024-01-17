package releasesources

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
	operations "github.com/openshift/assisted-service/restapi/operations/configuration"
	"gorm.io/gorm"
)

var _ = Describe("Test release sources API", func() {

	var (
		db      *gorm.DB
		dbName  string
		handler releaseSourcesHandler
	)

	BeforeEach(func() {
		db, dbName = common.PrepareTestDB()
		handler = NewReleaseSourcesHandler(getValidReleaseSources(), common.GetTestLog(), db, Config{OpenshiftMajorVersion: "4"})
	})

	AfterEach(func() {
		common.DeleteTestDB(db, dbName)
	})

	It("test success", func() {
		apiHandler := releaseSourcesAPIHandler{releaseSourcesHandler: handler, log: common.GetTestLog()}

		middlewareResponder := apiHandler.V2ListReleaseSources(
			context.Background(),
			operations.V2ListReleaseSourcesParams{},
		)
		Expect(middlewareResponder).Should(BeAssignableToTypeOf(operations.NewV2ListReleaseSourcesOK()))

		reply, ok := middlewareResponder.(*operations.V2ListReleaseSourcesOK)
		Expect(ok).To(BeTrue())

		payload := reply.Payload
		Expect(payload).To(Equal(apiHandler.releaseSourcesHandler.releaseSources))
	})
})
