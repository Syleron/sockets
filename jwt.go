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

