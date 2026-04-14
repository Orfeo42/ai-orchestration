// slogctx upgrades context-less slog calls to their Context variant when a
// context.Context parameter is in scope.
//
// Example:
//
//	func Foo(ctx context.Context) {
//		slog.Error("something failed", "error", err)
//	}
//
// becomes:
//
//	func Foo(ctx context.Context) {
//		slog.ErrorContext(ctx, "something failed", "error", err)
//	}
//
// Only uses the Go standard library — no external dependencies.
//
// Usage (run from the repo root):
//
//	go run ./.tools/slogctx/main.go ./...
//	go run ./.tools/slogctx/main.go ./src/some/pkg
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

// contextFree maps context-less slog functions to their Context variant.
var contextFree = map[string]string{
	"Debug": "DebugContext",
	"Info":  "InfoContext",
	"Warn":  "WarnContext",
	"Error": "ErrorContext",
}

type edit struct {
	startByte, endByte int
	replacement        []byte
}

func main() {
	patterns := os.Args[1:]
	if len(patterns) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slogctx ./... | ./path/to/pkg [...]")
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
		return false, nil
	}

	var edits []edit

	ast.Inspect(file, func(n ast.Node) bool {
		var (
			body     *ast.BlockStmt
			funcType *ast.FuncType
		)

		switch fn := n.(type) {
		case *ast.FuncDecl:
			if fn.Body == nil {
				return true
			}

			body = fn.Body
			funcType = fn.Type
		case *ast.FuncLit:
			body = fn.Body
			funcType = fn.Type
		default:
			return true
		}

		// Find the context.Context parameter name for this function.
		ctxVar := findContextParam(funcType)
		if ctxVar == "" {
			// No context in scope — nothing to upgrade in this function.
			return true
		}

		ast.Inspect(body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}

			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "slog" {
				return true
			}

			contextVariant, needsUpgrade := contextFree[sel.Sel.Name]
			if !needsUpgrade {
				return true
			}

			tf := fset.File(call.Pos())
			startOff := tf.Offset(call.Pos())
			endOff := tf.Offset(call.End())

			edits = append(edits, edit{
				startByte:   startOff,
				endByte:     endOff,
				replacement: buildContextCall(src, call, fset, contextVariant, ctxVar),
			})

			return true
		})

		return true
	})

	if len(edits) == 0 {
		return false, nil
	}

	// Apply in reverse order so earlier offsets stay valid.
	result := make([]byte, len(src))
	copy(result, src)

	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		result = append(result[:e.startByte], append(e.replacement, result[e.endByte:]...)...)
	}

	if formatted, ferr := format.Source(result); ferr == nil {
		result = formatted
	}

	if bytes.Equal(src, result) {
		return false, nil
	}

	fmt.Printf("rewriting %s\n", path)

	return true, os.WriteFile(path, result, 0o644)
}

// ── helpers ──────────────────────────────────────────────────────────────────

// findContextParam returns the name of the first context.Context parameter
// in the function signature, or "" if none exists.
func findContextParam(ft *ast.FuncType) string {
	if ft == nil || ft.Params == nil {
		return ""
	}

	for _, field := range ft.Params.List {
		if isContextType(field.Type) && len(field.Names) > 0 {
			return field.Names[0].Name
		}
	}

	return ""
}

// isContextType returns true if the expression is context.Context.
func isContextType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	pkg, ok := sel.X.(*ast.Ident)

	return ok && pkg.Name == "context" && sel.Sel.Name == "Context"
}

// buildContextCall rewrites slog.Error(msg, args...) →
// slog.ErrorContext(ctxVar, msg, args...) preserving original arg formatting.
func buildContextCall(src []byte, call *ast.CallExpr, fset *token.FileSet, newFunc, ctxVar string) []byte {
	tf := fset.File(call.Pos())

	argSrcs := make([][]byte, len(call.Args))
	for i, arg := range call.Args {
		start := tf.Offset(arg.Pos())
		end := tf.Offset(arg.End())
		argSrcs[i] = bytes.TrimSpace(src[start:end])
	}

	var buf bytes.Buffer
	buf.WriteString("slog.")
	buf.WriteString(newFunc)
	buf.WriteByte('(')
	buf.WriteString(ctxVar)

	for _, a := range argSrcs {
		buf.WriteString(", ")
		buf.Write(a)
	}

	buf.WriteByte(')')

	return buf.Bytes()
}
