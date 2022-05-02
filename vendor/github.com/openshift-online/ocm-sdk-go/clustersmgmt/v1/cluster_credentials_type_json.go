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

import (
	"io"
	"net/http"

	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-online/ocm-sdk-go/helpers"
)

// MarshalClusterCredentials writes a value of the 'cluster_credentials' type to the given writer.
func MarshalClusterCredentials(object *ClusterCredentials, writer io.Writer) error {
	stream := helpers.NewStream(writer)
	writeClusterCredentials(object, stream)
	stream.Flush()
	return stream.Error
}

// writeClusterCredentials writes a value of the 'cluster_credentials' type to the given stream.
func writeClusterCredentials(object *ClusterCredentials, stream *jsoniter.Stream) {
	count := 0
	stream.WriteObjectStart()
	stream.WriteObjectField("kind")
	if object.bitmap_&1 != 0 {
		stream.WriteString(ClusterCredentialsLinkKind)
	} else {
		stream.WriteString(ClusterCredentialsKind)
	}
	count++
	if object.bitmap_&2 != 0 {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("id")
		stream.WriteString(object.id)
		count++
	}
	if object.bitmap_&4 != 0 {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("href")
		stream.WriteString(object.href)
		count++
	}
	var present_ bool
	present_ = object.bitmap_&8 != 0 && object.ssh != nil
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("ssh")
		writeSSHCredentials(object.ssh, stream)
		count++
	}
	present_ = object.bitmap_&16 != 0 && object.admin != nil
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("admin")
		writeAdminCredentials(object.admin, stream)
		count++
	}
	present_ = object.bitmap_&32 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("kubeconfig")
		stream.WriteString(object.kubeconfig)
		count++
	}
	stream.WriteObjectEnd()
}

// UnmarshalClusterCredentials reads a value of the 'cluster_credentials' type from the given
// source, which can be an slice of bytes, a string or a reader.
func UnmarshalClusterCredentials(source interface{}) (object *ClusterCredentials, err error) {
	if source == http.NoBody {
		return
	}
	iterator, err := helpers.NewIterator(source)
	if err != nil {
		return
	}
	object = readClusterCredentials(iterator)
	err = iterator.Error
	return
}

// readClusterCredentials reads a value of the 'cluster_credentials' type from the given iterator.
func readClusterCredentials(iterator *jsoniter.Iterator) *ClusterCredentials {
	object := &ClusterCredentials{}
	for {
		field := iterator.ReadObject()
		if field == "" {
			break
		}
		switch field {
		case "kind":
			value := iterator.ReadString()
			if value == ClusterCredentialsLinkKind {
				object.bitmap_ |= 1
			}
		case "id":
			object.id = iterator.ReadString()
			object.bitmap_ |= 2
		case "href":
			object.href = iterator.ReadString()
			object.bitmap_ |= 4
		case "ssh":
			value := readSSHCredentials(iterator)
			object.ssh = value
			object.bitmap_ |= 8
		case "admin":
			value := readAdminCredentials(iterator)
			object.admin = value
			object.bitmap_ |= 16
		case "kubeconfig":
			value := iterator.ReadString()
			object.kubeconfig = value
			object.bitmap_ |= 32
		default:
			iterator.ReadAny()
		}
	}
	return object
}
