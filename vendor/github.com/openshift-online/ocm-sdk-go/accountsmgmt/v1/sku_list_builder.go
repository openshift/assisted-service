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

package v1 // github.com/openshift-online/ocm-sdk-go/accountsmgmt/v1

// SKUListBuilder contains the data and logic needed to build
// 'SKU' objects.
type SKUListBuilder struct {
	items []*SKUBuilder
}

// NewSKUList creates a new builder of 'SKU' objects.
func NewSKUList() *SKUListBuilder {
	return new(SKUListBuilder)
}

// Items sets the items of the list.
func (b *SKUListBuilder) Items(values ...*SKUBuilder) *SKUListBuilder {
	b.items = make([]*SKUBuilder, len(values))
	copy(b.items, values)
	return b
}

// Copy copies the items of the given list into this builder, discarding any previous items.
func (b *SKUListBuilder) Copy(list *SKUList) *SKUListBuilder {
	if list == nil || list.items == nil {
		b.items = nil
	} else {
		b.items = make([]*SKUBuilder, len(list.items))
		for i, v := range list.items {
			b.items[i] = NewSKU().Copy(v)
		}
	}
	return b
}

// Build creates a list of 'SKU' objects using the
// configuration stored in the builder.
func (b *SKUListBuilder) Build() (list *SKUList, err error) {
	items := make([]*SKU, len(b.items))
	for i, item := range b.items {
		items[i], err = item.Build()
		if err != nil {
			return
		}
	}
	list = new(SKUList)
	list.items = items
	return
}
