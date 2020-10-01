package main

import (
	"context"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
)

var Options struct {
	ImageName   string `envconfig:"IMAGE_NAME"`
	BaseISOFile string `envconfig:"COREOS_IMAGE"`
	S3Config    s3wrapper.Config
}

func main() {
	ctx := context.Background()
	log := logrus.New()
	log.SetReportCaller(true)

	log.Println("Starting assisted-iso-create")

	err := envconfig.Process("", &Options)
	if err != nil {
		log.Fatal(err.Error())
	}

	var s3Client *s3wrapper.S3Client
	log.Infof("S3 parameters: bucket %s, region %s", Options.S3Config.S3Bucket, Options.S3Config.Region)

	s3Client = s3wrapper.NewS3Client(&Options.S3Config, log)
	if s3Client == nil {
		log.Fatal("failed to create S3 client")
	}
	exists, err := s3Client.DoesObjectExist(ctx, Options.ImageName)
	if err != nil {
		log.WithError(err).Fatalf("Failed checking if base image exists")
	}
	if !exists {
		err = s3Client.UploadFile(ctx, Options.BaseISOFile, Options.ImageName)
		if err != nil {
			log.WithError(err).Fatalf("Failed to upload file %s as object %s", Options.BaseISOFile, Options.ImageName)
		}
	}

	log.Println("Image successfully uploaded to S3")
}
