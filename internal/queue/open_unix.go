//go:build unix

package queue

import "os"

func openAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) // #nosec G304 -- path is caller-controlled, matches this package's own documented contract
}

func openRead(path string) (*os.File, error) {
	return os.Open(path) // #nosec G304 -- path is caller-controlled, matches this package's own documented contract
}
