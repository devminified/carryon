package auth

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"

	"github.com/devminified/carryon/internal/config"
)

const (
	keyringService = "carryon"
	keyringAccount = "refresh_token"
)

// errNoCredential is returned by a backend's get when nothing is stored
// there. Distinct from a backend being unavailable (e.g. no OS keyring
// service running), which is any other error.
var errNoCredential = errors.New("no credential stored")

// backend is satisfied by the OS keyring and by the file fallback, so the
// store can try one and fall back to the other without caring which.
type backend interface {
	get() (string, error) // errNoCredential if empty, any other error if unavailable
	set(value string) error
	delete() error
}

type keyringBackend struct{}

func (keyringBackend) get() (string, error) {
	v, err := keyring.Get(keyringService, keyringAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", errNoCredential
	}
	return v, err
}

func (keyringBackend) set(value string) error {
	return keyring.Set(keyringService, keyringAccount, value)
}

func (keyringBackend) delete() error {
	err := keyring.Delete(keyringService, keyringAccount)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// fileBackend is the headless-Linux fallback (CO-103): a 0600 file at
// ~/.carryon/credentials, used only when the OS keyring is unavailable.
type fileBackend struct{ path string }

func newFileBackend() (fileBackend, error) {
	dir, err := config.Dir()
	if err != nil {
		return fileBackend{}, err
	}
	return fileBackend{path: filepath.Join(dir, "credentials")}, nil
}

func (f fileBackend) get() (string, error) {
	b, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", errNoCredential
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (f fileBackend) set(value string) error {
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(value), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func (f fileBackend) delete() error {
	err := os.Remove(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// store tries the OS keyring first; if it's unavailable (e.g. headless
// Linux with no Secret Service — CO-103's testing point), falls back to a
// file, warning once per process.
type store struct {
	primary  backend
	fallback backend
	warn     func(string)
	warned   bool
}

func newStore() (*store, error) {
	fb, err := newFileBackend()
	if err != nil {
		return nil, err
	}
	return &store{
		primary:  keyringBackend{},
		fallback: fb,
		warn:     func(msg string) { fmt.Fprintln(os.Stderr, "carryon:", msg) },
	}, nil
}

func (s *store) warnOnce(msg string) {
	if s.warned {
		return
	}
	s.warned = true
	if s.warn != nil {
		s.warn(msg)
	}
}

// get returns the stored refresh token, or errNoCredential if the user
// isn't logged in on either backend.
func (s *store) get() (string, error) {
	v, err := s.primary.get()
	switch {
	case err == nil:
		return v, nil
	case errors.Is(err, errNoCredential):
		// Keyring is available but empty — still worth checking the file,
		// in case a previous run had to fall back to it.
		return s.fallback.get()
	default:
		s.warnOnce("OS keychain unavailable, using ~/.carryon/credentials instead: " + err.Error())
		return s.fallback.get()
	}
}

// set stores the refresh token, preferring the keyring and falling back to
// the file on any keyring failure. On a successful keyring write, any
// stale file-fallback copy is removed so the two backends never disagree.
func (s *store) set(value string) error {
	if err := s.primary.set(value); err == nil {
		_ = s.fallback.delete()
		return nil
	} else {
		s.warnOnce("OS keychain unavailable, using ~/.carryon/credentials instead: " + err.Error())
	}
	return s.fallback.set(value)
}

// delete removes the refresh token from both backends. Best-effort: a
// backend that was never written to is not an error.
func (s *store) delete() error {
	err1 := s.primary.delete()
	err2 := s.fallback.delete()
	if err1 != nil {
		return err1
	}
	return err2
}
