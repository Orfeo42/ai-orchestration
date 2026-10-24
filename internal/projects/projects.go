package projects

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
)

type projectsError string

func (e projectsError) Error() string { return string(e) }

const ErrInvalidRepo projectsError = "invalid repo, expected owner/name"

const DefaultRoot = "/projects"

const defaultMemoryKeyPrefix = "HOME-sviluppo-ionstream-"

type Config struct {
	Repo       string `json:"repo"`
	BaseBranch string `json:"base_branch"`
	MemoryKey  string `json:"memory_key,omitempty"`
	Label      string `json:"label,omitempty"`
}

type Repo struct {
	Owner string
	Name  string
}

func SplitRepo(repo string) (Repo, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return Repo{}, fmt.Errorf("%w: %q", ErrInvalidRepo, repo)
	}
	return Repo{Owner: owner, Name: name}, nil
}

func (c Config) repoName() string {
	if parts := strings.SplitN(c.Repo, "/", 2); len(parts) == 2 {
		return parts[1]
	}
	return c.Repo
}

func (c Config) ResolvedMemoryKey() string {
	if c.MemoryKey != "" {
		return c.MemoryKey
	}
	return defaultMemoryKeyPrefix + c.repoName()
}

func (c Config) Path(root, workerID string) string {
	if root == "" {
		root = DefaultRoot
	}
	repoName := c.repoName()
	if workerID == "" {
		return root + "/" + repoName
	}
	return root + "/" + workerID + "/" + repoName
}

func Load(path string) (map[string]Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read projects config: %w", err)
	}
	var out map[string]Config
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse projects config: %w", err)
	}
	return out, nil
}

func ProjectForLabels(projs map[string]Config, labels []string) (string, bool) {
	keys := make([]string, 0, len(projs))
	for key := range projs {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		label := projs[key].Label
		if label != "" && slices.Contains(labels, label) {
			return key, true
		}
	}
	return "", false
}
