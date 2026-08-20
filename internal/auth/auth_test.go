package auth

import "testing"

func TestPasswordHashAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("expected password to verify")
	}
	if VerifyPassword(hash, "wrong password") {
		t.Fatal("wrong password verified")
	}
}

func TestTokenGenerationAndHash(t *testing.T) {
	first, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("tokens must be random")
	}
	if HashToken(first) == first || HashToken(first) != HashToken(first) {
		t.Fatal("token hash must be deterministic and distinct from token")
	}
}
