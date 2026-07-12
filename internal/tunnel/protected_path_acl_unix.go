//go:build !windows

package tunnel

const (
	protectedACLAnyPermit = ^uint32(0)

	protectedACLReplacementPermit = (1 << 2) | // write data / add file
		(1 << 4) | // delete
		(1 << 5) | // append data / add subdirectory
		(1 << 6) | // delete child
		(1 << 8) | // write attributes
		(1 << 10) | // write extended attributes
		(1 << 12) | // write security
		(1 << 13) | // take ownership
		(1 << 21) | // generic all
		(1 << 23) // generic write
)
