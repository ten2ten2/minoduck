package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	stripe "github.com/stripe/stripe-go/v86"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/subscriptions"
	"io"
	"strconv"
	"strings"
	"time"
)

func (s *Server) stripe() *stripe.Client {
	return subscriptions.NewStripe(s.Config.StripeKey, s.StripeHTTP)
}
func (s *Server) billingOwner(c *gin.Context, tx pgx.Tx) error {
	var id string
	account := c.GetString("billing_account_id")
	e := tx.QueryRow(c.Request.Context(), `SELECT owner_id FROM billing_accounts WHERE id=$1`, account).Scan(&id)
	if e != nil {
		return e
	}
	if id != session(c).UserID {
		return forbidden("OWNER_REQUIRED")
	}
	// The command journal can commit independently because this lock does not
	// conflict with its billing_accounts foreign key.
	return subscriptions.LockAccount(c.Request.Context(), tx, account)
}
func (s *Server) billingURL(c *gin.Context, tx pgx.Tx) (string, error) {
	var slug string
	if e := tx.QueryRow(c.Request.Context(), `SELECT slug FROM workspaces WHERE id=$1`, c.Param("wid")).Scan(&slug); e != nil {
		return "", e
	}
	return s.Config.AppURL + "/w/" + slug + "/settings/billing", nil
}
func (s *Server) subscription(c *gin.Context, tx pgx.Tx) (any, error) {
	p, e := s.plan(c, tx)
	if e != nil {
		return nil, e
	}
	var b []byte
	e = tx.QueryRow(c.Request.Context(), `SELECT json_build_object('has_subscription',provider_subscription_id IS NOT NULL,'plan_code',plan_code,'billing_interval',billing_interval,'status',status,'current_period_end',current_period_end,'cancel_at_period_end',cancel_at_period_end,'grace_period_until',grace_period_until,'scheduled_plan',scheduled_plan,'scheduled_interval',scheduled_interval) FROM subscriptions WHERE billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&b)
	if e != nil {
		return nil, e
	}
	// Team's spend allowance covers the billing account, not each workspace separately.
	rows, e := tx.Query(c.Request.Context(), `SELECT w.id FROM workspaces w JOIN workspace_members m ON m.workspace_id=w.id WHERE w.billing_account_id=$1 AND w.deletion_requested_at IS NULL AND m.user_id=$2`, c.GetString("billing_account_id"), session(c).UserID)
	if e != nil {
		return nil, e
	}
	ids, e := pgx.CollectRows(rows, pgx.RowTo[string])
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	amount := decimal.Zero
	currencies := map[string]string{}
	for _, wid := range ids {
		if _, e = tx.Exec(c.Request.Context(), `SELECT set_config('app.workspace_id',$1,true)`, wid); e != nil {
			return nil, e
		}
		rs, e := tx.Query(c.Request.Context(), `SELECT currency,sum(amount)::text FROM cost_entries WHERE workspace_id=$1 AND is_current AND cost_kind='actual' AND period_start>=$2 AND period_end<=$3 GROUP BY currency`, wid, month, month.AddDate(0, 1, 0))
		if e != nil {
			return nil, e
		}
		for rs.Next() {
			var cur, a string
			if e = rs.Scan(&cur, &a); e != nil {
				rs.Close()
				return nil, e
			}
			v, _ := decimal.NewFromString(a)
			old, _ := decimal.NewFromString(currencies[cur])
			currencies[cur] = old.Add(v).String()
			if cur == "USD" {
				amount = amount.Add(v)
			}
		}
		e = rs.Err()
		rs.Close()
		if e != nil {
			return nil, e
		}
	}
	if _, e = tx.Exec(c.Request.Context(), `SELECT set_config('app.workspace_id',$1,true)`, c.Param("wid")); e != nil {
		return nil, e
	}
	limit, _ := decimal.NewFromString(p.SpendLimit)
	level := "normal"
	if amount.GreaterThanOrEqual(limit) {
		level = "exceeded"
	} else if amount.GreaterThanOrEqual(limit.Mul(decimal.NewFromInt(8)).Div(decimal.NewFromInt(10))) {
		level = "approaching"
	}
	return gin.H{"subscription": json.RawMessage(b), "entitlements": p, "managed_spend": currencies, "spend_level": level, "non_usd_requires_review": len(currencies) > 1 || (len(currencies) == 1 && currencies["USD"] == ""), "soft_limit": true, "stripe_configured": s.Config.StripeKey != ""}, nil
}

type planRequest struct {
	Plan     string `json:"plan"`
	Interval string `json:"billing_interval"`
}

func (s *Server) planInput(c *gin.Context) (planRequest, string, error) {
	var in planRequest
	if e := bind(c, &in); e != nil {
		return in, "", e
	}
	if (in.Plan != "starter" && in.Plan != "team") || (in.Interval != "month" && in.Interval != "year") {
		return in, "", bad("INVALID_PLAN")
	}
	price := s.Config.Prices[in.Plan+":"+in.Interval]
	if price == "" || s.Config.StripeKey == "" {
		return in, "", APIError{"STRIPE_NOT_CONFIGURED", 503}
	}
	return in, price, nil
}
func (s *Server) checkout(c *gin.Context, tx pgx.Tx) (any, error) {
	in, price, e := s.planInput(c)
	if e != nil {
		return nil, e
	}
	if e = s.billingOwner(c, tx); e != nil {
		return nil, e
	}
	ctx := c.Request.Context()
	account := c.GetString("billing_account_id")
	var customer, sub *string
	e = tx.QueryRow(ctx, `SELECT provider_customer_id,provider_subscription_id FROM subscriptions WHERE billing_account_id=$1`, account).Scan(&customer, &sub)
	if e != nil {
		return nil, e
	}
	if sub != nil {
		return nil, APIError{"SUBSCRIPTION_ALREADY_EXISTS", 409}
	}
	client := s.stripe()
	if customer == nil {
		out, e := client.V1Customers.Create(ctx, &stripe.CustomerCreateParams{
			IdempotencyKey: stripe.String("customer-" + account),
			Email:          stripe.String(session(c).Email), Metadata: map[string]string{"billing_account_id": account},
		})
		if e != nil {
			return nil, APIError{"STRIPE_UNAVAILABLE", 503}
		}
		customer = &out.ID
		result, persistErr := s.DB.Exec(ctx, `UPDATE subscriptions SET provider_customer_id=$1 WHERE billing_account_id=$2 AND (provider_customer_id IS NULL OR provider_customer_id=$1)`, out.ID, account)
		if persistErr != nil {
			return nil, persistErr
		}
		if result.RowsAffected() != 1 {
			return nil, APIError{"BILLING_CUSTOMER_MISMATCH", 409}
		}
	}
	key, existingURL, expiry, e := s.beginCheckoutRequest(ctx, account, in.Plan, in.Interval)
	if e != nil {
		return nil, e
	}
	if existingURL != nil {
		return gin.H{"url": *existingURL}, nil
	}
	returnURL, e := s.billingURL(c, tx)
	if e != nil {
		return nil, e
	}
	out, e := client.V1CheckoutSessions.Create(ctx, &stripe.CheckoutSessionCreateParams{
		IdempotencyKey: stripe.String(key),
		Mode:           stripe.String("subscription"), Customer: customer,
		LineItems:  []*stripe.CheckoutSessionCreateLineItemParams{{Price: stripe.String(price), Quantity: stripe.Int64(1)}},
		SuccessURL: stripe.String(returnURL + "?checkout=success"), CancelURL: stripe.String(returnURL + "?checkout=canceled"),
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Metadata:    map[string]string{"billing_account_id": account},
			BillingMode: &stripe.CheckoutSessionCreateSubscriptionDataBillingModeParams{Type: stripe.String("flexible")},
		},
		ClientReferenceID: stripe.String(account), ExpiresAt: new(expiry.Unix()),
	})
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	result, e := s.DB.Exec(ctx, `UPDATE checkout_requests SET session_id=$1,url=$2,expires_at=$3 WHERE billing_account_id=$4 AND request_key=$5`, out.ID, out.URL, time.Unix(out.ExpiresAt, 0), account, key)
	if e != nil {
		return nil, e
	}
	if result.RowsAffected() != 1 {
		return nil, errors.New("checkout request journal changed")
	}
	return gin.H{"url": out.URL}, nil
}

func (s *Server) beginCheckoutRequest(ctx context.Context, account, plan, interval string) (string, *string, time.Time, error) {
	journal, err := s.DB.Begin(ctx)
	if err != nil {
		return "", nil, time.Time{}, err
	}
	defer journal.Rollback(ctx)
	var key, oldPlan, oldInterval string
	var oldURL *string
	var expiry time.Time
	err = journal.QueryRow(ctx, `SELECT request_key,plan_code,billing_interval,url,expires_at FROM checkout_requests WHERE billing_account_id=$1 FOR UPDATE`, account).Scan(&key, &oldPlan, &oldInterval, &oldURL, &expiry)
	if err != nil && err != pgx.ErrNoRows {
		return "", nil, time.Time{}, err
	}
	if err == nil && expiry.After(time.Now()) {
		if oldPlan != plan || oldInterval != interval {
			return "", nil, time.Time{}, APIError{"CHECKOUT_ALREADY_PENDING", 409}
		}
		if err = journal.Commit(ctx); err != nil {
			return "", nil, time.Time{}, err
		}
		return key, oldURL, expiry, nil
	}
	key = "checkout-" + uuid.NewString()
	expiry = time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	_, err = journal.Exec(ctx, `INSERT INTO checkout_requests(billing_account_id,request_key,plan_code,billing_interval,expires_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(billing_account_id) DO UPDATE SET request_key=excluded.request_key,plan_code=excluded.plan_code,billing_interval=excluded.billing_interval,session_id=NULL,url=NULL,expires_at=excluded.expires_at`, account, key, plan, interval, expiry)
	if err != nil {
		return "", nil, time.Time{}, err
	}
	if err = journal.Commit(ctx); err != nil {
		return "", nil, time.Time{}, err
	}
	return key, nil, expiry, nil
}
func (s *Server) portal(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.billingOwner(c, tx); e != nil {
		return nil, e
	}
	var customer *string
	e := tx.QueryRow(c.Request.Context(), `SELECT provider_customer_id FROM subscriptions WHERE billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&customer)
	if e != nil {
		return nil, e
	}
	if customer == nil {
		return nil, bad("NO_BILLING_CUSTOMER")
	}
	if s.Config.StripePortalConfig == "" {
		return nil, APIError{"STRIPE_PORTAL_NOT_CONFIGURED", 503}
	}
	returnURL, e := s.billingURL(c, tx)
	if e != nil {
		return nil, e
	}
	out, e := s.stripe().V1BillingPortalSessions.Create(c.Request.Context(), &stripe.BillingPortalSessionCreateParams{
		IdempotencyKey: stripe.String(uuid.NewString()),
		Customer:       customer, ReturnURL: stripe.String(returnURL), Configuration: stripe.String(s.Config.StripePortalConfig),
	})
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	return gin.H{"url": out.URL}, nil
}
func (s *Server) changeSubscription(c *gin.Context, tx pgx.Tx) (any, error) {
	in, price, e := s.planInput(c)
	if e != nil {
		return nil, e
	}
	if e = s.billingOwner(c, tx); e != nil {
		return nil, e
	}
	var sid *string
	e = tx.QueryRow(c.Request.Context(), `SELECT provider_subscription_id FROM subscriptions WHERE billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&sid)
	if e != nil {
		return nil, e
	}
	if sid == nil {
		return nil, bad("NO_SUBSCRIPTION")
	}
	client := s.stripe()
	live, e := client.V1Subscriptions.Retrieve(c.Request.Context(), *sid, nil)
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	item := subscriptionItem(live)
	if live.Status != stripe.SubscriptionStatusActive || item == nil {
		return nil, bad("SUBSCRIPTION_NOT_ACTIVE")
	}
	oldPlan, oldInterval := s.mapPrice(item.Price.ID)
	if oldPlan == "" {
		return nil, bad("UNRECOGNIZED_PRICE")
	}
	mode := subscriptionChangeMode(oldPlan, oldInterval, in.Plan, in.Interval, live.Schedule != nil)
	if mode == "unchanged" {
		return gin.H{"status": "unchanged"}, nil
	}
	if mode == "cancel_scheduled" {
		if e = s.releaseScheduledChange(c, tx, client, live.Schedule.ID); e != nil {
			return nil, e
		}
		return gin.H{"status": "schedule_canceled"}, nil
	}
	commandID, key, e := s.beginBillingCommand(c.Request.Context(), c.GetString("billing_account_id"), "change", *sid+":"+strconv.FormatInt(item.CurrentPeriodStart, 10)+":"+in.Plan+":"+in.Interval)
	if e != nil {
		return nil, e
	}
	if mode == "deferred" {
		if live.CancelAtPeriodEnd {
			if e = clearPendingCancellation(c.Request.Context(), client, *sid, key+"-resume"); e != nil {
				return nil, e
			}
		}
		scheduleID := ""
		if live.Schedule != nil {
			scheduleID = live.Schedule.ID
		} else {
			schedule, e := client.V1SubscriptionSchedules.Create(c.Request.Context(), &stripe.SubscriptionScheduleCreateParams{
				IdempotencyKey: stripe.String(key + "-create"), FromSubscription: sid,
			})
			if e != nil {
				return nil, APIError{"STRIPE_UNAVAILABLE", 503}
			}
			scheduleID = schedule.ID
		}
		if _, e = client.V1SubscriptionSchedules.Update(c.Request.Context(), scheduleID, scheduleParams(item, price, in.Interval, key+"-phases")); e != nil {
			return nil, APIError{"STRIPE_UNAVAILABLE", 503}
		}
		result, _ := json.Marshal(gin.H{"target_plan": in.Plan, "target_interval": in.Interval, "effective_at": item.CurrentPeriodEnd})
		if _, e = tx.Exec(c.Request.Context(), `UPDATE billing_commands SET state='submitted',provider_object_id=$1,result=$2,updated_at=now() WHERE billing_account_id=$3 AND id=$4`, scheduleID, result, c.GetString("billing_account_id"), commandID); e != nil {
			return nil, e
		}
		_, e = tx.Exec(c.Request.Context(), `UPDATE subscriptions SET scheduled_plan=$1,scheduled_interval=$2,provider_schedule_id=$3 WHERE billing_account_id=$4`, in.Plan, in.Interval, scheduleID, c.GetString("billing_account_id"))
		return gin.H{"status": "scheduled", "effective_at": time.Unix(item.CurrentPeriodEnd, 0)}, e
	}
	if mode == "release_then_immediate" {
		if e = s.releaseScheduledChange(c, tx, client, live.Schedule.ID); e != nil {
			return nil, e
		}
	}
	params := &stripe.SubscriptionUpdateParams{
		IdempotencyKey:    stripe.String(key),
		Items:             []*stripe.SubscriptionUpdateItemParams{{ID: stripe.String(item.ID), Price: stripe.String(price)}},
		ProrationBehavior: stripe.String("always_invoice"), PaymentBehavior: stripe.String("pending_if_incomplete"),
		CancelAtPeriodEnd: new(false),
	}
	if oldInterval != in.Interval {
		params.BillingCycleAnchorNow = new(true)
	}
	updated, e := client.V1Subscriptions.Update(c.Request.Context(), *sid, params)
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	state := "submitted"
	response := gin.H{"status": "awaiting_webhook"}
	result := gin.H{"target_plan": in.Plan, "target_interval": in.Interval}
	if updated.PendingUpdate != nil {
		state = "requires_action"
		response["status"] = "requires_action"
		if updated.LatestInvoice != nil && updated.LatestInvoice.ID != "" {
			invoice, invoiceErr := client.V1Invoices.Retrieve(c.Request.Context(), updated.LatestInvoice.ID, nil)
			if invoiceErr == nil && invoice.HostedInvoiceURL != "" {
				response["action_url"] = invoice.HostedInvoiceURL
				result["action_url"] = invoice.HostedInvoiceURL
			}
			result["invoice_id"] = updated.LatestInvoice.ID
		}
		if response["action_url"] == nil {
			return nil, APIError{"PAYMENT_ACTION_UNAVAILABLE", 503}
		}
	}
	encoded, _ := json.Marshal(result)
	if _, e = tx.Exec(c.Request.Context(), `UPDATE billing_commands SET state=$1,provider_object_id=$2,result=$3,updated_at=now() WHERE billing_account_id=$4 AND id=$5`, state, *sid, encoded, c.GetString("billing_account_id"), commandID); e != nil {
		return nil, e
	}
	return response, nil
}
func (s *Server) releaseScheduledChange(c *gin.Context, tx pgx.Tx, client *stripe.Client, scheduleID string) error {
	if _, e := client.V1SubscriptionSchedules.Release(c.Request.Context(), scheduleID, &stripe.SubscriptionScheduleReleaseParams{
		IdempotencyKey: stripe.String("release-" + scheduleID),
	}); e != nil {
		return APIError{"STRIPE_UNAVAILABLE", 503}
	}
	if _, e := tx.Exec(c.Request.Context(), `UPDATE billing_commands SET state='canceled',updated_at=now() WHERE billing_account_id=$1 AND provider_object_id=$2 AND state IN ('pending','requires_action','submitted')`, c.GetString("billing_account_id"), scheduleID); e != nil {
		return e
	}
	_, e := tx.Exec(c.Request.Context(), `UPDATE subscriptions SET scheduled_plan=NULL,scheduled_interval=NULL,provider_schedule_id=NULL WHERE billing_account_id=$1`, c.GetString("billing_account_id"))
	return e
}
func (s *Server) cancelSubscription(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.billingOwner(c, tx); e != nil {
		return nil, e
	}
	var id *string
	e := tx.QueryRow(c.Request.Context(), `SELECT provider_subscription_id FROM subscriptions WHERE billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&id)
	if e != nil {
		return nil, e
	}
	if id == nil {
		return nil, bad("NO_SUBSCRIPTION")
	}
	client := s.stripe()
	live, e := client.V1Subscriptions.Retrieve(c.Request.Context(), *id, nil)
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	if live.Schedule != nil {
		if e = s.releaseScheduledChange(c, tx, client, live.Schedule.ID); e != nil {
			return nil, e
		}
	}
	commandID, key, e := s.beginBillingCommand(c.Request.Context(), c.GetString("billing_account_id"), "cancel", *id)
	if e != nil {
		return nil, e
	}
	_, e = client.V1Subscriptions.Update(c.Request.Context(), *id, &stripe.SubscriptionUpdateParams{
		IdempotencyKey: stripe.String(key), CancelAtPeriodEnd: new(true),
	})
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	result, _ := json.Marshal(gin.H{"cancel_at_period_end": true})
	if _, e = tx.Exec(c.Request.Context(), `UPDATE billing_commands SET state='submitted',provider_object_id=$1,result=$2,updated_at=now() WHERE billing_account_id=$3 AND id=$4`, *id, result, c.GetString("billing_account_id"), commandID); e != nil {
		return nil, e
	}
	return gin.H{"status": "awaiting_webhook"}, nil
}

func (s *Server) beginBillingCommand(ctx context.Context, account, kind, fingerprint string) (string, string, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx)
	var id, key string
	err = tx.QueryRow(ctx, `SELECT id,idempotency_key FROM billing_commands WHERE billing_account_id=$1 AND command_type=$2 AND fingerprint=$3 AND state IN ('pending','requires_action','submitted') ORDER BY created_at DESC LIMIT 1 FOR UPDATE`, account, kind, fingerprint).Scan(&id, &key)
	if err == nil {
		err = tx.Commit(ctx)
		return id, key, err
	}
	if err != pgx.ErrNoRows {
		return "", "", err
	}
	id = uuid.NewString()
	key = "billing-" + kind + "-" + id
	if _, err = tx.Exec(ctx, `INSERT INTO billing_commands(id,billing_account_id,command_type,fingerprint,idempotency_key) VALUES($1,$2,$3,$4,$5)`, id, account, kind, fingerprint, key); err != nil {
		return "", "", err
	}
	err = tx.Commit(ctx)
	return id, key, err
}
func (s *Server) mapPrice(id string) (string, string) {
	for key, v := range s.Config.Prices {
		if v != "" && v == id {
			parts := strings.Split(key, ":")
			return parts[0], parts[1]
		}
	}
	return "", ""
}

