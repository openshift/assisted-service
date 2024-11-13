package templating

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"text/template"
)

// LoadTemplates loads the templates from the given file system.
//
// In addition to the default functions the templates will also have available the 'executeTemplate', 'toString',
// 'toJson' and 'toBase64' functions.
func LoadTemplates(fsys fs.FS) (result *template.Template, err error) {
	initial := template.New("")
	initial.Funcs(template.FuncMap{
		"executeTemplate": makeExecuteTemplateFunc(initial),
		"toBase64":        toBase64Func,
		"toJson":          toJsonFunc,
		"toString":        toStringFunc,
	})
	err = fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			content, err := fs.ReadFile(fsys, path)
			if err != nil {
				return err
			}
			_, err = initial.New(path).Parse(string(content))
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return
	}
	result = initial
	return
}

// makeExecuteTemplateFunc generates a function that implements the 'executeTemplate' template function. Note that this
// is not the template function itself, but rather a function that generates it. The reason for that is that the
// 'executeTemplate' function needs a reference to the initial template so that it can use it to lookup the included
// template. That reference is not provided by the standard template library. To use it first create the template, and
// then register the 'executeTemplate' function like this:
//
//	tmpl := template.New(...)
//	tmpl.Funcs(template.FuncMap{
//		"executeTemplate": makeExecuteTemplateFunc(tmpl),
//		...
//	})
//	tmpl.ParseFS(...)
//
// Note how the initial template needs to be passed as a parameter.
//
// The resulting 'executeTemplate' template funcion is equivalent to the template.ExecuteTemplate method, it receives as
// parameters the name of the template to execute and the data to pass to it. For example, to execute a 'my.tmpl'
// template passing it all the same data that was passed to the initial template:
//
//	{{ executeTemplate "my.tmpl" . }}
//
// The function returns an array of bytes containing the result of executing the template, so that it can then be passed
// to other functions.
func makeExecuteTemplateFunc(initial *template.Template) func(string, interface{}) ([]byte, error) {
	return func(name string, data interface{}) (result []byte, err error) {
		buffer := &bytes.Buffer{}
		executed := initial.Lookup(name)
		if executed == nil {
			err = fmt.Errorf("failed to find template '%s'", name)
			return
		}
		err = executed.Execute(buffer, data)
		if err != nil {
			return
		}
		result = buffer.Bytes()
		return
	}
}

// toString is a template function that converts the given data to a string. If the data is already a string it returns
// it without change. If it is an array of bytes it converts it to a string using the UTF-8 encoding. If the data
// implements the fmt.Stringer interface it converts it to string calling the String method. For any other kind of input
// it returns an error.
func toStringFunc(data interface{}) (result string, err error) {
	switch typed := data.(type) {
	case string:
		result = typed
	case []byte:
		result = string(typed)
	case fmt.Stringer:
		result = typed.String()
	default:
		err = fmt.Errorf(
			"expected a type that can be converted to string, but found %T",
			data,
		)
	}
	return
}

// toJsonFunc is a template function that encodes the given data as JSON. This can be used, for example, to encode as a
// JSON string the result of executing other function. For example, to create a JSON document with a 'content' field
// that contains the text of the 'my.tmpl' template:
//
//	"content": {{ executeTemplate "my.tmpl" . | toString | toJson }}
//
// Note how that the value of that 'content' field doesn't need to sorrounded by quotes, because the 'toJson' function
// will generate a valid JSON string, including those quotes.
func toJsonFunc(data interface{}) (result string, err error) {
	text, err := json.Marshal(data)
	if err != nil {
		return
	}
	result = string(text)
	return
}

// toBase64 is a template function that encodes the given data using Base64 and returns the result as a string. If the
// data is an array of bytes it will be encoded directly. If the data is a string it will be converted to an array of
// bytes using the UTF-8 encoding. If the data implements the fmt.Stringer interface it will be converted to a string
// using the String method, and then to an array of bytes using the UTF-8 encoding. Any other kind of data will result
// in an error.
func toBase64Func(data interface{}) (result string, err error) {
	var bytes []byte
	switch typed := data.(type) {
	case string:
		bytes = []byte(typed)
	case []byte:
		bytes = typed
	case fmt.Stringer:
		bytes = []byte(typed.String())
	default:
		err = fmt.Errorf(
			"expected a type that can be converted to an array of bytes, but found %T",
			data,
		)
		return
	}
	result = base64.StdEncoding.EncodeToString(bytes)
	return
}
