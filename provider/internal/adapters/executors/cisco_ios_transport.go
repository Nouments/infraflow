package executors

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"infraflow/provider/internal/infrastructure/credentials"

	"golang.org/x/crypto/ssh"
)

var (
	ErrPromptTimeout   = errors.New("timeout waiting for Cisco IOS prompt")
	ErrSessionClosed   = errors.New("Cisco IOS session closed")
	ErrCommandRejected = errors.New("command rejected by Cisco IOS")
	ErrOutputTooLarge  = errors.New("Cisco IOS command output exceeds the configured limit")
)

const (
	maxCiscoCommandOutputBytes = 1 << 20
	ciscoSSHReadChunkBytes     = 4 << 10
	ciscoPromptLineScanBytes   = 256
)

// ciscoPromptPattern matches privileged and configuration prompts:
// e.g. "Router#", "Router(config)#", "Router(config-if)#", "Router>"
var ciscoPromptPattern = regexp.MustCompile(`(?m)^[a-zA-Z0-9._-]+(?:\([a-zA-Z0-9._-]+\))?[>#]\s*$`)

// ciscoErrorPattern detects explicit Cisco IOS command execution errors.
var ciscoErrorPattern = regexp.MustCompile(`(?m)^% (.*)$`)

// CiscoIOSSession represents an active interactive session with a Cisco IOS router.
type CiscoIOSSession interface {
	io.Closer
	// ReadInitialPrompt reads the banner and waits for the initial prompt after connecting.
	ReadInitialPrompt(ctx context.Context) (string, error)
	// RunCommand writes a single command, waits for the prompt, and returns the command output.
	RunCommand(ctx context.Context, command string) (string, error)
}

// CiscoIOSTransport establishes SSH connections to authorized Cisco devices.
type CiscoIOSTransport interface {
	Connect(ctx context.Context, target AuthorizedDevice, creds credentials.DeviceCredentials) (CiscoIOSSession, error)
}

// SSHIOSTransport implements CiscoIOSTransport using golang.org/x/crypto/ssh.
type SSHIOSTransport struct {
	verifier       HostKeyVerifier
	connectTimeout time.Duration
	commandTimeout time.Duration
}

// NewSSHIOSTransport creates an SSH-backed Cisco IOS transport.
func NewSSHIOSTransport(verifier HostKeyVerifier, connectTimeout, commandTimeout time.Duration) (*SSHIOSTransport, error) {
	if verifier == nil {
		return nil, errors.New("host key verifier is required")
	}
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}
	if commandTimeout <= 0 {
		commandTimeout = 15 * time.Second
	}
	return &SSHIOSTransport{
		verifier:       verifier,
		connectTimeout: connectTimeout,
		commandTimeout: commandTimeout,
	}, nil
}

// Connect dials the device via TCP with context cancellation and establishes an SSH interactive shell.
func (t *SSHIOSTransport) Connect(ctx context.Context, target AuthorizedDevice, creds credentials.DeviceCredentials) (CiscoIOSSession, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	addr := fmt.Sprintf("%s:%d", target.ManagementIP, target.SSHPort)
	dialer := net.Dialer{Timeout: t.connectTimeout}
	tcpConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}
	connectDeadline := time.Now().Add(t.connectTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(connectDeadline) {
		connectDeadline = contextDeadline
	}
	if err := tcpConn.SetDeadline(connectDeadline); err != nil {
		_ = tcpConn.Close()
		return nil, fmt.Errorf("set SSH connection deadline: %w", err)
	}
	stopConnectWatch := context.AfterFunc(ctx, func() { _ = tcpConn.Close() })
	defer stopConnectWatch()

	sshConfig, err := t.buildSSHConfig(addr, creds)
	if err != nil {
		_ = tcpConn.Close()
		return nil, err
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, sshConfig)
	if err != nil {
		_ = tcpConn.Close()
		return nil, fmt.Errorf("ssh handshake with %s: %w", addr, err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)

	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("create ssh session: %w", err)
	}

	// Request VT100 PTY for interactive CLI
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 115200,
		ssh.TTY_OP_OSPEED: 115200,
	}
	if err := session.RequestPty("vt100", 80, 24, modes); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("request pty: %w", err)
	}

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	if err := session.Shell(); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("start shell: %w", err)
	}
	if err := tcpConn.SetDeadline(time.Time{}); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, fmt.Errorf("clear SSH connection deadline: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = session.Close()
		_ = client.Close()
		return nil, err
	}
	stopConnectWatch()

	sess := newLiveCiscoIOSSession(stdinPipe, stdoutPipe, t.commandTimeout, func() error {
		_ = stdinPipe.Close()
		_ = session.Close()
		return client.Close()
	})
	sess.watchContext(ctx)
	if err := ctx.Err(); err != nil {
		_ = sess.Close()
		return nil, err
	}

	return sess, nil
}

