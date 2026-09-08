DROP INDEX IF EXISTS idx_embedding_cache_binding;
ALTER TABLE embedding_cache DROP COLUMN IF EXISTS binding_id;
