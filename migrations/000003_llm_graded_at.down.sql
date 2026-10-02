ALTER TABLE repo_llm_responses_private DROP COLUMN IF EXISTS graded_at;
ALTER TABLE repo_llm_responses_public DROP COLUMN IF EXISTS graded_at;
