package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/FluxGraph/fluxgraph/core"
	"github.com/FluxGraph/fluxgraph/interfaces"
	"github.com/google/uuid"
	"github.com/pgvector/pgvector-go"
)

// DBQuerier is the minimal interface needed from pgx/sql, enabling clean unit-
// test mocking without importing the full pgx driver here.
type DBQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) DBRow
	Exec(ctx context.Context, sql string, args ...any) error
	Query(ctx context.Context, sql string, args ...any) (DBRows, error)
	BeginTx(ctx context.Context) (DBTx, error)
}

// DBRow mirrors pgx.Row.
type DBRow interface {
	Scan(dest ...any) error
}

// DBRows mirrors pgx.Rows.
type DBRows interface {
	Next() bool
	Scan(dest ...any) error
	Close()
	Err() error
}

// DBTx is a transaction handle.
type DBTx interface {
	DBQuerier
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// PostgresMemoryStore implements interfaces.MemoryStore backed by PostgreSQL.
// Schema (DDL lives in migrations/001_create_fluxgraph_tables.sql):
//
//	sessions       (session_id PK, tenant_id, created_at, last_active_at, status)
//	agent_states   (session_id FK, state_json, version, updated_at)
//	checkpoints    (checkpoint_id PK, session_id FK, node_id, state_json, created_at)
//	messages       (id SERIAL, session_id FK, tenant_id, role, content_json, created_at)
type PostgresMemoryStore struct {
	db             DBQuerier
	embedder       interfaces.EmbeddingProvider
	maxCheckpoints int
	maxMessages    int
}

type PostgresMemoryStoreOptions struct {
	MaxCheckpoints int
	MaxMessages    int
	Embedder       interfaces.EmbeddingProvider
}

func NewPostgresMemoryStore(db DBQuerier, opts PostgresMemoryStoreOptions) *PostgresMemoryStore {
	if opts.MaxCheckpoints == 0 {
		opts.MaxCheckpoints = 50
	}
	if opts.MaxMessages == 0 {
		opts.MaxMessages = 200
	}
	return &PostgresMemoryStore{
		db:             db,
		embedder:       opts.Embedder,
		maxCheckpoints: opts.MaxCheckpoints,
		maxMessages:    opts.MaxMessages,
	}
}

// Save upserts the agent state using optimistic-lock version control and writes
// a checkpoint record in the same transaction.
func (s *PostgresMemoryStore) Save(ctx context.Context, sessionID string, state *core.AgentState) (string, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	ckptID := uuid.New().String()
	tenantID, _ := ctx.Value("tenantID").(string)

	tx, err := s.db.BeginTx(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Upsert session record.
	err = tx.Exec(ctx, `
		INSERT INTO sessions (session_id, tenant_id, created_at, last_active_at, status)
		VALUES ($1, $2, NOW(), NOW(), $3)
		ON CONFLICT (session_id) DO UPDATE
		  SET last_active_at = NOW(), status = EXCLUDED.status`,
		sessionID, tenantID, string(state.Status),
	)
	if err != nil {
		return "", fmt.Errorf("upsert session: %w", err)
	}

	// 2. Upsert agent_state with optimistic version bump.
	err = tx.Exec(ctx, `
		INSERT INTO agent_states (session_id, state_json, version, updated_at)
		VALUES ($1, $2, 1, NOW())
		ON CONFLICT (session_id) DO UPDATE
		  SET state_json = EXCLUDED.state_json,
		      version    = agent_states.version + 1,
		      updated_at = NOW()`,
		sessionID, string(data),
	)
	if err != nil {
		return "", fmt.Errorf("upsert agent_state: %w", err)
	}

	// 3. Insert checkpoint.
	err = tx.Exec(ctx, `
		INSERT INTO checkpoints (checkpoint_id, session_id, state_json, created_at)
		VALUES ($1, $2, $3, NOW())`,
		ckptID, sessionID, string(data),
	)
	if err != nil {
		return "", fmt.Errorf("insert checkpoint: %w", err)
	}

	if e := tx.Commit(ctx); e != nil {
		return "", e
	}

	// Prune old checkpoints asynchronously.
	go s.pruneCheckpoints(context.Background(), sessionID)

	return ckptID, nil
}

func (s *PostgresMemoryStore) pruneCheckpoints(ctx context.Context, sessionID string) {
	_ = s.db.Exec(ctx, `
		DELETE FROM checkpoints
		WHERE session_id = $1
		  AND checkpoint_id NOT IN (
		    SELECT checkpoint_id FROM checkpoints
		    WHERE session_id = $1
		    ORDER BY created_at DESC
		    LIMIT $2
		  )`, sessionID, s.maxCheckpoints)
}

// Load reads the latest agent state for a session.
func (s *PostgresMemoryStore) Load(ctx context.Context, sessionID string) (*core.AgentState, error) {
	row := s.db.QueryRow(ctx,
		`SELECT state_json FROM agent_states WHERE session_id = $1`, sessionID)

	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, errNoRows) {
			return nil, fmt.Errorf("session %s not found", sessionID)
		}
		return nil, err
	}
	var state core.AgentState
	return &state, json.Unmarshal([]byte(raw), &state)
}

