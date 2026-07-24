package fileadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteProjectContextDescribeSearchSliceSymbolsAndDiagnostics(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is required for project-context describe test")
	}
	root := t.TempDir()
	mustWriteContextFile(t, filepath.Join(root, "go.mod"), "module example.test/project\n\ngo 1.25\n")
	mustWriteContextFile(t, filepath.Join(root, "AGENTS.md"), "Use focused tests.\n")
	mustWriteContextFile(t, filepath.Join(root, "README.md"), "# Project\n\nRemote context test.\n")
	mustWriteContextFile(t, filepath.Join(root, "internal", "greeting.go"), "package internal\n\n// Greeting returns the greeting.\nfunc Greeting() string { return \"hello\" }\n")
	mustWriteContextFile(t, filepath.Join(root, "internal", "broken.go"), "package internal\n\nfunc Broken( {\n")
	contextGit(t, root, "init")
	contextGit(t, root, "config", "user.email", "rdev@example.test")
	contextGit(t, root, "config", "user.name", "Remote Dev")
	contextGit(t, root, "add", ".")
	contextGit(t, root, "commit", "-m", "initial")
	mustWriteContextFile(t, filepath.Join(root, "README.md"), "# Project\n\nRemote context test, now dirty.\n")

	describe, err := Execute(Spec{WorkspaceRoot: root, Action: "describe", AllowGitInspection: true})
	if err != nil {
		t.Fatal(err)
	}
	if describe.ProjectContext == nil || describe.ProjectContext.Description == nil {
		t.Fatalf("describe must return a project-context description: %#v", describe)
	}
	description := describe.ProjectContext.Description
	if description.Git.TopLevel != root || description.Git.HeadSHA == "" || !description.Git.Dirty {
		t.Fatalf("describe must include dirty Git identity: %#v", description.Git)
	}
	if len(description.Git.Changes) != 1 || description.Git.Changes[0].Path != "README.md" {
		t.Fatalf("describe must preserve a bounded dirty-file summary: %#v", description.Git)
	}
	if !hasContextFile(description.DependencyManifests, "go.mod") || !hasContextFile(description.InstructionFiles, "AGENTS.md") {
		t.Fatalf("describe missed manifests or instructions: %#v", description)
	}
	if !containsContextString(description.Languages, "Go") || !containsContextString(description.BuildSystems, "go") {
		t.Fatalf("describe missed Go detection: %#v", description)
	}

	search, err := Execute(Spec{
		WorkspaceRoot: root,
		Action:        "search",
		Path:          "internal",
		Query:         "Greeting",
		Glob:          "*.go",
		MaxResults:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if search.ProjectContext == nil || len(search.ProjectContext.Matches) != 1 || search.ProjectContext.Matches[0].Path != "internal/greeting.go" {
		t.Fatalf("search did not return bounded matching source context: %#v", search)
	}
	if !search.ProjectContext.OutputTruncated || search.ProjectContext.NextResultOffset != 1 {
		t.Fatalf("search must provide a pagination cursor when capped: %#v", search.ProjectContext)
	}
	if _, err := Execute(Spec{WorkspaceRoot: root, Action: "search", Path: ".", ReadScope: []string{"internal"}, Query: "Project"}); err == nil {
		t.Fatal("search must reject a base path outside declared read_scope")
	}

	readme := []byte("# Project\n\nRemote context test, now dirty.\n")
	sum := sha256.Sum256(readme)
	slice, err := Execute(Spec{
		WorkspaceRoot: root,
		Action:        "read_slice",
		Path:          "README.md",
		ChunkBytes:    10,
		ExpectedHash:  "sha256:" + hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if slice.ProjectContext == nil || slice.ProjectContext.Slice == nil || slice.ProjectContext.Slice.NextOffset != 10 || slice.ProjectContext.Slice.SHA256 == "" {
		t.Fatalf("read_slice did not return hash and resume cursor: %#v", slice)
	}
	if _, err := Execute(Spec{WorkspaceRoot: root, Action: "read_slice", Path: "README.md", ExpectedHash: "sha256:bad"}); err == nil {
		t.Fatal("read_slice must reject a stale expected hash")
	}

	symbols, err := Execute(Spec{WorkspaceRoot: root, Action: "symbols", Path: "internal/greeting.go", MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
	if symbols.ProjectContext == nil || !hasContextSymbol(symbols.ProjectContext.Symbols, "Greeting", "function") {
		t.Fatalf("symbols did not locate a Go declaration: %#v", symbols)
	}

	diagnostics, err := Execute(Spec{WorkspaceRoot: root, Action: "diagnostics", Path: "internal", MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics.ProjectContext == nil || !hasContextDiagnostic(diagnostics.ProjectContext.Diagnostics, "go/parser") {
		t.Fatalf("diagnostics did not return structured Go parser errors: %#v", diagnostics)
	}
}

func TestExecuteProjectContextSearchRedactsMatches(t *testing.T) {
	root := t.TempDir()
	secret := "sk-" + strings.Repeat("a", 20)
	mustWriteContextFile(t, filepath.Join(root, "config.txt"), "api_key="+secret+"\n")

	result, err := Execute(Spec{WorkspaceRoot: root, Action: "search", Query: "api_key", MaxResults: 10})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectContext == nil || len(result.ProjectContext.Matches) != 1 {
		t.Fatalf("expected one redacted match, got %#v", result)
	}
	match := result.ProjectContext.Matches[0]
	if strings.Contains(match.Snippet, secret) || !strings.Contains(match.Snippet, "[REDACTED") || !result.ProjectContext.Redacted {
		t.Fatalf("context search leaked a secret: %#v", result.ProjectContext)
	}
}

func TestExecuteProjectContextSearchBoundsSnippet(t *testing.T) {
	root := t.TempDir()
	mustWriteContextFile(t, filepath.Join(root, "long.txt"), strings.Repeat("x", maxContextSnippetRunes+128)+" needle")

	result, err := Execute(Spec{WorkspaceRoot: root, Action: "search", Query: "needle", MaxResults: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectContext == nil || len(result.ProjectContext.Matches) != 1 {
		t.Fatalf("expected one bounded search match, got %#v", result)
	}
	if got := len([]rune(result.ProjectContext.Matches[0].Snippet)); got > maxContextSnippetRunes+1 {
		t.Fatalf("search snippet must stay bounded, got %d runes", got)
	}
}

func mustWriteContextFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contextGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func hasContextFile(files []ContextFile, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

func containsContextString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasContextSymbol(symbols []ContextSymbol, name, kind string) bool {
	for _, symbol := range symbols {
		if symbol.Name == name && symbol.Kind == kind {
			return true
		}
	}
	return false
}

func hasContextDiagnostic(diagnostics []ContextDiagnostic, source string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Source == source && diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}
