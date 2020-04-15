/**
MIT License

Copyright (c) 2018 Andrew Zak <andrew@linux.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/
package common

import (
	"errors"
	"github.com/dgrijalva/jwt-go"
	"log"
	"time"
)

type MapClaims map[string]interface{}

type JWT struct {
	Username string `json:"username"`
	jwt.StandardClaims
}

func (t *JWT) validate() error {
	var errMsgs string
	if t.Username == "" {
		return errors.New("missing username")
	}
	if errMsgs == "" {
		return nil
	} else {
		return errors.New(errMsgs)
	}
}

func DecodeJWT(tokenString, tokenKey string) (bool, JWT) {
	var jwtt = JWT{}
	token, err := jwt.ParseWithClaims(tokenString, &jwtt, func(token *jwt.Token) (interface{}, error) {
		return []byte(tokenKey), nil
	})
	if err != nil {
		return false, JWT{}
	}
	if token.Valid {
		claims, ok := token.Claims.(*JWT)
		if !ok {
			return false, JWT{}
		}
		err := claims.validate()
		if err != nil {
			log.Println(err)
			return false, JWT{}
		}
		return true, *claims
	}
	return false, JWT{}
}

func DecodeJWTNoVerify(tokenString string) (jwt.MapClaims, error) {
	token, _, err := new(jwt.Parser).ParseUnverified(tokenString, jwt.MapClaims{})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok {
		return claims, nil
	} else {
		return nil, err
	}
}

func GenerateJWT(username, secret string) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"id": username,
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", err
	}
	return tokenString, nil
}
