package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func TestLoadDir(t *testing.T) {
	reg := newTestRegistry(t)

	t.Run("loads yaml and yml sorted by filename", func(t *testing.T) {
		dir := t.TempDir()
		second := strings.Replace(exampleYAML, "name: implement-issue", "name: second", 1)
		writeFile(t, dir, "b.yml", second)
		writeFile(t, dir, "a.yaml", exampleYAML)
		writeFile(t, dir, "notes.txt", "ignored")
		require.NoError(t, os.Mkdir(filepath.Join(dir, "sub.yaml"), 0o750))

		sources, err := LoadDir(dir, reg)
		require.NoError(t, err)
		require.Len(t, sources, 2)
		assert.Equal(t, filepath.Join(dir, "a.yaml"), sources[0].Path)
		assert.Equal(t, "implement-issue", sources[0].Definition.Name)
		assert.Equal(t, []byte(exampleYAML), sources[0].YAML)
		assert.Equal(t, Hash([]byte(exampleYAML)), sources[0].Hash)
		assert.Equal(t, filepath.Join(dir, "b.yml"), sources[1].Path)
		assert.Equal(t, "second", sources[1].Definition.Name)
	})

	t.Run("duplicate name rejected", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "a.yaml", exampleYAML)
		writeFile(t, dir, "b.yaml", exampleYAML)

		_, err := LoadDir(dir, reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "b.yaml")
		assert.Contains(t, err.Error(), "implement-issue")
	})

	t.Run("invalid file named in error", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "good.yaml", exampleYAML)
		writeFile(t, dir, "bad.yaml", strings.Replace(exampleYAML, "name: implement-issue", "name: bad", 1)+"extra: 1\n")

		_, err := LoadDir(dir, reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "bad.yaml")
	})

	t.Run("validation failure named in error", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "broken.yaml", strings.Replace(exampleYAML, "start: assess", "start: ghost", 1))

		_, err := LoadDir(dir, reg)
		require.ErrorIs(t, err, ErrInvalid)
		assert.Contains(t, err.Error(), "broken.yaml")
	})

	t.Run("missing dir", func(t *testing.T) {
		_, err := LoadDir(filepath.Join(t.TempDir(), "nope"), reg)
		require.Error(t, err)
	})
}
