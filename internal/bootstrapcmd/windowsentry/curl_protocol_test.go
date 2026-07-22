package windowsentry

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
)

func TestWindowsEntryCurlRejectsUnsafeURLs(t *testing.T) {
	for _, rawURL := range []string{
		"http://downloads.example.test/core.exe",
		"https://user:secret@downloads.example.test/core.exe",
		"https://downloads.example.test/core.exe?token=secret",
		"https://downloads.example.test/core.exe#fragment",
		"https://downloads.example.test/{core,helper}.exe",
		"https://downloads.example.test/[a-z].exe",
		"https:/missing-host.exe",
		"https://:443/core.exe",
		"https://downloads.example.test:bad/core.exe",
		"https://downloads.example.test:65536/core.exe",
		"https://[::1/core.exe",
	} {
		if _, err := validateCurlURL(rawURL); err == nil {
			t.Fatalf("unsafe curl URL %q was accepted", rawURL)
		}
	}
	if _, err := validateCurlURL("https://downloads.example.test/core.exe"); err != nil {
		t.Fatalf("safe HTTPS URL rejected: %v", err)
	}
}

func TestWindowsEntryCurlArgumentsKeepTransportByteOnly(t *testing.T) {
	request := assetdownload.TransportRequest{
		URL:      "https://downloads.example.test/core.exe",
		Offset:   4096,
		MaxBytes: 16384,
	}
	args, err := curlArguments(request, `C:\private\headers.tmp`)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	if len(args) == 0 || args[0] != "--disable" {
		t.Fatalf("curl must disable user config before every other option: %q", args)
	}
	for _, want := range []string{
		"--globoff",
		"--proto =https",
		"--max-redirs 0",
		"--connect-timeout 10",
		"--max-time 120",
		"--speed-limit 1024",
		"--speed-time 30",
		"--max-filesize 16384",
		"--range 4096-",
		"--suppress-connect-headers",
		"--include",
		"--output -",
		"--url https://downloads.example.test/core.exe",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("curl arguments missing %q: %s", want, joined)
		}
	}
	for _, forbidden := range []string{"--dump-header", "--location", "--retry", "--remote-name", "--etag", "--remove-on-error"} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("curl transport took ownership of forbidden behavior %q: %s", forbidden, joined)
		}
	}
	if _, err := curlArguments(assetdownload.TransportRequest{URL: request.URL, Offset: 2, MaxBytes: 1}, `C:\private\headers.tmp`); err == nil {
		t.Fatal("curl accepted an offset beyond the signed byte bound")
	}
	if _, err := curlArguments(assetdownload.TransportRequest{URL: request.URL, MaxBytes: 0}, `C:\private\headers.tmp`); err == nil {
		t.Fatal("curl accepted an unbounded request")
	}
}

func TestWindowsEntryCurlRejectsUnsafeHeaderPaths(t *testing.T) {
	request := assetdownload.TransportRequest{URL: "https://downloads.example.test/core.exe", MaxBytes: 4}
	for _, headerPath := range []string{
		"headers.tmp",
		`\\server\share\headers.tmp`,
		`1:\private\headers.tmp`,
		`C:\private\..\headers.tmp`,
		`C:\private\headers.tmp:ads`,
	} {
		if _, err := curlArguments(request, headerPath); err == nil {
			t.Errorf("unsafe curl header path %q was accepted", headerPath)
		}
	}
}

func TestWindowsEntryCurlRejectsConflictingContentLengths(t *testing.T) {
	content := []byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\nContent-Length: 5\r\n\r\n")
	if _, _, err := parseCurlHeaders(content); err == nil {
		t.Fatal("curl headers with conflicting content lengths were accepted")
	}
}

func TestWindowsEntryCurlBodyWriterEnforcesHardBound(t *testing.T) {
	var headers bytes.Buffer
	var body bytes.Buffer
	writer := newCurlResponseWriter(&headers, &body, 4)
	responseHeaders := "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\n"
	written, err := writer.Write([]byte(responseHeaders + "12345"))
	if err == nil || written != len(responseHeaders)+4 || body.String() != "1234" {
		t.Fatalf("bounded curl response write = %d, %v, %q", written, err, body.String())
	}
}

func TestWindowsEntryCurlResponseWriterSplitsHeadersAcrossWrites(t *testing.T) {
	var headers bytes.Buffer
	var body bytes.Buffer
	writer := newCurlResponseWriter(&headers, &body, 4)
	for _, chunk := range []string{
		"HTTP/1.1 200 OK\r\nContent-Length: 4\r",
		"\n\r",
		"\nbo",
		"dy",
	} {
		written, err := writer.Write([]byte(chunk))
		if err != nil || written != len(chunk) {
			t.Fatalf("curl response chunk write = %d, %v, want %d", written, err, len(chunk))
		}
	}
	if err := writer.finish(); err != nil {
		t.Fatal(err)
	}
	if got, want := headers.String(), "HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\n"; got != want {
		t.Fatalf("curl response headers = %q, want %q", got, want)
	}
	if got := body.String(); got != "body" {
		t.Fatalf("curl response body = %q, want body", got)
	}
}

func TestWindowsEntryCurlResponseWriterAcceptsEarlyHintsBeforeFinalResponse(t *testing.T) {
	var headers bytes.Buffer
	var body bytes.Buffer
	writer := newCurlResponseWriter(&headers, &body, 4)
	response := "HTTP/1.1 103 Early Hints\r\nLink: </core>; rel=preload\r\n\r\n" +
		"HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\nbody"
	for _, chunk := range []string{response[:37], response[37:61], response[61:]} {
		written, err := writer.Write([]byte(chunk))
		if err != nil || written != len(chunk) {
			t.Fatalf("curl response chunk write = %d, %v, want %d", written, err, len(chunk))
		}
	}
	if err := writer.finish(); err != nil {
		t.Fatal(err)
	}
	status, contentLength, err := parseCurlHeaders(headers.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 || contentLength != 4 {
		t.Fatalf("final curl response = status %d length %d", status, contentLength)
	}
	if got := body.String(); got != "body" {
		t.Fatalf("curl response body = %q, want body", got)
	}
}

func TestWindowsEntryCurlResponseWriterEnforcesHeaderBound(t *testing.T) {
	var headers bytes.Buffer
	var body bytes.Buffer
	writer := newCurlResponseWriter(&headers, &body, 1)
	written, err := writer.Write(bytes.Repeat([]byte("h"), int(maxCurlHeaderBytes)+1))
	if err == nil || int64(written) != maxCurlHeaderBytes {
		t.Fatalf("oversized curl header write = %d, %v", written, err)
	}
	if headers.Len() != 0 || body.Len() != 0 {
		t.Fatalf("oversized curl response reached files: headers=%d body=%d", headers.Len(), body.Len())
	}
}

func TestWindowsEntryCurlBodyWriterRejectsShortWrite(t *testing.T) {
	writer := &boundedWriter{writer: shortNilWriter{}, remaining: 4}
	written, err := writer.Write([]byte("body"))
	if written != 3 || err != io.ErrShortWrite {
		t.Fatalf("short curl body write = %d, %v, want 3, %v", written, err, io.ErrShortWrite)
	}
}

type shortNilWriter struct{}

func (shortNilWriter) Write(content []byte) (int, error) {
	return len(content) - 1, nil
}
