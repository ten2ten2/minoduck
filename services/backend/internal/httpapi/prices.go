package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"net/url"
	"strings"
	"time"
)

func (s *Server) prices(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.paid(c, tx); e != nil {
		return nil, e
	}
	return platform.JSONRows(c.Request.Context(), tx, `SELECT to_jsonb(p)||jsonb_build_object('input_per_million',input_per_million::text,'output_per_million',output_per_million::text,'cache_read_per_million',cache_read_per_million::text,'cache_write_5m_per_million',cache_write_5m_per_million::text,'cache_write_1h_per_million',cache_write_1h_per_million::text) FROM price_versions p WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 200`, c.Param("wid"))
}
func (s *Server) createPrice(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.paid(c, tx); e != nil {
		return nil, e
	}
	var in struct {
		Provider    string `json:"billing_provider"`
		Model       string `json:"model_version"`
		Currency    string `json:"currency"`
		From        string `json:"effective_from"`
		To          string `json:"effective_to"`
		Tier        string `json:"service_tier"`
		Region      string `json:"region"`
		Route       string `json:"route"`
		Input       string `json:"input_per_million"`
		Output      string `json:"output_per_million"`
		Read        string `json:"cache_read_per_million"`
		Write5      string `json:"cache_write_5m_per_million"`
		Write1      string `json:"cache_write_1h_per_million"`
		Basis       string `json:"price_basis"`
		URL         string `json:"evidence_url"`
		Assumptions string `json:"assumptions"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	from, e1 := time.Parse("2006-01-02", in.From)
	to, e2 := time.Parse("2006-01-02", in.To)
	u, e := url.Parse(in.URL)
	if e1 != nil || e2 != nil || !to.After(from) || e != nil || u.Scheme != "https" || u.Host == "" || in.Model == "" || in.Provider == "" || !ledger.ValidCurrency(in.Currency) || (in.Basis != "public" && in.Basis != "contract") || strings.TrimSpace(in.Assumptions) == "" || len(in.Assumptions) > 4000 {
		return nil, bad("INVALID_RATE")
	}
	for _, amount := range []string{in.Input, in.Output, in.Read, in.Write5, in.Write1} {
		v, e := ledger.Amount(amount)
		if e != nil || v.IsNegative() {
			return nil, bad("INVALID_RATE")
		}
	}
	if _, e = tx.Exec(c.Request.Context(), `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, c.Param("wid")+in.Provider+in.Model+in.Tier+in.Region+in.Route); e != nil {
		return nil, e
	}
	var overlap bool
	e = tx.QueryRow(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM price_versions WHERE workspace_id=$1 AND billing_provider=$2 AND model_version=$3 AND currency=$4 AND service_tier=$5 AND region=$6 AND route=$7 AND effective_from<$9 AND effective_to>$8)`, c.Param("wid"), in.Provider, in.Model, in.Currency, in.Tier, in.Region, in.Route, from, to).Scan(&overlap)
	if e != nil {
		return nil, e
	}
	if overlap {
		return nil, APIError{"PRICE_WINDOW_OVERLAP", 409}
	}
	id := uuid.NewString()
	_, e = tx.Exec(c.Request.Context(), `INSERT INTO price_versions(id,workspace_id,billing_provider,model_version,currency,effective_from,effective_to,service_tier,region,route,input_per_million,output_per_million,cache_read_per_million,cache_write_5m_per_million,cache_write_1h_per_million,price_basis,evidence_url,assumptions) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, id, c.Param("wid"), in.Provider, in.Model, in.Currency, from, to, in.Tier, in.Region, in.Route, in.Input, in.Output, in.Read, in.Write5, in.Write1, in.Basis, in.URL, in.Assumptions)
	if e != nil {
		return nil, e
	}
	c.Set("response_status", 201)
	return gin.H{"id": id}, platform.Audit(c.Request.Context(), tx, c.Param("wid"), session(c).UserID, "price_version.created", id, gin.H{"evidence_url": in.URL})
}
func (s *Server) reconcileUsage(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.paid(c, tx); e != nil {
		return nil, e
	}
	var in struct {
		Account   string `json:"account_id"`
		Start     string `json:"period_start"`
		End       string `json:"period_end"`
		Currency  string `json:"currency"`
		Scope     string `json:"source_scope"`
		Confirmed bool   `json:"coverage_confirmed"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	start, e1 := time.Parse("2006-01-02", in.Start)
	end, e2 := time.Parse("2006-01-02", in.End)
	if _, e := uuid.Parse(in.Account); e != nil || e1 != nil || e2 != nil || !end.After(start) || !ledger.ValidCurrency(in.Currency) {
		return nil, bad("INVALID_PERIOD")
	}
	cutoff, historyErr := s.historyCutoff(c, tx)
	if historyErr != nil {
		return nil, historyErr
	}
	if start.Before(cutoff) {
		return nil, APIError{"HISTORY_LIMIT", 402}
	}
	ctx := c.Request.Context()
	wid := c.Param("wid")
	var provider string
	e := tx.QueryRow(ctx, `SELECT provider FROM provider_accounts WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, wid, in.Account).Scan(&provider)
	if e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT id,model,period_start,period_end,metrics,dimensions,source_batch_id FROM usage_buckets WHERE workspace_id=$1 AND account_id=$2 AND period_start>=$3 AND period_end<=$4 ORDER BY period_start,id`, wid, in.Account, start, end)
	if e != nil {
		return nil, e
	}
	type bucket struct {
		ID, Model, Batch    string
		Start, End          time.Time
		Metrics, Dimensions []byte
	}
	buckets := []bucket{}
	for rows.Next() {
		var b bucket
		if e = rows.Scan(&b.ID, &b.Model, &b.Start, &b.End, &b.Metrics, &b.Dimensions, &b.Batch); e != nil {
			rows.Close()
			return nil, e
		}
		buckets = append(buckets, b)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	calculated := decimal.Zero
	evidence := []gin.H{}
	missing := []string{}
	for _, b := range buckets {
		var m, d map[string]string
		if e = json.Unmarshal(b.Metrics, &m); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(b.Dimensions, &d); e != nil {
			return nil, e
		}
		// Anthropic fast mode has separate premium pricing. Until price versions
		// model speed explicitly, never apply a standard rate to fast usage. New
		// Anthropic usage must also carry an inference geography so US-only pricing
		// cannot be silently merged with global routing.
		if provider == "anthropic" && (d["region"] == "" || d["speed"] != "standard") {
			missing = append(missing, b.ID)
			continue
		}
		var rate ledger.RateCard
		var priceID, basis, ref string
		e = tx.QueryRow(ctx, `SELECT id,input_per_million::text,output_per_million::text,cache_read_per_million::text,cache_write_5m_per_million::text,cache_write_1h_per_million::text,price_basis,evidence_url FROM price_versions WHERE workspace_id=$1 AND billing_provider=$2 AND model_version=$3 AND currency=$4 AND effective_from<=$5 AND effective_to>=$6 AND service_tier=$7 AND region=$8 AND route=$9`, wid, provider, b.Model, in.Currency, b.Start, b.End, d["service_tier"], d["region"], d["endpoint_id"]).Scan(&priceID, &rate.Input, &rate.Output, &rate.Read, &rate.Write5m, &rate.Write1h, &basis, &ref)
		if e == pgx.ErrNoRows {
			missing = append(missing, b.ID)
			continue
		}
		if e != nil {
			return nil, e
		}
		amount, e := ledger.CalculateTextCost(m, rate)
		if e != nil {
			missing = append(missing, b.ID)
			continue
		}
		v, _ := decimal.NewFromString(amount)
		calculated = calculated.Add(v)
		evidence = append(evidence, gin.H{"usage_id": b.ID, "source_batch_id": b.Batch, "metrics": m, "dimensions": d, "price_version_id": priceID, "price_basis": basis, "reference": ref, "amount": amount})
	}
	var actual *string
	var partial int
	e = tx.QueryRow(ctx, `SELECT sum(amount)::text,count(*) FILTER(WHERE coverage_status<>'complete') FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND is_current AND cost_kind='actual' AND currency=$3 AND period_start>=$4 AND period_end<=$5 AND source_scope=$6`, wid, in.Account, in.Currency, start, end, in.Scope).Scan(&actual, &partial)
	if e != nil {
		return nil, e
	}
	expected := ""
	if len(buckets) > 0 && len(missing) == 0 {
		expected = calculated.String()
	}
	reported := ""
	if actual != nil {
		reported = *actual
	}
	complete, e := ledger.CompleteScope(ctx, tx, wid, in.Account, in.Currency, in.Scope, start, end)
	if e != nil {
		return nil, e
	}
	status, diff, e := ledger.Match(expected, reported, "0.01", in.Confirmed && complete && partial == 0)
	if e != nil {
		return nil, e
	}
	if expected == "" || reported == "" {
		status = "pending_source"
		diff = ""
	}

	var version int
	e = tx.QueryRow(ctx, `SELECT coalesce(max(run_version),0)+1 FROM reconciliation_runs WHERE workspace_id=$1 AND account_id=$2 AND period_start=$3 AND period_end=$4 AND currency=$5 AND level='L1'`, wid, in.Account, start, end, in.Currency).Scan(&version)
	if e != nil {
		return nil, e
	}
	id := uuid.NewString()
	details, _ := json.Marshal(gin.H{"price_evidence": evidence, "missing_usage_or_prices": missing, "period_start": start, "period_end": end, "source_scope": in.Scope, "coverage_confirmed": in.Confirmed, "actual_missing": actual == nil, "text_only": true, "assumptions": []string{"L1 covers text usage only; non-text, tax, fee and priority charges must be excluded from the declared comparable scope", "Anthropic fast-mode usage stays pending until price versions model speed explicitly"}})
	_, e = tx.Exec(ctx, `INSERT INTO reconciliation_runs(id,workspace_id,account_id,period_start,period_end,run_version,level,expected,billed,difference,currency,match_status,evidence) VALUES($1,$2,$3,$4,$5,$6,'L1',nullif($7,'')::numeric,nullif($8,'')::numeric,nullif($9,'')::numeric,$10,$11,$12)`, id, wid, in.Account, start, end, version, expected, reported, diff, in.Currency, status, details)
	if e != nil {
		return nil, e
	}
	c.Set("response_status", 201)
	return gin.H{"id": id, "match_status": status, "difference": nilIfEmpty(diff), "currency": in.Currency}, nil
}
