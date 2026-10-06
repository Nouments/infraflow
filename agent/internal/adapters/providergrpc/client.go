package providergrpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/protocol"
	infrav1 "infraflow/pkg/protocol/infraflow/v1"
)

const (
	maxCatalogArtifacts = 10000
	maxChunkBytes       = 1 << 20
	maxArtifactBytes    = int64(8 << 30)
)

type Client struct {
	connection *grpc.ClientConn
	service    infrav1.AgentProviderClient
	token      string
}

func New(address, token string, tlsEnabled bool, caFile string) (*Client, error) {
	if len([]byte(token)) < security.MinAgentTokenBytes {
		return nil, fmt.Errorf("agent token must be at least %d bytes", security.MinAgentTokenBytes)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host == "" || port == "" {
		return nil, fmt.Errorf("provider address must use host:port format")
	}
	var transport credentials.TransportCredentials
	if tlsEnabled {
		configuration := &tls.Config{MinVersion: tls.VersionTLS12}
		if caFile != "" {
			certificate, err := os.ReadFile(caFile)
			if err != nil {
				return nil, fmt.Errorf("read provider CA: %w", err)
			}
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(certificate) {
				return nil, fmt.Errorf("provider CA file contains no valid certificates")
			}
			configuration.RootCAs = roots
		}
		transport = credentials.NewTLS(configuration)
	} else if !isLoopback(host) {
		return nil, fmt.Errorf("TLS is required for non-loopback provider addresses")
	} else {
		transport = insecure.NewCredentials()
	}
	connection, err := grpc.NewClient(
		address,
		grpc.WithTransportCredentials(transport),
		grpc.WithPerRPCCredentials(bearerCredentials{token: token, secure: tlsEnabled}),
	)
	if err != nil {
		return nil, fmt.Errorf("create provider gRPC client: %w", err)
	}
	return NewWithConnection(connection, token), nil
}

func NewWithConnection(connection *grpc.ClientConn, token string) *Client {
	return &Client{connection: connection, service: infrav1.NewAgentProviderClient(connection), token: token}
}

func (client *Client) Close() error {
	return client.connection.Close()
}

func (client *Client) Catalog(ctx context.Context) ([]protocol.Artifact, error) {
	response, err := client.service.ListArtifacts(ctx, &infrav1.Empty{})
	if err != nil {
		return nil, fmt.Errorf("list provider artifacts: %w", err)
	}
	if len(response.GetArtifacts()) > maxCatalogArtifacts {
		return nil, fmt.Errorf("provider catalog exceeds %d artifacts", maxCatalogArtifacts)
	}
	artifacts := make([]protocol.Artifact, 0, len(response.GetArtifacts()))
	for _, artifact := range response.GetArtifacts() {
		artifacts = append(artifacts, protocol.Artifact{
			Type: artifact.GetType(), Path: artifact.GetPath(),
			InputHash: artifact.GetInputHash(), OutputHash: artifact.GetOutputHash(),
		})
	}
	return artifacts, nil
}

func (client *Client) Download(ctx context.Context, artifact protocol.Artifact, destination io.Writer) error {
	stream, err := client.service.DownloadArtifact(ctx, &infrav1.DownloadRequest{Path: artifact.Path})
	if err != nil {
		return fmt.Errorf("start artifact stream: %w", err)
	}
	var expectedOffset uint64
	var totalBytes int64
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return fmt.Errorf("artifact stream ended without a final chunk")
			}
			return fmt.Errorf("receive artifact chunk: %w", err)
		}
		if chunk.GetPath() != artifact.Path || chunk.GetOutputHash() != artifact.OutputHash || chunk.GetOffset() != expectedOffset {
			return fmt.Errorf("artifact stream metadata or offset mismatch")
		}
		if chunk.GetEof() {
			if len(chunk.GetData()) != 0 {
				return fmt.Errorf("final artifact chunk must be empty")
			}
			return nil
		}
		data := chunk.GetData()
		if len(data) == 0 || len(data) > maxChunkBytes {
			return fmt.Errorf("artifact chunk has an invalid size")
		}
		totalBytes += int64(len(data))
		if totalBytes > maxArtifactBytes {
			return fmt.Errorf("artifact exceeds the %d-byte limit", maxArtifactBytes)
		}
		written, err := destination.Write(data)
		if err != nil {
			return fmt.Errorf("write artifact chunk: %w", err)
		}
		if written != len(data) {
			return io.ErrShortWrite
		}
		expectedOffset += uint64(written)
	}
}

func (client *Client) Report(ctx context.Context, report protocol.AgentReport) error {
	request := &infrav1.AgentStateReport{
		ReportId: report.ReportID, AgentId: report.AgentID, ReportedAt: timestamppb.New(report.ReportedAt),
		Artifacts: make([]*infrav1.ArtifactExecutionResult, 0, len(report.Artifacts)),
	}
	for _, artifact := range report.Artifacts {
		request.Artifacts = append(request.Artifacts, &infrav1.ArtifactExecutionResult{
			Path: artifact.Path, OutputHash: artifact.OutputHash, Status: artifact.Status, Message: artifact.Message,
		})
	}
	if _, err := client.service.ReportState(ctx, request); err != nil {
		return fmt.Errorf("submit provider state report: %w", err)
	}
	return nil
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type bearerCredentials struct {
	token  string
	secure bool
}

func (credentials bearerCredentials) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + credentials.token}, nil
}

func (credentials bearerCredentials) RequireTransportSecurity() bool {
	return credentials.secure
}
