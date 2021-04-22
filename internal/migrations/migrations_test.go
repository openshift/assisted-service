package migrations

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/dbc"
)

var _ = Describe("Migrate", func() {
	It("Succeeds", func() {
		db, dbName := dbc.PrepareTestDB()
		defer dbc.DeleteTestDB(db, dbName)
		err := Migrate(db)
		Expect(err).ToNot(HaveOccurred())
	})
})
