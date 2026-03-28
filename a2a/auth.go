package a2a

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrInvalidToken = errors.New("invalid token")
)

type Claims struct {
	TenantID string   `json:"tenantId"`
	Scopes   []string `json:"scopes"`
	jwt.RegisteredClaims
}

type Authenticator struct {
	secret []byte
}

func NewAuthenticator(secret string) *Authenticator {
	return &Authenticator{secret: []byte(secret)}
}

// GenerateToken creates a new JWT for a tenant with specific scopes.
func (a *Authenticator) GenerateToken(tenantID string, scopes []string, duration time.Duration) (string, error) {
	claims := &Claims{
		TenantID: tenantID,
		Scopes:   scopes,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.secret)
}

// Middleware returns a chi-compatible middleware for JWT validation.
func (a *Authenticator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "missing authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
			return
		}

		tokenStr := parts[1]
		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return a.secret, nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}

		// Inject tenantID into context
		ctx := context.WithValue(r.Context(), "tenantID", claims.TenantID)
		ctx = context.WithValue(ctx, "scopes", claims.Scopes)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// HasScope checks if the context contains a specific scope.
func HasScope(ctx context.Context, scope string) bool {
	scopes, ok := ctx.Value("scopes").([]string)
	if !ok {
		return false
	}
	for _, s := range scopes {
		if s == scope || s == "agent:admin" {
			return true
		}
	}
	return false
}
