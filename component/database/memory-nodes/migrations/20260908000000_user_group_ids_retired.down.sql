-- Irreversible, and deliberately a no-op rather than a guess.
--
-- The up migration DROPS a key whose values are gone once dropped. Restoring
-- `groupIds: []` on every row would be inventing data -- and an empty array is
-- not what the removed rows necessarily held, even though it is what the two
-- mutations wrote. Rolling this back means rolling back the engine version that
-- retired the field, at which point the concept declares it again and an absent
-- key is legal (the field was never @required).
--
-- So there is nothing to undo: the down direction leaves the rows as they are
-- and the older engine reads them correctly.

SELECT 1;
