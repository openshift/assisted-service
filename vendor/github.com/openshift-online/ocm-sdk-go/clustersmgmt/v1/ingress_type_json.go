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
	"sort"

	jsoniter "github.com/json-iterator/go"
	"github.com/openshift-online/ocm-sdk-go/helpers"
)

// MarshalIngress writes a value of the 'ingress' type to the given writer.
func MarshalIngress(object *Ingress, writer io.Writer) error {
	stream := helpers.NewStream(writer)
	writeIngress(object, stream)
	stream.Flush()
	return stream.Error
}

// writeIngress writes a value of the 'ingress' type to the given stream.
func writeIngress(object *Ingress, stream *jsoniter.Stream) {
	count := 0
	stream.WriteObjectStart()
	stream.WriteObjectField("kind")
	if object.bitmap_&1 != 0 {
		stream.WriteString(IngressLinkKind)
	} else {
		stream.WriteString(IngressKind)
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
	present_ = object.bitmap_&8 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("dns_name")
		stream.WriteString(object.dnsName)
		count++
	}
	present_ = object.bitmap_&16 != 0 && object.cluster != nil
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("cluster")
		writeCluster(object.cluster, stream)
		count++
	}
	present_ = object.bitmap_&32 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("default")
		stream.WriteBool(object.default_)
		count++
	}
	present_ = object.bitmap_&64 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("listening")
		stream.WriteString(string(object.listening))
		count++
	}
	present_ = object.bitmap_&128 != 0 && object.routeSelectors != nil
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("route_selectors")
		if object.routeSelectors != nil {
			stream.WriteObjectStart()
			keys := make([]string, len(object.routeSelectors))
			i := 0
			for key := range object.routeSelectors {
				keys[i] = key
				i++
			}
			sort.Strings(keys)
			for i, key := range keys {
				if i > 0 {
					stream.WriteMore()
				}
				item := object.routeSelectors[key]
				stream.WriteObjectField(key)
				stream.WriteString(item)
			}
			stream.WriteObjectEnd()
		} else {
			stream.WriteNil()
		}
		count++
	}
	stream.WriteObjectEnd()
}

// UnmarshalIngress reads a value of the 'ingress' type from the given
// source, which can be an slice of bytes, a string or a reader.
func UnmarshalIngress(source interface{}) (object *Ingress, err error) {
	if source == http.NoBody {
		return
	}
	iterator, err := helpers.NewIterator(source)
	if err != nil {
		return
	}
	object = readIngress(iterator)
	err = iterator.Error
	return
}

// readIngress reads a value of the 'ingress' type from the given iterator.
func readIngress(iterator *jsoniter.Iterator) *Ingress {
	object := &Ingress{}
	for {
		field := iterator.ReadObject()
		if field == "" {
			break
		}
		switch field {
		case "kind":
			value := iterator.ReadString()
			if value == IngressLinkKind {
				object.bitmap_ |= 1
			}
		case "id":
			object.id = iterator.ReadString()
			object.bitmap_ |= 2
		case "href":
			object.href = iterator.ReadString()
			object.bitmap_ |= 4
		case "dns_name":
			value := iterator.ReadString()
			object.dnsName = value
			object.bitmap_ |= 8
		case "cluster":
			value := readCluster(iterator)
			object.cluster = value
			object.bitmap_ |= 16
		case "default":
			value := iterator.ReadBool()
			object.default_ = value
			object.bitmap_ |= 32
		case "listening":
			text := iterator.ReadString()
			value := ListeningMethod(text)
			object.listening = value
			object.bitmap_ |= 64
		case "route_selectors":
			value := map[string]string{}
			for {
				key := iterator.ReadObject()
				if key == "" {
					break
				}
				item := iterator.ReadString()
				value[key] = item
			}
			object.routeSelectors = value
			object.bitmap_ |= 128
		default:
			iterator.ReadAny()
		}
	}
	return object
}
