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

// MarshalSSHCredentials writes a value of the 'SSH_credentials' type to the given writer.
func MarshalSSHCredentials(object *SSHCredentials, writer io.Writer) error {
	stream := helpers.NewStream(writer)
	writeSSHCredentials(object, stream)
	stream.Flush()
	return stream.Error
}

// writeSSHCredentials writes a value of the 'SSH_credentials' type to the given stream.
func writeSSHCredentials(object *SSHCredentials, stream *jsoniter.Stream) {
	count := 0
	stream.WriteObjectStart()
	var present_ bool
	present_ = object.bitmap_&1 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("private_key")
		stream.WriteString(object.privateKey)
		count++
	}
	present_ = object.bitmap_&2 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("public_key")
		stream.WriteString(object.publicKey)
		count++
	}
	stream.WriteObjectEnd()
}

// UnmarshalSSHCredentials reads a value of the 'SSH_credentials' type from the given
// source, which can be an slice of bytes, a string or a reader.
func UnmarshalSSHCredentials(source interface{}) (object *SSHCredentials, err error) {
	if source == http.NoBody {
		return
	}
	iterator, err := helpers.NewIterator(source)
	if err != nil {
		return
	}
	object = readSSHCredentials(iterator)
	err = iterator.Error
	return
}

// readSSHCredentials reads a value of the 'SSH_credentials' type from the given iterator.
func readSSHCredentials(iterator *jsoniter.Iterator) *SSHCredentials {
	object := &SSHCredentials{}
	for {
		field := iterator.ReadObject()
		if field == "" {
			break
		}
		switch field {
		case "private_key":
			value := iterator.ReadString()
			object.privateKey = value
			object.bitmap_ |= 1
		case "public_key":
			value := iterator.ReadString()
			object.publicKey = value
			object.bitmap_ |= 2
		default:
			iterator.ReadAny()
		}
	}
	return object
}
