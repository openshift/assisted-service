package bminventory

import (
	"testing"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/network"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/restapi/operations/installer"
)

func newTargetConfigurationTestCluster(controlPlaneCount int64, userManagedNetworking bool) *common.Cluster {
	return &common.Cluster{
		Cluster: models.Cluster{
			ControlPlaneCount:     controlPlaneCount,
			UserManagedNetworking: swag.Bool(userManagedNetworking),
		},
	}
}

func TestBuildTargetConfigurationCarriesLoadBalancerOwnershipInputs(t *testing.T) {
	t.Run("multi-node user-managed networking cluster without load balancer in request is user-managed", func(t *testing.T) {
		cluster := newTargetConfigurationTestCluster(3, true)
		params := installer.V2UpdateClusterParams{ClusterUpdateParams: &models.V2ClusterUpdateParams{}}

		target := buildTargetConfiguration(cluster, params)

		if !network.IsLoadBalancerUserManaged(target) {
			t.Errorf("expected target configuration to be user-managed, got load balancer %+v", target.LoadBalancer)
		}
		if common.IsSingleNodeCluster(target) {
			t.Errorf("multi-node cluster must not be classified as single-node")
		}
	})

	t.Run("single-node cluster stays single-node and not user-managed", func(t *testing.T) {
		cluster := newTargetConfigurationTestCluster(1, true)
		params := installer.V2UpdateClusterParams{ClusterUpdateParams: &models.V2ClusterUpdateParams{}}

		target := buildTargetConfiguration(cluster, params)

		if !common.IsSingleNodeCluster(target) {
			t.Errorf("expected single-node classification to be preserved")
		}
		if network.IsLoadBalancerUserManaged(target) {
			t.Errorf("single-node cluster must not be user-managed without an explicit load balancer type")
		}
	})

	t.Run("enabling user-managed networking in request without load balancer is user-managed", func(t *testing.T) {
		cluster := newTargetConfigurationTestCluster(3, false)
		params := installer.V2UpdateClusterParams{ClusterUpdateParams: &models.V2ClusterUpdateParams{
			UserManagedNetworking: swag.Bool(true),
		}}

		target := buildTargetConfiguration(cluster, params)

		if !network.IsLoadBalancerUserManaged(target) {
			t.Errorf("expected requested user-managed networking to yield a user-managed load balancer")
		}
	})

	t.Run("explicit load balancer in request is used as-is", func(t *testing.T) {
		cluster := newTargetConfigurationTestCluster(1, false)
		requested := &models.LoadBalancer{Type: models.LoadBalancerTypeUserManaged}
		params := installer.V2UpdateClusterParams{ClusterUpdateParams: &models.V2ClusterUpdateParams{
			LoadBalancer: requested,
		}}

		target := buildTargetConfiguration(cluster, params)

		if target.LoadBalancer != requested {
			t.Errorf("expected requested load balancer to be used, got %+v", target.LoadBalancer)
		}
	})

	t.Run("control plane count in request is applied", func(t *testing.T) {
		cluster := newTargetConfigurationTestCluster(3, true)
		params := installer.V2UpdateClusterParams{ClusterUpdateParams: &models.V2ClusterUpdateParams{
			ControlPlaneCount: swag.Int64(1),
		}}

		target := buildTargetConfiguration(cluster, params)

		if target.ControlPlaneCount != 1 {
			t.Errorf("expected control plane count 1, got %d", target.ControlPlaneCount)
		}
	})

	t.Run("converting a multi-node user-managed cluster to single-node without an explicit load balancer is not user-managed", func(t *testing.T) {
		cluster := newTargetConfigurationTestCluster(3, true)
		params := installer.V2UpdateClusterParams{ClusterUpdateParams: &models.V2ClusterUpdateParams{
			ControlPlaneCount: swag.Int64(1),
		}}

		target := buildTargetConfiguration(cluster, params)

		if !common.IsSingleNodeCluster(target) {
			t.Errorf("expected single-node classification to be applied")
		}
		if network.IsLoadBalancerUserManaged(target) {
			t.Errorf("single-node target must not inherit user-managed ownership from the stale multi-node source cluster, got load balancer %+v", target.LoadBalancer)
		}
	})
}
