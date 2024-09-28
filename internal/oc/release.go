package oc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buger/jsonparser"
	"github.com/hashicorp/go-version"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/system"
	"github.com/openshift/assisted-service/pkg/executer"
	"github.com/openshift/assisted-service/pkg/mirrorregistries"
	"github.com/patrickmn/go-cache"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thedevsaddam/retry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8syaml "sigs.k8s.io/yaml"
)

const (
	mcoImageName                   = "machine-config-operator"
	ironicAgentImageName           = "ironic-agent"
	mustGatherImageName            = "must-gather"
	okdRPMSImageName               = "okd-rpms"
	DefaultTries                   = 5
	DefaltRetryDelay               = time.Second * 5
	staticInstallerRequiredVersion = "4.16.0-0.alpha"
)

type Config struct {
	MaxTries   uint
	RetryDelay time.Duration
}

//go:generate mockgen -source=release.go -package=oc -destination=mock_release.go
type Release interface {
	GetMCOImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetIronicAgentImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetOKDRPMSImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetMustGatherImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetOpenshiftVersion(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetMajorMinorVersion(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error)
	GetReleaseArchitecture(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) ([]string, error)
	GetReleaseBinaryPath(releaseImage string, cacheDir string, ocpVersion string) (workdir string, binary string, path string, err error)
	Extract(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string, ocpVersion string) (string, error)
}

type imageValue struct {
	value string
	mutex sync.Mutex
}

type release struct {
	executer                executer.Executer
	config                  Config
	mirrorRegistriesBuilder mirrorregistries.MirrorRegistriesConfigBuilder
	sys                     system.SystemInfo

	// A map for caching images (image name > release image URL > image)
	imagesMap common.ExpiringCache
}

func NewRelease(executer executer.Executer, config Config, mirrorRegistriesBuilder mirrorregistries.MirrorRegistriesConfigBuilder, sys system.SystemInfo) Release {
	return &release{
		executer:                executer,
		config:                  config,
		imagesMap:               common.NewExpiringCache(cache.NoExpiration, cache.NoExpiration),
		mirrorRegistriesBuilder: mirrorRegistriesBuilder,
		sys:                     sys,
	}
}

const (
	templateGetImage              = "oc adm release info --image-for=%s --insecure=%t %s"
	templateGetImageWithIcsp      = "oc adm release info --image-for=%s --insecure=%t --icsp-file=%s %s"
	templateGetVersion            = "oc adm release info -o template --template '{{.metadata.version}}' --insecure=%t %s"
	templateGetVersionWithIcsp    = "oc adm release info -o template --template '{{.metadata.version}}' --insecure=%t --icsp-file=%s %s"
	templateExtract               = "oc adm release extract --command=%s --to=%s --insecure=%t %s"
	templateExtractWithIcsp       = "oc adm release extract --command=%s --to=%s --insecure=%t --icsp-file=%s %s"
	templateImageInfo             = "oc image info --output json %s"
	templateImageInfoWithIcsp     = "oc image info --output json --icsp-file=%s %s"
	templateSkopeoDetectMultiarch = "skopeo inspect --raw --no-tags docker://%s"
	ocAuthArgument                = " --registry-config="
	skopeoAuthArgument            = " --authfile "
)

// GetMCOImage gets mcoImage url from the releaseImageMirror if provided.
// Else gets it from the source releaseImage
func (r *release) GetMCOImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	return r.getImageByName(log, mcoImageName, releaseImage, releaseImageMirror, pullSecret)
}

// GetIronicAgentImage gets the ironic agent image url from the releaseImageMirror if provided.
// Else gets it from the source releaseImage
func (r *release) GetIronicAgentImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	return r.getImageByName(log, ironicAgentImageName, releaseImage, releaseImageMirror, pullSecret)
}

// GetOKDRPMSImage gets okd RPMS image URL from the release image or releaseImageMirror, if provided.
func (r *release) GetOKDRPMSImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	return r.getImageByName(log, okdRPMSImageName, releaseImage, releaseImageMirror, pullSecret)
}

// GetMustGatherImage gets must-gather image URL from the release image or releaseImageMirror, if provided.
func (r *release) GetMustGatherImage(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	return r.getImageByName(log, mustGatherImageName, releaseImage, releaseImageMirror, pullSecret)
}

