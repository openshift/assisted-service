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

package v1 // github.com/openshift-online/ocm-sdk-go/servicelogs/v1

import (
	time "time"
)

// LogEntryBuilder contains the data and logic needed to build 'log_entry' objects.
//
//
type LogEntryBuilder struct {
	bitmap_      uint32
	id           string
	href         string
	clusterUUID  string
	description  string
	serviceName  string
	severity     Severity
	summary      string
	timestamp    time.Time
	username     string
	internalOnly bool
}

// NewLogEntry creates a new builder of 'log_entry' objects.
func NewLogEntry() *LogEntryBuilder {
	return &LogEntryBuilder{}
}

// Link sets the flag that indicates if this is a link.
func (b *LogEntryBuilder) Link(value bool) *LogEntryBuilder {
	b.bitmap_ |= 1
	return b
}

// ID sets the identifier of the object.
func (b *LogEntryBuilder) ID(value string) *LogEntryBuilder {
	b.id = value
	b.bitmap_ |= 2
	return b
}

// HREF sets the link to the object.
func (b *LogEntryBuilder) HREF(value string) *LogEntryBuilder {
	b.href = value
	b.bitmap_ |= 4
	return b
}

// ClusterUUID sets the value of the 'cluster_UUID' attribute to the given value.
//
//
func (b *LogEntryBuilder) ClusterUUID(value string) *LogEntryBuilder {
	b.clusterUUID = value
	b.bitmap_ |= 8
	return b
}

// Description sets the value of the 'description' attribute to the given value.
//
//
func (b *LogEntryBuilder) Description(value string) *LogEntryBuilder {
	b.description = value
	b.bitmap_ |= 16
	return b
}

// InternalOnly sets the value of the 'internal_only' attribute to the given value.
//
//
func (b *LogEntryBuilder) InternalOnly(value bool) *LogEntryBuilder {
	b.internalOnly = value
	b.bitmap_ |= 32
	return b
}

// ServiceName sets the value of the 'service_name' attribute to the given value.
//
//
func (b *LogEntryBuilder) ServiceName(value string) *LogEntryBuilder {
	b.serviceName = value
	b.bitmap_ |= 64
	return b
}

// Severity sets the value of the 'severity' attribute to the given value.
//
//
func (b *LogEntryBuilder) Severity(value Severity) *LogEntryBuilder {
	b.severity = value
	b.bitmap_ |= 128
	return b
}

// Summary sets the value of the 'summary' attribute to the given value.
//
//
func (b *LogEntryBuilder) Summary(value string) *LogEntryBuilder {
	b.summary = value
	b.bitmap_ |= 256
	return b
}

// Timestamp sets the value of the 'timestamp' attribute to the given value.
//
//
func (b *LogEntryBuilder) Timestamp(value time.Time) *LogEntryBuilder {
	b.timestamp = value
	b.bitmap_ |= 512
	return b
}

// Username sets the value of the 'username' attribute to the given value.
//
//
func (b *LogEntryBuilder) Username(value string) *LogEntryBuilder {
	b.username = value
	b.bitmap_ |= 1024
	return b
}

// Copy copies the attributes of the given object into this builder, discarding any previous values.
func (b *LogEntryBuilder) Copy(object *LogEntry) *LogEntryBuilder {
	if object == nil {
		return b
	}
	b.bitmap_ = object.bitmap_
	b.id = object.id
	b.href = object.href
	b.clusterUUID = object.clusterUUID
	b.description = object.description
	b.internalOnly = object.internalOnly
	b.serviceName = object.serviceName
	b.severity = object.severity
	b.summary = object.summary
	b.timestamp = object.timestamp
	b.username = object.username
	return b
}

// Build creates a 'log_entry' object using the configuration stored in the builder.
func (b *LogEntryBuilder) Build() (object *LogEntry, err error) {
	object = new(LogEntry)
	object.id = b.id
	object.href = b.href
	object.bitmap_ = b.bitmap_
	object.clusterUUID = b.clusterUUID
	object.description = b.description
	object.internalOnly = b.internalOnly
	object.serviceName = b.serviceName
	object.severity = b.severity
	object.summary = b.summary
	object.timestamp = b.timestamp
	object.username = b.username
	return
}
