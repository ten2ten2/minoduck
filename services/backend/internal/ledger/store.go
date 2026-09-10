package ledger

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

// CompleteScope verifies the union of complete source windows, including native
// zero-usage days. A user checkbox never substitutes for missing source data.
func CompleteScope(ctx context.Context, tx pgx.Tx, wid, account, currency, scope string, start, end time.Time) (bool, error) {
	var complete bool
	e := tx.QueryRow(ctx, `SELECT coalesce(range_agg(span) @> tstzrange($5,$6,'[)'),false) FROM (
 SELECT tstzrange(period_start,period_end,'[)') AS span FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND is_current AND cost_kind='actual' AND currency=$3 AND source_scope=$4 AND coverage_status='complete' AND period_start<$6 AND period_end>$5
 UNION ALL SELECT tstzrange(period_start,period_end,'[)') FROM sync_runs WHERE workspace_id=$1 AND account_id=$2 AND state='succeeded' AND $4='native-cost' AND period_start<$6 AND period_end>$5
 ) windows`, wid, account, currency, scope, start, end).Scan(&complete)
	return complete, e
}

// Publish runs under an account row lock in the caller's tenant transaction.
func Publish(ctx context.Context, tx pgx.Tx, wid, account, batch string, entries []Entry) (int, error) {
	changed := 0
	for i, v := range entries {
		var oldID, amount, coverage string
		var revision int
		e := tx.QueryRow(ctx, `SELECT id,amount::text,coverage_status,revision FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND source_record_key=$3 AND is_current FOR UPDATE`, wid, account, v.Key).Scan(&oldID, &amount, &coverage, &revision)
		if e != nil && e != pgx.ErrNoRows {
			return 0, e
		}
		if e == pgx.ErrNoRows {
			if e = tx.QueryRow(ctx, `SELECT coalesce(max(revision),0) FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND source_record_key=$3`, wid, account, v.Key).Scan(&revision); e != nil {
				return 0, e
			}
		}
		if oldID != "" {
			old, _ := Amount(amount)
			next, _ := Amount(v.Amount)
			if old.Equal(next) && coverage == v.Coverage {
				continue
			}
			if _, e = tx.Exec(ctx, `UPDATE cost_entries SET is_current=false WHERE workspace_id=$1 AND id=$2`, wid, oldID); e != nil {
				return 0, e
			}
		}
		dimensions, _ := json.Marshal(v.Dimensions)
		if v.Dimensions == nil {
			dimensions = []byte(`{}`)
		}
		_, e = tx.Exec(ctx, `INSERT INTO cost_entries(workspace_id,account_id,source_record_key,revision,source_batch_id,source_record_ref,period_start,period_end,source_timezone,billing_provider,model_vendor,raw_model_name,charge_category,cost_kind,source_scope,provider_project_ref,amount,currency,coverage_status,dimensions) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, wid, account, v.Key, revision+1, batch, jsonRef(i), v.Start, v.End, v.Timezone, v.Provider, v.Vendor, v.Model, v.Category, v.Kind, v.Scope, v.Project, v.Amount, v.Currency, v.Coverage, dimensions)
		if e != nil {
			return 0, e
		}
		changed++
	}
	return changed, nil
}
func jsonRef(i int) string { b, _ := json.Marshal(i); return "/entries/" + string(b) }
