//go:build darwin && cgo

package protectedstore

/*
#cgo CFLAGS: -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const errSecItemNotFoundStatus C.OSStatus = -25300

func platformKeychainBackend() keychainBackend {
	return securityFrameworkBackend{}
}

type securityFrameworkBackend struct{}

func (securityFrameworkBackend) Load(service, account string) ([]byte, bool, error) {
	serviceCString := C.CString(service)
	defer C.free(unsafe.Pointer(serviceCString))
	accountCString := C.CString(account)
	defer C.free(unsafe.Pointer(accountCString))

	var length C.UInt32
	var data unsafe.Pointer
	var item C.SecKeychainItemRef
	status := C.SecKeychainFindGenericPassword(
		C.CFTypeRef(unsafe.Pointer(nil)),
		C.UInt32(len(service)),
		serviceCString,
		C.UInt32(len(account)),
		accountCString,
		&length,
		&data,
		&item,
	)
	if status == errSecItemNotFoundStatus {
		return nil, false, nil
	}
	if status != 0 {
		return nil, false, fmt.Errorf("find keychain item: OSStatus %d", int(status))
	}
	defer C.SecKeychainItemFreeContent(nil, data)
	defer C.CFRelease(C.CFTypeRef(item))
	content := C.GoBytes(data, C.int(length))
	return content, true, nil
}

func (securityFrameworkBackend) Save(service, account string, content []byte) error {
	serviceCString := C.CString(service)
	defer C.free(unsafe.Pointer(serviceCString))
	accountCString := C.CString(account)
	defer C.free(unsafe.Pointer(accountCString))
	data, length := bytesPointer(content)

	var item C.SecKeychainItemRef
	status := C.SecKeychainFindGenericPassword(
		C.CFTypeRef(unsafe.Pointer(nil)),
		C.UInt32(len(service)),
		serviceCString,
		C.UInt32(len(account)),
		accountCString,
		nil,
		nil,
		&item,
	)
	if status == 0 {
		defer C.CFRelease(C.CFTypeRef(item))
		status = C.SecKeychainItemModifyAttributesAndData(item, nil, length, data)
		if status != 0 {
			return fmt.Errorf("update keychain item: OSStatus %d", int(status))
		}
		return nil
	}
	if status != errSecItemNotFoundStatus {
		return fmt.Errorf("find keychain item for update: OSStatus %d", int(status))
	}
	status = C.SecKeychainAddGenericPassword(
		C.SecKeychainRef(unsafe.Pointer(nil)),
		C.UInt32(len(service)),
		serviceCString,
		C.UInt32(len(account)),
		accountCString,
		length,
		data,
		nil,
	)
	if status != 0 {
		return fmt.Errorf("add keychain item: OSStatus %d", int(status))
	}
	return nil
}

func bytesPointer(content []byte) (unsafe.Pointer, C.UInt32) {
	if len(content) == 0 {
		return nil, 0
	}
	return unsafe.Pointer(&content[0]), C.UInt32(len(content))
}
