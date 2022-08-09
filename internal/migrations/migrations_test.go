package migrations

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

var _ = Describe("Migrate", func() {
	It("Succeeds", func() {
		db, _ := common.PrepareTestDB()
		err := MigratePost(db)
		Expect(err).ToNot(HaveOccurred())
	})
})
