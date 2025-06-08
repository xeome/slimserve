//go:build !go1.24

package security

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// RootFS provides a filesystem interface with manual path validation for Go < 1.24
type RootFS struct {
	path string
}

// NewRootFS creates a new RootFS instance for the given directory
func NewRootFS(dir string) (*RootFS, error) {
	// Validate that the directory exists
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("path is not a directory")
	}

	// Clean the path
	cleanPath, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	return &RootFS{
		path: cleanPath,
	}, nil
}

// Close is a no-op for legacy implementation
func (r *RootFS) Close() error {
	return nil
}

// validatePath performs manual path traversal protection
func (r *RootFS) validatePath(name string) (string, error) {
	// Basic path traversal protection - deny any path containing ".."
	if strings.Contains(name, "..") {
		return "", fs.ErrPermission
	}

	// Clean and join with root directory
	cleanPath := filepath.Clean(name)
	if cleanPath == "." {
		cleanPath = ""
	}

	// Remove leading slash to make it relative
	relPath := strings.TrimPrefix(cleanPath, "/")
	fullPath := filepath.Join(r.path, relPath)

	// Additional security check - ensure resolved path is within allowed root
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}

	rootPath := filepath.Clean(r.path)
	if !strings.HasSuffix(rootPath, string(filepath.Separator)) {
		rootPath += string(filepath.Separator)
	}

	if !strings.HasPrefix(absPath+string(filepath.Separator), rootPath) && absPath != filepath.Clean(r.path) {
		return "", fs.ErrPermission // Path is outside allowed root
	}

	return fullPath, nil
}

// Open opens a file relative to the root directory with manual validation
func (r *RootFS) Open(name string) (*os.File, error) {
	fullPath, err := r.validatePath(name)
	if err != nil {
		return nil, err
	}
	return os.Open(fullPath)
}

// OpenFile opens a file with specified flags and permissions
func (r *RootFS) OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error) {
	fullPath, err := r.validatePath(name)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(fullPath, flag, perm)
}

// Create creates a new file relative to the root
func (r *RootFS) Create(name string) (*os.File, error) {
	fullPath, err := r.validatePath(name)
	if err != nil {
		return nil, err
	}
	return os.Create(fullPath)
}

// Stat returns file information for the named file
func (r *RootFS) Stat(name string) (fs.FileInfo, error) {
	fullPath, err := r.validatePath(name)
	if err != nil {
		return nil, err
	}
	return os.Stat(fullPath)
}

// Lstat returns file information for the named file without following symlinks
func (r *RootFS) Lstat(name string) (fs.FileInfo, error) {
	fullPath, err := r.validatePath(name)
	if err != nil {
		return nil, err
	}
	return os.Lstat(fullPath)
}

// ReadDir reads the directory and returns directory entries
func (r *RootFS) ReadDir(name string) ([]fs.DirEntry, error) {
	fullPath, err := r.validatePath(name)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(fullPath)
}

// Mkdir creates a directory
func (r *RootFS) Mkdir(name string, perm fs.FileMode) error {
	fullPath, err := r.validatePath(name)
	if err != nil {
		return err
	}
	return os.Mkdir(fullPath, perm)
}

// Remove removes a file or directory
func (r *RootFS) Remove(name string) error {
	fullPath, err := r.validatePath(name)
	if err != nil {
		return err
	}
	return os.Remove(fullPath)
}

// OpenRoot opens a subdirectory as a new RootFS
func (r *RootFS) OpenRoot(name string) (*RootFS, error) {
	fullPath, err := r.validatePath(name)
	if err != nil {
		return nil, err
	}
	return NewRootFS(fullPath)
}

// Path returns the root directory path
func (r *RootFS) Path() string {
	return r.path
}
