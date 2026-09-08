-- Retired concept fields: the six pointers left behind by the planner
-- retirement (memql#5210, cluster 2 of 4).
--
--   v1:worker:invocation      planId, taskId
--   v1:worker:appSession      planId, taskId
--   v1:workbench:workspace    planId
--   v1:library:artifact       producedByPlanId
--
-- THE MECHANISM IS memql#5199's: `additionalProperties: false` plus a
-- read-merge that validates the MERGED payload means any stored key its concept
-- no longer declares makes that row unwritable on its next write, with
-- `additionalProperties 'planId' not allowed`.
--
-- WHERE THEY WENT. Commit f8b0126d6, "work: delete v1:planner:plan, task and
-- taskState, and sweep what named them" -- epic memql#5000, which replaced the
-- plan model with the work spine (v1:work:{goal,run,step,...}). The sweep
-- removed every field POINTING AT a plan or a task from the five concepts
-- above. It did not, and could not, touch the rows already carrying them.
--
-- WHY FOUR CONCEPTS IN ONE MIGRATION AND NOT SIX. One retirement, one
-- migration: these six pairs are one decision made once, and splitting them
-- per concept would produce six files whose comments all say the same thing
-- while making the set harder to read as a set. The statements below are
-- separate only where the KEY SET differs.
--
-- THE COUNTS ARE SMALL AND THAT IS NOT A REASON TO SKIP IT. Measured on the
-- shared throwaway database: 377 versions on v1:worker:invocation, 186 on
-- appSession, 20 on v1:data:log, 11 on artifact, 2 on workspace. A worker
-- invocation row is telemetry with a 90-day retention sweep, so those age out;
-- an ARTIFACT does not, and eleven unwritable artifacts on a real cluster is
-- eleven files somebody cannot rename.
--
-- v1:data:log's `planId` (20 versions) IS DELIBERATELY ABSENT from this
-- migration. It was measured in the same sweep and it is NOT this retirement:
-- `planId` was never declared on v1:data:log in this repository's history, so
-- there is nothing here that removed it and no cluster reached the shape by
-- taking an upgrade. It is one of five pairs the sweep turned up that are
-- fixture debris in the shared CI database -- see memql#5210 for the writer of
-- each. Stripping a key on a concept that never declared it would be a
-- migration written against a measurement nobody could explain.
--
-- SCOPED BY CONCEPT, NEVER BY KEY NAME. `planId`, `taskId` and
-- `producedByPlanId` are declared on NO concept in the tree today -- measured,
-- not assumed -- so a name-scoped strip would in fact hit nothing else. The
-- scoping is here anyway, because "it happens to be unique right now" is not a
-- property a migration may rest on: `status` was retired on one cluster concept
-- and live on two others with 4,679 versions between them (memql#5199), and
-- `gender` is live on v1:identity:user while being retired on v1:agents:agent
-- in the migration beside this one.
--
-- APPEND-ONLY ROWS, EVERY VERSION -- a read-merge reads the newest, but the
-- older versions are what an audit walk returns.
--
-- Idempotent: the `payload ?|` guards match nothing on a cluster whose rows
-- were all written after the retirement.

UPDATE "MemoryNodes"
SET payload = payload - 'planId' - 'taskId'
WHERE concept IN ('v1:worker:invocation', 'v1:worker:appSession')
  AND payload ?| array['planId', 'taskId'];

UPDATE "MemoryNodes"
SET payload = payload - 'planId'
WHERE concept = 'v1:workbench:workspace'
  AND payload ? 'planId';

UPDATE "MemoryNodes"
SET payload = payload - 'producedByPlanId'
WHERE concept = 'v1:library:artifact'
  AND payload ? 'producedByPlanId';
