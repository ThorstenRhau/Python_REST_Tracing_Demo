package otelbackend

import (
	"context"
	"errors"
	"net"

	colllogsv1 "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collmetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	colltracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type Receiver struct {
	store       *Store
	persistence *Persistence
}

type traceService struct {
	colltracev1.UnimplementedTraceServiceServer
	receiver *Receiver
}

type logsService struct {
	colllogsv1.UnimplementedLogsServiceServer
	receiver *Receiver
}

type metricsService struct {
	collmetricsv1.UnimplementedMetricsServiceServer
	receiver *Receiver
}

type Server struct {
	grpcServer *grpc.Server
	listener   net.Listener
}

func NewReceiver(store *Store, persistence *Persistence) *Receiver {
	return &Receiver{store: store, persistence: persistence}
}

func StartGRPC(listen string, receiver *Receiver) (*Server, error) {
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, err
	}
	grpcServer := grpc.NewServer()
	colltracev1.RegisterTraceServiceServer(grpcServer, &traceService{receiver: receiver})
	colllogsv1.RegisterLogsServiceServer(grpcServer, &logsService{receiver: receiver})
	collmetricsv1.RegisterMetricsServiceServer(grpcServer, &metricsService{receiver: receiver})

	server := &Server{grpcServer: grpcServer, listener: listener}
	go func() {
		if err := grpcServer.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			panic(err)
		}
	}()
	return server, nil
}

func (s *Server) Stop() {
	if s != nil {
		s.grpcServer.GracefulStop()
	}
}

func (s *traceService) Export(ctx context.Context, req *colltracev1.ExportTraceServiceRequest) (*colltracev1.ExportTraceServiceResponse, error) {
	s.receiver.store.IngestTraces(req.GetResourceSpans())
	if err := s.receiver.persist(ctx, "traces", req); err != nil {
		return nil, err
	}
	return &colltracev1.ExportTraceServiceResponse{}, nil
}

func (s *logsService) Export(ctx context.Context, req *colllogsv1.ExportLogsServiceRequest) (*colllogsv1.ExportLogsServiceResponse, error) {
	s.receiver.store.IngestLogs(req.GetResourceLogs())
	if err := s.receiver.persist(ctx, "logs", req); err != nil {
		return nil, err
	}
	return &colllogsv1.ExportLogsServiceResponse{}, nil
}

func (s *metricsService) Export(ctx context.Context, req *collmetricsv1.ExportMetricsServiceRequest) (*collmetricsv1.ExportMetricsServiceResponse, error) {
	s.receiver.store.IngestMetrics(req.GetResourceMetrics())
	if err := s.receiver.persist(ctx, "metrics", req); err != nil {
		return nil, err
	}
	return &collmetricsv1.ExportMetricsServiceResponse{}, nil
}

func (r *Receiver) persist(ctx context.Context, signal string, msg proto.Message) error {
	if r.persistence == nil {
		return nil
	}
	payload, err := proto.Marshal(msg)
	if err != nil {
		return err
	}
	if err := r.persistence.WriteRaw(ctx, signal, payload); err != nil {
		return err
	}
	return r.persistence.WriteSnapshot(ctx, r.store.Snapshot())
}
