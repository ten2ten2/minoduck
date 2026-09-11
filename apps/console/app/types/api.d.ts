export {}

declare global {
  interface ApiAction {
    id?: string
    slug?: string
    status?: string
    url?: string
    action_url?: string
    development_link?: string
  }

  interface MoneyRow {
    amount: string
    currency: string
  }

  interface Connection {
    id: string
    provider: string
    name: string
    external_account_ref: string
    provider_identity?: string | null
    provider_scope?: string
    status: string
    credential_suffix?: string | null
    data_through?: string | null
    billing_suspended?: boolean
  }

  interface ProviderCapability {
    id: string
    name: string
    credential_kind: string
  }

  interface CostEntry extends MoneyRow {
    id: string
    period_start: string
    period_end: string
    billing_provider: string
    model: string
    charge_category: string
    cost_kind: string
    source_scope: string
    coverage: string
  }

  interface CostRevision extends MoneyRow {
    revision: number
    source_batch_id: string
    source_record_ref: string
    ingested_at: string
  }

  interface CostsResponse {
    items: CostEntry[]
    total: number
    page: number
    page_size: number
  }

  interface CostDetailResponse {
    entry: CostEntry
    history: CostRevision[]
  }

  interface OverviewResponse {
    totals: MoneyRow[]
    billed: MoneyRow[]
    providers: Array<MoneyRow & { provider: string }>
    trend: Array<MoneyRow & { date: string }>
    sources: Connection[]
    coverage: string
    unexplained_count: number
    insight_count: number
  }

  interface AlertRule {
    id: string
    name: string
    kind: string
    currency: string
    amount: string
    enabled: boolean
    billing_suspended?: boolean
  }

  interface Insight {
    id: string
    kind: string
    state: string
    currency: string
    estimated_savings?: string | null
    details_locked: boolean
    evidence: {
      actual?: string
      budget?: string
      baseline_median?: string
      error_code?: string
    }
  }

  interface PriceVersion {
    id: string
    billing_provider: string
    model_version: string
    effective_from: string
    route: string
  }

  interface Invoice extends MoneyRow {
    id: string
    reference: string
    period_start: string
    period_end: string
    adjustment: string
    evidence_note: string
    source_scope: string
  }

  interface ImportPreview {
    id: string
    preview: {
      count: number
      rejected: number
      totals: Record<string, string>
      errors: Array<{ row: number; code: string }>
      rows: CostEntry[]
    }
  }

  interface ReconciliationRun {
    id: string
    run_version: number
    match_status: string
    expected?: string | null
    billed?: string | null
    difference?: string | null
    currency: string
    level: 'L1' | 'L2'
    handling_status: string
    handling_note?: string
    evidence: {
      source_scope: string
      period_start: string
      period_end: string
      adjustment?: string
      price_evidence?: Array<{
        usage_id: string
        reference: string
        price_version_id: string
        price_basis: string
        amount: string
      }>
      entry_refs: Array<{ entry_id: string; revision: number }>
    }
  }

  interface ReconciliationSummary {
    id: string
    created_at: string
    run_version: number
    match_status: string
    difference?: string | null
    currency: string
    handling_status: string
    details_locked: boolean
  }

  interface Member {
    id: string
    email: string
    role: 'owner' | 'admin' | 'viewer'
    billing_suspended?: boolean
  }

  interface ExportRecord {
    id: string
    state: string
    created_at: string
    expires_at: string
  }

  interface BillingResponse {
    subscription: {
      has_subscription: boolean
      plan_code: string
      billing_interval: 'month' | 'year' | null
      status: string
      current_period_end?: string | null
      cancel_at_period_end: boolean
      scheduled_plan?: string | null
      scheduled_interval?: string | null
    }
    entitlements: { code: string; spend_limit: string }
    managed_spend: Record<string, string>
    spend_level: string
    non_usd_requires_review: boolean
    stripe_configured: boolean
  }
}
