package fileadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/EitanWong/remote-dev-skillkit/internal/shelladapter"
)

const ProjectContextSchemaVersion = "rdev.project-context.v1"

const (
	defaultContextMaxResults = 50
	maxContextMaxResults     = 200
	maxContextFileBytes      = 1024 * 1024
	maxContextSnippetRunes   = 512
)

// ProjectContextArtifact keeps project discovery bounded, page-oriented, and
// reviewable inside the established file-adapter artifact boundary.
type ProjectContextArtifact struct {
	SchemaVersion    string              `json:"schema_version"`
	Operation        string              `json:"operation"`
	WorkspaceRoot    string              `json:"workspace_root"`
	Freshness        ContextFreshness    `json:"freshness"`
	Description      *ProjectDescription `json:"description,omitempty"`
	Matches          []ContextMatch      `json:"matches,omitempty"`
	Slice            *ContextSlice       `json:"slice,omitempty"`
	Symbols          []ContextSymbol     `json:"symbols,omitempty"`
	Diagnostics      []ContextDiagnostic `json:"diagnostics,omitempty"`
	NextResultOffset int                 `json:"next_result_offset,omitempty"`
	OutputTruncated  bool                `json:"output_truncated"`
	Redacted         bool                `json:"redacted"`
	RedactionRules   []string            `json:"redaction_rules"`
	RedactionCounts  map[string]int      `json:"redaction_counts,omitempty"`
}

type ContextFreshness struct {
	ObservedAt           string `json:"observed_at"`
	WorkspaceFingerprint string `json:"workspace_fingerprint"`
}

type ProjectDescription struct {
	Git                 GitDescription   `json:"git"`
	Languages           []string         `json:"languages,omitempty"`
	BuildSystems        []string         `json:"build_systems,omitempty"`
	DependencyManifests []ContextFile    `json:"dependency_manifests,omitempty"`
	LockFiles           []ContextFile    `json:"lock_files,omitempty"`
	InstructionFiles    []ContextFile    `json:"instruction_files,omitempty"`
	SuggestedCommands   []ContextCommand `json:"suggested_commands,omitempty"`
	AvailableAdapters   []string         `json:"available_adapters,omitempty"`
	Runtimes            []ContextRuntime `json:"runtimes,omitempty"`
}

type GitDescription struct {
	TopLevel        string             `json:"top_level,omitempty"`
	Branch          string             `json:"branch,omitempty"`
	HeadSHA         string             `json:"head_sha,omitempty"`
	BaseSHA         string             `json:"base_sha,omitempty"`
	Dirty           bool               `json:"dirty"`
	Changes         []ContextGitChange `json:"changes,omitempty"`
	OutputTruncated bool               `json:"output_truncated"`
	InspectionError string             `json:"inspection_error,omitempty"`
}

type ContextGitChange struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

type ContextFile struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
	Modified  string `json:"modified"`
}

type ContextCommand struct {
	Kind      string   `json:"kind"`
	Argv      []string `json:"argv"`
	Available bool     `json:"available"`
	Source    string   `json:"source"`
}

type ContextRuntime struct {
	Name      string `json:"name"`
	Command   string `json:"command"`
	Available bool   `json:"available"`
}

type ContextMatch struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Snippet  string `json:"snippet"`
	SHA256   string `json:"sha256"`
	Modified string `json:"modified"`
}

type ContextSlice struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	ChunkSHA256 string `json:"chunk_sha256"`
	ContentText string `json:"content_text,omitempty"`
	Encoding    string `json:"encoding"`
	Offset      int64  `json:"offset"`
	NextOffset  int64  `json:"next_offset"`
	TotalBytes  int64  `json:"total_bytes"`
	Complete    bool   `json:"complete"`
}

type ContextSymbol struct {
	Path   string `json:"path"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type ContextDiagnostic struct {
	Source   string `json:"source"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Message  string `json:"message"`
}

