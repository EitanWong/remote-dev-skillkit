package connectionentry

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteWindowsLayeredArchiveDeterministicAndPrivate(t *testing.T) {
	generatedAt := time.Date(2026, 7, 16, 20, 34, 56, 0, time.FixedZone("UTC+8", 8*60*60))
	firstFiles := []windowsLayeredArchiveFile{
		{name: "zeta.txt", content: []byte("zeta\n")},
		{name: "alpha.txt", content: []byte("alpha\n")},
	}
	secondFiles := []windowsLayeredArchiveFile{
		{name: "alpha.txt", content: []byte("alpha\n")},
		{name: "zeta.txt", content: []byte("zeta\n")},
	}
	firstPath := filepath.Join(t.TempDir(), "first.zip")
	secondPath := filepath.Join(t.TempDir(), "second.zip")

	firstReport, err := writeWindowsLayeredArchive(firstPath, generatedAt, firstFiles)
	if err != nil {
		t.Fatal(err)
	}
	secondReport, err := writeWindowsLayeredArchive(secondPath, generatedAt, secondFiles)
	if err != nil {
		t.Fatal(err)
	}
	if firstFiles[0].name != "zeta.txt" {
		t.Fatalf("archive writer mutated caller order: %#v", firstFiles)
	}

	firstArchive := readTestFile(t, firstPath)
	secondArchive := readTestFile(t, secondPath)
	if !bytes.Equal(firstArchive, secondArchive) {
		t.Fatal("same file set must produce byte-identical ZIP output regardless of input order")
	}
	digest := sha256.Sum256(firstArchive)
	wantSHA256 := hex.EncodeToString(digest[:])
	if firstReport.Path != firstPath || firstReport.SizeBytes != int64(len(firstArchive)) || firstReport.SHA256 != wantSHA256 {
		t.Fatalf("unexpected first archive report: %#v", firstReport)
	}
	if secondReport.SizeBytes != firstReport.SizeBytes || secondReport.SHA256 != firstReport.SHA256 {
		t.Fatalf("deterministic archives must have the same report: first=%#v second=%#v", firstReport, secondReport)
	}
	if !firstReport.Private || firstReport.PrivacyDetail == "" {
		t.Fatalf("archive report must record verified private protection: %#v", firstReport)
	}
	assertWindowsLayeredArchivePrivate(t, firstPath)

	reader, err := zip.OpenReader(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if len(reader.File) != 2 || reader.File[0].Name != "alpha.txt" || reader.File[1].Name != "zeta.txt" {
		t.Fatalf("archive entries must be sorted by safe basename: %#v", reader.File)
	}
	for _, file := range reader.File {
		if file.Method != zip.Deflate {
			t.Errorf("entry %q must use deflate, got method %d", file.Name, file.Method)
		}
		if file.Mode().Perm() != 0o600 {
			t.Errorf("entry %q must have mode 0600, got %o", file.Name, file.Mode().Perm())
		}
		if !file.Modified.Equal(generatedAt.UTC()) {
			t.Errorf("entry %q timestamp = %s, want %s", file.Name, file.Modified, generatedAt.UTC())
		}
	}
}

func TestWindowsSafeArchiveBasename(t *testing.T) {
	for name, wantKey := range map[string]string{
		"entry.txt": "ENTRY.TXT",
		"COM10.txt": "COM10.TXT",
		"LPT0":      "LPT0",
		".hidden":   ".HIDDEN",
	} {
		t.Run("accept "+name, func(t *testing.T) {
			key, ok := windowsSafeArchiveBasename(name)
			if !ok || key != wantKey {
				t.Fatalf("windowsSafeArchiveBasename(%q) = %q, %t; want %q, true", name, key, ok, wantKey)
			}
		})
	}

	for _, name := range []string{
		"", ".", "..", "/absolute.txt", `nested\file.txt`, "C:entry.txt",
		"CON", "con.txt", "CON .txt", "PRN.log", "AUX", "NUL.bin", "CLOCK$.txt",
		"COM1", "com9.log", "LPT1", "LPT1 .log", "lpt9.log",
		"COM¹", "COM².txt", "COM³ .txt", "LPT¹", "LPT².log", "LPT³ .log",
		"entry.", "entry ", "entry\x00.txt", "entry\x1f.txt", "entry\x7f.txt",
		"entry<.txt", "entry>.txt", `entry".txt`, "entry|.txt", "entry?.txt", "entry*.txt",
	} {
		t.Run("reject "+name, func(t *testing.T) {
			if key, ok := windowsSafeArchiveBasename(name); ok {
				t.Fatalf("windowsSafeArchiveBasename(%q) = %q, true; want rejection", name, key)
			}
		})
	}
}

func TestWriteWindowsLayeredArchiveReportsPrivacyCleanupFailure(t *testing.T) {
	validationErr := errors.New("injected privacy validation failure")
	cleanupErr := errors.New("injected privacy cleanup failure")
	previousHook := windowsLayeredArchivePostPublishHook
	previousCleanup := windowsLayeredArchivePublishedCleanup
	windowsLayeredArchivePostPublishHook = func(string) error {
		return validationErr
	}
	windowsLayeredArchivePublishedCleanup = func(path string) error {
		cleanupActualErr := cleanupPublishedWindowsLayeredArchive(path)
		return errors.Join(cleanupActualErr, cleanupErr)
	}
	t.Cleanup(func() {
		windowsLayeredArchivePostPublishHook = previousHook
		windowsLayeredArchivePublishedCleanup = previousCleanup
	})

	path := filepath.Join(t.TempDir(), "handoff.zip")
	_, err := writeWindowsLayeredArchive(path, time.Unix(0, 0), []windowsLayeredArchiveFile{{
		name: "entry.txt", content: []byte("sensitive archive bytes\n"),
	}})
	if !errors.Is(err, validationErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("privacy validation and cleanup errors must both be returned, got %v", err)
	}
	if content, readErr := os.ReadFile(path); readErr == nil && len(content) != 0 {
		t.Fatalf("privacy failure left sensitive archive contents behind: %d bytes", len(content))
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("inspect privacy-failure cleanup result: %v", readErr)
	}
}

func TestWindowsLayeredArchivePrepublishChecksStayHandleBound(t *testing.T) {
	content, err := os.ReadFile("windows_layered_archive.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(content)
	for _, forbidden := range []string{
		"os.Stat(temporaryPath)",
		"os.Open(temporaryPath)",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("archive prepublication validation must stay on the creation handle, found %q", forbidden)
		}
	}
	for _, required := range []string{
		"temporary.Stat()",
		"hashWindowsLayeredArchiveHandle(temporary)",
		"validateWindowsLayeredArchiveHandle(temporary)",
		"publishWindowsLayeredArchiveHandle(temporary, path)",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("archive prepublication validation is missing handle-bound operation %q", required)
		}
	}
	publishOffset := strings.Index(source, "publishWindowsLayeredArchiveHandle(temporary, path)")
	validationOffset := strings.Index(source, "validatePublishedWindowsLayeredArchive(path, expectation)")
	if publishOffset < 0 || validationOffset < publishOffset {
		t.Fatal("archive publication must precede final handle validation")
	}
	between := source[publishOffset:validationOffset]
	if closeOffset := strings.Index(between, "temporary.Close()"); closeOffset >= 0 {
		guardedFailureOffset := strings.Index(between[:closeOffset], "if windowsLayeredArchivePostPublishHook != nil")
		if guardedFailureOffset < 0 {
			t.Fatal("archive creation guard handle must remain open through normal final target reopen and validation")
		}
	}
}

