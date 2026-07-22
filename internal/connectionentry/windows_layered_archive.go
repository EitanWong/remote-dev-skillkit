package connectionentry

import (
	"archive/zip"
	"compress/flate"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxWindowsLayeredHandoffBytes int64 = 1 << 20

type windowsLayeredArchiveFile struct {
	name    string
	content []byte
}

type windowsLayeredArchiveReport struct {
	Path          string
	SizeBytes     int64
	SHA256        string
	Private       bool
	PrivacyDetail string
}

type windowsLayeredArchiveExpectation struct {
	identity  windowsLayeredArchiveFileIdentity
	sizeBytes int64
	sha256    string
}

type pendingWindowsLayeredArchive struct {
	temporaryPath string
	publishedPath string
	temporary     *os.File
	expectation   windowsLayeredArchiveExpectation
	report        windowsLayeredArchiveReport
}

var (
	windowsLayeredArchivePublishedCleanup = cleanupPublishedWindowsLayeredArchive
	windowsLayeredArchivePostPublishHook  func(string) error
)

func writeWindowsLayeredArchive(path string, generatedAt time.Time, files []windowsLayeredArchiveFile) (windowsLayeredArchiveReport, error) {
	pending, _, err := prepareWindowsLayeredArchive(filepath.Dir(path), filepath.Base(path), generatedAt, files)
	if err != nil {
		return windowsLayeredArchiveReport{}, err
	}
	return pending.publish(path)
}

func prepareWindowsLayeredArchive(temporaryDir, temporaryBase string, generatedAt time.Time, files []windowsLayeredArchiveFile) (_ *pendingWindowsLayeredArchive, _ windowsLayeredArchiveReport, resultErr error) {
	entries := append([]windowsLayeredArchiveFile(nil), files...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		normalizedName, ok := windowsSafeArchiveBasename(entry.name)
		if !ok {
			return nil, windowsLayeredArchiveReport{}, fmt.Errorf("unsafe Windows layered archive entry name %q", entry.name)
		}
		if _, ok := seen[normalizedName]; ok {
			return nil, windowsLayeredArchiveReport{}, fmt.Errorf("duplicate Windows layered archive entry name %q", entry.name)
		}
		seen[normalizedName] = struct{}{}
	}

	temporary, err := createWindowsLayeredArchiveTempFile(temporaryDir, temporaryBase)
	if err != nil {
		return nil, windowsLayeredArchiveReport{}, fmt.Errorf("create Windows layered archive: %w", err)
	}
	temporaryPath := temporary.Name()
	retainTemporary := false
	defer func() {
		if !retainTemporary {
			if err := temporary.Close(); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("close Windows layered archive during cleanup: %w", err))
			}
			if err := os.Remove(temporaryPath); err != nil && !os.IsNotExist(err) {
				resultErr = errors.Join(resultErr, fmt.Errorf("remove temporary Windows layered archive: %w", err))
			}
		}
	}()

	writer := zip.NewWriter(temporary)
	writer.RegisterCompressor(zip.Deflate, func(destination io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(destination, flate.BestCompression)
	})
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetModTime(generatedAt.UTC())
		header.SetMode(0o600)
		destination, err := writer.CreateHeader(header)
		if err != nil {
			closeErr := writer.Close()
			return nil, windowsLayeredArchiveReport{}, errors.Join(
				fmt.Errorf("create Windows layered archive entry %q: %w", entry.name, err),
				wrapWindowsLayeredArchiveCloseError(closeErr),
			)
		}
		if _, err := destination.Write(entry.content); err != nil {
			closeErr := writer.Close()
			return nil, windowsLayeredArchiveReport{}, errors.Join(
				fmt.Errorf("write Windows layered archive entry %q: %w", entry.name, err),
				wrapWindowsLayeredArchiveCloseError(closeErr),
			)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, windowsLayeredArchiveReport{}, fmt.Errorf("close Windows layered ZIP: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return nil, windowsLayeredArchiveReport{}, fmt.Errorf("sync Windows layered archive: %w", err)
	}
	info, err := temporary.Stat()
	if err != nil {
		return nil, windowsLayeredArchiveReport{}, fmt.Errorf("measure Windows layered archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, windowsLayeredArchiveReport{}, fmt.Errorf("Windows layered archive is not a regular file")
	}
	if info.Size() > maxWindowsLayeredHandoffBytes {
		return nil, windowsLayeredArchiveReport{}, fmt.Errorf("Windows layered archive size %d exceeds %d bytes", info.Size(), maxWindowsLayeredHandoffBytes)
	}
	digest, err := hashWindowsLayeredArchiveHandle(temporary)
	if err != nil {
		return nil, windowsLayeredArchiveReport{}, fmt.Errorf("hash Windows layered archive: %w", err)
	}
	identity, privacyDetail, err := validateWindowsLayeredArchiveHandle(temporary)
	if err != nil {
		return nil, windowsLayeredArchiveReport{}, fmt.Errorf("validate Windows layered archive before publication: %w", err)
	}
	report := windowsLayeredArchiveReport{
		SizeBytes:     info.Size(),
		SHA256:        digest,
		Private:       true,
		PrivacyDetail: privacyDetail,
	}
	retainTemporary = true
	return &pendingWindowsLayeredArchive{
		temporaryPath: temporaryPath,
		temporary:     temporary,
		expectation: windowsLayeredArchiveExpectation{
			identity:  identity,
			sizeBytes: info.Size(),
			sha256:    digest,
		},
		report: report,
	}, report, nil
}

func (pending *pendingWindowsLayeredArchive) publish(path string) (windowsLayeredArchiveReport, error) {
	if pending == nil || pending.temporary == nil {
		return windowsLayeredArchiveReport{}, fmt.Errorf("Windows layered archive publication has no guarded temporary file")
	}
	temporary := pending.temporary
	expectation := pending.expectation
	if _, err := os.Lstat(path); err == nil {
		return windowsLayeredArchiveReport{}, pending.publicationFailure(fmt.Errorf("Windows layered archive destination already exists: %s", path))
	} else if !os.IsNotExist(err) {
		return windowsLayeredArchiveReport{}, pending.publicationFailure(fmt.Errorf("inspect Windows layered archive destination: %w", err))
	}
	published, err := publishWindowsLayeredArchiveHandle(temporary, path)
	if published {
		pending.temporaryPath = ""
		pending.publishedPath = path
	}
	if err != nil {
		return windowsLayeredArchiveReport{}, pending.publicationFailure(fmt.Errorf("publish Windows layered archive: %w", err))
	}
	if windowsLayeredArchivePostPublishHook != nil {
		if err := windowsLayeredArchivePostPublishHook(path); err != nil {
			return windowsLayeredArchiveReport{}, pending.publicationFailure(fmt.Errorf("post-publication Windows layered archive check: %w", err))
		}
	}
	privacyDetail, err := validatePublishedWindowsLayeredArchive(path, expectation)
	if err != nil {
		return windowsLayeredArchiveReport{}, pending.publicationFailure(fmt.Errorf("validate published Windows layered archive: %w", err))
	}
	if err := temporary.Close(); err != nil {
		return windowsLayeredArchiveReport{}, pending.publicationFailure(fmt.Errorf("close validated Windows layered archive creation handle: %w", err))
	}
	pending.temporary = nil
	pending.temporaryPath = ""
	pending.publishedPath = ""
	report := pending.report
	report.Path = path
	report.PrivacyDetail = privacyDetail
	return report, nil
}

func (pending *pendingWindowsLayeredArchive) publicationFailure(validationErr error) error {
	cleanupErr := pending.discard()
	if cleanupErr != nil {
		cleanupErr = fmt.Errorf("clean up invalid published Windows layered archive: %w", cleanupErr)
	}
	return errors.Join(validationErr, cleanupErr)
}

func (pending *pendingWindowsLayeredArchive) discard() error {
	if pending == nil {
		return nil
	}
	var cleanupErrs []error
	if pending.temporary != nil {
		if err := pending.temporary.Truncate(0); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("truncate invalid Windows layered archive creation handle: %w", err))
		}
		if err := pending.temporary.Close(); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("close invalid Windows layered archive creation handle: %w", err))
		}
		pending.temporary = nil
	}
	cleanupPath := pending.publishedPath
	if cleanupPath == "" {
		cleanupPath = pending.temporaryPath
	}
	pending.publishedPath = ""
	pending.temporaryPath = ""
	if cleanupPath != "" {
		if err := windowsLayeredArchivePublishedCleanup(cleanupPath); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
	}
	return errors.Join(cleanupErrs...)
}