func executeProjectContext(ctx context.Context, root string, spec Spec, action string) (*ProjectContextArtifact, error) {
	freshness, err := projectFreshness(root)
	if err != nil {
		return nil, err
	}
	redactor := shelladapter.NewArtifactRedactor()
	artifact := &ProjectContextArtifact{
		SchemaVersion:  ProjectContextSchemaVersion,
		Operation:      action,
		WorkspaceRoot:  root,
		Freshness:      freshness,
		RedactionRules: shelladapter.RedactionRuleNames(),
	}
	finish := func() *ProjectContextArtifact {
		artifact.Redacted = redactor.Redacted()
		artifact.RedactionCounts = redactor.Counts()
		return artifact
	}

	switch action {
	case "describe":
		description, err := describeProject(ctx, root, spec, redactor)
		if err != nil {
			return nil, err
		}
		artifact.Description = description
	case "search":
		matches, next, truncated, err := searchProject(ctx, root, spec, redactor)
		if err != nil {
			return nil, err
		}
		artifact.Matches = matches
		artifact.NextResultOffset = next
		artifact.OutputTruncated = truncated
	case "read.slice":
		slice, truncated, err := readProjectSlice(ctx, root, spec, redactor)
		if err != nil {
			return nil, err
		}
		artifact.Slice = slice
		artifact.OutputTruncated = truncated
	case "symbols":
		symbols, next, truncated, err := projectSymbols(ctx, root, spec)
		if err != nil {
			return nil, err
		}
		artifact.Symbols = symbols
		artifact.NextResultOffset = next
		artifact.OutputTruncated = truncated
	case "diagnostics":
		diagnostics, next, truncated, err := projectDiagnostics(ctx, root, spec, redactor)
		if err != nil {
			return nil, err
		}
		artifact.Diagnostics = diagnostics
		artifact.NextResultOffset = next
		artifact.OutputTruncated = truncated
	default:
		return nil, fmt.Errorf("unsupported project context action %q", action)
	}
	return finish(), nil
}

func projectFreshness(root string) (ContextFreshness, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ContextFreshness{}, err
	}
	hasher := sha256.New()
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(hasher, "%s\x00%d\x00%d\x00%t\n", entry.Name(), info.Size(), info.ModTime().UnixNano(), entry.IsDir())
	}
	return ContextFreshness{
		ObservedAt:           time.Now().UTC().Format(time.RFC3339Nano),
		WorkspaceFingerprint: "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

func describeProject(ctx context.Context, root string, spec Spec, redactor *shelladapter.ArtifactRedactor) (*ProjectDescription, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]fs.DirEntry, len(entries))
	for _, entry := range entries {
		byName[strings.ToLower(entry.Name())] = entry
	}
	description := &ProjectDescription{
		Git:               inspectProjectGit(ctx, root, spec.AllowGitInspection, redactor),
		AvailableAdapters: []string{"shell", "powershell", "codex", "claude-code", "acpx", "file", "desktop"},
		Runtimes:          availableRuntimes(),
	}
	languages := map[string]bool{}
	buildSystems := map[string]bool{}
	if _, ok := byName["go.mod"]; ok {
		languages["Go"] = true
		buildSystems["go"] = true
		description.SuggestedCommands = append(description.SuggestedCommands,
			contextCommand("test", "go.mod", "go", "test", "./..."),
			contextCommand("lint", "go.mod", "go", "vet", "./..."),
			contextCommand("build", "go.mod", "go", "build", "./..."),
		)
	}
	if _, ok := byName["package.json"]; ok {
		languages["JavaScript"] = true
		buildSystems["node"] = true
		description.SuggestedCommands = append(description.SuggestedCommands, contextCommand("test", "package.json", "npm", "test"))
	}
	if _, ok := byName["pyproject.toml"]; ok {
		languages["Python"] = true
		buildSystems["python"] = true
		description.SuggestedCommands = append(description.SuggestedCommands, contextCommand("test", "pyproject.toml", "pytest"))
	}
	if _, ok := byName["cargo.toml"]; ok {
		languages["Rust"] = true
		buildSystems["cargo"] = true
		description.SuggestedCommands = append(description.SuggestedCommands, contextCommand("test", "Cargo.toml", "cargo", "test"))
	}
	if _, ok := byName["pom.xml"]; ok {
		languages["Java"] = true
		buildSystems["maven"] = true
		description.SuggestedCommands = append(description.SuggestedCommands, contextCommand("test", "pom.xml", "mvn", "test"))
	}
	if _, ok := byName["build.gradle"]; ok {
		languages["Java"] = true
		buildSystems["gradle"] = true
		description.SuggestedCommands = append(description.SuggestedCommands, contextCommand("test", "build.gradle", "gradle", "test"))
	}
	if _, ok := byName["package.swift"]; ok {
		languages["Swift"] = true
		buildSystems["swift"] = true
		description.SuggestedCommands = append(description.SuggestedCommands, contextCommand("test", "Package.swift", "swift", "test"))
	}
	for name, entry := range byName {
		if entry.IsDir() {
			continue
		}
		switch {
		case isDependencyManifest(name):
			file, err := contextFileReference(root, entry.Name())
			if err == nil {
				description.DependencyManifests = append(description.DependencyManifests, file)
			}
		case isLockFile(name):
			file, err := contextFileReference(root, entry.Name())
			if err == nil {
				description.LockFiles = append(description.LockFiles, file)
			}
		case isInstructionFile(name):
			file, err := contextFileReference(root, entry.Name())
			if err == nil {
				description.InstructionFiles = append(description.InstructionFiles, file)
			}
		}
	}
	if err := inferLanguages(ctx, root, languages); err != nil {
		return nil, err
	}
	description.Languages = sortedContextKeys(languages)
	description.BuildSystems = sortedContextKeys(buildSystems)
	sortContextFiles(description.DependencyManifests)
	sortContextFiles(description.LockFiles)
	sortContextFiles(description.InstructionFiles)
	sort.Slice(description.SuggestedCommands, func(i, j int) bool {
		return strings.Join(description.SuggestedCommands[i].Argv, "\x00") < strings.Join(description.SuggestedCommands[j].Argv, "\x00")
	})
	return description, nil
}

