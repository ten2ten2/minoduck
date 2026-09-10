package subscriptions

import "time"

type Plan struct {
	Code          string `json:"code"`
	Monthly       int    `json:"monthly"`
	Yearly        int    `json:"yearly"`
	Workspaces    int    `json:"workspaces"`
	Members       int    `json:"members"`
	Connections   int    `json:"connections"`
	Budgets       int    `json:"budgets"`
	RetentionDays int    `json:"retention_days"`
	SpendLimit    string `json:"spend_limit"`
	Export        bool   `json:"export"`
	Details       bool   `json:"details"`
}

var Plans = map[string]Plan{
	"free":    {"free", 0, 0, 1, 1, 2, 1, 30, "500", false, false},
	"starter": {"starter", 29, 290, 1, 3, 5, 5, 180, "5000", true, true},
	"team":    {"team", 79, 790, 3, 10, 15, 25, 730, "25000", true, true},
}

func Effective(code, status string, grace *time.Time, now time.Time) Plan {
	if status != "active" && !(status == "past_due" && grace != nil && now.Before(*grace)) {
		return Plans["free"]
	}
	if p, ok := Plans[code]; ok {
		return p
	}
	return Plans["free"]
}
func DeferredChange(from, to, fromInterval, toInterval string) bool {
	return (from == "team" && to == "starter") || (fromInterval == "year" && toInterval == "month")
}
