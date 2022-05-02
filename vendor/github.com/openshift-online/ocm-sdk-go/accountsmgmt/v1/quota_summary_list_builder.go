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

// QuotaSummaryListBuilder contains the data and logic needed to build
// 'quota_summary' objects.
type QuotaSummaryListBuilder struct {
	items []*QuotaSummaryBuilder
}

// NewQuotaSummaryList creates a new builder of 'quota_summary' objects.
func NewQuotaSummaryList() *QuotaSummaryListBuilder {
	return new(QuotaSummaryListBuilder)
}

// Items sets the items of the list.
func (b *QuotaSummaryListBuilder) Items(values ...*QuotaSummaryBuilder) *QuotaSummaryListBuilder {
	b.items = make([]*QuotaSummaryBuilder, len(values))
	copy(b.items, values)
	return b
}

// Copy copies the items of the given list into this builder, discarding any previous items.
func (b *QuotaSummaryListBuilder) Copy(list *QuotaSummaryList) *QuotaSummaryListBuilder {
	if list == nil || list.items == nil {
		b.items = nil
	} else {
		b.items = make([]*QuotaSummaryBuilder, len(list.items))
		for i, v := range list.items {
			b.items[i] = NewQuotaSummary().Copy(v)
		}
	}
	return b
}

// Build creates a list of 'quota_summary' objects using the
// configuration stored in the builder.
func (b *QuotaSummaryListBuilder) Build() (list *QuotaSummaryList, err error) {
	items := make([]*QuotaSummary, len(b.items))
	for i, item := range b.items {
		items[i], err = item.Build()
		if err != nil {
			return
		}
	}
	list = new(QuotaSummaryList)
	list.items = items
	return
}