func inspectProjectGit(ctx context.Context, root string, allowed bool, redactor *shelladapter.ArtifactRedactor) GitDescription {
	if !allowed {
		return GitDescription{InspectionError: "Git inspection requires the git.diff capability."}
	}
	topLevel, err := gitProjectOutput(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		if ctx.Err() != nil {
			return GitDescription{InspectionError: redactor.Redact(ctx.Err().Error())}
		}
		return GitDescription{InspectionError: "Workspace is not a Git repository or Git is unavailable."}
	}
	git := GitDescription{TopLevel: strings.TrimSpace(topLevel)}
	branch, _ := gitProjectOutput(ctx, root, "branch", "--show-current")
	git.Branch = strings.TrimSpace(branch)
	head, err := gitProjectOutput(ctx, root, "rev-parse", "HEAD")
	if err == nil {
		git.HeadSHA = strings.TrimSpace(head)
	}
	base, err := gitProjectOutput(ctx, root, "merge-base", "HEAD", "@{upstream}")
	if err == nil {
		git.BaseSHA = strings.TrimSpace(base)
	} else {
		git.BaseSHA = git.HeadSHA
	}
	status, err := gitProjectOutput(ctx, root, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		if ctx.Err() != nil {
			git.InspectionError = redactor.Redact(ctx.Err().Error())
		} else {
			git.InspectionError = "Git status inspection failed."
		}
		return git
	}
	git.Changes, git.OutputTruncated = parseGitStatus(status, redactor)
	git.Dirty = len(git.Changes) > 0
	return git
}

func gitProjectOutput(ctx context.Context, root string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func parseGitStatus(status string, redactor *shelladapter.ArtifactRedactor) ([]ContextGitChange, bool) {
	const maxChanges = 50
	lines := strings.Split(strings.TrimSuffix(status, "\n"), "\n")
	changes := make([]ContextGitChange, 0, minInt(len(lines), maxChanges))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(changes) >= maxChanges {
			return changes, true
		}
		state := strings.TrimSpace(line)
		path := ""
		if len(line) >= 3 {
			state = line[:2]
			path = strings.TrimSpace(line[3:])
		}
		changes = append(changes, ContextGitChange{Status: state, Path: redactor.Redact(path)})
	}
	return changes, false
}

