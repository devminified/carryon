// Package project handles git remote normalization and the project
// registry (CO-104): admins register a project by its git remote, and the
// capture agent later looks up which project a repo belongs to by the
// same canonical form (CO-108).
package project

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeRemote maps any common git remote URL spelling to one
// canonical, lowercase form: "<host>/<path>" — no scheme, no userinfo, no
// port, no ".git" suffix, no leading/trailing slash. Two different
// spellings of the same repo (SSH shorthand, HTTPS, explicit ssh://, with
// or without ".git", any port) normalize to the same string; a fork under
// a different owner does not, since the owner is part of path.
func NormalizeRemote(remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", fmt.Errorf("remote is empty")
	}

	var host, path string

	switch {
	case isWindowsDriveLetterPath(remote):
		// A Windows absolute path (C:\foo or C:/foo) would otherwise
		// fall into the bare "host:path" case below and "successfully"
		// normalize with a one-letter host — found via a real Windows
		// CI run where a local bare-path git remote (this project's own
		// test fixture style, standing in for GitHub without a network
		// call) normalized instead of being rejected as unrecognized.
		return "", fmt.Errorf("unrecognized remote format %q", remote)
	case strings.Contains(remote, "://"):
		// A real URL: https://host[:port]/path, ssh://[user@]host[:port]/path, git://host/path, …
		u, err := url.Parse(remote)
		if err != nil {
			return "", fmt.Errorf("parsing remote %q: %w", remote, err)
		}
		host = u.Hostname() // Hostname() already strips a port, unlike u.Host
		path = u.Path

	case strings.Contains(remote, "@") && strings.Contains(remote, ":"):
		// SCP-like syntax: [user@]host:path (e.g. git@github.com:org/repo.git).
		// This form has no port support — that's what ssh:// is for.
		at := strings.Index(remote, "@")
		rest := remote[at+1:]
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("could not parse SCP-like remote %q", remote)
		}
		host, path = parts[0], parts[1]

	case strings.Contains(remote, ":"):
		// Bare host:path with no explicit user@ (rare, but valid git syntax).
		parts := strings.SplitN(remote, ":", 2)
		host, path = parts[0], parts[1]

	default:
		return "", fmt.Errorf("unrecognized remote format %q", remote)
	}

	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return "", fmt.Errorf("remote %q has no host", remote)
	}

	path = strings.ToLower(strings.Trim(path, "/"))
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/") // a bare ".git" suffix could leave a trailing slash behind
	if path == "" {
		return "", fmt.Errorf("remote %q has no repository path", remote)
	}

	return host + "/" + path, nil
}

// isWindowsDriveLetterPath reports whether remote looks like a Windows
// absolute path — a single ASCII letter, a colon, then a path
// separator (C:\Users\... or C:/Users/...) — as opposed to a real
// scp-like remote, which never has a one-character hostname.
func isWindowsDriveLetterPath(remote string) bool {
	if len(remote) < 3 {
		return false
	}
	letter := remote[0]
	isLetter := (letter >= 'A' && letter <= 'Z') || (letter >= 'a' && letter <= 'z')
	return isLetter && remote[1] == ':' && (remote[2] == '/' || remote[2] == '\\')
}
