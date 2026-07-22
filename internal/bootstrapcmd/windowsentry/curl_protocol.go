package windowsentry

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
)

const maxCurlHeaderBytes int64 = 64 << 10

func validateCurlURL(rawURL string) (httpsURL, error) {
	if strings.ContainsAny(rawURL, "{}[]") {
		return httpsURL{}, fmt.Errorf("curl URL contains unsupported expansion syntax")
	}
	return strictHTTPSURL(rawURL)
}

func curlArguments(request assetdownload.TransportRequest, headerPath string) ([]string, error) {
	parsed, err := validateCurlURL(request.URL)
	if err != nil {
		return nil, err
	}
	if request.MaxBytes <= 0 {
		return nil, fmt.Errorf("curl request must have a signed byte bound")
	}
	if request.Offset < 0 || request.Offset > request.MaxBytes {
		return nil, fmt.Errorf("curl request offset exceeds the signed byte bound")
	}
	if !windowsAbsoluteLocalFilePath(headerPath) {
		return nil, fmt.Errorf("curl headers must use an absolute private path")
	}
	args := []string{
		"--disable",
		"--globoff",
		"--silent",
		"--show-error",
		"--proto", "=https",
		"--max-redirs", "0",
		"--connect-timeout", "10",
		"--max-time", "120",
		"--speed-limit", "1024",
		"--speed-time", "30",
		"--max-filesize", strconv.FormatInt(request.MaxBytes, 10),
	}
	if request.Offset > 0 {
		args = append(args, "--range", strconv.FormatInt(request.Offset, 10)+"-")
	}
	args = append(args,
		"--suppress-connect-headers",
		"--include",
		"--output", "-",
		"--url", parsed.String(),
	)
	return args, nil
}

func windowsAbsoluteLocalFilePath(value string) bool {
	if len(value) < 4 || !isASCIIAlpha(value[0]) || value[1] != ':' || value[2] != '\\' && value[2] != '/' || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	remainder := strings.ReplaceAll(value[3:], "/", `\`)
	if remainder == "" || strings.Contains(remainder, ":") {
		return false
	}
	for _, component := range strings.Split(remainder, `\`) {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

func isASCIIAlpha(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func parseCurlHeaders(content []byte) (int, int64, error) {
	normalized := strings.ReplaceAll(string(content), "\r\n", "\n")
	blocks := strings.Split(normalized, "\n\n")
	for index := len(blocks) - 1; index >= 0; index-- {
		lines := strings.Split(strings.TrimSpace(blocks[index]), "\n")
		if len(lines) == 0 || !strings.HasPrefix(lines[0], "HTTP/") {
			continue
		}
		statusFields := strings.Fields(lines[0])
		if len(statusFields) < 2 {
			return 0, 0, fmt.Errorf("curl returned a malformed HTTP status")
		}
		status, err := strconv.Atoi(statusFields[1])
		if err != nil || status < 100 || status > 999 {
			return 0, 0, fmt.Errorf("curl returned a malformed HTTP status")
		}
		contentLength := int64(-1)
		for _, line := range lines[1:] {
			name, value, found := strings.Cut(line, ":")
			if !found || !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
				continue
			}
			parsedLength, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if parseErr != nil || parsedLength < 0 {
				return 0, 0, fmt.Errorf("curl returned an invalid content length")
			}
			if contentLength >= 0 && contentLength != parsedLength {
				return 0, 0, fmt.Errorf("curl returned conflicting content lengths")
			}
			contentLength = parsedLength
		}
		return status, contentLength, nil
	}
	return 0, 0, fmt.Errorf("curl returned no HTTP response headers")
}

type boundedWriter struct {
	writer    io.Writer
	remaining int64
}

func (writer *boundedWriter) Write(content []byte) (int, error) {
	if int64(len(content)) > writer.remaining {
		if writer.remaining <= 0 {
			return 0, fmt.Errorf("curl response body exceeds its signed byte bound")
		}
		limit := writer.remaining
		written, err := writer.writer.Write(content[:limit])
		writer.remaining -= int64(written)
		if err != nil {
			return written, err
		}
		if int64(written) != limit {
			return written, io.ErrShortWrite
		}
		return written, fmt.Errorf("curl response body exceeds its signed byte bound")
	}
	written, err := writer.writer.Write(content)
	writer.remaining -= int64(written)
	if err == nil && written != len(content) {
		err = io.ErrShortWrite
	}
	return written, err
}

type curlResponseWriter struct {
	headerWriter      io.Writer
	bodyWriter        *boundedWriter
	headerContent     []byte
	currentBlockStart int
	finalHeaders      bool
}

func newCurlResponseWriter(headerWriter, bodyWriter io.Writer, maxBodyBytes int64) *curlResponseWriter {
	return &curlResponseWriter{
		headerWriter: headerWriter,
		bodyWriter: &boundedWriter{
			writer:    bodyWriter,
			remaining: maxBodyBytes,
		},
	}
}

func (writer *curlResponseWriter) Write(content []byte) (int, error) {
	if writer.finalHeaders {
		return writer.bodyWriter.Write(content)
	}
	written := 0
	for written < len(content) && !writer.finalHeaders {
		if int64(len(writer.headerContent)) >= maxCurlHeaderBytes {
			return written, fmt.Errorf("curl response headers exceed %d bytes", maxCurlHeaderBytes)
		}
		writer.headerContent = append(writer.headerContent, content[written])
		written++
		if !bytes.HasSuffix(writer.headerContent, []byte("\r\n\r\n")) {
			continue
		}
		status, _, err := parseCurlHeaders(writer.headerContent[writer.currentBlockStart:])
		if err != nil {
			return written, err
		}
		writer.currentBlockStart = len(writer.headerContent)
		if status >= 100 && status < 200 {
			continue
		}
		if _, err := io.Copy(writer.headerWriter, bytes.NewReader(writer.headerContent)); err != nil {
			return written, fmt.Errorf("write curl response headers: %w", err)
		}
		writer.headerContent = nil
		writer.finalHeaders = true
	}
	if written == len(content) {
		return written, nil
	}
	bodyWritten, err := writer.bodyWriter.Write(content[written:])
	return written + bodyWritten, err
}

func (writer *curlResponseWriter) finish() error {
	if !writer.finalHeaders {
		return fmt.Errorf("curl returned no final HTTP response headers")
	}
	return nil
}
