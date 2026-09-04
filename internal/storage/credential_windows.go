//go:build windows

package storage

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        syscall.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	advapi32    = syscall.NewLazyDLL("advapi32.dll")
	credWriteW  = advapi32.NewProc("CredWriteW")
	credReadW   = advapi32.NewProc("CredReadW")
	credDeleteW = advapi32.NewProc("CredDeleteW")
	credFree    = advapi32.NewProc("CredFree")
)

func SaveCredential(target, username, password string) error {
	if target == "" || username == "" || password == "" {
		return fmt.Errorf("credential: target、username 和 password 不能为空")
	}
	targetPtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	userPtr, err := syscall.UTF16PtrFromString(username)
	if err != nil {
		return err
	}
	blob := []byte(password)
	value := credential{
		Type:               credTypeGeneric,
		TargetName:         targetPtr,
		CredentialBlobSize: uint32(len(blob)),
		CredentialBlob:     &blob[0],
		Persist:            credPersistLocalMachine,
		UserName:           userPtr,
	}
	result, _, callErr := credWriteW.Call(uintptr(unsafe.Pointer(&value)), 0)
	if result == 0 {
		return fmt.Errorf("credential: 写入 Windows Credential Manager: %w", callErr)
	}
	return nil
}

func LoadCredential(target string) (username, password string, err error) {
	targetPtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return "", "", err
	}
	var value *credential
	result, _, callErr := credReadW.Call(
		uintptr(unsafe.Pointer(targetPtr)),
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&value)),
	)
	if result == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno == 1168 { // ERROR_NOT_FOUND
			return "", "", fmt.Errorf("credential: 未保存账号: %w", os.ErrNotExist)
		}
		return "", "", fmt.Errorf("credential: 读取 Windows Credential Manager: %w", callErr)
	}
	defer credFree.Call(uintptr(unsafe.Pointer(value)))
	if value == nil {
		return "", "", fmt.Errorf("credential: 返回空凭据")
	}
	username = utf16PointerString(value.UserName)
	if value.CredentialBlobSize > 0 && value.CredentialBlob != nil {
		password = string(append([]byte(nil), unsafe.Slice(value.CredentialBlob, value.CredentialBlobSize)...))
	}
	return username, password, nil
}

func DeleteCredential(target string) error {
	targetPtr, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	result, _, callErr := credDeleteW.Call(uintptr(unsafe.Pointer(targetPtr)), credTypeGeneric, 0)
	if result == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno == 1168 { // ERROR_NOT_FOUND
			return nil
		}
		return fmt.Errorf("credential: 删除 Windows Credential Manager 条目: %w", callErr)
	}
	return nil
}

func utf16PointerString(ptr *uint16) string {
	if ptr == nil {
		return ""
	}
	values := make([]uint16, 0, 64)
	for index := uintptr(0); index < 32768; index++ {
		value := *(*uint16)(unsafe.Add(unsafe.Pointer(ptr), index*2))
		if value == 0 {
			break
		}
		values = append(values, value)
	}
	return syscall.UTF16ToString(values)
}
