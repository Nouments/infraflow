package gns3

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// These are unit tests against an httptest server. They are not GNS3
// integration tests; the real-server run is cmd "gns3 inventory" / the lab
// scenario reports.

type fakeController struct {
	t          *testing.T
	tokens     atomic.Int32
	expireOnce atomic.Bool
	v2Only     bool
}

func (fake *fakeController) handler() http.Handler {
	mux := http.NewServeMux()
	if fake.v2Only {
		mux.HandleFunc("GET /v2/version", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"version":"2.2.50","local":true}`)
		})
		mux.HandleFunc("GET /v2/projects", func(w http.ResponseWriter, r *http.Request) {
			if user, password, ok := r.BasicAuth(); !ok || user != "lab" || password != "s3cret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `[{"project_id":"p1","name":"one","status":"closed"}]`)
		})
		return mux
	}
	mux.HandleFunc("GET /v3/version", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			fake.t.Error("version probe must not send credentials")
		}
		_, _ = io.WriteString(w, `{"version":"3.1.0a4","local":false,"controller_host":"127.0.0.1"}`)
	})
	mux.HandleFunc("POST /v3/access/users/authenticate", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["username"] != "lab" || body["password"] != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"message":"Authentication was unsuccessful."}`)
			return
		}
		n := fake.tokens.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "tok" + string(rune('0'+n)), "token_type": "bearer"})
	})
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok") {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if fake.expireOnce.CompareAndSwap(true, false) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"message":"token expired"}`)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /v3/computes", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"compute_id":"local","name":"local","password":"**********"}]`)
	}))
	mux.HandleFunc("GET /v3/templates", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[
		 {"template_id":"t1","name":"Cisco IOSv","template_type":"qemu","hda_disk_image":"vios.qcow2","builtin":false},
		 {"template_id":"t2","name":"Missing","template_type":"qemu","hda_disk_image":"/images/QEMU/gone.qcow2","builtin":false},
		 {"template_id":"t3","name":"VPCS","template_type":"vpcs","builtin":true},
		 {"template_id":"t4","name":"dhcp","template_type":"docker","image":"debian-dhcp","cpus":0.0,"builtin":false},
		 {"template_id":"t5","name":"iol","template_type":"docker","image":"iol-xe/iol-xe:17-18-02","builtin":false}]`)
	}))
	mux.HandleFunc("GET /v3/images", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"filename":"vios.qcow2","image_type":"qemu","image_size":10}]`)
	}))
	mux.HandleFunc("GET /v3/computes/local/docker/images", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"image":"debian-dhcp:latest"},{"image":"alpine:3.22"}]`)
	}))
	mux.HandleFunc("GET /v3/projects", auth(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"project_id":"p1","name":"InfraFlow-Automated-Validation","status":"opened"}]`)
	}))
	mux.HandleFunc("POST /v3/projects", auth(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["auto_start"] != false || body["auto_open"] != false {
			fake.t.Errorf("lab project must not auto-open/auto-start: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"project_id": "p2", "name": body["name"], "status": "opened"})
	}))
	mux.HandleFunc("POST /v3/projects/p1/nodes/n1/start", auth(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(w, `{"message":"QEMU image not found"}`)
	}))
	return mux
}

func newFake(t *testing.T, fake *fakeController, user, password string) *Client {
	t.Helper()
	fake.t = t
	server := httptest.NewServer(fake.handler())
	t.Cleanup(server.Close)
	client, err := New(Config{URL: server.URL, Username: user, Password: password, Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestInventoryMarksTemplatesWhoseImageIsAbsent(t *testing.T) {
	client := newFake(t, &fakeController{}, "lab", "s3cret")
	inventory, err := client.CollectInventory(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Version.APIVersion != "v3" || inventory.Version.Version != "3.1.0a4" || len(inventory.Errors) != 0 {
		t.Fatalf("unexpected inventory header: %+v", inventory)
	}
	byName := map[string]TemplateAvailability{}
	for _, template := range inventory.Templates {
		byName[template.Name] = template
	}
	if _, ok := byName["VPCS"]; ok {
		t.Fatal("builtin template listed although excluded")
	}
	if got := byName["Cisco IOSv"].ImageRegistered; got == nil || !*got {
		t.Fatalf("registered image not recognised: %+v", byName["Cisco IOSv"])
	}
	if got := byName["Missing"].ImageRegistered; got == nil || *got {
		t.Fatalf("absent image must be reported as not registered: %+v", byName["Missing"])
	}
	if got := byName["dhcp"].ImageRegistered; got == nil || !*got {
		t.Fatalf("present docker image (implicit :latest, float cpus) not recognised: %+v", byName["dhcp"])
	}
	if got := byName["iol"]; got.ImageRegistered == nil || *got.ImageRegistered || !strings.Contains(got.Note, "not present") {
		t.Fatalf("absent docker image must be reported absent: %+v", got)
	}
}

func TestUndecodableListReturnsNothingRatherThanAPartialList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/version":
			_, _ = io.WriteString(w, `{"version":"3.1.0"}`)
		case "/v3/access/users/authenticate":
			_, _ = io.WriteString(w, `{"access_token":"tok"}`)
		default:
			_, _ = io.WriteString(w, `[{"project_id":"ok","name":"a"},{"project_id":42}]`)
		}
	}))
	defer server.Close()
	client, _ := New(Config{URL: server.URL, Username: "u", Password: "p"})
	projects, err := client.Projects(context.Background())
	if err == nil || projects != nil {
		t.Fatalf("partial list returned: %+v %v", projects, err)
	}
}

func TestClientReauthenticatesOnceAfterTokenExpiry(t *testing.T) {
	fake := &fakeController{}
	client := newFake(t, fake, "lab", "s3cret")
	if _, err := client.Projects(context.Background()); err != nil {
		t.Fatal(err)
	}
	fake.expireOnce.Store(true)
	if _, err := client.Projects(context.Background()); err != nil {
		t.Fatalf("expired token not renewed: %v", err)
	}
	if fake.tokens.Load() != 2 {
		t.Fatalf("expected exactly 2 authentications, got %d", fake.tokens.Load())
	}
}

func TestWrongPasswordIsTypedAndNeverEchoed(t *testing.T) {
	client := newFake(t, &fakeController{}, "lab", "wrong-password-value")
	_, err := client.Projects(context.Background())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
	if strings.Contains(err.Error(), "wrong-password-value") {
		t.Fatalf("password leaked into error: %v", err)
	}
}

func TestMissingCredentialsFailBeforeAnyAuthenticatedCall(t *testing.T) {
	fake := &fakeController{}
	client := newFake(t, fake, "", "")
	if _, err := client.Projects(context.Background()); !errors.Is(err, ErrUnauthorized) || !strings.Contains(err.Error(), EnvPassword) {
		t.Fatalf("want a configuration error naming %s, got %v", EnvPassword, err)
	}
	if fake.tokens.Load() != 0 {
		t.Fatal("authentication attempted without credentials")
	}
}

func TestControllerErrorsKeepStatusAndMessage(t *testing.T) {
	client := newFake(t, &fakeController{}, "lab", "s3cret")
	err := client.StartNode(context.Background(), "p1", "n1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict || apiErr.Message != "QEMU image not found" {
		t.Fatalf("controller error not preserved: %v", err)
	}
}

func TestCreateProjectNeverAutoStarts(t *testing.T) {
	client := newFake(t, &fakeController{}, "lab", "s3cret")
	project, err := client.CreateProject(context.Background(), "InfraFlow-Automated-Validation-2")
	if err != nil || project.ProjectID != "p2" {
		t.Fatalf("create project: %+v %v", project, err)
	}
	found, err := client.FindProject(context.Background(), "InfraFlow-Automated-Validation")
	if err != nil || found.ProjectID != "p1" {
		t.Fatalf("find project: %+v %v", found, err)
	}
	if _, err := client.FindProject(context.Background(), "absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent project must be ErrNotFound: %v", err)
	}
}

func TestV2FallbackUsesBasicAuthOnlyForAuthenticatedCalls(t *testing.T) {
	client := newFake(t, &fakeController{v2Only: true}, "lab", "s3cret")
	version, err := client.Version(context.Background())
	if err != nil || version.APIVersion != "v2" {
		t.Fatalf("v2 fallback: %+v %v", version, err)
	}
	projects, err := client.Projects(context.Background())
	if err != nil || len(projects) != 1 {
		t.Fatalf("v2 projects: %+v %v", projects, err)
	}
}

func TestUnreachableControllerTimesOut(t *testing.T) {
	blocked := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-blocked }))
	defer server.Close()
	defer close(blocked)
	client, err := New(Config{URL: server.URL, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := client.Version(context.Background()); err == nil {
		t.Fatal("hung controller did not time out")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("timeout not enforced")
	}
}

func TestConfigRejectsCredentialsInURLAndFlagsExposure(t *testing.T) {
	if _, err := New(Config{URL: "http://admin:admin@127.0.0.1:3080"}); err == nil {
		t.Fatal("credentials in URL accepted")
	}
	if _, err := New(Config{URL: "ftp://127.0.0.1"}); err == nil {
		t.Fatal("non-http URL accepted")
	}
	local, _ := New(Config{URL: "http://127.0.0.1:3080", Username: "admin", Password: "admin"})
	exposure := local.Exposure()
	if !exposure.Loopback || !exposure.DefaultCredentials || len(exposure.Warnings) != 1 {
		t.Fatalf("loopback default-credential exposure: %+v", exposure)
	}
	remote, _ := New(Config{URL: "http://203.0.113.10:3080", Username: "lab", Password: "x"})
	if exposure := remote.Exposure(); exposure.Loopback || exposure.PrivateNetwork || len(exposure.Warnings) != 2 {
		t.Fatalf("public cleartext exposure not flagged: %+v", exposure)
	}
}
