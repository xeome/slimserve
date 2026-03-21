package storage

import (
	"io/fs"
	"testing"
	"time"
)

func TestDirEntry_Type(t *testing.T) {
	fileInfo := &FileInfo{
		name:    "test.txt",
		size:    1024,
		modTime: time.Now(),
		isDir:   false,
	}
	dirInfo := &FileInfo{
		name:    "testdir",
		size:    0,
		modTime: time.Now(),
		isDir:   true,
	}

	tests := []struct {
		name     string
		entry    *DirEntry
		expected fs.FileMode
	}{
		{
			name: "file returns zero mode",
			entry: &DirEntry{
				name:  "test.txt",
				isDir: false,
				info:  fileInfo,
			},
			expected: 0,
		},
		{
			name: "directory returns ModeDir",
			entry: &DirEntry{
				name:  "testdir",
				isDir: true,
				info:  dirInfo,
			},
			expected: fs.ModeDir,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.entry.Type()
			if got != tt.expected {
				t.Errorf("DirEntry.Type() = %v, want %v", got, tt.expected)
			}
		})
	}
}
