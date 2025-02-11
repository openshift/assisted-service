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

// EventBuilder contains the data and logic needed to build 'event' objects.
//
// Representation of a trackable event.
type EventBuilder struct {
	bitmap_ uint32
	key     string
}

// NewEvent creates a new builder of 'event' objects.
func NewEvent() *EventBuilder {
	return &EventBuilder{}
}

// Key sets the value of the 'key' attribute to the given value.
//
//
func (b *EventBuilder) Key(value string) *EventBuilder {
	b.key = value
	b.bitmap_ |= 1
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *EventBuilder) Copy(object *Event) *EventBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.key = object.key
	return b
}

// Build creates a 'event' object using the configuration stored in the builder.
func (b *EventBuilder) Build() (object *Event, err error) {
	object = new(Event)
	object.bitmap_ = b.bitmap_
	object.key = b.key
	return
}
