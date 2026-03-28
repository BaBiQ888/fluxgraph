-- FluxGraph schema migration: 002_add_vector_search.sql
-- Apply with: psql $DATABASE_URL -f migrations/002_add_vector_search.sql

BEGIN;

-- Enable the pgvector extension for semantic search capabilities
CREATE EXTENSION IF NOT EXISTS vector;

-- Add embedding column to the messages table for vectorizing the content_json
-- The dimensions are set to 1536 which matches OpenAI's text-embedding-3-small
ALTER TABLE messages ADD COLUMN IF NOT EXISTS embedding vector(1536);

-- Create HNSW index for fast approximate nearest neighbor (ANN) searching
-- We use vector_cosine_ops which corresponds to '<=>' operator for cosine distance.
-- HNSW is highly efficient for high-dimensional data compared to IVFFlat.
CREATE INDEX IF NOT EXISTS idx_messages_embedding_hnsw 
    ON messages USING hnsw (embedding vector_cosine_ops);

COMMIT;
