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
		"lab/bootstrap/dns/config.json":    []byte("{\"ok\":true}\n"),
		"lab/bootstrap/pxe/ipxe/menu.ipxe": []byte("#!ipxe\necho ready\n"),
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
	request = httptest.NewRequest(http.MethodGet, "/infraflow/lab/bootstrap/pxe/ipxe/menu.ipxe", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "#!ipxe\necho ready\n" {
		t.Fatalf("iPXE bootstrap path did not serve its published artifact: %d %q", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/artifacts/lab/bootstrap/dns/config.json", nil)
	responseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(responseRecorder, request)
	if responseRecorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST returned %d", responseRecorder.Code)
	}
	for _, path := range []string{"/artifacts/../secret", "/artifacts/lab/.infraflow-agent-state.json", "/infraflow/../secret", "/infraflow/lab/unknown.bin", "/other"} {
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

func TestBootstrapHandlerOnlyServesBootstrapArtifacts(t *testing.T) {
	handler, err := NewBootstrapHandler(memoryStore{files: map[string][]byte{
		"lab/bootstrap/pxe/ipxe/menu.ipxe": []byte("#!ipxe\n"),
		"lab/inventory.json":               []byte("{}\n"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path string
		code int
	}{
		{path: "/infraflow/lab/bootstrap/pxe/ipxe/menu.ipxe", code: http.StatusOK},
		{path: "/infraflow/lab/inventory.json", code: http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.code {
			t.Errorf("GET %s returned %d, want %d", test.path, response.Code, test.code)
		}
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
