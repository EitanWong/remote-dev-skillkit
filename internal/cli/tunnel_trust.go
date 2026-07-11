package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

const (
	localhostRunOfficialKnownHostsLine = "localhost.run ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC3lJnhW1oCXuAYV9IBdcJA+Vx7AHL5S/ZQvV2fhceOAPgO2kNQZla6xvUwoE4iw8lYu3zoE1KtieCU9yInWOVI6W/wFaT/ETH1tn55T2FVsK/zaxPiHZVJGLPPdEEid0vS2p1JDfc9onZ0pNSHLl1QusIOeMUyZ2bUMMLLgw46KOT9S3s/LmxgoJ3PocVUn5rVXz/Dng7Y8jYNe4IFrZOAUsi7hNBa+OYja6ceefpDvNDEJ1BdhbYfGolBdNA7f+FNl0kfaWru4Cblr843wBe2ckO/sNqgeAMXO/qH+SSgQxUXF2AgAw+TGp3yCIyYoOPvOgvcPsQziJLmDbUuQpnH"
	localhostRunTrustHost              = "localhost.run"
	localhostRunTrustPort              = 22
	localhostRunTrustFingerprint       = "SHA256:FV8IMJ4IYjYUTnd6on7PqbRjaZf4c1EhhEBgeUdE94I"
	localhostRunTrustSourceCommit      = "9f499be7ece07d59ed927edbcfa6860ee7bcb853"
	localhostRunTrustSourceURL         = "https://github.com/localhost-run/client-service/blob/9f499be7ece07d59ed927edbcfa6860ee7bcb853/linux/systemd/localhost.run.service"
	localhostRunTrustReviewedAt        = "2026-07-11"
)

type providerTrustAnchor struct {
	ProviderID   string
	Host         string
	KeyLine      string
	Fingerprint  string
	SourceCommit string
	SourceURL    string
	ReviewedAt   string
	Port         int
}

var localhostRunTrustAnchor = providerTrustAnchor{
	ProviderID:   tunnel.ProviderLocalhostRun,
	Host:         localhostRunTrustHost,
	Port:         localhostRunTrustPort,
	KeyLine:      localhostRunOfficialKnownHostsLine,
	Fingerprint:  localhostRunTrustFingerprint,
	SourceCommit: localhostRunTrustSourceCommit,
	SourceURL:    localhostRunTrustSourceURL,
	ReviewedAt:   localhostRunTrustReviewedAt,
}

func validateProviderTrustAnchor(anchor providerTrustAnchor) error {
	if anchor.ProviderID != tunnel.ProviderLocalhostRun || anchor.Host != localhostRunTrustHost || anchor.Port != localhostRunTrustPort ||
		anchor.KeyLine != localhostRunOfficialKnownHostsLine || anchor.Fingerprint != localhostRunTrustFingerprint ||
		anchor.SourceCommit != localhostRunTrustSourceCommit || anchor.SourceURL != localhostRunTrustSourceURL ||
		anchor.ReviewedAt != localhostRunTrustReviewedAt {
		return errors.New("provider trust anchor is not an approved localhost.run identity")
	}
	host, err := canonicalSSHDestinationHost(anchor.Host)
	if err != nil || host != anchor.Host || strings.Contains(anchor.Host, "@") {
		return errors.New("provider trust anchor has an invalid host")
	}
	if anchor.Port < 1 || anchor.Port > 65535 {
		return errors.New("provider trust anchor has an invalid port")
	}
	fields := strings.Fields(anchor.KeyLine)
	if len(fields) != 3 || strings.Join(fields, " ") != anchor.KeyLine {
		return errors.New("provider trust anchor key line is not canonical")
	}
	expectedHost := anchor.Host
	if anchor.Port != 22 {
		expectedHost = "[" + anchor.Host + "]:" + fmt.Sprint(anchor.Port)
	}
	if fields[0] != expectedHost || fields[1] != "ssh-rsa" {
		return errors.New("provider trust anchor key identity does not match")
	}
	keyBlob, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil || len(keyBlob) == 0 || base64.StdEncoding.EncodeToString(keyBlob) != fields[2] {
		return errors.New("provider trust anchor key is not canonical base64")
	}
	wireType, ok := sshWireKeyType(keyBlob)
	if !ok || wireType != fields[1] {
		return errors.New("provider trust anchor key type does not match its blob")
	}
	digest := sha256.Sum256(keyBlob)
	fingerprint := "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])
	if anchor.Fingerprint != fingerprint {
		return errors.New("provider trust anchor fingerprint does not match")
	}
	if !isLowerHexCommit(anchor.SourceCommit) {
		return errors.New("provider trust anchor source commit is invalid")
	}
	if err := validateProviderTrustSourceURL(anchor.SourceURL, anchor.SourceCommit); err != nil {
		return err
	}
	reviewedAt, err := time.Parse("2006-01-02", anchor.ReviewedAt)
	if err != nil || reviewedAt.Format("2006-01-02") != anchor.ReviewedAt {
		return errors.New("provider trust anchor review date is invalid")
	}
	return nil
}

