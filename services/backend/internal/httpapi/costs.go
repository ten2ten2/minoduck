package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"strconv"
	"time"
)

type costFilter struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Kind     string    `json:"cost_kind"`
	Currency string    `json:"currency"`
	Provider string    `json:"provider"`
	Model    string    `json:"model"`
	Page     int       `json:"page"`
	Size     int       `json:"page_size"`
}

func (s *Server) filter(c *gin.Context, tx pgx.Tx) (costFilter, error) {
	now := time.Now().UTC()
	f := costFilter{Start: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC), End: now.Truncate(24*time.Hour).AddDate(0, 0, 1), Kind: c.DefaultQuery("cost_kind", "actual"), Currency: c.Query("currency"), Provider: c.Query("provider"), Model: c.Query("model"), Page: 1, Size: 50}
	for _, p := range []struct {
		k string
		t *time.Time
	}{{"start", &f.Start}, {"end", &f.End}} {
		if v := c.Query(p.k); v != "" {
			t, e := time.Parse("2006-01-02", v)
			if e != nil {
				return f, bad("INVALID_PERIOD")
			}
			*p.t = t
		}
	}
	if f.Kind != "actual" && f.Kind != "billed" && f.Kind != "calculated" && f.Kind != "estimated" {
		return f, bad("INVALID_COST_KIND")
	}
	if f.Currency != "" && !ledger.ValidCurrency(f.Currency) {
		return f, bad("INVALID_CURRENCY")
	}
	if !f.End.After(f.Start) || f.End.Sub(f.Start) > 731*24*time.Hour {
		return f, bad("INVALID_PERIOD")
	}
	plan, e := s.plan(c, tx)
	if e != nil {
		return f, e
	}
	if f.Start.Before(now.Truncate(24*time.Hour).AddDate(0, 0, -plan.RetentionDays)) {
		return f, APIError{"HISTORY_LIMIT", 402}
	}
	if v := c.Query("page"); v != "" {
		f.Page, e = strconv.Atoi(v)
		if e != nil || f.Page < 1 || f.Page > 100000 {
			return f, bad("INVALID_PAGE")
		}
	}
	if v := c.Query("page_size"); v != "" {
		f.Size, e = strconv.Atoi(v)
		if e != nil || f.Size < 1 || f.Size > 100 {
			return f, bad("INVALID_PAGE")
		}
	}
	return f, nil
}

const costWhere = `e.workspace_id=$1 AND e.is_current AND e.period_start >= $2 AND e.period_end <= $3 AND e.cost_kind=$4 AND ($5='' OR e.currency=$5) AND ($6='' OR e.billing_provider=$6) AND ($7='' OR e.raw_model_name ILIKE '%'||$7||'%')`

func (f costFilter) args(wid string) []any {
	return []any{wid, f.Start, f.End, f.Kind, f.Currency, f.Provider, f.Model}
}

const costJSON = `json_build_object('id',e.id,'account_id',e.account_id,'period_start',e.period_start,'period_end',e.period_end,'billing_provider',e.billing_provider,'model_vendor',e.model_vendor,'model',e.raw_model_name,'charge_category',e.charge_category,'amount',e.amount::text,'currency',e.currency,'cost_kind',e.cost_kind,'source_scope',e.source_scope,'source_batch_id',e.source_batch_id,'coverage',e.coverage_status,'revision',e.revision,'price_basis',e.price_basis,'dimensions',e.dimensions)`

