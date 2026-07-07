package skillkit

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// RecommendedRdevCommand returns a stable command path suitable for long-lived
// MCP stdio config. It intentionally avoids go-run cache executables.
func RecommendedRdevCommand() string {
	if path, err := exec.LookPath(rdevExecutableName()); err == nil && strings.TrimSpace(path) != "" {
		if abs, err := filepath.Abs(path); err == nil {
			return abs
		}
		return path
	}
	if path := installedGoBinRdev(); path != "" {
		return path
	}
	if current, err := os.Executable(); err == nil && stableRdevExecutable(current) {
		return current
	}
	return "rdev"
}

func InstalledGoBinRdevForDiagnostics() string {
	return installedGoBinRdev()
}

func installedGoBinRdev() string {
	for _, dir := range goBinCandidates() {
		if path := executableFile(filepath.Join(dir, rdevExecutableName())); path != "" {
			return path
		}
	}
	return ""
}

func goBinCandidates() []string {
	candidates := []string{}
	if gobin := strings.TrimSpace(os.Getenv("GOBIN")); gobin != "" {
		candidates = append(candidates, gobin)
	}
	if gopath := strings.TrimSpace(os.Getenv("GOPATH")); gopath != "" {
		for _, part := range filepath.SplitList(gopath) {
			if strings.TrimSpace(part) != "" {
				candidates = append(candidates, filepath.Join(part, "bin"))
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		candidates = append(candidates, filepath.Join(home, "go", "bin"))
	}
	return dedupeStringValues(candidates)
}

func executableFile(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func stableRdevExecutable(path string) bool {
	base := filepath.Base(path)
	if base != rdevExecutableName() && base != "rdev" {
		return false
	}
	clean := filepath.ToSlash(path)
	return !strings.Contains(clean, "/go-build/") && !strings.Contains(clean, "/go run/")
}

func rdevExecutableName() string {
	if runtime.GOOS == "windows" {
		return "rdev.exe"
	}
	return "rdev"
}

func dedupeStringValues(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