func materializeProviderKnownHosts(root string, anchor providerTrustAnchor) (string, error) {
	if err := validateProviderTrustAnchor(anchor); err != nil {
		return "", err
	}
	if root == "" || strings.TrimSpace(root) != root || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", errors.New("provider trust root is unsafe")
	}
	protectedRoot, err := prepareManagedToolRoot(root)
	if err != nil {
		return "", errors.New("provider trust root is unsafe")
	}
	trustDirectory, err := prepareManagedToolRoot(filepath.Join(protectedRoot, "provider-trust", anchor.ProviderID))
	if err != nil {
		return "", errors.New("provider trust directory is unsafe")
	}
	path := filepath.Join(trustDirectory, "known_hosts")
	content := []byte(anchor.KeyLine + "\n")
	digest := sha256.Sum256(content)
	if _, err := os.Lstat(path); err == nil {
		if err := validateProviderKnownHostsSnapshot(path, anchor, content, digest); err != nil {
			return "", err
		}
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", errors.New("inspect provider trust snapshot")
	}

	temporary, err := os.CreateTemp(trustDirectory, ".known_hosts.stage-*")
	if err != nil {
		return "", errors.New("create provider trust snapshot")
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", errors.New("protect provider trust snapshot")
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return "", errors.New("write provider trust snapshot")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", errors.New("sync provider trust snapshot")
	}
	if err := temporary.Close(); err != nil {
		return "", errors.New("close provider trust snapshot")
	}
	if err := tunnel.VerifyProtectedRegularFileSHA256(temporaryPath, maxKnownHostsBytes, digest); err != nil {
		return "", errors.New("verify provider trust snapshot")
	}
	published, err := publishManagedToolNoReplace(temporaryPath, path)
	if err != nil {
		return "", errors.New("publish provider trust snapshot")
	}
	removeTemporary = false
	if err := syncSupportSessionArtifactDirectory(path); err != nil {
		return "", errors.New("sync provider trust snapshot directory")
	}
	if err := validateProviderKnownHostsSnapshot(path, anchor, content, digest); err != nil {
		if published {
			return "", errors.New("published provider trust snapshot failed validation")
		}
		return "", errors.New("competing provider trust snapshot failed validation")
	}
	return path, nil
}

func validateProviderKnownHostsSnapshot(path string, anchor providerTrustAnchor, expected []byte, digest [sha256.Size]byte) error {
	file, err := tunnel.OpenVerifiedProtectedRegularFileSHA256(path, maxKnownHostsBytes, digest)
	if err != nil {
		return errors.New("provider trust snapshot integrity failure")
	}
	content, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(content, expected) {
		return errors.New("provider trust snapshot integrity failure")
	}
	if err := validateKnownHostsFile(path, anchor.Host, anchor.Port); err != nil {
		return errors.New("provider trust snapshot validation failure")
	}
	return nil
}

func sshWireKeyType(blob []byte) (string, bool) {
	if len(blob) < 4 {
		return "", false
	}
	length := uint64(binary.BigEndian.Uint32(blob[:4]))
	if length == 0 || length > uint64(len(blob)-4) {
		return "", false
	}
	return string(blob[4 : 4+int(length)]), true
}

func isLowerHexCommit(value string) bool {
	if len(value) != 40 || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 20
}

func validateProviderTrustSourceURL(rawURL, commit string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil ||
		parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.Contains(rawURL, "#") {
		return errors.New("provider trust anchor source URL is not immutable HTTPS")
	}
	expectedPath := "/localhost-run/client-service/blob/" + commit + "/linux/systemd/localhost.run.service"
	if parsed.EscapedPath() != expectedPath {
		return errors.New("provider trust anchor source URL does not match its commit")
	}
	return nil
}
