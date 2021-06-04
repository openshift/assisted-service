package isoeditor

import (
	"bytes"
	"fmt"
	"io"

	"github.com/carbonin/overreader"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/pkg/errors"
)

type ClusterISOReader struct {
	// underlying reader for the actual iso
	isoReader io.Reader

	// reader configured to record the ignition info
	infoReader io.Reader
	// reader to use for the customized content
	contentReader io.Reader

	// embed info
	haveEmbedInfo bool
	embedInfo     *bytes.Buffer

	ignitionInfo *OffsetInfo
	ramdiskInfo  *OffsetInfo

	// embed content
	ignition       io.ReadSeeker
	ignitionLength uint64
	ramdisk        io.ReadSeeker
	ramdiskLength  uint64

	imageType models.ImageType
}

func NewClusterISOReader(isoReader io.Reader, ignitionContent string, netFiles []staticnetworkconfig.StaticNetworkConfigData, clusterProxyInfo *ClusterProxyInfo, imageType models.ImageType) (*ClusterISOReader, error) {
	ignitionBytes, err := IgnitionImageArchive(ignitionContent)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create ignition image")
	}

	r := &ClusterISOReader{
		isoReader:      isoReader,
		embedInfo:      new(bytes.Buffer),
		ignition:       bytes.NewReader(ignitionBytes),
		ignitionLength: uint64(len(ignitionBytes)),
		imageType:      imageType,
	}

	var headerLength int64
	switch imageType {
	case models.ImageTypeFullIso:
		headerLength = int64(24)
	case models.ImageTypeMinimalIso:
		headerLength = int64(48)

		if len(netFiles) > 0 || !clusterProxyInfo.Empty() {
			ramdiskBytes, err := ramdiskImageArchive(netFiles, clusterProxyInfo)
			if err != nil {
				return nil, errors.Wrap(err, "failed to create ramdisk image")
			}
			r.ramdisk = bytes.NewReader(ramdiskBytes)
			r.ramdiskLength = uint64(len(ramdiskBytes))
		}
	}

	// set up limit reader for the space before the header
	beforeHeaderReader := io.LimitReader(isoReader, isoSystemAreaSize-headerLength)
	// set up a tee reader to write to the ignition info using a bytes buffer
	headerInfoReader := io.TeeReader(io.LimitReader(isoReader, headerLength), r.embedInfo)

	r.infoReader = io.MultiReader(beforeHeaderReader, headerInfoReader)

	return r, nil
}

func (r *ClusterISOReader) transformEmbedInfo() error {
	infoBytes := make([]byte, 24)
	readBytes := func() error {
		n, err := r.embedInfo.Read(infoBytes)
		if err != nil {
			return errors.Wrap(err, "failed to read embed area data from buffer")
		}
		if n != 24 {
			return errors.New(fmt.Sprintf("incorrect embed info size, expected 24, got %d", n))
		}
		return nil
	}

	var err error
	if r.imageType == models.ImageTypeMinimalIso {
		if err = readBytes(); err != nil {
			return err
		}
		r.ramdiskInfo, err = GetRamDiskArea(infoBytes)
		if err != nil {
			return err
		}
		if r.ramdiskInfo.Length < r.ramdiskLength {
			return errors.New(fmt.Sprintf("ramdisk length (%d) exceeds embed area size (%d)", r.ramdiskLength, r.ramdiskInfo.Length))
		}
	}

	if err = readBytes(); err != nil {
		return err
	}

	r.ignitionInfo, err = GetIgnitionArea(infoBytes)
	if err != nil {
		return err
	}
	if r.ignitionInfo.Length < r.ignitionLength {
		return errors.New(fmt.Sprintf("ignition length (%d) exceeds embed area size (%d)", r.ignitionLength, r.ignitionInfo.Length))
	}

	r.haveEmbedInfo = true

	return nil
}

func (r *ClusterISOReader) Read(p []byte) (int, error) {
	if !r.haveEmbedInfo {
		n, err := r.infoReader.Read(p)
		if err == io.EOF {
			err = r.transformEmbedInfo()
		}
		return n, err
	}

	var err error
	if r.contentReader == nil {
		// Set the offset to the distance to the embed area taking into account that we've already read the system area
		ranges := []*overreader.Range{
			{
				Content: r.ignition,
				Offset:  int64(r.ignitionInfo.Offset - isoSystemAreaSize),
			},
		}

		if r.imageType == models.ImageTypeMinimalIso && r.ramdisk != nil {
			rdRange := &overreader.Range{
				Content: r.ramdisk,
				Offset:  int64(r.ramdiskInfo.Offset - isoSystemAreaSize),
			}
			ranges = append(ranges, rdRange)
		}

		r.contentReader, err = overreader.NewReader(r.isoReader, ranges...)
		if err != nil {
			return 0, errors.Wrapf(err, "failed to create overwrite reader")
		}
	}

	return r.contentReader.Read(p)
}
