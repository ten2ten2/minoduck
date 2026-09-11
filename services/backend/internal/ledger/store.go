package ledger

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// CompleteScope verifies the union of complete source windows, including native
// zero-usage days. A user checkbox never substitutes for missing source data.
func CompleteScope(ctx context.Context, tx pgx.Tx, wid, account, currency, scope string, start, end time.Time) (bool, error) {
	var complete bool
	e := tx.QueryRow(ctx, `SELECT coalesce(range_agg(span) @> tstzrange($5,$6,'[)'),false) FROM (
 SELECT tstzrange(period_start,period_end,'[)') AS span FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND is_current AND cost_kind='actual' AND currency=$3 AND source_scope=$4 AND coverage_status='complete' AND period_start<$6 AND period_end>$5
 UNION ALL SELECT tstzrange((preview->>'period_start')::timestamptz,(preview->>'period_end')::timestamptz,'[)') FROM source_batches WHERE workspace_id=$1 AND account_id=$2 AND state='committed' AND source_scope='native-cost' AND $4='native-cost' AND preview ? 'period_start' AND preview ? 'period_end' AND (preview->>'period_start')::timestamptz<$6 AND (preview->>'period_end')::timestamptz>$5
 ) windows`, wid, account, currency, scope, start, end).Scan(&complete)
	return complete, e
}

// CompleteUsage verifies that retained, fully fetched native source shards cover
// the entire requested interval. A missing usage bucket can legitimately mean
// zero usage, so committed shard windows—not row presence—are the evidence.
func CompleteUsage(ctx context.Context, tx pgx.Tx, wid, account string, start, end time.Time) (bool, error) {
	var complete bool
	e := tx.QueryRow(ctx, `SELECT coalesce(range_agg(tstzrange((preview->>'period_start')::timestamptz,(preview->>'period_end')::timestamptz,'[)')) @> tstzrange($3,$4,'[)'),false) FROM source_batches WHERE workspace_id=$1 AND account_id=$2 AND state='committed' AND source_scope='native-cost' AND preview ? 'period_start' AND preview ? 'period_end' AND (preview->>'period_start')::timestamptz<$4 AND (preview->>'period_end')::timestamptz>$3`, wid, account, start, end).Scan(&complete)
	return complete, e
}

// Publish runs under an account row lock in the caller's tenant transaction.
func Publish(ctx context.Context, tx pgx.Tx, wid, account, batch string, entries []Entry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE IF NOT EXISTS publish_stage (
 row_number integer NOT NULL, source_record_key text NOT NULL, source_record_ref text NOT NULL,
 period_start timestamptz NOT NULL, period_end timestamptz NOT NULL, source_timezone text NOT NULL,
 billing_provider text NOT NULL, model_vendor text NOT NULL, raw_model_name text NOT NULL,
 charge_category text NOT NULL, cost_kind text NOT NULL, source_scope text NOT NULL,
 provider_project_ref text NOT NULL, amount numeric(30,12) NOT NULL, currency char(3) NOT NULL,
 coverage_status text NOT NULL, dimensions jsonb NOT NULL
) ON COMMIT DROP; TRUNCATE pg_temp.publish_stage`); err != nil {
		return 0, err
	}
	rows := make([][]any, 0, len(entries))
	for i, v := range entries {
		dimensions, _ := json.Marshal(v.Dimensions)
		if v.Dimensions == nil {
			dimensions = []byte(`{}`)
		}
		ref := v.SourceRef
		if ref == "" {
			ref = jsonRef(i)
		}
		rows = append(rows, []any{i, v.Key, ref, v.Start, v.End, v.Timezone, v.Provider, v.Vendor, v.Model, v.Category, v.Kind, v.Scope, v.Project, v.Amount, v.Currency, v.Coverage, dimensions})
	}
	columns := []string{"row_number", "source_record_key", "source_record_ref", "period_start", "period_end", "source_timezone", "billing_provider", "model_vendor", "raw_model_name", "charge_category", "cost_kind", "source_scope", "provider_project_ref", "amount", "currency", "coverage_status", "dimensions"}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"publish_stage"}, columns, pgx.CopyFromRows(rows)); err != nil {
		return 0, err
	}
	// Materialize candidates before retiring previous revisions. The old current
	// row must be retired before the partial unique index sees its replacement.
	// Publish can run more than once in an import/sync transaction, so reset the
	// transaction-local candidate table between batches.
	if _, err := tx.Exec(ctx, `DROP TABLE IF EXISTS pg_temp.publish_candidates`); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE publish_candidates ON COMMIT DROP AS
	 SELECT s.*,c.id AS old_id,coalesce(h.revision,0) AS previous_revision
 FROM pg_temp.publish_stage s
 LEFT JOIN cost_entries c ON c.workspace_id=$1 AND c.account_id=$2 AND c.source_record_key=s.source_record_key AND c.is_current
 LEFT JOIN LATERAL (
  SELECT max(revision) AS revision FROM cost_entries h
  WHERE h.workspace_id=$1 AND h.account_id=$2 AND h.source_record_key=s.source_record_key
 ) h ON true
 WHERE c.id IS NULL OR c.amount IS DISTINCT FROM s.amount
  OR c.coverage_status IS DISTINCT FROM s.coverage_status
  OR c.period_start IS DISTINCT FROM s.period_start OR c.period_end IS DISTINCT FROM s.period_end
  OR c.source_timezone IS DISTINCT FROM s.source_timezone
  OR c.billing_provider IS DISTINCT FROM s.billing_provider
  OR c.model_vendor IS DISTINCT FROM s.model_vendor OR c.raw_model_name IS DISTINCT FROM s.raw_model_name
  OR c.charge_category IS DISTINCT FROM s.charge_category OR c.cost_kind IS DISTINCT FROM s.cost_kind
	  OR c.source_scope IS DISTINCT FROM s.source_scope
	  OR c.provider_project_ref IS DISTINCT FROM s.provider_project_ref
	  OR c.currency IS DISTINCT FROM s.currency OR c.dimensions IS DISTINCT FROM s.dimensions`, wid, account); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE cost_entries c SET is_current=false FROM pg_temp.publish_candidates n
	 WHERE n.old_id IS NOT NULL AND c.workspace_id=$1 AND c.id=n.old_id
	`, wid); err != nil {
		return 0, err
	}
	var changed int
	err := tx.QueryRow(ctx, `WITH inserted AS (
	 INSERT INTO cost_entries(workspace_id,account_id,source_record_key,revision,source_batch_id,source_record_ref,period_start,period_end,source_timezone,billing_provider,model_vendor,raw_model_name,charge_category,cost_kind,source_scope,provider_project_ref,amount,currency,coverage_status,dimensions)
	 SELECT $1,$2,source_record_key,previous_revision+1,$3,source_record_ref,period_start,period_end,source_timezone,billing_provider,model_vendor,raw_model_name,charge_category,cost_kind,source_scope,provider_project_ref,amount,currency,coverage_status,dimensions
	 FROM pg_temp.publish_candidates ORDER BY row_number RETURNING 1
	) SELECT count(*) FROM inserted`, wid, account, batch).Scan(&changed)
	return changed, err
}
func jsonRef(i int) string { b, _ := json.Marshal(i); return "/entries/" + string(b) }
