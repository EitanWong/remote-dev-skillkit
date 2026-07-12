package tunnel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const MaxProtectedJSONBytes = 1 << 20

// ReadProtectedJSONFile opens path before validating and strictly decoding it.
// Platform-specific permission checks inspect the same opened file handle that
// is later decoded.
func ReadProtectedJSONFile(path string, destination any) error {
	file, err := openProtectedJSONFile(path)
	if err != nil {
		return fmt.Errorf("open protected JSON file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat opened protected JSON file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("protected JSON input must be a regular file")
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect protected JSON path: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) {
		return fmt.Errorf("protected JSON input must not be a symlink or replaced path")
	}
	if err := validateProtectedJSONPermissions(file, info); err != nil {
		return err
	}

	content, err := io.ReadAll(io.LimitReader(file, MaxProtectedJSONBytes+1))
	if err != nil {
		return fmt.Errorf("read protected JSON file: %w", err)
	}
	if len(content) > MaxProtectedJSONBytes {
		return fmt.Errorf("protected JSON input exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode protected JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode protected JSON: trailing JSON value is not allowed")
		}
		return fmt.Errorf("decode protected JSON trailing data: %w", err)
	}
	return nil
}
