-- Lease token prevents a stale Worker from committing a Job after another
-- Worker has recovered and re-claimed it.
ALTER TABLE online_eval_jobs
    ADD COLUMN IF NOT EXISTS lease_token UUID;
