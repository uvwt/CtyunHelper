package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// validProtectedName 与 statePath 的校验规则保持一致：仅允许当前目录下的
// 普通文件名，拒绝空串、路径分隔符与 "."/".." 这类特殊目录引用。
func validProtectedName(name string) bool {
	return name != "" && filepath.Base(name) == name && name != "." && name != ".."
}

func SaveProtectedJSON(paths Paths, name string, value any) error {
	if !validProtectedName(name) {
		return fmt.Errorf("storage: 非法受保护文件名")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("storage: 编码受保护数据: %w", err)
	}
	protected, err := Protect(raw)
	clear(raw)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(paths.DataDir, name), protected, 0o600)
}

func LoadProtectedJSON(paths Paths, name string, destination any) error {
	if !validProtectedName(name) {
		return fmt.Errorf("storage: 非法受保护文件名")
	}
	raw, err := os.ReadFile(filepath.Join(paths.DataDir, name))
	if err != nil {
		return err
	}
	plain, err := Unprotect(raw)
	if err != nil {
		return err
	}
	defer clear(plain)
	if err := json.Unmarshal(plain, destination); err != nil {
		return fmt.Errorf("storage: 解析受保护数据: %w", err)
	}
	return nil
}

func DeleteProtected(paths Paths, name string) error {
	if !validProtectedName(name) {
		return fmt.Errorf("storage: 非法受保护文件名")
	}
	err := os.Remove(filepath.Join(paths.DataDir, name))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
