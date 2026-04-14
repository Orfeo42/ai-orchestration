package forgejo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ai-orchestration/internal/models"
)

const (
	LabelImplement     = "To-do"
	LabelInProgress    = "in-progress"
	LabelDone          = "done"
	LabelProjectPrefix = "project:"
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) GetIssues(owner, repo, label string, assignee *string) ([]models.ForgejoIssue, error) {
	params := url.Values{
		"state":  {"open"},
		"type":   {"issues"},
		"labels": {label},
		"limit":  {"50"},
	}
	if assignee != nil {
		params.Set("assigned_by", *assignee)
	}

	slog.Debug("fetching issues",
		"repo", owner+"/"+repo,
		"label", label,
	)

	body, err := c.get(fmt.Sprintf("/api/v1/repos/%s/%s/issues?%s", owner, repo, params.Encode()))
	if err != nil {
		return nil, err
	}

	var raw []struct {
		ID     int    `json:"id"`
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal issues: %w", err)
	}

	issues := make([]models.ForgejoIssue, len(raw))
	for i, r := range raw {
		labels := make([]string, len(r.Labels))
		for j, l := range r.Labels {
			labels[j] = l.Name
		}
		issues[i] = models.ForgejoIssue{
			ID:        r.ID,
			Number:    r.Number,
			Title:     r.Title,
			Body:      r.Body,
			RepoOwner: owner,
			RepoName:  repo,
			Labels:    labels,
		}
	}
	return issues, nil
}

func (c *Client) GetLabelID(owner, repo, name string) (*int, error) {
	body, err := c.get(fmt.Sprintf("/api/v1/repos/%s/%s/labels", owner, repo))
	if err != nil {
		return nil, err
	}

	var labels []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &labels); err != nil {
		return nil, fmt.Errorf("unmarshal labels: %w", err)
	}

	for _, l := range labels {
		if l.Name == name {
			id := l.ID
			return &id, nil
		}
	}
	return nil, nil
}

func (c *Client) AddLabel(owner, repo string, issueNumber, labelID int) error {
	payload, err := json.Marshal(map[string][]int{"labels": {labelID}})
	if err != nil {
		return err
	}
	return c.doRequest("POST", fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/labels", owner, repo, issueNumber), payload, nil)
}

func (c *Client) RemoveLabel(owner, repo string, issueNumber, labelID int) error {
	return c.doRequest("DELETE", fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/labels/%d", owner, repo, issueNumber, labelID), nil, nil)
}

func (c *Client) CreateComment(owner, repo string, issueNumber int, body string) error {
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return err
	}
	return c.doRequest("POST", fmt.Sprintf("/api/v1/repos/%s/%s/issues/%d/comments", owner, repo, issueNumber), payload, nil)
}

func (c *Client) CreatePR(owner, repo, title, body, head, base string) (models.ForgejoPR, error) {
	payload, err := json.Marshal(map[string]string{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	})
	if err != nil {
		return models.ForgejoPR{}, err
	}
	var pr models.ForgejoPR
	if err := c.doRequest("POST", fmt.Sprintf("/api/v1/repos/%s/%s/pulls", owner, repo), payload, &pr); err != nil {
		return models.ForgejoPR{}, err
	}
	return pr, nil
}

func (c *Client) UpdatePR(owner, repo string, prNumber int, body string) error {
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return err
	}
	return c.doRequest("PATCH", fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d", owner, repo, prNumber), payload, nil)
}

func (c *Client) GetPRDiff(owner, repo string, prNumber int) (string, error) {
	body, err := c.get(fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d.diff", owner, repo, prNumber))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (c *Client) CreatePRReview(owner, repo string, prNumber int, body string, approved bool) error {
	event := "REQUEST_CHANGES"
	if approved {
		event = "APPROVED"
	}
	payload, err := json.Marshal(map[string]string{"body": body, "event": event})
	if err != nil {
		return err
	}
	return c.doRequest("POST", fmt.Sprintf("/api/v1/repos/%s/%s/pulls/%d/reviews", owner, repo, prNumber), payload, nil)
}

func (c *Client) get(path string) ([]byte, error) {
	req, err := http.NewRequest("GET", c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "token "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: status %d: %s", path, resp.StatusCode, body)
	}
	return body, nil
}

func (c *Client) doRequest(method, path string, payload []byte, out any) error {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Authorization", "token "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, respBody)
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}
	return nil
}
