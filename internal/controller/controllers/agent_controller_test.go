package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/controller/api/v1beta1"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fakeclient "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newHostRequest(host *v1beta1.Agent) ctrl.Request {
	namespacedName := types.NamespacedName{
		Namespace: host.ObjectMeta.Namespace,
		Name:      host.ObjectMeta.Name,
	}
	return ctrl.Request{NamespacedName: namespacedName}
}

func newAgent(name, namespace string, spec v1beta1.AgentSpec) *v1beta1.Agent {
	return &v1beta1.Agent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}
}

var _ = Describe("agent reconcile", func() {
	var (
		c                     client.Client
		hr                    *AgentReconciler
		ctx                   = context.Background()
		mockCtrl              *gomock.Controller
		mockInstallerInternal *bminventory.MockInstallerInternals
		sId                   strfmt.UUID
		backEndCluster        *common.Cluster
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal = bminventory.NewMockInstallerInternals(mockCtrl)
		hr = &AgentReconciler{
			Client:    c,
			Scheme:    scheme.Scheme,
			Log:       common.GetTestLog(),
			Installer: mockInstallerInternal,
		}
		sId = strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{ID: &sId}}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	It("none existing agent", func() {
		host := newAgent("host", testNamespace, v1beta1.AgentSpec{})
		Expect(c.Create(ctx, host)).To(BeNil())

		noneExistingHost := newAgent("host2", testNamespace, v1beta1.AgentSpec{})

		result, err := hr.Reconcile(ctx, newHostRequest(noneExistingHost))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
	})

	It("cluster deployment not set", func() {
		host := newAgent("host", testNamespace, v1beta1.AgentSpec{})
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "host",
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
	})

	It("cluster deployment not found", func() {
		host := newAgent("host", testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "host",
		}
		Expect(c.Get(ctx, key, agent).Error()).To(Equal("agents.agent-install.openshift.io \"host\" not found"))
	})

	It("cluster not found in database", func() {
		host := newAgent("host", testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		Expect(c.Create(ctx, host)).To(BeNil())
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound).Times(1)
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "host",
		}
		Expect(c.Get(ctx, key, agent).Error()).To(Equal("agents.agent-install.openshift.io \"host\" not found"))
	})

	It("error getting cluster from database", func() {
		host := newAgent("host", testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		Expect(c.Create(ctx, host)).To(BeNil())
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		errString := "Error getting Cluster"
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, common.NewApiError(http.StatusInternalServerError,
			errors.New(errString))).Times(1)
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "host",
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, errString)
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("host not found in cluster", func() {
		host := newAgent("host", testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "host",
		}
		Expect(c.Get(ctx, key, agent).Error()).To(Equal("agents.agent-install.openshift.io \"host\" not found"))
	})

	It("Agent update", func() {
		newHostName := "hostname123"
		newRole := "worker"
		newInstallDiskPath := "/dev/disk/by-id/wwn-0x6141877064533b0020adf3bb03167694"
		hostId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID:         &hostId,
					Inventory:  common.GenerateTestDefaultInventory(),
					Status:     swag.String(models.HostStatusKnown),
					StatusInfo: swag.String("Some status info"),
				},
			}}}
		updateReply := &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID:                &hostId,
					RequestedHostname: newHostName,
					Status:            swag.String(models.HostStatusKnown),
					StatusInfo:        swag.String("Some status info"),
				},
			}}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		host.Spec.Hostname = newHostName
		host.Spec.Role = models.HostRole(newRole)
		host.Spec.InstallationDiskID = newInstallDiskPath
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{}, nil)
		mockInstallerInternal.EXPECT().UpdateClusterInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, param installer.UpdateClusterParams) {
				Expect(param.ClusterUpdateParams.DisksSelectedConfig[0].DisksConfig[0].ID).To(Equal(&newInstallDiskPath))
				Expect(param.ClusterUpdateParams.DisksSelectedConfig[0].DisksConfig[0].Role).To(Equal(models.DiskRoleInstall))
				Expect(param.ClusterUpdateParams.DisksSelectedConfig[0].ID).To(Equal(hostId))
				Expect(param.ClusterUpdateParams.HostsNames[0].Hostname).To(Equal(newHostName))
				Expect(param.ClusterUpdateParams.HostsNames[0].ID).To(Equal(hostId))
				Expect(param.ClusterUpdateParams.HostsRoles[0].Role).To(Equal(models.HostRoleUpdateParams(models.HostRole(newRole))))
			}).Return(updateReply, nil)
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Agent update empty disk path", func() {
		newInstallDiskPath := ""
		hostId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID:                 &hostId,
					Inventory:          common.GenerateTestDefaultInventory(),
					InstallationDiskID: "/dev/disk/by-id/wwn-0x1111111111111111111111",
					Status:             swag.String(models.HostStatusKnown),
					StatusInfo:         swag.String("Some status info"),
				},
			}}}

		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		host.Spec.InstallationDiskID = newInstallDiskPath
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{}, nil)
		mockInstallerInternal.EXPECT().UpdateClusterInternal(gomock.Any(), gomock.Any()).Return(nil, nil).Times(0)
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Agent update error", func() {
		newHostName := "hostname123"
		newRole := "worker"
		hostId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID: &hostId,
				},
			}}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		host.Spec.Hostname = newHostName
		host.Spec.Role = models.HostRole(newRole)
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{}, nil)
		errString := "update internal error"
		mockInstallerInternal.EXPECT().UpdateClusterInternal(gomock.Any(), gomock.Any()).Return(nil, common.NewApiError(http.StatusInternalServerError,
			errors.New(errString)))
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, errString)
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, InstalledCondition).Status).To(Equal(corev1.ConditionUnknown))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, ReadyForInstallationCondition).Status).To(Equal(corev1.ConditionUnknown))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, ValidatedCondition).Status).To(Equal(corev1.ConditionUnknown))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, ConnectedCondition).Status).To(Equal(corev1.ConditionUnknown))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, ConnectedCondition).Message).To(Equal(NotAvailableMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, ConnectedCondition).Reason).To(Equal(NotAvailableReason))
	})

	It("Agent update approved", func() {
		hostId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID:         &hostId,
					Status:     swag.String(models.HostStatusKnown),
					StatusInfo: swag.String("Some status info"),
				},
			}}}

		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		host.Spec.Approved = true
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: false}, nil)
		mockInstallerInternal.EXPECT().UpdateHostApprovedInternal(gomock.Any(), gomock.Any(), gomock.Any(), true).Return(nil)
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Agent update ignition override valid cases", func() {
		hostId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID: &hostId,
				},
			}}}
		ignitionConfigOverrides := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).AnyTimes()

		By("Reconcile without setting ignition override, validate update ignition override didn't run")
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: false}, nil)
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))

		By("Reconcile add update ignition override, validate UpdateHostIgnitionInternal run once")
		mockInstallerInternal.EXPECT().UpdateHostIgnitionInternal(gomock.Any(), gomock.Any()).Return(nil, nil)

		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: false}, nil)
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		agent.Spec.IgnitionConfigOverrides = ignitionConfigOverrides
		Expect(c.Update(ctx, agent)).To(BeNil())
		result, err = hr.Reconcile(ctx, newHostRequest(agent))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(agent.Spec.IgnitionConfigOverrides).To(Equal(ignitionConfigOverrides))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))

	})

	It("Agent update ignition config errors", func() {
		hostId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID: &hostId,
				},
			}}}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}

		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).AnyTimes()
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		By("Reconcile with ignition config, UpdateHostIgnitionInternal returns error")
		ignitionConfigOverrides := `{"ignition": "version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{}, nil)
		errString := "update internal error"
		mockInstallerInternal.EXPECT().UpdateHostIgnitionInternal(gomock.Any(), gomock.Any()).Return(nil, errors.Errorf(errString)).Times(1)
		host.Spec.IgnitionConfigOverrides = ignitionConfigOverrides
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		Expect(c.Get(ctx, key, host)).To(BeNil())
		expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, errString)
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, SpecSyncedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("Agent update installer args valid cases", func() {
		hostId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID: &hostId,
				},
			}}}

		installerArgs := `["--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).AnyTimes()

		By("Reconcile without setting args, validate update installer args didn't run")
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		// mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: false}, nil)
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))

		By("Reconcile add update installer args, validate UpdateHostInstallerArgsInternal run once")
		mockInstallerInternal.EXPECT().UpdateHostInstallerArgsInternal(gomock.Any(), gomock.Any()).Return(nil, nil)

		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: false}, nil)
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		agent.Spec.InstallerArgs = installerArgs
		Expect(c.Update(ctx, agent)).To(BeNil())
		result, err = hr.Reconcile(ctx, newHostRequest(agent))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(agent.Spec.InstallerArgs).To(Equal(installerArgs))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))

		By("Reconcile with same installer args, validate UpdateHostInstallerArgsInternal didn't run")
		var j []string
		err = json.Unmarshal([]byte(agent.Spec.InstallerArgs), &j)
		Expect(err).To(BeNil())
		arrBytes, _ := json.Marshal(j)
		internalHost := &common.Host{}
		internalHost.InstallerArgs = string(arrBytes)
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(internalHost, nil)
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(agent.Spec.InstallerArgs).To(Equal(installerArgs))
		result, err = hr.Reconcile(ctx, newHostRequest(agent))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Agent update installer args errors", func() {

		hostId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID: &hostId,
				},
			}}}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}

		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).AnyTimes()
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		By("Reconcile with bad json in installer args, validate UpdateHostInstallerArgsInternal didn't run")
		installerArgs := `"--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{Approved: false}, nil)
		host.Spec.InstallerArgs = installerArgs
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{Requeue: false}))
		Expect(c.Get(ctx, key, host)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(InputErrorReason))
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))

		By("Reconcile with installer args, UpdateHostInstallerArgsInternal returns error")
		installerArgs = `["--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{}, nil)
		errString := "update internal error"
		mockInstallerInternal.EXPECT().UpdateHostInstallerArgsInternal(gomock.Any(), gomock.Any()).Return(nil, errors.Errorf(errString)).Times(1)
		host.Spec.InstallerArgs = installerArgs
		Expect(c.Update(ctx, host)).To(BeNil())
		result, err = hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		Expect(c.Get(ctx, key, host)).To(BeNil())
		expectedState := fmt.Sprintf("%s %s", BackendErrorMsg, errString)
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, SpecSyncedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("Agent inventory status", func() {
		macAddress := "some MAC address"
		hostId := strfmt.UUID(uuid.New().String())
		inventory := models.Inventory{
			Interfaces: []*models.Interface{
				{
					Name: "eth0",
					IPV4Addresses: []string{
						"1.2.3.4/24",
					},
					IPV6Addresses: []string{
						"1001:db8::10/120",
					},
					MacAddress: macAddress,
				},
			},
			Disks: []*models.Disk{
				{Path: "/dev/sda", Bootable: true},
				{Path: "/dev/sdb", Bootable: false},
			},
		}
		inv, _ := json.Marshal(&inventory)

		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID:         &hostId,
					Inventory:  string(inv),
					Status:     swag.String(models.HostStatusKnown),
					StatusInfo: swag.String("Some status info"),
				},
			}}}

		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{}, nil)
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Message).To(Equal(SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Reason).To(Equal(SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
		Expect(agent.Status.Inventory.Interfaces[0].MacAddress).To(Equal(macAddress))
	})

})

