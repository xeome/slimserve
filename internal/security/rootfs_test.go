//go:build go1.24

package security

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootFS(t *testing.T) {
	t.Run("valid directory", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "test-rootfs-valid")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		rfs, err := NewRootFS(tempDir)
		require.NoError(t, err)
		require.NotNil(t, rfs)
		assert.Equal(t, tempDir, rfs.Path())
		err = rfs.Close()
		assert.NoError(t, err)
	})

	t.Run("non-existent directory", func(t *testing.T) {
		nonExistentDir := filepath.Join(os.TempDir(), "non-existent-dir-for-rootfs")
		rfs, err := NewRootFS(nonExistentDir)
		require.Error(t, err)
		assert.True(t, errors.Is(err, fs.ErrNotExist) || strings.Contains(err.Error(), "no such file or directory"), "expected ErrNotExist or similar")
		assert.Nil(t, rfs)
	})

	t.Run("path is a file", func(t *testing.T) {
		tempFile, err := os.CreateTemp("", "test-rootfs-file")
		require.NoError(t, err)
		defer os.Remove(tempFile.Name())
		tempFile.Close()

		rfs, err := NewRootFS(tempFile.Name())
		require.Error(t, err)
		// Error message varies by OS for opening a file as a root.
		// On Linux, it's "not a directory". On Windows, it can be "The directory name is invalid."
		// os.OpenRoot internally uses a system call that might return different errors.
		// We check for a non-nil error.
		assert.Nil(t, rfs)
	})
}

func setupTestFS(t *testing.T, structure map[string]string) (*RootFS, string, func()) {
	t.Helper()
	baseDir, err := os.MkdirTemp("", "test-rootfs-ops")
	require.NoError(t, err)

	for path, content := range structure {
		fullPath := filepath.Join(baseDir, path)
		if content == "DIR" {
			err = os.MkdirAll(fullPath, 0755)
			require.NoError(t, err)
		} else {
			err = os.MkdirAll(filepath.Dir(fullPath), 0755)
			require.NoError(t, err)
			err = os.WriteFile(fullPath, []byte(content), 0644)
			require.NoError(t, err)
		}
	}

	rfs, err := NewRootFS(baseDir)
	require.NoError(t, err)

	cleanup := func() {
		rfs.Close()
		os.RemoveAll(baseDir)
	}
	return rfs, baseDir, cleanup
}

