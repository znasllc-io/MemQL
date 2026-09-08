-- Retired concept field: `recommendedPolicySlug` on v1:agents:agentRole
-- (memql#5210, cluster 4 of 4).
--
-- THE MECHANISM IS memql#5199's: `additionalProperties: false` plus a
-- read-merge that validates the MERGED payload means any stored key its concept
-- no longer declares makes that row unwritable on its next write.
--
-- WHERE IT WENT. Commit 4812ec5c4, "Issue #5130, #5128: the corpus gates, the
-- floor-rule ordering fix, and the last two places a retired policy survived".
-- The field named an AI Router policy per role -- its own @description listed
-- `balancedChat`, `strongReasoning`, `fastCoding`, `cheapestCapable` and said
-- an empty value fell back to `balancedChat`. Epic memql#5127 replaced
-- per-call provider selection with LEVELS, POLICIES and RULES: a call declares
-- a level and one seam decides, and the four policy slugs that field offered no
-- longer exist. It was one of the two places a retired policy name survived,
-- which is what that commit removed.
--
-- WHY IT MATTERS OUT OF PROPORTION TO ITS SHAPE. v1:agents:agentRole is the
-- role CATALOG, and its predefined rows are re-seeded from dsl/agents on EVERY
-- STARTUP -- the locks "come back on the next startup if not aligned with the
-- source slice", in the concept's own words. A seed write is a write, so on a
-- cluster that crossed the removal this is a failure on every boot, on the rows
-- that decide what every agent may do. Measured on the shared throwaway
-- database: 9,700 versions.
--
-- SCOPED BY CONCEPT, NEVER BY KEY NAME. Declared on no concept in the tree
-- today (measured); scoped anyway, for the reason the sibling migrations give.
--
-- APPEND-ONLY ROWS, EVERY VERSION. Idempotent: the guard matches nothing on a
-- cluster whose role rows were all written after the removal.

UPDATE "MemoryNodes"
SET payload = payload - 'recommendedPolicySlug'
WHERE concept = 'v1:agents:agentRole'
  AND payload ? 'recommendedPolicySlug';
