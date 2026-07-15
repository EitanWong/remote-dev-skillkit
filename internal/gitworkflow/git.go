package gitworkflow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type CommandEvidence struct {
	Argv     []string `json:"argv"`
	Dir      string   `json:"dir"`
	Stdout   string   `json:"stdout"`
	Stderr   string   `json:"stderr"`
	ExitCode int      `json:"exit_code"`
}

type Runner interface {
	Run(ctx context.Context, dir string, args ...string) (CommandEvidence, error)
}

type ExecRunner struct{}

type Repo struct {
	Root string `json:"root"`
}

func (ExecRunner) Run(ctx context.Context, dir string, args ...string) (CommandEvidence, error) {
	canonicalDir, err := canonicalGitDir(dir)
	if err != nil {
		return CommandEvidence{}, err
	}

	argv := append([]string{"git", "-C", canonicalDir}, args...)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = os.Environ()

	runErr := cmd.Run()
	evidence := CommandEvidence{
		Argv:     argv,
		Dir:      canonicalDir,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: commandExitCode(runErr),
	}
	if runErr != nil {
		trimmed := strings.TrimSpace(stderr.String())
		if trimmed != "" {
			return evidence, errors.New(trimmed)
		}
		if ctx.Err() != nil {
			return evidence, fmt.Errorf("git command failed without stderr: %w", ctx.Err())
		}
		return evidence, fmt.Errorf("git command failed without stderr")
	}
	return evidence, nil
}

func DiscoverRepo(ctx context.Context, r Runner, cwd string) (Repo, error) {
	canonicalCwd, err := canonicalGitDir(cwd)
	if err != nil {
		return Repo{}, err
	}

	evidence, err := r.Run(ctx, canonicalCwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repo{}, err
	}

	root := strings.TrimSpace(evidence.Stdout)
	if root == "" {
		return Repo{}, fmt.Errorf("git rev-parse --show-toplevel returned empty output")
	}
	canonicalRoot, err := canonicalGitDir(root)
	if err != nil {
		return Repo{}, err
	}
	return Repo{Root: canonicalRoot}, nil
}

func canonicalGitDir(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path must be a directory")
	}
	return filepath.Clean(canonical), nil
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
