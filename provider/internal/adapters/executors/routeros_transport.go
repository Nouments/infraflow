package executors

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path"
	"regexp"
	"strconv"
	"time"

	"infraflow/provider/internal/infrastructure/credentials"

	"golang.org/x/crypto/ssh"
)

const maxRouterOSOutputBytes = 1 << 20

// routerOSFileName limits uploaded names to a flat, predictable file name.
var routerOSFileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`)

// RouterOSClient is an authenticated management session with a RouterOS device.
type RouterOSClient interface {
	io.Closer
	// Upload stores content as a file in the device's root file system.
	Upload(ctx context.Context, name string, content []byte) error
	// Run executes one RouterOS console command and returns its output.
	Run(ctx context.Context, command string) (string, error)
}

// RouterOSTransport connects to authorized RouterOS devices.
type RouterOSTransport interface {
	Connect(ctx context.Context, target AuthorizedDevice, creds credentials.DeviceCredentials) (RouterOSClient, error)
}

// SSHRouterOSTransport talks to RouterOS over SSH: commands run in exec
// channels and files are uploaded with the SCP sink protocol, which RouterOS
// implements natively. Host keys and algorithms follow the same verifier and
// policy as the Cisco executor.
type SSHRouterOSTransport struct {
	verifier       HostKeyVerifier
	policy         *SSHAlgorithmPolicy
	connectTimeout time.Duration
	commandTimeout time.Duration
}

// NewSSHRouterOSTransport creates a RouterOS SSH transport.
func NewSSHRouterOSTransport(verifier HostKeyVerifier, policy *SSHAlgorithmPolicy, connectTimeout, commandTimeout time.Duration) (*SSHRouterOSTransport, error) {
	if verifier == nil {
		return nil, errors.New("host key verifier is required")
	}
	if policy == nil {
		policy = &SSHAlgorithmPolicy{}
	}
	if connectTimeout <= 0 {
		connectTimeout = 10 * time.Second
	}
	if commandTimeout <= 0 {
		commandTimeout = 30 * time.Second
	}
	return &SSHRouterOSTransport{verifier: verifier, policy: policy, connectTimeout: connectTimeout, commandTimeout: commandTimeout}, nil
}

// Connect dials and authenticates; it does not open a shell.
func (t *SSHRouterOSTransport) Connect(ctx context.Context, target AuthorizedDevice, creds credentials.DeviceCredentials) (RouterOSClient, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(target.ManagementIP, strconv.Itoa(target.SSHPort))
	var auth []ssh.AuthMethod
	if creds.Password != "" {
		auth = append(auth, ssh.Password(creds.Password))
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
		auth = append(auth, ssh.PublicKeys(signer))
	}
	if len(auth) == 0 {
		return nil, errors.New("no authentication methods available for device")
	}
	config := &ssh.ClientConfig{
		User:            creds.Username,
		Auth:            auth,
		HostKeyCallback: t.verifier.Verify,
		Timeout:         t.connectTimeout,
	}
	t.policy.Apply(target, config)

	dialer := net.Dialer{Timeout: t.connectTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", addr, err)
	}
	deadline := time.Now().Add(t.connectTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	stop()
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ssh handshake with %s: %w", addr, t.policy.explainAlgorithmMismatch(target, err))
	}
	_ = conn.SetDeadline(time.Time{})
	return &sshRouterOSClient{client: ssh.NewClient(sshConn, chans, reqs), commandTimeout: t.commandTimeout}, nil
}

type sshRouterOSClient struct {
	client         *ssh.Client
	commandTimeout time.Duration
}

func (c *sshRouterOSClient) Close() error { return c.client.Close() }

// withSession runs fn in a new SSH channel bounded by ctx and the command
// timeout. On expiry the channel is closed, which aborts blocked I/O.
func (c *sshRouterOSClient) withSession(ctx context.Context, fn func(*ssh.Session) error) error {
	ctx, cancel := context.WithTimeout(ctx, c.commandTimeout)
	defer cancel()
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("open ssh channel: %w", err)
	}
	defer session.Close()
	done := make(chan error, 1)
	go func() { done <- fn(session) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = session.Close()
		<-done
		return ctx.Err()
	}
}

func (c *sshRouterOSClient) Run(ctx context.Context, command string) (string, error) {
	var output bytes.Buffer
	err := c.withSession(ctx, func(session *ssh.Session) error {
		session.Stdout = &limitedWriter{w: &output, remaining: maxRouterOSOutputBytes}
		session.Stderr = session.Stdout
		return session.Run(command)
	})
	if err != nil {
		return output.String(), fmt.Errorf("run %q: %w", firstWord(command), err)
	}
	return output.String(), nil
}

// Upload implements the sink side of the SCP protocol ("scp -t").
func (c *sshRouterOSClient) Upload(ctx context.Context, name string, content []byte) error {
	if !routerOSFileName.MatchString(name) || path.Base(name) != name {
		return fmt.Errorf("invalid RouterOS file name %q", name)
	}
	return c.withSession(ctx, func(session *ssh.Session) error {
		stdin, err := session.StdinPipe()
		if err != nil {
			return err
		}
		stdout, err := session.StdoutPipe()
		if err != nil {
			return err
		}
		if err := session.Start("scp -t " + name); err != nil {
			return fmt.Errorf("start scp: %w", err)
		}
		acks := bufio.NewReader(stdout)
		if err := scpAck(acks); err != nil {
			return fmt.Errorf("scp not accepted: %w", err)
		}
		if _, err := fmt.Fprintf(stdin, "C0600 %d %s\n", len(content), name); err != nil {
			return fmt.Errorf("scp header: %w", err)
		}
		if err := scpAck(acks); err != nil {
			return fmt.Errorf("scp header rejected: %w", err)
		}
		if _, err := stdin.Write(content); err != nil {
			return fmt.Errorf("scp transfer interrupted: %w", err)
		}
		if _, err := stdin.Write([]byte{0}); err != nil {
			return fmt.Errorf("scp transfer interrupted: %w", err)
		}
		if err := scpAck(acks); err != nil {
			return fmt.Errorf("scp transfer not confirmed: %w", err)
		}
		_ = stdin.Close()
		if err := session.Wait(); err != nil {
			var exitMissing *ssh.ExitMissingError
			if !errors.As(err, &exitMissing) {
				return fmt.Errorf("scp finished with error: %w", err)
			}
		}
		return nil
	})
}

func scpAck(reader *bufio.Reader) error {
	code, err := reader.ReadByte()
	if err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	message, _ := reader.ReadString('\n')
	return fmt.Errorf("scp error %d: %s", code, bytes.TrimSpace([]byte(message)))
}

type limitedWriter struct {
	w         io.Writer
	remaining int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > l.remaining {
		return 0, ErrOutputTooLarge
	}
	l.remaining -= len(p)
	return l.w.Write(p)
}

func firstWord(command string) string {
	for i, r := range command {
		if r == ' ' {
			return command[:i] + " …"
		}
	}
	return command
}
