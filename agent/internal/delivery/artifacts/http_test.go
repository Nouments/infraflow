package artifacts

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

type memoryStore struct {
	files map[string][]byte
}

func (store memoryStore) OpenArtifact(path string) (io.ReadCloser, error) {
	data, ok := store.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestHandlerServesVerifiedArtifactsReadOnly(t *testing.T) {
	handler, err := NewHandler(memoryStore{files: map[string][]byte{
		"lab/bootstrap/dns/config.json": []byte("{\"ok\":true}\n"),
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/artifacts/lab/bootstrap/dns/config.json", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "{\"ok\":true}\n" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unexpected artifact response: %d %q %#v", response.Code, response.Body.String(), response.Header())
	}

	request = httptest.NewRequest(http.MethodPost, "/artifacts/lab/bootstrap/dns/config.json", nil)
	responseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST returned %d", responseRecorder.Code)
	}
	for _, path := range []string{"/artifacts/../secret", "/artifacts/lab/.infraflow-agent-state.json", "/other"} {
		request = httptest.NewRequest(http.MethodGet, path, nil)
		responseRecorder = httptest.NewRecorder()
		handler.ServeHTTP(responseRecorder, request)
		if responseRecorder.Code != http.StatusNotFound {
			t.Errorf("unsafe path %q returned %d", path, responseRecorder.Code)
		}
	}
}

func TestHandlerRequiresTokenWhenConfigured(t *testing.T) {
	secret := strings.Repeat("s", 32)
	handler, err := NewHandler(memoryStore{files: map[string][]byte{
		"lab/inventory.json": []byte("{}"),
	}}, secret)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/artifacts/lab/inventory.json", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("missing token returned %d", response.Code)
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("valid token returned %d", response.Code)
	}
}

func TestHandlerHealthEndpointIsMinimal(t *testing.T) {
	handler, err := NewHandler(memoryStore{files: map[string][]byte{}}, "")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "ok\n" {
		t.Fatalf("unexpected health response: %d %q", response.Code, response.Body.String())
	}
}
