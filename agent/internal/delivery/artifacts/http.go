package artifacts

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"strings"

	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/protocol"
)

const artifactPrefix = "/artifacts/"
const bootstrapPrefix = "/infraflow/"

const maxServedArtifactBytes = int64(8 << 30)

// Store is intentionally narrower than the agent state store. The HTTP
// service can only open already validated artifacts and cannot write state.
type Store interface {
	OpenArtifact(string) (io.ReadCloser, error)
}

type Handler struct {
	store         Store
	token         []byte
	bootstrapOnly bool
}

func NewHandler(store Store, token string) (*Handler, error) {
	if store == nil {
		return nil, fmt.Errorf("artifact store is required")
	}
	if token != "" && len([]byte(token)) < security.MinAgentTokenBytes {
		return nil, fmt.Errorf("artifact token must be at least %d bytes", security.MinAgentTokenBytes)
	}
	return &Handler{store: store, token: []byte(token)}, nil
}

func NewBootstrapHandler(store Store) (*Handler, error) {
	handler, err := NewHandler(store, "")
	if err != nil {
		return nil, err
	}
	handler.bootstrapOnly = true
	return handler, nil
}

func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.URL.Path == "/healthz" {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
		return
	}
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.Header().Set("Allow", "GET, HEAD")
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var relativePath string
	switch {
	case strings.HasPrefix(request.URL.Path, artifactPrefix):
		relativePath = strings.TrimPrefix(request.URL.Path, artifactPrefix)
	case strings.HasPrefix(request.URL.Path, bootstrapPrefix):
		relativePath = strings.TrimPrefix(request.URL.Path, bootstrapPrefix)
	default:
		http.NotFound(writer, request)
		return
	}
	if len(handler.token) > 0 && !security.BearerTokenMatches(request.Header.Get("Authorization"), handler.token) {
		writer.Header().Set("WWW-Authenticate", "Bearer")
		writer.WriteHeader(http.StatusUnauthorized)
		return
	}

	artifact, ok := artifactForPath(relativePath)
	if !ok || (handler.bootstrapOnly && !strings.Contains(relativePath, "/bootstrap/")) {
		http.NotFound(writer, request)
		return
	}
	file, err := handler.store.OpenArtifact(artifact.Path)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	defer file.Close()

	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Content-Type", contentType(artifact.Path))
	if seeker, ok := file.(io.Seeker); ok {
		size, err := seeker.Seek(0, io.SeekEnd)
		if err != nil || size > maxServedArtifactBytes {
			writer.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.Header().Set("Content-Length", fmt.Sprintf("%d", size))
	}
	if request.Method == http.MethodHead {
		writer.WriteHeader(http.StatusOK)
		return
	}
	if _, err := io.Copy(writer, io.LimitReader(file, maxServedArtifactBytes)); err != nil {
		return
	}
}

func artifactForPath(relativePath string) (protocol.Artifact, bool) {
	if relativePath == "" || strings.Contains(relativePath, "\\") || path.IsAbs(relativePath) || path.Clean(relativePath) != relativePath {
		return protocol.Artifact{}, false
	}
	site, _, found := strings.Cut(relativePath, "/")
	if !found || !protocol.ValidSiteName(site) {
		return protocol.Artifact{}, false
	}
	for _, artifactType := range []string{
		"inventory", "topology", "ansible_inventory", "ansible_playbook",
		"ansible_vendor_inventory", "ansible_requirements", "ansible_vendor_manifest",
		"terraform_versions", "terraform_providers", "terraform_variables", "terraform_locals",
		"terraform_main", "terraform_outputs", "terraform_tfvars_example",
		"bootstrap_dhcp", "bootstrap_dns", "bootstrap_tftp", "bootstrap_pxe",
		"bootstrap_ipxe_script", "bootstrap_ipxe_menu",
	} {
		artifact := protocol.Artifact{Type: artifactType, Path: relativePath}
		if protocol.ValidArtifactPath(site, artifact) {
			return artifact, true
		}
	}
	return protocol.Artifact{}, false
}

func contentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".yml"):
		return "application/yaml"
	case strings.HasSuffix(name, ".tfvars.example") || strings.HasSuffix(name, ".tf") || strings.HasSuffix(name, ".ipxe"):
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// IsLoopbackListenAddress is used by the CLI to require authentication when
// the artifact server is exposed beyond the local machine.
func IsLoopbackListenAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
