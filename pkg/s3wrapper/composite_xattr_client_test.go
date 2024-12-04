package s3wrapper

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/golang/mock/gomock"
	"github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Composite Xattr Client", func() {

	var (
		log                        *logrus.Logger
		ctrl                       *gomock.Controller
		baseDir                    string
		osXattrClient              *MockXattrClient
		fileSystemBasedXattrClient XattrClient
		file1path                  string
		file2path                  string
	)

	BeforeEach(func() {
		var err error
		log = logrus.New()
		log.SetOutput(ginkgo.GinkgoWriter)
		baseDir, err = os.MkdirTemp("", "test")
		Expect(err).ToNot(HaveOccurred())
		ctrl = gomock.NewController(GinkgoT())
		fileSystemBasedXattrClient = NewFilesystemBasedXattrClient(log, baseDir)
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

	getExpectedMetadataPath := func(filePath string, attributeName string) string {
		relativePath := filePath[len(baseDir):]
		return fmt.Sprintf("%s%s%s%s", filepath.Join(baseDir, filesystemXattrMetaDataDirectoryName, relativePath), delimiter, "user.", attributeName)
	}

	assertFileMetadataCorrect := func(filepath string, attributeName string, expectedValue string) {
		fileMetadataItemPath := getExpectedMetadataPath(filepath, attributeName)
		data, err := os.ReadFile(fileMetadataItemPath)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data)).To(Equal(expectedValue))
	}

	assertFileSystemBasedAttributeWrite := func(path string, attribute string, value string, compositeXattrClient XattrClient) {
		err := compositeXattrClient.Set("", path, attribute, value)
		Expect(err).ToNot(HaveOccurred())
		assertFileMetadataCorrect(path, attribute, value)
	}

	It("Upgrading to xattr supported natively after unsupported", func() {
		var (
			compositeXattrClient XattrClient
		)

		By("Native xattr is unsupported", func() {
			osXattrClient = NewMockXattrClient(ctrl)
			osXattrClient.EXPECT().IsSupported().Return(false, nil).AnyTimes()
		})
		By("Create composite xattr client", func() {
			var err error
			compositeXattrClient, err = NewCompositeXattrClient(log, osXattrClient, fileSystemBasedXattrClient)
			Expect(err).ToNot(HaveOccurred())
		})
		By("Filesystem xattr client should be used for writes", func() {
			assertFileSystemBasedAttributeWrite(file1path, "arbitrary-attribute", "some-arbitrary-value", compositeXattrClient)
			assertFileSystemBasedAttributeWrite(file1path, "another-arbitrary-attribute", "another-arbitrary-value", compositeXattrClient)
			assertFileSystemBasedAttributeWrite(file2path, "arbitrary-attribute-a", "some-arbitrary-value-a", compositeXattrClient)
			assertFileSystemBasedAttributeWrite(file2path, "arbitrary-attribute-b", "some-arbitrary-value-b", compositeXattrClient)
		})
		By("Native xattr is now supported - simulated upgrade", func() {
			var err error
			compositeXattrClient, err = NewCompositeXattrClient(log, NewOSxAttrClient(log, baseDir), fileSystemBasedXattrClient)
			Expect(err).ToNot(HaveOccurred())
		})
		By("Composite client should return previously stored keys from filesystem", func() {
			value, ok, err := compositeXattrClient.Get(file1path, "arbitrary-attribute")
			Expect(err).ToNot(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(value).To(Equal("some-arbitrary-value"))
		})
		By("Composite client should write to os native xattr, leaving filesystem based xattr intact", func() {
			err := compositeXattrClient.Set(file1path, file1path, "arbitrary-attribute", "a-new-value")
			Expect(err).ToNot(HaveOccurred())
			assertFileMetadataCorrect(file1path, "arbitrary-attribute", "some-arbitrary-value")
		})
		By("should fetch newly written value from the composite client", func() {
			value, ok, err := compositeXattrClient.Get(file1path, "arbitrary-attribute")
			Expect(err).ToNot(HaveOccurred())
			Expect(ok).To(BeTrue())
			Expect(value).To(Equal("a-new-value"))
		})
		By("list from composite xattr client should merge keys", func() {
			items, err := compositeXattrClient.List(file1path)
			Expect(err).ToNot(HaveOccurred())
			Expect(items).To(ContainElement("arbitrary-attribute"))
			Expect(items).To(ContainElement("another-arbitrary-attribute"))
		})
		By("RemoveAll should remove filesystem based user keys", func() {
			err := compositeXattrClient.Set(file2path, file2path, "additional-os-xattr", "some-value")
			Expect(err).ToNot(HaveOccurred())
			items, err := compositeXattrClient.List(file2path)
			Expect(err).ToNot(HaveOccurred())
			Expect(items).To(ContainElement("arbitrary-attribute-a"))
			Expect(items).To(ContainElement("arbitrary-attribute-a"))
			Expect(items).To(ContainElement("additional-os-xattr"))
			err = compositeXattrClient.RemoveAll(file2path)
			Expect(err).ToNot(HaveOccurred())
			items, err = compositeXattrClient.List(file2path)
			Expect(err).ToNot(HaveOccurred())
			Expect(items).ToNot(ContainElement("arbitrary-attribute-a"))
			Expect(items).ToNot(ContainElement("arbitrary-attribute-a"))
			// Placed here by the os native xattr for which we do not delete the keys in this way
			// (xattr data is part of the file itself)
			Expect(items).To(ContainElement("additional-os-xattr"))
		})
	})

	AfterEach(func() {
		ctrl.Finish()
		err := os.RemoveAll(baseDir)
		Expect(err).ToNot(HaveOccurred())
	})

})
