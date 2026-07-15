package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      = 64 * 1024
	argonIterations  = 3
	argonParallelism = 2
	argonSaltBytes   = 16
	argonKeyBytes    = 32
)

var (
	ErrPasswordLength   = errors.New("password must be between 12 characters and 128 bytes")
	ErrPasswordEncoding = errors.New("password must be valid UTF-8")
)

// dummyPasswordHash has production-bounded Argon2id parameters and fixed
// non-secret material. Missing-user logins still perform the same KDF work.
const dummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func HashPassword(password string) (string, error) { return hashPassword(password, rand.Reader) }

func hashPassword(password string, entropy io.Reader) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, argonSaltBytes)
	if _, err := io.ReadFull(entropy, salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyBytes)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonIterations,
		argonParallelism, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) (bool, error) {
	if err := validatePassword(password); err != nil {
		return false, err
	}
	params, salt, expected, err := parsePasswordHash(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(password), salt, params.iterations, params.memory, params.parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func validatePassword(password string) error {
	if !utf8.ValidString(password) {
		return ErrPasswordEncoding
	}
	if len([]rune(password)) < 12 || len(password) > 128 {
		return ErrPasswordLength
	}
	return nil
}

type argonParams struct {
	memory, iterations uint32
	parallelism        uint8
}

func parsePasswordHash(encoded string) (argonParams, []byte, []byte, error) {
	if len(encoded) > 256 || strings.Count(encoded, "$") != 5 {
		return argonParams{}, nil, nil, errors.New("invalid password hash format")
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return argonParams{}, nil, nil, errors.New("invalid password hash format")
	}
	var memory, iterations uint32
	var parallelism uint8
	if n, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil || n != 3 || parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", memory, iterations, parallelism) {
		return argonParams{}, nil, nil, errors.New("invalid password hash parameters")
	}
	// Bound every attacker-controlled work factor before decoding or allocating.
	if memory != argonMemory || iterations != argonIterations || parallelism != argonParallelism {
		return argonParams{}, nil, nil, errors.New("password hash parameters out of bounds")
	}
	if len(parts[4]) < 22 || len(parts[4]) > 86 || len(parts[5]) < 22 || len(parts[5]) > 86 {
		return argonParams{}, nil, nil, errors.New("password hash fields out of bounds")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != argonSaltBytes {
		return argonParams{}, nil, nil, errors.New("invalid password hash salt")
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) != argonKeyBytes {
		return argonParams{}, nil, nil, errors.New("invalid password hash key")
	}
	return argonParams{memory: memory, iterations: iterations, parallelism: parallelism}, salt, key, nil
}
