package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/subscriptions"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
	"sort"
	"time"
)

func (w *Worker) buildInsights(ctx context.Context, wid string) error {
	tx, e := platform.TenantTx(ctx, w.DB, wid)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var code, status string
	var grace *time.Time
	e = tx.QueryRow(ctx, `SELECT s.plan_code,s.status,s.grace_period_until FROM subscriptions s JOIN workspaces w ON w.billing_account_id=s.billing_account_id WHERE w.id=$1`, wid).Scan(&code, &status, &grace)
	if e != nil {
		return e
	}
	p := subscriptions.Effective(code, status, grace, time.Now())
	rows, e := tx.Query(ctx, `SELECT id,kind,currency,amount::text FROM alert_rules WHERE workspace_id=$1 AND enabled ORDER BY created_at,id`, wid)
	if e != nil {
		return e
	}
	type rule struct{ ID, Kind, Currency, Amount string }
	rules := []rule{}
	for rows.Next() {
		var r rule
		if e = rows.Scan(&r.ID, &r.Kind, &r.Currency, &r.Amount); e != nil {
			rows.Close()
			return e
		}
		rules = append(rules, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	now := time.Now().UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, r := range rules {
		if r.Kind != "budget" && !p.Details {
			continue
		}
		if r.Kind == "budget" {
			var sum *string
			e = tx.QueryRow(ctx, `SELECT sum(amount)::text FROM cost_entries WHERE workspace_id=$1 AND is_current AND cost_kind='actual' AND currency=$2 AND period_start>=$3 AND period_end<=$4`, wid, r.Currency, month, month.AddDate(0, 1, 0)).Scan(&sum)
			if e != nil {
				return e
			}
			if sum == nil {
				continue
			}
			spent, _ := decimal.NewFromString(*sum)
			budget, _ := decimal.NewFromString(r.Amount)
			for _, threshold := range []int64{50, 80, 100} {
				if !p.Details && threshold != 100 {
					continue
				}
				if spent.GreaterThanOrEqual(budget.Mul(decimal.NewFromInt(threshold)).Div(decimal.NewFromInt(100))) {
					key := fmt.Sprintf("budget:%s:%s:%d", r.ID, month.Format("2006-01"), threshold)
					if e = w.insight(ctx, tx, wid, key, "budget", r.Currency, map[string]any{"actual": spent.String(), "budget": budget.String(), "threshold": threshold, "period_start": month, "period_end": month.AddDate(0, 1, 0), "assumptions": []string{"reported costs only; source data can be delayed"}}, true); e != nil {
						return e
					}
				}
			}
		}
		if r.Kind == "spike" {
			rs, e := tx.Query(ctx, `SELECT period_start::date,sum(amount)::text FROM cost_entries WHERE workspace_id=$1 AND is_current AND cost_kind='actual' AND currency=$2 AND coverage_status='complete' AND period_end-period_start=interval '1 day' AND period_start>=$3 AND period_end<=$4 GROUP BY period_start::date HAVING count(DISTINCT account_id)=(SELECT count(*) FROM provider_accounts WHERE workspace_id=$1 AND status<>'disconnected') ORDER BY period_start::date`, wid, r.Currency, now.Truncate(24*time.Hour).AddDate(0, 0, -8), now.Truncate(24*time.Hour))
			if e != nil {
				return e
			}
			values := []decimal.Decimal{}
			for rs.Next() {
				var day time.Time
				var amount string
				if e = rs.Scan(&day, &amount); e != nil {
					rs.Close()
					return e
				}
				v, _ := decimal.NewFromString(amount)
				values = append(values, v)
			}
			e = rs.Err()
			rs.Close()
			if e != nil {
				return e
			}
			if len(values) == 8 {
				last := values[7]
				baseline := values[:7]
				sort.Slice(baseline, func(i, j int) bool { return baseline[i].LessThan(baseline[j]) })
				median := baseline[3]
				minimum, _ := decimal.NewFromString(r.Amount)
				if last.GreaterThan(median.Mul(decimal.RequireFromString("1.5"))) && last.Sub(median).GreaterThan(minimum) {
					key := "spike:" + r.ID + ":" + now.Format("2006-01-02")
					if e = w.insight(ctx, tx, wid, key, "spike", r.Currency, map[string]any{"actual": last.String(), "baseline_median": median.String(), "minimum_increase": minimum.String(), "window_days": 8, "assumptions": []string{"eight complete UTC days; same currency and actual cost basis"}}, true); e != nil {
						return e
					}
				}
			}
		}
		if r.Kind == "sync_failure" {
			rs, e := tx.Query(ctx, `SELECT id,error_code,generation FROM provider_accounts WHERE workspace_id=$1 AND status='error'`, wid)
			if e != nil {
				return e
			}
			type failure struct {
				ID, Code   string
				Generation int
			}
			failures := []failure{}
			for rs.Next() {
				var f failure
				if e = rs.Scan(&f.ID, &f.Code, &f.Generation); e != nil {
					rs.Close()
					return e
				}
				failures = append(failures, f)
			}
			e = rs.Err()
			rs.Close()
			if e != nil {
				return e
			}
			for _, f := range failures {
				key := fmt.Sprintf("sync:%s:%d:%s", f.ID, f.Generation, now.Format("2006-01-02"))
				if e = w.insight(ctx, tx, wid, key, "sync_failure", r.Currency, map[string]any{"account_id": f.ID, "error_code": f.Code}, true); e != nil {
					return e
				}
			}
		}
	}
	return tx.Commit(ctx)
}
func (w *Worker) insight(ctx context.Context, tx pgx.Tx, wid, key, kind, currency string, evidence any, notify bool) error {
	data, e := json.Marshal(evidence)
	if e != nil {
		return e
	}
	id := uuid.NewString()
	tag, e := tx.Exec(ctx, `INSERT INTO insights(id,workspace_id,rule_key,kind,currency,evidence) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(workspace_id,rule_key) DO NOTHING`, id, wid, key, kind, currency, data)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 || !notify {
		return nil
	}
	rows, e := tx.Query(ctx, `SELECT u.email,u.locale FROM workspace_members m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 AND m.role='owner'`, wid)
	if e != nil {
		return e
	}
	type recipient struct{ Email, Locale string }
	recipients := []recipient{}
	for rows.Next() {
		var r recipient
		if e = rows.Scan(&r.Email, &r.Locale); e != nil {
			rows.Close()
			return e
		}
		recipients = append(recipients, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, r := range recipients {
		nid := uuid.NewString()
		tag, e = tx.Exec(ctx, `INSERT INTO notification_deliveries(id,workspace_id,dedupe_key,recipient,locale,template,payload) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(dedupe_key) DO NOTHING`, nid, wid, key+":"+r.Email, r.Email, r.Locale, kind, data)
		if e != nil {
			return e
		}
		if tag.RowsAffected() > 0 {
			if _, e = w.Queue.InsertTx(ctx, tx, tasks.Args{Task: "notification", WorkspaceID: wid, ResourceID: nid}, nil); e != nil {
				return e
			}
		}
	}
	return nil
}
func (w *Worker) notify(ctx context.Context, a tasks.Args) error {
	var email, locale, kind, state, slug string
	var payload []byte
	e := w.DB.QueryRow(ctx, `SELECT n.recipient,n.locale,n.template,n.state,n.payload,w.slug FROM notification_deliveries n JOIN workspaces w ON w.id=n.workspace_id JOIN workspace_members m ON m.workspace_id=w.id JOIN users u ON u.id=m.user_id AND u.email=n.recipient WHERE n.workspace_id=$1 AND n.id=$2 AND m.role='owner' AND w.deletion_requested_at IS NULL`, a.WorkspaceID, a.ResourceID).Scan(&email, &locale, &kind, &state, &payload, &slug)
	if e == pgx.ErrNoRows {
		return nil
	}
	if e != nil {
		return e
	}
	if state == "sent" {
		return nil
	}
	title := map[string]string{"budget": "Budget threshold reached", "spike": "Reported cost increase detected", "sync_failure": "A connection needs attention"}[kind]
	if locale == "zh-hans" {
		title = map[string]string{"budget": "预算达到提醒阈值", "spike": "报告费用出现增长", "sync_failure": "账单连接需要处理"}[kind]
	}
	if locale == "zh-hant" {
		title = map[string]string{"budget": "預算達到提醒門檻", "spike": "報告費用出現增長", "sync_failure": "帳單連線需要處理"}[kind]
	}
	body := title + "\n\n" + w.Config.AppURL + "/w/" + slug + "/insights"
	if w.Config.Env != "production" && w.Config.ResendKey == "" {
		_, e = w.DB.Exec(ctx, `UPDATE notification_deliveries SET state='development_skipped' WHERE workspace_id=$1 AND id=$2`, a.WorkspaceID, a.ResourceID)
		return e
	}
	if e = platform.SendMail(ctx, w.Config, email, "MinoDuck · "+title, body, a.ResourceID); e != nil {
		return e
	}
	_, e = w.DB.Exec(ctx, `UPDATE notification_deliveries SET state='sent',sent_at=now() WHERE workspace_id=$1 AND id=$2`, a.WorkspaceID, a.ResourceID)
	return e
}
