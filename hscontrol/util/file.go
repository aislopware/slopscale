package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/conf"
	"github.com/tailscale/hujson"
	"go.yaml.in/yaml/v3"
)

const (
	Base8              = 8
	Base10             = 10
	BitSize16          = 16
	BitSize32          = 32
	BitSize64          = 64
	PermissionFallback = 0o700
)

// ErrDirectoryPermission is returned when creating a directory fails due to permission issues.
var ErrDirectoryPermission = errors.New("creating directory failed with permission error")

// ErrUnknownFileFormat is returned for a file whose extension names no format
// [UnmarshalByExt] reads.
var ErrUnknownFileFormat = errors.New("unknown file format, want .json, .hujson, .yaml or .yml")

// UnmarshalByExt decodes data into a T in the format name's extension picks:
// .json, .hujson (JSON with comments and trailing commas), or .yaml/.yml.
// The extension decides, not the content: YAML parses JSON syntax, so sniffing
// cannot tell the two apart. YAML keys are the lowercased Go field names.
func UnmarshalByExt[T any](name string, data []byte) (T, error) {
	var (
		v   T
		err error
	)

	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".json":
		err = json.Unmarshal(data, &v)
	case ".hujson":
		data, err = hujson.Standardize(data)
		if err == nil {
			err = json.Unmarshal(data, &v)
		}
	case ".yaml", ".yml":
		err = yaml.Unmarshal(data, &v)
	default:
		return v, fmt.Errorf("%s: %w", name, ErrUnknownFileFormat)
	}

	if err != nil {
		return v, fmt.Errorf("decoding %s: %w", name, err)
	}

	return v, nil
}

// ReadFileByExt reads the file at path and decodes it with [UnmarshalByExt].
func ReadFileByExt[T any](path string) (T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		var zero T

		return zero, fmt.Errorf("reading %s: %w", path, err)
	}

	return UnmarshalByExt[T](path, data)
}

func AbsolutePathFromConfigPath(path string) string {
	// If a relative path is provided, prefix it with the directory where
	// the config file was found.
	if (path != "") && !strings.HasPrefix(path, string(os.PathSeparator)) {
		dir, _ := filepath.Split(conf.ConfigFileUsed())
		if dir != "" {
			path = filepath.Join(dir, path)
		}
	}

	return path
}

func GetFileMode(key string) fs.FileMode {
	modeStr := conf.GetString(key)

	mode, err := strconv.ParseUint(modeStr, Base8, BitSize32)
	if err != nil {
		return PermissionFallback
	}

	return fs.FileMode(mode)
}

func EnsureDir(dir string) error {
	_, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		mkdirErr := os.MkdirAll(dir, PermissionFallback)
		if mkdirErr != nil {
			if errors.Is(mkdirErr, os.ErrPermission) {
				return fmt.Errorf("%w: %s", ErrDirectoryPermission, dir)
			}

			return fmt.Errorf("creating directory %s: %w", dir, mkdirErr)
		}
	}

	return nil
}
