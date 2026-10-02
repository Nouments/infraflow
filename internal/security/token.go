package security

import (
	"crypto/subtle"
	"strings"
)

const MinAgentTokenBytes = 32

func BearerTokenMatches(header string, expected []byte) bool {
	scheme, token, found := strings.Cut(header, " ")
	if !found || scheme != "Bearer" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), expected) == 1
}