func TestWriteWindowsLayeredArchiveRejectsPublishedPathReplacement(t *testing.T) {
	previousHook := windowsLayeredArchivePostPublishHook
	windowsLayeredArchivePostPublishHook = func(path string) error {
		if err := os.Rename(path, path+".original"); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte("replacement archive\n"), 0o600); err != nil {
			return err
		}
		return nil
	}
	t.Cleanup(func() { windowsLayeredArchivePostPublishHook = previousHook })

	path := filepath.Join(t.TempDir(), "handoff.zip")
	_, err := writeWindowsLayeredArchive(path, time.Unix(0, 0), []windowsLayeredArchiveFile{{
		name: "entry.txt", content: []byte("sensitive archive bytes\n"),
	}})
	if err == nil {
		t.Fatal("archive publication must reject a path replacement with a different file identity")
	}
	if content, readErr := os.ReadFile(path); readErr == nil && len(content) != 0 {
		t.Fatalf("replacement failure left published bytes behind: %d bytes", len(content))
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("inspect replacement cleanup result: %v", readErr)
	}
	if content, readErr := os.ReadFile(path + ".original"); readErr == nil && len(content) != 0 {
		t.Fatalf("replacement failure left original sensitive bytes behind: %d bytes", len(content))
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("inspect original replacement cleanup result: %v", readErr)
	}
}

