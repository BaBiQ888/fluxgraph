package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/FluxGraph/fluxgraph/config"
	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/storage"
)

type mockEmbedder struct{}

func (m *mockEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	// Return a dummy 1536-dimensional vector to simulate OpenAI embeddings
	vec := make([]float32, 1536)
	vec[0] = float32(len(text)) // Slight variation based on text length for sorting differences
	return vec, nil
}

func main() {
	cfg := &config.Config{
		Storage: config.StorageConfig{
			Postgres: config.PostgresConfig{
				URL:      "postgres://fluxgraph:password@localhost:5432/fluxgraph?sslmode=disable",
				MaxConns: 5,
			},
		},
	}
	
	pgDriver, err := storage.NewPostgresDriver(cfg.Storage.Postgres.URL, cfg.Storage.Postgres.MaxConns)
	if err != nil {
		log.Fatalf("failed to connect to postgres: %v", err)
	}

	embedProvider := &mockEmbedder{}

	pgStore := storage.NewPostgresMemoryStore(pgDriver, storage.PostgresMemoryStoreOptions{
		Embedder: embedProvider,
	})

	ctx := context.Background()
	sessionID := "test-rag-session-12345"
	
	err = pgDriver.Exec(ctx, "INSERT INTO sessions (session_id) VALUES ($1) ON CONFLICT DO NOTHING", sessionID)
	if err != nil {
		log.Fatalf("failed to insert session: %v", err)
	}

	// Stub a faux multi-turn conversation
	msgs := []core.Message{
		{
			ID:        "msg-1",
			Role:      core.RoleUser,
			Parts:     []core.Part{{Type: core.PartTypeText, Text: "Hi, I am looking for a new laptop for programming."}},
			Timestamp: time.Now().Add(-10 * time.Minute),
		},
		{
			ID:        "msg-2",
			Role:      core.RoleAssistant,
			Parts:     []core.Part{{Type: core.PartTypeText, Text: "I'd recommend a MacBook Pro with an M3 chip or a ThinkPad X1 Carbon. Do you prefer macOS or Windows/Linux?"}},
			Timestamp: time.Now().Add(-9 * time.Minute),
		},
		{
			ID:        "msg-3",
			Role:      core.RoleUser,
			Parts:     []core.Part{{Type: core.PartTypeText, Text: "I prefer macOS. I also like photography so a good screen is important."}},
			Timestamp: time.Now().Add(-8 * time.Minute),
		},
		{
			ID:        "msg-4",
			Role:      core.RoleAssistant,
			Parts:     []core.Part{{Type: core.PartTypeText, Text: "The MacBook Pro's Liquid Retina XDR display is excellent for photography. You might want 32GB of RAM if you edit photos while coding."}},
			Timestamp: time.Now().Add(-7 * time.Minute),
		},
	}

	if err := pgStore.AppendMessages(ctx, sessionID, msgs); err != nil {
		log.Fatalf("failed to append messages: %v", err)
	}

	fmt.Println("Inserted faux conversation successfully.")

	// Query contextually
	query := "What operating system does the user prefer for their new laptop?"
	results, err := pgStore.Search(ctx, sessionID, query, 3)
	if err != nil {
		log.Fatalf("failed to search messages: %v", err)
	}

	fmt.Printf("\nSearch Query: %s\n", query)
	for i, msg := range results {
		fmt.Printf("Match %d (Role: %s): ", i+1, msg.Role)
		for _, part := range msg.Parts {
			if part.Type == core.PartTypeText {
				fmt.Print(part.Text)
			}
		}
		fmt.Println()
	}
}
