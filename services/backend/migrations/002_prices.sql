ALTER TABLE users ADD COLUMN locale_explicit boolean NOT NULL DEFAULT false;
CREATE TABLE price_versions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 billing_provider text NOT NULL, model_version text NOT NULL, currency char(3) NOT NULL,
 effective_from timestamptz NOT NULL, effective_to timestamptz NOT NULL CHECK(effective_to>effective_from),
 service_tier text NOT NULL DEFAULT '', region text NOT NULL DEFAULT '', route text NOT NULL DEFAULT '',
 input_per_million numeric(30,12) NOT NULL CHECK(input_per_million>=0),
 output_per_million numeric(30,12) NOT NULL CHECK(output_per_million>=0),
 cache_read_per_million numeric(30,12) NOT NULL CHECK(cache_read_per_million>=0),
 cache_write_5m_per_million numeric(30,12) NOT NULL CHECK(cache_write_5m_per_million>=0),
 cache_write_1h_per_million numeric(30,12) NOT NULL CHECK(cache_write_1h_per_million>=0),
 price_basis text NOT NULL CHECK(price_basis IN ('public','contract')),
 evidence_url text NOT NULL, assumptions text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id)
);
ALTER TABLE price_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE price_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON price_versions USING(workspace_id=nullif(current_setting('app.workspace_id',true),'')::uuid) WITH CHECK(workspace_id=nullif(current_setting('app.workspace_id',true),'')::uuid);
ALTER TABLE reconciliation_runs ALTER COLUMN invoice_id DROP NOT NULL;
ALTER TABLE reconciliation_runs ALTER COLUMN billed DROP NOT NULL;
ALTER TABLE reconciliation_runs ADD COLUMN account_id uuid;
ALTER TABLE reconciliation_runs ADD COLUMN period_start timestamptz;
ALTER TABLE reconciliation_runs ADD COLUMN period_end timestamptz;
ALTER TABLE reconciliation_runs ADD CONSTRAINT run_account_fk FOREIGN KEY(workspace_id,account_id) REFERENCES provider_accounts(workspace_id,id);
CREATE UNIQUE INDEX l1_run_version ON reconciliation_runs(account_id,period_start,period_end,currency,run_version) WHERE level='L1';
ALTER TABLE sync_runs ADD COLUMN attempts integer NOT NULL DEFAULT 0;
