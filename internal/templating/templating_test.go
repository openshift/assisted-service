package templating

import (
	"bytes"
	"os"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

var _ = Describe("Templating", func() {
	Context("Template function 'executeTemplate'", func() {
		It("Executes the target template", func() {
			// Create the file system:
			tmp, fsys := Tmp(
				"caller.txt", `{{ executeTemplate "called.txt" . | toString }}`,
				"called.txt", `mytext`,
			)
			defer os.RemoveAll(tmp)

			// Load the templates:
			templates, err := LoadTemplates(fsys)
			Expect(err).ToNot(HaveOccurred())

			// Execute the template:
			template := templates.Lookup("caller.txt")
			Expect(template).ToNot(BeNil())
			buffer := &bytes.Buffer{}
			err = template.Execute(buffer, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(buffer.String()).To(Equal("mytext"))
		})

		It("Executes multiple chained templates", func() {
			// Create the file system:
			tmp, fsys := Tmp(
				"first.txt", `{{ executeTemplate "second.txt" . | toString }}`,
				"second.txt", `{{ executeTemplate "third.txt" . | toString }}`,
				"third.txt", `mytext`,
			)
			defer os.RemoveAll(tmp)

			// Load the templates:
			templates, err := LoadTemplates(fsys)
			Expect(err).ToNot(HaveOccurred())

			// Execute the template:
			template := templates.Lookup("first.txt")
			Expect(template).ToNot(BeNil())
			buffer := &bytes.Buffer{}
			err = template.Execute(buffer, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(buffer.String()).To(Equal("mytext"))
		})

		It("Accepts input", func() {
			// Create the file system:
			tmp, fsys := Tmp(
				"caller.txt", `{{ executeTemplate "called.txt" 42 | toString }}`,
				"called.txt", `{{ . }}`,
			)
			defer os.RemoveAll(tmp)

			// Load the templates:
			templates, err := LoadTemplates(fsys)
			Expect(err).ToNot(HaveOccurred())

			// Execute the template:
			template := templates.Lookup("caller.txt")
			Expect(template).ToNot(BeNil())
			buffer := &bytes.Buffer{}
			err = template.Execute(buffer, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(buffer.String()).To(Equal("42"))
		})

		It("Fails if executed template doesn't exist", func() {
			// Create the file system. Note the typo in the name of the called template.
			tmp, fsys := Tmp(
				"caller.txt", `{{ executeTemplate "caled.txt" 42 | toString }}`,
				"called.txt", `{{ . }}`,
			)
			defer os.RemoveAll(tmp)

			// Load the templats:
			templates, err := LoadTemplates(fsys)
			Expect(err).ToNot(HaveOccurred())

			// Execute the template:
			template := templates.Lookup("caller.txt")
			Expect(template).ToNot(BeNil())
			buffer := &bytes.Buffer{}
			err = template.Execute(buffer, nil)
			Expect(err).To(HaveOccurred())
			msg := err.Error()
			Expect(msg).To(ContainSubstring("find"))
			Expect(msg).To(ContainSubstring("caled.txt"))
		})
	})

	Context("Template function 'toBase64'", func() {
		It("Encodes text", func() {
			// Create the file system:
			tmp, fsys := Tmp(
				"myfile.txt", `{{ "mytext" | toBase64 }}`,
			)
			defer os.RemoveAll(tmp)

			// Load the templats:
			templates, err := LoadTemplates(fsys)
			Expect(err).ToNot(HaveOccurred())

			// Execute the template:
			template := templates.Lookup("myfile.txt")
			Expect(template).ToNot(BeNil())
			buffer := &bytes.Buffer{}
			err = template.Execute(buffer, nil)
			Expect(err).ToNot(HaveOccurred())
			Expect(buffer.String()).To(Equal("bXl0ZXh0"))
		})
	})

	DescribeTable(
		"Template function 'toJson'",
		func(input any, expected string) {
			// Create the file system:
			tmp, fsys := Tmp(
				"myfile.txt", `{{ . | toJson }}`,
			)
			defer os.RemoveAll(tmp)

			// Load the templats:
			templates, err := LoadTemplates(fsys)
			Expect(err).ToNot(HaveOccurred())

			// Execute the template:
			template := templates.Lookup("myfile.txt")
			Expect(template).ToNot(BeNil())
			buffer := &bytes.Buffer{}
			err = template.Execute(buffer, input)
			Expect(err).ToNot(HaveOccurred())
			Expect(buffer.String()).To(Equal(expected))
		},
		Entry(
			"String that doesn't need quotes",
			`mytext`,
			`"mytext"`,
		),
		Entry(
			"String that needs quotes",
			`my"text"`,
			`"my\"text\""`,
		),
		Entry(
			"Integer",
			42,
			`42`,
		),
		Entry(
			"Boolean",
			true,
			`true`,
		),
		Entry(
			"Struct without tags",
			struct {
				X int
				Y int
			}{
				X: 42,
				Y: 24,
			},
			`{"X":42,"Y":24}`,
		),
		Entry(
			"Struct with tags",
			struct {
				X int `json:"my_x"`
				Y int `json:"my_y"`
			}{
				X: 42,
				Y: 24,
			},
			`{"my_x":42,"my_y":24}`,
		),
		Entry(
			"Slice",
			[]int{42, 24},
			`[42,24]`,
		),
		Entry(
			"Map",
			map[string]int{"x": 42, "y": 24},
			`{"x":42,"y":24}`,
		),
	)
})
