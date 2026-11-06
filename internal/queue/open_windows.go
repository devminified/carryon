//go:build windows

package queue

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// openAppend and openRead exist because Go's standard library never grants
// FILE_SHARE_DELETE on Windows (see syscall.Open in the Go source: its
// sharemode is hardcoded to FILE_SHARE_READ|FILE_SHARE_WRITE). Without it,
// a concurrent os.Rename of path — which Compact's steal-and-reset depends
// on — fails outright with "being used by another process" the instant
// any writer or reader has the file open. Not a rare race: this package's
// own concurrent-writer test hits it on nearly every trial under real
// load, which is how this was found (a Windows CI run of CO-107, not a
// guess).
//
// Both functions otherwise mirror golang.org/x/sys/windows.Open's own
// flag-to-access-rights translation for these two specific, narrow modes
// (O_WRONLY|O_CREATE|O_APPEND and O_RDONLY) rather than reimplementing
// CreateFile's full generality — in particular, using FILE_APPEND_DATA
// (not GENERIC_WRITE) is what gives atomic append-at-EOF semantics on
// Windows, the same guarantee a single O_APPEND write gets on POSIX.

func openAppend(path string) (*os.File, error) {
	namep, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}

	const access = windows.FILE_APPEND_DATA
	const sharemode = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE
	const createmode = windows.OPEN_ALWAYS // create if missing, open if it exists — O_CREATE without O_EXCL/O_TRUNC

	h, err := windows.CreateFile(namep, access, sharemode, nil, createmode, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

func openRead(path string) (*os.File, error) {
	namep, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}

	const sharemode = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

	h, err := windows.CreateFile(namep, windows.GENERIC_READ, sharemode, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}
