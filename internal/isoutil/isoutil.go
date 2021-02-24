package isoutil

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
	"github.com/pkg/errors"
)

//go:generate mockgen -package=isoutil -destination=mock_isoutil.go . Handler
type Handler interface {
	Extract() error
	ExtractedPath(rel string) string
	Create(outPath string, volumeLabel string) error
	ReadFile(filePath string) (io.ReadWriteSeeker, error)
	VolumeIdentifier() (string, error)
}

type installerHandler struct {
	isoPath string
	workDir string
}

func NewHandler(isoPath, workDir string) Handler {
	return &installerHandler{isoPath: isoPath, workDir: workDir}
}

func (h *installerHandler) isoFS() (filesystem.FileSystem, error) {
	d, err := diskfs.OpenWithMode(h.isoPath, diskfs.ReadOnly)
	if err != nil {
		return nil, err
	}

	fs, err := d.GetFilesystem(0)
	if err != nil {
		return nil, err
	}

	return fs, nil
}

// ReadFile returns a reader for a known path in the iso without extracting first
func (h *installerHandler) ReadFile(filePath string) (io.ReadWriteSeeker, error) {
	fs, err := h.isoFS()
	if err != nil {
		return nil, err
	}

	fsFile, err := fs.OpenFile(filePath, os.O_RDONLY)
	if err != nil {
		return nil, err
	}

	return fsFile, nil
}

func (h *installerHandler) ExtractedPath(rel string) string {
	return filepath.Join(h.workDir, rel)
}

// Extract unpacks the iso contents into the working directory
func (h *installerHandler) Extract() error {
	fs, err := h.isoFS()
	if err != nil {
		return err
	}

	files, err := fs.ReadDir("/")
	if err != nil {
		return err
	}
	err = copyAll(fs, "/", files, h.workDir)
	if err != nil {
		return err
	}

	return nil
}

// recursive function for unpacking all files and directores from the given iso filesystem starting at fsDir
func copyAll(fs filesystem.FileSystem, fsDir string, infos []os.FileInfo, targetDir string) error {
	for _, info := range infos {
		osName := filepath.Join(targetDir, info.Name())
		fsName := filepath.Join(fsDir, info.Name())

		if info.IsDir() {
			if err := os.Mkdir(osName, info.Mode().Perm()); err != nil {
				return err
			}

			files, err := fs.ReadDir(fsName)
			if err != nil {
				return err
			}
			if err := copyAll(fs, fsName, files[:], osName); err != nil {
				return err
			}
		} else {
			fsFile, err := fs.OpenFile(fsName, os.O_RDONLY)
			if err != nil {
				return err
			}
			osFile, err := os.Create(osName)
			if err != nil {
				return err
			}

			_, err = io.Copy(osFile, fsFile)
			if err != nil {
				osFile.Close()
				return err
			}

			if err := osFile.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// Create builds an iso file at outPath with the given volumeLabel using the contents of the working directory
func (h *installerHandler) Create(outPath string, volumeLabel string) error {
	// Use the minimum iso size that will satisfy diskfs validations here.
	// This value doesn't determine the final image size, but is used
	// to truncate the initial file. This value would be relevant if
	// we were writing to a particular partition on a device, but we are
	// not so the minimum iso size will work for us here
	minISOSize := 38 * 1024
	d, err := diskfs.Create(outPath, int64(minISOSize), diskfs.Raw)
	if err != nil {
		return err
	}

	d.LogicalBlocksize = 2048
	fspec := disk.FilesystemSpec{
		Partition:   0,
		FSType:      filesystem.TypeISO9660,
		VolumeLabel: volumeLabel,
		WorkDir:     h.workDir,
	}
	fs, err := d.CreateFilesystem(fspec)
	if err != nil {
		return err
	}

	iso, ok := fs.(*iso9660.FileSystem)
	if !ok {
		return fmt.Errorf("not an iso9660 filesystem")
	}

	options := iso9660.FinalizeOptions{
		RockRidge:        true,
		VolumeIdentifier: volumeLabel,
	}

	if haveFiles, err := h.haveBootFiles(); err != nil {
		return err
	} else if haveFiles {
		options.ElTorito = &iso9660.ElTorito{
			BootCatalog: "isolinux/boot.cat",
			Entries: []*iso9660.ElToritoEntry{
				{
					Platform:  iso9660.BIOS,
					Emulation: iso9660.NoEmulation,
					BootFile:  "isolinux/isolinux.bin",
					BootTable: true,
					LoadSize:  4,
				},
				{
					Platform:  iso9660.EFI,
					Emulation: iso9660.NoEmulation,
					BootFile:  "images/efiboot.img",
				},
			},
		}
	}

	return iso.Finalize(options)
}

func (h *installerHandler) haveBootFiles() (bool, error) {
	files := []string{"isolinux/boot.cat", "isolinux/isolinux.bin", "images/efiboot.img"}
	for _, f := range files {
		if exists, err := h.fileExists(f); err != nil {
			return false, err
		} else if !exists {
			return false, nil
		}
	}

	return true, nil
}

func (h *installerHandler) fileExists(relName string) (bool, error) {
	name := filepath.Join(h.workDir, relName)
	if _, err := os.Stat(name); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (h *installerHandler) VolumeIdentifier() (string, error) {
	// Need to get the volume id from the ISO provided
	iso, err := os.Open(h.isoPath)
	if err != nil {
		return "", err
	}
	defer iso.Close()

	// Need a method to identify the ISO provided
	// The first 32768 bytes are unused by the ISO 9660 standard, typically for bootable media
	// This is where the data area begins and the 32 byte string representing the volume identifier
	// is offset 40 bytes into the primary volume descriptor
	volumeId := make([]byte, 32)
	_, err = iso.ReadAt(volumeId, 32808)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(volumeId)), nil
}

func GetFileLocation(filePath, isoPath string) (uint64, error) {
	myFile, err := GetFile(filePath, isoPath)
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to fetch ISO file %s", filePath)
	}
	defaultSectorSize := uint64(2 * 1024)
	offset := uint64(myFile.Location()) * defaultSectorSize
	return offset, nil
}

func GetFileSize(filePath, isoPath string) (uint64, error) {
	myFile, err := GetFile(filePath, isoPath)
	if err != nil {
		return 0, errors.Wrapf(err, "Failed to fetch ISO file %s", filePath)
	}
	return uint64(myFile.Size()), nil
}

func GetFile(filePath, isoPath string) (*iso9660.File, error) {
	d, err := diskfs.OpenWithMode(isoPath, diskfs.ReadOnly)
	if err != nil {
		return nil, err
	}

	fs, err := d.GetFilesystem(0)
	if err != nil {
		return nil, err
	}

	isoFile, err := fs.OpenFile(filePath, os.O_RDONLY)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to open file %s", filePath)
	}

	return isoFile.(*iso9660.File), nil
}