func hashWindowsLayeredArchiveHandle(file *os.File) (string, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func validatePublishedWindowsLayeredArchive(path string, expected windowsLayeredArchiveExpectation) (_ string, resultErr error) {
	file, err := openPublishedWindowsLayeredArchive(path)
	if err != nil {
		return "", fmt.Errorf("open published Windows layered archive: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close published Windows layered archive validation handle: %w", err))
		}
	}()
	identity, privacyDetail, err := validateWindowsLayeredArchiveHandle(file)
	if err != nil {
		return "", err
	}
	if !sameWindowsLayeredArchiveFileIdentity(identity, expected.identity) {
		return "", fmt.Errorf("published Windows layered archive file identity changed")
	}
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("measure published Windows layered archive: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() != expected.sizeBytes {
		return "", fmt.Errorf("published Windows layered archive size or type changed")
	}
	digest, err := hashWindowsLayeredArchiveHandle(file)
	if err != nil {
		return "", fmt.Errorf("hash published Windows layered archive: %w", err)
	}
	if digest != expected.sha256 {
		return "", fmt.Errorf("published Windows layered archive digest changed")
	}
	return privacyDetail, nil
}

func wrapWindowsLayeredArchiveCloseError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("close Windows layered ZIP after entry failure: %w", err)
}

func cleanupPublishedWindowsLayeredArchive(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove published Windows layered archive: %w", err)
	}
	return nil
}

func windowsSafeArchiveBasename(name string) (string, bool) {
	if name == "" || name == "." || name == ".." || filepath.IsAbs(name) || filepath.Base(name) != name || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return "", false
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f || strings.ContainsRune(`/\:<>"|?*`, character) {
			return "", false
		}
	}

	normalizedName := strings.ToUpper(strings.TrimRight(name, ". "))
	deviceName := normalizedName
	if extension := strings.IndexByte(deviceName, '.'); extension >= 0 {
		deviceName = deviceName[:extension]
	}
	deviceName = strings.TrimRight(deviceName, " ")
	deviceName = strings.Map(func(character rune) rune {
		switch character {
		case '¹':
			return '1'
		case '²':
			return '2'
		case '³':
			return '3'
		default:
			return character
		}
	}, deviceName)
	if deviceName == "CON" || deviceName == "PRN" || deviceName == "AUX" || deviceName == "NUL" || deviceName == "CLOCK$" {
		return "", false
	}
	if len(deviceName) == 4 && (strings.HasPrefix(deviceName, "COM") || strings.HasPrefix(deviceName, "LPT")) && deviceName[3] >= '1' && deviceName[3] <= '9' {
		return "", false
	}
	return normalizedName, true
}
