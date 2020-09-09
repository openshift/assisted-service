package imgexpirer

import (
	"context"
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	"github.com/openshift/assisted-service/pkg/leader"

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
		leaderMock *leader.MockElectorInterface
		log        = logrus.New()
	)

	leaderSuccess := func() {
		leaderMock.EXPECT().IsLeader().Return(true).Times(1)
	}

	BeforeEach(func() {
		log.SetOutput(ioutil.Discard)
		ctrl = gomock.NewController(GinkgoT())
		mockEvents = events.NewMockHandler(ctrl)
		deleteTime, _ := time.ParseDuration("60m")
		leaderMock = leader.NewMockElectorInterface(ctrl)
		imgExp = NewManager(nil, mockEvents, deleteTime, leaderMock)
	})
	It("callback_valid_objname", func() {
		clusterId := "53116787-3eb0-4211-93ac-611d5cedaa30"
		leaderSuccess()
		mockEvents.EXPECT().AddEvent(gomock.Any(), strfmt.UUID(clusterId), nil, models.EventSeverityInfo, gomock.Any(), gomock.Any())
		imgExp.DeletedImageCallback(ctx, log, fmt.Sprintf("discovery-image-%s.iso", clusterId))
	})
	It("callback_invalid_objname", func() {
		clusterId := "53116787-3eb0-4211-93ac-611d5cedaa30"
		leaderSuccess()
		imgExp.DeletedImageCallback(ctx, log, fmt.Sprintf("discovery-image-%s", clusterId))
	})
	It("callback_not_leader", func() {
		clusterId := "53116787-3eb0-4211-93ac-611d5cedaa30"
		leaderMock.EXPECT().IsLeader().Return(false).Times(1)
		imgExp.DeletedImageCallback(ctx, log, fmt.Sprintf("discovery-image-%s.iso", clusterId))
	})

	AfterEach(func() {
		ctrl.Finish()
	})
})
