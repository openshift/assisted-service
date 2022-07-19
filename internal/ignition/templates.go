package ignition

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"text/template"
)

//go:embed templates
var templatesFS embed.FS

// loadTemplates loads the templates from the embedded 'templates' directory.
//
// In addition to the default functions the templates will also have available the
// 'executeTemplate', 'toJson' and 'toBase64' functions.
func loadTemplates() (result *template.Template, err error) {
	initial := template.New("")
	initial.Funcs(template.FuncMap{
		"executeTemplate": makeExecuteTemplateFunc(initial),
		"toBase64":        toBase64Func,
		"toJson":          toJsonFunc,
	})
	_, err = initial.ParseFS(templatesFS, "templates/*")
	if err != nil {
		return
	}
	result = initial
	return
}

// makeExecuteTemplateFunc generates a function that implements the 'executeTemplate' template
// function. Note that this is not the template function itself, but rather a function that
// generates it. The reason for that is that the 'executeTemplate' function needs a reference to the
// initial template so that it can use it to lookup the included template. That reference is not
// provided the standard template library. To use it first create the template, and then register
// the 'executeTemplate' function like this:
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
// The resulting 'executeTemplate' template funcion is equivalent to the template.ExecuteTemplate
// method, it receives as parameters the name of the template to execute and the data to pass to it.
// For example, to execute a 'my.tmpl' template passing it all the same data that was passed to the
// initial template:
//
//	{{ executeTemplate "my.tmpl" . }}
//
// The function returns a string containing the result of executing the template, so that it can
// then be passed to other functions.
func makeExecuteTemplateFunc(initial *template.Template) func(string, interface{}) (string, error) {
	return func(name string, data interface{}) (result string, err error) {
		buffer := &bytes.Buffer{}
		executed := initial.Lookup(name)
		err = executed.Execute(buffer, data)
		if err != nil {
			return
		}
		result = buffer.String()
		return
	}
}

// toJsonFunc is a template function that encodes the given data as JSON. This can be used, for
// example, to encode as a JSON string the result of executing other function. For example, to
// create a JSON document with a 'content' field that contains the text of the 'my.tmpl' template:
//
//	"content": {{ executeTemplate "my.tmpl" . | toJson }}
//
// Note how that the value of that 'content' field doesn't need to sorrounded by quotes, because the
// 'toJson' function will generate a valid JSON string, including those quotes.
func toJsonFunc(data interface{}) (result string, err error) {
	text, err := json.Marshal(data)
	if err != nil {
		return
	}
	result = string(text)
	return
}

// toBase64 is a template function that encodes the given data using Base64.
func toBase64Func(data string) string {
	return base64.StdEncoding.EncodeToString([]byte(data))
}
