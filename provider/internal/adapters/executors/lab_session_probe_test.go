//go:build lab

package executors

// Lab-only diagnostic (go test -tags lab): drives the real SSHIOSTransport
// session against a lab router and prints each command's duration, error and
// output. Never part of CI. Inputs come from environment variables only.
import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"infraflow/provider/internal/infrastructure/credentials"
)

func TestLabCiscoSessionProbe(t *testing.T) {
	verifier, err := NewKnownHostsFileVerifier(os.Getenv("LAB_KNOWN_HOSTS"))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := NewSSHAlgorithmPolicy([]string{os.Getenv("LAB_SITE") + "/" + os.Getenv("LAB_DEVICE")})
	if err != nil {
		t.Fatal(err)
	}
	transport, err := NewSSHIOSTransport(verifier, 10*time.Second, 15*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	transport.WithAlgorithmPolicy(policy)
	target := AuthorizedDevice{Site: os.Getenv("LAB_SITE"), DeviceID: os.Getenv("LAB_DEVICE"), Name: os.Getenv("LAB_DEVICE"), Vendor: "cisco", ManagementIP: os.Getenv("LAB_IP"), SSHPort: 22}
	ctx := context.Background()
	session, err := transport.Connect(ctx, target, credentials.DeviceCredentials{Username: os.Getenv("LAB_USER"), Password: os.Getenv("LAB_PASSWORD")})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	start := time.Now()
	out, err := session.ReadInitialPrompt(ctx)
	fmt.Printf("initial %v err=%v %q\n", time.Since(start).Round(time.Millisecond), err, out)
	for _, command := range strings.Split(os.Getenv("LAB_COMMANDS"), ";") {
		start := time.Now()
		out, err := session.RunCommand(ctx, command)
		fmt.Printf("%-45q %8v err=%v\n    %q\n", command, time.Since(start).Round(time.Millisecond), err, out)
	}
}
