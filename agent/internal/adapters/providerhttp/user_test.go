package providerhttp

import "testing"

func TestNewUserClientRequiresTLSForRemoteAddresses(t *testing.T) {
	if _, err := NewUserClient("http://10.0.0.5:8080", ""); err == nil {
		t.Fatal("remote plaintext TUI address was accepted")
	}
	if _, err := NewUserClient("https://10.0.0.5:8080", ""); err != nil {
		t.Fatalf("remote HTTPS TUI address was rejected: %v", err)
	}
	if _, err := NewUserClient("http://127.0.0.1:8080", ""); err != nil {
		t.Fatalf("loopback HTTP TUI address was rejected: %v", err)
	}
}
