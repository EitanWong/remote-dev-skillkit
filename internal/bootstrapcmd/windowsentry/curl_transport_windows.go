//go:build windows

package windowsentry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/EitanWong/remote-dev-skillkit/internal/assetdownload"
)

type CurlTransport struct {
	SystemRoot string
	Executable string
}

func defaultTransport() (assetdownload.Transport, error) {
	systemRoot, err := actualWindowsDirectory()
	if err != nil {
		return nil, err
	}
	transport := CurlTransport{SystemRoot: systemRoot}
	if _, err := windowsSystemCurlPath(transport.SystemRoot, ""); err != nil {
		return nil, err
	}
	return transport, nil
}

func (transport CurlTransport) Fetch(ctx context.Context, request assetdownload.TransportRequest) (_ assetdownload.TransportResponse, resultErr error) {
	actualRoot, err := actualWindowsDirectory()
	if err != nil {
		return assetdownload.TransportResponse{}, err
	}
	systemRoot := strings.TrimSpace(transport.SystemRoot)
	if systemRoot == "" {
		systemRoot = actualRoot
	}
	if !strings.EqualFold(filepath.Clean(systemRoot), actualRoot) {
		return assetdownload.TransportResponse{}, fmt.Errorf("curl SystemRoot does not match the Windows system directory")
	}
	executable, err := windowsSystemCurlPath(systemRoot, transport.Executable)
	if err != nil {
		return assetdownload.TransportResponse{}, err
	}
	if err := validateWindowsSystemExecutable(executable); err != nil {
		return assetdownload.TransportResponse{}, err
	}
	tempDir, err := createPrivateTemporaryDirectory("rdev-bootstrap-curl-")
	if err != nil {
		return assetdownload.TransportResponse{}, err
	}
	keepTemp := false
	var header *os.File
	var body *os.File
	defer func() {
		if !keepTemp {
			if header != nil {
				resultErr = errors.Join(resultErr, header.Close())
			}
			if body != nil {
				resultErr = errors.Join(resultErr, body.Close())
			}
			resultErr = errors.Join(resultErr, os.RemoveAll(tempDir))
		}
	}()
	headerPath := filepath.Join(tempDir, "headers.tmp")
	bodyPath := filepath.Join(tempDir, "body.tmp")
	header, err = createPrivateTemporaryFile(tempDir, "headers.tmp")
	if err != nil {
		return assetdownload.TransportResponse{}, err
	}
	body, err = createPrivateTemporaryFile(tempDir, "body.tmp")
	if err != nil {
		return assetdownload.TransportResponse{}, err
	}
	args, err := curlArguments(request, headerPath)
	if err != nil {
		return assetdownload.TransportResponse{}, err
	}
	command := exec.CommandContext(ctx, executable, args...)
	responseWriter := newCurlResponseWriter(header, body, request.MaxBytes)
	command.Stdout = responseWriter
	command.Stderr = io.Discard
	runErr := command.Run()
	if runErr != nil {
		return assetdownload.TransportResponse{}, fmt.Errorf("system curl request failed: %w", runErr)
	}
	if err := responseWriter.finish(); err != nil {
		return assetdownload.TransportResponse{}, err
	}
	if err := header.Sync(); err != nil {
		return assetdownload.TransportResponse{}, err
	}
	if err := body.Sync(); err != nil {
		return assetdownload.TransportResponse{}, err
	}
	if err := validatePrivateTemporaryFile(header, headerPath, maxCurlHeaderBytes); err != nil {
		return assetdownload.TransportResponse{}, err
	}
	if err := validatePrivateTemporaryFile(body, bodyPath, request.MaxBytes); err != nil {
		return assetdownload.TransportResponse{}, err
	}
	if _, err := header.Seek(0, io.SeekStart); err != nil {
		return assetdownload.TransportResponse{}, err
	}
	headerContent, err := io.ReadAll(io.LimitReader(header, maxCurlHeaderBytes+1))
	if err != nil {
		return assetdownload.TransportResponse{}, err
	}
	if int64(len(headerContent)) > maxCurlHeaderBytes {
		return assetdownload.TransportResponse{}, fmt.Errorf("curl response headers exceed %d bytes", maxCurlHeaderBytes)
	}
	status, contentLength, err := parseCurlHeaders(headerContent)
	if err != nil {
		return assetdownload.TransportResponse{}, err
	}
	if err := header.Close(); err != nil {
		return assetdownload.TransportResponse{}, err
	}
	header = nil
	if _, err := body.Seek(0, io.SeekStart); err != nil {
		return assetdownload.TransportResponse{}, err
	}
	keepTemp = true
	return assetdownload.TransportResponse{
		StatusCode:    status,
		ContentLength: contentLength,
		Body:          &temporaryBody{File: body, dir: tempDir},
	}, nil
}

func windowsSystemCurlPath(systemRoot, configured string) (string, error) {
	root := strings.TrimSpace(systemRoot)
	cleanRoot := filepath.Clean(root)
	volume := filepath.VolumeName(cleanRoot)
	if root == "" || root != cleanRoot || !filepath.IsAbs(cleanRoot) || strings.HasPrefix(cleanRoot, `\\`) || len(volume) != 2 || volume[1] != ':' {
		return "", fmt.Errorf("SystemRoot must be an absolute local drive path")
	}
	expected := filepath.Join(cleanRoot, "System32", "curl.exe")
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return expected, nil
	}
	if filepath.Clean(configured) != configured || !strings.EqualFold(configured, expected) {
		return "", fmt.Errorf("curl executable must be the System32 copy")
	}
	return expected, nil
}

type temporaryBody struct {
	*os.File
	dir string
}

func (body *temporaryBody) Close() error {
	closeErr := body.File.Close()
	removeErr := os.RemoveAll(body.dir)
	return errors.Join(closeErr, removeErr)
}