func TestRootFS_Open(t *testing.T) {
	t.Run("successfully open valid existing file", func(t *testing.T) {
		rfs, _, cleanup := setupTestFS(t, map[string]string{"file.txt": "content"})
		defer cleanup()

		f, err := rfs.Open("file.txt")
		require.NoError(t, err)
		require.NotNil(t, f)
		_ = f.Close()

		fClean, errClean := rfs.Open("./file.txt")
		require.NoError(t, errClean)
		require.NotNil(t, fClean)
		_ = fClean.Close()
	})

	t.Run("attempt to open non-existent file", func(t *testing.T) {
		rfs, _, cleanup := setupTestFS(t, map[string]string{})
		defer cleanup()

		f, err := rfs.Open("nonexistent.txt")
		require.Error(t, err)
		assert.True(t, errors.Is(err, fs.ErrNotExist))
		assert.Nil(t, f)
	})

	t.Run("attempt path traversal using ..", func(t *testing.T) {
		// Create a file outside the intended root
		outerDir, err := os.MkdirTemp("", "outer-root")
		require.NoError(t, err)
		defer os.RemoveAll(outerDir)

		err = os.WriteFile(filepath.Join(outerDir, "outerfile.txt"), []byte("outer"), 0644)
		require.NoError(t, err)

		innerDir := filepath.Join(outerDir, "inner")
		err = os.Mkdir(innerDir, 0755)
		require.NoError(t, err)

		rfs, err := NewRootFS(innerDir)
		require.NoError(t, err)
		defer rfs.Close()

		// Attempt to open ../outerfile.txt
		// os.Root prevents this; the path is interpreted as relative to the root.
		// So, it looks for "outerfile.txt" *inside* "innerDir", which doesn't exist.
		f, err := rfs.Open("../outerfile.txt")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path escapes from parent")
		assert.Nil(t, f)

		f2, err2 := rfs.Open(filepath.FromSlash("../outerfile.txt"))
		require.Error(t, err2)
		assert.Contains(t, err2.Error(), "path escapes from parent")
		assert.Nil(t, f2)
	})

	t.Run("attempt path traversal using absolute paths", func(t *testing.T) {
		// Create a file to try to access absolutely
		absTargetFile, err := os.CreateTemp("", "abs-target")
		require.NoError(t, err)
		absTargetFilePath := absTargetFile.Name()
		absTargetFile.Close()
		defer os.Remove(absTargetFilePath)

		rfs, _, cleanup := setupTestFS(t, map[string]string{})
		defer cleanup()

		// os.Root treats absolute paths as relative to its root, so this
		// will likely fail with ErrNotExist unless the absolute path happens
		// to map to something *inside* the RootFS, which is highly unlikely
		// and not the traversal itself.
		f, err := rfs.Open(absTargetFilePath)
		require.Error(t, err)
		// The error should be ErrNotExist because it's looking for a path like
		// /tmp/test-rootfs-opsXYZ/tmp/abs-targetXYZ
		assert.Contains(t, err.Error(), "path escapes from parent")
		assert.Nil(t, f)
	})

	t.Run("handling of clean vs unclean paths", func(t *testing.T) {
		rfs, _, cleanup := setupTestFS(t, map[string]string{"file.txt": "content", "subdir/another.txt": "subcontent"})
		defer cleanup()

		validCleanCases := []string{
			"file.txt",
			"./file.txt",
			"subdir/../file.txt",
			"./subdir/../file.txt",
		}

		for _, path := range validCleanCases {
			t.Run(path, func(t *testing.T) {
				f, err := rfs.Open(path)
				require.NoError(t, err, "Path: %s", path)
				require.NotNil(t, f)
				st, err := f.Stat()
				require.NoError(t, err)
				assert.Equal(t, "file.txt", st.Name())
				_ = f.Close()
			})
		}

		invalidPathCases := []string{
			"/file.txt",  // os.Root treats leading / as escape
			"//file.txt", // os.Root treats leading // as escape
		}
		for _, path := range invalidPathCases {
			t.Run(path, func(t *testing.T) {
				_, err := rfs.Open(path)
				require.Error(t, err, "Path: %s", path)
				assert.Contains(t, err.Error(), "path escapes from parent", "Path: %s", path)
			})
		}

		// Test with a path that cleans to a file in a subdirectory
		f, err := rfs.Open("subdir/./another.txt")
		require.NoError(t, err)
		require.NotNil(t, f)
		st, err := f.Stat()
		require.NoError(t, err)
		assert.Equal(t, "another.txt", st.Name())
		_ = f.Close()
	})
}

