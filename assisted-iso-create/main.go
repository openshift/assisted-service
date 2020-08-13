package main

import (
	"bytes"
	"context"
	"io/ioutil"
	"os/exec"
	"path/filepath"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
)

const (
	ignitionFileName       string = "ignition.config"
	coreosInstallerCommand string = "coreos-installer"
)

var Options struct {
	WorkDir        string `envconfig:"WORK_DIR"`
	IgnitionConfig string `envconfig:"IGNITION_CONFIG"`
	ImageName      string `envconfig:"IMAGE_NAME"`
	BaseISOFile    string `envconfig:"COREOS_IMAGE"`
	UseS3          bool   `envconfig:"USE_S3" default:"true"`
	S3Config       s3wrapper.Config
}

func setIgnitionConfigToFile(workDir, ignitionConfig string, log *logrus.Logger) (string, error) {
	fullFileName := filepath.Join(workDir, ignitionFileName)
	err := ioutil.WriteFile(fullFileName, []byte(ignitionConfig), 0600)
	if err != nil {
		log.Errorf("failed to write ignition into file %s", fullFileName)
		return "", err
	}
	return fullFileName, nil
}

func embedIgnitionIntoISO(workDir, ignitionFile, imageName, baseISOFile string, log *logrus.Logger) (string, error) {
	var out bytes.Buffer
	resultFile := filepath.Join(workDir, imageName)
	installerCommand := filepath.Join(workDir, coreosInstallerCommand)
	cmd := exec.Command(installerCommand, "iso", "embed", "-c", ignitionFile, "-o", resultFile, baseISOFile, "-f")
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		log.Errorf("coreos-installer failed: %s", out.String())
		return "", err
	}
	return resultFile, nil
}

func uploadCreatedISOToS3(s3Client *s3wrapper.S3Client, isoFile, isoObjectName string, log *logrus.Logger) error {
	ctx := context.Background()
	err := s3Client.UploadFile(ctx, isoFile, isoObjectName)
	if err != nil {
		log.Errorf("Failed to upload file %s as object %s", isoFile, isoObjectName)
		return err
	}
	_, err = s3Client.UpdateObjectTimestamp(ctx, isoObjectName)
	return err
}

func main() {
	log := logrus.New()
	log.SetReportCaller(true)

	log.Println("Starting assisted-iso-create")

	err := envconfig.Process("", &Options)
	if err != nil {
		log.Fatal(err.Error())
	}

	ignitionFilePath, err := setIgnitionConfigToFile(Options.WorkDir, Options.IgnitionConfig, log)
	if err != nil {
		log.Fatal(err.Error())
	}

	createImageFile, err := embedIgnitionIntoISO(Options.WorkDir, ignitionFilePath, Options.ImageName, Options.BaseISOFile, log)
	if err != nil {
		log.Fatal(err.Error())
	}

	if Options.UseS3 {
		log.Infof("S3 parameters: bucket %s, region %s", Options.S3Config.S3Bucket, Options.S3Config.Region)

		s3Client := s3wrapper.NewS3Client(&Options.S3Config, log)
		if s3Client == nil {
			log.Fatal("failed to create S3 client, ", err)
		}

		err = uploadCreatedISOToS3(s3Client, createImageFile, Options.ImageName, log)
		if err != nil {
			log.Fatal(err.Error())
		}
	} else {
		log.Println("Using local storage for image")
	}

	log.Println("Image uploaded to S3, Success")
}
