package migrations

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/common"
)

var _ = Describe("Migrate", func() {
	It("Succeeds", func() {
		db, dbName := common.PrepareTestDB()
		defer common.DeleteTestDB(db, dbName)
		err := MigratePost(db)
		Expect(err).ToNot(HaveOccurred())
	})
})
