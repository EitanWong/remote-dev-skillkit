package claudecodeadapter

import (
	"strings"
	"testing"
)

func TestExecuteAppliesRuntimeEnvironmentOnlyToClaudeChild(t *testing.T) {
	repo := initGitRepo(t)
	fakeClaudeCode := writeFakeClaudeCodeProgram(t, `package main

import (
	"fmt"
	"os"
)

func main() {
	if os.Getenv("RDEV_TEST_CLAUDE_ENV") != "configured" {
		panic("missing runtime environment")
	}
	fmt.Println("runtime environment applied")
}
`)
	result, err := Execute(Spec{
		WorkspaceRoot:      repo,
		Prompt:             "verify runtime environment",
		ClaudeCodeCommand:  "go",
		ClaudeCodeArgs:     []string{"run", fakeClaudeCode},
		Environment:        map[string]string{"RDEV_TEST_CLAUDE_ENV": "configured"},
		MaxDurationSeconds: 30,
		MaxOutputBytes:     64 * 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.ClaudeCodeCommand.Stdout, "runtime environment applied") {
		t.Fatalf("runtime environment was not visible to Claude child: %#v", result.ClaudeCodeCommand)
	}
	if strings.Contains(result.ArtifactContent(), "RDEV_TEST_CLAUDE_ENV") {
		t.Fatalf("artifact exposed runtime environment metadata: %s", result.ArtifactContent())
	}
}
