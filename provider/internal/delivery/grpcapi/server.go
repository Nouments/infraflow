package grpcapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/protocol"
	infrav1 "infraflow/pkg/protocol/infraflow/v1"
	"infraflow/provider/internal/application"
)

const (
	DefaultChunkSize = 64 << 10
	MaxChunkSize     = 1 << 20
)

type agentProviderServer struct {
	infrav1.UnimplementedAgentProviderServer
	service   *application.Service
	token     []byte
	chunkSize int
}

func NewServer(service *application.Service, token string, chunkSize int, transportCredentials credentials.TransportCredentials) (*grpc.Server, error) {
	if service == nil {
		return nil, fmt.Errorf("provider application service is required")
	}
	if len([]byte(token)) < security.MinAgentTokenBytes {
		return nil, fmt.Errorf("agent token must be at least %d bytes", security.MinAgentTokenBytes)
	}
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if chunkSize > MaxChunkSize {
		return nil, fmt.Errorf("gRPC chunk size must not exceed %d bytes", MaxChunkSize)
	}
	options := make([]grpc.ServerOption, 0, 1)
	if transportCredentials != nil {
		options = append(options, grpc.Creds(transportCredentials))
	}
	server := grpc.NewServer(options...)
	infrav1.RegisterAgentProviderServer(server, &agentProviderServer{
		service: service, token: []byte(token), chunkSize: chunkSize,
	})
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	return server, nil
}

func (service *agentProviderServer) ListArtifacts(ctx context.Context, _ *infrav1.Empty) (*infrav1.ArtifactCatalog, error) {
	if err := service.authorize(ctx); err != nil {
		return nil, err
	}
	artifacts, err := service.service.Catalog()
	if err != nil {
		return nil, status.Error(codes.Internal, "artifact catalog unavailable")
	}
	response := &infrav1.ArtifactCatalog{Artifacts: make([]*infrav1.ArtifactRecord, 0, len(artifacts))}
	for _, artifact := range artifacts {
		response.Artifacts = append(response.Artifacts, &infrav1.ArtifactRecord{
			Type: artifact.Type, Path: artifact.Path, InputHash: artifact.InputHash, OutputHash: artifact.OutputHash,
		})
	}
	return response, nil
}

func (service *agentProviderServer) DownloadArtifact(request *infrav1.DownloadRequest, stream grpc.ServerStreamingServer[infrav1.ArtifactChunk]) error {
	if err := service.authorize(stream.Context()); err != nil {
		return err
	}
	if request.GetPath() == "" {
		return status.Error(codes.InvalidArgument, "artifact path is required")
	}
	reader, artifact, err := service.service.OpenArtifact(request.GetPath())
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return status.Error(codes.NotFound, "artifact not found")
	}
	if err != nil {
		return status.Error(codes.NotFound, "artifact is unavailable")
	}
	defer reader.Close()
	buffer := make([]byte, service.chunkSize)
	digest := sha256.New()
	var offset uint64
	for {
		count, readErr := reader.Read(buffer)
		if count > 0 {
			_, _ = digest.Write(buffer[:count])
			chunk := &infrav1.ArtifactChunk{
				Path: artifact.Path, OutputHash: artifact.OutputHash,
				Offset: offset, Data: append([]byte(nil), buffer[:count]...),
			}
			if err := stream.Send(chunk); err != nil {
				return status.Error(codes.Unavailable, "artifact stream interrupted")
			}
			offset += uint64(count)
		}
		if errors.Is(readErr, io.EOF) {
			if hex.EncodeToString(digest.Sum(nil)) != artifact.OutputHash {
				return status.Error(codes.DataLoss, "artifact hash does not match manifest")
			}
			return stream.Send(&infrav1.ArtifactChunk{
				Path: artifact.Path, OutputHash: artifact.OutputHash, Offset: offset, Eof: true,
			})
		}
		if readErr != nil {
			return status.Error(codes.Internal, "artifact read failed")
		}
	}
}

func (service *agentProviderServer) ReportState(ctx context.Context, input *infrav1.AgentStateReport) (*infrav1.ReportAck, error) {
	if err := service.authorize(ctx); err != nil {
		return nil, err
	}
	if input.GetReportedAt() == nil || !input.GetReportedAt().IsValid() {
		return nil, status.Error(codes.InvalidArgument, "report timestamp is invalid")
	}
	report := protocol.AgentReport{
		ReportID: input.GetReportId(), AgentID: input.GetAgentId(), ReportedAt: input.GetReportedAt().AsTime(),
		Artifacts: make([]protocol.ArtifactResult, 0, len(input.GetArtifacts())),
	}
	for _, artifact := range input.GetArtifacts() {
		report.Artifacts = append(report.Artifacts, protocol.ArtifactResult{
			Path: artifact.GetPath(), OutputHash: artifact.GetOutputHash(), Status: artifact.GetStatus(), Message: artifact.GetMessage(),
		})
	}
	added, err := service.service.SubmitReport(report)
	if err != nil {
		if errors.Is(err, application.ErrInvalidAgentReport) {
			return nil, status.Error(codes.FailedPrecondition, "agent report does not match published state")
		}
		return nil, status.Error(codes.Internal, "agent report could not be saved")
	}
	return &infrav1.ReportAck{Accepted: true, Duplicate: !added}, nil
}

func (service *agentProviderServer) authorize(ctx context.Context) error {
	metadataValues, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Error(codes.Unauthenticated, "bearer token is required")
	}
	authorization := metadataValues.Get("authorization")
	if len(authorization) != 1 || !security.BearerTokenMatches(authorization[0], service.token) {
		return status.Error(codes.Unauthenticated, "invalid bearer token")
	}
	return nil
}
