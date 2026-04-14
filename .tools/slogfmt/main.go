// slogfmt rewrites slog calls so each key-value argument pair is on its own
// line, satisfying the sloglint args-on-sep-lines rule.
//
// Only uses the Go standard library — no external dependencies.
//
// Usage (run from the repo root):
//
//	go run ./tools/slogfmt/main.go ./...
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// slogKVFuncs maps slog function names to the index at which key-value pairs
// begin (i.e. how many leading positional args precede the variadic kv pairs).
var slogKVFuncs = map[string]int{
	"Debug":        1, // slog.Debug(msg, args...)
	"Info":         1,
	"Warn":         1,
	"Error":        1,
	"DebugContext": 2, // slog.DebugContext(ctx, msg, args...)
	"InfoContext":  2,
	"WarnContext":  2,
	"ErrorContext": 2,
	"Log":          3, // slog.Log(ctx, level, msg, args...)
}

func main() {
	patterns := os.Args[1:]
	if len(patterns) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slogfmt ./... | ./path/to/pkg [...]")
		os.Exit(1)
	}

	files, err := collectGoFiles(patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	changed := 0

	for _, path := range files {
		ok, err := processFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: %v\n", path, err)

			continue
		}

		if ok {
			changed++
		}
	}

	fmt.Printf("done: %d file(s) rewritten\n", changed)
}

// ── file collection ──────────────────────────────────────────────────────────

func collectGoFiles(patterns []string) ([]string, error) {
	var files []string

	seen := map[string]bool{}

	add := func(path string) {
		abs, _ := filepath.Abs(path)
		if seen[abs] {
			return
		}

		seen[abs] = true

		files = append(files, path)
	}

	for _, pattern := range patterns {
		// Support both "./..." and "." style patterns.
		dir := strings.TrimSuffix(strings.TrimSuffix(pattern, "/..."), "...")
		if dir == "" {
			dir = "."
		}

		info, err := os.Stat(dir)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", dir, err)
		}

		if !info.IsDir() {
			if strings.HasSuffix(dir, ".go") {
				add(dir)
			}

			continue
		}

		if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if d.IsDir() && (d.Name() == "vendor" || d.Name() == ".git") {
				return filepath.SkipDir
			}

			if !d.IsDir() && strings.HasSuffix(path, ".go") {
				add(path)
			}

			return nil
		}); err != nil {
			return nil, err
		}
	}

	return files, nil
}

// ── per-file processing ──────────────────────────────────────────────────────

func processFile(path string) (changed bool, err error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return false, nil // skip files with syntax errors
	}

	type edit struct {
		startByte, endByte int
		replacement        []byte
	}

	var edits []edit

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		kvStart, isSlog := slogCallKVStart(call)
		if !isSlog {
			return true
		}
		// Need at least 2 args after the fixed leading ones to have a pair.
		if len(call.Args) <= kvStart+1 {
			return true
		}

		tf := fset.File(call.Pos())

		// Only rewrite if any two consecutive args share a line.
		needsRewrite := false

		for i := kvStart + 1; i < len(call.Args); i++ {
			if tf.Line(call.Args[i-1].Pos()) == tf.Line(call.Args[i].Pos()) {
				needsRewrite = true

				break
			}
		}

		if !needsRewrite {
			return true
		}

		startOff := tf.Offset(call.Pos())
		endOff := tf.Offset(call.End())

		rewritten, rerr := rewriteCall(src, call, fset)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "  skip %s:%d: %v\n", path, tf.Line(call.Pos()), rerr)

			return true
		}

		edits = append(edits, edit{startOff, endOff, rewritten})

		return true
	})

	if len(edits) == 0 {
		return false, nil
	}

	// Apply edits in reverse order so earlier byte offsets stay valid.
	result := make([]byte, len(src))
	copy(result, src)

	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		result = append(result[:e.startByte], append(e.replacement, result[e.endByte:]...)...)
	}

	// Run gofmt for clean, canonical indentation.
	if formatted, ferr := format.Source(result); ferr == nil {
		result = formatted
	}

	if bytes.Equal(src, result) {
		return false, nil
	}

	fmt.Printf("rewriting %s\n", path)

	return true, os.WriteFile(path, result, 0o644)
}

// ── AST helpers ──────────────────────────────────────────────────────────────

// slogCallKVStart returns the index at which kv pairs start for a slog.Xxx
// call, and whether this is a slog call at all.
func slogCallKVStart(call *ast.CallExpr) (kvStart int, ok bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return 0, false
	}

	pkg, isPkg := sel.X.(*ast.Ident)
	if !isPkg || pkg.Name != "slog" {
		return 0, false
	}

	kvStart, ok = slogKVFuncs[sel.Sel.Name]

	return kvStart, ok
}

// rewriteCall rebuilds the source of a slog call so that the fixed leading
// args stay on the first line and each key-value pair occupies one line:
//
//	slog.Error("msg", "k1", v1, "k2", v2)
//
// becomes:
//
//	slog.Error("msg",
//		"k1", v1,
//		"k2", v2,
//	)
//
// For context variants the ctx stays on the first line too:
//
//	slog.ErrorContext(ctx, "msg",
//		"k1", v1,
//	)
func rewriteCall(src []byte, call *ast.CallExpr, fset *token.FileSet) ([]byte, error) {
	tf := fset.File(call.Pos())

	kvStart, _ := slogCallKVStart(call)

	// Derive indentation from the call's position in its source line.
	callStartOff := tf.Offset(call.Pos())

	lineStart := callStartOff
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}

	raw := src[lineStart:callStartOff]
	indent := ""

	var indentSb250 strings.Builder

	for _, b := range raw {
		if b == '\t' || b == ' ' {
			indentSb250.WriteString(string(b))
		} else {
			break
		}
	}

	indent += indentSb250.String()

	innerIndent := indent + "\t"

	// Extract source bytes for each argument.
	argSrcs := make([][]byte, len(call.Args))
	for i, arg := range call.Args {
		start := tf.Offset(arg.Pos())
		end := tf.Offset(arg.End())
		argSrcs[i] = bytes.TrimSpace(src[start:end])
	}

	// Extract the function expression (e.g. "slog.ErrorContext").
	funSrc := src[tf.Offset(call.Fun.Pos()):tf.Offset(call.Fun.End())]

	var buf bytes.Buffer

	// First line: funcName(fixedArg0, fixedArg1, ..., fixedArgN-1,
	buf.Write(funSrc)
	buf.WriteByte('(')

	for i := 0; i < kvStart; i++ {
		buf.Write(argSrcs[i])
		buf.WriteString(", ")
	}

	kvArgs := argSrcs[kvStart:]

	if len(kvArgs) == 0 {
		// No kv pairs — shouldn't reach here, but be safe.
		buf.WriteByte(')')

		return buf.Bytes(), nil
	}

	// Each kv pair ("key", value) on its own indented line.
	// kvArgs should always be even (key, val, key, val...) but handle odd
	// counts gracefully by treating the last lone arg as its own line.
	buf.WriteByte('\n')

	for i := 0; i < len(kvArgs); i += 2 {
		buf.WriteString(innerIndent)
		buf.Write(kvArgs[i]) // key

		if i+1 < len(kvArgs) {
			buf.WriteString(", ")
			buf.Write(kvArgs[i+1]) // value
		}

		buf.WriteString(",\n")
	}

	buf.WriteString(indent)
	buf.WriteByte(')')

	return buf.Bytes(), nil
}
