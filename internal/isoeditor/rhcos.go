package isoeditor

import (
	"bytes"
	"compress/gzip"
	"path/filepath"
	"text/template"

	"github.com/cavaliercoder/go-cpio"
	"github.com/openshift/assisted-service/internal/constants"
	"github.com/openshift/assisted-service/pkg/staticnetworkconfig"
	"github.com/pkg/errors"
)

const rootfsServiceConfigFormat = `[Service]
Environment=http_proxy={{.HTTP_PROXY}}
Environment=https_proxy={{.HTTPS_PROXY}}
Environment=no_proxy={{.NO_PROXY}}
Environment=HTTP_PROXY={{.HTTP_PROXY}}
Environment=HTTPS_PROXY={{.HTTPS_PROXY}}
Environment=NO_PROXY={{.NO_PROXY}}`

type ClusterProxyInfo struct {
	HTTPProxy  string
	HTTPSProxy string
	NoProxy    string
}

func (i *ClusterProxyInfo) Empty() bool {
	return i == nil || (i.HTTPProxy == "" && i.HTTPSProxy == "" && i.NoProxy == "")
}

func RamdiskImageArchive(netFiles []staticnetworkconfig.StaticNetworkConfigData, clusterProxyInfo *ClusterProxyInfo) ([]byte, error) {
	if len(netFiles) == 0 && clusterProxyInfo.Empty() {
		return nil, nil
	}
	buffer := new(bytes.Buffer)
	w := cpio.NewWriter(buffer)
	if len(netFiles) > 0 {
		for _, file := range netFiles {
			err := addFileToArchive(w, filepath.Join("/etc/assisted/network", file.FilePath), file.FileContents, 0o600)
			if err != nil {
				return nil, err
			}
		}

		scriptPath := "/usr/local/bin/pre-network-manager-config.sh"
		scriptContent := constants.PreNetworkConfigScript
		if err := addFileToArchive(w, scriptPath, scriptContent, 0o755); err != nil {
			return nil, err
		}

		servicePath := "/etc/systemd/system/pre-network-manager-config.service"
		serviceContent := constants.MinimalISONetworkConfigService
		if err := addFileToArchive(w, servicePath, serviceContent, 0o644); err != nil {
			return nil, err
		}

		serviceLink := "/etc/systemd/system/initrd.target.wants/pre-network-manager-config.service"
		if err := addFileToArchive(w, serviceLink, servicePath, cpio.ModeSymlink|0o777); err != nil {
			return nil, err
		}
	}
	if !clusterProxyInfo.Empty() {
		rootfsServiceConfigPath := "/etc/systemd/system/coreos-livepxe-rootfs.service.d/10-proxy.conf"
		rootfsServiceConfig, err := formatRootfsServiceConfigFile(clusterProxyInfo)
		if err != nil {
			return nil, err
		}
		if err := addFileToArchive(w, rootfsServiceConfigPath, rootfsServiceConfig, 0o664); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	// Run gzip compression
	compressedBuffer := new(bytes.Buffer)
	gzipWriter := gzip.NewWriter(compressedBuffer)
	if _, err := gzipWriter.Write(buffer.Bytes()); err != nil {
		return nil, errors.Wrap(err, "Failed to gzip archive")
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, errors.Wrap(err, "Failed to gzip archive")
	}

	return compressedBuffer.Bytes(), nil
}

func formatRootfsServiceConfigFile(clusterProxyInfo *ClusterProxyInfo) (string, error) {
	var rootfsServicConfigParams = map[string]string{
		"HTTP_PROXY":  clusterProxyInfo.HTTPProxy,
		"HTTPS_PROXY": clusterProxyInfo.HTTPSProxy,
		"NO_PROXY":    clusterProxyInfo.NoProxy,
	}
	tmpl, err := template.New("rootfsServiceConfig").Parse(rootfsServiceConfigFormat)
	if err != nil {
		return "", err
	}
	buf := &bytes.Buffer{}
	if err = tmpl.Execute(buf, rootfsServicConfigParams); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func addFileToArchive(w *cpio.Writer, path string, content string, mode cpio.FileMode) error {
	// add all the directories in path in the correct order, using dirsStack as FILO
	dirsStack := []string{}
	for dir := filepath.Dir(path); dir != "" && dir != "/"; dir = filepath.Dir(dir) {
		dirsStack = append(dirsStack, dir)
	}
	for i := len(dirsStack) - 1; i >= 0; i-- {
		hdr := &cpio.Header{
			Name: dirsStack[i],
			Mode: 040755,
			Size: 0,
		}
		if err := w.WriteHeader(hdr); err != nil {
			return err
		}
	}

	// add the file content
	hdr := &cpio.Header{
		Name: path,
		Mode: mode,
		Size: int64(len(content)),
	}
	if err := w.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := w.Write([]byte(content)); err != nil {
		return err
	}
	return nil
}
