package httpapi

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	stripe "github.com/stripe/stripe-go/v86"
)

func clearPendingCancellation(ctx context.Context, client *stripe.Client, subscriptionID, idempotencyKey string) error {
	if _, err := client.V1Subscriptions.Update(ctx, subscriptionID, &stripe.SubscriptionUpdateParams{
		Params:            stripe.Params{IdempotencyKey: stripe.String(idempotencyKey)},
		CancelAtPeriodEnd: stripe.Bool(false),
	}); err != nil {
		return APIError{"STRIPE_UNAVAILABLE", 503}
	}
	return nil
}

func (s *Server) resumeSubscription(c *gin.Context, tx pgx.Tx) (any, error) {
	if err := s.billingOwner(c, tx); err != nil {
		return nil, err
	}
	var id *string
	if err := tx.QueryRow(c.Request.Context(), `SELECT provider_subscription_id FROM subscriptions WHERE billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&id); err != nil {
		return nil, err
	}
	if id == nil {
		return nil, bad("NO_SUBSCRIPTION")
	}
	client := s.stripe()
	live, err := client.V1Subscriptions.Retrieve(c.Request.Context(), *id, nil)
	if err != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	if live.Status != stripe.SubscriptionStatusActive {
		return nil, bad("SUBSCRIPTION_NOT_ACTIVE")
	}
	if !live.CancelAtPeriodEnd {
		return gin.H{"status": "unchanged"}, nil
	}
	commandID, key, err := s.beginBillingCommand(c.Request.Context(), c.GetString("billing_account_id"), "resume", *id)
	if err != nil {
		return nil, err
	}
	if err = clearPendingCancellation(c.Request.Context(), client, *id, key); err != nil {
		return nil, err
	}
	result, _ := json.Marshal(gin.H{"cancel_at_period_end": false})
	if _, err = tx.Exec(c.Request.Context(), `UPDATE billing_commands SET state='submitted',provider_object_id=$1,result=$2,updated_at=now() WHERE billing_account_id=$3 AND id=$4`, *id, result, c.GetString("billing_account_id"), commandID); err != nil {
		return nil, err
	}
	return gin.H{"status": "awaiting_webhook"}, nil
}
