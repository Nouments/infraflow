package executors

import (
	"fmt"
	"slices"
	"strings"

	"infraflow/pkg/protocol"

	"golang.org/x/crypto/ssh"
)

// Legacy SHA-1 algorithms that some network operating systems still require
// (for example Cisco IOSv 15.9, which only offers diffie-hellman-group14-sha1
// and ssh-rsa host-key signatures). They are never enabled globally.
var legacySHA1Algorithms = ssh.Algorithms{
	KeyExchanges: []string{ssh.InsecureKeyExchangeDH14SHA1},
	MACs:         []string{ssh.HMACSHA1},
	HostKeys:     []string{ssh.KeyAlgoRSA},
}

// SSHAlgorithmPolicy decides which SSH algorithms InfraFlow offers to a device.
// By default only algorithms that x/crypto/ssh classifies as supported (not
// insecure) and that do not rely on SHA-1 are offered. Individual devices can
// be granted the legacy SHA-1 set through an explicit operator exception in
// the Provider configuration.
type SSHAlgorithmPolicy struct {
	legacy map[string]struct{}
}

// NewSSHAlgorithmPolicy validates the "site/device" exception list.
func NewSSHAlgorithmPolicy(legacySHA1Targets []string) (*SSHAlgorithmPolicy, error) {
	policy := &SSHAlgorithmPolicy{legacy: make(map[string]struct{}, len(legacySHA1Targets))}
	for _, raw := range legacySHA1Targets {
		site, device, ok := strings.Cut(strings.TrimSpace(raw), "/")
		if !ok || !protocol.ValidSiteName(site) || !protocol.ValidSiteName(device) {
			return nil, fmt.Errorf("invalid SSH legacy algorithm target %q: expected <site>/<device>", raw)
		}
		policy.legacy[policyKey(site, device)] = struct{}{}
	}
	return policy, nil
}

func policyKey(site, device string) string {
	return strings.ToLower(strings.TrimSpace(site) + "/" + strings.TrimSpace(device))
}

// AllowsLegacySHA1 reports whether the operator granted the SHA-1 exception.
func (p *SSHAlgorithmPolicy) AllowsLegacySHA1(target AuthorizedDevice) bool {
	if p == nil {
		return false
	}
	_, ok := p.legacy[policyKey(target.Site, target.DeviceID)]
	return ok
}

// Apply sets the algorithm lists on an SSH client configuration.
func (p *SSHAlgorithmPolicy) Apply(target AuthorizedDevice, config *ssh.ClientConfig) {
	supported := ssh.SupportedAlgorithms()
	kex := withoutSHA1(supported.KeyExchanges)
	macs := withoutSHA1(supported.MACs)
	hostKeys := withoutSHA1(supported.HostKeys)
	if p.AllowsLegacySHA1(target) {
		kex = append(kex, legacySHA1Algorithms.KeyExchanges...)
		macs = append(macs, legacySHA1Algorithms.MACs...)
		hostKeys = append(hostKeys, legacySHA1Algorithms.HostKeys...)
	}
	config.KeyExchanges = kex
	config.MACs = macs
	config.HostKeyAlgorithms = hostKeys
	config.Ciphers = supported.Ciphers
}

func withoutSHA1(algorithms []string) []string {
	return slices.DeleteFunc(slices.Clone(algorithms), func(name string) bool {
		return strings.Contains(name, "sha1") || name == ssh.KeyAlgoRSA || name == ssh.CertAlgoRSAv01
	})
}

// explainAlgorithmMismatch turns an x/crypto negotiation failure into an
// actionable message without suggesting the exception for devices that
// already have it.
func (p *SSHAlgorithmPolicy) explainAlgorithmMismatch(target AuthorizedDevice, err error) error {
	if err == nil || !strings.Contains(err.Error(), "no common algorithm") {
		return err
	}
	if p.AllowsLegacySHA1(target) {
		return fmt.Errorf("%w (device offers no algorithm InfraFlow implements, even with the legacy SHA-1 exception)", err)
	}
	return fmt.Errorf("%w; the device may only offer legacy SHA-1 algorithms, which are disabled by default. "+
		"If this is accepted risk, add %q to ssh_legacy_sha1_targets in the Provider configuration", err, target.Site+"/"+target.DeviceID)
}