func TestRootFS_OpenFile(t *testing.T) {
	rfs, baseDir, cleanup := setupTestFS(t, map[string]string{"readonly.txt": "read"})
	defer cleanup()

	// Test opening an existing file for reading
	f, err := rfs.OpenFile("readonly.txt", os.O_RDONLY, 0)
	require.NoError(t, err)
	require.NotNil(t, f)
	f.Close()

	// Test creating a new file
	newFilePath := "newfile.txt"
	fCreate, err := rfs.OpenFile(newFilePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	require.NoError(t, err)
	require.NotNil(t, fCreate)
	_, err = fCreate.WriteString("hello")
	assert.NoError(t, err)
	fCreate.Close()

	_, err = os.Stat(filepath.Join(baseDir, newFilePath))
	assert.NoError(t, err, "File should exist on underlying FS")

	// Test path traversal attempt (should fail with fs.ErrInvalid)
	_, err = rfs.OpenFile("../somefile", os.O_RDONLY, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes from parent")
}

func TestRootFS_Create(t *testing.T) {
	rfs, baseDir, cleanup := setupTestFS(t, map[string]string{})
	defer cleanup()

	newFilePath := "created.txt"
	f, err := rfs.Create(newFilePath)
	require.NoError(t, err)
	require.NotNil(t, f)
	_, err = f.WriteString("created content")
	assert.NoError(t, err)
	f.Close()

	_, err = os.Stat(filepath.Join(baseDir, newFilePath))
	assert.NoError(t, err, "File should exist on underlying FS")

	// Test path traversal attempt
	_, err = rfs.Create("../traversal.txt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes from parent")
}

func TestRootFS_Stat_Lstat(t *testing.T) {
	rfs, baseDir, cleanup := setupTestFS(t, map[string]string{"file.txt": "content", "dirA": "DIR"})
	defer cleanup()

	// Stat existing file
	fi, err := rfs.Stat("file.txt")
	require.NoError(t, err)
	assert.Equal(t, "file.txt", fi.Name())
	assert.False(t, fi.IsDir())

	// Lstat existing file (should be same as Stat for regular files)
	lfi, err := rfs.Lstat("file.txt")
	require.NoError(t, err)
	assert.Equal(t, "file.txt", lfi.Name())

	// Stat existing directory
	dirFi, err := rfs.Stat("dirA")
	require.NoError(t, err)
	assert.Equal(t, "dirA", dirFi.Name())
	assert.True(t, dirFi.IsDir())

	// Stat non-existent
	_, err = rfs.Stat("nonexistent.txt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))

	// Lstat non-existent
	_, err = rfs.Lstat("nonexistent.txt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))

	// Symlink test (if supported and makes sense for os.Root context)
	// os.Root's Lstat will operate on the symlink itself within the rooted context.
	// For this test, we'll create a symlink within the test directory.
	if runtime.GOOS != "windows" { // Symlinks are trickier on Windows for non-admins
		targetPath := filepath.Join(baseDir, "file.txt")
		linkPathInFS := "symlink.txt" // relative to rfs root
		linkPathActual := filepath.Join(baseDir, linkPathInFS)
		err = os.Symlink(targetPath, linkPathActual) // target needs to be relative or absolute
		// To make it work within the chroot-like environment of RootFS,
		// the symlink target should be relative to the link's location *within* the root.
		// So, if link is baseDir/symlink.txt, target should be "file.txt"
		os.Remove(linkPathActual) // remove previous attempt
		err = os.Symlink("file.txt", linkPathActual)
		require.NoError(t, err, "Failed to create symlink")

		// Stat on symlink with os.Root: Name() seems to return link name, not target.
		statSymFi, err := rfs.Stat(linkPathInFS)
		require.NoError(t, err)
		assert.Equal(t, linkPathInFS, statSymFi.Name()) // Name is of the link itself
		assert.False(t, statSymFi.IsDir())              // Target is not a dir
		// Mode should reflect the target if followed, but os.Root.Stat might not fully "follow" for FileInfo contents beyond error checks
		// We rely on Lstat to explicitly get symlink properties.

		// Lstat on symlink does not follow
		lstatSymFi, err := rfs.Lstat(linkPathInFS)
		require.NoError(t, err)
		assert.Equal(t, linkPathInFS, lstatSymFi.Name())
		assert.NotZero(t, lstatSymFi.Mode()&fs.ModeSymlink)
	}
}

func TestRootFS_ReadDir(t *testing.T) {
	structure := map[string]string{
		"dirA/file1.txt":     "a1",
		"dirA/file2.txt":     "a2",
		"dirA/subdir/s1.txt": "s1",
		"file_top.txt":       "top",
	}
	rfs, _, cleanup := setupTestFS(t, structure)
	defer cleanup()

	// Read existing directory
	entries, err := rfs.ReadDir("dirA")
	require.NoError(t, err)
	assert.Len(t, entries, 3) // file1.txt, file2.txt, subdir

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name()] = true
	}
	assert.True(t, names["file1.txt"])
	assert.True(t, names["file2.txt"])
	assert.True(t, names["subdir"])

	// ReadDir on root
	rootEntries, err := rfs.ReadDir(".")
	require.NoError(t, err)
	assert.Len(t, rootEntries, 2) // dirA, file_top.txt

	// Read non-existent directory
	_, err = rfs.ReadDir("nonexistentdir")
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))

	// ReadDir on a file
	_, err = rfs.ReadDir("file_top.txt")
	require.Error(t, err) // "not a directory" or similar
	// Error type can vary based on OS. On Linux: syscall.ENOTDIR
}

