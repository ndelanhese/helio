package auth

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestPasswordRoundTripAndPolicy(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected hash parameters: %q", hash)
	}
	ok, err := VerifyPassword(hash, "correct horse battery staple")
	if err != nil || !ok {
		t.Fatalf("round trip: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword(hash, "incorrect horse battery staple")
	if err != nil || ok {
		t.Fatalf("wrong password: ok=%v err=%v", ok, err)
	}
	for _, password := range []string{"short", strings.Repeat("x", 129)} {
		if _, err := HashPassword(password); err == nil {
			t.Fatalf("HashPassword accepted %d bytes", len(password))
		}
	}
}

func TestPasswordRejectsMalformedOrDangerousHashes(t *testing.T) {
	bad := []string{
		"", "$argon2id$v=19$m=65536,t=3,p=2$only-five",
		"$argon2id$v=19$m=999999999,t=3,p=2$c2FsdA$a2V5",
		"$argon2id$v=19$m=65536,t=999999999,p=2$c2FsdA$a2V5",
		"$argon2id$v=18$m=65536,t=3,p=2$c2FsdA$a2V5",
	}
	for _, hash := range bad {
		if ok, err := VerifyPassword(hash, "correct horse battery staple"); err == nil || ok {
			t.Fatalf("VerifyPassword(%q)=(%v,%v)", hash, ok, err)
		}
	}
}

func TestPasswordVerifyRequiresExactArgon2ParametersAndSizes(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(hash, "$")
	weaker := []string{
		strings.Replace(hash, "m=65536", "m=32768", 1),
		strings.Replace(hash, "t=3", "t=2", 1),
		strings.Replace(hash, "p=2", "p=1", 1),
		strings.Replace(hash, "v=19", "v=18", 1),
	}
	shortSalt := append([]string(nil), parts...)
	shortSalt[4] = base64.RawStdEncoding.EncodeToString(make([]byte, 15))
	weaker = append(weaker, strings.Join(shortSalt, "$"))
	shortKey := append([]string(nil), parts...)
	shortKey[5] = base64.RawStdEncoding.EncodeToString(make([]byte, 31))
	weaker = append(weaker, strings.Join(shortKey, "$"))
	for _, encoded := range weaker {
		if ok, err := VerifyPassword(encoded, "correct horse battery staple"); err == nil || ok {
			t.Fatalf("accepted noncanonical hash: ok=%v err=%v", ok, err)
		}
	}
}

func TestPasswordRejectsInvalidUTF8(t *testing.T) {
	password := string([]byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8, 0xf7, 0xf6, 0xf5, 0xf4})
	if _, err := HashPassword(password); err == nil {
		t.Fatal("HashPassword accepted invalid UTF-8")
	}
}
