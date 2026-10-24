package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/Orfeo42/ai-orchestration/pluginapi"
)

const (
	mimeJSON        = "application/json"
	mimeEventStream = "text/event-stream"
)

type HTTPError struct {
	Status int
	API    pluginapi.APIError
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("plugin api: status %d: %s: %s", e.Status, e.API.Code, e.API.Message)
}

func (e *HTTPError) Unwrap() error {
	switch e.API.Code {
	case pluginapi.CodeInvalidInput:
		return pluginapi.ErrInvalidInput
	case pluginapi.CodeUnknownAction:
		return pluginapi.ErrUnknownAction
	case pluginapi.CodeJobNotFound:
		return pluginapi.ErrJobNotFound
	case pluginapi.CodeJobConflict:
		return pluginapi.ErrJobConflict
	case pluginapi.CodeJobNotActive:
		return pluginapi.ErrJobNotActive
	case pluginapi.CodeUnsupported:
		return pluginapi.ErrUnsupported
	case pluginapi.CodeUnauthorized:
		return pluginapi.ErrUnauthorized
	case pluginapi.CodePayloadTooLarge:
		return pluginapi.ErrPayloadTooLarge
	case pluginapi.CodeUpstreamFailure:
		return pluginapi.ErrUpstream
	case pluginapi.CodeProtocolMismatch:
		return pluginapi.ErrUnsupportedProtocol
	}
	switch {
	case e.Status == http.StatusGone:
		return pluginapi.ErrJobNotActive
	case e.Status == http.StatusUnauthorized:
		return pluginapi.ErrUnauthorized
	case e.Status >= http.StatusInternalServerError:
		return pluginapi.ErrUpstream
	}
	return nil
}

func newHTTPError(status int, body []byte) *HTTPError {
	var api pluginapi.APIError
	if err := json.Unmarshal(body, &api); err == nil && api.Code != "" {
		return &HTTPError{Status: status, API: api}
	}
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &HTTPError{Status: status, API: pluginapi.APIError{Code: pluginapi.CodeInternal, Message: msg}}
}

func newRequest(ctx context.Context, method, target string, body []byte, accept string) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", accept)
	if body != nil {
		req.Header.Set("Content-Type", mimeJSON)
	}
	return req, nil
}

func closeBody(body io.Closer, err error) error {
	return errors.Join(err, body.Close())
}

func readLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, pluginapi.DefaultMaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > pluginapi.DefaultMaxBodyBytes {
		return nil, fmt.Errorf("response exceeds %d bytes: %w", pluginapi.DefaultMaxBodyBytes, pluginapi.ErrPayloadTooLarge)
	}
	return data, nil
}

func failure(resp *http.Response) error {
	data, err := readLimited(resp.Body)
	if err = closeBody(resp.Body, err); err != nil {
		return err
	}
	return newHTTPError(resp.StatusCode, data)
}

func doRequest(hc *http.Client, req *http.Request, accepted []int) ([]byte, error) {
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", pluginapi.ErrUpstream, err)
	}
	ok := resp.StatusCode/100 == 2
	if len(accepted) > 0 {
		ok = slices.Contains(accepted, resp.StatusCode)
	}
	if !ok {
		return nil, failure(resp)
	}
	data, err := readLimited(resp.Body)
	if err = closeBody(resp.Body, err); err != nil {
		return nil, err
	}
	return data, nil
}

func clientOrDefault(hc *http.Client) *http.Client {
	if hc == nil {
		return http.DefaultClient
	}
	return hc
}

type Plugin struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewPlugin(baseURL, token string, hc *http.Client) *Plugin {
	return &Plugin{baseURL: baseURL, token: token, http: clientOrDefault(hc)}
}

func (c *Plugin) newRequest(ctx context.Context, method, target string, body []byte, accept string) (*http.Request, error) {
	req, err := newRequest(ctx, method, target, body, accept)
	if err != nil {
		return nil, err
	}
	req.Header.Set(pluginapi.HeaderAuthorization, pluginapi.BearerPrefix+c.token)
	return req, nil
}

func (c *Plugin) call(ctx context.Context, method, target string, body []byte, accepted ...int) ([]byte, error) {
	req, err := c.newRequest(ctx, method, target, body, mimeJSON)
	if err != nil {
		return nil, err
	}
	return doRequest(c.http, req, accepted)
}

func (c *Plugin) jobURL(path, jobID string) string {
	return c.baseURL + strings.ReplaceAll(path, "{job_id}", url.PathEscape(jobID))
}

func (c *Plugin) Manifest(ctx context.Context) (pluginapi.Manifest, error) {
	data, err := c.call(ctx, http.MethodGet, c.baseURL+pluginapi.PathManifest, nil, http.StatusOK)
	if err != nil {
		return pluginapi.Manifest{}, err
	}
	var m pluginapi.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return pluginapi.Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return m, nil
}

func (c *Plugin) Health(ctx context.Context) (pluginapi.Health, error) {
	data, err := c.call(ctx, http.MethodGet, c.baseURL+pluginapi.PathHealth, nil, http.StatusOK)
	if err != nil {
		return pluginapi.Health{}, err
	}
	var h pluginapi.Health
	if err := json.Unmarshal(data, &h); err != nil {
		return pluginapi.Health{}, fmt.Errorf("decode health: %w", err)
	}
	return h, nil
}

func (c *Plugin) StartJob(ctx context.Context, action string, req pluginapi.JobRequest) (pluginapi.JobState, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return pluginapi.JobState{}, fmt.Errorf("encode job request: %w", err)
	}
	target := c.baseURL + strings.ReplaceAll(pluginapi.PathStartJob, "{action}", url.PathEscape(action))
	data, err := c.call(ctx, http.MethodPost, target, body, http.StatusOK, http.StatusAccepted)
	if err != nil {
		return pluginapi.JobState{}, err
	}
	var st pluginapi.JobState
	if err := json.Unmarshal(data, &st); err != nil {
		return pluginapi.JobState{}, fmt.Errorf("decode job state: %w", err)
	}
	return st, nil
}

