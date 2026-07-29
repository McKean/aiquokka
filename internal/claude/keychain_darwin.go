//go:build darwin && cgo

package claude

/*
#cgo LDFLAGS: -framework Security
#include "keychain_darwin.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func loadKeychainCredentials() ([]byte, []byte, error) {
	var data, item C.CFDataRef
	status := C.findClaudeCredentials(&data, &item)
	if status == C.errSecItemNotFound {
		return nil, nil, errKeychainCredentialsNotFound
	}
	if status == C.errSecDuplicateItem {
		return nil, nil, errMultipleKeychainCredentials
	}
	if status != C.errSecSuccess {
		return nil, nil, fmt.Errorf("reading Claude Code credentials from Keychain: OSStatus %d", status)
	}
	defer C.CFRelease(C.CFTypeRef(data))
	defer C.CFRelease(C.CFTypeRef(item))
	return cfDataBytes(data), cfDataBytes(item), nil
}

func persistKeychainCredentials(item, data []byte) error {
	itemData := C.dataFromBytes(unsafe.Pointer(unsafe.SliceData(item)), C.CFIndex(len(item)))
	defer C.CFRelease(C.CFTypeRef(itemData))
	credentialData := C.dataFromBytes(unsafe.Pointer(unsafe.SliceData(data)), C.CFIndex(len(data)))
	defer C.CFRelease(C.CFTypeRef(credentialData))
	if status := C.updateClaudeCredentials(itemData, credentialData); status != C.errSecSuccess {
		return fmt.Errorf("updating Claude Code credentials in Keychain: OSStatus %d", status)
	}
	return nil
}

func loadKeychainCredential(item []byte) ([]byte, error) {
	itemData := C.dataFromBytes(unsafe.Pointer(unsafe.SliceData(item)), C.CFIndex(len(item)))
	defer C.CFRelease(C.CFTypeRef(itemData))
	var data C.CFDataRef
	if status := C.readClaudeCredentials(itemData, &data); status != C.errSecSuccess {
		return nil, fmt.Errorf("reading Claude Code credentials from Keychain: OSStatus %d", status)
	}
	defer C.CFRelease(C.CFTypeRef(data))
	return cfDataBytes(data), nil
}

func cfDataBytes(data C.CFDataRef) []byte {
	return C.GoBytes(unsafe.Pointer(C.CFDataGetBytePtr(data)), C.int(C.CFDataGetLength(data)))
}
