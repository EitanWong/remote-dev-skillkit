//go:build !windows

package tunnel

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openProtectedJSONFile(path string) (*os.File, error) {
	fileDescriptor, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fileDescriptor), path)
	if file == nil {
		unix.Close(fileDescriptor)
		return nil, fmt.Errorf("wrap protected JSON file descriptor")
	}
	return file, nil
}
