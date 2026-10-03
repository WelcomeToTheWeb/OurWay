package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims extends the standard JWT claims with user-specific data.
type Claims struct {
	UserID   string   `json:"user_id"`
	Username string   `json:"username"`
	Roles    []string `json:"roles"`
	Type     string   `json:"type"` // "access" or "refresh"
	// Gen is the user's token-generation at issue time; bumping it
	// (password change) invalidates earlier tokens (H3). 0 means
	// "issued before generations existed" and skips the check.
	Gen uint32 `json:"gen,omitempty"`
	jwt.RegisteredClaims
}

// JWTAuth generates and validates JWT tokens.
//
// denylist maps JTI -> expiry for logged-out/invalidated access tokens;
// userGen tracks per-user token generations so a password change
// invalidates every access token issued before it. Both are in-memory:
// a server restart clears them (access tokens are short-lived, which is
// the point).
type JWTAuth struct {
	secret []byte

	mu       sync.RWMutex
	denylist map[string]time.Time
	userGen  map[string]uint32
}

// NewJWTAuth creates a JWT auth service with the given secret.
func NewJWTAuth(secret string) *JWTAuth {
	return &JWTAuth{
		secret:   []byte(secret),
		denylist: make(map[string]time.Time),
		userGen:  make(map[string]uint32),
	}
}

func newJTI() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return hex.EncodeToString(buf)
}

// GenerateToken creates an access token valid for 15 minutes.
func (j *JWTAuth) GenerateToken(userID, username string, roles []string) (string, error) {
	j.mu.Lock()
	gen, ok := j.userGen[userID]
	if !ok {
		gen = 1
		j.userGen[userID] = gen
	}
	j.mu.Unlock()

	claims := Claims{
		UserID:   userID,
		Username: username,
		Roles:    roles,
		Type:     "access",
		Gen:      gen,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        newJTI(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "ourway",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

// GenerateRefreshToken creates a refresh token valid for 7 days.
func (j *JWTAuth) GenerateRefreshToken(userID, username string, roles []string) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		Roles:    roles,
		Type:     "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "ourway-refresh",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

// parseToken parses and validates a JWT token (signature and expiry),
// returning its claims. It does not check the token type.
func (j *JWTAuth) parseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return j.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// ValidateToken parses and validates a JWT access token, returning its
// claims. Refresh tokens and legacy tokens without a type claim are
// rejected, as are logged-out tokens (JTI denylist) and tokens from a
// stale token generation (issued before the user's last password
// change).
func (j *JWTAuth) ValidateToken(tokenString string) (*Claims, error) {
	claims, err := j.parseToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.Type != "access" {
		return nil, errors.New("token is not an access token")
	}

	j.mu.RLock()
	if exp, ok := j.denylist[claims.ID]; ok && time.Now().Before(exp) {
		j.mu.RUnlock()
		return nil, errors.New("token has been revoked")
	}
	if claims.Gen > 0 {
		if cur, ok := j.userGen[claims.UserID]; ok && cur != claims.Gen {
			j.mu.RUnlock()
			return nil, errors.New("token generation is stale")
		}
	}
	j.mu.RUnlock()
	return claims, nil
}

// Deny revokes an access token by its JTI until its expiry (logout,
// H3). Unknown JTIs are recorded harmlessly.
func (j *JWTAuth) Deny(jti string, exp *jwt.NumericDate) {
	var until time.Time
	if exp != nil {
		until = exp.Time
	} else {
		until = time.Now().Add(15 * time.Minute)
	}
	now := time.Now()
	j.mu.Lock()
	defer j.mu.Unlock()
	j.denylist[jti] = until
	// Opportunistic prune so the map is bounded by active sessions.
	for k, v := range j.denylist {
		if v.Before(now) {
			delete(j.denylist, k)
		}
	}
}

// BumpUserGen invalidates every access token issued before this call
// for the given user (password change, H3).
func (j *JWTAuth) BumpUserGen(userID string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.userGen[userID]++
}

// ValidateRefreshToken parses and validates a JWT refresh token, returning its claims.
func (j *JWTAuth) ValidateRefreshToken(tokenString string) (*Claims, error) {
	claims, err := j.parseToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.Type != "refresh" {
		return nil, errors.New("token is not a refresh token")
	}
	return claims, nil
}

// HasRole checks if the claims contain the specified role.
func (c *Claims) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasAnyRole checks if the claims contain any of the specified roles.
func (c *Claims) HasAnyRole(roles []string) bool {
	for _, r := range c.Roles {
		for _, needed := range roles {
			if r == needed {
				return true
			}
		}
	}
	return false
}
