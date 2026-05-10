package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/viper"
)

func GetPackwizLocalStore() (string, error) {
	if //goland:noinspection GoBoolExpressions
	runtime.GOOS == "linux" {
		// Prefer $XDG_DATA_HOME over $XDG_CACHE_HOME
		dataHome := os.Getenv("XDG_DATA_HOME")
		if dataHome != "" {
			return filepath.Join(dataHome, "packwiz"), nil
		}
	}
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userConfigDir, "packwiz"), nil
}

func GetPackwizLocalCache() (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userCacheDir, "packwiz"), nil
}

func GetPackwizInstallBinPath() (string, error) {
	localStore, err := GetPackwizLocalStore()
	if err != nil {
		return "", err
	}
	return filepath.Join(localStore, "bin"), nil
}

func GetPackwizInstallBinFile() (string, error) {
	binPath, err := GetPackwizInstallBinPath()
	if err != nil {
		return "", err
	}
	var exeName string
	if //goland:noinspection GoBoolExpressions
	runtime.GOOS == "windows" {
		exeName = "packwiz.exe"
	} else {
		exeName = "packwiz"
	}
	return filepath.Join(binPath, exeName), nil
}

// GetPackwizCache returns the path to the packwiz download cache directory.
// This can be overridden by setting cache.directory in the config.
func GetPackwizCache() (string, error) {
	configuredCache := viper.GetString("cache.directory")
	if configuredCache != "" {
		return configuredCache, nil
	}
	localStore, err := GetPackwizLocalCache()
	if err != nil {
		return "", err
	}
	return filepath.Join(localStore, "cache"), nil
}

// GetDepsCacheDir returns the path to the directory for caching mod JARs
// for dependency resolution. JARs are stored here when they're not available
// locally and need to be downloaded for deps parsing.
func GetDepsCacheDir() (string, error) {
	localStore, err := GetPackwizLocalCache()
	if err != nil {
		return "", err
	}
	depsDir := filepath.Join(localStore, "deps")
	err = os.MkdirAll(depsDir, 0755)
	if err != nil {
		return "", fmt.Errorf("failed to create deps cache directory: %w", err)
	}
	return depsDir, nil
}
