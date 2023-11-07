package oc

import (
	"fmt"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/pkg/executer"
)

var _ = Describe("oc debug", func() {
	const (
		kubeconfig = "kubeconfig"
		nodeName   = "node1"
	)
	var (
		ctrl     *gomock.Controller
		execMock *executer.MockExecuter
		d        Debug
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		execMock = executer.NewMockExecuter(ctrl)
		d = NewDebugWithExecuter([]byte(kubeconfig), execMock)
	})
	expectOk := func(ret string) {
		execMock.EXPECT().ExecuteWithContext(gomock.Any(),
			"oc",
			"debug",
			"--kubeconfig",
			gomock.Any(),
			fmt.Sprintf("node/%s", nodeName),
			"--",
			"chroot",
			"/host",
			"last",
			"reboot").Return(ret, "", 0)
	}
	It("1 reboot", func() {
		expectOk("reboot   system boot  4.18.0-372.9.1.e Tue Mar  7 04:13   still running\n")
		numReboots, err := d.RebootsForNode(nodeName)
		Expect(err).ToNot(HaveOccurred())
		Expect(numReboots).To(Equal(1))
	})
	It("2 reboot", func() {
		expectOk("reboot   system boot  4.18.0-372.9.1.e Tue Mar  7 04:13   still running\nreboot   system boot  4.18.0-372.9.1.e Sun Mar  5 07:29 - 09:11 (2+01:41)\n")
		numReboots, err := d.RebootsForNode(nodeName)
		Expect(err).ToNot(HaveOccurred())
		Expect(numReboots).To(Equal(2))
	})
	It("with error", func() {
		execMock.EXPECT().ExecuteWithContext(gomock.Any(),
			"oc",
			"debug",
			"--kubeconfig",
			gomock.Any(),
			fmt.Sprintf("node/%s", nodeName),
			"--",
			"chroot",
			"/host",
			"last",
			"reboot").Return("", "This is an error", -1)
		_, err := d.RebootsForNode(nodeName)
		Expect(err).To(HaveOccurred())
	})
})
