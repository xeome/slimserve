package storage

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"slimserve/internal/logger"
	"slimserve/internal/security"
)

type FileInfo struct {
	name    string
	size    int64
	modTime time.Time
	isDir   bool
}

func (f *FileInfo) Name() string       { return f.name }
func (f *FileInfo) Size() int64        { return f.size }
func (f *FileInfo) ModTime() time.Time { return f.modTime }
func (f *FileInfo) IsDir() bool        { return f.isDir }
func (f *FileInfo) Sys() interface{}   { return nil }
func (f *FileInfo) Mode() fs.FileMode {
	if f.isDir {
		return fs.ModeDir | 0755
	}
	return 0644
}

type DirEntry struct {
	name  string
	isDir bool
	info  *FileInfo
}

func (d *DirEntry) Name() string { return d.name }
func (d *DirEntry) IsDir() bool  { return d.isDir }
func (d *DirEntry) Type() fs.FileMode {
	if d.isDir {
		return fs.ModeDir
	}
	return 0
}
func (d *DirEntry) Info() (fs.FileInfo, error) { return d.info, nil }

type Backend interface {
	Path() string
	Stat(ctx context.Context, name string) (*FileInfo, error)
	ReadDir(ctx context.Context, name string) ([]*DirEntry, error)
	Open(ctx context.Context, name string) (io.ReadSeekCloser, error)
	IsIgnored(ctx context.Context, path string) (bool, error)
	Close() error
}

// Uploader is an interface for backends that support writing
type Uploader interface {
	Backend
	Put(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
}

type LocalBackend struct {
	root           *security.RootFS
	path           string
	ignorePatterns []string
}

func NewLocalBackend(root *security.RootFS, ignorePatterns []string) *LocalBackend {
	return &LocalBackend{
		root:           root,
		path:           root.Path(),
		ignorePatterns: ignorePatterns,
	}
}

func (l *LocalBackend) Path() string {
	return l.path
}

func (l *LocalBackend) Stat(ctx context.Context, name string) (*FileInfo, error) {
	info, err := l.root.Stat(name)
	if err != nil {
		return nil, err
	}
	return &FileInfo{
		name:    info.Name(),
		size:    info.Size(),
		modTime: info.ModTime(),
		isDir:   info.IsDir(),
	}, nil
}

func (l *LocalBackend) ReadDir(ctx context.Context, name string) ([]*DirEntry, error) {
	entries, err := l.root.ReadDir(name)
	if err != nil {
		return nil, err
	}
	result := make([]*DirEntry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			logger.Log.Warn().Err(err).Str("entry", e.Name()).Msg("Failed to get entry info, skipping")
			continue
		}
		result = append(result, &DirEntry{
			name:  e.Name(),
			isDir: e.IsDir(),
			info: &FileInfo{
				name:    info.Name(),
				size:    info.Size(),
				modTime: info.ModTime(),
				isDir:   info.IsDir(),
			},
		})
	}
	return result, nil
}

func (l *LocalBackend) Open(ctx context.Context, name string) (io.ReadSeekCloser, error) {
	return l.root.Open(name)
}

func (l *LocalBackend) IsIgnored(ctx context.Context, relPath string) (bool, error) {
	if filepath.Base(relPath) == ".slimserveignore" {
		return true, nil
	}
	for _, pattern := range l.ignorePatterns {
		matched, err := filepath.Match(pattern, relPath)
		if err != nil {
			continue
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func (l *LocalBackend) Close() error {
	return l.root.Close()
}

type bytesReadSeekCloser struct {
	*io.SectionReader
	data []byte
}

func (b *bytesReadSeekCloser) Close() error { return nil }

func (s *S3Backend) Stat(ctx context.Context, key string) (*FileInfo, error) {
	obj, err := s.StatObject(ctx, key)
	if err != nil {
		return nil, err
	}
	if obj == nil {
		return nil, os.ErrNotExist
	}
	return &FileInfo{
		name:    key,
		size:    obj.Size,
		modTime: obj.LastModified,
		isDir:   obj.IsDir,
	}, nil
}

func (s *S3Backend) ReadDir(ctx context.Context, prefix string) ([]*DirEntry, error) {
	objects, err := s.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	result := make([]*DirEntry, 0, len(objects))
	for _, obj := range objects {
		result = append(result, &DirEntry{
			name:  obj.Key,
			isDir: obj.IsDir,
			info: &FileInfo{
				name:    obj.Key,
				size:    obj.Size,
				modTime: obj.LastModified,
				isDir:   obj.IsDir,
			},
		})
	}
	return result, nil
}

func (s *S3Backend) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	data, err := s.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return &bytesReadSeekCloser{
		SectionReader: io.NewSectionReader(bytes.NewReader(data), 0, int64(len(data))),
		data:          data,
	}, nil
}
