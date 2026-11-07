package config

import (
	"os"
	"path/filepath"
)

// LoadExample reads the committed config.yml.example through Load.
// Tests and the print flag use it so the sample is not parsed a second way.
func LoadExample() (Config, error) {
	root, err := moduleRoot()
	if err != nil {
		return Config{}, err
	}
	return Load(filepath.Join(root, "config.yml.example"))
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