// LoadCheckpoint retrieves a specific historical snapshot by ID.
func (s *PostgresMemoryStore) LoadCheckpoint(ctx context.Context, checkpointID string) (*core.AgentState, error) {
	row := s.db.QueryRow(ctx,
		`SELECT state_json FROM checkpoints WHERE checkpoint_id = $1`, checkpointID)

	var raw string
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, errNoRows) {
			return nil, fmt.Errorf("checkpoint %s not found", checkpointID)
		}
		return nil, err
	}
	var state core.AgentState
	return &state, json.Unmarshal([]byte(raw), &state)
}

// ListCheckpoints returns metadata for all checkpoints of a session, most-recent first.
func (s *PostgresMemoryStore) ListCheckpoints(ctx context.Context, sessionID string) ([]interfaces.CheckpointMeta, error) {
	rows, err := s.db.Query(ctx,
		`SELECT checkpoint_id, session_id, created_at
		 FROM checkpoints WHERE session_id = $1
		 ORDER BY created_at DESC LIMIT $2`,
		sessionID, s.maxCheckpoints)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var metas []interfaces.CheckpointMeta
	for rows.Next() {
		var m interfaces.CheckpointMeta
		if e := rows.Scan(&m.CheckpointID, &m.SessionID, &m.CreatedAt); e != nil {
			return nil, e
		}
		metas = append(metas, m)
	}
	return metas, rows.Err()
}

// AppendMessages inserts new messages and trims the list to maxMessages.
// Both operations run in a single transaction.
func (s *PostgresMemoryStore) AppendMessages(ctx context.Context, sessionID string, messages []core.Message) error {
	tenantID, _ := ctx.Value("tenantID").(string)

	tx, err := s.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, msg := range messages {
		b, err := json.Marshal(msg.Parts)
		if err != nil {
			return err
		}

		// Phase 5: Vector RAG Integration
		// Only attempt to embed user messages to save tokens and focus semantic search on user queries.
		var embeddingVal any
		if s.embedder != nil && msg.Role == core.RoleUser {
			var textContent string
			for _, part := range msg.Parts {
				if part.Type == core.PartTypeText {
					textContent += part.Text + "\n"
				}
			}
			if textContent != "" {
				emb, err := s.embedder.EmbedText(ctx, textContent)
				if err == nil {
					embeddingVal = pgvector.NewVector(emb)
				}
			}
		}

		if e := tx.Exec(ctx, `
			INSERT INTO messages (session_id, tenant_id, role, content_json, embedding, created_at)
			VALUES ($1, $2, $3, $4, $5, NOW())`,
			sessionID, tenantID, string(msg.Role), string(b), embeddingVal,
		); e != nil {
			return e
		}
	}

	// Sliding-window trim: delete oldest messages beyond maxMessages.
	if e := tx.Exec(ctx, `
		DELETE FROM messages
		WHERE session_id = $1
		  AND id NOT IN (
		    SELECT id FROM messages
		    WHERE session_id = $1
		    ORDER BY created_at DESC
		    LIMIT $2
		  )`, sessionID, s.maxMessages); e != nil {
		return e
	}

	return tx.Commit(ctx)
}

// Search utilizes pgvector to find semantically similar historical messages.
func (s *PostgresMemoryStore) Search(ctx context.Context, sessionID string, query string, topK int) ([]core.Message, error) {
	if s.embedder == nil {
		return nil, errors.New("embedding provider not configured for pg store")
	}

	queryEmb, err := s.embedder.EmbedText(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query failed: %w", err)
	}
	vec := pgvector.NewVector(queryEmb)

	// <=> is the cosine distance operator in pgvector
	rows, err := s.db.Query(ctx, `
		SELECT role, content_json 
		FROM messages 
		WHERE session_id = $1 AND embedding IS NOT NULL
		ORDER BY embedding <=> $2
		LIMIT $3`,
		sessionID, vec, topK)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []core.Message
	for rows.Next() {
		var roleStr string
		var contentStr string
		if err := rows.Scan(&roleStr, &contentStr); err != nil {
			return nil, err
		}
		
		msg := core.Message{Role: core.Role(roleStr)}
		if err := json.Unmarshal([]byte(contentStr), &msg.Parts); err != nil {
			continue // Skip corrupted history rows safely
		}
		results = append(results, msg)
	}

	return results, rows.Err()
}

// errNoRows is a sentinel used to detect "not found" without importing pgx directly.
var errNoRows = errors.New("no rows in result set")
