package executors

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"infraflow/internal/domain"
	"infraflow/pkg/protocol"
	"infraflow/provider/internal/infrastructure/credentials"

	"golang.org/x/crypto/ssh"
)

// fakeRouterOS is an SSH server that implements the RouterOS behaviours the
// executor depends on: exec commands, the SCP sink, /import and /file remove.
type fakeRouterOS struct {
	mu             sync.Mutex
	password       string
	files          map[string][]byte
	commands       []string
	outputs        map[string]string
	importOutput   string
	silentImport   bool // pre-fix device behaviour: nothing printed, marker never reached
	dropAfterBytes int  // close the SCP channel after receiving this many content bytes
	connections    int
	hostKey        ssh.PublicKey
	host           string
	port           int
}

func newFakeRouterOS(t *testing.T) *fakeRouterOS {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeRouterOS{
		password:     "lab-test-secret",
		files:        map[string][]byte{},
		hostKey:      signer.PublicKey(),
		importOutput: "#line 1\r\n/system identity set name=\"mt1\"\r\n",
		outputs: map[string]string{
			":put [/system resource get version]": "7.8 (stable)\r\n",
			":put [/system identity get name]":    "mt1\r\n",
			"/ip address print terse":             " 0   address=192.0.2.21/24 network=192.0.2.0 interface=ether1 actual-interface=ether1 comment=infraflow:mt1:ether1\r\n 1   address=198.51.100.129/25 network=198.51.100.128 interface=ether2 actual-interface=ether2 comment=\"infraflow:mt1:ether2\"\r\n",
			"/ip dhcp-client print terse":         " 0   interface=ether1 add-default-route=yes use-peer-dns=yes status=bound\r\n",
			"/ip route print terse":               " 0  As dst-address=203.0.113.0/24 routing-table=main gateway=192.0.2.2 immediate-gw=192.0.2.2%ether1 distance=1 scope=30 target-scope=10\r\n 1  DAc dst-address=192.0.2.0/24 routing-table=main gateway=ether1 immediate-gw=ether1 distance=0 scope=10\r\n",
			"/routing ospf instance print terse":  " 0    name=infraflow-v2 version=2 vrf=main router-id=192.0.2.21\r\n",
		},
	}
	config := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if string(password) != fake.password {
			return nil, errors.New("invalid credentials")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			fake.mu.Lock()
			fake.connections++
			fake.mu.Unlock()
			go fake.serve(conn, config)
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	fake.host = host
	fake.port, _ = strconv.Atoi(portText)
	return fake
}

func (f *fakeRouterOS) serve(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()
	_, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for newChannel := range chans {
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer channel.Close()
			for request := range requests {
				if request.Type != "exec" {
					_ = request.Reply(false, nil)
					continue
				}
				var payload struct{ Command string }
				_ = ssh.Unmarshal(request.Payload, &payload)
				_ = request.Reply(true, nil)
				status := f.exec(channel, payload.Command)
				_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				return
			}
		}()
	}
}

func (f *fakeRouterOS) exec(channel ssh.Channel, command string) uint32 {
	f.mu.Lock()
	f.commands = append(f.commands, command)
	f.mu.Unlock()
	switch {
	case strings.HasPrefix(command, "scp -t "):
		return f.scpSink(channel, strings.TrimPrefix(command, "scp -t "))
	case strings.HasPrefix(command, "/import file-name="):
		name := strings.Fields(strings.TrimPrefix(command, "/import file-name="))[0]
		f.mu.Lock()
		_, exists := f.files[name]
		f.mu.Unlock()
		if !exists {
			_, _ = io.WriteString(channel, "failure: no such file\r\n")
			return 1
		}
		// Mirrors RouterOS 7.8: a failing verbose import exits 1 and the
		// chained marker command never runs.
		_, _ = io.WriteString(channel, f.importOutput)
		if f.silentImport {
			return 0
		}
		if strings.Contains(f.importOutput, "failure:") {
			return 1
		}
		if marker := `:put "` + routerOSImportMarker + `"`; strings.Contains(command, marker) {
			_, _ = io.WriteString(channel, routerOSImportMarker+"\r\n")
		}
		return 0
	case strings.HasPrefix(command, "/file remove"):
		f.mu.Lock()
		for name := range f.files {
			if strings.Contains(command, `"`+name+`"`) {
				delete(f.files, name)
			}
		}
		f.mu.Unlock()
		return 0
	}
	f.mu.Lock()
	output, ok := f.outputs[command]
	f.mu.Unlock()
	if !ok {
		_, _ = io.WriteString(channel, "bad command name\r\n")
		return 1
	}
	_, _ = io.WriteString(channel, output)
	return 0
}

