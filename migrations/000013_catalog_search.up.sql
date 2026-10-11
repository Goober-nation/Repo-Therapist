-- Name search over the public repo catalog (GET /search): trigram indexes serve the substring and
-- fuzzy matching of repo and org names.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX public_repo_catalog_name_trgm ON public_repo_catalog
  USING GIN (lower(org_slug || '/' || repo_slug) gin_trgm_ops);
CREATE INDEX public_repo_catalog_org_trgm ON public_repo_catalog
  USING GIN (lower(org_slug) gin_trgm_ops);
