package s3wrapper

import (
	"os"
	"path/filepath"

	"github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Native Xattr Client", func() {

	var (
		log           *logrus.Logger
		baseDir       string
		osXattrClient *OSxattrClient
		file1path     string
		file2path     string
	)

	BeforeEach(func() {
		var err error
		log = logrus.New()
		log.SetOutput(ginkgo.GinkgoWriter)
		baseDir, err = os.MkdirTemp("", "test")
		Expect(err).ToNot(HaveOccurred())
		osXattrClient = NewOSxAttrClient(log, baseDir)
		Expect(osXattrClient).ToNot(BeNil())
		err = os.MkdirAll(filepath.Join(baseDir, "manifests", "openshift"), 0o700)
		Expect(err).ToNot(HaveOccurred())
		err = os.MkdirAll(filepath.Join(baseDir, "manifests", "manifests"), 0o700)
		Expect(err).ToNot(HaveOccurred())
		file1path = filepath.Join(baseDir, "manifests", "openshift", "file1.yaml")
		file2path = filepath.Join(baseDir, "manifests", "manifests", "file2.yaml")
		err = os.WriteFile(file1path, []byte{}, 0o600)
		Expect(err).ToNot(HaveOccurred())
		err = os.WriteFile(file2path, []byte{}, 0o600)
		Expect(err).ToNot(HaveOccurred())
	})

	It("Should add user metadata to a file", func() {
		err := osXattrClient.Set(file1path, file1path, "some-user-attribute", "some-value")
		Expect(err).ToNot(HaveOccurred())
		value, ok, err := osXattrClient.Get(file1path, "some-user-attribute")
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal("some-value"))
	})

	It("File should take multiple attributes and should be able to list and retrieve them", func() {
		err := osXattrClient.Set(file1path, file1path, "some-user-attribute", "some-value")
		Expect(err).ToNot(HaveOccurred())
		err = osXattrClient.Set(file1path, file1path, "some-user-attribute-2", "some-value-2")
		Expect(err).ToNot(HaveOccurred())
		paths, err := osXattrClient.List(file1path)
		Expect(err).ToNot(HaveOccurred())
		Expect(len(paths)).To(Equal(2))
		Expect(paths).To(ContainElement("some-user-attribute"))
		Expect(paths).To(ContainElement("some-user-attribute-2"))
		value, ok, err := osXattrClient.Get(file1path, "some-user-attribute")
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal("some-value"))
		value, ok, err = osXattrClient.Get(file1path, "some-user-attribute-2")
		Expect(err).ToNot(HaveOccurred())
		Expect(ok).To(BeTrue())
		Expect(value).To(Equal("some-value-2"))
	})

	AfterEach(func() {
		err := os.RemoveAll(baseDir)
		Expect(err).ToNot(HaveOccurred())
	})

})
