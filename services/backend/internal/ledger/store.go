package ledger

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"maps"
	"time"
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
	changed := 0
	for i, v := range entries {
		var oldID, amount, coverage, timezone, provider, vendor, model, category, kind, scope, project, currency string
		var oldStart, oldEnd time.Time
		var dimensions []byte
		var revision int
		e := tx.QueryRow(ctx, `SELECT id,amount::text,coverage_status,revision,period_start,period_end,source_timezone,billing_provider,model_vendor,raw_model_name,charge_category,cost_kind,source_scope,provider_project_ref,currency::text,dimensions FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND source_record_key=$3 AND is_current FOR UPDATE`, wid, account, v.Key).Scan(&oldID, &amount, &coverage, &revision, &oldStart, &oldEnd, &timezone, &provider, &vendor, &model, &category, &kind, &scope, &project, &currency, &dimensions)
		if e != nil && e != pgx.ErrNoRows {
			return 0, e
		}
		if e == pgx.ErrNoRows {
			if e = tx.QueryRow(ctx, `SELECT coalesce(max(revision),0) FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND source_record_key=$3`, wid, account, v.Key).Scan(&revision); e != nil {
				return 0, e
			}
		}
		if oldID != "" {
			oldAmount, _ := Amount(amount)
			nextAmount, _ := Amount(v.Amount)
			oldDimensions := map[string]string{}
			if e = json.Unmarshal(dimensions, &oldDimensions); e != nil {
				return 0, e
			}
			if oldAmount.Equal(nextAmount) && coverage == v.Coverage && oldStart.Equal(v.Start) && oldEnd.Equal(v.End) && timezone == v.Timezone && provider == v.Provider && vendor == v.Vendor && model == v.Model && category == v.Category && kind == v.Kind && scope == v.Scope && project == v.Project && currency == v.Currency && maps.Equal(oldDimensions, v.Dimensions) {
				continue
			}
			if _, e = tx.Exec(ctx, `UPDATE cost_entries SET is_current=false WHERE workspace_id=$1 AND id=$2`, wid, oldID); e != nil {
				return 0, e
			}
		}
		dimensions, _ = json.Marshal(v.Dimensions)
		if v.Dimensions == nil {
			dimensions = []byte(`{}`)
		}
		ref := v.SourceRef
		if ref == "" {
			ref = jsonRef(i)
		}
		_, e = tx.Exec(ctx, `INSERT INTO cost_entries(workspace_id,account_id,source_record_key,revision,source_batch_id,source_record_ref,period_start,period_end,source_timezone,billing_provider,model_vendor,raw_model_name,charge_category,cost_kind,source_scope,provider_project_ref,amount,currency,coverage_status,dimensions) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, wid, account, v.Key, revision+1, batch, ref, v.Start, v.End, v.Timezone, v.Provider, v.Vendor, v.Model, v.Category, v.Kind, v.Scope, v.Project, v.Amount, v.Currency, v.Coverage, dimensions)
		if e != nil {
			return 0, e
		}
		changed++
	}
	return changed, nil
}
func jsonRef(i int) string { b, _ := json.Marshal(i); return "/entries/" + string(b) }
