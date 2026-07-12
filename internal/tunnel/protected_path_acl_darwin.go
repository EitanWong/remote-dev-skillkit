//go:build darwin

package tunnel

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	darwinFileSecurityMagic = 0x012cc16d
	darwinNoACL             = ^uint32(0)
	darwinACLPermit         = 1
	darwinACLDeny           = 2
	darwinMaxACLEntries     = 128
)

func validateProtectedExtendedACL(file *os.File, rejectedPermitRights uint32) error {
	attributes := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_RETURNED_ATTRS | unix.ATTR_CMN_EXTENDED_SECURITY,
	}
	buffer := make([]byte, 4096)
	_, _, errno := unix.Syscall6(
		unix.SYS_FGETATTRLIST,
		file.Fd(),
		uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		0,
		0,
	)
	runtime.KeepAlive(file)
	if errno != 0 {
		return fmt.Errorf("inspect protected path Darwin ACL: %w", errno)
	}
	return validateDarwinExtendedSecurity(buffer, rejectedPermitRights)
}

func validateDarwinExtendedSecurity(buffer []byte, rejectedPermitRights uint32) error {
	const (
		lengthBytes       = 4
		returnedAttrsSize = 20
		attributeRefSize  = 8
		attributeRefStart = lengthBytes + returnedAttrsSize
		fileSecuritySize  = 44
		aceSize           = 24
	)
	if len(buffer) < attributeRefStart+attributeRefSize {
		return fmt.Errorf("inspect protected path Darwin ACL: truncated attribute response")
	}
	responseLength := int(binary.LittleEndian.Uint32(buffer[:4]))
	if responseLength < attributeRefStart || responseLength > len(buffer) {
		return fmt.Errorf("inspect protected path Darwin ACL: invalid attribute response length")
	}
	returnedCommon := binary.LittleEndian.Uint32(buffer[4:8])
	if returnedCommon&unix.ATTR_CMN_EXTENDED_SECURITY == 0 {
		return nil
	}
	offset := int(int32(binary.LittleEndian.Uint32(buffer[attributeRefStart : attributeRefStart+4])))
	securityLength := int(binary.LittleEndian.Uint32(buffer[attributeRefStart+4 : attributeRefStart+8]))
	securityStart := attributeRefStart + offset
	securityEnd := securityStart + securityLength
	if securityStart < attributeRefStart+attributeRefSize || securityLength < fileSecuritySize || securityEnd < securityStart || securityEnd > responseLength {
		return fmt.Errorf("inspect protected path Darwin ACL: invalid extended security bounds")
	}
	security := buffer[securityStart:securityEnd]
	if binary.LittleEndian.Uint32(security[:4]) != darwinFileSecurityMagic {
		return fmt.Errorf("inspect protected path Darwin ACL: invalid file security header")
	}
	entryCount := binary.LittleEndian.Uint32(security[36:40])
	if entryCount == darwinNoACL || entryCount == 0 {
		return nil
	}
	if entryCount > darwinMaxACLEntries || fileSecuritySize+int(entryCount)*aceSize > len(security) {
		return fmt.Errorf("inspect protected path Darwin ACL: invalid entry count")
	}
	for index := 0; index < int(entryCount); index++ {
		entry := fileSecuritySize + index*aceSize
		flags := binary.LittleEndian.Uint32(security[entry+16 : entry+20])
		rights := binary.LittleEndian.Uint32(security[entry+20 : entry+24])
		switch flags & 0xf {
		case darwinACLPermit:
			if rights&rejectedPermitRights != 0 {
				return fmt.Errorf("protected path Darwin ACL grants disallowed access")
			}
		case darwinACLDeny:
		default:
			return fmt.Errorf("protected path Darwin ACL contains an unsupported entry")
		}
	}
	return nil
}
