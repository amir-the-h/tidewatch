// Package auth provides JWT token generation/validation and password hashing
// for the iran-watcher web UI. Tokens use HS256 (HMAC-SHA256) via stdlib only.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/amir-the-h/iran-watcher/internal/model"
)

// contextKey is the key used to store the authenticated user in a request context.
type contextKey struct{}

// TokenTTL is how long a JWT remains valid.
const TokenTTL = 24 * time.Hour

// jwtHeader is the fixed base64url-encoded header for all tokens.
var jwtHeader = base64url(mustJSON(map[string]string{"alg": "HS256", "typ": "JWT"}))

// Claims are the JWT payload fields.
type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
	Iat      int64  `json:"iat"`
}

// IssueToken creates a signed JWT for the given user.
func IssueToken(secret string, u *model.User) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   u.ID,
		Username: u.Username,
		Role:     u.Role,
		Iat:      now.Unix(),
		Exp:      now.Add(TokenTTL).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	b64payload := base64url(payload)
	sig := sign(secret, jwtHeader+"."+b64payload)
	return jwtHeader + "." + b64payload + "." + sig, nil
}

// ValidateToken parses and verifies a JWT string. Returns the Claims on success.
func ValidateToken(secret, token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("auth: malformed token")
	}
	expected := sign(secret, parts[0]+"."+parts[1])
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return nil, errors.New("auth: invalid signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("auth: malformed payload")
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, errors.New("auth: malformed claims")
	}
	if time.Now().Unix() > claims.Exp {
		return nil, errors.New("auth: token expired")
	}
	return &claims, nil
}

// HashPassword returns the bcrypt hash of a plaintext password.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword reports whether password matches the stored bcrypt hash.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Middleware returns an HTTP middleware that requires a valid Bearer token.
// On success, the Claims are stored in the request context.
func Middleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := bearerToken(r)
			if token == "" {
				writeUnauthorized(w)
				return
			}
			claims, err := ValidateToken(secret, token)
			if err != nil {
				writeUnauthorized(w)
				return
			}
			ctx := context.WithValue(r.Context(), contextKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AdminOnly returns an HTTP middleware that requires the admin role.
// Must be used after Middleware.
func AdminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := ClaimsFromContext(r.Context())
		if claims == nil || claims.Role != model.RoleAdmin {
			writeForbidden(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClaimsFromContext extracts the Claims stored by Middleware, or nil.
func ClaimsFromContext(ctx context.Context) *Claims {
	v, _ := ctx.Value(contextKey{}).(*Claims)
	return v
}

// ── helpers ───────────────────────────────────────────────────────────────────

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(h, "Bearer ")
}

func sign(secret, data string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return base64url(mac.Sum(nil))
}

func base64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"unauthorized"}`))
}

func writeForbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"error":"forbidden"}`))
}
