package job

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("local_job_test", func() {
	var (
		j   LocalJob
		log = logrus.New()
	)

	Context("Execute", func() {
		BeforeEach(func() {
			j = NewLocalJob(log, Config{})

		})

		It("success", func() {
			Expect(j.Execute("echo", "noop.py", nil, log)).ShouldNot(HaveOccurred())
		})

		It("failure", func() {
			Expect(j.Execute("python", "script_not_exist.py", nil, log)).Should(HaveOccurred())
		})

	})
})
