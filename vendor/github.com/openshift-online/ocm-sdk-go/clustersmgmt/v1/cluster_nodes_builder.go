/*
Copyright (c) 2020 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// IMPORTANT: This file has been generated automatically, refrain from modifying it manually as all
// your changes will be lost when the file is generated again.

package v1 // github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1

// ClusterNodesBuilder contains the data and logic needed to build 'cluster_nodes' objects.
//
// Counts of different classes of nodes inside a cluster.
type ClusterNodesBuilder struct {
	bitmap_            uint32
	autoscaleCompute   *MachinePoolAutoscalingBuilder
	availabilityZones  []string
	compute            int
	computeLabels      map[string]string
	computeMachineType *MachineTypeBuilder
	infra              int
	master             int
	total              int
}

// NewClusterNodes creates a new builder of 'cluster_nodes' objects.
func NewClusterNodes() *ClusterNodesBuilder {
	return &ClusterNodesBuilder{}
}

// AutoscaleCompute sets the value of the 'autoscale_compute' attribute to the given value.
//
// Representation of a autoscaling in a machine pool.
func (b *ClusterNodesBuilder) AutoscaleCompute(value *MachinePoolAutoscalingBuilder) *ClusterNodesBuilder {
	b.autoscaleCompute = value
	if value != nil {
		b.bitmap_ |= 1
	} else {
		b.bitmap_ &^= 1
	}
	return b
}

// AvailabilityZones sets the value of the 'availability_zones' attribute to the given values.
//
//
func (b *ClusterNodesBuilder) AvailabilityZones(values ...string) *ClusterNodesBuilder {
	b.availabilityZones = make([]string, len(values))
	copy(b.availabilityZones, values)
	b.bitmap_ |= 2
	return b
}

// Compute sets the value of the 'compute' attribute to the given value.
//
//
func (b *ClusterNodesBuilder) Compute(value int) *ClusterNodesBuilder {
	b.compute = value
	b.bitmap_ |= 4
	return b
}

// ComputeLabels sets the value of the 'compute_labels' attribute to the given value.
//
//
func (b *ClusterNodesBuilder) ComputeLabels(value map[string]string) *ClusterNodesBuilder {
	b.computeLabels = value
	if value != nil {
		b.bitmap_ |= 8
	} else {
		b.bitmap_ &^= 8
	}
	return b
}

// ComputeMachineType sets the value of the 'compute_machine_type' attribute to the given value.
//
// Machine type.
func (b *ClusterNodesBuilder) ComputeMachineType(value *MachineTypeBuilder) *ClusterNodesBuilder {
	b.computeMachineType = value
	if value != nil {
		b.bitmap_ |= 16
	} else {
		b.bitmap_ &^= 16
	}
	return b
}

// Infra sets the value of the 'infra' attribute to the given value.
//
//
func (b *ClusterNodesBuilder) Infra(value int) *ClusterNodesBuilder {
	b.infra = value
	b.bitmap_ |= 32
	return b
}

// Master sets the value of the 'master' attribute to the given value.
//
//
func (b *ClusterNodesBuilder) Master(value int) *ClusterNodesBuilder {
	b.master = value
	b.bitmap_ |= 64
	return b
}

// Total sets the value of the 'total' attribute to the given value.
//
//
func (b *ClusterNodesBuilder) Total(value int) *ClusterNodesBuilder {
	b.total = value
	b.bitmap_ |= 128
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *ClusterNodesBuilder) Copy(object *ClusterNodes) *ClusterNodesBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	if object.autoscaleCompute != nil {
		b.autoscaleCompute = NewMachinePoolAutoscaling().Copy(object.autoscaleCompute)
	} else {
		b.autoscaleCompute = nil
	}
	if object.availabilityZones != nil {
		b.availabilityZones = make([]string, len(object.availabilityZones))
		copy(b.availabilityZones, object.availabilityZones)
	} else {
		b.availabilityZones = nil
	}
	b.compute = object.compute
	if len(object.computeLabels) > 0 {
		b.computeLabels = map[string]string{}
		for k, v := range object.computeLabels {
			b.computeLabels[k] = v
		}
	} else {
		b.computeLabels = nil
	}
	if object.computeMachineType != nil {
		b.computeMachineType = NewMachineType().Copy(object.computeMachineType)
	} else {
		b.computeMachineType = nil
	}
	b.infra = object.infra
	b.master = object.master
	b.total = object.total
	return b
}

// Build creates a 'cluster_nodes' object using the configuration stored in the builder.
func (b *ClusterNodesBuilder) Build() (object *ClusterNodes, err error) {
	object = new(ClusterNodes)
	object.bitmap_ = b.bitmap_
	if b.autoscaleCompute != nil {
		object.autoscaleCompute, err = b.autoscaleCompute.Build()
		if err != nil {
			return
		}
	}
	if b.availabilityZones != nil {
		object.availabilityZones = make([]string, len(b.availabilityZones))
		copy(object.availabilityZones, b.availabilityZones)
	}
	object.compute = b.compute
	if b.computeLabels != nil {
		object.computeLabels = make(map[string]string)
		for k, v := range b.computeLabels {
			object.computeLabels[k] = v
		}
	}
	if b.computeMachineType != nil {
		object.computeMachineType, err = b.computeMachineType.Build()
		if err != nil {
			return
		}
	}
	object.infra = b.infra
	object.master = b.master
	object.total = b.total
	return
}