func (r *release) getImageByName(log logrus.FieldLogger, imageName, releaseImage, releaseImageMirror, pullSecret string) (string, error) {
	var image string
	var err error
	if releaseImage == "" && releaseImageMirror == "" {
		return "", errors.New("neither releaseImage, nor releaseImageMirror are provided")
	}

	icspFile, err := r.getIcspFileFromRegistriesConfig(log)
	if err != nil {
		return "", errors.Wrap(err, "failed to create file ICSP file from registries config")
	}
	defer removeIcspFile(icspFile)

	if releaseImageMirror != "" {
		//TODO: Get mirror registry certificate from install-config
		image, err = r.getImageFromRelease(log, imageName, releaseImageMirror, pullSecret, icspFile, true)
		if err != nil {
			log.WithError(err).Errorf("failed to get %s image from mirror release image %s", imageName, releaseImageMirror)
			return "", err
		}
	} else {
		image, err = r.getImageFromRelease(log, imageName, releaseImage, pullSecret, icspFile, false)
		if err != nil {
			log.WithError(err).Errorf("failed to get %s image from release image %s", imageName, releaseImage)
			return "", err
		}
	}
	return image, err
}

func (r *release) GetOpenshiftVersion(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	var openshiftVersion string
	var err error
	if releaseImage == "" && releaseImageMirror == "" {
		return "", errors.New("no releaseImage nor releaseImageMirror provided")
	}

	icspFile, err := r.getIcspFileFromRegistriesConfig(log)
	if err != nil {
		return "", errors.Wrap(err, "failed to create file ICSP file from registries config")
	}
	defer removeIcspFile(icspFile)

	if releaseImageMirror != "" {
		//TODO: Get mirror registry certificate from install-config
		openshiftVersion, err = r.getOpenshiftVersionFromRelease(log, releaseImageMirror, pullSecret, icspFile, true)
		if err != nil {
			log.WithError(err).Errorf("failed to get image openshift version from mirror release image %s", releaseImageMirror)
			return "", err
		}
	} else {
		openshiftVersion, err = r.getOpenshiftVersionFromRelease(log, releaseImage, pullSecret, icspFile, false)
		if err != nil {
			log.WithError(err).Errorf("failed to get image openshift version from release image %s", releaseImage)
			return "", err
		}
	}

	return openshiftVersion, err
}

func (r *release) GetMajorMinorVersion(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) (string, error) {
	openshiftVersion, err := r.GetOpenshiftVersion(log, releaseImage, releaseImageMirror, pullSecret)
	if err != nil {
		return "", err
	}

	v, err := version.NewVersion(openshiftVersion)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%d.%d", v.Segments()[0], v.Segments()[1]), nil
}

func (r *release) GetReleaseArchitecture(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, pullSecret string) ([]string, error) {
	image := releaseImageMirror
	if image == "" {
		image = releaseImage
	}
	if image == "" {
		return nil, errors.New("no releaseImage nor releaseImageMirror provided")
	}

	icspFile, err := r.getIcspFileFromRegistriesConfig(log)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create file ICSP file from registries config")
	}
	defer removeIcspFile(icspFile)

	var cmd string
	if icspFile == "" {
		cmd = fmt.Sprintf(templateImageInfo, image)
	} else {
		cmd = fmt.Sprintf(templateImageInfoWithIcsp, icspFile, image)
	}

	cmdMultiarch := fmt.Sprintf(templateSkopeoDetectMultiarch, image)

	imageInfoStr, err := execute(log, r.executer, pullSecret, cmd, ocAuthArgument)
	if err != nil {
		// TODO(WRKLDS-222) At this moment we don't have a better way to detect if the release image is a multiarch
		//                  image. Introducing skopeo as an additional dependency, to be able to manually parse
		//                  the manifest. https://bugzilla.redhat.com/show_bug.cgi?id=2111537 tracks the missing
		//                  feature in oc cli.
		skopeoImageRaw, err2 := execute(log, r.executer, pullSecret, cmdMultiarch, skopeoAuthArgument)
		if err2 != nil {
			return nil, errors.Errorf("failed to inspect image, oc: %v, skopeo: %v", err, err2)
		}

		var multiarchContent []string
		_, err2 = jsonparser.ArrayEach([]byte(skopeoImageRaw), func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
			res, _ := jsonparser.GetString(value, "platform", "architecture")

			// Convert architecture naming to supported values
			res = common.NormalizeCPUArchitecture(res)
			if res == "" {
				return
			}

			multiarchContent = append(multiarchContent, res)
		}, "manifests")
		if err2 != nil {
			return nil, errors.Errorf("failed to get image info using oc: %v", err)
		}

		if len(multiarchContent) == 0 {
			return nil, errors.Errorf("image manifest does not contain architecture: %v", skopeoImageRaw)
		}

		return multiarchContent, nil
	}

	architecture, err := jsonparser.GetString([]byte(imageInfoStr), "config", "architecture")
	if err != nil {
		return nil, err
	}

	// Convert architecture naming to supported values
	architecture = common.NormalizeCPUArchitecture(architecture)

	return []string{architecture}, nil
}

