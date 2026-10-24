package control

import "context"

//go:generate moq -rm -out channel_mock.go . Channel:MockChannel

const (
	TypeCancel = "cancel"
	TypeInject = "inject"
)

type Message struct {
	Type string `json:"type"`
	Note string `json:"note,omitempty"`
}

type Subscription interface {
	Messages() <-chan Message
	Close() error
}

type Channel interface {
	Subscribe(ctx context.Context, runID string) (Subscription, error)
	Cancel(ctx context.Context, runID string) error
	Inject(ctx context.Context, runID, note string) error
	Close() error
}
