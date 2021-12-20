package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	"github.com/jinzhu/gorm"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
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
		hostId := strfmt.UUID(uuid.New().String())
		infraEnvId := strfmt.UUID(uuid.New().String())
		host := newAgent("host", testNamespace, v1beta1.AgentSpec{})
		Expect(c.Create(ctx, host)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(&common.Host{Host: models.Host{ID: &hostId, InfraEnvID: infraEnvId}}, nil).AnyTimes()
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
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(&common.Host{}, nil).AnyTimes()
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "host",
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("cluster not found in database", func() {
		host := newAgent("host", testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		Expect(c.Create(ctx, host)).To(BeNil())
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(&common.Host{}, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound).Times(1)
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		agent := &v1beta1.Agent{}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      "host",
		}
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		expectedState := fmt.Sprintf("%s %s", v1beta1.BackendErrorMsg, gorm.ErrRecordNotFound)
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("error getting cluster from database", func() {
		host := newAgent("host", testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		Expect(c.Create(ctx, host)).To(BeNil())
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		errString := "Error getting Cluster"
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(&common.Host{}, nil).AnyTimes()
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
		expectedState := fmt.Sprintf("%s %s", v1beta1.BackendErrorMsg, errString)
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("host not found ", func() {
		host := newAgent("host", testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound).Times(1)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(0)
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
		Expect(agent.ObjectMeta.DeletionTimestamp.IsZero()).To(BeFalse())
	})

	It("Agent update", func() {
		newHostName := "hostname123"
		newRole := "worker"
		newInstallDiskPath := "/dev/disk/by-id/wwn-0x6141877064533b0020adf3bb03167694"
		hostId := strfmt.UUID(uuid.New().String())
		infraEnvId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:         &hostId,
				ClusterID:  &sId,
				Inventory:  common.GenerateTestDefaultInventory(),
				Status:     swag.String(models.HostStatusKnown),
				StatusInfo: swag.String("Some status info"),
				InfraEnvID: infraEnvId,
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		host.Spec.Hostname = newHostName
		host.Spec.Role = models.HostRole(newRole)
		host.Spec.InstallationDiskID = newInstallDiskPath
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
		mockInstallerInternal.EXPECT().V2UpdateHostInternal(gomock.Any(), gomock.Any()).
			Do(func(ctx context.Context, param installer.V2UpdateHostParams) {
				Expect(param.HostUpdateParams.DisksSelectedConfig[0].ID).To(Equal(&newInstallDiskPath))
				Expect(param.HostUpdateParams.DisksSelectedConfig[0].Role).To(Equal(models.DiskRoleInstall))
				Expect(swag.StringValue(param.HostUpdateParams.HostName)).To(Equal(newHostName))
				Expect(param.HostID).To(Equal(hostId))
				Expect(param.InfraEnvID).To(Equal(infraEnvId))
				Expect(param.HostUpdateParams.HostRole).To(Equal(&newRole))
			}).Return(&common.Host{}, nil)
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
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Agent update empty disk path", func() {
		newInstallDiskPath := ""
		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:                 &hostId,
				ClusterID:          &sId,
				Inventory:          common.GenerateTestDefaultInventory(),
				InstallationDiskID: "/dev/disk/by-id/wwn-0x1111111111111111111111",
				Status:             swag.String(models.HostStatusKnown),
				StatusInfo:         swag.String("Some status info"),
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		host.Spec.InstallationDiskID = newInstallDiskPath
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
		mockInstallerInternal.EXPECT().V2UpdateHostInternal(gomock.Any(), gomock.Any()).Return(nil, nil).Times(0)
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
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Host parameters are not updated post install", func() {
		newRole := "worker"
		hostId := strfmt.UUID(uuid.New().String())
		status := models.HostStatusPreparingForInstallation
		commonHost := &common.Host{
			Host: models.Host{
				ID:        &hostId,
				ClusterID: &sId,
				Status:    &status,
				Role:      models.HostRoleMaster,
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		host.Spec.Hostname = "newhostname"
		host.Spec.Role = models.HostRole(newRole)
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
		mockInstallerInternal.EXPECT().V2UpdateHostInternal(gomock.Any(), gomock.Any()).Return(nil, nil).Times(0)
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
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Agent update error", func() {
		newHostName := "hostname123"
		newRole := "worker"
		status := models.HostStatusKnown
		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:        &hostId,
				ClusterID: &sId,
				Status:    &status,
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		host.Spec.Hostname = newHostName
		host.Spec.Role = models.HostRole(newRole)
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
		errString := "update internal error"
		mockInstallerInternal.EXPECT().V2UpdateHostInternal(gomock.Any(), gomock.Any()).Return(nil, common.NewApiError(http.StatusInternalServerError,
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
		expectedState := fmt.Sprintf("%s %s", v1beta1.BackendErrorMsg, errString)
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.InstalledCondition).Status).To(Equal(corev1.ConditionFalse))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.RequirementsMetCondition).Status).To(Equal(corev1.ConditionFalse))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.ValidatedCondition).Status).To(Equal(corev1.ConditionUnknown))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.ConnectedCondition).Status).To(Equal(corev1.ConditionTrue))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.ConnectedCondition).Message).To(Equal(v1beta1.AgentConnectedMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.ConnectedCondition).Reason).To(Equal(v1beta1.AgentConnectedReason))
	})

	It("Agent update approved", func() {
		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:         &hostId,
				ClusterID:  &sId,
				Inventory:  common.GenerateTestDefaultInventory(),
				Status:     swag.String(models.HostStatusKnown),
				StatusInfo: swag.String("Some status info"),
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}

		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		host.Spec.Approved = true
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
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
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Agent unbind", func() {
		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:         &hostId,
				ClusterID:  &sId,
				Inventory:  common.GenerateTestDefaultInventory(),
				Status:     swag.String(models.HostStatusKnown),
				StatusInfo: swag.String("Some status info"),
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}

		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		host.Spec.ClusterDeploymentName = nil
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil)
		mockInstallerInternal.EXPECT().UnbindHostInternal(gomock.Any(), gomock.Any()).Return(commonHost, nil)
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
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Agent bind", func() {
		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:         &hostId,
				Inventory:  common.GenerateTestDefaultInventory(),
				Status:     swag.String(models.HostStatusKnown),
				StatusInfo: swag.String("Some status info"),
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID:    &sId,
			Hosts: []*models.Host{}}}

		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		mockInstallerInternal.EXPECT().BindHostInternal(gomock.Any(), gomock.Any()).Return(commonHost, nil)
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
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Agent bind, cluster not found in DB and recover", func() {
		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:         &hostId,
				Inventory:  common.GenerateTestDefaultInventory(),
				Status:     swag.String(models.HostStatusKnown),
				StatusInfo: swag.String("Some status info"),
			},
		}

		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(nil, gorm.ErrRecordNotFound).Times(1)
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
		expectedState := fmt.Sprintf("%s %s", v1beta1.BackendErrorMsg, gorm.ErrRecordNotFound)
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))

		By("Reconcile again with existing cluster")
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
		mockInstallerInternal.EXPECT().BindHostInternal(gomock.Any(), gomock.Any()).Return(commonHost, nil).Times(1)
		result, err = hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))

	})

	It("Move Agent- unbind", func() {
		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:         &hostId,
				ClusterID:  &sId,
				Inventory:  common.GenerateTestDefaultInventory(),
				Status:     swag.String(models.HostStatusKnown),
				StatusInfo: swag.String("Some status info"),
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		targetId := strfmt.UUID(uuid.New().String())
		targetBECluster := &common.Cluster{Cluster: models.Cluster{
			ID: &targetId}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(targetBECluster, nil)
		mockInstallerInternal.EXPECT().UnbindHostInternal(gomock.Any(), gomock.Any()).Return(commonHost, nil)
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
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("validate Event URL", func() {
		_, priv, err := gencrypto.ECDSAKeyPairPEM()
		Expect(err).NotTo(HaveOccurred())
		os.Setenv("EC_PRIVATE_KEY_PEM", priv)
		defer os.Unsetenv("EC_PRIVATE_KEY_PEM")
		Expect(err).NotTo(HaveOccurred())
		serviceBaseURL := "http://acme.com"
		hr.ServiceBaseURL = serviceBaseURL
		hostId := strfmt.UUID(uuid.New().String())
		expectedEventUrlPrefix := fmt.Sprintf("%s/api/assisted-install/v1/clusters/%s/events?host_id=%s", serviceBaseURL, sId, hostId.String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:         &hostId,
				ClusterID:  &sId,
				Status:     swag.String(models.HostStatusKnown),
				StatusInfo: swag.String("Some status info"),
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
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

		Expect(agent.Status.DebugInfo.EventsURL).NotTo(BeNil())
		Expect(agent.Status.DebugInfo.EventsURL).To(HavePrefix(expectedEventUrlPrefix))
	})

	It("Agent update ignition override valid cases", func() {
		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:        &hostId,
				ClusterID: &sId,
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}

		ignitionConfigOverrides := `{"ignition": {"version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)

		By("Reconcile without setting ignition override, validate update ignition override didn't run")
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
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
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))

		By("Reconcile add update ignition override, validate UpdateHostIgnitionInternal run once")
		mockInstallerInternal.EXPECT().V2UpdateHostIgnitionInternal(gomock.Any(), gomock.Any()).Return(nil, nil)

		Expect(c.Get(ctx, key, agent)).To(BeNil())
		agent.Spec.IgnitionConfigOverrides = ignitionConfigOverrides
		Expect(c.Update(ctx, agent)).To(BeNil())
		result, err = hr.Reconcile(ctx, newHostRequest(agent))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(agent.Spec.IgnitionConfigOverrides).To(Equal(ignitionConfigOverrides))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))

	})

	It("Agent update ignition config errors", func() {
		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:        &hostId,
				ClusterID: &sId,
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}

		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}

		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		By("Reconcile with ignition config, UpdateHostIgnitionInternal returns error")
		ignitionConfigOverrides := `{"ignition": "version": "3.1.0"}, "storage": {"files": [{"path": "/tmp/example", "contents": {"source": "data:text/plain;base64,aGVscGltdHJhcHBlZGluYXN3YWdnZXJzcGVj"}}]}}`
		errString := "update internal error"
		mockInstallerInternal.EXPECT().V2UpdateHostIgnitionInternal(gomock.Any(), gomock.Any()).Return(nil, errors.Errorf(errString)).Times(1)
		host.Spec.IgnitionConfigOverrides = ignitionConfigOverrides
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		Expect(c.Get(ctx, key, host)).To(BeNil())
		expectedState := fmt.Sprintf("%s %s", v1beta1.BackendErrorMsg, errString)
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
	})

	It("Agent update installer args valid cases", func() {
		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:        &hostId,
				ClusterID: &sId,
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}

		installerArgs := `["--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)

		By("Reconcile without setting args, validate update installer args didn't run")
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
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
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))

		By("Reconcile add update installer args, validate UpdateHostInstallerArgsInternal run once")
		mockInstallerInternal.EXPECT().V2UpdateHostInstallerArgsInternal(gomock.Any(), gomock.Any()).Return(nil, nil)
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)

		Expect(c.Get(ctx, key, agent)).To(BeNil())
		agent.Spec.InstallerArgs = installerArgs
		Expect(c.Update(ctx, agent)).To(BeNil())
		result, err = hr.Reconcile(ctx, newHostRequest(agent))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(agent.Spec.InstallerArgs).To(Equal(installerArgs))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))

		By("Reconcile with same installer args, validate UpdateHostInstallerArgsInternal didn't run")
		var j []string
		err = json.Unmarshal([]byte(installerArgs), &j)
		Expect(err).To(BeNil())
		arrBytes, _ := json.Marshal(j)
		commonHostUpdated := &common.Host{
			Host: models.Host{
				ID:            &hostId,
				ClusterID:     &sId,
				InstallerArgs: string(arrBytes),
			},
		}
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHostUpdated, nil)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil)
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(agent.Spec.InstallerArgs).To(Equal(installerArgs))
		result, err = hr.Reconcile(ctx, newHostRequest(agent))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{}))
		Expect(c.Get(ctx, key, agent)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
	})

	It("Agent update installer args errors", func() {

		hostId := strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:        &hostId,
				ClusterID: &sId,
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		key := types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}

		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).AnyTimes()
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())

		By("Reconcile with bad json in installer args, validate UpdateHostInstallerArgsInternal didn't run")
		installerArgs := `"--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`
		host.Spec.InstallerArgs = installerArgs
		Expect(c.Create(ctx, host)).To(BeNil())
		result, err := hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{Requeue: false}))
		Expect(c.Get(ctx, key, host)).To(BeNil())
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.InputErrorReason))
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))

		By("Reconcile with installer args, UpdateHostInstallerArgsInternal returns error")
		installerArgs = `["--append-karg", "ip=192.0.2.2::192.0.2.254:255.255.255.0:core0.example.com:enp1s0:none", "--save-partindex", "1", "-n"]`
		errString := "update internal error"
		mockInstallerInternal.EXPECT().V2UpdateHostInstallerArgsInternal(gomock.Any(), gomock.Any()).Return(nil, errors.Errorf(errString)).Times(1)
		host.Spec.InstallerArgs = installerArgs
		Expect(c.Update(ctx, host)).To(BeNil())
		result, err = hr.Reconcile(ctx, newHostRequest(host))
		Expect(err).To(BeNil())
		Expect(result).To(Equal(ctrl.Result{RequeueAfter: defaultRequeueAfterOnError}))
		Expect(c.Get(ctx, key, host)).To(BeNil())
		expectedState := fmt.Sprintf("%s %s", v1beta1.BackendErrorMsg, errString)
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(expectedState))
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.BackendErrorReason))
		Expect(conditionsv1.FindStatusCondition(host.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionFalse))
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
		commonHost := &common.Host{
			Host: models.Host{
				ID:         &hostId,
				ClusterID:  &sId,
				Inventory:  string(inv),
				Status:     swag.String(models.HostStatusKnown),
				StatusInfo: swag.String("Some status info"),
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
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
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
		Expect(agent.Status.Inventory.Interfaces[0].MacAddress).To(Equal(macAddress))
	})

	It("Agent ntp sources, role, bootstrap status", func() {
		hostId := strfmt.UUID(uuid.New().String())
		srcName := "1.1.1.1"
		srcState := models.SourceStateError
		role := models.HostRoleMaster
		bootStrap := true
		ntpSources := []*models.NtpSource{
			{
				SourceName:  srcName,
				SourceState: srcState,
			},
		}
		ntp, _ := json.Marshal(&ntpSources)
		commonHost := &common.Host{
			Host: models.Host{
				ID:         &hostId,
				ClusterID:  &sId,
				Role:       role,
				Bootstrap:  bootStrap,
				NtpSources: string(ntp),
				Status:     swag.String(models.HostStatusKnown),
				StatusInfo: swag.String("Some status info"),
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).Times(1)
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
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
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Message).To(Equal(v1beta1.SyncedOkMsg))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Reason).To(Equal(v1beta1.SyncedOkReason))
		Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, v1beta1.SpecSyncedCondition).Status).To(Equal(corev1.ConditionTrue))
		Expect(agent.Status.NtpSources[0].SourceName).To(Equal(srcName))
		Expect(agent.Status.NtpSources[0].SourceState).To(Equal(srcState))
		Expect(agent.Status.Role).To(Equal(role))
		Expect(agent.Status.Bootstrap).To(Equal(bootStrap))
	})

	It("Agent progress status", func() {
		hostId := strfmt.UUID(uuid.New().String())
		progress := &models.HostProgressInfo{
			CurrentStage:   models.HostStageConfiguring,
			ProgressInfo:   "some info",
			StageStartedAt: strfmt.DateTime(time.Now()),
			StageUpdatedAt: strfmt.DateTime(time.Now()),
		}
		commonHost := &common.Host{
			Host: models.Host{
				ID:         &hostId,
				ClusterID:  &sId,
				Inventory:  common.GenerateTestDefaultInventory(),
				Status:     swag.String(models.HostStatusKnown),
				StatusInfo: swag.String("Some status info"),
				Progress:   progress,
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
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
		Expect(agent.Status.Progress.ProgressInfo).To(Equal(progress.ProgressInfo))
		Expect(agent.Status.Progress.StageStartTime).NotTo(BeNil())
		Expect(agent.Status.Progress.StageUpdateTime).NotTo(BeNil())
	})

})

