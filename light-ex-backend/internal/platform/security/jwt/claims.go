package jwt

import "github.com/golang-jwt/jwt/v5"

// Claims represents the JWT claims used by LightEx.
//
// Only immutable identity information should live here.
// Everything else should be loaded from the database.
type Claims struct {
	UserID    string `json:"user_id"`
	TokenType string `json:"token_type,omitempty"`

	jwt.RegisteredClaims
}
