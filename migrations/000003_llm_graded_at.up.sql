-- graded_at of the grade an LLM response describes (llm.recommendations.received graded_at).
-- A response is current only when it equals the stored grade's graded_at; NULL is never current.

ALTER TABLE repo_llm_responses_public ADD COLUMN graded_at TIMESTAMPTZ;
ALTER TABLE repo_llm_responses_private ADD COLUMN graded_at TIMESTAMPTZ;
