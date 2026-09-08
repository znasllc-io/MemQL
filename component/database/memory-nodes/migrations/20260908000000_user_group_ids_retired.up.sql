-- Retired concept fields: v1:identity:user.groupIds, and the five
-- v1:identity:group sheds (epic memql#5165).
--
-- WHY A DATA MIGRATION AND NOT JUST A CONCEPT EDIT.
--
-- Every concept in this engine builds its JSON schema with
-- `additionalProperties: false` (concept_parser.go's parsedConcept defaults
-- noAdditional to true), and a mutation's read-merge validates the MERGED
-- payload -- stored keys included. So deleting a field from a concept does not
-- merely stop new writes carrying it: it makes every EXISTING row that carries
-- it unwritable. The next `updateUser`, `deleteUserHard` or lastSeenAt stamp
-- against such a row fails validation with
-- `additionalProperties 'groupIds' not allowed`.
--
-- `createUser` and `createUserOnFirstLogin` both wrote `groupIds: args.groupIds
-- ?? []` on every user they ever made, so on a cluster with any history that is
-- EVERY user row, and the first symptom would be that signing in stops working
-- -- the session write stamps the user row.
--
-- Membership now lives on v1:identity:groupMembership, written by
-- integrations/groups. The field held nothing this migration needs to preserve:
-- its own @description said "reserved for future cluster-side group-based
-- access derivation", nothing ever read it, and no mutation but the two above
-- ever wrote it.
--
-- SCOPED TO v1:identity:user ON PURPOSE. v1:agents:agent carries its own
-- `groupIds` -- a different field on a different concept, still declared, still
-- filtered on by `activeAgents` -- and agents as group members is out of scope
-- (design section N). A migration that stripped by key name alone would delete
-- live data from a concept this epic never touched.
--
-- APPEND-ONLY ROWS, EVERY VERSION. The table is a time series and a row's
-- history is its versions, so this rewrites all of them rather than the newest
-- per id: a read-merge reads the newest, but the older versions are what an
-- audit walk returns, and half a history validating is worse than none.

UPDATE "MemoryNodes"
SET payload = payload - 'groupIds'
WHERE concept = 'v1:identity:user'
  AND payload ? 'groupIds';

-- And the six fields the RESHAPED v1:identity:group sheds: memberIds,
-- agentIds, maxHumans, maxAgents, externalId, active. Membership is its own concept
-- now (v1:identity:groupMembership), the capacities had no enforcement behind
-- them, and agents as group members is out of scope (design section N).
--
-- WHY THIS IS NOT BELT-AND-BRACES. The concept was inert -- no mutation, query
-- or shape was ever bound to it, so nothing in the DSL ever wrote a group row.
-- But `externalId`'s own @description said the field was "preserved for any
-- legacy rows that came from a previous external sync source", which is a
-- statement that such rows may exist somewhere. If they do, they carry all
-- six keys and the first write to touch one fails exactly as the user rows
-- did.
--
-- `active` was in that set and this migration MISSED IT until a per-concept
-- field sweep found it (memql-f9's, written after the per-file version I ran
-- proved unable to see a field that moves between concepts in one file). The
-- reshaped concept replaced `active` with a `status` enum -- a lifecycle with
-- an archivedAt rather than a boolean -- so the key is genuinely gone, and it
-- was missing here only because I enumerated the shed fields from memory of
-- the reshape instead of measuring the difference.
--
-- Idempotent and free where they do not: `payload ?| array[...]` matches
-- nothing on a cluster whose group rows this epic wrote.

UPDATE "MemoryNodes"
SET payload = payload - 'memberIds' - 'agentIds' - 'maxHumans' - 'maxAgents'
                    - 'externalId' - 'active'
WHERE concept = 'v1:identity:group'
  AND payload ?| array['memberIds', 'agentIds', 'maxHumans', 'maxAgents', 'externalId', 'active'];
