UPDATE alert_rules
SET currency = NULL, amount = NULL
WHERE kind = 'sync_failure';

ALTER TABLE alert_rules ALTER COLUMN currency DROP NOT NULL;
ALTER TABLE alert_rules ALTER COLUMN amount DROP NOT NULL;
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_amount_check;
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_value_check CHECK (
  (kind IN ('budget','spike') AND currency IS NOT NULL AND amount IS NOT NULL AND amount > 0)
  OR (kind = 'sync_failure' AND currency IS NULL AND amount IS NULL)
);

ALTER TABLE insights ALTER COLUMN currency DROP NOT NULL;
