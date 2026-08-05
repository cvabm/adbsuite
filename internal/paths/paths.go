package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Resolve finds a tool binary: custom path first, then bundled bin/, then PATH name.
func Resolve(custom, bundledRel, fallbackName string) string {
	if custom = strings.TrimSpace(custom); custom != "" {
		if st, err := os.Stat(custom); err == nil && !st.IsDir() {
			return custom
		}
	}
	for _, base := range candidateBases() {
		p := filepath.Join(base, bundledRel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return fallbackName
}

func AdbPath(custom string) string {
	name := "adb"
	if runtime.GOOS == "windows" {
		name = "adb.exe"
	}
	return Resolve(custom, filepath.Join("bin", "platform-tools", name), name)
}

func ScrcpyPath(custom string) string {
	name := "scrcpy"
	if runtime.GOOS == "windows" {
		name = "scrcpy.exe"
	}
	return Resolve(custom, filepath.Join("bin", "scrcpy", name), name)
}

func candidateBases() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if !seen[abs] {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	if exe, err := os.Executable(); err == nil {
		add(filepath.Dir(exe))
		// go run / dev: often under build/bin
		add(filepath.Join(filepath.Dir(exe), "..", ".."))
	}
	if wd, err := os.Getwd(); err == nil {
		add(wd)
		// frontend cwd during wails dev
		add(filepath.Join(wd, ".."))
	}
	return out
}
