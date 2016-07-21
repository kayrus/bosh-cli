package pkg

import (
	gopath "path"

	bosherr "github.com/cloudfoundry/bosh-utils/errors"
	boshsys "github.com/cloudfoundry/bosh-utils/system"

	. "github.com/cloudfoundry/bosh-init/release/pkg/manifest"
	. "github.com/cloudfoundry/bosh-init/release/resource"
	"errors"
)

type DirReaderImpl struct {
	archiveFactory ArchiveFunc

	srcDirPath   string
	blobsDirPath string

	fs boshsys.FileSystem
}

func NewDirReaderImpl(
	archiveFactory ArchiveFunc,
	srcDirPath string,
	blobsDirPath string,
	fs boshsys.FileSystem,
) DirReaderImpl {
	return DirReaderImpl{
		archiveFactory: archiveFactory,
		srcDirPath:     srcDirPath,
		blobsDirPath:   blobsDirPath,
		fs:             fs,
	}
}

func (r DirReaderImpl) Read(path string) (*Package, error) {
	manifest, files, prepFiles, err := r.collectFiles(path)
	if err != nil {
		return nil, bosherr.WrapErrorf(err, "Collecting package files")
	}

	// Note that files do not include package's spec file,
	// but rather specify dependencies as additional chunks for the fingerprint.
	archive := r.archiveFactory(files, prepFiles, manifest.Dependencies)

	fp, err := archive.Fingerprint()
	if err != nil {
		return nil, err
	}

	resource := NewResource(manifest.Name, fp, archive)

	return NewPackage(resource, manifest.Dependencies), nil
}

func (r DirReaderImpl) collectFiles(path string) (Manifest, []File, []File, error) {
	var files, prepFiles []File

	specPath := gopath.Join(path, "spec")

	manifest, err := NewManifestFromPath(specPath, r.fs)
	if err != nil {
		return Manifest{}, nil, nil, err
	}

	packagingPath := gopath.Join(path, "packaging")
	files, err = r.checkAndFilterDir(packagingPath, path)
	if err != nil {
		return manifest, nil, nil, bosherr.Errorf(
			"Expected to find '%s' for package '%s'", packagingPath, manifest.Name)
	}

	prePackagingPath := gopath.Join(path, "pre_packaging")
	prepFiles, _ = r.checkAndFilterDir(prePackagingPath, path) //can proceed if there is no pre_packaging
	files = append(files, prepFiles...)

	filesByRelPath, err := r.applyFilesPattern(manifest)
	if err != nil {
		return manifest, nil, nil, err
	}

	excludedFiles, err := r.applyExcludedFilesPattern(manifest)
	if err != nil {
		return manifest, nil, nil, err
	}

	for _, excludedFile := range excludedFiles {
		delete(filesByRelPath, excludedFile.RelativePath)
	}

	for _, specialFileName := range []string{"packaging", "pre_packaging"} {
		if _, ok := filesByRelPath[specialFileName]; ok {
			errMsg := "Expected special '%s' file to not be included via 'files' key for package '%s'"
			return manifest, nil, nil, bosherr.Errorf(errMsg, specialFileName, manifest.Name)
		}
	}

	for _, file := range filesByRelPath {
		if !r.isDir(file.Path) {
			files = append(files, file)
		}
	}

	return manifest, files, prepFiles, nil
}

func (r DirReaderImpl) applyFilesPattern(manifest Manifest) (map[string]File, error) {
	filesByRelPath := map[string]File{}
	for _, glob := range manifest.Files {
		srcDirMatches, err := r.fs.RecursiveGlob(gopath.Join(r.srcDirPath, glob))
		if err != nil {
			return map[string]File{}, bosherr.WrapErrorf(err, "Listing package files in src")
		}

		for _, path := range srcDirMatches {
			file := NewFile(path, r.srcDirPath)
			if _, found := filesByRelPath[file.RelativePath]; !found {
				filesByRelPath[file.RelativePath] = file
			}
		}

		blobsDirMatches, err := r.fs.RecursiveGlob(gopath.Join(r.blobsDirPath, glob))
		if err != nil {
			return map[string]File{}, bosherr.WrapErrorf(err, "Listing package files in blobs")
		}

		for _, path := range blobsDirMatches {
			file := NewFile(path, r.blobsDirPath)
			if _, found := filesByRelPath[file.RelativePath]; !found {
				filesByRelPath[file.RelativePath] = file
			}
		}
	}

	return filesByRelPath, nil
}

func (r DirReaderImpl) applyExcludedFilesPattern(manifest Manifest) ([]File, error) {
	var excludedFiles []File
	for _, glob := range manifest.ExcludedFiles {
		srcDirMatches, err := r.fs.RecursiveGlob(gopath.Join(r.srcDirPath, glob))
		if err != nil {
			return []File{}, bosherr.WrapErrorf(err, "Listing package excluded files in src")
		}

		for _, path := range srcDirMatches {
			file := NewFile(path, r.srcDirPath)
			excludedFiles = append(excludedFiles, file)
		}

		blobsDirMatches, err := r.fs.RecursiveGlob(gopath.Join(r.blobsDirPath, glob))
		if err != nil {
			return []File{}, bosherr.WrapErrorf(err, "Listing package excluded files in blobs")
		}

		for _, path := range blobsDirMatches {
			file := NewFile(path, r.blobsDirPath)
			excludedFiles = append(excludedFiles, file)
		}
	}

	return excludedFiles, nil
}

func (r DirReaderImpl) checkAndFilterDir(packagePath, path string) ([]File, error) {
	var files []File
	if r.fs.FileExists(packagePath) {
		if !r.isDir(packagePath) {
			file := NewFile(packagePath, path)
			file.ExcludeMode = true
			files = append(files, file)
		}
		return files, nil
	}

	return []File{}, errors.New("File not found")
}

func (r DirReaderImpl) isDir(path string) bool {
	info, err := r.fs.Stat(path)
	if err != nil {
		return false;
	}
	return info.IsDir()
}
