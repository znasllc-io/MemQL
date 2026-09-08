-- Irreversible, and deliberately a no-op rather than a guess.
--
-- The up migration DROPS six pointers into a model that no longer exists.
-- v1:planner:plan and v1:planner:task were DELETED by epic memql#5000, so even
-- a perfectly restored `planId` would name a row nothing can read -- the values
-- were only ever meaningful as a join into concepts the engine no longer
-- registers.
--
-- Rolling this back means rolling back the engine version that retired the
-- fields, at which point the five concepts declare them again and an absent key
-- is legal -- none of the six was @required. The rows lose their lineage
-- pointers, which is the same thing that happens to every row written after the
-- retirement.
--
-- So there is nothing to undo: the down direction leaves the rows as they are
-- and the older engine reads them correctly.

SELECT 1;
