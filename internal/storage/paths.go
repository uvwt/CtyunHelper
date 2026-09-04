package storage

import (
	"os"
	"path/filepath"
)

type Paths struct {
	ConfigDir string
	DataDir   string
	LogDir    string
}

func ResolvePaths() (Paths, error) {
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, err
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, err
	}
	configDir := filepath.Join(configRoot, "CtyunHelper")
	dataDir := filepath.Join(cacheRoot, "CtyunHelper")
	return Paths{
		ConfigDir: configDir,
		DataDir:   dataDir,
		LogDir:    filepath.Join(dataDir, "logs"),
	}, nil
}
