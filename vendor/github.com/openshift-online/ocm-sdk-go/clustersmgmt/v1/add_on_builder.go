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

// AddOnBuilder contains the data and logic needed to build 'add_on' objects.
//
// Representation of an add-on that can be installed in a cluster.
type AddOnBuilder struct {
	bitmap_              uint32
	id                   string
	href                 string
	description          string
	docsLink             string
	icon                 string
	installMode          AddOnInstallMode
	label                string
	name                 string
	operatorName         string
	parameters           *AddOnParameterListBuilder
	requirements         []*AddOnRequirementBuilder
	resourceCost         float64
	resourceName         string
	subOperators         []*AddOnSubOperatorBuilder
	targetNamespace      string
	enabled              bool
	hasExternalResources bool
	hidden               bool
}

// NewAddOn creates a new builder of 'add_on' objects.
func NewAddOn() *AddOnBuilder {
	return &AddOnBuilder{}
}

// Link sets the flag that indicates if this is a link.
func (b *AddOnBuilder) Link(value bool) *AddOnBuilder {
	b.bitmap_ |= 1
	return b
}

// ID sets the identifier of the object.
func (b *AddOnBuilder) ID(value string) *AddOnBuilder {
	b.id = value
	b.bitmap_ |= 2
	return b
}

// HREF sets the link to the object.
func (b *AddOnBuilder) HREF(value string) *AddOnBuilder {
	b.href = value
	b.bitmap_ |= 4
	return b
}

// Description sets the value of the 'description' attribute to the given value.
//
//
func (b *AddOnBuilder) Description(value string) *AddOnBuilder {
	b.description = value
	b.bitmap_ |= 8
	return b
}

// DocsLink sets the value of the 'docs_link' attribute to the given value.
//
//
func (b *AddOnBuilder) DocsLink(value string) *AddOnBuilder {
	b.docsLink = value
	b.bitmap_ |= 16
	return b
}

// Enabled sets the value of the 'enabled' attribute to the given value.
//
//
func (b *AddOnBuilder) Enabled(value bool) *AddOnBuilder {
	b.enabled = value
	b.bitmap_ |= 32
	return b
}

// HasExternalResources sets the value of the 'has_external_resources' attribute to the given value.
//
//
func (b *AddOnBuilder) HasExternalResources(value bool) *AddOnBuilder {
	b.hasExternalResources = value
	b.bitmap_ |= 64
	return b
}

// Hidden sets the value of the 'hidden' attribute to the given value.
//
//
func (b *AddOnBuilder) Hidden(value bool) *AddOnBuilder {
	b.hidden = value
	b.bitmap_ |= 128
	return b
}

// Icon sets the value of the 'icon' attribute to the given value.
//
//
func (b *AddOnBuilder) Icon(value string) *AddOnBuilder {
	b.icon = value
	b.bitmap_ |= 256
	return b
}

// InstallMode sets the value of the 'install_mode' attribute to the given value.
//
// Representation of an add-on InstallMode field.
func (b *AddOnBuilder) InstallMode(value AddOnInstallMode) *AddOnBuilder {
	b.installMode = value
	b.bitmap_ |= 512
	return b
}

// Label sets the value of the 'label' attribute to the given value.
//
//
func (b *AddOnBuilder) Label(value string) *AddOnBuilder {
	b.label = value
	b.bitmap_ |= 1024
	return b
}

// Name sets the value of the 'name' attribute to the given value.
//
//
func (b *AddOnBuilder) Name(value string) *AddOnBuilder {
	b.name = value
	b.bitmap_ |= 2048
	return b
}

// OperatorName sets the value of the 'operator_name' attribute to the given value.
//
//
func (b *AddOnBuilder) OperatorName(value string) *AddOnBuilder {
	b.operatorName = value
	b.bitmap_ |= 4096
	return b
}

// Parameters sets the value of the 'parameters' attribute to the given values.
//
//
func (b *AddOnBuilder) Parameters(value *AddOnParameterListBuilder) *AddOnBuilder {
	b.parameters = value
	b.bitmap_ |= 8192
	return b
}

