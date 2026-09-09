-- Irreversible: removed settings and planner pointers cannot be reconstructed.
-- Restoring the old image does not restore these values. Recover them from
-- the pre-upgrade database backup if a rollback needs the retired behavior.
SELECT 1;
