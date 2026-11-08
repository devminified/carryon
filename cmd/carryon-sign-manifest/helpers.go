package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

// splitOnce splits s on the first occurrence of sep, returning ok =
// false if sep isn't present at all.
func splitOnce(s, sep string) (before, after string, ok bool) {
	i := strings.Index(s, sep)
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+len(sep):], true
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// assetFileName is the uploaded release asset's file name for one
// platform's bare binary — goos/arch-qualified (matching goreleaser's
// own archive name_template, {{.Binary}}_{{.Os}}_{{.Arch}}, minus the
// archive extension since this is the unpacked binary variant) rather
// than the local source path's own basename, which is the same
// "carryon" (or "carryon.exe") for every platform and would otherwise
// collide once every platform's asset is uploaded side by side in one
// release.
func assetFileName(path, goos, goarch string) string {
	name := fmt.Sprintf("carryon_%s_%s", goos, goarch)
	if ext := filepath.Ext(path); ext == ".exe" {
		name += ext
	}
	return name
}
