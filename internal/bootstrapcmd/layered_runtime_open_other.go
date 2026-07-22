//go:build !windows

package bootstrapcmd

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"io"
	"math"
	"os"
)

func openVerifiedLayeredRuntime(runtime layeredRuntime) (_ *os.File, returnErr error) {
	if runtime.size < 1 || runtime.size == math.MaxInt64 {
		return nil, fmt.Errorf("layered runtime size must be between 1 and %d", math.MaxInt64-1)
	}
	file, err := os.Open(runtime.path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if returnErr != nil {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	pathInfo, err := os.Lstat(runtime.path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, pathInfo) || info.Size() != runtime.size {
		return nil, fmt.Errorf("layered runtime path or size is invalid")
	}
	digest := sha256.New()
	written, err := io.Copy(digest, io.LimitReader(file, runtime.size+1))
	if err != nil {
		return nil, err
	}
	if written != runtime.size || subtle.ConstantTimeCompare(digest.Sum(nil), runtime.digest[:]) != 1 {
		return nil, fmt.Errorf("layered runtime digest or size mismatch")
	}
	postInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	postPathInfo, err := os.Lstat(runtime.path)
	if err != nil {
		return nil, err
	}
	if !postInfo.Mode().IsRegular() || postPathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(postInfo, postPathInfo) || postInfo.Size() != runtime.size {
		return nil, fmt.Errorf("layered runtime changed while opening verified handle")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return file, nil
}