func getImageKey(imageName, releaseImage string) string {
	return imageName + "@" + releaseImage
}

func (r *release) getImageValue(imageName, releaseImage string) (*imageValue, error) {
	actualIntf, _ := r.imagesMap.GetOrInsert(getImageKey(imageName, releaseImage), &imageValue{})
	value, ok := actualIntf.(*imageValue)
	if !ok {
		return nil, errors.Errorf("unexpected error - could not cast value for image %s release %s", imageName, releaseImage)
	}
	return value, nil
}

func (r *release) getImageFromRelease(log logrus.FieldLogger, imageName, releaseImage, pullSecret, icspFile string, insecure bool) (string, error) {
	// Fetch image URL from cache
	actualImageValue, err := r.getImageValue(imageName, releaseImage)
	if err != nil {
		return "", err
	}
	if actualImageValue.value != "" {
		return actualImageValue.value, nil
	}
	actualImageValue.mutex.Lock()
	defer actualImageValue.mutex.Unlock()
	if actualImageValue.value != "" {
		return actualImageValue.value, nil
	}

	var cmd string
	if icspFile == "" {
		cmd = fmt.Sprintf(templateGetImage, imageName, insecure, releaseImage)
	} else {
		cmd = fmt.Sprintf(templateGetImageWithIcsp, imageName, insecure, icspFile, releaseImage)
	}

	log.Infof("Fetching image from OCP release (%s)", cmd)
	image, err := execute(log, r.executer, pullSecret, cmd, ocAuthArgument)
	if err != nil {
		return "", err
	}

	// Update image URL in cache
	actualImageValue.value = image

	return image, nil
}

func (r *release) getOpenshiftVersionFromRelease(log logrus.FieldLogger, releaseImage, pullSecret, icspFile string, insecure bool) (string, error) {
	var cmd string
	if icspFile == "" {
		cmd = fmt.Sprintf(templateGetVersion, insecure, releaseImage)
	} else {
		cmd = fmt.Sprintf(templateGetVersionWithIcsp, insecure, icspFile, releaseImage)
	}
	version, err := execute(log, r.executer, pullSecret, cmd, ocAuthArgument)
	if err != nil {
		return "", err
	}
	// Trimming as output is retrieved wrapped with single quotes.
	return strings.Trim(version, "'"), nil
}

// Extract installer binary from releaseImageMirror if provided.
// Else extract from the source releaseImage
func (r *release) Extract(log logrus.FieldLogger, releaseImage string, releaseImageMirror string, cacheDir string, pullSecret string, ocpVersion string) (string, error) {
	var path string
	var err error
	if releaseImage == "" && releaseImageMirror == "" {
		return "", errors.New("no releaseImage or releaseImageMirror provided")
	}

	icspFile, err := r.getIcspFileFromRegistriesConfig(log)
	if err != nil {
		return "", errors.Wrap(err, "failed to create file ICSP file from registries config")
	}
	defer removeIcspFile(icspFile)

	if releaseImageMirror != "" {
		//TODO: Get mirror registry certificate from install-config
		path, err = r.extractFromRelease(log, releaseImageMirror, cacheDir, pullSecret, true, icspFile, ocpVersion)
		if err != nil {
			log.WithError(err).Errorf("failed to extract openshift-baremetal-install from mirror release image %s", releaseImageMirror)
			return "", err
		}
	} else {
		path, err = r.extractFromRelease(log, releaseImage, cacheDir, pullSecret, false, icspFile, ocpVersion)
		if err != nil {
			log.WithError(err).Errorf("failed to extract openshift-baremetal-install from release image %s", releaseImage)
			return "", err
		}
	}
	return path, err
}

func (r *release) GetReleaseBinaryPath(releaseImage string, cacheDir string, ocpVersion string) (workdir string, binary string, path string, err error) {
	binary = "openshift-baremetal-install"

	fipsEnabled, err := r.sys.FIPSEnabled()
	if err != nil {
		return "", "", "", err
	}

	if !fipsEnabled {
		// use the statically linked binary for 4.16 and up since our container is el8
		// based and the baremetal binary for those versions is dynamically linked
		// against el9 libaries
		staticLinkingRequiredVersion := version.Must(version.NewVersion(staticInstallerRequiredVersion))
		v, err := version.NewVersion(ocpVersion)
		if err != nil {
			return "", "", "", err
		}
		if v.GreaterThanOrEqual(staticLinkingRequiredVersion) {
			binary = "openshift-install"
		}
	}

	workdir = filepath.Join(cacheDir, releaseImage)
	path = filepath.Join(workdir, binary)
	return
}