func availableRuntimes() []ContextRuntime {
	runtimes := []ContextRuntime{
		{Name: "Go", Command: "go"},
		{Name: "Node.js", Command: "node"},
		{Name: "Python", Command: "python3"},
		{Name: "Rust", Command: "cargo"},
		{Name: ".NET", Command: "dotnet"},
		{Name: "Swift", Command: "swift"},
	}
	for i := range runtimes {
		_, err := exec.LookPath(runtimes[i].Command)
		runtimes[i].Available = err == nil
	}
	return runtimes
}

func contextCommand(kind, source string, argv ...string) ContextCommand {
	available := false
	if len(argv) > 0 {
		_, err := exec.LookPath(argv[0])
		available = err == nil
	}
	return ContextCommand{Kind: kind, Argv: argv, Available: available, Source: source}
}

func isDependencyManifest(name string) bool {
	switch name {
	case "go.mod", "package.json", "pyproject.toml", "requirements.txt", "cargo.toml", "pom.xml", "build.gradle", "package.swift":
		return true
	default:
		return strings.HasSuffix(name, ".csproj") || strings.HasSuffix(name, ".fsproj") || strings.HasSuffix(name, ".sln")
	}
}

func isLockFile(name string) bool {
	switch name {
	case "go.sum", "package-lock.json", "npm-shrinkwrap.json", "yarn.lock", "pnpm-lock.yaml", "poetry.lock", "cargo.lock", "gradle.lockfile", "package.resolved":
		return true
	default:
		return strings.HasSuffix(name, ".lock")
	}
}

func isInstructionFile(name string) bool {
	switch name {
	case "agents.md", "claude.md", "readme.md", "readme", "readme.txt":
		return true
	default:
		return false
	}
}

func contextFileReference(root, name string) (ContextFile, error) {
	path := filepath.Join(root, name)
	info, err := os.Stat(path)
	if err != nil {
		return ContextFile{}, err
	}
	hash, err := contextFileHash(path)
	if err != nil {
		return ContextFile{}, err
	}
	return ContextFile{
		Path:      filepath.ToSlash(name),
		SHA256:    hash,
		SizeBytes: info.Size(),
		Modified:  info.ModTime().UTC().Format(time.RFC3339Nano),
	}, nil
}

func contextFileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

func inferLanguages(ctx context.Context, root string, languages map[string]bool) error {
	return walkContextFiles(ctx, root, root, nil, func(path string, entry fs.DirEntry) (bool, error) {
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".go":
			languages["Go"] = true
		case ".ts", ".tsx":
			languages["TypeScript"] = true
		case ".js", ".jsx", ".mjs", ".cjs":
			languages["JavaScript"] = true
		case ".py":
			languages["Python"] = true
		case ".rs":
			languages["Rust"] = true
		case ".java":
			languages["Java"] = true
		case ".cs":
			languages["C#"] = true
		case ".swift":
			languages["Swift"] = true
		}
		return false, nil
	})
}

