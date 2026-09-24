package service

import (
	"github.com/domainry/domainry-todo-sdk/contract"
	sdk "github.com/domainry/domainry-tools-sdk"
	"strings"
	"time"
	"unicode/utf8"
)

func failure(class, code string) error {
	return &sdk.Error{Class: class, Code: "agent.conversation." + code}
}
func text(value string, limit int, required bool) bool {
	return limit > 0 && len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, 0) && (!required || strings.TrimSpace(value) != "")
}
func Validate(in contract.TodoInput) error {
	if !text(in.Title, 128, true) || !text(in.Description, 1024, false) || !text(in.Timezone, 128, true) || in.Timezone == "Local" || len(in.DueDate) > 10 || len(in.DueAt) > 64 || in.DueDate != "" && in.DueAt != "" {
		return failure("bad_request", "todo_invalid")
	}
	zone, err := time.LoadLocation(in.Timezone)
	if err != nil {
		return failure("bad_request", "todo_timezone_invalid")
	}
	if in.DueDate != "" {
		parsed, err := time.Parse("2006-01-02", in.DueDate)
		if err != nil || parsed.Format("2006-01-02") != in.DueDate {
			return failure("bad_request", "todo_date_invalid")
		}
	}
	if in.DueAt != "" {
		parsed, err := time.Parse(time.RFC3339Nano, in.DueAt)
		if err != nil {
			return failure("bad_request", "todo_date_invalid")
		}
		_, supplied := parsed.Zone()
		_, actual := parsed.In(zone).Zone()
		if supplied != actual {
			return failure("bad_request", "todo_timezone_mismatch")
		}
	}
	return nil
}