// extractFromRelease returns the path to an openshift-baremetal-install binary extracted from
// the referenced release image.
func (r *release) extractFromRelease(log logrus.FieldLogger, releaseImage, cacheDir, pullSecret string, insecure bool, icspFile string, ocpVersion string) (string, error) {
	workdir, binary, path, err := r.GetReleaseBinaryPath(releaseImage, cacheDir, ocpVersion)
	if err != nil {
		return "", err
	}
	log.Infof("extracting %s binary to %s", binary, workdir)
	err = os.MkdirAll(workdir, 0755)
	if err != nil {
		return "", err
	}

	var cmd string
	if icspFile == "" {
		cmd = fmt.Sprintf(templateExtract, binary, workdir, insecure, releaseImage)
	} else {
		cmd = fmt.Sprintf(templateExtractWithIcsp, binary, workdir, insecure, icspFile, releaseImage)
	}

	_, err = retry.Do(r.config.MaxTries, r.config.RetryDelay, execute, log, r.executer, pullSecret, cmd, ocAuthArgument)
	if err != nil {
		return "", err
	}

	log.Infof("Successfully extracted %s binary from the release to: %s", binary, path)
	return path, nil
}

func execute(log logrus.FieldLogger, executer executer.Executer, pullSecret string, command string, authArgument string) (string, error) {
	// write pull secret to a temp file
	ps, err := executer.TempFile("", "registry-config")
	if err != nil {
		return "", err
	}
	defer func() {
		ps.Close()
		os.Remove(ps.Name())
	}()
	_, err = ps.Write([]byte(pullSecret))
	if err != nil {
		return "", err
	}
	// flush the buffer to ensure the file can be read
	ps.Close()
	executeCommand := command[:] + authArgument + ps.Name()
	args := strings.Split(executeCommand, " ")

	stdout, stderr, exitCode := executer.Execute(args[0], args[1:]...)

	if exitCode == 0 {
		return strings.TrimSpace(stdout), nil
	} else {
		err = fmt.Errorf("command '%s' exited with non-zero exit code %d: %s\n%s", executeCommand, exitCode, stdout, stderr)
		log.Warn(err)
		return "", err
	}
}

// Create a temporary file containing the ImageContentPolicySources
func (r *release) getIcspFileFromRegistriesConfig(log logrus.FieldLogger) (string, error) {

	if !r.mirrorRegistriesBuilder.IsMirrorRegistriesConfigured() {
		log.Debugf("No mirrors configured to build ICSP file")
		return "", nil
	}

	mirrorRegistriesConfig, err := r.mirrorRegistriesBuilder.ExtractLocationMirrorDataFromRegistries()
	if err != nil {
		log.WithError(err).Errorf("Failed to get the mirror registries needed for ImageContentSources")
		return "", err
	}

	contents, err := getIcspContents(mirrorRegistriesConfig)
	if err != nil {
		log.WithError(err).Errorf("Failed to create the ICSP file from registries.conf")
		return "", err
	}
	if contents == nil {
		log.Debugf("No registry entries to build ICSP file")
		return "", nil
	}

	icspFile, err := os.CreateTemp("", "icsp-file")
	if err != nil {
		return "", err
	}
	log.Debugf("Building ICSP file from registries.conf with contents %s", contents)
	if _, err := icspFile.Write(contents); err != nil {
		icspFile.Close()
		os.Remove(icspFile.Name())
		return "", err
	}
	icspFile.Close()

	return icspFile.Name(), nil
}

// Convert the data in registries.conf into ICSP format
func getIcspContents(mirrorConfig []mirrorregistries.RegistriesConf) ([]byte, error) {

	icsp := operatorv1alpha1.ImageContentSourcePolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: operatorv1alpha1.SchemeGroupVersion.String(),
			Kind:       "ImageContentSourcePolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "image-policy",
			// not namespaced
		},
	}

	icsp.Spec.RepositoryDigestMirrors = make([]operatorv1alpha1.RepositoryDigestMirrors, len(mirrorConfig))
	for i, mirrorRegistries := range mirrorConfig {
		icsp.Spec.RepositoryDigestMirrors[i] = operatorv1alpha1.RepositoryDigestMirrors{Source: mirrorRegistries.Location, Mirrors: mirrorRegistries.Mirror}
	}

	// Convert to json first so json tags are handled
	jsonData, err := json.Marshal(&icsp)
	if err != nil {
		return nil, err
	}
	contents, err := k8syaml.JSONToYAML(jsonData)
	if err != nil {
		return nil, err
	}

	return contents, nil
}

func removeIcspFile(filename string) {
	if filename != "" {
		os.Remove(filename)
	}
}
