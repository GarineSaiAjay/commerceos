package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

const (
	saltBytes = 16
	keyBytes  = 32
	// iterations follows OWASP's 2023 minimum for PBKDF2-HMAC-SHA256.
	iterations = 210_000
)

// HashPassword derives a salted key from password using PBKDF2-HMAC-SHA256
// (RFC 8018), implemented by hand against the standard library only.
// This sandbox has no network access to fetch golang.org/x/crypto/bcrypt
// (or any new Go module), so bcrypt/argon2id -- the usual first choice --
// isn't available here; a hand-rolled *weaker* scheme (unsalted, single
// round, a bespoke cipher) would be worse than being explicit about that
// constraint and using the well-specified, standard-library-only
// algorithm instead. Encoded as "pbkdf2-sha256$<iterations>$<salt-hex>$
// <key-hex>" so the iteration count travels with the hash, letting it be
// tuned later without invalidating existing rows.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := pbkdf2HMACSHA256(password, salt, iterations, keyBytes)
	return fmt.Sprintf(
		"pbkdf2-sha256$%d$%s$%s",
		iterations,
		hex.EncodeToString(salt),
		hex.EncodeToString(key),
	), nil
}

// VerifyPassword checks password against an encoded hash produced by
// HashPassword, comparing in constant time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iters, err := strconv.Atoi(parts[1])
	if err != nil || iters <= 0 {
		return false
	}
	salt, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got := pbkdf2HMACSHA256(password, salt, iters, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// pbkdf2HMACSHA256 implements PBKDF2 (RFC 8018) with HMAC-SHA256 as the
// pseudorandom function, using only crypto/hmac and crypto/sha256.
func pbkdf2HMACSHA256(password string, salt []byte, iterations, keyLen int) []byte {
	prf := hmac.New(sha256.New, []byte(password))
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen

	derived := make([]byte, 0, numBlocks*hashLen)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		var blockIndex [4]byte
		binary.BigEndian.PutUint32(blockIndex[:], uint32(block))
		prf.Write(blockIndex[:])
		u := prf.Sum(nil)

		t := make([]byte, len(u))
		copy(t, u)

		for i := 1; i < iterations; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		derived = append(derived, t...)
	}
	return derived[:keyLen]
}

// generateSessionToken returns a random 256-bit token, hex-encoded, and
// the hex-encoded SHA-256 hash stored at rest instead of the raw token
// (so a database read alone never yields a usable session).
func generateSessionToken() (token string, tokenHash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate session token: %w", err)
	}
	token = hex.EncodeToString(raw)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
