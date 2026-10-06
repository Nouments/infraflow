package security

import (
	"crypto/subtle"
	"strings"
)

const MinAgentTokenBytes = 32

func BearerTokenMatches(header string, expected []byte) bool {
	token, found := BearerToken(header)
	if !found {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), expected) == 1
}

func BearerToken(header string) (string, bool) {
	scheme, token, found := strings.Cut(header, " ")
	if !found || scheme != "Bearer" || token == "" {
		return "", false
	}
	return token, true
}
