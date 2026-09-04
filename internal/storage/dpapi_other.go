//go:build !windows

package storage

import "fmt"

func Protect(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("DPAPI 仅支持 Windows")
}

func Unprotect(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("DPAPI 仅支持 Windows")
}
