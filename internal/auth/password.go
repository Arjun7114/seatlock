package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP 2026 starting point: m=64MiB, t=3, p=1).
// Stored inside each hash so they can be raised later without breaking old hashes.
type params struct {
	memory      uint32 // KiB
	iterations  uint32
	parallelism uint8
	saltLength  uint32
	keyLength   uint32
}

var defaultParams = params{
	memory:      64 * 1024, // 64 MiB
	iterations:  3,
	parallelism: 1,
	saltLength:  16,
	keyLength:   32,
}

var ErrInvalidHash = errors.New("invalid password hash format")

// HashPassword returns a self-describing PHC-format Argon2id hash.
func HashPassword(password string) (string, error) {
	p := defaultParams

	salt := make([]byte, p.saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	key := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Key := base64.RawStdEncoding.EncodeToString(key)

	// PHC string format: embeds algorithm, version, params, salt, hash.
	encoded := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.memory, p.iterations, p.parallelism, b64Salt, b64Key)
	return encoded, nil
}

// VerifyPassword reports whether password matches the stored PHC hash.
func VerifyPassword(password, encoded string) (bool, error) {
	p, salt, key, err := decodeHash(encoded)
	if err != nil {
		return false, err
	}

	otherKey := argon2.IDKey([]byte(password), salt, p.iterations, p.memory, p.parallelism, p.keyLength)

	// Constant-time comparison to avoid timing leaks.
	if subtle.ConstantTimeCompare(key, otherKey) == 1 {
		return true, nil
	}
	return false, nil
}

func decodeHash(encoded string) (p params, salt, key []byte, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return p, nil, nil, ErrInvalidHash
	}

	var version int
	if _, err = fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	if version != argon2.Version {
		return p, nil, nil, ErrInvalidHash
	}

	if _, err = fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.iterations, &p.parallelism); err != nil {
		return p, nil, nil, ErrInvalidHash
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	p.saltLength = uint32(len(salt))

	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return p, nil, nil, ErrInvalidHash
	}
	p.keyLength = uint32(len(key))

	return p, salt, key, nil
}