// MIT License
//
// Copyright (c) 2022 Andrew Zak <andrew@linux.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
/// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package common

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type MapClaims map[string]interface{}

// JWT holds the claims accepted by DecodeJWT: a required username plus the
// registered claims (exp, nbf, iat, ...).
type JWT struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

var (
	// ErrMissingUsername is returned (wrapped) by ParseJWT when the token's
	// username claim is empty. JWT.Validate returns it unwrapped.
	ErrMissingUsername = errors.New("missing username")
	// ErrEmptyKey is returned when an empty HMAC key is supplied to ParseJWT
	// or GenerateJWT.
	ErrEmptyKey = errors.New("empty signing key")
	// ErrInvalidToken wraps every ParseJWT rejection other than ErrEmptyKey.
	// The jwt/v5 cause (for example jwt.ErrTokenMalformed,
	// jwt.ErrTokenSignatureInvalid, jwt.ErrTokenExpired) is wrapped too, so
	// errors.Is matches both.
	ErrInvalidToken = errors.New("invalid token")
	// errUnexpectedClaims is returned if the parser yields an unexpected claims type.
	errUnexpectedClaims = errors.New("unexpected claims type")
)

// Validate implements jwt.ClaimsValidator. The parser calls it after the
// registered-claim checks, and only once the signature has been verified.
func (t JWT) Validate() error {
	if t.Username == "" {
		return ErrMissingUsername
	}
	return nil
}

// validMethods pins DecodeJWT to HS256. The token's alg header is not trusted:
// "none", other HMAC sizes and asymmetric algorithms are rejected before any
// signature check.
var validMethods = []string{jwt.SigningMethodHS256.Alg()}

// DecodeJWT verifies an HS256 token with tokenKey and returns its claims.
// It returns false if the key is empty, the alg is not HS256, the signature is
// invalid, the token is malformed, expired (exp), not yet valid (nbf) or issued
// in the future (iat), or the username claim is missing. exp, nbf and iat are
// optional, as in v1; when present they are enforced with no leeway.
//
// DecodeJWT does not log. Use ParseJWT to get the reason for a rejection.
func DecodeJWT(tokenString, tokenKey string) (bool, JWT) {
	claims, err := ParseJWT(tokenString, tokenKey)
	return err == nil, claims
}

// ParseJWT applies the same checks as DecodeJWT and returns the claims, or the
// zero JWT and an error describing the rejection:
//   - ErrEmptyKey if tokenKey is empty;
//   - otherwise an error wrapping ErrInvalidToken and the jwt/v5 cause, plus
//     ErrMissingUsername when the username claim is empty.
//
// The error text never includes the token or the key, but it can include the
// jwt/v5 parser's description of a malformed segment.
func ParseJWT(tokenString, tokenKey string) (JWT, error) {
	if tokenKey == "" {
		return JWT{}, ErrEmptyKey
	}
	var claims JWT
	token, err := jwt.ParseWithClaims(tokenString, &claims, func(*jwt.Token) (interface{}, error) {
		return []byte(tokenKey), nil
	},
		jwt.WithValidMethods(validMethods),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return JWT{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if !token.Valid {
		return JWT{}, ErrInvalidToken
	}
	return claims, nil
}

// DecodeJWTNoVerify parses a token WITHOUT verifying its signature or
// validating any claim. The returned claims are attacker-controlled and must
// never be used for authentication or authorisation; use DecodeJWT for that.
func DecodeJWTNoVerify(tokenString string) (jwt.MapClaims, error) {
	token, _, err := jwt.NewParser().ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errUnexpectedClaims
	}
	return claims, nil
}

// GenerateJWT returns an HS256 token carrying the username claim, signed with
// secret and expiring in one hour. DecodeJWT accepts it with the same secret.
func GenerateJWT(username, secret string) (string, error) {
	if secret == "" {
		return "", ErrEmptyKey
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, JWT{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	return token.SignedString([]byte(secret))
}