func searchProject(ctx context.Context, root string, spec Spec, redactor *shelladapter.ArtifactRedactor) ([]ContextMatch, int, bool, error) {
	query := strings.TrimSpace(spec.Query)
	if query == "" {
		return nil, 0, false, fmt.Errorf("project context search query is required")
	}
	base, err := resolveProjectContextPath(root, spec.Path, spec.ReadScope)
	if err != nil {
		return nil, 0, false, err
	}
	limit := contextResultLimit(spec.MaxResults)
	offset := maxInt(spec.ResultOffset, 0)
	matches := make([]ContextMatch, 0, limit)
	seen := 0
	truncated := false
	err = walkContextFiles(ctx, root, base, spec.ReadScope, func(path string, entry fs.DirEntry) (bool, error) {
		if !matchesContextGlob(root, path, spec.Glob) {
			return false, nil
		}
		content, info, ok, err := readContextText(path)
		if err != nil || !ok {
			return false, err
		}
		for lineNumber, line := range strings.Split(content, "\n") {
			column := strings.Index(line, query)
			if column < 0 {
				continue
			}
			if seen < offset {
				seen++
				continue
			}
			if len(matches) >= limit {
				truncated = true
				return true, nil
			}
			hash, err := contextFileHash(path)
			if err != nil {
				return false, err
			}
			rel, _ := filepath.Rel(root, path)
			matches = append(matches, ContextMatch{
				Path:     filepath.ToSlash(rel),
				Line:     lineNumber + 1,
				Column:   column + 1,
				Snippet:  contextSnippet(redactor.Redact(line)),
				SHA256:   hash,
				Modified: info.ModTime().UTC().Format(time.RFC3339Nano),
			})
			seen++
		}
		return false, nil
	})
	if err != nil && !errors.Is(err, errContextWalkStopped) {
		return nil, 0, false, err
	}
	next := 0
	if truncated {
		next = offset + len(matches)
	}
	return matches, next, truncated, nil
}

