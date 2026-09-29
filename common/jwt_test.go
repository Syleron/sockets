package common

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testKey = "test-secret-key-0123456789abcdef"

func sign(t *testing.T, method jwt.SigningMethod, claims jwt.Claims, key interface{}) string {
	t.Helper()
	s, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func claimsFor(username string, exp time.Time) JWT {
	return JWT{
		Username:         username,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(exp)},
	}
}

func TestDecodeJWT_Valid(t *testing.T) {
	tok := sign(t, jwt.SigningMethodHS256, claimsFor("alice", time.Now().Add(time.Hour)), []byte(testKey))
	ok, c := DecodeJWT(tok, testKey)
	if !ok {
		t.Fatal("expected valid token to be accepted")
	}
	if c.Username != "alice" {
		t.Fatalf("username = %q, want alice", c.Username)
	}
	if c.ExpiresAt == nil || c.ExpiresAt.Before(time.Now()) {
		t.Fatalf("ExpiresAt not decoded: %v", c.ExpiresAt)
	}
}

func TestDecodeJWT_NoExpIsAccepted(t *testing.T) {
	// exp is optional, as it was in v1 (jwt v3 StandardClaims).
	tok := sign(t, jwt.SigningMethodHS256, JWT{Username: "alice"}, []byte(testKey))
	if ok, _ := DecodeJWT(tok, testKey); !ok {
		t.Fatal("expected token without exp to be accepted")
	}
}

func TestDecodeJWT_Rejected(t *testing.T) {
	now := time.Now()
	valid := claimsFor("alice", now.Add(time.Hour))

	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	// Key confusion: an attacker who knows an RSA public key signs HS256 with
	// the PEM as the HMAC secret. DecodeJWT only ever verifies with the shared
	// secret, so this must fail; RS256 itself is rejected by alg pinning.
	pubDER, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	noneTok := sign(t, jwt.SigningMethodNone, valid, jwt.UnsafeAllowNoneSignatureType)
	hs256 := sign(t, jwt.SigningMethodHS256, valid, []byte(testKey))
	parts := strings.Split(hs256, ".")

	cases := map[string]string{
		"expired": sign(t, jwt.SigningMethodHS256, claimsFor("alice", now.Add(-time.Minute)), []byte(testKey)),
		"not yet valid (nbf)": sign(t, jwt.SigningMethodHS256, JWT{Username: "alice", RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(2 * time.Hour)),
			NotBefore: jwt.NewNumericDate(now.Add(time.Hour)),
		}}, []byte(testKey)),
		"issued in the future (iat)": sign(t, jwt.SigningMethodHS256, JWT{Username: "alice", RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(2 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now.Add(time.Hour)),
		}}, []byte(testKey)),
		"bad signature":          sign(t, jwt.SigningMethodHS256, valid, []byte("some-other-key")),
		"tampered payload":       parts[0] + "." + strings.Split(sign(t, jwt.SigningMethodHS256, claimsFor("mallory", now.Add(time.Hour)), []byte("x")), ".")[1] + "." + parts[2],
		"stripped signature":     parts[0] + "." + parts[1] + ".",
		"wrong alg HS384":        sign(t, jwt.SigningMethodHS384, valid, []byte(testKey)),
		"wrong alg HS512":        sign(t, jwt.SigningMethodHS512, valid, []byte(testKey)),
		"alg none":               noneTok,
		"RS256":                  sign(t, jwt.SigningMethodRS256, valid, rsaKey),
		"HS256 with RSA pub key": sign(t, jwt.SigningMethodHS256, valid, pubPEM),
		"missing username":       sign(t, jwt.SigningMethodHS256, claimsFor("", now.Add(time.Hour)), []byte(testKey)),
		"empty":                  "",
		"garbage":                "abc",
		"two segments":           parts[0] + "." + parts[1],
	}
	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			ok, c := DecodeJWT(tok, testKey)
			if ok {
				t.Fatalf("expected rejection, got accepted with %+v", c)
			}
			if !reflect.DeepEqual(c, JWT{}) {
				t.Fatalf("expected zero JWT on rejection, got %+v", c)
			}
		})
	}
}

func TestDecodeJWT_EmptyKeyRejected(t *testing.T) {
	// A token signed with an empty key must not validate against an empty key.
	tok := sign(t, jwt.SigningMethodHS256, claimsFor("alice", time.Now().Add(time.Hour)), []byte(""))
	if ok, _ := DecodeJWT(tok, ""); ok {
		t.Fatal("expected empty key to be rejected")
	}
}

func TestDecodeJWTNoVerify(t *testing.T) {
	tok := sign(t, jwt.SigningMethodHS256, claimsFor("alice", time.Now().Add(-time.Hour)), []byte("unknown-key"))
	claims, err := DecodeJWTNoVerify(tok)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Unverified by design: expired token with an unknown key still parses.
	if claims["username"] != "alice" {
		t.Fatalf("username = %v, want alice", claims["username"])
	}
}

func TestDecodeJWTNoVerify_Malformed(t *testing.T) {
	for name, tok := range map[string]string{
		"empty":         "",
		"garbage":       "abc",
		"two segments":  "eyJhbGciOiJIUzI1NiJ9.eyJ1c2VybmFtZSI6ImEifQ",
		"bad base64":    "!!!.@@@.###",
		"non-JSON body": "eyJhbGciOiJIUzI1NiJ9.bm90LWpzb24.sig",
	} {
		t.Run(name, func(t *testing.T) {
			claims, err := DecodeJWTNoVerify(tok)
			if err == nil {
				t.Fatalf("expected error, got claims %v", claims)
			}
			if claims != nil {
				t.Fatalf("expected nil claims on error, got %v", claims)
			}
		})
	}
}

func TestGenerateJWT_RoundTrip(t *testing.T) {
	tok, err := GenerateJWT("alice", testKey)
	if err != nil {
		t.Fatal(err)
	}
	ok, c := DecodeJWT(tok, testKey)
	if !ok {
		t.Fatal("GenerateJWT token rejected by DecodeJWT")
	}
	if c.Username != "alice" {
		t.Fatalf("username = %q, want alice", c.Username)
	}
	if c.ExpiresAt == nil {
		t.Fatal("expected exp claim")
	}
	if d := time.Until(c.ExpiresAt.Time); d <= 59*time.Minute || d > time.Hour {
		t.Fatalf("exp %v from now, want ~1h", d)
	}

	// Header must be HS256 and the legacy "id" claim is gone.
	raw, err := DecodeJWTNoVerify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := raw["id"]; has {
		t.Fatal("unexpected legacy id claim")
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(tok, jwt.MapClaims{})
	if err != nil {
		t.Fatal(err)
	}
	if alg := parsed.Header["alg"]; alg != "HS256" {
		t.Fatalf("alg = %v, want HS256", alg)
	}

	// Wrong key must fail.
	if ok, _ := DecodeJWT(tok, "wrong-key"); ok {
		t.Fatal("token accepted with wrong key")
	}
}

func TestGenerateJWT_EmptySecret(t *testing.T) {
	if _, err := GenerateJWT("alice", ""); err == nil {
		t.Fatal("expected error for empty secret")
	}
}
