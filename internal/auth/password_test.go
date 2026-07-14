package auth

import (
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