func subscriptionChangeMode(fromPlan, fromInterval, toPlan, toInterval string, hasSchedule bool) string {
	if fromPlan == toPlan && fromInterval == toInterval {
		if hasSchedule {
			return "cancel_scheduled"
		}
		return "unchanged"
	}
	if subscriptions.DeferredChange(fromPlan, toPlan, fromInterval, toInterval) {
		return "deferred"
	}
	if hasSchedule {
		return "release_then_immediate"
	}
	return "immediate"
}

func subscriptionItem(live *stripe.Subscription) *stripe.SubscriptionItem {
	if live.Items == nil || len(live.Items.Data) != 1 {
		return nil
	}
	item := live.Items.Data[0]
	if item == nil || item.Price == nil || item.Price.Recurring == nil {
		return nil
	}
	return item
}

func scheduleParams(current *stripe.SubscriptionItem, price, interval, key string) *stripe.SubscriptionScheduleUpdateParams {
	return &stripe.SubscriptionScheduleUpdateParams{
		IdempotencyKey: stripe.String(key),
		EndBehavior:    stripe.String("release"), ProrationBehavior: stripe.String("none"),
		Phases: []*stripe.SubscriptionScheduleUpdatePhaseParams{
			{
				StartDate: new(current.CurrentPeriodStart), EndDate: new(current.CurrentPeriodEnd),
				Items:             []*stripe.SubscriptionScheduleUpdatePhaseItemParams{{Price: stripe.String(current.Price.ID), Quantity: stripe.Int64(1)}},
				ProrationBehavior: stripe.String("none"),
			},
			{
				Duration:          &stripe.SubscriptionScheduleUpdatePhaseDurationParams{Interval: stripe.String(interval), IntervalCount: stripe.Int64(1)},
				Items:             []*stripe.SubscriptionScheduleUpdatePhaseItemParams{{Price: stripe.String(price), Quantity: stripe.Int64(1)}},
				ProrationBehavior: stripe.String("none"),
			},
		},
	}
}

