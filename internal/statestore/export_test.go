package statestore

import (
	"os"
	"syscall"
)

// SetRenameFuncForTest replaces the package-level rename primitive used by
// writeAtomic. The returned function restores the previous value. Tests use
// this to exercise the EXDEV cross-device fallback path deterministically
// without needing a real cross-device mount.
func SetRenameFuncForTest(fn func(oldpath, newpath string) error) func() {
	prev := renameFunc
	renameFunc = fn
	return func() { renameFunc = prev }
}

// MakeEXDEVError returns a *os.LinkError that wraps syscall.EXDEV — the same
// shape os.Rename returns when crossing filesystems.
func MakeEXDEVError(oldpath, newpath string) error {
	return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: syscall.EXDEV}
}

// IsCrossDeviceErrForTest exposes isCrossDeviceErr to tests.
func IsCrossDeviceErrForTest(err error) bool { return isCrossDeviceErr(err) }

// SetWriteFnForTest replaces the write primitive. Returns a restore function.
func SetWriteFnForTest(fn func(f *os.File, data []byte) (int, error)) func() {
	prev := writeFn
	writeFn = fn
	return func() { writeFn = prev }
}

// SetSyncFnForTest replaces the sync primitive. Returns a restore function.
func SetSyncFnForTest(fn func(f *os.File) error) func() {
	prev := syncFn
	syncFn = fn
	return func() { syncFn = prev }
}

// SetCloseFnForTest replaces the close primitive. Returns a restore function.
func SetCloseFnForTest(fn func(f *os.File) error) func() {
	prev := closeFn
	closeFn = fn
	return func() { closeFn = prev }
}

// SetReadDirFnForTest replaces Delete's directory-read primitive. Returns a
// restore function.
func SetReadDirFnForTest(fn func(string) ([]os.DirEntry, error)) func() {
	prev := readDirFn
	readDirFn = fn
	return func() { readDirFn = prev }
}

// SetRemoveFnForTest replaces Delete's unlink primitive. Returns a restore
// function.
func SetRemoveFnForTest(fn func(string) error) func() {
	prev := removeFn
	removeFn = fn
	return func() { removeFn = prev }
}

// SetStatFnForTest replaces Read's post-read stat primitive. Returns a restore
// function.
func SetStatFnForTest(fn func(string) (os.FileInfo, error)) func() {
	prev := statFn
	statFn = fn
	return func() { statFn = prev }
}
