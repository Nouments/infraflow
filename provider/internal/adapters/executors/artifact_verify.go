package executors

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	coregeneration "infraflow/internal/adapters/generation"
	"infraflow/internal/domain"
	"infraflow/internal/ports"
	"infraflow/pkg/protocol"
)

const (
	// ArtifactVerifyType is the registered task type for verifying published provider artifacts.
	ArtifactVerifyType = "artifact.verify"

	// maxArtifactReadBytes enforces a strict upper bound on artifact size when streaming from disk.
	maxArtifactReadBytes = protocol.MaxArtifactBytes

	// maxArtifactPathLength limits the length of an artifact path.
	maxArtifactPathLength = 256
)

// ArtifactVerifyParameters defines the strict JSON parameter contract for artifact.verify tasks.
type ArtifactVerifyParameters struct {
	Path string `json:"path"`
}

// ArtifactVerificationResult represents the structured, verified evidence returned on success.
type ArtifactVerificationResult struct {
	Path       string `json:"path"`
	Type       string `json:"type"`
	InputHash  string `json:"input_hash"`
	OutputHash string `json:"output_hash"`
	ActualHash string `json:"actual_hash"`
	SizeBytes  int64  `json:"size_bytes"`
	Verified   bool   `json:"verified"`
	VerifiedAt string `json:"verified_at"`
}

// ArtifactVerifyExecutor implements ports.ExecutionExecutor to verify the integrity of
// provider-generated artifacts against catalog metadata without mutation or network deployment.
type ArtifactVerifyExecutor struct {
	repository ports.ArtifactRepository
}

// NewArtifactVerifyExecutor constructs a verified artifact executor.
func NewArtifactVerifyExecutor(repository ports.ArtifactRepository) (*ArtifactVerifyExecutor, error) {
	if repository == nil {
		return nil, errors.New("artifact repository is required")
	}
	return &ArtifactVerifyExecutor{repository: repository}, nil
}

// Type returns the task type identifier "artifact.verify".
func (executor *ArtifactVerifyExecutor) Type() string {
	return ArtifactVerifyType
}

// ValidateParameters validates that task parameters adhere strictly to the schema, rejecting unknown fields,
// empty paths, directory traversals, and non-canonical structures.
func (executor *ArtifactVerifyExecutor) ValidateParameters(parameters json.RawMessage) (json.RawMessage, error) {
	if len(parameters) == 0 {
		return nil, errors.New("task parameters must not be empty")
	}
	decoder := json.NewDecoder(bytes.NewReader(parameters))
	decoder.DisallowUnknownFields()
	var params ArtifactVerifyParameters
	if err := decoder.Decode(&params); err != nil {
		return nil, fmt.Errorf("invalid parameters: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, errors.New("task parameters contain trailing JSON data")
	}
	cleanedPath, err := validateArtifactPath(params.Path)
	if err != nil {
		return nil, err
	}
	params.Path = cleanedPath
	canonical, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("canonicalize parameters: %w", err)
	}
	return canonical, nil
}

