package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FluxGraph/fluxgraph/a2a"
	a2apb "github.com/FluxGraph/fluxgraph/api/proto"
	"github.com/FluxGraph/fluxgraph/config"
	"github.com/FluxGraph/fluxgraph/engine"
	"github.com/FluxGraph/fluxgraph/graph"
	"github.com/FluxGraph/fluxgraph/observability"
	"github.com/FluxGraph/fluxgraph/providers"
	"github.com/FluxGraph/fluxgraph/storage"
	"github.com/FluxGraph/fluxgraph/tools"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

func main() {
	// 1. Initialize Logger
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	
	// 2. Load Config
	cfg, err := config.LoadConfig("")
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load configuration")
	}
	if cfg.App.Env == "production" {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
		log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	} else {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	log.Info().
		Str("app", cfg.App.Name).
		Str("env", cfg.App.Env).
		Int("http_port", cfg.App.Port).
		Int("grpc_port", cfg.App.GRPCPort).
		Msg("starting fluxgraph")

	// 3. Initialize Observability
	_, err = observability.InitTracer(cfg.App.Name)
	if err != nil {
		log.Warn().Err(err).Msg("failed to initialize tracer")
	}

	// 4. Initialize Storage Drivers
	redisDriver, err := storage.NewRedisDriver(cfg.Storage.Redis.URL, cfg.Storage.Redis.PoolSize)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to redis")
	}

	pgDriver, err := storage.NewPostgresDriver(cfg.Storage.Postgres.URL, cfg.Storage.Postgres.MaxConns)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to postgres")
	}
	// 5. Initialize Resiliency Providers (Embedding & LLM)
	embedProvider := providers.NewOpenAIEmbeddingProvider(providers.OpenAIEmbeddingOptions{
		APIKey:  cfg.LLM.OpenAI.APIKey,
		BaseURL: cfg.LLM.OpenAI.BaseURL,
	})

	_ = providers.NewOpenAIProvider(providers.OpenAIOptions{
		APIKey:  cfg.LLM.OpenAI.APIKey,
		BaseURL: cfg.LLM.OpenAI.BaseURL,
		Model:   cfg.LLM.OpenAI.Model,
	})

	// 6. Initialize Memory & Task Stores
	// Cold Layer (Postgres + pgvector RAG)
	pgStore := storage.NewPostgresMemoryStore(pgDriver, storage.PostgresMemoryStoreOptions{
		Embedder: embedProvider,
	})
	_ = pgStore // Can be injected into engine/server as the historical Searcher

	// Hot Layer (Redis)
	memStore := storage.NewRedisMemoryStore(redisDriver, storage.RedisMemoryStoreOptions{
		StateTTL: cfg.Storage.Redis.TTL,
	})
	ts := storage.NewRedisTaskStore(redisDriver, "fluxgraph", cfg.Storage.Redis.TTL)
	
	// 6. Setup Graph & Engine
	builder := graph.NewBuilder()
	g, _ := builder.Build()
	reg := tools.NewConcreteToolRegistry()
	reg.Register(tools.NewSearchMemoryTool(pgStore))
	
	eng := engine.NewEngine(g, memStore, nil, 
		engine.WithHooks(
			observability.NewOtelTracingHook(),
			observability.NewPrometheusMetricHook(),
		),
	)

	// 7. (Providers are initialized at step 5)
	
	// 8. Setup A2A Servers
	serverOpts := a2a.ServerOptions{
		Name:    cfg.App.Name,
		Version: "1.0.0",
		URL:     fmt.Sprintf("http://localhost:%d", cfg.App.Port),
		Secret:  cfg.App.A2ASecret,
	}
	
	a2aSrv := a2a.NewServer(eng, ts, memStore, reg, nil, serverOpts)

	// HTTP Server
	httpSrv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.App.Port),
		Handler: a2aSrv,
	}

	go func() {
		log.Info().Msgf("HTTP server listening on %s", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("http server failed")
		}
	}()

	// gRPC Server
	grpcSrv := grpc.NewServer()
	a2aGrpc := a2a.NewGRPCServer(a2aSrv)
	a2apb.RegisterAgentServiceServer(grpcSrv, a2aGrpc)

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.App.GRPCPort))
	if err != nil {
		log.Fatal().Err(err).Msg("failed to listen for gRPC")
	}

	go func() {
		log.Info().Msgf("gRPC server listening on %s", lis.Addr())
		if err := grpcSrv.Serve(lis); err != nil {
			log.Fatal().Err(err).Msg("gRPC server failed")
		}
	}()

	// 10. Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Info().Msg("shutting down servers...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("http server forced to shutdown")
	}
	grpcSrv.GracefulStop()

	log.Info().Msg("servers exited gracefully")
}