var _ = Describe("TestConditions", func() {
	var (
		c              client.Client
		hr             *AgentReconciler
		ctx            = context.Background()
		mockCtrl       *gomock.Controller
		backEndCluster *common.Cluster
		hostRequest    ctrl.Request
		agentKey       types.NamespacedName
	)

	BeforeEach(func() {
		c = fakeclient.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		mockCtrl = gomock.NewController(GinkgoT())
		mockInstallerInternal := bminventory.NewMockInstallerInternals(mockCtrl)
		hr = &AgentReconciler{
			Client:    c,
			Scheme:    scheme.Scheme,
			Log:       common.GetTestLog(),
			Installer: mockInstallerInternal,
		}
		sId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{ID: &sId}}
		hostId := strfmt.UUID(uuid.New().String())
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				{
					ID: &hostId,
				},
			}}}
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().GetCommonHostInternal(gomock.Any(), gomock.Any(), gomock.Any()).Return(&common.Host{}, nil)
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		Expect(c.Create(ctx, host)).To(BeNil())
		hostRequest = newHostRequest(host)
		agentKey = types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	tests := []struct {
		name           string
		hostStatus     string
		statusInfo     string
		validationInfo string
		conditions     []conditionsv1.Condition
	}{
		{
			name:           "PendingForInput",
			hostStatus:     models.HostStatusPendingForInput,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking1\",\"status\":\"failure\",\"message\":\"Host check1 is not OK\"},{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Host check2 is OK\"},{\"id\":\"checking3\",\"status\":\"failure\",\"message\":\"Host check3 is not OK\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    ReadyForInstallationCondition,
					Message: AgentNotReadyMsg,
					Reason:  AgentNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ConnectedCondition,
					Message: AgentConnectedMsg,
					Reason:  AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    InstalledCondition,
					Message: InstallationNotStartedMsg,
					Reason:  InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ValidatedCondition,
					Message: AgentValidationsFailingMsg + " Host check1 is not OK,Host check3 is not OK",
					Reason:  ValidationsFailingReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "Unsufficient",
			hostStatus:     models.HostStatusInsufficient,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking1\",\"status\":\"failure\",\"message\":\"Host check1 is not OK\"},{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Host check2 is OK\"},{\"id\":\"checking3\",\"status\":\"failure\",\"message\":\"Host check3 is not OK\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    ReadyForInstallationCondition,
					Message: AgentNotReadyMsg,
					Reason:  AgentNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ConnectedCondition,
					Message: AgentConnectedMsg,
					Reason:  AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    InstalledCondition,
					Message: InstallationNotStartedMsg,
					Reason:  InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ValidatedCondition,
					Message: AgentValidationsFailingMsg + " Host check1 is not OK,Host check3 is not OK",
					Reason:  ValidationsFailingReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "Known",
			hostStatus:     models.HostStatusKnown,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    ReadyForInstallationCondition,
					Message: AgentReadyMsg,
					Reason:  AgentReadyReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ConnectedCondition,
					Message: AgentConnectedMsg,
					Reason:  AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    InstalledCondition,
					Message: InstallationNotStartedMsg,
					Reason:  InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ValidatedCondition,
					Message: AgentValidationsPassingMsg,
					Reason:  ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "Installed",
			hostStatus:     models.HostStatusInstalled,
			statusInfo:     "Done",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    ReadyForInstallationCondition,
					Message: AgentAlreadyInstallingMsg,
					Reason:  AgentAlreadyInstallingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ConnectedCondition,
					Message: AgentConnectedMsg,
					Reason:  AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    InstalledCondition,
					Message: InstalledMsg + " Done",
					Reason:  InstalledReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    ValidatedCondition,
					Message: AgentValidationsPassingMsg,
					Reason:  ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "Installing",
			hostStatus:     models.HostStatusInstalling,
			statusInfo:     "Joined",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    ReadyForInstallationCondition,
					Message: AgentAlreadyInstallingMsg,
					Reason:  AgentAlreadyInstallingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ConnectedCondition,
					Message: AgentConnectedMsg,
					Reason:  AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    InstalledCondition,
					Message: InstallationInProgressMsg + " Joined",
					Reason:  InstallationInProgressReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ValidatedCondition,
					Message: AgentValidationsPassingMsg,
					Reason:  ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "Error",
			hostStatus:     models.HostStatusError,
			statusInfo:     "Done",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    ReadyForInstallationCondition,
					Message: AgentAlreadyInstallingMsg,
					Reason:  AgentAlreadyInstallingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ConnectedCondition,
					Message: AgentConnectedMsg,
					Reason:  AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    InstalledCondition,
					Message: InstallationFailedMsg + " Done",
					Reason:  InstallationFailedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ValidatedCondition,
					Message: AgentValidationsPassingMsg,
					Reason:  ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "Disconnected",
			hostStatus:     models.HostStatusDisconnected,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    ReadyForInstallationCondition,
					Message: AgentNotReadyMsg,
					Reason:  AgentNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ConnectedCondition,
					Message: AgentDisonnectedMsg,
					Reason:  AgentDisconnectedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    InstalledCondition,
					Message: InstallationNotStartedMsg,
					Reason:  InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    ValidatedCondition,
					Message: AgentValidationsPassingMsg,
					Reason:  ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
	}

	for i := range tests {
		t := tests[i]
		It(t.name, func() {
			backEndCluster.Hosts[0].Status = swag.String(t.hostStatus)
			backEndCluster.Hosts[0].StatusInfo = swag.String(t.statusInfo)
			backEndCluster.Hosts[0].ValidationsInfo = t.validationInfo
			result, err := hr.Reconcile(ctx, hostRequest)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
			agent := &v1beta1.Agent{}
			Expect(c.Get(ctx, agentKey, agent)).To(BeNil())
			for _, cond := range t.conditions {
				Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, cond.Type).Message).To(Equal(cond.Message))
				Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, cond.Type).Reason).To(Equal(cond.Reason))
				Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, cond.Type).Status).To(Equal(cond.Status))
			}

		})
	}
})