var _ = Describe("TestConditions", func() {
	var (
		c                     client.Client
		hr                    *AgentReconciler
		ctx                   = context.Background()
		mockCtrl              *gomock.Controller
		backEndCluster        *common.Cluster
		hostRequest           ctrl.Request
		agentKey              types.NamespacedName
		hostId                strfmt.UUID
		mockInstallerInternal *bminventory.MockInstallerInternals
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
		sId := strfmt.UUID(uuid.New().String())
		hostId = strfmt.UUID(uuid.New().String())
		commonHost := &common.Host{
			Host: models.Host{
				ID:        &hostId,
				ClusterID: &sId,
				Inventory: common.GenerateTestDefaultInventory(),
			},
		}
		backEndCluster = &common.Cluster{Cluster: models.Cluster{
			ID: &sId,
			Hosts: []*models.Host{
				&commonHost.Host,
			}}}
		clusterDeployment := newClusterDeployment("clusterDeployment", testNamespace, getDefaultClusterDeploymentSpec("clusterDeployment-test", "test-cluster-aci", "pull-secret"))
		Expect(c.Create(ctx, clusterDeployment)).To(BeNil())
		mockInstallerInternal.EXPECT().GetHostByKubeKey(gomock.Any()).Return(commonHost, nil).AnyTimes()
		agentKey = types.NamespacedName{
			Namespace: testNamespace,
			Name:      hostId.String(),
		}
		mockInstallerInternal.EXPECT().GetClusterByKubeKey(gomock.Any()).Return(backEndCluster, nil).Times(1)
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	tests := []struct {
		name           string
		hostStatus     string
		hostApproved   bool
		statusInfo     string
		validationInfo string
		conditions     []conditionsv1.Condition
	}{
		{
			name:           "PendingForInput",
			hostStatus:     models.HostStatusPendingForInput,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking1\",\"status\":\"failure\",\"message\":\"Host check1 is not OK\"},{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Host check2 is OK\"},{\"id\":\"checking3\",\"status\":\"failure\",\"message\":\"Host check3 is not OK\"},{\"id\":\"checking4\",\"status\":\"pending\",\"message\":\"Host check4 is pending\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentNotReadyMsg,
					Reason:  v1beta1.AgentNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationNotStartedMsg,
					Reason:  v1beta1.InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsUserPendingMsg + " Host check1 is not OK,Host check3 is not OK,Host check4 is pending",
					Reason:  v1beta1.ValidationsUserPendingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.BoundMsg,
					Reason:  v1beta1.BoundReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "Insufficient",
			hostStatus:     models.HostStatusInsufficient,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking1\",\"status\":\"failure\",\"message\":\"Host check1 is not OK\"},{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Host check2 is OK\"},{\"id\":\"checking3\",\"status\":\"failure\",\"message\":\"Host check3 is not OK\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentNotReadyMsg,
					Reason:  v1beta1.AgentNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationNotStartedMsg,
					Reason:  v1beta1.InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsFailingMsg + " Host check1 is not OK,Host check3 is not OK",
					Reason:  v1beta1.ValidationsFailingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.BoundMsg,
					Reason:  v1beta1.BoundReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "InsufficientUnbound",
			hostStatus:     models.HostStatusInsufficientUnbound,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking1\",\"status\":\"failure\",\"message\":\"Host check1 is not OK\"},{\"id\":\"checking2\",\"status\":\"success\",\"message\":\"Host check2 is OK\"},{\"id\":\"checking3\",\"status\":\"failure\",\"message\":\"Host check3 is not OK\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentNotReadyMsg,
					Reason:  v1beta1.AgentNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationNotStartedMsg,
					Reason:  v1beta1.InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsFailingMsg + " Host check1 is not OK,Host check3 is not OK",
					Reason:  v1beta1.ValidationsFailingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.UnboundMsg,
					Reason:  v1beta1.UnboundReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "Known",
			hostStatus:     models.HostStatusKnown,
			hostApproved:   true,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentReadyMsg,
					Reason:  v1beta1.AgentReadyReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationNotStartedMsg,
					Reason:  v1beta1.InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsPassingMsg,
					Reason:  v1beta1.ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.BoundMsg,
					Reason:  v1beta1.BoundReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "KnownUnbound",
			hostStatus:     models.HostStatusKnownUnbound,
			hostApproved:   true,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentReadyMsg,
					Reason:  v1beta1.AgentReadyReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationNotStartedMsg,
					Reason:  v1beta1.InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsPassingMsg,
					Reason:  v1beta1.ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.UnboundMsg,
					Reason:  v1beta1.UnboundReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "Known",
			hostStatus:     models.HostStatusKnown,
			hostApproved:   false,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentIsNotApprovedMsg,
					Reason:  v1beta1.AgentIsNotApprovedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationNotStartedMsg,
					Reason:  v1beta1.InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsPassingMsg,
					Reason:  v1beta1.ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.BoundMsg,
					Reason:  v1beta1.BoundReason,
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
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentInstallationStoppedMsg,
					Reason:  v1beta1.AgentInstallationStoppedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstalledMsg + " Done",
					Reason:  v1beta1.InstalledReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsPassingMsg,
					Reason:  v1beta1.ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.BoundMsg,
					Reason:  v1beta1.BoundReason,
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
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentAlreadyInstallingMsg,
					Reason:  v1beta1.AgentAlreadyInstallingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationInProgressMsg + " Joined",
					Reason:  v1beta1.InstallationInProgressReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsPassingMsg,
					Reason:  v1beta1.ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.BoundMsg,
					Reason:  v1beta1.BoundReason,
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
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentInstallationStoppedMsg,
					Reason:  v1beta1.AgentInstallationStoppedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationFailedMsg + " Done",
					Reason:  v1beta1.InstallationFailedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsPassingMsg,
					Reason:  v1beta1.ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.BoundMsg,
					Reason:  v1beta1.BoundReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "Discovering",
			hostStatus:     models.HostStatusDiscovering,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentNotReadyMsg,
					Reason:  v1beta1.AgentNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationNotStartedMsg,
					Reason:  v1beta1.InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsPassingMsg,
					Reason:  v1beta1.ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.BoundMsg,
					Reason:  v1beta1.BoundReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "DiscoveringUnbound",
			hostStatus:     models.HostStatusDiscoveringUnbound,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentNotReadyMsg,
					Reason:  v1beta1.AgentNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationNotStartedMsg,
					Reason:  v1beta1.InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsPassingMsg,
					Reason:  v1beta1.ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.UnboundMsg,
					Reason:  v1beta1.UnboundReason,
					Status:  corev1.ConditionFalse,
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
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentNotReadyMsg,
					Reason:  v1beta1.AgentNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentDisonnectedMsg,
					Reason:  v1beta1.AgentDisconnectedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationNotStartedMsg,
					Reason:  v1beta1.InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsPassingMsg,
					Reason:  v1beta1.ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.BoundMsg,
					Reason:  v1beta1.BoundReason,
					Status:  corev1.ConditionTrue,
				},
			},
		},
		{
			name:           "Binding",
			hostStatus:     models.HostStatusBinding,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.BindingMsg,
					Reason:  v1beta1.BindingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.BindingMsg,
					Reason:  v1beta1.BindingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.BindingMsg,
					Reason:  v1beta1.BindingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.BindingMsg,
					Reason:  v1beta1.BindingReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "Unbinding",
			hostStatus:     models.HostStatusUnbinding,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.UnbindingMsg,
					Reason:  v1beta1.UnbindingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentConnectedMsg,
					Reason:  v1beta1.AgentConnectedReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.UnbindingMsg,
					Reason:  v1beta1.UnbindingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.UnbindingMsg,
					Reason:  v1beta1.UnbindingReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.UnbindingMsg,
					Reason:  v1beta1.UnbindingReason,
					Status:  corev1.ConditionFalse,
				},
			},
		},
		{
			name:           "DisconnectedUnbound",
			hostStatus:     models.HostStatusDisconnectedUnbound,
			statusInfo:     "",
			validationInfo: "{\"some-check\":[{\"id\":\"checking\",\"status\":\"success\",\"message\":\"Host is checked\"}]}",
			conditions: []conditionsv1.Condition{
				{
					Type:    v1beta1.RequirementsMetCondition,
					Message: v1beta1.AgentNotReadyMsg,
					Reason:  v1beta1.AgentNotReadyReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ConnectedCondition,
					Message: v1beta1.AgentDisonnectedMsg,
					Reason:  v1beta1.AgentDisconnectedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.InstalledCondition,
					Message: v1beta1.InstallationNotStartedMsg,
					Reason:  v1beta1.InstallationNotStartedReason,
					Status:  corev1.ConditionFalse,
				},
				{
					Type:    v1beta1.ValidatedCondition,
					Message: v1beta1.AgentValidationsPassingMsg,
					Reason:  v1beta1.ValidationsPassingReason,
					Status:  corev1.ConditionTrue,
				},
				{
					Type:    v1beta1.BoundCondition,
					Message: v1beta1.UnboundMsg,
					Reason:  v1beta1.UnboundReason,
					Status:  corev1.ConditionFalse,
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

			host := newAgent(hostId.String(), testNamespace, v1beta1.AgentSpec{ClusterDeploymentName: &v1beta1.ClusterReference{Name: "clusterDeployment", Namespace: testNamespace}})
			if t.hostApproved {
				host.Spec.Approved = true
				mockInstallerInternal.EXPECT().UpdateHostApprovedInternal(gomock.Any(), gomock.Any(), gomock.Any(), true).Return(nil)
			}
			Expect(c.Create(ctx, host)).To(BeNil())

			hostRequest = newHostRequest(host)
			result, err := hr.Reconcile(ctx, hostRequest)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
			agent := &v1beta1.Agent{}
			agent.Spec.Approved = true
			Expect(c.Get(ctx, agentKey, agent)).To(BeNil())
			for _, cond := range t.conditions {
				Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, cond.Type).Message).To(Equal(cond.Message))
				Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, cond.Type).Reason).To(Equal(cond.Reason))
				Expect(conditionsv1.FindStatusCondition(agent.Status.Conditions, cond.Type).Status).To(Equal(cond.Status))
			}
			Expect(agent.Status.DebugInfo.State).To(Equal(t.hostStatus))
			Expect(agent.Status.DebugInfo.StateInfo).To(Equal(t.statusInfo))
		})
	}
})
