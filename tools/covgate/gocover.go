package main

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// goBlock is one basic block of a Go coverage profile.
type goBlock struct {
	File                         string
	StartLine, StartCol, EndLine int
	EndCol, Statements, Count    int
}

var profileLine = regexp.MustCompile(`^(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$`)

// parseGoProfile reads a text coverage profile (go test -coverprofile or
// go tool covdata textfmt). Blocks reported by several test binaries are
// merged by position, keeping the highest count.
func parseGoProfile(path, module string) (map[string]*goBlock, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	blocks := map[string]*goBlock{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		m := profileLine.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		n := make([]int, 6)
		for i := range n {
			n[i], _ = strconv.Atoi(m[i+2])
		}
		file := strings.TrimPrefix(m[1], module+"/")
		key := fmt.Sprintf("%s:%d.%d,%d.%d", file, n[0], n[1], n[2], n[3])
		b, ok := blocks[key]
		if !ok {
			b = &goBlock{File: file, StartLine: n[0], StartCol: n[1], EndLine: n[2], EndCol: n[3], Statements: n[4]}
			blocks[key] = b
		}
		if n[5] > b.Count {
			b.Count = n[5]
		}
	}
	return blocks, sc.Err()
}

// goStatementElements expands blocks into one element per statement.
func goStatementElements(blocks map[string]*goBlock) []Element {
	keys := make([]string, 0, len(blocks))
	for k := range blocks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []Element
	for _, k := range keys {
		b := blocks[k]
		for i := 0; i < b.Statements; i++ {
			out = append(out, Element{Metric: "statements", Key: b.File, Label: fmt.Sprintf("%s:%d", b.File, b.StartLine), Covered: b.Count > 0})
		}
	}
	return out
}

// funcCovered reports whether any block inside [start,end] lines of file ran.
func funcCovered(blocks map[string]*goBlock, file string, start, end int) bool {
	for _, b := range blocks {
		if b.File == file && b.StartLine >= start && b.EndLine <= end && b.Count > 0 {
			return true
		}
	}
	return false
}

// queryMethods returns the line range of every method of the sqlc Queries type in a generated file.
func queryMethods(path string) (map[string][2]int, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := map[string][2]int{}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
			continue
		}
		if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
			if id, ok := star.X.(*ast.Ident); ok && id.Name == "Queries" {
				out[fn.Name.Name] = [2]int{fset.Position(fn.Pos()).Line, fset.Position(fn.End()).Line}
			}
		}
	}
	return out, nil
}

// decisionPoints counts if/case/&&/|| decision points of non-test Go files
// matching include — the complementary branch inventory of the backend Unit
// gate (Go's native tooling does not measure branch coverage).
func decisionPoints(root string, include func(string) bool) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if !include(filepath.ToSlash(rel)) {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.IfStmt:
				count += 2
			case *ast.CaseClause, *ast.CommClause:
				count++
			case *ast.BinaryExpr:
				if x.Op == token.LAND || x.Op == token.LOR {
					count++
				}
			}
			return true
		})
		return nil
	})
	return count, err
}

// missingPackages reports product packages (backend/cmd, backend/internal)
// with executable code that are absent from a profile: code no test binary
// links would otherwise silently fall out of the denominator.
func missingPackages(backendRoot string, blocks map[string]*goBlock) []string {
	present := map[string]bool{}
	for _, b := range blocks {
		present[filepath.Dir(b.File)] = true
	}
	var out []string
	for _, top := range []string{"cmd", "internal"} {
		_ = filepath.WalkDir(filepath.Join(backendRoot, top), func(p string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(backendRoot, p)
			rel = filepath.ToSlash(rel)
			if !present[rel] && hasFuncBodies(p) {
				out = append(out, "package "+rel+" has executable code but is missing from the profile (no test links it)")
			}
			return nil
		})
	}
	return out
}

func hasFuncBodies(dir string) bool {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		if err != nil {
			continue
		}
		for _, d := range parsed.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
				return true
			}
		}
	}
	return false
}
