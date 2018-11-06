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
package sockets

import (
	"log"
	"github.com/dgrijalva/jwt-go"
	"errors"
)

var JWTKey string

type MapClaims map[string]interface{}

type JWT struct {
	Username string `json:"id"`
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

func decodeJWT(tokenString string) (bool, JWT) {
	var jwtt = JWT{}
	token, err := jwt.ParseWithClaims(tokenString, &jwtt, func(token *jwt.Token) (interface{}, error) {
		return []byte(JWTKey), nil
	})
	check(err)
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

