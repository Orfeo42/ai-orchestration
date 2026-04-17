package runstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"ai-orchestration/internal/events"
)

const (
	keyIndex      = "runs:index"
	keyActive     = "runs:active"
	streamMaxLen  = 50000
	indexMaxLen   = 5000
	runTTL        = 30 * 24 * time.Hour
	dailyKeyFmt   = "tokens:daily:%s"
	runKeyFmt     = "run:%s"
	streamKeyFmt  = "run:%s:events"
	seqKeyFmt     = "run:%s:seq"
	dataField     = "data"
	dailyDateFmt  = "2006-01-02"
	usageInput    = "input"
	usageOutput   = "output"
	usageCacheCre = "cache_creation_input"
	usageCacheRd  = "cache_read_input"
)

var ErrNotFound = errors.New("run not found")

type RedisStore struct {
	rdb *redis.Client
}

func NewRedis(url string) (*RedisStore, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return &RedisStore{rdb: redis.NewClient(opts)}, nil
}

func NewRedisFromClient(c *redis.Client) *RedisStore {
	return &RedisStore{rdb: c}
}

func (s *RedisStore) Close() error {
	return s.rdb.Close()
}

func (s *RedisStore) Create(ctx context.Context, run Run) error {
	if run.ID == "" {
		return errors.New("run id required")
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.Phase == "" {
		run.Phase = PhaseAssessing
	}
	if err := s.write(ctx, run); err != nil {
		return err
	}
	pipe := s.rdb.Pipeline()
	pipe.ZAdd(ctx, keyIndex, redis.Z{Score: float64(run.StartedAt.UnixNano()), Member: run.ID})
	pipe.ZRemRangeByRank(ctx, keyIndex, 0, -indexMaxLen-1)
	pipe.SAdd(ctx, keyActive, run.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("index run: %w", err)
	}
	return nil
}

func (s *RedisStore) Get(ctx context.Context, id string) (Run, error) {
	data, err := s.rdb.Get(ctx, fmt.Sprintf(runKeyFmt, id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, fmt.Errorf("get run: %w", err)
	}
	var run Run
	if err := json.Unmarshal(data, &run); err != nil {
		return Run{}, fmt.Errorf("unmarshal run: %w", err)
	}
	return run, nil
}

func (s *RedisStore) Update(ctx context.Context, id string, mutate func(*Run)) error {
	run, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	mutate(&run)
	return s.write(ctx, run)
}

func (s *RedisStore) write(ctx context.Context, run Run) error {
	data, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("marshal run: %w", err)
	}
	if err := s.rdb.Set(ctx, fmt.Sprintf(runKeyFmt, run.ID), data, runTTL).Err(); err != nil {
		return fmt.Errorf("set run: %w", err)
	}
	return nil
}

func (s *RedisStore) Finalize(ctx context.Context, id string, phase Phase, errStr string) error {
	run, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	run.Phase = phase
	run.EndedAt = &now
	if errStr != "" {
		run.Error = errStr
	}
	if err := s.write(ctx, run); err != nil {
		return err
	}
	pipe := s.rdb.Pipeline()
	pipe.SRem(ctx, keyActive, id)
	if u := run.Usage; u.Input+u.Output+u.CacheCreationInput+u.CacheReadInput > 0 {
		key := fmt.Sprintf(dailyKeyFmt, now.Format(dailyDateFmt))
		pipe.HIncrBy(ctx, key, usageInput, int64(u.Input))
		pipe.HIncrBy(ctx, key, usageOutput, int64(u.Output))
		pipe.HIncrBy(ctx, key, usageCacheCre, int64(u.CacheCreationInput))
		pipe.HIncrBy(ctx, key, usageCacheRd, int64(u.CacheReadInput))
		pipe.Expire(ctx, key, 365*24*time.Hour)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("finalize pipe: %w", err)
	}
	return nil
}

func (s *RedisStore) Append(ctx context.Context, runID string, evt events.Event) (events.Event, error) {
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

func (s *RedisStore) AppendSink(runID string) events.Sink {
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

func (s *RedisStore) EventsRange(ctx context.Context, runID string, sinceSeq int64, limit int) ([]events.Event, error) {
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

func (s *RedisStore) EventsTail(ctx context.Context, runID, lastStreamID string, block time.Duration) (EventTail, error) {
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

func (s *RedisStore) List(ctx context.Context, filter ListFilter) ([]Run, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	start := max(filter.Cursor, 0)
	ids, err := s.rdb.ZRevRange(ctx, keyIndex, start, start+int64(limit)-1).Result()
	if err != nil {
		return nil, fmt.Errorf("zrevrange: %w", err)
	}
	out := make([]Run, 0, len(ids))
	for _, id := range ids {
		run, err := s.Get(ctx, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if filter.Service != "" && run.Service != filter.Service {
			continue
		}
		if filter.Phase != "" && run.Phase != filter.Phase {
			continue
		}
		out = append(out, run)
	}
	return out, nil
}

func (s *RedisStore) Active(ctx context.Context) ([]Run, error) {
	ids, err := s.rdb.SMembers(ctx, keyActive).Result()
	if err != nil {
		return nil, fmt.Errorf("smembers: %w", err)
	}
	out := make([]Run, 0, len(ids))
	for _, id := range ids {
		run, err := s.Get(ctx, id)
		if errors.Is(err, ErrNotFound) {
			if rerr := s.rdb.SRem(ctx, keyActive, id).Err(); rerr != nil {
				slog.WarnContext(ctx, "stale active run cleanup failed",
					"run", id,
					"error", rerr,
				)
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, nil
}

func (s *RedisStore) DailyTokens(ctx context.Context, from, to time.Time) (map[string]events.TokenUsage, error) {
	if to.Before(from) {
		return nil, errors.New("to before from")
	}
	out := map[string]events.TokenUsage{}
	for d := from.UTC(); !d.After(to.UTC()); d = d.AddDate(0, 0, 1) {
		key := fmt.Sprintf(dailyKeyFmt, d.Format(dailyDateFmt))
		fields, err := s.rdb.HGetAll(ctx, key).Result()
		if err != nil {
			return nil, fmt.Errorf("hgetall: %w", err)
		}
		if len(fields) == 0 {
			continue
		}
		out[d.Format(dailyDateFmt)] = events.TokenUsage{
			Input:              parseInt(fields[usageInput]),
			Output:             parseInt(fields[usageOutput]),
			CacheCreationInput: parseInt(fields[usageCacheCre]),
			CacheReadInput:     parseInt(fields[usageCacheRd]),
		}
	}
	return out, nil
}

func parseInt(s string) int {
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}
