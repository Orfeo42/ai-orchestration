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

func (c Config) Path(root string) string {
	if root == "" {
		root = DefaultRoot
	}
	parts := strings.SplitN(c.Repo, "/", 2)
	if len(parts) == 2 {
		return root + "/" + parts[1]
	}
	return root + "/" + c.Repo
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
