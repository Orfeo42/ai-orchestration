package pluginapi

import (
	"encoding/json"
	"time"
)

type Subject struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	URL   string `json:"url,omitempty"`
	Title string `json:"title,omitempty"`
}

type TriggerEvent struct {
	Plugin   string          `json:"plugin"`
	Trigger  string          `json:"trigger"`
	Subject  Subject         `json:"subject"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	DedupKey string          `json:"dedup_key,omitempty"`
	At       time.Time       `json:"at"`
}

type TriggerAccepted struct {
	ProcessIDs []string `json:"process_ids"`
}
