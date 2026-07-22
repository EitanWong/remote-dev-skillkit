//go:build windows

package bootstrapcmd

import (
	"fmt"
	"os"

	"github.com/EitanWong/remote-dev-skillkit/internal/tunnel"
)

func openVerifiedLayeredRuntime(runtime layeredRuntime) (*os.File, error) {
	if err := tunnel.ValidateProtectedParentChain(runtime.path); err != nil {
		return nil, err
	}
	file, err := tunnel.OpenVerifiedProtectedExecutableSHA256(runtime.path, runtime.size, runtime.digest)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Size() != runtime.size {
		_ = file.Close()
		return nil, fmt.Errorf("protected layered runtime size mismatch")
	}
	return file, nil
}
