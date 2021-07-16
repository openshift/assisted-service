package oc

import (
	os "os"
	"time"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/executer"
)

var _ = Describe("oc extract", func() {
	var (
		oc           Extracter
		tempFilePath string
		ctrl         *gomock.Controller
		mockExecuter *executer.MockExecuter
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockExecuter = executer.NewMockExecuter(ctrl)
		config := Config{MaxTries: DefaultTries, RetryDelay: time.Millisecond}
		oc = NewExtracter(mockExecuter, config)
		tempFilePath = "/tmp/pull-secret"
		mockExecuter.EXPECT().TempFile(gomock.Any(), gomock.Any()).DoAndReturn(
			func(dir, pattern string) (*os.File, error) {
				tempPullSecretFile, err := os.Create(tempFilePath)
				Expect(err).ShouldNot(HaveOccurred())
				return tempPullSecretFile, nil
			},
		).AnyTimes()
	})

	AfterEach(func() {
		os.Remove(tempFilePath)
	})

	Context("ExtractDatabaseIndex", func() {
		It("extract db from 4.7 openshift version", func() {
			mockExecuter.EXPECT().Execute(gomock.Any(), gomock.Any()).Return("", "", 0).Times(1)

			tempFile, err := oc.ExtractDatabaseIndex(log, "", "4.7", pullSecret)
			Expect(tempFile).ShouldNot(BeEmpty())
			Expect(err).ShouldNot(HaveOccurred())
		})
	})
})