func (c *Plugin) GetJob(ctx context.Context, jobID string) (pluginapi.JobState, error) {
	data, err := c.call(ctx, http.MethodGet, c.jobURL(pluginapi.PathJob, jobID), nil, http.StatusOK)
	if err != nil {
		return pluginapi.JobState{}, err
	}
	var st pluginapi.JobState
	if err := json.Unmarshal(data, &st); err != nil {
		return pluginapi.JobState{}, fmt.Errorf("decode job state: %w", err)
	}
	return st, nil
}

func (c *Plugin) CancelJob(ctx context.Context, jobID string) (pluginapi.JobState, error) {
	data, err := c.call(ctx, http.MethodPost, c.jobURL(pluginapi.PathCancelJob, jobID), nil, http.StatusOK, http.StatusAccepted)
	if err != nil {
		return pluginapi.JobState{}, err
	}
	var st pluginapi.JobState
	if err := json.Unmarshal(data, &st); err != nil {
		return pluginapi.JobState{}, fmt.Errorf("decode job state: %w", err)
	}
	return st, nil
}

func (c *Plugin) StreamEvents(ctx context.Context, jobID string, after int64, fn func(pluginapi.Event) error) error {
	target := fmt.Sprintf("%s?after=%d", c.jobURL(pluginapi.PathJobEvents, jobID), after)
	req, err := c.newRequest(ctx, http.MethodGet, target, nil, mimeEventStream)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", pluginapi.ErrUpstream, err)
	}
	if resp.StatusCode != http.StatusOK {
		return failure(resp)
	}
	return closeBody(resp.Body, readEvents(ctx, resp.Body, fn))
}

func readLine(br *bufio.Reader, budget int64) (string, error) {
	var line []byte
	for {
		chunk, err := br.ReadSlice('\n')
		line = append(line, chunk...)
		if int64(len(line)) > budget {
			return "", fmt.Errorf("event exceeds %d bytes: %w", pluginapi.DefaultMaxBodyBytes, pluginapi.ErrPayloadTooLarge)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return string(line), err
	}
}

func readEvents(ctx context.Context, r io.Reader, fn func(pluginapi.Event) error) error {
	br := bufio.NewReader(r)
	var data []string
	var size int64
	for {
		line, readErr := readLine(br, pluginapi.DefaultMaxBodyBytes-size)
		atEOF := errors.Is(readErr, io.EOF)
		if readErr != nil && !atEOF {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return readErr
		}
		line = strings.TrimRight(line, "\r\n")
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			data = append(data, strings.TrimPrefix(rest, " "))
			size += int64(len(rest)) + 1
		}
		if (line == "" || atEOF) && len(data) > 0 {
			var ev pluginapi.Event
			if err := json.Unmarshal([]byte(strings.Join(data, "\n")), &ev); err != nil {
				return fmt.Errorf("decode event: %w", err)
			}
			data = nil
			size = 0
			if err := fn(ev); err != nil {
				return err
			}
		}
		if atEOF {
			return nil
		}
	}
}

func (c *Plugin) SendMessage(ctx context.Context, jobID string, m pluginapi.Message) error {
	body, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	_, err = c.call(ctx, http.MethodPost, c.jobURL(pluginapi.PathJobMessages, jobID), body, http.StatusAccepted)
	return err
}

type Core struct {
	baseURL string
	plugin  string
	token   string
	http    *http.Client
}

func NewCore(baseURL, plugin, token string, hc *http.Client) *Core {
	return &Core{baseURL: baseURL, plugin: plugin, token: token, http: clientOrDefault(hc)}
}

func (c *Core) EmitTrigger(ctx context.Context, e pluginapi.TriggerEvent) (pluginapi.TriggerAccepted, error) {
	body, err := json.Marshal(e)
	if err != nil {
		return pluginapi.TriggerAccepted{}, fmt.Errorf("encode trigger: %w", err)
	}
	req, err := newRequest(ctx, http.MethodPost, c.baseURL+pluginapi.CorePathTrigger, body, mimeJSON)
	if err != nil {
		return pluginapi.TriggerAccepted{}, err
	}
	req.Header.Set(pluginapi.HeaderAuthorization, pluginapi.BearerPrefix+c.token)
	req.Header.Set(pluginapi.HeaderPluginName, c.plugin)
	data, err := doRequest(c.http, req, []int{http.StatusAccepted})
	if err != nil {
		return pluginapi.TriggerAccepted{}, err
	}
	var acc pluginapi.TriggerAccepted
	if err := json.Unmarshal(data, &acc); err != nil {
		return pluginapi.TriggerAccepted{}, fmt.Errorf("decode trigger response: %w", err)
	}
	return acc, nil
}

func (c *Core) postCallback(ctx context.Context, target string, body []byte) error {
	req, err := newRequest(ctx, http.MethodPost, target, body, mimeJSON)
	if err != nil {
		return err
	}
	_, err = doRequest(c.http, req, nil)
	return err
}

func (c *Core) Complete(ctx context.Context, cb pluginapi.Callback, r pluginapi.Result) error {
	body, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	return c.postCallback(ctx, cb.ResultURL, body)
}

func (c *Core) Progress(ctx context.Context, cb pluginapi.Callback, p pluginapi.Progress) error {
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("encode progress: %w", err)
	}
	return c.postCallback(ctx, cb.ProgressURL, body)
}
