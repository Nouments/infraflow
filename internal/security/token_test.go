package security

import "testing"

func TestBearerTokenMatchesOnlyExactSchemeAndToken(t *testing.T) {
	secret := []byte("a token longer than thirty-two bytes")
	for _, header := range []string{"Bearer a token longer than thirty-two bytes", "bearer a token longer than thirty-two bytes", "Bearer wrong", "Basic a token longer than thirty-two bytes"} {
		matched := BearerTokenMatches(header, secret)
		if (header == "Bearer a token longer than thirty-two bytes") != matched {
			t.Errorf("unexpected match result for %q: %v", header, matched)
		}
	}
}
