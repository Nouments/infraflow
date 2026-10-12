package executors

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"infraflow/internal/domain"
	"infraflow/internal/ports"
	"infraflow/provider/internal/adapters/filesystem"
	"infraflow/provider/internal/application"
	"infraflow/provider/internal/infrastructure/credentials"

	"golang.org/x/crypto/ssh"
)

type mockCiscoSession struct {
	responses     map[string]string
	errors        map[string]error
	recordedCmds  []string
	initialPrompt string
	closed        bool
	closeErr      error
}

func (s *mockCiscoSession) Close() error {
	s.closed = true
	return s.closeErr
}

func (s *mockCiscoSession) ReadInitialPrompt(ctx context.Context) (string, error) {
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	if s.initialPrompt != "" {
		return s.initialPrompt, nil
	}
	return "Router#", nil
}

func (s *mockCiscoSession) RunCommand(ctx context.Context, command string) (string, error) {
	if ctx != nil && ctx.Err() != nil {
		return "", ctx.Err()
	}
	clean := strings.TrimSpace(command)
	s.recordedCmds = append(s.recordedCmds, clean)
	if err, exists := s.errors[clean]; exists {
		return s.responses[clean], err
	}
	if resp, exists := s.responses[clean]; exists {
		return resp, nil
	}
	return clean + "\nRouter#", nil
}

type mockCiscoTransport struct {
	session    *mockCiscoSession
	connectErr error
}

func (t *mockCiscoTransport) Connect(ctx context.Context, target AuthorizedDevice, creds credentials.DeviceCredentials) (CiscoIOSSession, error) {
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if t.connectErr != nil {
		return nil, t.connectErr
	}
	return t.session, nil
}

func setupTestExecutor(session *mockCiscoSession, transportErr error) (*CiscoIOSConfigureExecutor, *StaticDeviceResolver, *credentials.StaticDeviceCredentialResolver) {
	targetResolver, _ := NewStaticDeviceResolver([]AuthorizedDevice{
		{
			Site:         "paris",
			DeviceID:     "rtr-core-01",
			Name:         "rtr-core-01",
			Vendor:       "cisco",
			Family:       "ios",
			ManagementIP: "192.168.10.1",
			SSHPort:      22,
		},
	})
	credResolver := credentials.NewStaticDeviceCredentialResolver(map[string]credentials.DeviceCredentials{
		"paris/rtr-core-01": {
			Username: "admin",
			Password: "superSecretPassword123!",
		},
	})
	transport := &mockCiscoTransport{
		session:    session,
		connectErr: transportErr,
	}
	executor, _ := NewCiscoIOSConfigureExecutor(targetResolver, credResolver, transport)
	return executor, targetResolver, credResolver
}

func standardMockSession() *mockCiscoSession {
	return &mockCiscoSession{
		responses: map[string]string{
			"terminal length 0":                       "terminal length 0\nRouter#",
			"terminal width 0":                        "terminal width 0\nRouter#",
			"configure terminal":                      "configure terminal\nEnter configuration commands, one per line.  End with CNTL/Z.\nRouter(config)#",
			"hostname rtr-core-01":                    "hostname rtr-core-01\nrtr-core-01(config)#",
			"interface GigabitEthernet0/0":            "interface GigabitEthernet0/0\nrtr-core-01(config-if)#",
			"description WAN Uplink to Paris HQ":      "description WAN Uplink to Paris HQ\nrtr-core-01(config-if)#",
			"ip address 10.0.0.1 255.255.255.252":     "ip address 10.0.0.1 255.255.255.252\nrtr-core-01(config-if)#",
			"no shutdown":                             "no shutdown\nrtr-core-01(config-if)#",
			"exit":                                    "exit\nrtr-core-01(config)#",
			"end":                                     "end\nrtr-core-01#",
			"show running-config | include ^hostname": "hostname rtr-core-01\nrtr-core-01#",
			"show running-config interface GigabitEthernet0/0": "Building configuration...\n\nCurrent configuration : 138 bytes\n!\ninterface GigabitEthernet0/0\n description WAN Uplink to Paris HQ\n ip address 10.0.0.1 255.255.255.252\n negotiation auto\nend\nrtr-core-01#",
		},
		errors: map[string]error{},
	}
}

func validTaskPayload() string {
	return `{
		"site": "paris",
		"device_id": "rtr-core-01",
		"hostname": "rtr-core-01",
		"interfaces": [
			{
				"name": "GigabitEthernet0/0",
				"description": "WAN Uplink to Paris HQ",
				"ipv4_cidr": "10.0.0.1/30",
				"admin_state": "up"
			}
		]
	}`
}

