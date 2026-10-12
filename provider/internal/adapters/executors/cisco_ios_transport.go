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
	"time"

	"infraflow/provider/internal/infrastructure/credentials"

	"golang.org/x/crypto/ssh"
)

var (
	ErrPromptTimeout   = errors.New("timeout waiting for Cisco IOS prompt")
	ErrSessionClosed   = errors.New("Cisco IOS session closed")
	ErrCommandRejected = errors.New("command rejected by Cisco IOS")
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

	sshConfig, err := t.buildSSHConfig(addr, creds)
	if err != nil {
		_ = tcpConn.Close()
		return nil, err
	}

	// Wrap connection for cancellation support
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

	sess := &liveCiscoIOSSession{
		client:         client,
		session:        session,
		stdin:          stdinPipe,
		stdout:         stdoutPipe,
		commandTimeout: t.commandTimeout,
	}

	// Watch for context cancellation to cleanly abort the network session
	go func() {
		<-ctx.Done()
		_ = sess.Close()
	}()

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
	client         *ssh.Client
	session        *ssh.Session
	stdin          io.WriteCloser
	stdout         io.Reader
	commandTimeout time.Duration

	mu     sync.Mutex
	closed bool
}

func (s *liveCiscoIOSSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var errs []error
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.session != nil {
		if err := s.session.Close(); err != nil && !errors.Is(err, io.EOF) {
			errs = append(errs, err)
		}
	}
	if s.client != nil {
		if err := s.client.Close(); err != nil && !errors.Is(err, io.EOF) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (s *liveCiscoIOSSession) ReadInitialPrompt(ctx context.Context) (string, error) {
	return s.readUntilPrompt(ctx)
}

func (s *liveCiscoIOSSession) RunCommand(ctx context.Context, command string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", ErrSessionClosed
	}

	cleanCmd := strings.TrimSpace(command)
	if _, err := io.WriteString(s.stdin, cleanCmd+"\n"); err != nil {
		return "", fmt.Errorf("write command %q: %w", cleanCmd, err)
	}

	output, err := s.readUntilPrompt(ctx)
	if err != nil {
		return output, err
	}

	// Detect Cisco error messages
	if match := ciscoErrorPattern.FindStringSubmatch(output); len(match) > 1 {
		return output, fmt.Errorf("%w on command %q: %s", ErrCommandRejected, cleanCmd, match[1])
	}
	return output, nil
}

func (s *liveCiscoIOSSession) readUntilPrompt(ctx context.Context) (string, error) {
	timeout := s.commandTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining < timeout {
			timeout = remaining
		}
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	type chunkResult struct {
		data []byte
		err  error
	}

	buf := make([]byte, 1024)
	var accumulated bytes.Buffer

	for {
		readDone := make(chan chunkResult, 1)
		go func() {
			n, err := s.stdout.Read(buf)
			if n > 0 {
				readDone <- chunkResult{data: append([]byte(nil), buf[:n]...), err: err}
			} else {
				readDone <- chunkResult{err: err}
			}
		}()

		select {
		case <-ctx.Done():
			return accumulated.String(), ctx.Err()
		case <-timer.C:
			return accumulated.String(), ErrPromptTimeout
		case res := <-readDone:
			if len(res.data) > 0 {
				accumulated.Write(res.data)
				// Check if the accumulated buffer ends with a Cisco prompt
				text := cleanAnsiAndControl(accumulated.String())
				lines := strings.Split(text, "\n")
				for i := len(lines) - 1; i >= 0; i-- {
					line := strings.TrimRight(lines[i], " \r\t")
					if ciscoPromptPattern.MatchString(line) {
						return text, nil
					}
				}
			}
			if res.err != nil {
				if errors.Is(res.err, io.EOF) {
					return accumulated.String(), io.EOF
				}
				return accumulated.String(), res.err
			}
		}
	}
}

// cleanAnsiAndControl removes ANSI escape codes and normalizes carriage returns.
func cleanAnsiAndControl(input string) string {
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	cleaned := ansiRegex.ReplaceAllString(input, "")
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\n")
	cleaned = strings.ReplaceAll(cleaned, "\r", "\n")
	return cleaned
}
