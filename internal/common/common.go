package common

import (
	"archive/tar"
	"context"
	"io"
	"sync"
	"time"

	"github.com/go-openapi/swag"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const MinMasterHostsNeededForInstallation = 3
const AllowedNumberOfMasterHostsInNoneHaMode = 1
const AllowedNumberOfWorkersInNoneHaMode = 0
const IllegalWorkerHostsCount = 1

const HostCACertPath = "/etc/assisted-service/service-ca-cert.crt"

// Configuration to be injected by discovery ignition.  It will cause IPv6 DHCP client identifier to be the same
// after reboot.  This will cause the DHCP server to provide the same IP address after reboot.
const Ipv6DuidDiscoveryConf = `
[connection]
ipv6.dhcp-iaid=mac
ipv6.dhcp-duid=ll
`

// Configuration to be used by MCO manifest to get consistent IPv6 DHCP client identification.
const Ipv6DuidRuntimeConf = `
[connection]
ipv6.dhcp-iaid=mac
ipv6.dhcp-duid=ll
[keyfile]
path=/etc/NetworkManager/system-connections-merged
`

// continueOnError is set when running as stream, error is doing nothing when it happens cause we in the middle of stream
// and 200 was already returned
func CreateTar(ctx context.Context, w io.Writer, files, tarredFilenames []string, client s3wrapper.API, continueOnError bool) error {
	var rdr io.ReadCloser
	tarWriter := tar.NewWriter(w)
	defer func() {
		if rdr != nil {
			rdr.Close()
		}
		tarWriter.Close()

	}()
	var err error
	var objectSize int64

	// Create tar headers from s3 files
	for i, file := range files {
		// Read file from S3, log any errors
		rdr, objectSize, err = client.Download(ctx, file)
		if err != nil {
			if continueOnError {
				continue
			}
			return errors.Wrapf(err, "Failed to open reader for %s", file)
		}

		header := tar.Header{
			Name:    tarredFilenames[i],
			Size:    objectSize,
			Mode:    0644,
			ModTime: time.Now(),
		}
		err = tarWriter.WriteHeader(&header)
		if err != nil && !continueOnError {
			return errors.Wrapf(err, "Failed to write tar header with file %s details", file)
		}
		_, err = io.Copy(tarWriter, rdr)
		if err != nil && !continueOnError {
			return errors.Wrapf(err, "Failed to write file %s to tar", file)
		}
		_ = rdr.Close()
	}

	return nil
}

// Tar given files in s3 bucket.
// We open pipe for reading from aws and writing archived back to it while archiving them.
// It creates stream by using io.pipe
func TarAwsFiles(ctx context.Context, tarName string, files, tarredFilenames []string, client s3wrapper.API, log logrus.FieldLogger) error {
	// Create pipe
	var err error
	pr, pw := io.Pipe()
	wg := sync.WaitGroup{}
	// Wait for downloader and uploader
	wg.Add(2)
	// Run 'downloader'
	go func() {
		defer func() {
			wg.Done()
			// closing pipe will stop uploading
			pw.Close()
		}()
		downloadError := CreateTar(ctx, pw, files, tarredFilenames, client, false)
		if downloadError != nil && err == nil {
			err = errors.Wrapf(downloadError, "Failed to download files while creating archive %s", tarName)
			log.Error(err)
		}
	}()
	go func() {
		defer func() {
			wg.Done()
			// if upload fails close pipe
			// will fail download too
			pr.Close()
		}()
		// Upload the file, body is `io.Reader` from pipe
		uploadError := client.UploadStream(ctx, pr, tarName)
		if uploadError != nil && err == nil {
			err = errors.Wrapf(uploadError, "Failed to upload archive %s", tarName)
			log.Error(err)
		}
	}()
	wg.Wait()
	return err
}

func AllStrings(vs []string, f func(string) bool) bool {
	for _, v := range vs {
		if !f(v) {
			return false
		}
	}
	return true
}

// GetBootstrapHost return host that was set as bootstrap
func GetBootstrapHost(cluster *Cluster) *models.Host {
	for _, host := range cluster.Hosts {
		if host.Bootstrap {
			return host
		}
	}
	return nil
}

// IsSingleNodeCluster if this cluster is single-node or not
func IsSingleNodeCluster(cluster *Cluster) bool {
	return swag.StringValue(cluster.HighAvailabilityMode) == models.ClusterHighAvailabilityModeNone
}
