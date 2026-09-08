-- Irreversible, and deliberately a no-op rather than a guess.
--
-- The up migration DROPS a key whose values are gone once dropped, and writing
-- one back would be worse than leaving it absent: the only values it ever held
-- name a staging-versus-production split the product no longer has, so
-- restoring "production" onto every row would re-assert a distinction the
-- collapse existed to delete.
--
-- Rolling this back means rolling back the engine version that retired the
-- field, at which point both concepts declare it again and an absent key is
-- legal -- `environment` was never @required.
--
-- So there is nothing to undo: the down direction leaves the rows as they are
-- and the older engine reads them correctly.

SELECT 1;