func TestRootFS_Mkdir(t *testing.T) {
	rfs, baseDir, cleanup := setupTestFS(t, map[string]string{})
	defer cleanup()

	newDir := "newdir"
	err := rfs.Mkdir(newDir, 0755)
	require.NoError(t, err)
	fi, err := os.Stat(filepath.Join(baseDir, newDir))
	require.NoError(t, err)
	assert.True(t, fi.IsDir())

	// Mkdir existing
	err = rfs.Mkdir(newDir, 0755)
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrExist))

	// Mkdir with traversal (should fail at parent path resolution)
	err = rfs.Mkdir("../anotherdir", 0755)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes from parent")
}

func TestRootFS_Remove(t *testing.T) {
	rfs, baseDir, cleanup := setupTestFS(t, map[string]string{"filetoremove.txt": "delete me", "dirtoremove/file.txt": "in dir"})
	defer cleanup()

	// Remove file
	err := rfs.Remove("filetoremove.txt")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(baseDir, "filetoremove.txt"))
	assert.Error(t, err) // Should be gone

	// Remove directory (requires it to be empty for os.Remove, RootFS.Remove calls os.Remove directly)
	// So, we need to remove the file inside first.
	err = rfs.Remove("dirtoremove/file.txt")
	require.NoError(t, err)
	err = rfs.Remove("dirtoremove")
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(baseDir, "dirtoremove"))
	assert.Error(t, err) // Should be gone

	// Remove non-existent
	err = rfs.Remove("nonexistent.txt")
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

func TestRootFS_OpenRoot(t *testing.T) {
	rfsMaster, baseDir, cleanupMaster := setupTestFS(t, map[string]string{
		"sub/file.txt":          "content",
		"sub/nested/deeper.txt": "deep content",
		"another.txt":           "top level",
	})
	defer cleanupMaster()

	// Open existing subdirectory as root
	subRFS, err := rfsMaster.OpenRoot("sub")
	require.NoError(t, err)
	require.NotNil(t, subRFS)
	defer subRFS.Close()

	assert.Equal(t, filepath.Join(baseDir, "sub"), subRFS.Path(), "Path for subRFS should be original_base/sub")

	// Try to open file within the new sub-root
	f, err := subRFS.Open("file.txt")
	require.NoError(t, err)
	f.Close()

	// Try to access file outside the sub-root (should fail)
	_, err = subRFS.Open("../another.txt") // Looks for "sub/../another.txt" relative to subRFS's root (baseDir/sub)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path escapes from parent")

	// Open a nested root
	nestedRFS, err := subRFS.OpenRoot("nested")
	require.NoError(t, err)
	defer nestedRFS.Close()
	assert.Equal(t, filepath.Join(baseDir, "sub", "nested"), nestedRFS.Path())

	nf, err := nestedRFS.Open("deeper.txt")
	require.NoError(t, err)
	nf.Close()

	// Open non-existent directory as root
	_, err = rfsMaster.OpenRoot("nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, fs.ErrNotExist))

	// Open a file as root (should fail)
	_, err = rfsMaster.OpenRoot("another.txt")
	require.Error(t, err) // "not a directory" or similar
}

func TestRootFS_Path(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-path")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	rfs, err := NewRootFS(tempDir)
	require.NoError(t, err)
	defer rfs.Close()

	assert.Equal(t, tempDir, rfs.Path())
}
