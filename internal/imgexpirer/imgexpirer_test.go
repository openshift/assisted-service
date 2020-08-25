package imgexpirer

import (
	"context"
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	"github.com/openshift/assisted-service/internal/events"
	"github.com/openshift/assisted-service/models"

	"github.com/go-openapi/strfmt"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

func TestJob(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "imgexpirer")
}

var _ = Describe("imgexpirer", func() {
	var (
		imgExp     *Manager
		ctx        = context.Background()
		ctrl       *gomock.Controller
		mockEvents *events.MockHandler
		log        = logrus.New()
	)
	BeforeEach(func() {
		log.SetOutput(ioutil.Discard)
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		deleteTime, _ := time.ParseDuration("60m")
		imgExp = NewManager(nil, mockEvents, deleteTime)
	})
	It("callback_valid_objname", func() {
		clusterId := "53116787-3eb0-4211-93ac-611d5cedaa30"
		mockEvents.EXPECT().AddEvent(gomock.Any(), strfmt.UUID(clusterId), nil, models.EventSeverityInfo, gomock.Any(), gomock.Any())
		imgExp.DeletedImageCallback(ctx, log, fmt.Sprintf("discovery-image-%s.iso", clusterId))
	})
	It("callback_invalid_objname", func() {
		clusterId := "53116787-3eb0-4211-93ac-611d5cedaa30"
		imgExp.DeletedImageCallback(ctx, log, fmt.Sprintf("discovery-image-%s", clusterId))
	})

	AfterEach(func() {
		ctrl.Finish()
	})
})
