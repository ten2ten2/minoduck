package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"strings"
	"time"
)

const invoiceJSON = `to_jsonb(i)||jsonb_build_object('amount',i.amount::text,'adjustment',i.adjustment::text)`
const runJSON = `to_jsonb(r)||jsonb_build_object('expected',r.expected::text,'billed',r.billed::text,'difference',r.difference::text,'tolerance',r.tolerance::text)`

func (s *Server) invoices(c *gin.Context, tx pgx.Tx) (any, error) {
	cutoff, e := s.historyCutoff(c, tx)
	if e != nil {
		return nil, e
	}
	return platform.JSONRows(c.Request.Context(), tx, `SELECT `+invoiceJSON+` FROM invoices i WHERE workspace_id=$1 AND period_start>=$2 ORDER BY created_at DESC,id DESC LIMIT 100`, c.Param("wid"), cutoff)
}
func (s *Server) invoice(c *gin.Context, tx pgx.Tx) (any, error) {
	cutoff, e := s.historyCutoff(c, tx)
	if e != nil {
		return nil, e
	}
	var b []byte
	e = tx.QueryRow(c.Request.Context(), `SELECT `+invoiceJSON+` FROM invoices i WHERE workspace_id=$1 AND id=$2 AND period_start>=$3`, c.Param("wid"), c.Param("iid"), cutoff).Scan(&b)
	return json.RawMessage(b), e
}
func (s *Server) createInvoice(c *gin.Context, tx pgx.Tx) (any, error) {
	var in struct {
		AccountID  string `json:"account_id"`
		Reference  string `json:"reference"`
		Currency   string `json:"currency"`
		Start      string `json:"period_start"`
		End        string `json:"period_end"`
		Amount     string `json:"amount"`
		Adjustment string `json:"adjustment"`
		Scope      string `json:"source_scope"`
		Confirmed  bool   `json:"coverage_confirmed"`
		Evidence   string `json:"evidence_note"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	if _, e := uuid.Parse(in.AccountID); e != nil {
		return nil, bad("INVALID_CONNECTION")
	}
	if !ledger.ValidCurrency(in.Currency) || strings.TrimSpace(in.Reference) == "" || len(in.Reference) > 200 || len(in.Scope) == 0 || len(in.Scope) > 120 || strings.TrimSpace(in.Evidence) == "" || len(in.Evidence) > 4000 {
		return nil, bad("INVALID_INVOICE")
	}
	start, e1 := time.Parse("2006-01-02", in.Start)
	end, e2 := time.Parse("2006-01-02", in.End)
	if e1 != nil || e2 != nil || !end.After(start) {
		return nil, bad("INVALID_PERIOD")
	}
	if _, e := ledger.Amount(in.Amount); e != nil {
		return nil, bad("INVALID_AMOUNT")
	}
	if in.Adjustment == "" {
		in.Adjustment = "0"
	}
	if _, e := ledger.Amount(in.Adjustment); e != nil {
		return nil, bad("INVALID_AMOUNT")
	}
	var exists bool
	e := tx.QueryRow(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM provider_accounts WHERE workspace_id=$1 AND id=$2)`, c.Param("wid"), in.AccountID).Scan(&exists)
	if e != nil {
		return nil, e
	}
	if !exists {
		return nil, pgx.ErrNoRows
	}
	var id string
	e = tx.QueryRow(c.Request.Context(), `INSERT INTO invoices(workspace_id,account_id,reference,currency,period_start,period_end,amount,adjustment,source_scope,coverage_confirmed,evidence_note) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT DO NOTHING RETURNING id`, c.Param("wid"), in.AccountID, in.Reference, in.Currency, start, end, in.Amount, in.Adjustment, in.Scope, in.Confirmed, in.Evidence).Scan(&id)
	if e == pgx.ErrNoRows {
		return nil, APIError{"INVOICE_ALREADY_EXISTS", 409}
	}
	if e != nil {
		return nil, e
	}
	c.Set("response_status", 201)
	return gin.H{"id": id}, platform.Audit(c.Request.Context(), tx, c.Param("wid"), session(c).UserID, "invoice.created", id, gin.H{})
}
func (s *Server) reconcile(c *gin.Context, tx pgx.Tx) (any, error) {
	var in struct {
		InvoiceID string `json:"invoice_id"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	if _, e := uuid.Parse(in.InvoiceID); e != nil {
		return nil, bad("INVALID_INVOICE")
	}
	var aid, currency, billed, adjustment, scope string
	var start, end time.Time
	var confirmed bool
	ctx := c.Request.Context()
	e := tx.QueryRow(ctx, `SELECT account_id,currency,amount::text,adjustment::text,source_scope,period_start,period_end,coverage_confirmed FROM invoices WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Param("wid"), in.InvoiceID).Scan(&aid, &currency, &billed, &adjustment, &scope, &start, &end, &confirmed)
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
	var actual *string
	var rows, partial int
	e = tx.QueryRow(ctx, `SELECT sum(amount)::text,count(*),count(*) FILTER(WHERE coverage_status<>'complete') FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND is_current AND cost_kind='actual' AND currency=$3 AND period_start>=$4 AND period_end<=$5 AND source_scope=$6`, c.Param("wid"), aid, currency, start, end, scope).Scan(&actual, &rows, &partial)
	if e != nil {
		return nil, e
	}
	var boundary bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND is_current AND cost_kind='actual' AND currency=$3 AND source_scope=$6 AND period_start<$5 AND period_end>$4 AND (period_start<$4 OR period_end>$5))`, c.Param("wid"), aid, currency, start, end, scope).Scan(&boundary)
	if e != nil {
		return nil, e
	}
	expected := ""
	if actual != nil {
		a, _ := decimal.NewFromString(*actual)
		adj, _ := decimal.NewFromString(adjustment)
		expected = a.Add(adj).String()
	}
	complete, e := ledger.CompleteScope(ctx, tx, c.Param("wid"), aid, currency, scope, start, end)
	if e != nil {
		return nil, e
	}
	status, diff, e := ledger.Match(expected, billed, "0.01", confirmed && complete && partial == 0 && !boundary)
	if e != nil {
		return nil, e
	}
	if rows == 0 {
		status = "pending_source"
		diff = ""
	}
	var version int
	e = tx.QueryRow(ctx, `SELECT coalesce(max(run_version),0)+1 FROM reconciliation_runs WHERE workspace_id=$1 AND invoice_id=$2`, c.Param("wid"), in.InvoiceID).Scan(&version)
	if e != nil {
		return nil, e
	}
	refs, e := platform.JSONRows(ctx, tx, `SELECT json_build_object('entry_id',id,'revision',revision,'batch_id',source_batch_id) FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND is_current AND cost_kind='actual' AND currency=$3 AND period_start>=$4 AND period_end<=$5 AND source_scope=$6 ORDER BY id`, c.Param("wid"), aid, currency, start, end, scope)
	if e != nil {
		return nil, e
	}
	evidence, _ := json.Marshal(gin.H{"entry_refs": refs, "adjustment": adjustment, "coverage_confirmed": confirmed, "partial_entries": partial, "boundary_overlap": boundary, "period_start": start, "period_end": end, "source_scope": scope, "invoice_id": in.InvoiceID, "rule": "actual + explicit_adjustment vs billed; original currency; absolute tolerance 0.01"})
	id := uuid.NewString()
	_, e = tx.Exec(ctx, `INSERT INTO reconciliation_runs(id,workspace_id,invoice_id,run_version,level,expected,billed,difference,currency,match_status,evidence,account_id,period_start,period_end) VALUES($1,$2,$3,$4,'L2',nullif($5,'')::numeric,$6,nullif($7,'')::numeric,$8,$9,$10,$11,$12,$13)`, id, c.Param("wid"), in.InvoiceID, version, expected, billed, diff, currency, status, evidence, aid, start, end)
	if e != nil {
		return nil, e
	}
	c.Set("response_status", 201)
	return gin.H{"id": id, "run_version": version, "match_status": status, "difference": nilIfEmpty(diff), "currency": currency}, platform.Audit(ctx, tx, c.Param("wid"), session(c).UserID, "reconciliation.created", id, gin.H{"run_version": version})
}
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func (s *Server) reconciliations(c *gin.Context, tx pgx.Tx) (any, error) {
	p, e := s.plan(c, tx)
	if e != nil {
		return nil, e
	}
	if !p.Details {
		return platform.JSONRows(c.Request.Context(), tx, `SELECT json_build_object('id',r.id,'invoice_id',r.invoice_id,'match_status',r.match_status,'difference',r.difference::text,'currency',r.currency,'handling_status',r.handling_status,'run_version',r.run_version,'created_at',r.created_at,'details_locked',true) FROM reconciliation_runs r WHERE workspace_id=$1 AND r.period_start>=$2 AND run_version=(SELECT max(run_version) FROM reconciliation_runs n WHERE n.workspace_id=r.workspace_id AND n.level=r.level AND n.invoice_id IS NOT DISTINCT FROM r.invoice_id AND n.account_id=r.account_id AND n.period_start=r.period_start AND n.period_end=r.period_end AND n.currency=r.currency) ORDER BY created_at DESC,id DESC LIMIT 100`, c.Param("wid"), time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -p.RetentionDays))
	}
	return platform.JSONRows(c.Request.Context(), tx, `SELECT `+runJSON+` FROM reconciliation_runs r WHERE workspace_id=$1 AND period_start>=$2 ORDER BY created_at DESC,id DESC LIMIT 100`, c.Param("wid"), time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -p.RetentionDays))
}
func (s *Server) reconciliation(c *gin.Context, tx pgx.Tx) (any, error) {
	cutoff, e := s.historyCutoff(c, tx)
	if e != nil {
		return nil, e
	}
	if e := s.paid(c, tx); e != nil {
		return nil, e
	}
	var b []byte
	e = tx.QueryRow(c.Request.Context(), `SELECT `+runJSON+` FROM reconciliation_runs r WHERE workspace_id=$1 AND id=$2 AND period_start>=$3`, c.Param("wid"), c.Param("rid"), cutoff).Scan(&b)
	return json.RawMessage(b), e
}
func (s *Server) handleReconciliation(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.paid(c, tx); e != nil {
		return nil, e
	}
	var in struct {
		Status string `json:"handling_status"`
		Note   string `json:"note"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	if (in.Status != "explained" && in.Status != "ignored" && in.Status != "open") || strings.TrimSpace(in.Note) == "" || len(in.Note) > 4000 {
		return nil, bad("EXPLANATION_REQUIRED")
	}
	result, e := tx.Exec(c.Request.Context(), `UPDATE reconciliation_runs SET handling_status=$1,handling_note=$2 WHERE workspace_id=$3 AND id=$4`, in.Status, in.Note, c.Param("wid"), c.Param("rid"))
	if e != nil {
		return nil, e
	}
	if result.RowsAffected() == 0 {
		return nil, pgx.ErrNoRows
	}
	return gin.H{"status": "saved"}, platform.Audit(c.Request.Context(), tx, c.Param("wid"), session(c).UserID, "reconciliation.handled", c.Param("rid"), in)
}
