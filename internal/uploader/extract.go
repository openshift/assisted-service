package uploader

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	eventModels "github.com/openshift/assisted-service/pkg/uploader/models"
)

// ExtractEvents parses the uploaded content and returns events per clusterID.
// content must be a reader of the uploaded binary.
func ExtractEvents(content io.Reader) ([]eventModels.Events, error) {
	if content == nil {
		return nil, nil
	}

	events := make(map[string]eventModels.Events)

	gzipReader, err := gzip.NewReader(content)
	if err != nil {
		if errors.Is(err, io.EOF) {
			// Empty reader
			return nil, nil
		}

		return nil, fmt.Errorf("failed to read gzip: %w", err)
	}

	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

LOOP:
	for {
		header, err := tarReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break LOOP
			}

			return nil, fmt.Errorf("failed to read next entry in tar archive: %w", err)
		}

		splitFileName := strings.SplitN(header.Name, "/", 2)
		if len(splitFileName) != 2 {
			return nil, fmt.Errorf("unexpected filename %s", header.Name)
		}

		clusterID := splitFileName[0]

		event, ok := events[clusterID]
		if !ok {
			event = eventModels.NewEvents(clusterID)
		}

		switch {
		// Metadata
		case strings.HasSuffix(header.Name, metadataFileName):
			meta, err := extractMeta(tarReader)
			if err != nil {
				return nil, fmt.Errorf("failed to read metadata file %s: %w", header.Name, err)
			}

			// This should never happen since they would have the file name
			if event.Metadata != nil && *event.Metadata != meta {
				return nil, fmt.Errorf("metadata are defined twice for cluster %s", clusterID)
			}

			event.Metadata = &meta

		// Other file types are not supported yet
		default:
			continue
		}

		events[clusterID] = event
	}

	ret := make([]eventModels.Events, 0, len(events))
	for _, event := range events {
		ret = append(ret, event)
	}

	return ret, nil
}

func extractMeta(reader io.Reader) (eventModels.Metadata, error) {
	ret := eventModels.Metadata{}

	bytes, err := io.ReadAll(reader)
	if err != nil {
		return ret, fmt.Errorf("failed to read from reader: %w", err)
	}

	err = json.Unmarshal(bytes, &ret)
	if err != nil {
		return ret, fmt.Errorf("failed to extract metadata: %w", err)
	}

	return ret, nil
}
