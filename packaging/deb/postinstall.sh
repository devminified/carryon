#!/bin/sh
# CO-308: dpkg runs this as root (via `apt install`/`dpkg -i`, almost
# always under sudo) — but `carryon install` writes to the real
# developer's own $HOME (~/.claude/settings.json, a user-level systemd
# unit), never root's. Running it unconditionally here would silently
# configure the wrong home directory entirely.
#
# sudo preserves the invoking user's login as $SUDO_USER (apt/dpkg's
# own postinst scripts inherit the environment sudo set up) — when
# present, re-run `carryon install` as that user instead of root. When
# it's genuinely absent (installed some other way, or as an actual
# root login), skip gracefully and tell the operator to run it
# themselves — never guess a $HOME to write into.
set -e

if [ -n "$SUDO_USER" ] && [ "$SUDO_USER" != "root" ]; then
  su - "$SUDO_USER" -c "carryon install" || {
    echo "carryon: 'carryon install' failed for user $SUDO_USER — run it yourself: carryon install" >&2
  }
else
  echo "carryon: run 'carryon install' as your own (non-root) user to finish setup." >&2
fi
