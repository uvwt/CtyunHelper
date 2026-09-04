//go:build windows

package storage

import (
	"fmt"
	"syscall"
	"unsafe"
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32            = syscall.NewLazyDLL("crypt32.dll")
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	cryptProtectData   = crypt32.NewProc("CryptProtectData")
	cryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	localFree          = kernel32.NewProc("LocalFree")
)

func Protect(data []byte) ([]byte, error) {
	return cryptData(cryptProtectData, data)
}

func Unprotect(data []byte) ([]byte, error) {
	return cryptData(cryptUnprotectData, data)
}

func cryptData(proc *syscall.LazyProc, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}
	in := dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out dataBlob
	r1, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r1 == 0 {
		return nil, fmt.Errorf("DPAPI 调用失败: %w", callErr)
	}
	defer localFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return append([]byte(nil), unsafe.Slice(out.pbData, out.cbData)...), nil
}
