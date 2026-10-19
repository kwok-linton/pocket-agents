package claudenative

import (
	"errors"
	"pocketagents/internal/core"
	"regexp"
)

var modelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/\[\]-]{0,199}$`)

func validateModelChoice(a core.Action) error {
	if a.Model != "" && !modelNamePattern.MatchString(a.Model) {
		return errors.New("invalid model alias or ID")
	}
	switch a.Effort {
	case "", "low", "medium", "high", "xhigh", "max":
	default:
		return errors.New("unsupported Claude effort")
	}
	return nil
}
