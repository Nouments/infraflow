package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	configadapter "infraflow/internal/adapters/config"
	"infraflow/internal/diagnostics"
	"infraflow/internal/domain"
	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/observability"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/adapters/generation"
	"infraflow/provider/internal/application"
	userdomain "infraflow/provider/internal/domain"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
}

type operationsFixture struct {
	handler    http.Handler
	adminToken string
	agentToken string
	clock      *testClock
	service    *application.Service
	logs       *observability.Store
}

func newOperationsFixture(t *testing.T, withCatalog bool) operationsFixture {
	t.Helper()
	root := t.TempDir()
	if withCatalog {
		input, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "examples", "multisite-preparation.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		infrastructure, err := configadapter.Parser{}.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := (generation.Generator{}).GenerateAll(infrastructure, root); err != nil {
			t.Fatalf("generate catalog: %v", err)
		}
	}
	agents, err := filesystem.NewAgentStore(root)
	if err != nil {
		t.Fatal(err)
	}
	events, err := filesystem.NewEventStore(root)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := observability.NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	service := application.NewServiceWithJobsAgentsEvents(filesystem.NewArtifactRepository(root), nil, nil, nil, agents, events, application.Dependencies{Logs: logs, Parser: configadapter.Parser{}})
	clock := &testClock{now: time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)}
	service.SetAgentClock(clock.Now)
	if err := service.SetAgentHeartbeatTimeout(30 * time.Second); err != nil {
		t.Fatal(err)
	}
	agentToken := strings.Repeat("o", security.MinAgentTokenBytes)
	handler, adminToken := newUserSessionHandler(t, root, agentToken, service)
	return operationsFixture{handler: handler, adminToken: adminToken, agentToken: agentToken, clock: clock, service: service, logs: logs}
}

func TestHealthIsPublicAndMinimal(t *testing.T) {
	fixture := newOperationsFixture(t, false)
	response := authRequest(t, fixture.handler, http.MethodGet, "/api/v1/health", "", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"ok"`) || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("health = %d %s", response.Code, response.Body.String())
	}
	for _, forbidden := range []string{"agent", "site", "token"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("public health disclosed %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestOperationsRoutesRequireAdministrator(t *testing.T) {
	fixture := newOperationsFixture(t, false)
	authenticator := fixture.handler.(*Handler).authenticator
	if _, err := authenticator.CreateUser(t.Context(), "operator", "a long operator password", userdomain.UserRoleUser); err != nil {
		t.Fatal(err)
	}
	userSession, err := authenticator.Login(t.Context(), "operator", "a long operator password")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/status", "/api/v1/sites", "/api/v1/sites/site-a", "/api/v1/diagnostics?agent_id=agent-a"} {
		if code := authRequest(t, fixture.handler, http.MethodGet, path, "", "").Code; code != http.StatusUnauthorized {
			t.Fatalf("%s anonymous = %d, want 401", path, code)
		}
		if code := authRequest(t, fixture.handler, http.MethodGet, path, "", userSession.Token).Code; code != http.StatusForbidden {
			t.Fatalf("%s user role = %d, want 403", path, code)
		}
		if code := authRequest(t, fixture.handler, http.MethodGet, path, "", fixture.agentToken).Code; code != http.StatusForbidden {
			t.Fatalf("%s agent token = %d, want 403", path, code)
		}
	}
}

func TestAgentDisconnectRequiresAgentCredentialAndMatchingSite(t *testing.T) {
	fixture := newOperationsFixture(t, false)
	agentRequest(t, fixture.handler, http.MethodPost, "/api/v1/agents/register", `{"agent_id":"agent-a","site_id":"site-a"}`, fixture.agentToken)
	agentRequest(t, fixture.handler, http.MethodPost, "/api/v1/agents/agent-a/heartbeat", `{"site_id":"site-a","queue_depth":0}`, fixture.agentToken)
	if code := authRequest(t, fixture.handler, http.MethodPost, "/api/v1/agents/agent-a/disconnect", `{"site_id":"site-a"}`, fixture.adminToken).Code; code != http.StatusForbidden {
		t.Fatalf("user session could disconnect an agent: %d", code)
	}
	if code := agentRequest(t, fixture.handler, http.MethodPost, "/api/v1/agents/agent-a/disconnect", `{"site_id":"site-b"}`, fixture.agentToken).Code; code != http.StatusConflict {
		t.Fatalf("cross-site disconnect = %d, want 409", code)
	}
	response := agentRequest(t, fixture.handler, http.MethodPost, "/api/v1/agents/agent-a/disconnect", `{"site_id":"site-a"}`, fixture.agentToken)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"offline"`) {
		t.Fatalf("disconnect = %d %s", response.Code, response.Body.String())
	}
}

