package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadStateJSON 从用户数据目录读取非敏感运行状态。不存在时返回 found=false。
// 敏感 Profile 继续使用 DPAPI，不经过这里。
func LoadStateJSON(paths Paths, name string, value any) (found bool, err error) {
	path, err := statePath(paths, name)
	if err != nil {
		return false, err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: 读取状态 %s: %w", name, err)
	}
	if err := json.Unmarshal(raw, value); err != nil {
		return false, fmt.Errorf("storage: 解析状态 %s: %w", name, err)
	}
	return true, nil
}

func SaveStateJSON(paths Paths, name string, value any) error {
	path, err := statePath(paths, name)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("storage: 编码状态 %s: %w", name, err)
	}
	return writeAtomic(path, raw, 0o600)
}

func statePath(paths Paths, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return "", fmt.Errorf("storage: 状态文件名无效")
	}
	return filepath.Join(paths.DataDir, name), nil
}