func (f *fakeRouterOS) scpSink(channel ssh.Channel, name string) uint32 {
	reader := bufio.NewReader(channel)
	_, _ = channel.Write([]byte{0})
	header, err := reader.ReadString('\n')
	if err != nil {
		return 1
	}
	var mode string
	var size int
	var headerName string
	if _, err := fmt.Sscanf(header, "C%s %d %s", &mode, &size, &headerName); err != nil || headerName != name {
		_, _ = channel.Write([]byte("\x01bad header\n"))
		return 1
	}
	_, _ = channel.Write([]byte{0})
	content := make([]byte, size)
	limit := size
	if f.dropAfterBytes > 0 && f.dropAfterBytes < size {
		limit = f.dropAfterBytes
	}
	if _, err := io.ReadFull(reader, content[:limit]); err != nil {
		return 1
	}
	// RouterOS keeps whatever arrived, as a real interrupted transfer would.
	f.mu.Lock()
	f.files[name] = content[:limit]
	f.mu.Unlock()
	if limit < size {
		_ = channel.Close()
		return 1
	}
	if terminator, err := reader.ReadByte(); err != nil || terminator != 0 {
		return 1
	}
	_, _ = channel.Write([]byte{0})
	return 0
}

type intentResolver struct{ device AuthorizedDevice }

func (r intentResolver) ResolveDevice(context.Context, string, string) (AuthorizedDevice, error) {
	return r.device, nil
}

const testRouterOSScript = "/system identity set name=\"mt1\"\n"

func routerOSFixture(t *testing.T, fake *fakeRouterOS, catalogHashOverride ...string) (*RouterOSImportExecutor, *mockArtifactRepo) {
	t.Helper()
	catalogHash := strings.Join(catalogHashOverride, "")
	enabled := true
	intent := &domain.Device{
		Name: "mt1", Vendor: "mikrotik", Family: "routeros", Model: "CHR", OSVersion: "7.8",
		Provisioning: domain.Provisioning{Method: "auto-rsc"},
		Network: &domain.DeviceNetwork{
			Interfaces: []domain.NetworkInterface{
				{Name: "ether1", IPv4Mode: "static", IPv4Address: "192.0.2.21/24"},
				{Name: "ether2", IPv4Mode: "static", IPv4Address: "198.51.100.129/25"},
			},
			Routes: []domain.StaticRoute{{Destination: "203.0.113.0/24", NextHop: "192.0.2.2"}},
		},
		Routing: &domain.Routing{OSPF: &domain.OSPFConfig{Enabled: &enabled, RouterID: "192.0.2.21"}},
	}
	target := AuthorizedDevice{Site: "vendor-lab", DeviceID: "mt1", Name: "mt1", Vendor: "mikrotik", ManagementIP: fake.host, SSHPort: fake.port, Intent: intent}
	path := "vendor-lab/native/device-mt1.auto.rsc"
	if catalogHash == "" {
		sum := sha256.Sum256([]byte(testRouterOSScript))
		catalogHash = hex.EncodeToString(sum[:])
	}
	repo := &mockArtifactRepo{
		catalog: []protocol.Artifact{{Type: "native", Path: path, OutputHash: catalogHash}},
		files:   map[string][]byte{path: []byte(testRouterOSScript)},
	}
	verifier := NewStaticPublicKeyVerifier(map[string]ssh.PublicKey{net.JoinHostPort(fake.host, strconv.Itoa(fake.port)): fake.hostKey})
	transport, _ := NewSSHRouterOSTransport(verifier, nil, 2*time.Second, 2*time.Second)
	creds := credentials.NewStaticDeviceCredentialResolver(map[string]credentials.DeviceCredentials{
		"vendor-lab/mt1": {Username: "infraflow", Password: "lab-test-secret"},
	})
	executor, err := NewRouterOSImportExecutor(intentResolver{target}, creds, transport, repo)
	if err != nil {
		t.Fatal(err)
	}
	return executor, repo
}

