package secretdump

import (
	"fmt"
	"reflect"
	"strings"
)

// DumpSecretStruct generates a string representation of a struct with
// `secret:"true"` tagged fields removed.
// Does not recurse into pointers to structs (or show pointer values in general),
// only regular nested structs.
// Pointer addresses are also redacted in order to not leak addresses
func DumpSecretStruct(obj interface{}) string {
	var sb strings.Builder
	dumpSecretStructInternal(obj, &sb, 1)
	return sb.String()
}

func dumpSecretStructInternal(obj interface{}, sb *strings.Builder, depth int) {
	v := reflect.ValueOf(obj)

	sb.WriteString(fmt.Sprintf("struct %s {\n", v.Type().Name()))

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)

		name, tag := v.Type().Field(i).Name,
			reflect.TypeOf(obj).Field(i).Tag

		for j := 0; j < depth; j++ {
			sb.WriteString("\t")
		}

		sb.WriteString(fmt.Sprintf("%s: ", name))

		if tag.Get("secret") == "true" {
			sb.WriteString("<SECRET>")
		} else {
			if field.CanInterface() {
				value := field.Interface()
				if field.Kind() == reflect.Struct {
					dumpSecretStructInternal(value, sb, depth+1)
				} else if field.Kind() == reflect.Ptr {
					sb.WriteString(fmt.Sprintf("<%T>", value))
				} else {
					sb.WriteString(fmt.Sprintf("%#v", value))
				}
			} else {
				sb.WriteString("<PRIVATE>")
			}
		}

		sb.WriteString(",\n")
	}

	for j := 0; j < depth-1; j++ {
		sb.WriteString("\t")
	}

	sb.WriteString("}")
}
