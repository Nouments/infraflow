package credentials

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// DeviceCredentials represents the credentials required to authenticate to a network device.
// Passwords and private keys must never be logged or serialized into public records.
type DeviceCredentials struct {
	Username   string
	Password   string
	PrivateKey []byte
	Passphrase string
}

// DeviceCredentialResolver defines the injectable contract for securely resolving device credentials
// at execution time without exposing them in task parameters or infrastructure files.
type DeviceCredentialResolver interface {
	ResolveDeviceCredentials(ctx context.Context, site, deviceID string) (DeviceCredentials, error)
}

// StaticDeviceCredentialResolver provides credentials from an in-memory map.
// Intended for testing and controlled programmatic execution.
type StaticDeviceCredentialResolver struct {
	entries map[string]DeviceCredentials
}

// NewStaticDeviceCredentialResolver creates a static credential resolver.
func NewStaticDeviceCredentialResolver(entries map[string]DeviceCredentials) *StaticDeviceCredentialResolver {
	copied := make(map[string]DeviceCredentials, len(entries))
	for k, v := range entries {
		copied[k] = v
	}
	return &StaticDeviceCredentialResolver{entries: copied}
}

// ResolveDeviceCredentials returns credentials matching "<site>/<deviceID>".
func (r *StaticDeviceCredentialResolver) ResolveDeviceCredentials(ctx context.Context, site, deviceID string) (DeviceCredentials, error) {
	if ctx != nil && ctx.Err() != nil {
		return DeviceCredentials{}, ctx.Err()
	}
	key := site + "/" + deviceID
	creds, exists := r.entries[key]
	if !exists {
		// Fallback to deviceID only if site is empty or not matched
		creds, exists = r.entries[deviceID]
	}
	if !exists {
		return DeviceCredentials{}, fmt.Errorf("device credentials not configured for target %s/%s", site, deviceID)
	}
	if creds.Username == "" {
		return DeviceCredentials{}, fmt.Errorf("device credentials for target %s/%s missing username", site, deviceID)
	}
	if creds.Password == "" && len(creds.PrivateKey) == 0 {
		return DeviceCredentials{}, fmt.Errorf("device credentials for target %s/%s missing password or private key", site, deviceID)
	}
	return creds, nil
}

// EnvDeviceCredentialResolver resolves credentials from environment variables using a standardized naming convention:
// INFRAFLOW_DEVICE_<SITE>_<DEVICE>_SSH_USER and INFRAFLOW_DEVICE_<SITE>_<DEVICE>_SSH_PASSWORD / INFRAFLOW_DEVICE_<SITE>_<DEVICE>_SSH_KEY_FILE
// with fallback to global INFRAFLOW_CISCO_SSH_USER and INFRAFLOW_CISCO_SSH_PASSWORD.
type EnvDeviceCredentialResolver struct{}

// NewEnvDeviceCredentialResolver returns an environment-based credential resolver.
func NewEnvDeviceCredentialResolver() *EnvDeviceCredentialResolver {
	return &EnvDeviceCredentialResolver{}
}

// ResolveDeviceCredentials looks up device credentials from environment variables.
func (r *EnvDeviceCredentialResolver) ResolveDeviceCredentials(ctx context.Context, site, deviceID string) (DeviceCredentials, error) {
	if ctx != nil && ctx.Err() != nil {
		return DeviceCredentials{}, ctx.Err()
	}
	cleanSite := sanitizeEnvSegment(site)
	cleanDevice := sanitizeEnvSegment(deviceID)

	prefix := fmt.Sprintf("INFRAFLOW_DEVICE_%s_%s_", cleanSite, cleanDevice)
	user := strings.TrimSpace(os.Getenv(prefix + "SSH_USER"))
	pass := os.Getenv(prefix + "SSH_PASSWORD")
	keyFile := strings.TrimSpace(os.Getenv(prefix + "SSH_KEY_FILE"))
	passphrase := os.Getenv(prefix + "SSH_KEY_PASSPHRASE")

	if user == "" {
		user = strings.TrimSpace(os.Getenv("INFRAFLOW_CISCO_SSH_USER"))
	}
	if pass == "" && keyFile == "" {
		pass = os.Getenv("INFRAFLOW_CISCO_SSH_PASSWORD")
		keyFile = strings.TrimSpace(os.Getenv("INFRAFLOW_CISCO_SSH_KEY_FILE"))
		passphrase = os.Getenv("INFRAFLOW_CISCO_SSH_KEY_PASSPHRASE")
	}

	if user == "" {
		return DeviceCredentials{}, fmt.Errorf("device credentials not configured for target %s/%s", site, deviceID)
	}

	var keyBytes []byte
	if keyFile != "" {
		data, err := os.ReadFile(keyFile)
		if err != nil {
			return DeviceCredentials{}, fmt.Errorf("read private key file for target %s/%s: %w", site, deviceID, err)
		}
		keyBytes = data
	}

	if pass == "" && len(keyBytes) == 0 {
		return DeviceCredentials{}, fmt.Errorf("device credentials for target %s/%s missing password or private key", site, deviceID)
	}

	return DeviceCredentials{
		Username:   user,
		Password:   pass,
		PrivateKey: keyBytes,
		Passphrase: passphrase,
	}, nil
}

func sanitizeEnvSegment(val string) string {
	s := strings.ToUpper(strings.TrimSpace(val))
	var b strings.Builder
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}