func readProjectSlice(ctx context.Context, root string, spec Spec, redactor *shelladapter.ArtifactRedactor) (*ContextSlice, bool, error) {
	if err := contextErr(ctx); err != nil {
		return nil, false, err
	}
	path, err := resolveProjectContextPath(root, spec.Path, spec.ReadScope)
	if err != nil {
		return nil, false, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if info.IsDir() {
		return nil, false, fmt.Errorf("project context read_slice path must be a file")
	}
	if spec.Offset < 0 || spec.Offset > info.Size() {
		return nil, false, fmt.Errorf("project context read_slice offset is outside file size")
	}
	hash, err := contextFileHash(path)
	if err != nil {
		return nil, false, err
	}
	expected := strings.TrimSpace(spec.ExpectedHash)
	if expected == "" {
		expected = strings.TrimSpace(spec.ExpectedSHA256)
	}
	if expected != "" && normalizeSHA256(expected) != normalizeSHA256(hash) {
		return nil, false, fmt.Errorf("project context read_slice expected_hash does not match current file hash")
	}
	chunkBytes := spec.ChunkBytes
	if chunkBytes <= 0 {
		chunkBytes = 32 * 1024
	}
	if chunkBytes > maxContextFileBytes {
		chunkBytes = maxContextFileBytes
	}
	if spec.MaxOutputBytes > 0 && chunkBytes > safeChunkBytes(spec.MaxOutputBytes) {
		chunkBytes = safeChunkBytes(spec.MaxOutputBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	if _, err := file.Seek(spec.Offset, io.SeekStart); err != nil {
		return nil, false, err
	}
	content, err := io.ReadAll(io.LimitReader(file, int64(chunkBytes)))
	if err != nil {
		return nil, false, err
	}
	chunkHash := sha256.Sum256(content)
	next := spec.Offset + int64(len(content))
	slice := &ContextSlice{
		Path:        relativeContextPath(root, path),
		SHA256:      hash,
		ChunkSHA256: "sha256:" + hex.EncodeToString(chunkHash[:]),
		Offset:      spec.Offset,
		NextOffset:  next,
		TotalBytes:  info.Size(),
		Complete:    next >= info.Size(),
	}
	if utf8.Valid(content) {
		slice.Encoding = "utf-8"
		slice.ContentText = redactor.Redact(string(content))
	} else {
		slice.Encoding = "binary-omitted"
	}
	return slice, !slice.Complete, nil
}

func projectSymbols(ctx context.Context, root string, spec Spec) ([]ContextSymbol, int, bool, error) {
	base, err := resolveProjectContextPath(root, spec.Path, spec.ReadScope)
	if err != nil {
		return nil, 0, false, err
	}
	limit := contextResultLimit(spec.MaxResults)
	offset := maxInt(spec.ResultOffset, 0)
	symbols := make([]ContextSymbol, 0, limit)
	seen := 0
	truncated := false
	err = walkContextFiles(ctx, root, base, spec.ReadScope, func(path string, entry fs.DirEntry) (bool, error) {
		fileSymbols, err := symbolsForContextFile(root, path)
		if err != nil {
			return false, nil
		}
		for _, symbol := range fileSymbols {
			if seen < offset {
				seen++
				continue
			}
			if len(symbols) >= limit {
				truncated = true
				return true, nil
			}
			symbols = append(symbols, symbol)
			seen++
		}
		return false, nil
	})
	if err != nil && !errors.Is(err, errContextWalkStopped) {
		return nil, 0, false, err
	}
	next := 0
	if truncated {
		next = offset + len(symbols)
	}
	return symbols, next, truncated, nil
}

func symbolsForContextFile(root, path string) ([]ContextSymbol, error) {
	if strings.EqualFold(filepath.Ext(path), ".go") {
		return goContextSymbols(root, path)
	}
	content, _, ok, err := readContextText(path)
	if err != nil || !ok {
		return nil, err
	}
	return genericContextSymbols(root, path, content), nil
}

func goContextSymbols(root, path string) ([]ContextSymbol, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	symbols := make([]ContextSymbol, 0)
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			symbols = append(symbols, contextSymbolAt(root, path, fset.Position(typed.Name.Pos()), typed.Name.Name, "function"))
		case *ast.GenDecl:
			for _, specification := range typed.Specs {
				switch item := specification.(type) {
				case *ast.TypeSpec:
					symbols = append(symbols, contextSymbolAt(root, path, fset.Position(item.Name.Pos()), item.Name.Name, "type"))
				case *ast.ValueSpec:
					kind := "variable"
					if typed.Tok.String() == "const" {
						kind = "constant"
					}
					for _, name := range item.Names {
						symbols = append(symbols, contextSymbolAt(root, path, fset.Position(name.Pos()), name.Name, kind))
					}
				}
			}
		}
	}
	return symbols, nil
}

func contextSymbolAt(root, path string, position token.Position, name, kind string) ContextSymbol {
	return ContextSymbol{Path: relativeContextPath(root, path), Name: name, Kind: kind, Line: position.Line, Column: position.Column}
}

var genericSymbolPattern = regexp.MustCompile(`(?m)^\s*(?:function|func|class|interface|type|struct|def)\s+([A-Za-z_][A-Za-z0-9_]*)`)

func genericContextSymbols(root, path, content string) []ContextSymbol {
	matches := genericSymbolPattern.FindAllStringSubmatchIndex(content, -1)
	out := make([]ContextSymbol, 0, len(matches))
	for _, match := range matches {
		name := content[match[2]:match[3]]
		prefix := content[:match[2]]
		line := strings.Count(prefix, "\n") + 1
		column := len(prefix) - strings.LastIndex(prefix, "\n")
		out = append(out, ContextSymbol{Path: relativeContextPath(root, path), Name: name, Kind: "symbol", Line: line, Column: column})
	}
	return out
}

func projectDiagnostics(ctx context.Context, root string, spec Spec, redactor *shelladapter.ArtifactRedactor) ([]ContextDiagnostic, int, bool, error) {
	base, err := resolveProjectContextPath(root, spec.Path, spec.ReadScope)
	if err != nil {
		return nil, 0, false, err
	}
	limit := contextResultLimit(spec.MaxResults)
	offset := maxInt(spec.ResultOffset, 0)
	diagnostics := make([]ContextDiagnostic, 0, limit)
	seen := 0
	truncated := false
	err = walkContextFiles(ctx, root, base, spec.ReadScope, func(path string, entry fs.DirEntry) (bool, error) {
		if !strings.EqualFold(filepath.Ext(path), ".go") {
			return false, nil
		}
		fileDiagnostics := goContextDiagnostics(root, path, redactor)
		for _, diagnostic := range fileDiagnostics {
			if seen < offset {
				seen++
				continue
			}
			if len(diagnostics) >= limit {
				truncated = true
				return true, nil
			}
			diagnostics = append(diagnostics, diagnostic)
			seen++
		}
		return false, nil
	})
	if err != nil && !errors.Is(err, errContextWalkStopped) {
		return nil, 0, false, err
	}
	next := 0
	if truncated {
		next = offset + len(diagnostics)
	}
	return diagnostics, next, truncated, nil
}

func goContextDiagnostics(root, path string, redactor *shelladapter.ArtifactRedactor) []ContextDiagnostic {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, path, nil, parser.AllErrors|parser.SkipObjectResolution)
	if err == nil {
		return nil
	}
	var list scanner.ErrorList
	if errors.As(err, &list) {
		out := make([]ContextDiagnostic, 0, len(list))
		for _, item := range list {
			out = append(out, ContextDiagnostic{
				Source:   "go/parser",
				Severity: "error",
				Path:     relativeContextPath(root, item.Pos.Filename),
				Line:     item.Pos.Line,
				Column:   item.Pos.Column,
				Message:  redactor.Redact(item.Msg),
			})
		}
		return out
	}
	return []ContextDiagnostic{{
		Source:   "go/parser",
		Severity: "error",
		Path:     relativeContextPath(root, path),
		Message:  redactor.Redact(err.Error()),
	}}
}

