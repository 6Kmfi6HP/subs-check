package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// cacheDir is set once at startup; see SetCacheDir.
var cacheDir string

// SetCacheDir derives the private cache dir (results snapshot, export cache) from
// the config file: <config dir>/cache/<config name>. Deployments already persist
// the config dir (e.g. the Docker volume) and /sub/ never serves it; the config
// name keeps instances that share a config dir apart.
func SetCacheDir(configPath string) {
	if abs, err := filepath.Abs(configPath); err == nil {
		configPath = abs
	}
	name := strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath))
	cacheDir = filepath.Join(filepath.Dir(configPath), "cache", name)
}

// CacheDir returns the dir chosen by SetCacheDir, or the one for the default
// config path if it was never called.
func CacheDir() string {
	if cacheDir == "" {
		return filepath.Join(GetExecutablePath(), "config", "cache", "config")
	}
	return cacheDir
}

// WriteFileAtomic writes via a temp file and rename so readers never see partial
// content. Missing dirs are created with 0700; the file ends up 0600.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
