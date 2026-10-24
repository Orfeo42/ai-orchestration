package main

import (
	"slices"
	"strings"

	"github.com/Orfeo42/ai-orchestration/internal/step"
)

type queueError string

func (e queueError) Error() string { return string(e) }

const (
	errNoQueues       queueError = "at least one queue is required"
	errUnknownQueue   queueError = "unknown queue"
	errDuplicateQueue queueError = "duplicate queue"
)

func parseQueues(raw string) ([]string, error) {
	var queues []string
	for _, name := range strings.Split(raw, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if name != step.QueueClaude && name != step.QueueDefault {
			return nil, errUnknownQueue + ": " + queueError(name)
		}
		if slices.Contains(queues, name) {
			return nil, errDuplicateQueue + ": " + queueError(name)
		}
		queues = append(queues, name)
	}
	if len(queues) == 0 {
		return nil, errNoQueues
	}
	return queues, nil
}
