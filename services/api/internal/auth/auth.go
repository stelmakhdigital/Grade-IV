// Package auth — хеширование паролей (bcrypt) и JWT-токены (HS256).
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidToken — токен недействителен (подпись, срок, формат).
var ErrInvalidToken = errors.New("недействительный токен")

// Claims — поля JWT.
type Claims struct {
	UserID int64  `json:"uid"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// HashPassword хеширует пароль (bcrypt).
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt hash: %w", err)
	}
	return string(b), nil
}

// CheckPassword проверяет пароль по хешу.
func CheckPassword(hashed, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hashed), []byte(pw)) == nil
}

// IssueToken выпускает JWT (HS256) на срок ttl.
func IssueToken(secret string, userID int64, email string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Email:  email,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", userID),
			Issuer:    "grade-api",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("issue token: %w", err)
	}
	return token, nil
}

// ParseToken проверяет подпись и срок действия и возвращает claims.
func ParseToken(secret, token string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("неожиданный алгоритм: %s", t.Method.Alg())
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