// Requirements sets the value of the 'requirements' attribute to the given values.
//
//
func (b *AddOnBuilder) Requirements(values ...*AddOnRequirementBuilder) *AddOnBuilder {
	b.requirements = make([]*AddOnRequirementBuilder, len(values))
	copy(b.requirements, values)
	b.bitmap_ |= 16384
	return b
}

// ResourceCost sets the value of the 'resource_cost' attribute to the given value.
//
//
func (b *AddOnBuilder) ResourceCost(value float64) *AddOnBuilder {
	b.resourceCost = value
	b.bitmap_ |= 32768
	return b
}

// ResourceName sets the value of the 'resource_name' attribute to the given value.
//
//
func (b *AddOnBuilder) ResourceName(value string) *AddOnBuilder {
	b.resourceName = value
	b.bitmap_ |= 65536
	return b
}

// SubOperators sets the value of the 'sub_operators' attribute to the given values.
//
//
func (b *AddOnBuilder) SubOperators(values ...*AddOnSubOperatorBuilder) *AddOnBuilder {
	b.subOperators = make([]*AddOnSubOperatorBuilder, len(values))
	copy(b.subOperators, values)
	b.bitmap_ |= 131072
	return b
}

// TargetNamespace sets the value of the 'target_namespace' attribute to the given value.
//
//
func (b *AddOnBuilder) TargetNamespace(value string) *AddOnBuilder {
	b.targetNamespace = value
	b.bitmap_ |= 262144
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *AddOnBuilder) Copy(object *AddOn) *AddOnBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.id = object.id
	b.href = object.href
	b.description = object.description
	b.docsLink = object.docsLink
	b.enabled = object.enabled
	b.hasExternalResources = object.hasExternalResources
	b.hidden = object.hidden
	b.icon = object.icon
	b.installMode = object.installMode
	b.label = object.label
	b.name = object.name
	b.operatorName = object.operatorName
	if object.parameters != nil {
		b.parameters = NewAddOnParameterList().Copy(object.parameters)
	} else {
		b.parameters = nil
	}
	if object.requirements != nil {
		b.requirements = make([]*AddOnRequirementBuilder, len(object.requirements))
		for i, v := range object.requirements {
			b.requirements[i] = NewAddOnRequirement().Copy(v)
		}
	} else {
		b.requirements = nil
	}
	b.resourceCost = object.resourceCost
	b.resourceName = object.resourceName
	if object.subOperators != nil {
		b.subOperators = make([]*AddOnSubOperatorBuilder, len(object.subOperators))
		for i, v := range object.subOperators {
			b.subOperators[i] = NewAddOnSubOperator().Copy(v)
		}
	} else {
		b.subOperators = nil
	}
	b.targetNamespace = object.targetNamespace
	return b
}

// Build creates a 'add_on' object using the configuration stored in the builder.
func (b *AddOnBuilder) Build() (object *AddOn, err error) {
	object = new(AddOn)
	object.id = b.id
	object.href = b.href
	object.bitmap_ = b.bitmap_
	object.description = b.description
	object.docsLink = b.docsLink
	object.enabled = b.enabled
	object.hasExternalResources = b.hasExternalResources
	object.hidden = b.hidden
	object.icon = b.icon
	object.installMode = b.installMode
	object.label = b.label
	object.name = b.name
	object.operatorName = b.operatorName
	if b.parameters != nil {
		object.parameters, err = b.parameters.Build()
		if err != nil {
			return
		}
	}
	if b.requirements != nil {
		object.requirements = make([]*AddOnRequirement, len(b.requirements))
		for i, v := range b.requirements {
			object.requirements[i], err = v.Build()
			if err != nil {
				return
			}
		}
	}
	object.resourceCost = b.resourceCost
	object.resourceName = b.resourceName
	if b.subOperators != nil {
		object.subOperators = make([]*AddOnSubOperator, len(b.subOperators))
		for i, v := range b.subOperators {
			object.subOperators[i], err = v.Build()
			if err != nil {
				return
			}
		}
	}
	object.targetNamespace = b.targetNamespace
	return
}
