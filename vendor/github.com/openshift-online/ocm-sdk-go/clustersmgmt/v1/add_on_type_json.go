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

// MarshalAddOn writes a value of the 'add_on' type to the given writer.
func MarshalAddOn(object *AddOn, writer io.Writer) error {
	stream := helpers.NewStream(writer)
	writeAddOn(object, stream)
	stream.Flush()
	return stream.Error
}

// writeAddOn writes a value of the 'add_on' type to the given stream.
func writeAddOn(object *AddOn, stream *jsoniter.Stream) {
	count := 0
	stream.WriteObjectStart()
	stream.WriteObjectField("kind")
	if object.bitmap_&1 != 0 {
		stream.WriteString(AddOnLinkKind)
	} else {
		stream.WriteString(AddOnKind)
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
		stream.WriteObjectField("description")
		stream.WriteString(object.description)
		count++
	}
	present_ = object.bitmap_&16 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("docs_link")
		stream.WriteString(object.docsLink)
		count++
	}
	present_ = object.bitmap_&32 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("enabled")
		stream.WriteBool(object.enabled)
		count++
	}
	present_ = object.bitmap_&64 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("has_external_resources")
		stream.WriteBool(object.hasExternalResources)
		count++
	}
	present_ = object.bitmap_&128 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("hidden")
		stream.WriteBool(object.hidden)
		count++
	}
	present_ = object.bitmap_&256 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("icon")
		stream.WriteString(object.icon)
		count++
	}
	present_ = object.bitmap_&512 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("install_mode")
		stream.WriteString(string(object.installMode))
		count++
	}
	present_ = object.bitmap_&1024 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("label")
		stream.WriteString(object.label)
		count++
	}
	present_ = object.bitmap_&2048 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("name")
		stream.WriteString(object.name)
		count++
	}
	present_ = object.bitmap_&4096 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("operator_name")
		stream.WriteString(object.operatorName)
		count++
	}
	present_ = object.bitmap_&8192 != 0 && object.parameters != nil
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("parameters")
		stream.WriteObjectStart()
		stream.WriteObjectField("items")
		writeAddOnParameterList(object.parameters.items, stream)
		stream.WriteObjectEnd()
		count++
	}
	present_ = object.bitmap_&16384 != 0 && object.requirements != nil
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("requirements")
		writeAddOnRequirementList(object.requirements, stream)
		count++
	}
	present_ = object.bitmap_&32768 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("resource_cost")
		stream.WriteFloat64(object.resourceCost)
		count++
	}
	present_ = object.bitmap_&65536 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("resource_name")
		stream.WriteString(object.resourceName)
		count++
	}
	present_ = object.bitmap_&131072 != 0 && object.subOperators != nil
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("sub_operators")
		writeAddOnSubOperatorList(object.subOperators, stream)
		count++
	}
	present_ = object.bitmap_&262144 != 0
	if present_ {
		if count > 0 {
			stream.WriteMore()
		}
		stream.WriteObjectField("target_namespace")
		stream.WriteString(object.targetNamespace)
		count++
	}
	stream.WriteObjectEnd()
}

// UnmarshalAddOn reads a value of the 'add_on' type from the given
// source, which can be an slice of bytes, a string or a reader.
func UnmarshalAddOn(source interface{}) (object *AddOn, err error) {
	if source == http.NoBody {
		return
	}
	iterator, err := helpers.NewIterator(source)
	if err != nil {
		return
	}
	object = readAddOn(iterator)
	err = iterator.Error
	return
}

// readAddOn reads a value of the 'add_on' type from the given iterator.
func readAddOn(iterator *jsoniter.Iterator) *AddOn {
	object := &AddOn{}
	for {
		field := iterator.ReadObject()
		if field == "" {
			break
		}
		switch field {
		case "kind":
			value := iterator.ReadString()
			if value == AddOnLinkKind {
				object.bitmap_ |= 1
			}
		case "id":
			object.id = iterator.ReadString()
			object.bitmap_ |= 2
		case "href":
			object.href = iterator.ReadString()
			object.bitmap_ |= 4
		case "description":
			value := iterator.ReadString()
			object.description = value
			object.bitmap_ |= 8
		case "docs_link":
			value := iterator.ReadString()
			object.docsLink = value
			object.bitmap_ |= 16
		case "enabled":
			value := iterator.ReadBool()
			object.enabled = value
			object.bitmap_ |= 32
		case "has_external_resources":
			value := iterator.ReadBool()
			object.hasExternalResources = value
			object.bitmap_ |= 64
		case "hidden":
			value := iterator.ReadBool()
			object.hidden = value
			object.bitmap_ |= 128
		case "icon":
			value := iterator.ReadString()
			object.icon = value
			object.bitmap_ |= 256
		case "install_mode":
			text := iterator.ReadString()
			value := AddOnInstallMode(text)
			object.installMode = value
			object.bitmap_ |= 512
		case "label":
			value := iterator.ReadString()
			object.label = value
			object.bitmap_ |= 1024
		case "name":
			value := iterator.ReadString()
			object.name = value
			object.bitmap_ |= 2048
		case "operator_name":
			value := iterator.ReadString()
			object.operatorName = value
			object.bitmap_ |= 4096
		case "parameters":
			value := &AddOnParameterList{}
			for {
				field := iterator.ReadObject()
				if field == "" {
					break
				}
				switch field {
				case "kind":
					text := iterator.ReadString()
					value.link = text == AddOnParameterListLinkKind
				case "href":
					value.href = iterator.ReadString()
				case "items":
					value.items = readAddOnParameterList(iterator)
				default:
					iterator.ReadAny()
				}
			}
			object.parameters = value
			object.bitmap_ |= 8192
		case "requirements":
			value := readAddOnRequirementList(iterator)
			object.requirements = value
			object.bitmap_ |= 16384
		case "resource_cost":
			value := iterator.ReadFloat64()
			object.resourceCost = value
			object.bitmap_ |= 32768
		case "resource_name":
			value := iterator.ReadString()
			object.resourceName = value
			object.bitmap_ |= 65536
		case "sub_operators":
			value := readAddOnSubOperatorList(iterator)
			object.subOperators = value
			object.bitmap_ |= 131072
		case "target_namespace":
			value := iterator.ReadString()
			object.targetNamespace = value
			object.bitmap_ |= 262144
		default:
			iterator.ReadAny()
		}
	}
	return object
}
