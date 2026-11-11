package git

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5/plumbing/format/config"
)

// ReadObjectFormat reads the code repository's object format from the
// config in its common dir: extensions.objectFormat, SHA-1 when unset, as
// git reads it. No config is every default, as git reads that too; a config
// that does not parse is an error, and so is the extension under format
// version 0, which git refuses to open. It reads the code repository and
// never writes it.
func ReadObjectFormat(commonDir string) (f ObjectFormat, err error) {
	path := filepath.Join(commonDir, "config")
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return SHA1, nil
	}
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	cfg := config.New()
	if err := config.NewDecoder(file).Decode(cfg); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	ext := cfg.Section("extensions")
	if v := cfg.Section("core").Option("repositoryformatversion"); ext.HasOption("objectformat") && (v == "" || v == "0") {
		return 0, fmt.Errorf("%s: extensions.objectformat is set under repository format version 0, which admits no extension git defines for version 1", path)
	}
	switch v := ext.Option("objectformat"); v {
	case "", "sha1":
		return SHA1, nil
	case "sha256":
		return SHA256, nil
	default:
		return 0, fmt.Errorf("%s: extensions.objectformat is %q, which is not an object format git defines", path, v)
	}
}
