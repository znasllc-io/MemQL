-- Irreversible, and deliberately a no-op rather than a guess.
--
-- The up migration DROPS a key whose values are gone once dropped. Restoring
-- `status: "healthy"` on every row would be inventing data -- and although
-- "healthy" is in fact the only value `createCluster` ever wrote, writing it
-- back would re-assert the stale constant the removal existed to delete, and
-- an engine old enough to read it is an engine that would show it to an
-- operator with every node down.
--
-- Rolling this back means rolling back the engine version that retired the
-- field, at which point the concept declares it again and an absent key is
-- legal (`status` was never @required -- it resolved through
-- `args.status ?? "healthy"`).
--
-- So there is nothing to undo: the down direction leaves the rows as they are
-- and the older engine reads them correctly.

SELECT 1;
