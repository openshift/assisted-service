package migrations

import (
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/events"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Migrate", func() {
	It("Succeeds", func() {
		db := common.PrepareTestDB("migration_test", &events.Event{})
		err := Migrate(db)
		Expect(err).ToNot(HaveOccurred())
	})
})
