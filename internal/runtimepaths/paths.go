package runtimepaths

import (
	"fmt"
	"os"
	"path/filepath"
)

const dataDirectoryName = "Discord Unlocker"

// Paths contains every mutable file created by the launcher. Keeping all state
// under one directory makes uninstall cleanup predictable.
type Paths struct {
	DataDir   string
	CacheFile string
	PACFile   string
	LogFile   string
}

func FromLocalAppData(localAppData string) (Paths, error) {
	if localAppData == "" {
		return Paths{}, fmt.Errorf("LOCALAPPDATA não está disponível")
	}

	dataDir := filepath.Join(localAppData, dataDirectoryName)
	return Paths{
		DataDir:   dataDir,
		CacheFile: filepath.Join(dataDir, "proxy-cache-v1.json"),
		PACFile:   filepath.Join(dataDir, "gateway-proxy.pac"),
		LogFile:   filepath.Join(dataDir, "discord-unlocker.log"),
	}, nil
}

func ForCurrentUser() (Paths, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return Paths{}, fmt.Errorf("localizar pasta de dados do usuário: %w", err)
	}
	return FromLocalAppData(base)
}

func (p Paths) Ensure() error {
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		return fmt.Errorf("criar pasta de dados: %w", err)
	}
	return nil
}
