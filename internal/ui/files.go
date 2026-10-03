package ui

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// downloadsDir resolves where exported files go: $K9DESK_DOWNLOAD_DIR,
// XDG_DOWNLOAD_DIR (user-dirs.dirs), ~/Downloads, then home.
func downloadsDir() string {
	if d := os.Getenv("K9DESK_DOWNLOAD_DIR"); d != "" {
		_ = os.MkdirAll(d, 0o755)
		if abs, err := filepath.Abs(d); err == nil {
			return abs
		}
		return d
	}
	home, _ := os.UserHomeDir()
	if f, err := os.Open(filepath.Join(home, ".config", "user-dirs.dirs")); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if v, ok := strings.CutPrefix(l, "XDG_DOWNLOAD_DIR="); ok {
				v = strings.ReplaceAll(strings.Trim(v, `"`), "$HOME", home)
				if st, err := os.Stat(v); err == nil && st.IsDir() {
					return v
				}
			}
		}
	}
	if d := filepath.Join(home, "Downloads"); dirExists(d) {
		return d
	}
	return home
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
