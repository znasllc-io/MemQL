-- Retired concept field: `environment` on v1:cluster:node and
-- v1:cluster:deployment (memql#5210, cluster 3 of 4).
--
-- THE MECHANISM IS memql#5199's: `additionalProperties: false` plus a
-- read-merge that validates the MERGED payload means any stored key its concept
-- no longer declares makes that row unwritable on its next write.
--
-- WHERE IT WENT. Commit 9cd492f19, "refactor(deploy): collapse the environment
-- split to one installation" (epic memql#3943 / #3949). MemQL ships ONE
-- installation shape: an operator who wants a second environment installs a
-- second instance, with its own domain and its own ArgoCD. There is no
-- staging-versus-production dimension inside the product, so a field naming one
-- had nothing left to hold. What varies is the deploy TARGET, and that carries
-- its own field, `provider` (docker-local / azure), which is live on both
-- concepts and is NOT touched here.
--
-- WHY THIS ONE IS AN UPGRADE HAZARD RATHER THAN A TEST FAILURE, sharply.
-- v1:cluster:node rows are written at node REGISTRATION -- every pod, every
-- restart -- and v1:cluster:deployment is the append-only deploy timeline. A
-- cluster that ran before the collapse has node rows carrying `environment`,
-- and the first registration that read-merges one fails. CI never sees it: the
-- db-tests lane runs against a FRESH database where the boot seed writes clean
-- rows. Measured on the shared throwaway database: 287 versions on deployment,
-- 84 on node.
--
-- THE FIELD NAME IS ITSELF NOW GATED, WHICH IS WHY THE STRIP MUST BE HERE AND
-- NOT IN GO. TestNoEnvironmentBranchingInEngineCode fails the build on engine
-- code so much as NAMING the tier words in any form -- comparison, switch case
-- or map key -- and its exemption map is EMPTY. So a Go-side repair that read
-- the stored value to decide anything is not merely discouraged, it does not
-- compile. A migration that deletes the key without reading it is the only
-- shape available, and it is also the right one.
--
-- SCOPED BY CONCEPT, NEVER BY KEY NAME. `environment` is declared on no concept
-- in the tree today (measured), but the scoping is the discipline rather than a
-- reaction to a collision: `environment` is an ordinary English word and
-- exactly the kind of key a product bundle mounted at MEMQL_DSL_PATH may
-- declare on a concept this repository has never seen.
--
-- APPEND-ONLY ROWS, EVERY VERSION. Idempotent: the guard matches nothing on a
-- cluster whose rows were all written after the collapse.

UPDATE "MemoryNodes"
SET payload = payload - 'environment'
WHERE concept IN ('v1:cluster:node', 'v1:cluster:deployment')
  AND payload ? 'environment';
