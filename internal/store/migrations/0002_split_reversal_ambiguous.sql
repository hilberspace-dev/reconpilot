-- Split settlements and reversals are match kinds of their own, and a row the
-- engine will not pair because more than one partner fits is ambiguous.
-- Migrate runs every file on each start. Rewriting a CHECK constraint locks
-- its table and rechecks every row, so each one is replaced only while it
-- still lacks the new values.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conrelid = 'match_group'::regclass
                     AND conname = 'match_group_kind_check'
                     AND pg_get_constraintdef(oid) LIKE '%''split''%'
                     AND pg_get_constraintdef(oid) LIKE '%''reversal''%') THEN
        ALTER TABLE match_group DROP CONSTRAINT IF EXISTS match_group_kind_check;
        ALTER TABLE match_group ADD CONSTRAINT match_group_kind_check
            CHECK (kind IN ('exact','split','tolerant','reversal','group','manual'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conrelid = 'discrepancy'::regclass
                     AND conname = 'discrepancy_type_check'
                     AND pg_get_constraintdef(oid) LIKE '%''ambiguous''%') THEN
        ALTER TABLE discrepancy DROP CONSTRAINT IF EXISTS discrepancy_type_check;
        ALTER TABLE discrepancy ADD CONSTRAINT discrepancy_type_check CHECK (type IN
            ('commission','refund','partial','timing','duplicate','missing','ambiguous','unknown'));
    END IF;
END
$$;