// 1. Paramètres valides
func TestCiscoIOSConfigureValidParameters(t *testing.T) {
	executor, _, _ := setupTestExecutor(standardMockSession(), nil)

	payload := validTaskPayload()
	validated, err := executor.ValidateParameters(json.RawMessage(payload))
	if err != nil {
		t.Fatalf("expected valid parameters to succeed, got %v", err)
	}

	var params CiscoIOSConfigureParameters
	if err := json.Unmarshal(validated, &params); err != nil {
		t.Fatalf("failed to unmarshal validated parameters: %v", err)
	}
	if params.Site != "paris" || params.DeviceID != "rtr-core-01" || params.Hostname != "rtr-core-01" {
		t.Fatalf("unexpected validated params: %+v", params)
	}
	if len(params.Interfaces) != 1 || params.Interfaces[0].Name != "GigabitEthernet0/0" {
		t.Fatalf("unexpected interface in validated params: %+v", params.Interfaces)
	}
}

// 2. Champs inconnus et JSON invalide
func TestCiscoIOSConfigureUnknownFieldsAndInvalidJSON(t *testing.T) {
	executor, _, _ := setupTestExecutor(standardMockSession(), nil)

	testCases := []struct {
		name    string
		payload string
	}{
		{"empty payload", ""},
		{"malformed json", "{not-valid-json"},
		{"trailing data", validTaskPayload() + " extra"},
		{"unknown field", `{"site":"paris","device_id":"rtr-01","interfaces":[{"name":"Gi0/0","admin_state":"up"}],"unknown":"field"}`},
		{"unknown field in interface", `{"site":"paris","device_id":"rtr-01","interfaces":[{"name":"Gi0/0","admin_state":"up","vlan":10}]}`},
		{"empty site", `{"site":"","device_id":"rtr-01","interfaces":[{"name":"Gi0/0","admin_state":"up"}]}`},
		{"empty device_id", `{"site":"paris","device_id":"","interfaces":[{"name":"Gi0/0","admin_state":"up"}]}`},
		{"empty interfaces", `{"site":"paris","device_id":"rtr-01","interfaces":[]}`},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := executor.ValidateParameters(json.RawMessage(tc.payload))
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

// 3. Adresses IPv4 ou préfixes invalides
func TestCiscoIOSConfigureInvalidIPv4(t *testing.T) {
	executor, _, _ := setupTestExecutor(standardMockSession(), nil)

	invalidCIDRs := []string{
		"999.999.999.999/24",
		"192.168.1.1",        // missing prefix
		"192.168.1.1/33",     // prefix out of range
		"192.168.1.1/0",      // prefix 0
		"2001:db8::1/64",     // IPv6 disallowed
		"224.0.0.1/24",       // multicast disallowed
		"127.0.0.1/8",        // loopback disallowed
		"255.255.255.255/32", // broadcast disallowed
		"0.0.0.0/0",          // zero disallowed
	}

	for _, cidr := range invalidCIDRs {
		t.Run(cidr, func(t *testing.T) {
			payload := `{
				"site": "paris",
				"device_id": "rtr-01",
				"interfaces": [{"name": "GigabitEthernet0/0", "ipv4_cidr": "` + cidr + `", "admin_state": "up"}]
			}`
			_, err := executor.ValidateParameters(json.RawMessage(payload))
			if err == nil {
				t.Fatalf("expected CIDR %q to fail validation", cidr)
			}
		})
	}
}

// 4. Interfaces dupliquées ou malformées
func TestCiscoIOSConfigureDuplicateOrMalformedInterfaces(t *testing.T) {
	executor, _, _ := setupTestExecutor(standardMockSession(), nil)

	t.Run("duplicate interfaces", func(t *testing.T) {
		payload := `{
			"site": "paris",
			"device_id": "rtr-01",
			"interfaces": [
				{"name": "GigabitEthernet0/0", "admin_state": "up"},
				{"name": "Gi0/0", "admin_state": "down"}
			]
		}`
		_, err := executor.ValidateParameters(json.RawMessage(payload))
		if err == nil || !strings.Contains(err.Error(), "duplicate interface") {
			t.Fatalf("expected duplicate interface error, got %v", err)
		}
	})

	t.Run("malformed interface name", func(t *testing.T) {
		payload := `{
			"site": "paris",
			"device_id": "rtr-01",
			"interfaces": [{"name": "Loopback0", "admin_state": "up"}]
		}`
		_, err := executor.ValidateParameters(json.RawMessage(payload))
		if err == nil || !strings.Contains(err.Error(), "invalid Cisco Ethernet interface name") {
			t.Fatalf("expected invalid interface error, got %v", err)
		}
	})

	t.Run("invalid admin state", func(t *testing.T) {
		payload := `{
			"site": "paris",
			"device_id": "rtr-01",
			"interfaces": [{"name": "GigabitEthernet0/0", "admin_state": "disabled"}]
		}`
		_, err := executor.ValidateParameters(json.RawMessage(payload))
		if err == nil || !strings.Contains(err.Error(), "admin_state must be 'up' or 'down'") {
			t.Fatalf("expected admin state error, got %v", err)
		}
	})
}

// 5. Descriptions avec retours à la ligne ou caractères de contrôle
func TestCiscoIOSConfigureInvalidDescription(t *testing.T) {
	executor, _, _ := setupTestExecutor(standardMockSession(), nil)

	invalidDescs := []string{
		"Description with \n newline",
		"Description with \r carriage return",
		"Description with \x00 null byte",
		"Description with \t tab byte",
	}

	for _, desc := range invalidDescs {
		t.Run("control char", func(t *testing.T) {
			data, _ := json.Marshal(map[string]any{
				"site":      "paris",
				"device_id": "rtr-01",
				"interfaces": []map[string]any{
					{"name": "GigabitEthernet0/0", "description": desc, "admin_state": "up"},
				},
			})
			_, err := executor.ValidateParameters(data)
			if err == nil {
				t.Fatalf("expected description with control chars to fail validation, got nil")
			}
		})
	}
}

// 6. Cible inconnue ou non autorisée
func TestCiscoIOSConfigureUnknownTarget(t *testing.T) {
	executor, _, _ := setupTestExecutor(standardMockSession(), nil)

	task := domain.ExecutionTask{
		Parameters: json.RawMessage(`{
			"site": "paris",
			"device_id": "unknown-router",
			"interfaces": [{"name": "GigabitEthernet0/0", "admin_state": "up"}]
		}`),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure for unauthorized target, got res=%+v, err=%v", res, err)
	}
	if !strings.Contains(res.Error, "target device authorization failed") {
		t.Fatalf("unexpected error message: %s", res.Error)
	}
}

// 7. Résolution des identifiants indisponible
func TestCiscoIOSConfigureCredentialsUnavailable(t *testing.T) {
	targetResolver, _ := NewStaticDeviceResolver([]AuthorizedDevice{
		{Site: "paris", DeviceID: "rtr-02", Name: "rtr-02", Vendor: "cisco", ManagementIP: "192.168.1.2", SSHPort: 22},
	})
	emptyCreds := credentials.NewStaticDeviceCredentialResolver(map[string]credentials.DeviceCredentials{})
	executor, _ := NewCiscoIOSConfigureExecutor(targetResolver, emptyCreds, &mockCiscoTransport{})

	task := domain.ExecutionTask{
		Parameters: json.RawMessage(`{
			"site": "paris",
			"device_id": "rtr-02",
			"interfaces": [{"name": "GigabitEthernet0/0", "admin_state": "up"}]
		}`),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure for missing credentials, got res=%+v, err=%v", res, err)
	}
	if !strings.Contains(res.Error, "target credentials resolution failed") {
		t.Fatalf("unexpected error message: %s", res.Error)
	}
}

// 8. Échec d'authentification SSH
func TestCiscoIOSConfigureSSHAuthFailure(t *testing.T) {
	executor, _, _ := setupTestExecutor(nil, errors.New("ssh: handshake failed: unable to authenticate"))

	task := domain.ExecutionTask{
		Parameters: json.RawMessage(validTaskPayload()),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure on ssh auth failure, got res=%+v, err=%v", res, err)
	}
	if !strings.Contains(res.Error, "SSH connection to target") {
		t.Fatalf("unexpected error message: %s", res.Error)
	}
}

// 9. Clé d'hôte inconnue ou modifiée
func TestHostKeyVerificationRules(t *testing.T) {
	t.Run("static key verifier rejects unknown host", func(t *testing.T) {
		verifier := NewStaticPublicKeyVerifier(map[string]ssh.PublicKey{})
		err := verifier.Verify("192.168.10.1:22", &net.TCPAddr{IP: net.ParseIP("192.168.10.1"), Port: 22}, nil)
		if err == nil || !errors.Is(err, ErrHostKeyUnknown) {
			t.Fatalf("expected ErrHostKeyUnknown, got %v", err)
		}
	})

	t.Run("fingerprint verifier rejects mismatch", func(t *testing.T) {
		verifier, _ := NewFixedFingerprintVerifier("SHA256:expectedHash123")
		// Passing a dummy key whose sum won't match
		mockKey := &mockPublicKey{marshaled: []byte("different-key")}
		err := verifier.Verify("192.168.10.1:22", nil, mockKey)
		if err == nil || !errors.Is(err, ErrHostKeyMismatch) {
			t.Fatalf("expected ErrHostKeyMismatch, got %v", err)
		}
	})
}

type mockPublicKey struct {
	marshaled []byte
}

func (m *mockPublicKey) Type() string                        { return "ssh-rsa" }
func (m *mockPublicKey) Marshal() []byte                     { return m.marshaled }
func (m *mockPublicKey) Verify([]byte, *ssh.Signature) error { return nil }

// 10. Expiration de délai et annulation
func TestCiscoIOSConfigureCancellationAndTimeout(t *testing.T) {
	executor, _, _ := setupTestExecutor(standardMockSession(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	task := domain.ExecutionTask{
		Parameters: json.RawMessage(validTaskPayload()),
	}

	res, err := executor.Execute(ctx, task)
	if err != context.Canceled || res.Status != domain.ExecutionTaskCancelled {
		t.Fatalf("expected ExecutionTaskCancelled with context.Canceled, got res=%+v, err=%v", res, err)
	}
}

// 11. Erreur IOS explicite
func TestCiscoIOSConfigureExplicitIOSError(t *testing.T) {
	session := standardMockSession()
	session.errors["ip address 10.0.0.1 255.255.255.252"] = errors.New("% Bad IP address or subnet mask")

	executor, _, _ := setupTestExecutor(session, nil)

	task := domain.ExecutionTask{
		Parameters: json.RawMessage(validTaskPayload()),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure on explicit IOS error, got res=%+v, err=%v", res, err)
	}
	if !strings.Contains(res.Error, "configuration application failed") {
		t.Fatalf("unexpected error message: %s", res.Error)
	}
}

// 12. Connexion interrompue
func TestCiscoIOSConfigureConnectionInterrupted(t *testing.T) {
	session := standardMockSession()
	session.errors["interface GigabitEthernet0/0"] = errors.New("read tcp 192.168.10.1: connection reset by peer")

	executor, _, _ := setupTestExecutor(session, nil)

	task := domain.ExecutionTask{
		Parameters: json.RawMessage(validTaskPayload()),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure on connection reset, got res=%+v, err=%v", res, err)
	}
}

// 13. Résultat partiel
func TestCiscoIOSConfigurePartialResultDetection(t *testing.T) {
	session := standardMockSession()
	// Fail on second command
	session.errors["description WAN Uplink to Paris HQ"] = errors.New("% Invalid input detected at '^' marker")

	executor, _, _ := setupTestExecutor(session, nil)

	task := domain.ExecutionTask{
		Parameters: json.RawMessage(validTaskPayload()),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure, got res=%+v, err=%v", res, err)
	}
	// Must indicate partial command count applied before failure
	if !strings.Contains(res.Error, "after 2 command(s)") {
		t.Fatalf("expected mention of applied commands count, got %s", res.Error)
	}
	if !strings.Contains(res.Error, "router state is uncertain and must be re-read before any retry") {
		t.Fatalf("partial configuration did not require a state read-back: %s", res.Error)
	}
}

// 14. Vérification finale différente de la demande
func TestCiscoIOSConfigureVerificationMismatch(t *testing.T) {
	session := standardMockSession()
	// Router running config has wrong IP
	session.responses["show running-config interface GigabitEthernet0/0"] = `Building configuration...
interface GigabitEthernet0/0
 description WAN Uplink to Paris HQ
 ip address 10.0.0.99 255.255.255.252
end
rtr-core-01#`

	executor, _, _ := setupTestExecutor(session, nil)

	task := domain.ExecutionTask{
		Parameters: json.RawMessage(validTaskPayload()),
	}

	res, err := executor.Execute(context.Background(), task)
	if err == nil || res.Status != domain.ExecutionTaskFailed {
		t.Fatalf("expected failure when observed state differs from requested, got res=%+v, err=%v", res, err)
	}
	if len(res.Output) != 0 || !strings.Contains(res.Error, "router state must be re-read before any retry") {
		t.Fatalf("verification failure was reported as partial success or omitted the read-back requirement: %+v", res)
	}
	if !strings.Contains(res.Error, "verification failed") || !strings.Contains(res.Error, "ipv4 mismatch") {
		t.Fatalf("unexpected error message: %s", res.Error)
	}
}

func TestCiscoIOSConfigureOversizedOutputCannotSucceed(t *testing.T) {
	session := standardMockSession()
	session.errors["show running-config interface GigabitEthernet0/0"] = ErrOutputTooLarge
	executor, _, _ := setupTestExecutor(session, nil)
	result, err := executor.Execute(context.Background(), domain.ExecutionTask{Parameters: json.RawMessage(validTaskPayload())})
	if err == nil || result.Status != domain.ExecutionTaskFailed || len(result.Output) != 0 {
		t.Fatalf("oversized verification output was accepted: result=%+v err=%v", result, err)
	}
	if !strings.Contains(result.Error, "exceeds the configured limit") || !strings.Contains(result.Error, "must be re-read") {
		t.Fatalf("oversized output error did not explain failure and required read-back: %s", result.Error)
	}
}

func TestCiscoIOSConfigureIsNeverAutomaticallyRetryable(t *testing.T) {
	executor, _, _ := setupTestExecutor(standardMockSession(), nil)
	for _, err := range []error{ErrPromptTimeout, context.DeadlineExceeded, errors.New("connection reset")} {
		if executor.Retryable(err) {
			t.Fatalf("Cisco configuration error was marked retryable: %v", err)
		}
	}
}

// 15. Succès uniquement après vérification positive
func TestCiscoIOSConfigureVerifiedSuccess(t *testing.T) {
	session := standardMockSession()
	executor, _, _ := setupTestExecutor(session, nil)

	task := domain.ExecutionTask{
		Parameters: json.RawMessage(validTaskPayload()),
	}

	res, err := executor.Execute(context.Background(), task)
	if err != nil || res.Status != domain.ExecutionTaskSucceeded {
		t.Fatalf("expected succeeded task after verification, got res=%+v, err=%v", res, err)
	}

	var output CiscoIOSConfigureResult
	if err := json.Unmarshal(res.Output, &output); err != nil {
		t.Fatalf("failed to decode output: %v", err)
	}

	if !output.Verified {
		t.Fatal("expected output.Verified to be true")
	}
	if output.Status != "succeeded" {
		t.Fatalf("unexpected output status: %s", output.Status)
	}
	if output.ObservedChanges.Hostname != "rtr-core-01" {
		t.Fatalf("unexpected observed hostname: %s", output.ObservedChanges.Hostname)
	}
	if len(output.ObservedChanges.Interfaces) != 1 {
		t.Fatalf("expected 1 observed interface, got %d", len(output.ObservedChanges.Interfaces))
	}
	obsIface := output.ObservedChanges.Interfaces[0]
	if obsIface.IPv4Address != "10.0.0.1" || obsIface.SubnetMask != "255.255.255.252" || obsIface.AdminState != "up" {
		t.Fatalf("unexpected observed interface: %+v", obsIface)
	}
	if len(output.Warnings) == 0 || !strings.Contains(output.Warnings[0], "write memory disabled by policy") {
		t.Fatalf("expected warning about startup-config not persisted, got %+v", output.Warnings)
	}
}

// 16. Absence de secrets dans les erreurs et les logs
func TestCiscoIOSConfigureNoSecretsInErrorsOrLogs(t *testing.T) {
	session := standardMockSession()
	secret := "superSecretPassword123!"

	// Intentionally cause an error with secret in raw transport error
	session.errors["interface GigabitEthernet0/0"] = errors.New("auth failed with password " + secret)

	executor, _, _ := setupTestExecutor(session, nil)

	task := domain.ExecutionTask{
		Parameters: json.RawMessage(validTaskPayload()),
	}

	res, _ := executor.Execute(context.Background(), task)
	if strings.Contains(res.Error, secret) {
		t.Fatalf("SECURITY VIOLATION: secret password leaked in execution result error: %s", res.Error)
	}
	if strings.Contains(string(res.Output), secret) {
		t.Fatalf("SECURITY VIOLATION: secret password leaked in execution output: %s", string(res.Output))
	}
}

// Tests d'intégration locaux avec ExecutionService, scheduler, persistence et coexistence avec artifact.verify
func TestCiscoIOSConfigureIntegrationWithExecutionService(t *testing.T) {
	tempDir := t.TempDir()
	execStore, err := filesystem.NewExecutionStore(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	eventStore, err := filesystem.NewEventStore(tempDir)
	if err != nil {
		t.Fatal(err)
	}

	session := standardMockSession()
	ciscoExecutor, _, _ := setupTestExecutor(session, nil)

	artifactRepo := &mockArtifactRepo{}
	artifactVerifier, err := NewArtifactVerifyExecutor(artifactRepo)
	if err != nil {
		t.Fatal(err)
	}

	// Register both artifact.verify and cisco.ios.configure
	execService, err := application.NewExecutionService(
		execStore,
		eventStore,
		[]ports.ExecutionExecutor{artifactVerifier, ciscoExecutor},
		2,
	)
	if err != nil {
		t.Fatalf("failed to initialize execution service: %v", err)
	}
	defer execService.Close(context.Background())

	// 1. Submit a valid cisco.ios.configure execution
	submission, isNew, err := execService.Submit(
		context.Background(),
		"admin",
		"idempotency-key-cisco-test-1234",
		application.ExecutionRequest{
			Tasks: []application.ExecutionTaskRequest{
				{
					ID:         "task-cisco-01",
					Type:       CiscoIOSConfigureType,
					Parameters: json.RawMessage(validTaskPayload()),
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("failed to submit execution: %v", err)
	}
	if !isNew {
		t.Fatal("expected execution to be new")
	}

	// Wait for execution completion
	waitCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished, err := execService.Wait(waitCtx, submission.ID)
	if err != nil {
		t.Fatalf("wait execution failed: %v", err)
	}

	if finished.Status != domain.ExecutionSucceeded {
		t.Fatalf("expected execution status %s, got %s (error: %s)", domain.ExecutionSucceeded, finished.Status, finished.Error)
	}
	if len(finished.Tasks) != 1 || finished.Tasks[0].Status != domain.ExecutionTaskSucceeded {
		t.Fatalf("expected task status succeeded, got %+v", finished.Tasks)
	}

	// Verify that output has been persisted correctly
	var result CiscoIOSConfigureResult
	if err := json.Unmarshal(finished.Tasks[0].Result, &result); err != nil {
		t.Fatalf("failed to unmarshal persisted result: %v", err)
	}
	if !result.Verified || result.Status != "succeeded" {
		t.Fatalf("unexpected persisted result: %+v", result)
	}

	// A timeout on the first mutating command must never schedule another application.
	session.errors["hostname rtr-core-01"] = ErrPromptTimeout
	timeoutSubmission, _, err := execService.Submit(context.Background(), "admin", "idempotency-key-cisco-timeout-1234", application.ExecutionRequest{
		Tasks: []application.ExecutionTaskRequest{{
			ID: "task-cisco-timeout", Type: CiscoIOSConfigureType,
			Parameters: json.RawMessage(validTaskPayload()),
		}},
	})
	if err != nil {
		t.Fatalf("failed to submit timeout execution: %v", err)
	}
	timeoutResult, err := execService.Wait(waitCtx, timeoutSubmission.ID)
	if err != nil {
		t.Fatalf("wait timeout execution failed: %v", err)
	}
	if len(timeoutResult.Tasks) != 1 || len(timeoutResult.Tasks[0].Attempts) != 1 || timeoutResult.Tasks[0].Status != domain.ExecutionTaskFailed || !strings.Contains(timeoutResult.Tasks[0].Error, "router state is uncertain and must be re-read before any retry") {
		t.Fatalf("uncertain Cisco configuration was retried or not flagged: %#v", timeoutResult.Tasks)
	}
	var hostnameAttempts int
	for _, command := range session.recordedCmds {
		if command == "hostname rtr-core-01" {
			hostnameAttempts++
		}
	}
	if hostnameAttempts != 2 {
		t.Fatalf("expected one hostname application per execution, got %d", hostnameAttempts)
	}

	// 2. Verify that unknown task types are still rejected
	_, _, err = execService.Submit(
		context.Background(),
		"admin",
		"idempotency-key-unknown-task-1234",
		application.ExecutionRequest{
			Tasks: []application.ExecutionTaskRequest{
				{
					ID:         "task-unknown-01",
					Type:       "fortigate.configure",
					Parameters: json.RawMessage(`{"site":"paris"}`),
				},
			},
		},
	)
	if err == nil || !errors.Is(err, application.ErrExecutionUnsupported) {
		t.Fatalf("expected ErrExecutionUnsupported for unregistered task type, got %v", err)
	}
}
