package host

import (
	"io/ioutil"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/filanov/bm-inventory/models"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/sqlite"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var defaultHwInfo = "default hw info" // invalid hw info used only for tests

func TestSubsystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "host state machine tests")
}

func getHost(hostId, clusterId strfmt.UUID, db *gorm.DB) *models.Host {
	var host models.Host
	Expect(db.First(&host, "id = ? and cluster_id = ?", hostId, clusterId).Error).ShouldNot(HaveOccurred())
	return &host
}

func prepareDB() *gorm.DB {
	db, err := gorm.Open("sqlite3", ":memory:")
	Expect(err).ShouldNot(HaveOccurred())
	//db = db.Debug()
	db.AutoMigrate(&models.Host{})
	return db
}

type expect struct {
	expectError   bool
	expectedState string
	postCheck     func()
}

func postValidation(expectedReply *expect, firstState string, db *gorm.DB, id, clusterId strfmt.UUID,
	updateReply *UpdateReply, updateErr error) {
	if expectedReply != nil {
		h := getHost(id, clusterId, db)
		if expectedReply.expectError {
			Expect(updateReply).To(BeNil())
			Expect(updateErr).Should(HaveOccurred())
			Expect(swag.StringValue(h.Status)).Should(Equal(firstState))
		} else {
			Expect(updateErr).ShouldNot(HaveOccurred())
			Expect(updateReply).NotTo(BeNil())
			Expect(updateReply.State).Should(Equal(expectedReply.expectedState))
			if updateReply.State == firstState {
				Expect(updateReply.IsChanged).Should(BeFalse())
			} else {
				Expect(updateReply.IsChanged).Should(BeTrue())
			}
			Expect(swag.StringValue(h.Status)).Should(Equal(expectedReply.expectedState))
		}

		if expectedReply.postCheck != nil {
			expectedReply.postCheck()
		}
	}
}

func getTestLog() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(ioutil.Discard)
	return l
}