func runRouterOSImport(t *testing.T, executor *RouterOSImportExecutor) (RouterOSImportResult, error) {
	t.Helper()
	params, err := executor.ValidateParameters(json.RawMessage(`{"site":"vendor-lab","device_id":"mt1"}`))
	if err != nil {
		t.Fatal(err)
	}
	res, execErr := executor.Execute(context.Background(), domain.ExecutionTask{Parameters: params})
	var result RouterOSImportResult
	if err := json.Unmarshal(res.Output, &result); err != nil {
		t.Fatalf("result must always carry structured evidence: %v", err)
	}
	if (execErr == nil) != (res.Status == domain.ExecutionTaskSucceeded) || (execErr == nil) != result.Verified {
		t.Fatalf("status, error and verification disagree: %+v %v", res, execErr)
	}
	return result, execErr
}

func TestRouterOSImportDeliversVerifiesAndCleansUp(t *testing.T) {
	fake := newFakeRouterOS(t)
	executor, _ := routerOSFixture(t, fake)
	result, err := runRouterOSImport(t, executor)
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.UploadedFile != "infraflow-mt1.rsc" || strings.HasSuffix(result.UploadedFile, ".auto.rsc") {
		t.Fatalf("upload must not use the self-executing .auto.rsc suffix: %q", result.UploadedFile)
	}
	if len(fake.files) != 0 {
		t.Fatalf("uploaded script was left on the device: %v", fake.files)
	}
	if !contains(fake.commands, `/import file-name=infraflow-mt1.rsc verbose=yes; :put "INFRAFLOW-IMPORT-COMPLETE"`) || result.DeviceVersion != "7.8 (stable)" {
		t.Fatalf("import was not triggered explicitly: %v", fake.commands)
	}
	if result.Observed.Identity != "mt1" || len(result.Observed.Routes) != 2 {
		t.Fatalf("read-back incomplete: %+v", result.Observed)
	}
}

func TestRouterOSImportRefusesArtifactWithWrongHashBeforeConnecting(t *testing.T) {
	fake := newFakeRouterOS(t)
	executor, _ := routerOSFixture(t, fake, strings.Repeat("0", 64))
	_, err := runRouterOSImport(t, executor)
	if err == nil || !strings.Contains(err.Error(), "does not match its catalog hash") || fake.connections != 0 {
		t.Fatalf("tampered artifact must be refused without contacting the device: %v (connections %d)", err, fake.connections)
	}
}

func TestRouterOSImportReportsInterruptedTransferWithoutImporting(t *testing.T) {
	fake := newFakeRouterOS(t)
	fake.dropAfterBytes = 5
	executor, _ := routerOSFixture(t, fake)
	_, err := runRouterOSImport(t, executor)
	if err == nil || !strings.Contains(err.Error(), "script upload failed") || !strings.Contains(err.Error(), "no configuration was imported") {
		t.Fatalf("interrupted transfer not reported: %v", err)
	}
	for _, command := range fake.commands {
		if strings.HasPrefix(command, "/import") {
			t.Fatal("a partially transferred script must never be imported")
		}
	}
	if len(fake.files) != 0 {
		t.Fatalf("partial upload was not removed: %v", fake.files)
	}
}

func TestRouterOSImportFailureRequiresReadBeforeRetry(t *testing.T) {
	fake := newFakeRouterOS(t)
	fake.importOutput = "#line 3\r\n/ip address add address=192.0.2.21/24 interface=ether1\r\nfailure: already have such address\r\n"
	executor, _ := routerOSFixture(t, fake)
	result, err := runRouterOSImport(t, executor)
	if err == nil || !strings.Contains(err.Error(), "device state must be re-read before any retry") || result.ImportOutput == "" ||
		!strings.Contains(err.Error(), "failure: already have such address (script line 3 /ip address add") {
		t.Fatalf("failed import not reported with device output: %v %+v", err, result)
	}
}

