//go:build windows

package storage

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	startupRunKey        = `Software\Microsoft\Windows\CurrentVersion\Run`
	regOptionNonVolatile = 0
	regSZ                = 1
	keyQueryValue        = 0x0001
	keySetValue          = 0x0002
	errorSuccess         = 0
	errorFileNotFound    = 2
)

var (
	registryAdvapi32 = syscall.NewLazyDLL("advapi32.dll")
	regOpenKeyExW    = registryAdvapi32.NewProc("RegOpenKeyExW")
	regCreateKeyExW  = registryAdvapi32.NewProc("RegCreateKeyExW")
	regQueryValueExW = registryAdvapi32.NewProc("RegQueryValueExW")
	regSetValueExW   = registryAdvapi32.NewProc("RegSetValueExW")
	regDeleteValueW  = registryAdvapi32.NewProc("RegDeleteValueW")
	regCloseKey      = registryAdvapi32.NewProc("RegCloseKey")
)

const hkeyCurrentUser = uintptr(0x80000001)

func (s *Startup) Enabled() (bool, error) {
	if s == nil {
		return false, fmt.Errorf("storage: 自启动未初始化")
	}
	handle, found, err := openRunKey(keyQueryValue, false)
	if err != nil || !found {
		return false, err
	}
	defer regCloseKey.Call(handle)
	value, exists, err := queryRegistryString(handle, startupValueName)
	if err != nil || !exists {
		return false, err
	}
	return value == s.Command(), nil
}

func (s *Startup) SetEnabled(enabled bool) error {
	if s == nil {
		return fmt.Errorf("storage: 自启动未初始化")
	}
	if enabled {
		handle, _, err := openRunKey(keySetValue, true)
		if err != nil {
			return err
		}
		defer regCloseKey.Call(handle)
		return setRegistryString(handle, startupValueName, s.Command())
	}

	handle, found, err := openRunKey(keySetValue, false)
	if err != nil || !found {
		return err
	}
	defer regCloseKey.Call(handle)
	name, err := syscall.UTF16PtrFromString(startupValueName)
	if err != nil {
		return fmt.Errorf("storage: 编码自启动值名: %w", err)
	}
	status, _, _ := regDeleteValueW.Call(handle, uintptr(unsafe.Pointer(name)))
	if status == errorSuccess || status == errorFileNotFound {
		return nil
	}
	return fmt.Errorf("storage: 删除当前用户自启动项: %w", syscall.Errno(status))
}

func openRunKey(access uintptr, create bool) (handle uintptr, found bool, err error) {
	path, err := syscall.UTF16PtrFromString(startupRunKey)
	if err != nil {
		return 0, false, fmt.Errorf("storage: 编码自启动注册表路径: %w", err)
	}
	if create {
		var result uintptr
		status, _, _ := regCreateKeyExW.Call(
			hkeyCurrentUser,
			uintptr(unsafe.Pointer(path)),
			0,
			0,
			regOptionNonVolatile,
			access,
			0,
			uintptr(unsafe.Pointer(&result)),
			0,
		)
		if status != errorSuccess {
			return 0, false, fmt.Errorf("storage: 打开当前用户自启动注册表: %w", syscall.Errno(status))
		}
		return result, true, nil
	}

	var result uintptr
	status, _, _ := regOpenKeyExW.Call(
		hkeyCurrentUser,
		uintptr(unsafe.Pointer(path)),
		0,
		access,
		uintptr(unsafe.Pointer(&result)),
	)
	if status == errorFileNotFound {
		return 0, false, nil
	}
	if status != errorSuccess {
		return 0, false, fmt.Errorf("storage: 打开当前用户自启动注册表: %w", syscall.Errno(status))
	}
	return result, true, nil
}

func queryRegistryString(handle uintptr, nameValue string) (string, bool, error) {
	name, err := syscall.UTF16PtrFromString(nameValue)
	if err != nil {
		return "", false, fmt.Errorf("storage: 编码自启动值名: %w", err)
	}
	var valueType uint32
	var byteCount uint32
	status, _, _ := regQueryValueExW.Call(
		handle,
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&valueType)),
		0,
		uintptr(unsafe.Pointer(&byteCount)),
	)
	if status == errorFileNotFound {
		return "", false, nil
	}
	if status != errorSuccess {
		return "", false, fmt.Errorf("storage: 查询自启动项大小: %w", syscall.Errno(status))
	}
	if valueType != regSZ {
		return "", false, nil
	}
	if byteCount == 0 {
		return "", true, nil
	}
	buffer := make([]uint16, (byteCount+1)/2)
	status, _, _ = regQueryValueExW.Call(
		handle,
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&valueType)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&byteCount)),
	)
	if status != errorSuccess {
		return "", false, fmt.Errorf("storage: 读取自启动项: %w", syscall.Errno(status))
	}
	return syscall.UTF16ToString(buffer), true, nil
}

func setRegistryString(handle uintptr, nameValue, value string) error {
	name, err := syscall.UTF16PtrFromString(nameValue)
	if err != nil {
		return fmt.Errorf("storage: 编码自启动值名: %w", err)
	}
	data, err := syscall.UTF16FromString(value)
	if err != nil {
		return fmt.Errorf("storage: 编码自启动命令: %w", err)
	}
	status, _, _ := regSetValueExW.Call(
		handle,
		uintptr(unsafe.Pointer(name)),
		0,
		regSZ,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)*2),
	)
	if status != errorSuccess {
		return fmt.Errorf("storage: 写入当前用户自启动项: %w", syscall.Errno(status))
	}
	return nil
}
