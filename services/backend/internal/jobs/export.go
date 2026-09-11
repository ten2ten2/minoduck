package jobs

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/subscriptions"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
	"time"
)

func (w *Worker) export(ctx context.Context, a tasks.Args) error {
	tx, e := platform.TenantTx(ctx, w.DB, a.WorkspaceID)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var raw []byte
	var locale, state, code, status string
	var grace *time.Time
	e = tx.QueryRow(ctx, `SELECT x.filters,x.locale,x.state,s.plan_code,s.status,s.grace_period_until FROM exports x JOIN workspaces w ON w.id=x.workspace_id JOIN subscriptions s ON s.billing_account_id=w.billing_account_id WHERE x.workspace_id=$1 AND x.id=$2 FOR UPDATE OF x FOR SHARE OF w`, a.WorkspaceID, a.ResourceID).Scan(&raw, &locale, &state, &code, &status, &grace)
	if e != nil {
		return e
	}
	if state == "ready" {
		return nil
	}
	if !subscriptions.Effective(code, status, grace, time.Now()).Export {
		return fmt.Errorf("UPGRADE_REQUIRED")
	}
	var f struct {
		Start, End time.Time
		Kind       string `json:"cost_kind"`
		Currency   string `json:"currency"`
		Provider   string `json:"provider"`
		Model      string `json:"model"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		return e
	}
	header := []string{"Period start", "Period end", "Billing provider", "Model", "Category", "Amount", "Currency", "Cost basis", "Source scope", "Revision"}
	if locale == "zh-hans" {
		header = []string{"周期开始", "周期结束", "计费平台", "模型", "费用类型", "金额", "币种", "成本口径", "来源范围", "版本"}
	}
	if locale == "zh-hant" {
		header = []string{"週期開始", "週期結束", "計費平台", "模型", "費用類型", "金額", "幣別", "成本口徑", "來源範圍", "版本"}
	}
	var b bytes.Buffer
	b.WriteString("\ufeff")
	writer := csv.NewWriter(&b)
	if e = writer.Write(header); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, `SELECT period_start,period_end,billing_provider,raw_model_name,charge_category,amount::text,currency,cost_kind,source_scope,revision::text FROM cost_entries WHERE workspace_id=$1 AND is_current AND period_start>=$2 AND period_end<=$3 AND cost_kind=$4 AND ($5='' OR currency=$5) AND ($6='' OR billing_provider=$6) AND ($7='' OR raw_model_name ILIKE '%'||$7||'%') ORDER BY period_start DESC,id DESC LIMIT 100001`, a.WorkspaceID, f.Start, f.End, f.Kind, f.Currency, f.Provider, f.Model)
	if e != nil {
		return e
	}
	count := 0
	for rows.Next() {
		var start, end time.Time
		values := make([]string, 10)
		if e = rows.Scan(&start, &end, &values[2], &values[3], &values[4], &values[5], &values[6], &values[7], &values[8], &values[9]); e != nil {
			rows.Close()
			return e
		}
		count++
		if count > 100000 {
			rows.Close()
			return fmt.Errorf("EXPORT_TOO_LARGE")
		}
		values[0] = start.Format(time.RFC3339)
		values[1] = end.Format(time.RFC3339)
		for i := range values {
			values[i] = ledger.SafeCell(values[i])
		}
		if e = writer.Write(values); e != nil {
			rows.Close()
			return e
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	writer.Flush()
	if e = writer.Error(); e != nil {
		return e
	}
	if b.Len() > 64*1024*1024 {
		return fmt.Errorf("EXPORT_TOO_LARGE")
	}
	object := a.WorkspaceID + "/" + a.ResourceID + ".csv"
	if e = platform.RegisterObjectIntent(ctx, w.DB, a.WorkspaceID, object, "export"); e != nil {
		return e
	}
	if e = w.Objects.Put(ctx, object, b.Bytes()); e != nil {
		_ = platform.CleanupObject(context.Background(), w.DB, w.Objects, a.WorkspaceID, object)
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE exports SET state='ready',object_key=$1,expires_at=now()+interval '24 hours' WHERE workspace_id=$2 AND id=$3`, object, a.WorkspaceID, a.ResourceID); e != nil {
		_ = platform.CleanupObject(context.Background(), w.DB, w.Objects, a.WorkspaceID, object)
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		// Commit errors are ambiguous: the database may already reference the
		// object. Leave the intent to let maintenance resolve linked vs orphaned.
		return e
	}
	return platform.ClearObjectIntent(ctx, w.DB, a.WorkspaceID, object)
}
