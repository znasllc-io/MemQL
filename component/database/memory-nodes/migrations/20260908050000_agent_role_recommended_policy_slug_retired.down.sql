-- Irreversible, and deliberately a no-op rather than a guess.
--
-- The up migration DROPS a key whose values name AI Router policies that no
-- longer exist. Writing `balancedChat` back -- the value its own @description
-- gave as the fallback -- would restore a slug epic memql#5127 retired, so an
-- engine old enough to read it would route on a policy name nothing resolves.
--
-- Rolling this back means rolling back the engine version that retired the
-- field, at which point v1:agents:agentRole declares it again and an absent key
-- is legal -- the field was never @required, and its own documented behaviour
-- for an empty value was to fall back.
--
-- So there is nothing to undo: the down direction leaves the rows as they are
-- and the older engine reads them correctly.

SELECT 1;