func TestWindowsLayeredArchiveCleanupHelpers(t *testing.T) {
	if err := cleanupPublishedWindowsLayeredArchive(filepath.Join(t.TempDir(), "missing.zip")); err != nil {
		t.Fatalf("cleanup of an already absent archive must succeed: %v", err)
	}
	if err := wrapWindowsLayeredArchiveCloseError(nil); err != nil {
		t.Fatalf("nil ZIP close error must remain nil: %v", err)
	}
	sentinel := errors.New("close failed")
	if err := wrapWindowsLayeredArchiveCloseError(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("ZIP close error must be wrapped, got %v", err)
	}
	closed, err := os.CreateTemp(t.TempDir(), "closed-archive-*")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := hashWindowsLayeredArchiveHandle(closed); err == nil {
		t.Fatal("hashing a closed archive handle must fail")
	}
}

func TestCleanupPublishedWindowsLayeredArchiveDoesNotFollowSymlink(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "unrelated-target.txt")
	want := []byte("unrelated target must remain intact\n")
	if err := os.WriteFile(targetPath, want, 0o600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(root, "handoff.zip")
	if err := os.Symlink(targetPath, archivePath); err != nil {
		t.Skipf("create symlink fixture: %v", err)
	}

	if err := cleanupPublishedWindowsLayeredArchive(archivePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("archive directory entry still exists: %v", err)
	}
	if got, err := os.ReadFile(targetPath); err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(got, want) {
		t.Fatalf("cleanup followed archive symlink: got %q, want %q", got, want)
	}
}