func (s *Server) costs(c *gin.Context, tx pgx.Tx) (any, error) {
	f, e := s.filter(c, tx)
	if e != nil {
		return nil, e
	}
	args := f.args(c.Param("wid"))
	var count int
	e = tx.QueryRow(c.Request.Context(), `SELECT count(*) FROM cost_entries e WHERE `+costWhere, args...).Scan(&count)
	if e != nil {
		return nil, e
	}
	args = append(args, f.Size, (f.Page-1)*f.Size)
	rows, e := platform.JSONRows(c.Request.Context(), tx, `SELECT `+costJSON+` FROM cost_entries e WHERE `+costWhere+` ORDER BY e.period_start DESC,e.id DESC LIMIT $8 OFFSET $9`, args...)
	return gin.H{"items": rows, "total": count, "page": f.Page, "page_size": f.Size, "period": f}, e
}
func (s *Server) costDetail(c *gin.Context, tx pgx.Tx) (any, error) {
	var key, aid string
	var b []byte
	var start time.Time
	e := tx.QueryRow(c.Request.Context(), `SELECT source_record_key,account_id,period_start,`+costJSON+` FROM cost_entries e WHERE workspace_id=$1 AND id=$2`, c.Param("wid"), c.Param("eid")).Scan(&key, &aid, &start, &b)
	if e != nil {
		return nil, e
	}
	p, e := s.plan(c, tx)
	if e != nil {
		return nil, e
	}
	if start.Before(time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -p.RetentionDays)) {
		return nil, APIError{"HISTORY_LIMIT", 402}
	}
	history, e := platform.JSONRows(c.Request.Context(), tx, `SELECT json_build_object('revision',revision,'amount',amount::text,'currency',currency,'is_current',is_current,'source_batch_id',source_batch_id,'source_record_ref',source_record_ref,'ingested_at',ingested_at) FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND source_record_key=$3 ORDER BY revision DESC`, c.Param("wid"), aid, key)
	return gin.H{"entry": json.RawMessage(b), "history": history}, e
}
func (s *Server) overview(c *gin.Context, tx pgx.Tx) (any, error) {
	f, e := s.filter(c, tx)
	if e != nil {
		return nil, e
	}
	args := f.args(c.Param("wid"))
	ctx := c.Request.Context()
	totals, e := platform.JSONRows(ctx, tx, `SELECT json_build_object('currency',e.currency,'amount',sum(e.amount)::text,'entry_count',count(*),'partial_entries',count(*) FILTER(WHERE e.coverage_status<>'complete')) FROM cost_entries e WHERE `+costWhere+` GROUP BY currency ORDER BY currency`, args...)
	if e != nil {
		return nil, e
	}
	trend, e := platform.JSONRows(ctx, tx, `SELECT json_build_object('date',e.period_start,'currency',e.currency,'amount',sum(e.amount)::text) FROM cost_entries e WHERE `+costWhere+` GROUP BY e.period_start,e.currency ORDER BY e.period_start,e.currency`, args...)
	if e != nil {
		return nil, e
	}
	providers, e := platform.JSONRows(ctx, tx, `SELECT json_build_object('provider',e.billing_provider,'currency',e.currency,'amount',sum(e.amount)::text) FROM cost_entries e WHERE `+costWhere+` GROUP BY e.billing_provider,e.currency ORDER BY sum(e.amount) DESC`, args...)
	if e != nil {
		return nil, e
	}
	models, e := platform.JSONRows(ctx, tx, `SELECT json_build_object('model',e.raw_model_name,'currency',e.currency,'amount',sum(e.amount)::text) FROM cost_entries e WHERE `+costWhere+` GROUP BY e.raw_model_name,e.currency ORDER BY sum(e.amount) DESC LIMIT 10`, args...)
	if e != nil {
		return nil, e
	}
	sources, e := platform.JSONRows(ctx, tx, `SELECT `+connectionJSON+` FROM provider_accounts a WHERE workspace_id=$1 ORDER BY created_at,id`, c.Param("wid"))
	if e != nil {
		return nil, e
	}
	billed, e := platform.JSONRows(ctx, tx, `SELECT json_build_object('currency',currency,'amount',sum(amount)::text,'invoice_count',count(*)) FROM invoices WHERE workspace_id=$1 AND period_start>=$2 AND period_end<=$3 GROUP BY currency`, c.Param("wid"), f.Start, f.End)
	if e != nil {
		return nil, e
	}
	var issues, insightCount int
	e = tx.QueryRow(ctx, `SELECT count(*) FROM reconciliation_runs r WHERE workspace_id=$1 AND handling_status='open' AND match_status='mismatch' AND run_version=(SELECT max(run_version) FROM reconciliation_runs n WHERE n.workspace_id=r.workspace_id AND n.level=r.level AND n.invoice_id IS NOT DISTINCT FROM r.invoice_id AND n.account_id=r.account_id AND n.period_start=r.period_start AND n.period_end=r.period_end AND n.currency=r.currency)`, c.Param("wid")).Scan(&issues)
	if e != nil {
		return nil, e
	}
	e = tx.QueryRow(ctx, `SELECT count(*) FROM insights WHERE workspace_id=$1 AND state='open'`, c.Param("wid")).Scan(&insightCount)
	if e != nil {
		return nil, e
	}
	state := "partial"
	if len(totals) == 0 {
		state = "empty"
	}
	// Completeness is conservative: matching amounts do not establish source coverage.
	return gin.H{"totals": totals, "trend": trend, "providers": providers, "models": models, "sources": sources, "billed": billed, "unexplained_count": issues, "insight_count": insightCount, "period": f, "cost_kind": f.Kind, "coverage": state, "warnings": []string{"SOURCE_COVERAGE_NOT_GUARANTEED", "NO_CURRENCY_CONVERSION"}}, nil
}

func (s *Server) historyCutoff(c *gin.Context, tx pgx.Tx) (time.Time, error) {
	p, e := s.plan(c, tx)
	return time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -p.RetentionDays), e
}
