package runstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"ai-orchestration/internal/events"
)

const (
	streamMaxLen = 50000
	streamKeyFmt = "run:%s:events"
	seqKeyFmt    = "run:%s:seq"
	dataField    = "data"
)

type RedisEvents struct {
	rdb *redis.Client
}

func NewRedisEvents(url string) (*RedisEvents, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return &RedisEvents{rdb: redis.NewClient(opts)}, nil
}

func NewRedisEventsFromClient(c *redis.Client) *RedisEvents {
	return &RedisEvents{rdb: c}
}

func (s *RedisEvents) Close() error {
	return s.rdb.Close()
}

func (s *RedisEvents) Append(ctx context.Context, runID string, evt events.Event) (events.Event, error) {
	if runID == "" {
		return events.Event{}, errors.New("run id required")
	}
	if evt.At.IsZero() {
		evt.At = time.Now().UTC()
	}
	evt.RunID = runID
	seq, err := s.rdb.Incr(ctx, fmt.Sprintf(seqKeyFmt, runID)).Result()
	if err != nil {
		return events.Event{}, fmt.Errorf("incr seq: %w", err)
	}
	evt.Seq = seq
	data, err := json.Marshal(evt)
	if err != nil {
		return events.Event{}, fmt.Errorf("marshal event: %w", err)
	}
	id, err := s.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: fmt.Sprintf(streamKeyFmt, runID),
		MaxLen: streamMaxLen,
		Approx: true,
		Values: map[string]string{dataField: string(data)},
	}).Result()
	if err != nil {
		return events.Event{}, fmt.Errorf("xadd: %w", err)
	}
	evt.ID = id
	return evt, nil
}

func (s *RedisEvents) AppendSink(runID string) events.Sink {
	return events.SinkFunc(func(ctx context.Context, evt events.Event) error {
		_, err := s.Append(ctx, runID, evt)
		if err != nil {
			slog.WarnContext(ctx, "append event failed",
				"run", runID,
				"error", err,
			)
		}
		return err
	})
}

func (s *RedisEvents) EventsRange(ctx context.Context, runID string, sinceSeq int64, limit int) ([]events.Event, error) {
	if limit <= 0 {
		limit = 1000
	}
	res, err := s.rdb.XRange(ctx, fmt.Sprintf(streamKeyFmt, runID), "-", "+").Result()
	if err != nil {
		return nil, fmt.Errorf("xrange: %w", err)
	}
	out := make([]events.Event, 0, len(res))
	for _, msg := range res {
		evt, err := decodeMessage(msg)
		if err != nil {
			slog.WarnContext(ctx, "decode event failed",
				"run", runID,
				"id", msg.ID,
				"error", err,
			)
			continue
		}
		if evt.Seq <= sinceSeq {
			continue
		}
		out = append(out, evt)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *RedisEvents) EventsTail(ctx context.Context, runID, lastStreamID string, block time.Duration) (EventTail, error) {
	if lastStreamID == "" {
		lastStreamID = "0"
	}
	res, err := s.rdb.XRead(ctx, &redis.XReadArgs{
		Streams: []string{fmt.Sprintf(streamKeyFmt, runID), lastStreamID},
		Block:   block,
		Count:   200,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return EventTail{LastID: lastStreamID}, nil
	}
	if err != nil {
		return EventTail{LastID: lastStreamID}, fmt.Errorf("xread: %w", err)
	}
	if len(res) == 0 {
		return EventTail{LastID: lastStreamID}, nil
	}
	stream := res[0]
	out := make([]events.Event, 0, len(stream.Messages))
	newLast := lastStreamID
	for _, msg := range stream.Messages {
		evt, err := decodeMessage(msg)
		if err != nil {
			slog.WarnContext(ctx, "decode event failed",
				"run", runID,
				"id", msg.ID,
				"error", err,
			)
			continue
		}
		out = append(out, evt)
		newLast = msg.ID
	}
	return EventTail{Events: out, LastID: newLast}, nil
}

func decodeMessage(msg redis.XMessage) (events.Event, error) {
	raw, ok := msg.Values[dataField].(string)
	if !ok {
		return events.Event{}, errors.New("missing data field")
	}
	var evt events.Event
	if err := json.Unmarshal([]byte(raw), &evt); err != nil {
		return events.Event{}, err
	}
	if evt.ID == "" {
		evt.ID = msg.ID
	}
	return evt, nil
}
