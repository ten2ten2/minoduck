package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"time"
)

func (s *Server) comparePrices(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.paid(c, tx); e != nil {
		return nil, e
	}
	var in struct {
		Account   string `json:"account_id"`
		Baseline  string `json:"baseline_price_id"`
		Candidate string `json:"candidate_price_id"`
		Start     string `json:"period_start"`
		End       string `json:"period_end"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	for _, v := range []string{in.Account, in.Baseline, in.Candidate} {
		if _, e := uuid.Parse(v); e != nil {
			return nil, bad("INVALID_ID")
		}
	}
	start, e1 := time.Parse("2006-01-02", in.Start)
	end, e2 := time.Parse("2006-01-02", in.End)
	if e1 != nil || e2 != nil || !end.After(start) || in.Baseline == in.Candidate {
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
	type price struct {
		Provider, Model, Currency, Tier, Region, Route, Assumptions, Reference string
		Rate                                                                   ledger.RateCard
	}
	prices := []price{}
	for _, id := range []string{in.Baseline, in.Candidate} {
		var p price
		e := tx.QueryRow(ctx, `SELECT billing_provider,model_version,currency,service_tier,region,route,assumptions,evidence_url,input_per_million::text,output_per_million::text,cache_read_per_million::text,cache_write_5m_per_million::text,cache_write_1h_per_million::text FROM price_versions WHERE workspace_id=$1 AND id=$2 AND effective_from<=$3 AND effective_to>=$4`, wid, id, start, end).Scan(&p.Provider, &p.Model, &p.Currency, &p.Tier, &p.Region, &p.Route, &p.Assumptions, &p.Reference, &p.Rate.Input, &p.Rate.Output, &p.Rate.Read, &p.Rate.Write5m, &p.Rate.Write1h)
		if e != nil {
			return nil, e
		}
		prices = append(prices, p)
	}
	baseline, candidate := prices[0], prices[1]
	if baseline.Model != candidate.Model || baseline.Currency != candidate.Currency || baseline.Tier != candidate.Tier || baseline.Region != candidate.Region {
		return nil, bad("PRICE_CANDIDATE_NOT_COMPARABLE")
	}
	var provider string
	e := tx.QueryRow(ctx, `SELECT provider FROM provider_accounts WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, wid, in.Account).Scan(&provider)
	if e != nil {
		return nil, e
	}
	if provider != baseline.Provider {
		return nil, bad("PRICE_CANDIDATE_NOT_COMPARABLE")
	}
	usageComplete, e := ledger.CompleteUsage(ctx, tx, wid, in.Account, start, end)
	if e != nil {
		return nil, e
	}
	if !usageComplete {
		return nil, bad("INCOMPLETE_USAGE")
	}
	if provider == "anthropic" && baseline.Region == "" {
		return nil, bad("INCOMPLETE_USAGE")
	}
	var boundary bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM usage_buckets WHERE workspace_id=$1 AND account_id=$2 AND model=$3 AND period_start<$5 AND period_end>$4 AND (period_start<$4 OR period_end>$5) AND coalesce(dimensions->>'service_tier','')=$6 AND coalesce(dimensions->>'region','')=$7 AND coalesce(dimensions->>'endpoint_id','')=$8)`, wid, in.Account, baseline.Model, start, end, baseline.Tier, baseline.Region, baseline.Route).Scan(&boundary)
	if e != nil {
		return nil, e
	}
	if boundary {
		return nil, bad("INCOMPLETE_USAGE")
	}
	rows, e := tx.Query(ctx, `SELECT id,metrics,dimensions FROM usage_buckets WHERE workspace_id=$1 AND account_id=$2 AND model=$3 AND period_start>=$4 AND period_end<=$5 AND coalesce(dimensions->>'service_tier','')=$6 AND coalesce(dimensions->>'region','')=$7 AND coalesce(dimensions->>'endpoint_id','')=$8`, wid, in.Account, baseline.Model, start, end, baseline.Tier, baseline.Region, baseline.Route)
	if e != nil {
		return nil, e
	}
	refs := []string{}
	baseTotal, candidateTotal := decimal.Zero, decimal.Zero
	for rows.Next() {
		var id string
		var raw, dimensionsRaw []byte
		if e = rows.Scan(&id, &raw, &dimensionsRaw); e != nil {
			rows.Close()
			return nil, e
		}
		var m, dimensions map[string]string
		if e = json.Unmarshal(raw, &m); e != nil {
			rows.Close()
			return nil, e
		}
		if e = json.Unmarshal(dimensionsRaw, &dimensions); e != nil {
			rows.Close()
			return nil, e
		}
		if provider == "anthropic" && (dimensions["region"] == "" || dimensions["speed"] != "standard") {
			rows.Close()
			return nil, bad("INCOMPLETE_USAGE")
		}
		if !textUsageComparable(provider, m, dimensions) {
			rows.Close()
			return nil, bad("INCOMPLETE_USAGE")
		}
		a, e := ledger.CalculateTextCost(m, baseline.Rate)
		if e != nil {
			rows.Close()
			return nil, bad("INCOMPLETE_USAGE")
		}
		b, e := ledger.CalculateTextCost(m, candidate.Rate)
		if e != nil {
			rows.Close()
			return nil, bad("INCOMPLETE_USAGE")
		}
		av, _ := decimal.NewFromString(a)
		bv, _ := decimal.NewFromString(b)
		baseTotal = baseTotal.Add(av)
		candidateTotal = candidateTotal.Add(bv)
		refs = append(refs, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(refs) == 0 {
		return nil, bad("INCOMPLETE_USAGE")
	}
	key := "price:" + in.Account + ":" + in.Baseline + ":" + in.Candidate + ":" + in.Start + ":" + in.End
	difference := baseTotal.Sub(candidateTotal)
	if !difference.IsPositive() {
		if _, e = tx.Exec(ctx, `DELETE FROM insights WHERE workspace_id=$1 AND rule_key=$2 AND kind='price_candidate'`, wid, key); e != nil {
			return nil, e
		}
		result := gin.H{"status": "no_savings", "baseline": baseTotal.String(), "candidate": candidateTotal.String(), "currency": baseline.Currency}
		return result, platform.Audit(ctx, tx, wid, session(c).UserID, "price_candidate.cleared", key, gin.H{"baseline": baseTotal.String(), "candidate": candidateTotal.String()})
	}
	evidence, _ := json.Marshal(gin.H{"baseline": baseTotal.String(), "candidate": candidateTotal.String(), "baseline_price_id": in.Baseline, "candidate_price_id": in.Candidate, "model_version": baseline.Model, "period_start": start, "period_end": end, "usage_refs": refs, "usage_coverage_complete": true, "evidence_refs": []string{baseline.Reference, candidate.Reference}, "confidence": "requires_validation", "assumptions": []string{baseline.Assumptions, candidate.Assumptions, "successful native sync windows cover the full comparison period", "price simulation only; validate model identity, modality, context limits, latency, quality, data policy and all extra fees before changing providers", "Anthropic fast-mode usage is excluded until price versions model speed explicitly"}})
	id := uuid.NewString()
	e = tx.QueryRow(ctx, `INSERT INTO insights(id,workspace_id,rule_key,kind,currency,evidence,estimated_savings) VALUES($1,$2,$3,'price_candidate',$4,$5,$6) ON CONFLICT(workspace_id,rule_key) DO UPDATE SET evidence=excluded.evidence,estimated_savings=excluded.estimated_savings,generated_at=now() RETURNING id`, id, wid, key, baseline.Currency, evidence, difference.String()).Scan(&id)
	if e != nil {
		return nil, e
	}
	return gin.H{"id": id, "status": "candidate", "estimated_savings": difference.String(), "currency": baseline.Currency}, platform.Audit(ctx, tx, wid, session(c).UserID, "price_candidate.created", id, gin.H{})
}
