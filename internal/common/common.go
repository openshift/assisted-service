package common

import (
	"archive/tar"
	"context"
	"io"

	"github.com/openshift/assisted-service/pkg/s3wrapper"
	"github.com/pkg/errors"
)

const MinMasterHostsNeededForInstallation = 3

func CreateTar(ctx context.Context, w io.Writer, files []string, client s3wrapper.API) error {
	var rdr io.ReadCloser
	tarWriter := tar.NewWriter(w)
	defer func() {
		tarWriter.Close()
		if rdr != nil {
			rdr.Close()
		}
	}()
	var err error
	var objectSize int64

	// Create tar headers from s3 files
	for _, file := range files {
		// Read file from S3, log any errors
		rdr, objectSize, err = client.Download(ctx, file)
		if err != nil {
			return errors.Wrapf(err, "Failed to open reader for %s", file)
		}

		header := tar.Header{
			Name: file,
			Size: objectSize,
		}
		err = tarWriter.WriteHeader(&header)
		if err != nil {
			return errors.Wrapf(err, "Failed to write tar header with file %s details", file)
		}
		_, err = io.Copy(tarWriter, rdr)
		if err != nil {
			return errors.Wrapf(err, "Failed to write file %s to tar", file)
		}
		_ = rdr.Close()
	}

	return nil
}
