//go:build !windows

package storage

import "fmt"

func SaveCredential(target, username, password string) error {
	return fmt.Errorf("Windows Credential Manager 仅支持 Windows")
}

func LoadCredential(target string) (username, password string, err error) {
	return "", "", fmt.Errorf("Windows Credential Manager 仅支持 Windows")
}

func DeleteCredential(target string) error {
	return fmt.Errorf("Windows Credential Manager 仅支持 Windows")
}
