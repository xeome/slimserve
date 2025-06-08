//go:build go1.24

package security

import (
	"io/fs"
	"os"
)

// RootFS provides a traversal-resistant filesystem interface using Go 1.24's os.Root
type RootFS struct {
	root *os.Root
	path string // original path for legacy compatibility
}

// NewRootFS creates a new RootFS instance for the given directory
func NewRootFS(dir string) (*RootFS, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &RootFS{
		root: root,
		path: dir,
	}, nil
}

// Close closes the underlying root
func (r *RootFS) Close() error {
	return r.root.Close()
}

// Open opens a file relative to the root directory in a traversal-resistant manner
func (r *RootFS) Open(name string) (*os.File, error) {
	return r.root.Open(name)
}

// OpenFile opens a file with specified flags and permissions
func (r *RootFS) OpenFile(name string, flag int, perm fs.FileMode) (*os.File, error) {
	return r.root.OpenFile(name, flag, perm)
}

// Create creates a new file relative to the root
func (r *RootFS) Create(name string) (*os.File, error) {
	return r.root.Create(name)
}

// Stat returns file information for the named file
func (r *RootFS) Stat(name string) (fs.FileInfo, error) {
	return r.root.Stat(name)
}

// Lstat returns file information for the named file without following symlinks
func (r *RootFS) Lstat(name string) (fs.FileInfo, error) {
	return r.root.Lstat(name)
}

// ReadDir reads the directory and returns directory entries
func (r *RootFS) ReadDir(name string) ([]fs.DirEntry, error) {
	f, err := r.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return f.ReadDir(-1)
}

// Mkdir creates a directory
func (r *RootFS) Mkdir(name string, perm fs.FileMode) error {
	return r.root.Mkdir(name, perm)
}

// Remove removes a file or directory
func (r *RootFS) Remove(name string) error {
	return r.root.Remove(name)
}

// OpenRoot opens a subdirectory as a new RootFS
func (r *RootFS) OpenRoot(name string) (*RootFS, error) {
	subRoot, err := r.root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	return &RootFS{
		root: subRoot,
		path: r.path + "/" + name, // for legacy compatibility
	}, nil
}

// Path returns the original directory path (for compatibility)
func (r *RootFS) Path() string {
	return r.path
}
