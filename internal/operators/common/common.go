package common

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"text/template"

	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/conversions"
)

// Returns count for disks that are not installion disk and fulfill size requirements (eligible disks) and
// disks that are not installation disk (available disks)
func NonInstallationDiskCount(disks []*models.Disk, installationDiskID string, minSizeGB int64) (int64, int64) {
	var eligibleDisks int64
	var availableDisks int64

	for _, disk := range disks {
		if (disk.DriveType == models.DriveTypeSSD || disk.DriveType == models.DriveTypeHDD) && installationDiskID != disk.ID && disk.SizeBytes != 0 {
			if disk.SizeBytes >= conversions.GbToBytes(minSizeGB) {
				eligibleDisks++
			} else {
				availableDisks++
			}
		}
	}

	return eligibleDisks, availableDisks
}

func HasOperator(operators []*models.MonitoredOperator, operatorName string) bool {
	for _, o := range operators {
		if o.Name == operatorName {
			return true
		}
	}
	return false
}

func GenerateManifests(
	templatesRoot fs.FS,
	templates *template.Template,
	config any,
	operator *models.MonitoredOperator,

) (openshiftManifests map[string][]byte, customManifests []byte, err error) {
	openshiftManifests = map[string][]byte{}
	openshiftTemplatePaths, err := fs.Glob(templatesRoot, "openshift/*.yaml")
	if err != nil {
		return
	}
	for _, openshiftTemplatePath := range openshiftTemplatePaths {
		manifestName := path.Base(openshiftTemplatePath)
		var manifestContent []byte
		manifestContent, err = executeTemplate(
			openshiftTemplatePath, templates, config, operator,
		)
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
		manifestContent, err = executeTemplate(
			customTemplatePath, templates, config, operator,
		)
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

func executeTemplate(
	name string,
	templates *template.Template,
	config any,
	operator *models.MonitoredOperator,
) (result []byte, err error) {
	template := templates.Lookup(name)
	if template == nil {
		err = fmt.Errorf("failed to find template '%s'", name)
		return
	}

	type Data struct {
		Operator *models.MonitoredOperator
		Config   any
	}

	data := &Data{
		Operator: operator,
		Config:   config,
	}

	buffer := &bytes.Buffer{}
	err = template.Execute(buffer, data)
	if err != nil {
		return
	}
	result = buffer.Bytes()

	return
}

func GetOperator(operators []*models.MonitoredOperator, operatorName string) *models.MonitoredOperator {
	for _, o := range operators {
		if o.Name == operatorName {
			return o
		}
	}
	return nil
}

