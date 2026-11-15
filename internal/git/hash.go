package git

import (
	"crypto/sha1" //nolint:gosec // SHA-1 is git's object hash for SHA-1 repositories, not a security check
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
)

// HashBlob is the id git gives a blob holding b in a repository of format f:
// the hash of "blob <len>", a NUL, then the bytes. It names bytes without
// writing them, so a config version's id is the id git would give its file.
func HashBlob(f ObjectFormat, b []byte) ObjectID {
	var h hash.Hash
	switch f {
	case SHA1:
		h = sha1.New()
	case SHA256:
		h = sha256.New()
	default:
		panic(fmt.Sprintf("git: HashBlob of %s, which names no object", f))
	}
	_, _ = io.WriteString(h, fmt.Sprintf("blob %d\x00", len(b)))
	_, _ = h.Write(b)
	id, err := ObjectIDFrom(f, h.Sum(nil))
	if err != nil {
		panic(fmt.Sprintf("git: HashBlob: %v", err))
	}
	return id
}
