package auth

import (
	"errors"
	"testing"
	"time"
)

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("secret-pw")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !CheckPassword(hash, "secret-pw") {
		t.Fatal("check: валидный пароль отклонён")
	}
	if CheckPassword(hash, "other-pw") {
		t.Fatal("check: неверный пароль принят")
	}
}

func TestTokenRoundTrip(t *testing.T) {
	tok, err := IssueToken("s3cret", 42, "a@b.ru", time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	claims, err := ParseToken("s3cret", tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.UserID != 42 || claims.Email != "a@b.ru" {
		t.Fatalf("claims = %+v, want uid=42 email=a@b.ru", claims)
	}
}

func TestTokenWrongSecret(t *testing.T) {
	tok, err := IssueToken("s3cret", 1, "a@b.ru", time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := ParseToken("other-secret", tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestTokenExpired(t *testing.T) {
	tok, err := IssueToken("s3cret", 1, "a@b.ru", -time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := ParseToken("s3cret", tok); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken (истёкший токен)", err)
	}
}
