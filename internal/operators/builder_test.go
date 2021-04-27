package operators

import (
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/operators/api"
	"github.com/openshift/assisted-service/models"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Operators manager builder", func() {
	var (
		log                  = logrus.New()
		ctrl                 *gomock.Controller
		operator1, operator2 *api.MockOperator
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		operator1 = api.NewMockOperator(ctrl)
		operator2 = api.NewMockOperator(ctrl)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("should add OLM and builtin operators to monitored operators - without cvo", func() {
		operator1Name := "operator-1"
		monitoredOperator1 := &models.MonitoredOperator{}
		operator1.EXPECT().GetName().AnyTimes().Return(operator1Name)
		operator1.EXPECT().GetMonitoredOperator().Return(monitoredOperator1)

		operator2Name := "operator-2"
		monitoredOperator2 := &models.MonitoredOperator{}
		operator2.EXPECT().GetName().AnyTimes().Return(operator2Name)
		operator2.EXPECT().GetMonitoredOperator().Return(monitoredOperator2)

		options := Options{CheckClusterVersion: false}
		manager := NewManagerWithOperators(log, nil, options, nil, operator1, operator2)
		Expect(manager).ToNot(BeNil())

		monitoredOperatorsList := manager.GetMonitoredOperatorsList()
		Expect(monitoredOperatorsList).To(HaveLen(3))
		// OLM
		Expect(monitoredOperatorsList).To(HaveKeyWithValue(operator1Name, monitoredOperator1))
		Expect(monitoredOperatorsList).To(HaveKeyWithValue(operator2Name, monitoredOperator2))
		// builtins
		Expect(monitoredOperatorsList).To(HaveKeyWithValue(OperatorConsole.Name, &OperatorConsole))
	})

	It("should add OLM and builtin operators to monitored operators - with cvo", func() {
		operator1Name := "operator-1"
		monitoredOperator1 := &models.MonitoredOperator{}
		operator1.EXPECT().GetName().AnyTimes().Return(operator1Name)
		operator1.EXPECT().GetMonitoredOperator().Return(monitoredOperator1)

		operator2Name := "operator-2"
		monitoredOperator2 := &models.MonitoredOperator{}
		operator2.EXPECT().GetName().AnyTimes().Return(operator2Name)
		operator2.EXPECT().GetMonitoredOperator().Return(monitoredOperator2)

		options := Options{CheckClusterVersion: true}
		manager := NewManagerWithOperators(log, nil, options, nil, operator1, operator2)
		Expect(manager).ToNot(BeNil())

		monitoredOperatorsList := manager.GetMonitoredOperatorsList()
		Expect(monitoredOperatorsList).To(HaveLen(4))
		// OLM
		Expect(monitoredOperatorsList).To(HaveKeyWithValue(operator1Name, monitoredOperator1))
		Expect(monitoredOperatorsList).To(HaveKeyWithValue(operator2Name, monitoredOperator2))
		// builtins
		Expect(monitoredOperatorsList).To(HaveKeyWithValue(OperatorConsole.Name, &OperatorConsole))
		Expect(monitoredOperatorsList).To(HaveKeyWithValue(OperatorCVO.Name, &OperatorCVO))
	})
})
