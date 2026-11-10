package jwt

import (
	"errors"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"

	"github.com/light-group/light-ex-backend/internal/platform/config"
)

var (
	ErrInvalidToken = errors.New("invalid token")
)

// Service defines JWT operations.
type Service interface {
	GenerateAccessToken(userID string) (string, error)
	GenerateRefreshToken(userID string) (string, error)
	Validate(token string) (*Claims, error)

	AccessTokenTTL() time.Duration
	RefreshTokenTTL() time.Duration
}

// JWTService implements Service.
type JWTService struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// Compile-time interface check.
var _ Service = (*JWTService)(nil)

// New creates a JWT service.
func New(cfg config.JWTConfig) Service {
	return &JWTService{
		secret:     []byte(cfg.Secret),
		issuer:     cfg.Issuer,
		accessTTL:  cfg.AccessTokenTTL,
		refreshTTL: cfg.RefreshTokenTTL,
	}
}

// GenerateAccessToken creates a signed access token.
func (s *JWTService) GenerateAccessToken(
	userID string,
) (string, error) {

	now := time.Now().UTC()

	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwtv5.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			IssuedAt:  jwtv5.NewNumericDate(now),
			NotBefore: jwtv5.NewNumericDate(now),
			ExpiresAt: jwtv5.NewNumericDate(now.Add(s.accessTTL)),
		},
		TokenType: "access",
	}

	token := jwtv5.NewWithClaims(
		jwtv5.SigningMethodHS256,
		claims,
	)

	return token.SignedString(s.secret)
}

// GenerateRefreshToken creates a signed refresh token.
func (s *JWTService) GenerateRefreshToken(
	userID string,
) (string, error) {

	now := time.Now().UTC()

	claims := Claims{
		UserID: userID,
		RegisteredClaims: jwtv5.RegisteredClaims{
			Issuer:    s.issuer,
			Subject:   userID,
			IssuedAt:  jwtv5.NewNumericDate(now),
			NotBefore: jwtv5.NewNumericDate(now),
			ExpiresAt: jwtv5.NewNumericDate(now.Add(s.refreshTTL)),
		},
		TokenType: "refresh",
	}

	token := jwtv5.NewWithClaims(
		jwtv5.SigningMethodHS256,
		claims,
	)

	return token.SignedString(s.secret)
}

// Validate validates and parses a JWT.
func (s *JWTService) Validate(
	tokenString string,
) (*Claims, error) {

	token, err := jwtv5.ParseWithClaims(
		tokenString,
		&Claims{},
		func(token *jwtv5.Token) (any, error) {

			if _, ok := token.Method.(*jwtv5.SigningMethodHMAC); !ok {
				return nil, ErrInvalidToken
			}

			return s.secret, nil
		},
	)

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)

	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// AccessTokenTTL returns the configured access token lifetime.
func (s *JWTService) AccessTokenTTL() time.Duration {
	return s.accessTTL
}

// RefreshTokenTTL returns the configured refresh token lifetime.
func (s *JWTService) RefreshTokenTTL() time.Duration {
	return s.refreshTTL
}
