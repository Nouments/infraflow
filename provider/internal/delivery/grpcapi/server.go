package grpcapi

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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"infraflow/internal/infrastructure/security"
	"infraflow/pkg/observability"
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
	logger    *observability.Logger
}

func NewServer(service *application.Service, token string, chunkSize int, transportCredentials credentials.TransportCredentials, loggers ...*observability.Logger) (*grpc.Server, error) {
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
	var logger *observability.Logger
	if len(loggers) > 0 {
		logger = loggers[0]
	}
	options = append(options,
		grpc.ChainUnaryInterceptor(unaryLogInterceptor(logger)),
		grpc.ChainStreamInterceptor(streamLogInterceptor(logger)),
	)
	server := grpc.NewServer(options...)
	infrav1.RegisterAgentProviderServer(server, &agentProviderServer{
		service: service, token: []byte(token), chunkSize: chunkSize, logger: logger,
	})
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	return server, nil
}

func unaryLogInterceptor(logger *observability.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if logger == nil {
			return handler(ctx, request)
		}
		correlationID := observability.CorrelationID(ctx)
		if correlationID == "" {
			correlationID, _ = observability.NewEventID()
		}
		started := time.Now()
		logGRPC(logger, ctx, info.FullMethod, "started", correlationID, started, nil)
		response, err := handler(ctx, request)
		logErr := logGRPC(logger, ctx, info.FullMethod, status.Code(err).String(), correlationID, started, err)
		if logErr != nil {
			fmt.Fprintf(os.Stderr, "infraflow-provider: persist gRPC log event: %v\n", logErr)
		}
		return response, err
	}
}

func streamLogInterceptor(logger *observability.Logger) grpc.StreamServerInterceptor {
	return func(server any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if logger == nil {
			return handler(server, stream)
		}
		correlationID := observability.CorrelationID(stream.Context())
		if correlationID == "" {
			correlationID, _ = observability.NewEventID()
		}
		started := time.Now()
		err := logGRPC(logger, stream.Context(), info.FullMethod, "started", correlationID, started, nil)
		serveErr := handler(server, stream)
		logErr := logGRPC(logger, stream.Context(), info.FullMethod, status.Code(serveErr).String(), correlationID, started, serveErr)
		if err != nil || logErr != nil {
			fmt.Fprintf(os.Stderr, "infraflow-provider: persist gRPC stream log event: %v\n", errors.Join(err, logErr))
		}
		return serveErr
	}
}

func logGRPC(logger *observability.Logger, ctx context.Context, method, result, correlationID string, started time.Time, operationErr error) error {
	level := "INFO"
	message := "gRPC operation completed"
	if result == "started" {
		message = "gRPC operation started"
	} else if result != codes.OK.String() {
		level = "WARN"
		message = "gRPC operation ended with non-OK status"
	}
	if operationErr != nil && status.Code(operationErr) == codes.Internal {
		level = "ERROR"
	}
	duration := time.Since(started).Milliseconds()
	return logger.Emit(ctx, observability.Event{
		Level: level, Event: "provider.grpc.operation", Message: message,
		Service: "provider", Source: "grpcapi", Operation: method,
		Status: result, DurationMS: &duration,
		RunID: correlationID, CorrelationID: correlationID,
	})
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

func (service *agentProviderServer) ReportLogs(ctx context.Context, input *infrav1.AgentLogBatch) (*infrav1.AgentLogAck, error) {
	if err := service.authorize(ctx); err != nil {
		return nil, err
	}
	if len(input.GetEventsJson()) == 0 || len(input.GetEventsJson()) > observability.MaxIngestBatch {
		return nil, status.Error(codes.InvalidArgument, "log batch size is invalid")
	}
	events := make([]observability.Event, 0, len(input.GetEventsJson()))
	for _, encoded := range input.GetEventsJson() {
		if len(encoded) > observability.MaxEventBytes {
			return nil, status.Error(codes.InvalidArgument, "log event exceeds its size limit")
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		var event observability.Event
		if err := decoder.Decode(&event); err != nil {
			return nil, status.Error(codes.InvalidArgument, "log event is invalid")
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, status.Error(codes.InvalidArgument, "log event contains trailing JSON")
		}
		events = append(events, event)
	}
	result, err := service.service.IngestAgentLogs(input.GetAgentId(), events)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "agent log batch was rejected")
	}
	return &infrav1.AgentLogAck{AcceptedCount: uint32(result.Accepted), DuplicateCount: uint32(result.Duplicate)}, nil
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
