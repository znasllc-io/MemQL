-- The embedding cache is keyed by BINDING, and its rows expire
-- (epic memql#5137, D6).
--
-- WHY THE KEY CHANGES. `embedding_cache` was keyed by text and PROVIDER, which
-- was adequate while the cluster had exactly one embedder pinned by name in
-- five files. With a binding it is not: two bindings can name the same provider
-- at different widths (qwen3-embedding truncates from 4096 down to 32), and two
-- different bindings can produce vectors of the same width that mean entirely
-- different things. A vector cached under the wrong binding is not a stale
-- answer -- it is a vector from another geometry, returned as though it were
-- this one's, and cosine distance against it is a number with no meaning.
--
-- WHY AN EXPIRY. The column already existed and nothing ever set it, so every
-- entry lived forever. A cache with no eviction is a table that only grows, and
-- for a corpus that is re-embedded on a binding switch it holds vectors for a
-- model the cluster no longer uses.
--
-- THE OLD ROWS ARE NOT MIGRATED, DELIBERATELY. They carry no binding id, so
-- there is nothing to migrate them TO -- inventing one would claim they came
-- from a binding that may not exist. They are simply never read again under the
-- new key, and the expiry sweep removes them. Pre-release, no shims: a cache
-- miss re-embeds, which is the correct and cheap outcome.

ALTER TABLE embedding_cache
  ADD COLUMN IF NOT EXISTS binding_id TEXT NOT NULL DEFAULT '';

-- The lookup index. `cache_key` is already the primary key, so this covers the
-- (key, binding) pair a read actually asks for.
CREATE INDEX IF NOT EXISTS idx_embedding_cache_binding
  ON embedding_cache (binding_id, cache_key);

-- Default TTL for entries written from here on. Applied by the writer rather
-- than by the schema -- a column default cannot see the binding's activation
-- time, and an entry's usefulness ends when its binding does, not when a clock
-- says so.
