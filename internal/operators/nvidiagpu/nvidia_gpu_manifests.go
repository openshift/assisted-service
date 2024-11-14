package nvidiagpu

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"

	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/models"
)

// GenerateManifests generates manifests for the operator.
func (o *operator) GenerateManifests(_ *common.Cluster) (openshiftManifests map[string][]byte, customManifests []byte,
	err error) {
	openshiftManifests = map[string][]byte{}
	openshiftTemplatePaths, err := fs.Glob(templatesRoot, "openshift/*.yaml")
	if err != nil {
		return
	}
	for _, openshiftTemplatePath := range openshiftTemplatePaths {
		manifestName := path.Base(openshiftTemplatePath)
		var manifestContent []byte
		manifestContent, err = o.executeTemplate(openshiftTemplatePath)
		if err != nil {
			return
		}
		openshiftManifests[manifestName] = manifestContent
	}

	customManifestsBuffer := &bytes.Buffer{}
	customTemplatePaths, err := fs.Glob(templatesRoot, "custom/*.yaml")
	if err != nil {
		return
	}
	for _, customTemplatePath := range customTemplatePaths {
		var manifestContent []byte
		manifestContent, err = o.executeTemplate(customTemplatePath)
		if err != nil {
			return
		}
		customManifestsBuffer.WriteString("---\n")
		customManifestsBuffer.Write(manifestContent)
		customManifestsBuffer.WriteString("\n")
	}
	customManifests = customManifestsBuffer.Bytes()

	return
}

func (o operator) executeTemplate(name string) (result []byte, err error) {
	template := o.templates.Lookup(name)
	if template == nil {
		err = fmt.Errorf("failed to find template '%s'", name)
		return
	}
	type Data struct {
		Operator *models.MonitoredOperator
		Config   *Config
	}
	data := &Data{
		Operator: &Operator,
		Config:   o.config,
	}
	buffer := &bytes.Buffer{}
	err = template.Execute(buffer, data)
	if err != nil {
		return
	}
	result = buffer.Bytes()
	return
}
