-- Irreversible, and deliberately a no-op rather than a guess.
--
-- The up migration DROPS four keys whose values are gone once dropped.
-- Inventing them back is not possible and would not help: `gender` was a free
-- string driving an avatar voice, and the two media toggles and the trigger
-- behaviour were per-agent settings a person chose. There is no default that
-- would be true of any particular row.
--
-- Rolling this back means rolling back the engine version that retired the
-- fields, at which point v1:agents:agent declares them again and an absent key
-- is legal -- none of the four was @required.
--
-- So there is nothing to undo: the down direction leaves the rows as they are
-- and the older engine reads them correctly, showing an agent whose voice and
-- media settings have returned to their defaults.

SELECT 1;
