// MIT License
//
// Copyright (c) 2018-2024 Andrew Zak <andrew@linux.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
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

package sockets

import (
	"crypto/x509"
	"github.com/gorilla/websocket"
	"log"
	"net"
	"net/http"
)

// determineRealIP returns the real IP of the client
func determineRealIP(ws *websocket.Conn, realIP string) string {
	if realIP == "" {
		return ws.RemoteAddr().String()
	}
	if net.ParseIP(realIP) == nil {
		log.Printf("Invalid realIP provided: %s", realIP)
		return ws.RemoteAddr().String() // Fallback to the connection's remote address
	}
	return realIP
}

// getPeerCertificates returns the certificates from the request
// IMPORTANT:To get the client certificate, you have to set the tls config on the server side to RequestClientCert at minimum
// Example:
//
//	&tls.Config{
//		ClientAuth:         tls.RequestClientCert,
//	}
func getPeerCertificates(r *http.Request) (requestCerts []*x509.Certificate) {
	// Check if the request has a TLS connection
	if r.TLS == nil {
		return make([]*x509.Certificate, 0)
	}
	// Get the certificates from the connection
	return r.TLS.PeerCertificates
}
