package projects

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const DefaultRoot = "/projects"

type Config struct {
	Repo       string `json:"repo"`
	BaseBranch string `json:"base_branch"`
}

func (c Config) Path(root, workerID string) string {
	if root == "" {
		root = DefaultRoot
	}
	repoName := c.Repo
	if parts := strings.SplitN(c.Repo, "/", 2); len(parts) == 2 {
		repoName = parts[1]
	}
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