// Execute performs read-only resolution, streaming read, and cryptographic integrity check of the artifact.
func (executor *ArtifactVerifyExecutor) Execute(ctx context.Context, task domain.ExecutionTask) (ports.ExecutionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ports.ExecutionResult{Status: domain.ExecutionTaskCancelled}, ctx.Err()
	}

	var params ArtifactVerifyParameters
	if err := json.Unmarshal(task.Parameters, &params); err != nil {
		msg := "invalid task parameters format"
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, errors.New(msg)
	}

	cleanedPath, err := validateArtifactPath(params.Path)
	if err != nil {
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: err.Error()}, err
	}

	reader, published, err := executor.repository.Open(cleanedPath)
	if err != nil {
		if errors.Is(err, coregeneration.ErrArtifactTooLarge) {
			msg := fmt.Sprintf("artifact %q exceeds maximum verification size of %d bytes", cleanedPath, maxArtifactReadBytes)
			return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, errors.New(msg)
		}
		if errors.Is(err, coregeneration.ErrArtifactHashMismatch) {
			msg := fmt.Sprintf("artifact %q integrity check failed: %v", cleanedPath, err)
			return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, errors.New(msg)
		}
		msg := fmt.Sprintf("artifact %q not found or inaccessible: %v", cleanedPath, err)
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, fmt.Errorf("artifact %q not found or inaccessible: %w", cleanedPath, err)
	}
	defer reader.Close()

	if published.Path != cleanedPath {
		msg := fmt.Sprintf("catalog entry path %q does not match requested path %q", published.Path, cleanedPath)
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, errors.New(msg)
	}
	parts := strings.Split(published.Path, "/")
	if len(parts) < 2 || !protocol.ValidArtifactPath(parts[0], published) {
		msg := fmt.Sprintf("artifact %q is not an authorized artifact specification", published.Path)
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, errors.New(msg)
	}
	if !protocol.IsSHA256(published.OutputHash) {
		msg := fmt.Sprintf("catalog artifact %q has invalid output hash metadata", published.Path)
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, errors.New(msg)
	}

	hasher := sha256.New()
	buf := make([]byte, 32*1024)
	var totalBytes int64

	for {
		if ctx.Err() != nil {
			return ports.ExecutionResult{Status: domain.ExecutionTaskCancelled}, ctx.Err()
		}
		n, readErr := reader.Read(buf)
		if n > 0 {
			totalBytes += int64(n)
			if totalBytes > maxArtifactReadBytes {
				msg := fmt.Sprintf("artifact %q exceeds maximum permitted size of %d bytes", cleanedPath, maxArtifactReadBytes)
				return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, errors.New(msg)
			}
			hasher.Write(buf[:n])
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			msg := fmt.Sprintf("read artifact %q: %v", cleanedPath, readErr)
			return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, fmt.Errorf("read artifact %q: %w", cleanedPath, readErr)
		}
	}

	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if actualHash != published.OutputHash {
		msg := fmt.Sprintf("artifact %q integrity check failed: expected hash %s, computed hash %s", cleanedPath, published.OutputHash, actualHash)
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, errors.New(msg)
	}

	outputPayload, err := json.Marshal(ArtifactVerificationResult{
		Path:       published.Path,
		Type:       published.Type,
		InputHash:  published.InputHash,
		OutputHash: published.OutputHash,
		ActualHash: actualHash,
		SizeBytes:  totalBytes,
		Verified:   true,
		VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		msg := "failed to marshal verification output"
		return ports.ExecutionResult{Status: domain.ExecutionTaskFailed, Error: msg}, errors.New(msg)
	}

	return ports.ExecutionResult{
		Status: domain.ExecutionTaskSucceeded,
		Output: outputPayload,
	}, nil
}

// Retryable permits retries only for transient I/O failures; integrity mismatches remain permanent.
func (executor *ArtifactVerifyExecutor) Retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var temporary interface{ Temporary() bool }
	return errors.As(err, &temporary) && temporary.Temporary()
}

func validateArtifactPath(raw string) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return "", errors.New("artifact path is required")
	}
	if len(clean) > maxArtifactPathLength {
		return "", fmt.Errorf("artifact path exceeds maximum length of %d", maxArtifactPathLength)
	}
	if strings.ContainsAny(clean, "\x00\r\n\t") {
		return "", errors.New("artifact path contains invalid control characters")
	}
	if strings.Contains(clean, "\\") {
		return "", errors.New("artifact path must use forward slashes")
	}
	if path.IsAbs(clean) {
		return "", errors.New("artifact path must be relative")
	}
	if path.Clean(clean) != clean {
		return "", errors.New("artifact path is not normalized")
	}
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || clean == ".." || clean == "." {
		return "", errors.New("directory traversal is not allowed")
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 2 {
		return "", errors.New("artifact path must specify a site and an artifact file")
	}
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("artifact path contains empty or relative segments")
		}
		if index == 0 {
			if !protocol.ValidSiteName(part) {
				return "", fmt.Errorf("invalid site name %q in artifact path", part)
			}
		} else {
			if !validPathSegment(part) {
				return "", fmt.Errorf("invalid path segment %q in artifact path", part)
			}
		}
	}
	return clean, nil
}

func validPathSegment(segment string) bool {
	if len(segment) == 0 || len(segment) > 128 {
		return false
	}
	for _, character := range segment {
		valid := (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-'
		if !valid {
			return false
		}
	}
	return true
}