func (t *SSHIOSTransport) buildSSHConfig(addr string, creds credentials.DeviceCredentials) (*ssh.ClientConfig, error) {
	var authMethods []ssh.AuthMethod
	if creds.Password != "" {
		authMethods = append(authMethods, ssh.Password(creds.Password))
	}
	if len(creds.PrivateKey) > 0 {
		var signer ssh.Signer
		var err error
		if creds.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(creds.PrivateKey, []byte(creds.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(creds.PrivateKey)
		}
		if err != nil {
			return nil, fmt.Errorf("parse device private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if len(authMethods) == 0 {
		return nil, errors.New("no authentication methods available for device")
	}

	return &ssh.ClientConfig{
		User: creds.Username,
		Auth: authMethods,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return t.verifier.Verify(hostname, remote, key)
		},
		Timeout: t.connectTimeout,
	}, nil
}

type liveCiscoIOSSession struct {
	stdin            io.WriteCloser
	commandTimeout   time.Duration
	closeResources   func() error
	watchMu          sync.Mutex
	stopContextWatch func() bool

	commandMu  sync.Mutex
	closed     atomic.Bool
	closeOnce  sync.Once
	closeErr   error
	prompt     string
	stopRead   chan struct{}
	readerDone chan struct{}
	readEvents chan ciscoReadEvent
}

type ciscoReadEvent struct {
	data []byte
	err  error
}

func newLiveCiscoIOSSession(stdin io.WriteCloser, stdout io.Reader, commandTimeout time.Duration, closeResources func() error) *liveCiscoIOSSession {
	session := &liveCiscoIOSSession{
		stdin:          stdin,
		commandTimeout: commandTimeout,
		closeResources: closeResources,
		stopRead:       make(chan struct{}),
		readerDone:     make(chan struct{}),
		readEvents:     make(chan ciscoReadEvent, 1),
	}
	go session.readLoop(stdout)
	return session
}

func (s *liveCiscoIOSSession) watchContext(ctx context.Context) {
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	s.watchMu.Lock()
	if s.closed.Load() {
		s.watchMu.Unlock()
		stop()
		return
	}
	s.stopContextWatch = stop
	s.watchMu.Unlock()
}

func (s *liveCiscoIOSSession) readLoop(stdout io.Reader) {
	defer close(s.readerDone)
	buffer := make([]byte, ciscoSSHReadChunkBytes)
	for {
		n, err := stdout.Read(buffer)
		if n > 0 {
			data := append([]byte(nil), buffer[:n]...)
			select {
			case s.readEvents <- ciscoReadEvent{data: data, err: err}:
			case <-s.stopRead:
				return
			}
			if err != nil {
				return
			}
			continue
		}
		if err != nil {
			select {
			case s.readEvents <- ciscoReadEvent{err: err}:
			case <-s.stopRead:
			}
			return
		}
		if n == 0 {
			select {
			case s.readEvents <- ciscoReadEvent{err: io.ErrNoProgress}:
			case <-s.stopRead:
			}
			return
		}
	}
}

func (s *liveCiscoIOSSession) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		close(s.stopRead)
		s.watchMu.Lock()
		stop := s.stopContextWatch
		s.stopContextWatch = nil
		s.watchMu.Unlock()
		if stop != nil {
			stop()
		}
		if s.closeResources != nil {
			s.closeErr = s.closeResources()
		}
		<-s.readerDone
	})
	return s.closeErr
}

func (s *liveCiscoIOSSession) ReadInitialPrompt(ctx context.Context) (string, error) {
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	if s.closed.Load() {
		return "", ErrSessionClosed
	}
	output, err := s.readUntilPrompt(ctx, "")
	if err == nil {
		s.prompt, _ = finalCiscoPrompt(output, "")
	}
	return output, err
}