func TestRouterOSImportDetectsStateThatDiffersFromIntent(t *testing.T) {
	fake := newFakeRouterOS(t)
	fake.outputs["/routing ospf instance print terse"] = " 0    name=infraflow-v2 version=2 router-id=10.9.9.9\r\n"
	fake.outputs["/ip address print terse"] = " 0   address=192.0.2.21/24 interface=ether1\r\n 1 X address=198.51.100.129/25 interface=ether2\r\n"
	executor, _ := routerOSFixture(t, fake)
	result, err := runRouterOSImport(t, executor)
	if err == nil || len(result.Mismatches) != 2 {
		t.Fatalf("expected OSPF and disabled-address mismatches, got %v %v", err, result.Mismatches)
	}
}

func TestRouterOSImportRejectsInvalidCredentialsWithoutLeakingThem(t *testing.T) {
	fake := newFakeRouterOS(t)
	fake.password = "different"
	executor, _ := routerOSFixture(t, fake)
	result, err := runRouterOSImport(t, executor)
	if err == nil || !strings.Contains(err.Error(), "unable to authenticate") || strings.Contains(result.Error, "lab-test-secret") {
		t.Fatalf("authentication failure not reported safely: %v", err)
	}
}

func TestRouterOSImportReportsUnreachableDevice(t *testing.T) {
	fake := newFakeRouterOS(t)
	executor, _ := routerOSFixture(t, fake)
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	closedPort := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	resolver := executor.targets.(intentResolver)
	resolver.device.SSHPort = closedPort
	executor.targets = resolver
	_, err := runRouterOSImport(t, executor)
	if err == nil || !strings.Contains(err.Error(), "connection refused") || !strings.Contains(err.Error(), "no configuration was imported") {
		t.Fatalf("unreachable device not reported: %v", err)
	}
}

func TestRouterOSImportRefusesRouterOS6(t *testing.T) {
	fake := newFakeRouterOS(t)
	fake.outputs[":put [/system resource get version]"] = "6.49.10 (long-term)\r\n"
	executor, _ := routerOSFixture(t, fake)
	_, err := runRouterOSImport(t, executor)
	if err == nil || !strings.Contains(err.Error(), "targets RouterOS 7") || len(fake.files) != 0 {
		t.Fatalf("RouterOS 6 must be refused before upload: %v", err)
	}
}

func TestRouterOSImportValidatesParametersStrictly(t *testing.T) {
	fake := newFakeRouterOS(t)
	executor, _ := routerOSFixture(t, fake)
	for _, raw := range []string{`{}`, `{"site":"a","device_id":"../x"}`, `{"site":"a","device_id":"b","script":"x"}`, `{"site":"a","device_id":"b"} {}`} {
		if _, err := executor.ValidateParameters(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if executor.Retryable(errors.New("x")) {
		t.Fatal("RouterOS imports must never be retried automatically")
	}
}

func TestRouterOSInventoryResolverAuthorizesOnlyMikroTik(t *testing.T) {
	root := t.TempDir()
	writeResolverInventory(t, root, `{"site":"lab","devices":[{"name":"mt1","vendor":"mikrotik","management":{"ipv4":"192.0.2.21"},"provisioning":{"method":"auto-rsc"}},{"name":"r1","vendor":"cisco","management":{"ipv4":"192.0.2.11"}}]}`)
	resolver := NewArtifactInventoryDeviceResolver(root).ForVendor("mikrotik")
	device, err := resolver.ResolveDevice(context.Background(), "lab", "mt1")
	if err != nil || device.Intent == nil || device.Intent.Provisioning.Method != "auto-rsc" {
		t.Fatalf("MikroTik target or its intent not resolved: %+v %v", device, err)
	}
	if _, err := resolver.ResolveDevice(context.Background(), "lab", "r1"); !errors.Is(err, ErrTargetVendorUnsupported) {
		t.Fatalf("Cisco device must not be authorized for RouterOS imports: %v", err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// Regression (GNS3 CHR 7.8): a failing import over SSH exec printed nothing
// and exited 0. Silence must never be read as success.
func TestRouterOSImportTreatsSilentImportAsFailure(t *testing.T) {
	fake := newFakeRouterOS(t)
	fake.importOutput = ""
	fake.silentImport = true
	executor, _ := routerOSFixture(t, fake)
	if _, err := runRouterOSImport(t, executor); err == nil || !strings.Contains(err.Error(), "did not confirm the import") {
		t.Fatalf("silent import accepted: %v", err)
	}
}