// The HTTP agent list must always reflect the evidence-based status.
func TestAgentListReflectsHeartbeatExpiry(t *testing.T) {
	fixture := newOperationsFixture(t, false)
	agentRequest(t, fixture.handler, http.MethodPost, "/api/v1/agents/register", `{"agent_id":"agent-a","site_id":"site-a"}`, fixture.agentToken)
	statusOf := func() domain.Agent {
		response := authRequest(t, fixture.handler, http.MethodGet, "/api/v1/agents/agent-a", "", fixture.adminToken)
		var agent domain.Agent
		if err := json.Unmarshal(response.Body.Bytes(), &agent); err != nil {
			t.Fatalf("agent response: %v %s", err, response.Body.String())
		}
		return agent
	}
	if got := statusOf(); got.Status != domain.AgentStatusUnknown {
		t.Fatalf("after register = %q", got.Status)
	}
	agentRequest(t, fixture.handler, http.MethodPost, "/api/v1/agents/agent-a/heartbeat", `{"site_id":"site-a","queue_depth":0}`, fixture.agentToken)
	if got := statusOf(); got.Status != domain.AgentStatusOnline {
		t.Fatalf("after heartbeat = %q", got.Status)
	}
	fixture.clock.Advance(31 * time.Second)
	got := statusOf()
	if got.Status != domain.AgentStatusOffline || got.StatusReason == "" || got.HeartbeatAgeSeconds == nil {
		t.Fatalf("after expiry = %+v", got)
	}
	status := authRequest(t, fixture.handler, http.MethodGet, "/api/v1/status", "", fixture.adminToken)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"offline":1`) || !strings.Contains(status.Body.String(), `"agent_heartbeat_timeout_seconds":30`) {
		t.Fatalf("status = %d %s", status.Code, status.Body.String())
	}
}

func TestSitesAreBuiltFromPublishedCatalogAndAgents(t *testing.T) {
	fixture := newOperationsFixture(t, true)
	agentRequest(t, fixture.handler, http.MethodPost, "/api/v1/agents/register", `{"agent_id":"agent-a","site_id":"site-a"}`, fixture.agentToken)
	response := authRequest(t, fixture.handler, http.MethodGet, "/api/v1/sites", "", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("sites = %d %s", response.Code, response.Body.String())
	}
	var sites []application.SiteSummary
	if err := json.Unmarshal(response.Body.Bytes(), &sites); err != nil {
		t.Fatal(err)
	}
	var siteA *application.SiteSummary
	for index := range sites {
		if sites[index].ID == "site-a" {
			siteA = &sites[index]
		}
	}
	if len(sites) != 4 || siteA == nil {
		t.Fatalf("expected four published sites including site-a: %+v", sites)
	}
	if siteA.ArtifactCount == 0 || len(siteA.Devices) != 1 || siteA.Devices[0].Name != "edge-a" || siteA.Devices[0].Vendor != "cisco" {
		t.Fatalf("site-a inventory not read from the published catalog: %+v", siteA)
	}
	if len(siteA.Agents) != 1 || siteA.Agents[0].Status != domain.AgentStatusUnknown || len(siteA.Capabilities) == 0 || len(siteA.Problems) != 0 {
		t.Fatalf("site-a agent/capability evidence wrong: %+v", siteA)
	}
	if code := authRequest(t, fixture.handler, http.MethodGet, "/api/v1/sites/site-zz", "", fixture.adminToken).Code; code != http.StatusNotFound {
		t.Fatalf("unknown site = %d", code)
	}
	if code := authRequest(t, fixture.handler, http.MethodGet, "/api/v1/sites/..%2fetc", "", fixture.adminToken).Code; code != http.StatusNotFound {
		t.Fatalf("traversal-like site id = %d", code)
	}
}

func TestDiagnosticsAPIExplainsMissingHeartbeatFromRealStores(t *testing.T) {
	fixture := newOperationsFixture(t, false)
	agentRequest(t, fixture.handler, http.MethodPost, "/api/v1/agents/register", `{"agent_id":"agent-a","site_id":"site-a"}`, fixture.agentToken)
	agentRequest(t, fixture.handler, http.MethodPost, "/api/v1/agents/agent-a/heartbeat", `{"site_id":"site-a","queue_depth":0}`, fixture.agentToken)
	// Agent-side events as ingested through the authenticated log API.
	ingest := `{"agent_id":"agent-a","events":[` +
		`{"event_id":"evt-run-1","timestamp":"2026-11-19T09:00:01Z","level":"INFO","event":"agent.artifact.completed","message":"artifact processing finished","service":"agent","source":"agent-runner","agent_id":"agent-a","site_id":"site-a","run_id":"run-1","status":"COMPLETED","line":"site-a/inventory.json"},` +
		`{"event_id":"evt-run-2","timestamp":"2026-11-19T09:00:02Z","level":"INFO","event":"agent.run.completed","message":"agent artifact run completed","service":"agent","source":"agent-runner","agent_id":"agent-a","site_id":"site-a","run_id":"run-1","status":"COMPLETED"}]}`
	if response := agentRequest(t, fixture.handler, http.MethodPost, "/api/v1/agent/logs", ingest, fixture.agentToken); response.Code != http.StatusOK && response.Code != http.StatusAccepted {
		t.Fatalf("log ingest = %d %s", response.Code, response.Body.String())
	}
	fixture.clock.Advance(5 * time.Minute)
	response := authRequest(t, fixture.handler, http.MethodGet, "/api/v1/diagnostics?agent_id=agent-a", "", fixture.adminToken)
	if response.Code != http.StatusOK {
		t.Fatalf("diagnostics = %d %s", response.Code, response.Body.String())
	}
	var report diagnostics.Report
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.FailureCode != "AGENT_HEARTBEAT_MISSING" || report.LastConfirmedSuccessfulStage != diagnostics.StageIntegrity || report.Agent == nil || report.Agent.Status != "offline" {
		t.Fatalf("diagnosis = %+v", report)
	}
	linked := false
	for _, evidence := range report.Evidence {
		if evidence.EventID == "evt-run-1" {
			linked = true
		}
	}
	if !linked {
		t.Fatalf("diagnosis does not cite the ingested Agent event: %+v", report.Evidence)
	}
	if code := authRequest(t, fixture.handler, http.MethodGet, "/api/v1/diagnostics", "", fixture.adminToken).Code; code != http.StatusBadRequest {
		t.Fatalf("diagnostics without selector = %d", code)
	}
}

func TestScopedAgentCredentialsBindHTTPIdentity(t *testing.T) {
	fixture := newOperationsFixture(t, false)
	handler := fixture.handler.(*Handler)
	tokenA, tokenB := strings.Repeat("A", security.MinAgentTokenBytes), strings.Repeat("B", security.MinAgentTokenBytes)
	if err := handler.SetAgentCredentials([]AgentCredential{{AgentID: "agent-a", SiteID: "site-a", Token: tokenA}, {AgentID: "agent-b", SiteID: "site-b", Token: tokenB}}); err != nil {
		t.Fatal(err)
	}
	expect := func(name string, got, want int) {
		t.Helper()
		if got != want {
			t.Fatalf("%s = %d, want %d", name, got, want)
		}
	}
	expect("agent-a registers itself", agentRequest(t, handler, http.MethodPost, "/api/v1/agents/register", `{"agent_id":"agent-a","site_id":"site-a"}`, tokenA).Code, http.StatusOK)
	expect("agent-b registers itself", agentRequest(t, handler, http.MethodPost, "/api/v1/agents/register", `{"agent_id":"agent-b","site_id":"site-b"}`, tokenB).Code, http.StatusOK)
	expect("agent-a registering as agent-b", agentRequest(t, handler, http.MethodPost, "/api/v1/agents/register", `{"agent_id":"agent-b","site_id":"site-b"}`, tokenA).Code, http.StatusForbidden)
	expect("agent-a heartbeating as agent-b", agentRequest(t, handler, http.MethodPost, "/api/v1/agents/agent-b/heartbeat", `{"site_id":"site-b","queue_depth":0}`, tokenA).Code, http.StatusForbidden)
	expect("agent-a disconnecting agent-b", agentRequest(t, handler, http.MethodPost, "/api/v1/agents/agent-b/disconnect", `{"site_id":"site-b"}`, tokenA).Code, http.StatusForbidden)
	expect("agent-a ingesting logs as agent-b", agentRequest(t, handler, http.MethodPost, "/api/v1/agent/logs", `{"agent_id":"agent-b","events":[{"event_id":"x1","timestamp":"2026-11-19T09:00:00Z","level":"INFO","event":"agent.run.completed","message":"m","service":"agent","source":"s"}]}`, tokenA).Code, http.StatusForbidden)
	expect("agent-a listing all agents", agentRequest(t, handler, http.MethodGet, "/api/v1/agents", "", tokenA).Code, http.StatusForbidden)
	expect("agent-a reading the audit trail", agentRequest(t, handler, http.MethodGet, "/api/v1/events", "", tokenA).Code, http.StatusForbidden)
	expect("agent-a reading agent-b", agentRequest(t, handler, http.MethodGet, "/api/v1/agents/agent-b", "", tokenA).Code, http.StatusForbidden)
	expect("agent-a reading itself", agentRequest(t, handler, http.MethodGet, "/api/v1/agents/agent-a", "", tokenA).Code, http.StatusOK)
	expect("shared token heartbeating once scoped credentials exist", agentRequest(t, handler, http.MethodPost, "/api/v1/agents/agent-a/heartbeat", `{"site_id":"site-a","queue_depth":0}`, fixture.agentToken).Code, http.StatusForbidden)
	expect("agent-a heartbeating itself", agentRequest(t, handler, http.MethodPost, "/api/v1/agents/agent-a/heartbeat", `{"site_id":"site-a","queue_depth":0}`, tokenA).Code, http.StatusOK)
	if got := fixture.service; got == nil {
		t.Fatal("service missing")
	}
	agentB, err := fixture.service.Agent("agent-b")
	if err != nil || agentB.Status != domain.AgentStatusUnknown {
		t.Fatalf("rejected cross-site requests changed agent-b: %+v %v", agentB, err)
	}
	if err := handler.SetAgentCredentials([]AgentCredential{{AgentID: "x", SiteID: "y", Token: "short"}}); err == nil {
		t.Fatal("short scoped token was accepted")
	}
}
