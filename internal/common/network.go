package common

import (
	"reflect"

	"github.com/openshift/assisted-service/models"
)

// OrderNetworksByPrimaryStack is a single function to order any dual-stack network list by primary IP stack
func OrderNetworksByPrimaryStack(items interface{}, primaryStack PrimaryIPStack) interface{} {
	v := reflect.ValueOf(items)

	// Check if it's a slice with exactly 2 elements
	if v.Kind() != reflect.Slice || v.Len() != 2 {
		return items
	}
	if primaryStack == PrimaryIPStackV4 {
		// Should be IPv4 first already
		return items
	}
	// Want IPv6 first
	result := reflect.MakeSlice(v.Type(), 2, 2)
	result.Index(0).Set(v.Index(1))
	result.Index(1).Set(v.Index(0))
	return result.Interface()
}

func OrderClusterNetworks(cluster *Cluster) {
	if cluster.PrimaryIPStack == nil {
		return
	}
	cluster.ClusterNetworks = OrderNetworksByPrimaryStack(cluster.ClusterNetworks, *cluster.PrimaryIPStack).([]*models.ClusterNetwork)
	cluster.ServiceNetworks = OrderNetworksByPrimaryStack(cluster.ServiceNetworks, *cluster.PrimaryIPStack).([]*models.ServiceNetwork)
	cluster.MachineNetworks = OrderNetworksByPrimaryStack(cluster.MachineNetworks, *cluster.PrimaryIPStack).([]*models.MachineNetwork)
	cluster.APIVips = OrderNetworksByPrimaryStack(cluster.APIVips, *cluster.PrimaryIPStack).([]*models.APIVip)
	cluster.IngressVips = OrderNetworksByPrimaryStack(cluster.IngressVips, *cluster.PrimaryIPStack).([]*models.IngressVip)
}
