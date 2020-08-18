package main

import (
	"context"
	"os"

	"github.com/kelseyhightower/envconfig"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/sirupsen/logrus"
)

var Options struct {
	ClusterID string `envconfig:"CLUSTER_ID"`
	S3Config  s3wrapper.Config
}

var dummyFileMap map[string]string = map[string]string{
	"bootstrap.ign":        "bootstrap file",
	"master.ign":           "master file",
	"worker.ign":           "worker file",
	"kubeadmin-password":   "kubeadmin-password file",
	"kubeconfig-noingress": "kubeconfig-noingress file",
	"metadata.json":        "metadata file",
	"install-config.yaml":  "install-config file",
}

func createDummyFile(name, data string) error {
	if name == "kubeconfig-noingress" {
		return nil
	}
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	_, err = f.WriteString("data")
	f.Close()
	return err
}

func createDummyFiles() error {
	for fileName, fileData := range dummyFileMap {
		err := createDummyFile(fileName, fileData)
		if err != nil {
			return err
		}
	}
	return nil
}

func uploadDummyFiles(s3Client *s3wrapper.S3Client, clusterID string) error {
	ctx := context.Background()
	for fileName := range dummyFileMap {
		err := s3Client.UploadFile(ctx, fileName, clusterID+"/"+fileName)
		if err != nil {
			return err
		}
	}
	return nil
}

func main() {
	log := logrus.New()
	log.SetReportCaller(true)

	log.Println("Starting dummy-ignition")

	err := envconfig.Process("", &Options)
	if err != nil {
		log.Fatal(err.Error())
	}

	log.Infof("S3 parameters: bucket %s, region %s", Options.S3Config.S3Bucket, Options.S3Config.Region)

	s3Client := s3wrapper.NewS3Client(&Options.S3Config, log)
	if s3Client == nil {
		log.Fatal("failed to create S3 client, ", err)
	}

	err = createDummyFiles()
	if err != nil {
		log.Fatalf("Failed to create dummy files, err: %s", err)
	}

	err = uploadDummyFiles(s3Client, Options.ClusterID)
	if err != nil {
		log.Fatalf("Failed to upload dummy files, err: %s", err)
	}

	log.Println("Dummy ignition finished, Success")
}