func stripeEventSubject(event stripe.Event) (customerID, subscriptionID string, err error) {
	var customer *stripe.Customer
	var subscription *stripe.Subscription
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		var session stripe.CheckoutSession
		if err = json.Unmarshal(event.Data.Raw, &session); err != nil {
			return
		}
		customer, subscription = session.Customer, session.Subscription
	case stripe.EventTypeCustomerSubscriptionCreated, stripe.EventTypeCustomerSubscriptionUpdated, stripe.EventTypeCustomerSubscriptionDeleted:
		var sub stripe.Subscription
		if err = json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return
		}
		customer, subscription = sub.Customer, &sub
	case stripe.EventTypeInvoicePaid, stripe.EventTypeInvoicePaymentFailed:
		var invoice stripe.Invoice
		if err = json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			return
		}
		customer = invoice.Customer
		if invoice.Parent != nil && invoice.Parent.SubscriptionDetails != nil {
			subscription = invoice.Parent.SubscriptionDetails.Subscription
		}
	}
	if customer != nil {
		customerID = customer.ID
	}
	if subscription != nil {
		subscriptionID = subscription.ID
	}
	return
}

func stripeEventSubscription(event stripe.Event) *stripe.Subscription {
	if event.Data == nil {
		return nil
	}
	switch event.Type {
	case stripe.EventTypeCustomerSubscriptionCreated, stripe.EventTypeCustomerSubscriptionUpdated, stripe.EventTypeCustomerSubscriptionDeleted:
		var subscription stripe.Subscription
		if json.Unmarshal(event.Data.Raw, &subscription) == nil && subscription.ID != "" {
			return &subscription
		}
	}
	return nil
}