func TestValidatePublishedWindowsLayeredArchiveRejectsSizeAndDigestChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "handoff.zip")
	if err := os.WriteFile(path, []byte("archive bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := validateWindowsLayeredArchiveHandle(file)
	if err != nil {
		t.Fatal(err)
	}
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := hashWindowsLayeredArchiveHandle(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name     string
		expected windowsLayeredArchiveExpectation
		want     string
	}{
		{
			name:     "valid",
			expected: windowsLayeredArchiveExpectation{identity: identity, sizeBytes: info.Size(), sha256: digest},
		},
		{
			name:     "size",
			expected: windowsLayeredArchiveExpectation{identity: identity, sizeBytes: info.Size() + 1, sha256: digest},
			want:     "size or type changed",
		},
		{
			name:     "digest",
			expected: windowsLayeredArchiveExpectation{identity: identity, sizeBytes: info.Size(), sha256: strings.Repeat("0", 64)},
			want:     "digest changed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := validatePublishedWindowsLayeredArchive(path, test.expected)
			if test.want == "" {
				if err != nil {
					t.Fatalf("expected valid published archive, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %s validation failure, got %v", test.name, err)
			}
		})
	}
}

func TestValidatePublishedWindowsLayeredArchiveRejectsDifferentIdentity(t *testing.T) {
	root := t.TempDir()
	firstPath := filepath.Join(root, "first.zip")
	secondPath := filepath.Join(root, "second.zip")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.WriteFile(path, []byte("same archive bytes\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := os.Open(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	identity, _, err := validateWindowsLayeredArchiveHandle(first)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := os.Open(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := second.Stat()
	if err != nil {
		t.Fatal(err)
	}
	digest, err := hashWindowsLayeredArchiveHandle(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}

	expected := windowsLayeredArchiveExpectation{identity: identity, sizeBytes: info.Size(), sha256: digest}
	if _, err := validatePublishedWindowsLayeredArchive(secondPath, expected); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("expected published archive identity mismatch, got %v", err)
	}
}

func TestWriteWindowsLayeredArchiveRejectsUnsafeNames(t *testing.T) {
	tests := []struct {
		name  string
		files []windowsLayeredArchiveFile
	}{
		{name: "duplicate", files: []windowsLayeredArchiveFile{{name: "same.txt"}, {name: "same.txt"}}},
		{name: "absolute", files: []windowsLayeredArchiveFile{{name: "/absolute.txt"}}},
		{name: "traversal", files: []windowsLayeredArchiveFile{{name: "../escape.txt"}}},
		{name: "backslash", files: []windowsLayeredArchiveFile{{name: `nested\file.txt`}}},
		{name: "drive relative", files: []windowsLayeredArchiveFile{{name: "C:entry.txt"}}},
		{name: "case folded duplicate", files: []windowsLayeredArchiveFile{{name: "ENTRY.txt"}, {name: "entry.txt"}}},
		{name: "Windows normalized duplicate", files: []windowsLayeredArchiveFile{{name: "ſafe.txt"}, {name: "SAFE.TXT"}}},
		{name: "reserved device", files: []windowsLayeredArchiveFile{{name: "CON.txt"}}},
		{name: "trailing dot", files: []windowsLayeredArchiveFile{{name: "entry."}}},
		{name: "trailing space", files: []windowsLayeredArchiveFile{{name: "entry "}}},
		{name: "ASCII control", files: []windowsLayeredArchiveFile{{name: "entry\x1f.txt"}}},
		{name: "forbidden punctuation", files: []windowsLayeredArchiveFile{{name: "entry?.txt"}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "handoff.zip")
			if _, err := writeWindowsLayeredArchive(path, time.Unix(0, 0), test.files); err == nil {
				t.Fatalf("expected archive name validation to reject %#v", test.files)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("rejected archive must not be published, stat error = %v", err)
			}
		})
	}
}

func TestWriteWindowsLayeredArchiveRejectsOversizeOutput(t *testing.T) {
	content := make([]byte, maxWindowsLayeredHandoffBytes+64*1024)
	if _, err := rand.New(rand.NewSource(1)).Read(content); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "handoff.zip")

	_, err := writeWindowsLayeredArchive(path, time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC), []windowsLayeredArchiveFile{
		{name: "rdev-bootstrap.exe", content: content},
	})
	if err == nil {
		t.Fatal("expected final compressed archive larger than 1 MiB to be rejected")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected final-size gate error, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("oversize archive must not be published, stat error = %v", err)
	}
}

func TestWriteWindowsLayeredArchiveRemovesTemporaryFileOnPublishFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "handoff.zip")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := writeWindowsLayeredArchive(path, time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC), []windowsLayeredArchiveFile{
		{name: "entry.txt", content: []byte("entry\n")},
	}); err == nil {
		t.Fatal("expected publishing over a directory to fail")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) || !entries[0].IsDir() {
		t.Fatalf("publish failure left a temporary archive behind: %#v", entries)
	}
}

func TestWriteWindowsLayeredArchiveRejectsExistingDestination(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "handoff.zip")
	if err := os.WriteFile(path, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := writeWindowsLayeredArchive(path, time.Unix(0, 0), []windowsLayeredArchiveFile{{
		name: "entry.txt", content: []byte("new archive\n"),
	}})
	if err == nil || !strings.Contains(err.Error(), "destination already exists") {
		t.Fatalf("expected existing destination rejection, got %v", err)
	}
	if got := string(readTestFile(t, path)); got != "existing\n" {
		t.Fatalf("existing destination changed: %q", got)
	}
}
