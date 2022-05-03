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

// AWSMachinePoolBuilder contains the data and logic needed to build 'AWS_machine_pool' objects.
//
// Representation of aws machine pool specific parameters.
type AWSMachinePoolBuilder struct {
	bitmap_           uint32
	id                string
	href              string
	spotMarketOptions *AWSSpotMarketOptionsBuilder
}

// NewAWSMachinePool creates a new builder of 'AWS_machine_pool' objects.
func NewAWSMachinePool() *AWSMachinePoolBuilder {
	return &AWSMachinePoolBuilder{}
}

// Link sets the flag that indicates if this is a link.
func (b *AWSMachinePoolBuilder) Link(value bool) *AWSMachinePoolBuilder {
	b.bitmap_ |= 1
	return b
}

// ID sets the identifier of the object.
func (b *AWSMachinePoolBuilder) ID(value string) *AWSMachinePoolBuilder {
	b.id = value
	b.bitmap_ |= 2
	return b
}

// HREF sets the link to the object.
func (b *AWSMachinePoolBuilder) HREF(value string) *AWSMachinePoolBuilder {
	b.href = value
	b.bitmap_ |= 4
	return b
}

// SpotMarketOptions sets the value of the 'spot_market_options' attribute to the given value.
//
// Spot market options for AWS machine pool.
func (b *AWSMachinePoolBuilder) SpotMarketOptions(value *AWSSpotMarketOptionsBuilder) *AWSMachinePoolBuilder {
	b.spotMarketOptions = value
	if value != nil {
		b.bitmap_ |= 8
	} else {
		b.bitmap_ &^= 8
	}
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *AWSMachinePoolBuilder) Copy(object *AWSMachinePool) *AWSMachinePoolBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.id = object.id
	b.href = object.href
	if object.spotMarketOptions != nil {
		b.spotMarketOptions = NewAWSSpotMarketOptions().Copy(object.spotMarketOptions)
	} else {
		b.spotMarketOptions = nil
	}
	return b
}

// Build creates a 'AWS_machine_pool' object using the configuration stored in the builder.
func (b *AWSMachinePoolBuilder) Build() (object *AWSMachinePool, err error) {
	object = new(AWSMachinePool)
	object.id = b.id
	object.href = b.href
	object.bitmap_ = b.bitmap_
	if b.spotMarketOptions != nil {
		object.spotMarketOptions, err = b.spotMarketOptions.Build()
		if err != nil {
			return
		}
	}
	return
}