func resolveProjectContextPath(root, raw string, readScope []string) (string, error) {
	path, err := resolveExistingPath(root, raw)
	if err != nil {
		return "", err
	}
	if !projectPathWithinReadScope(root, path, readScope) {
		return "", fmt.Errorf("project context path is outside declared read_scope")
	}
	return path, nil
}

func projectPathWithinReadScope(root, target string, readScope []string) bool {
	if len(readScope) == 0 {
		return true
	}
	for _, scope := range readScope {
		scopeTarget, err := cleanTarget(root, scope)
		if err != nil {
			continue
		}
		if existing, err := filepath.EvalSymlinks(scopeTarget); err == nil {
			scopeTarget = existing
		}
		if pathWithin(scopeTarget, target) {
			return true
		}
	}
	return false
}

func walkContextFiles(ctx context.Context, root, base string, readScope []string, visit func(path string, entry fs.DirEntry) (stop bool, err error)) error {
	return filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := contextErr(ctx); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".git" || name == ".rdev" || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			if !projectPathWithinReadScope(root, path, readScope) && filepath.Clean(path) != filepath.Clean(base) {
				return filepath.SkipDir
			}
			return nil
		}
		if !projectPathWithinReadScope(root, path, readScope) {
			return nil
		}
		stop, err := visit(path, entry)
		if err != nil {
			return err
		}
		if stop {
			return errContextWalkStopped
		}
		return nil
	})
}

var errContextWalkStopped = errors.New("project context walk stopped")

func readContextText(path string) (string, os.FileInfo, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, false, err
	}
	if info.IsDir() || info.Size() > maxContextFileBytes {
		return "", info, false, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", nil, false, err
	}
	if !utf8.Valid(content) {
		return "", info, false, nil
	}
	return string(content), info, true, nil
}

func matchesContextGlob(root, path, glob string) bool {
	glob = strings.TrimSpace(glob)
	if glob == "" || glob == "*" {
		return true
	}
	rel := relativeContextPath(root, path)
	if matched, err := pathpkg.Match(glob, rel); err == nil && matched {
		return true
	}
	if strings.HasPrefix(glob, "**/") {
		if matched, err := pathpkg.Match(strings.TrimPrefix(glob, "**/"), rel); err == nil && matched {
			return true
		}
	}
	matched, err := pathpkg.Match(glob, pathpkg.Base(rel))
	return err == nil && matched
}

func contextSnippet(value string) string {
	runes := []rune(value)
	if len(runes) <= maxContextSnippetRunes {
		return value
	}
	return string(runes[:maxContextSnippetRunes]) + "…"
}

func relativeContextPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func contextResultLimit(value int) int {
	if value <= 0 {
		return defaultContextMaxResults
	}
	if value > maxContextMaxResults {
		return maxContextMaxResults
	}
	return value
}

func contextErr(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func sortedContextKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func sortContextFiles(files []ContextFile) {
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