func (s *liveCiscoIOSSession) RunCommand(ctx context.Context, command string) (string, error) {
	s.commandMu.Lock()
	defer s.commandMu.Unlock()
	if s.closed.Load() {
		return "", ErrSessionClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return "", err
	}
	cleanCmd := strings.TrimSpace(command)
	if _, err := io.WriteString(s.stdin, cleanCmd+"\n"); err != nil {
		_ = s.Close()
		return "", fmt.Errorf("write command: %w", err)
	}
	output, err := s.readUntilPrompt(ctx, expectedCiscoPrompt(cleanCmd, s.prompt))
	if err != nil {
		return "", err
	}
	s.prompt, _ = finalCiscoPrompt(output, "")
	if match := ciscoErrorPattern.FindStringSubmatch(output); len(match) > 1 {
		return output, fmt.Errorf("%w on command %q: %s", ErrCommandRejected, cleanCmd, match[1])
	}
	return output, nil
}

func (s *liveCiscoIOSSession) readUntilPrompt(ctx context.Context, expectedPrompt string) (string, error) {
	if s.closed.Load() {
		return "", ErrSessionClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return "", err
	}
	timeout := s.commandTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		_ = s.Close()
		return "", ErrPromptTimeout
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var accumulated bytes.Buffer

	for {
		select {
		case <-ctx.Done():
			_ = s.Close()
			return "", ctx.Err()
		case <-timer.C:
			_ = s.Close()
			return "", ErrPromptTimeout
		case res := <-s.readEvents:
			if len(res.data) > 0 {
				if accumulated.Len()+len(res.data) > maxCiscoCommandOutputBytes {
					_ = s.Close()
					return "", ErrOutputTooLarge
				}
				_, _ = accumulated.Write(res.data)
			}
			if res.err != nil {
				_ = s.Close()
				return "", res.err
			}
			select {
			case <-ctx.Done():
				_ = s.Close()
				return "", ctx.Err()
			default:
			}
			select {
			case <-timer.C:
				_ = s.Close()
				return "", ErrPromptTimeout
			default:
			}
			text := accumulated.String()
			if _, ok := finalCiscoPrompt(text, expectedPrompt); ok {
				return cleanAnsiAndControl(text), nil
			}
		}
	}
}

func isFinalCiscoPrompt(text string) bool {
	_, ok := finalCiscoPrompt(text, "")
	return ok
}

func finalCiscoPrompt(text, expected string) (string, bool) {
	if len(text) > ciscoPromptLineScanBytes {
		text = text[len(text)-ciscoPromptLineScanBytes:]
	}
	trimmed := strings.TrimRight(text, " \t")
	if trimmed == "" {
		return "", false
	}
	if strings.HasSuffix(trimmed, "\r") || strings.HasSuffix(trimmed, "\n") {
		return "", false
	}
	line := trimmed
	if separator := strings.LastIndexAny(trimmed, "\r\n"); separator >= 0 {
		line = trimmed[separator+1:]
	}
	line = strings.TrimSpace(cleanAnsiAndControl(line))
	if !ciscoPromptPattern.MatchString(line) || expected != "" && line != expected {
		return "", false
	}
	return line, true
}

func expectedCiscoPrompt(command, current string) string {
	if current == "" {
		return ""
	}
	hostname := current
	if index := strings.IndexByte(hostname, '('); index >= 0 {
		hostname = hostname[:index]
	} else {
		hostname = strings.TrimRight(hostname, ">#")
	}
	switch {
	case command == "configure terminal":
		return hostname + "(config)#"
	case strings.HasPrefix(command, "interface "):
		return hostname + "(config-if)#"
	case command == "exit" && strings.HasSuffix(current, "(config-if)#"):
		return hostname + "(config)#"
	case command == "exit" && strings.HasSuffix(current, "(config)#"):
		return hostname + "#"
	case command == "end":
		return hostname + "#"
	case strings.HasPrefix(command, "hostname "):
		newHostname := strings.TrimSpace(strings.TrimPrefix(command, "hostname "))
		if strings.HasSuffix(current, "(config-if)#") {
			return newHostname + "(config-if)#"
		}
		if strings.HasSuffix(current, "(config)#") {
			return newHostname + "(config)#"
		}
		return newHostname + "#"
	default:
		return current
	}
}

// cleanAnsiAndControl removes ANSI escape codes and normalizes carriage returns.
func cleanAnsiAndControl(input string) string {
	cleaned := ciscoANSISequencePattern.ReplaceAllString(input, "")
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\n")
	cleaned = strings.ReplaceAll(cleaned, "\r", "\n")
	return cleaned
}

var ciscoANSISequencePattern = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
