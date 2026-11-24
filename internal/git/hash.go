package git

import (
	"crypto/sha1" //nolint:gosec // SHA-1 is git's object hash for SHA-1 repositories, not a security check
	"crypto/sha256"
	"fmt"
	"hash"
	"io"
)

// HashBlob is the id git gives a blob holding b in a repository of format f.
// It names bytes without writing them, so a config version's id is the id
// git would give its file.
func HashBlob(f ObjectFormat, b []byte) ObjectID { return hashObject(f, blobKind, b) }

// hashObject is the id git gives an object of kind k holding b: the hash
// of "<kind> <len>", a NUL, then the bytes.
func hashObject(f ObjectFormat, k kind, b []byte) ObjectID {
	var h hash.Hash
	switch f {
	case SHA1:
		h = sha1.New()
	case SHA256:
		h = sha256.New()
	default:
		panic(fmt.Sprintf("git: hashing in %s, which names no object", f))
	}
	_, _ = io.WriteString(h, fmt.Sprintf("%s %d\x00", k, len(b)))
	_, _ = h.Write(b)
	id, err := ObjectIDFrom(f, h.Sum(nil))
	if err != nil {
		panic(fmt.Sprintf("git: hashing: %v", err))
	}
	return id
}