func terminalSubscription(status stripe.SubscriptionStatus) bool {
	return status == stripe.SubscriptionStatusCanceled || status == stripe.SubscriptionStatusIncompleteExpired
}

func (s *Server) registerBillingEvent(ctx context.Context, event stripe.Event) (string, error) {
	var outcome string
	err := s.DB.QueryRow(ctx, `INSERT INTO billing_events(provider_event_id,event_type,outcome) VALUES($1,$2,'received') ON CONFLICT(provider,provider_event_id) DO UPDATE SET attempts=billing_events.attempts+1,updated_at=now() RETURNING outcome`, event.ID, event.Type).Scan(&outcome)
	return outcome, err
}

func (s *Server) markBillingEvent(ctx context.Context, eventID, outcome, code string) {
	cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.DB.Exec(cleanup, `UPDATE billing_events SET outcome=$1,last_error=nullif($2,''),updated_at=now(),processed_at=CASE WHEN $1 IN ('processed','ignored','quarantined') THEN now() ELSE processed_at END WHERE provider='stripe' AND provider_event_id=$3 AND ($1<>'failed' OR outcome NOT IN ('processed','ignored','quarantined'))`, outcome, code, eventID)
}

func (s *Server) stripeWebhook(c *gin.Context) {
	body, e := io.ReadAll(io.LimitReader(c.Request.Body, 1024*1024+1))
	if e != nil || len(body) > 1024*1024 {
		s.fail(c, bad("INVALID_WEBHOOK"))
		return
	}
	event, e := subscriptions.ParseStripeEvent(body, c.GetHeader("Stripe-Signature"), s.Config.StripeWebhookSecret)
	if e != nil {
		s.fail(c, bad("INVALID_STRIPE_SIGNATURE"))
		return
	}
	if event.ID == "" || event.Data == nil {
		s.fail(c, bad("INVALID_WEBHOOK"))
		return
	}
	customerID, sid, e := stripeEventSubject(event)
	if e != nil {
		s.fail(c, bad("INVALID_WEBHOOK"))
		return
	}
	outcome, e := s.registerBillingEvent(c.Request.Context(), event)
	if e != nil {
		s.fail(c, e)
		return
	}
	if outcome == "processed" || outcome == "ignored" || outcome == "quarantined" {
		c.JSON(200, gin.H{"status": "duplicate"})
		return
	}
	if sid == "" {
		s.markBillingEvent(c.Request.Context(), event.ID, "ignored", "")
		c.JSON(200, gin.H{"status": "ignored"})
		return
	}
	if customerID == "" {
		s.markBillingEvent(c.Request.Context(), event.ID, "quarantined", "MISSING_CUSTOMER")
		c.JSON(200, gin.H{"status": "quarantined"})
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		s.fail(c, e)
		return
	}
	defer tx.Rollback(ctx)
	fail := func(err error) {
		_ = tx.Rollback(ctx)
		code := "INTERNAL_ERROR"
		if known, ok := errors.AsType[APIError](err); ok {
			code = known.Code
		}
		s.markBillingEvent(ctx, event.ID, "failed", code)
		s.fail(c, err)
	}
	quarantine := func(code string) {
		_ = tx.Rollback(ctx)
		s.markBillingEvent(ctx, event.ID, "quarantined", code)
		c.JSON(200, gin.H{"status": "quarantined"})
	}
	var lockedOutcome string
	if e = tx.QueryRow(ctx, `SELECT outcome FROM billing_events WHERE provider='stripe' AND provider_event_id=$1 FOR UPDATE`, event.ID).Scan(&lockedOutcome); e != nil {
		fail(e)
		return
	}
	if lockedOutcome == "processed" || lockedOutcome == "ignored" || lockedOutcome == "quarantined" {
		c.JSON(200, gin.H{"status": "duplicate"})
		return
	}
	var account string
	e = tx.QueryRow(ctx, `SELECT billing_account_id FROM subscriptions WHERE provider_customer_id=$1`, customerID).Scan(&account)
	if e == pgx.ErrNoRows {
		fail(APIError{"BILLING_CUSTOMER_PENDING", 503})
		return
	}
	if e != nil {
		fail(e)
		return
	}
	if e = subscriptions.LockAccount(ctx, tx, account); e != nil {
		fail(e)
		return
	}
	var current *string
	e = tx.QueryRow(ctx, `SELECT provider_subscription_id FROM subscriptions WHERE billing_account_id=$1 AND provider_customer_id=$2 FOR UPDATE`, account, customerID).Scan(&current)
	if e != nil {
		fail(e)
		return
	}
	embedded := stripeEventSubscription(event)
	client := s.stripe()
	live, retrieveErr := client.V1Subscriptions.Retrieve(ctx, sid, nil)
	if retrieveErr != nil {
		if embedded == nil || !terminalSubscription(embedded.Status) {
			fail(APIError{"STRIPE_UNAVAILABLE", 503})
			return
		}
		live = embedded
	}
	if live.Customer == nil || live.Customer.ID != customerID {
		quarantine("SUBSCRIPTION_SCOPE_MISMATCH")
		return
	}
	// An old canceled subscription cannot revoke a replacement subscription.
	if current != nil && *current != sid {
		if terminalSubscription(live.Status) {
			if _, e = tx.Exec(ctx, `UPDATE billing_events SET outcome='ignored',last_error=NULL,updated_at=now(),processed_at=now() WHERE provider='stripe' AND provider_event_id=$1`, event.ID); e != nil {
				fail(e)
				return
			}
			if e = tx.Commit(ctx); e != nil {
				fail(e)
				return
			}
			c.JSON(200, gin.H{"status": "obsolete"})
			return
		}
		quarantine("DUPLICATE_SUBSCRIPTION_REVIEW")
		return
	}
	if terminalSubscription(live.Status) {
		// Terminal objects may no longer contain items, metadata or a recognized
		// price. Revoke first, using the already verified customer/subscription
		// relationship, so malformed terminal expansion cannot preserve access.
		if current == nil {
			if _, e = tx.Exec(ctx, `UPDATE billing_events SET outcome='ignored',last_error=NULL,updated_at=now(),processed_at=now() WHERE provider='stripe' AND provider_event_id=$1`, event.ID); e != nil {
				fail(e)
				return
			}
		} else {
			if _, e = tx.Exec(ctx, `UPDATE subscriptions SET provider_subscription_id=NULL,plan_code='free',billing_interval=NULL,status=$1,current_period_start=NULL,current_period_end=NULL,cancel_at_period_end=false,grace_period_until=NULL,grace_invoice_id=NULL,scheduled_plan=NULL,scheduled_interval=NULL,provider_schedule_id=NULL,retention_grace_until=now()+interval '30 days',updated_at=now() WHERE billing_account_id=$2`, live.Status, account); e != nil {
				fail(e)
				return
			}
			if e = subscriptions.ApplyEntitlements(ctx, tx, account, subscriptions.Plans["free"]); e != nil {
				fail(e)
				return
			}
			if _, e = tx.Exec(ctx, `UPDATE billing_commands SET state=CASE WHEN command_type='cancel' THEN 'succeeded' ELSE 'canceled' END,updated_at=now() WHERE billing_account_id=$1 AND (provider_object_id=$2 OR provider_object_id IS NULL AND (fingerprint=$2 OR fingerprint LIKE $2||':%')) AND state IN ('pending','requires_action','submitted')`, account, sid); e != nil {
				fail(e)
				return
			}
			if _, e = tx.Exec(ctx, `UPDATE billing_events SET outcome='processed',last_error=NULL,updated_at=now(),processed_at=now() WHERE provider='stripe' AND provider_event_id=$1`, event.ID); e != nil {
				fail(e)
				return
			}
		}
		if _, e = tx.Exec(ctx, `DELETE FROM checkout_requests WHERE billing_account_id=$1`, account); e != nil {
			fail(e)
			return
		}
		if e = tx.Commit(ctx); e != nil {
			fail(e)
			return
		}
		c.JSON(200, gin.H{"status": "processed"})
		return
	}
	item := subscriptionItem(live)
	if live.Metadata["billing_account_id"] != account || item == nil || item.CurrentPeriodEnd <= item.CurrentPeriodStart {
		quarantine("SUBSCRIPTION_SCOPE_MISMATCH")
		return
	}
	plan, interval := s.mapPrice(item.Price.ID)
	if plan == "" {
		quarantine("UNRECOGNIZED_PRICE")
		return
	}
	var grace *time.Time
	var graceInvoice *string
	e = tx.QueryRow(ctx, `SELECT grace_period_until,grace_invoice_id FROM subscriptions WHERE billing_account_id=$1`, account).Scan(&grace, &graceInvoice)
	if e != nil {
		fail(e)
		return
	}
	if live.Status == stripe.SubscriptionStatusPastDue && (grace == nil || live.LatestInvoice == nil || graceInvoice == nil || *graceInvoice != live.LatestInvoice.ID) {
		// Derive from Stripe's current invoice rather than delayed webhook arrival.
		if live.LatestInvoice == nil || live.LatestInvoice.ID == "" {
			fail(APIError{"INVOICE_STATE_PENDING", 503})
			return
		}
		inv, e := client.V1Invoices.Retrieve(ctx, live.LatestInvoice.ID, nil)
		if e != nil {
			fail(APIError{"STRIPE_UNAVAILABLE", 503})
			return
		}
		until := time.Unix(inv.Created, 0).Add(7 * 24 * time.Hour)
		grace = &until
		invoiceID := live.LatestInvoice.ID
		graceInvoice = &invoiceID
	}
	if live.Status != stripe.SubscriptionStatusPastDue {
		grace = nil
		graceInvoice = nil
	}
	var scheduleID *string
	if live.Schedule != nil {
		scheduleID = &live.Schedule.ID
	}
	_, e = tx.Exec(ctx, `UPDATE subscriptions SET provider_subscription_id=$1,plan_code=$2,billing_interval=$3,status=$4,current_period_start=$5,current_period_end=$6,cancel_at_period_end=$7,grace_period_until=$8,grace_invoice_id=$9,scheduled_plan=CASE WHEN scheduled_plan=$2 AND scheduled_interval=$3 OR $10::text IS NULL THEN NULL ELSE scheduled_plan END,scheduled_interval=CASE WHEN scheduled_plan=$2 AND scheduled_interval=$3 OR $10::text IS NULL THEN NULL ELSE scheduled_interval END,provider_schedule_id=$10,retention_grace_until=CASE WHEN plan_code='team' AND $2 IN ('starter','free') OR plan_code='starter' AND $2='free' THEN now()+interval '30 days' ELSE retention_grace_until END,updated_at=now() WHERE billing_account_id=$11`, sid, plan, interval, live.Status, time.Unix(item.CurrentPeriodStart, 0), time.Unix(item.CurrentPeriodEnd, 0), live.CancelAtPeriodEnd, grace, graceInvoice, scheduleID, account)
	if e != nil {
		fail(e)
		return
	}
	effective := subscriptions.Effective(plan, string(live.Status), grace, time.Now())
	if e = subscriptions.ApplyEntitlements(ctx, tx, account, effective); e != nil {
		fail(e)
		return
	}
	if _, e = tx.Exec(ctx, `UPDATE billing_commands SET state=CASE WHEN command_type='change' AND (result->>'target_plan'=$2 AND result->>'target_interval'=$3 OR provider_object_id IS NULL AND fingerprint LIKE $5||':%:'||$2||':'||$3) THEN 'succeeded' WHEN command_type='cancel' AND $4 THEN 'succeeded' WHEN command_type='resume' AND NOT $4 THEN 'succeeded' ELSE state END,updated_at=now() WHERE billing_account_id=$1 AND (provider_object_id=$5 OR provider_object_id=$6 OR provider_object_id IS NULL AND ((command_type IN ('cancel','resume') AND fingerprint=$5) OR command_type='change' AND fingerprint LIKE $5||':%:'||$2||':'||$3)) AND state IN ('pending','requires_action','submitted')`, account, plan, interval, live.CancelAtPeriodEnd, sid, scheduleID); e != nil {
		fail(e)
		return
	}
	if _, e = tx.Exec(ctx, `UPDATE billing_events SET outcome='processed',last_error=NULL,updated_at=now(),processed_at=now() WHERE provider='stripe' AND provider_event_id=$1`, event.ID); e != nil {
		fail(e)
		return
	}
	if _, e = tx.Exec(ctx, `DELETE FROM checkout_requests WHERE billing_account_id=$1`, account); e != nil {
		fail(e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		fail(e)
		return
	}
	c.JSON(200, gin.H{"status": "processed"})
}

var _ = platform.RandomToken
