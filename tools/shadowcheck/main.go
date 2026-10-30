// Command shadowcheck reports local declarations that shadow a name of an
// enclosing scope: a variable, a package-level declaration, an import or a
// builtin. Unlike go vet's shadow analyzer it also reports shadowing with
// another type. It exits with status 1 when it finds any. Run it from the
// module root:
//
//	go run ./tools/shadowcheck
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

type listedPackage struct {
	Dir, ImportPath                    string
	GoFiles, TestGoFiles, XTestGoFiles []string
}

type finding struct {
	pos   token.Position
	text  string
	after bool // the shadowed variable is used after the shadowing scope ends
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	out, err := exec.Command("go", "list", "-json", "./...").Output()
	if err != nil {
		fail(fmt.Errorf("go list: %w", err))
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil).(types.ImporterFrom)
	var findings []finding
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var l listedPackage
		if err = dec.Decode(&l); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			fail(err)
		}
		// The package with its internal tests, then its external tests.
		for _, names := range [][]string{append(append([]string{}, l.GoFiles...), l.TestGoFiles...), l.XTestGoFiles} {
			if len(names) > 0 {
				findings = append(findings, check(fset, imp, l, names)...)
			}
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i].pos, findings[j].pos
		return a.Filename < b.Filename || a.Filename == b.Filename && a.Line < b.Line
	})
	for _, f := range findings {
		rel, _ := filepath.Rel(root, f.pos.Filename)
		mark := ""
		if f.after {
			mark = " (used after the inner scope)"
		}
		fmt.Printf("%s:%d: %s%s\n", rel, f.pos.Line, f.text, mark)
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "shadowcheck:", err)
	os.Exit(2)
}

func check(fset *token.FileSet, imp types.ImporterFrom, l listedPackage, names []string) []finding {
	var files []*ast.File
	for _, name := range names {
		f, err := parser.ParseFile(fset, filepath.Join(l.Dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			fail(err)
		}
		files = append(files, f)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Implicits: map[ast.Node]types.Object{}}
	conf := types.Config{Importer: imp}
	pkg, err := conf.Check(l.ImportPath, fset, files, info)
	if err != nil {
		fail(err)
	}
	seen := map[token.Pos]bool{}
	var out []finding
	report := func(id *ast.Ident, obj types.Object) {
		if obj == nil || id.Name == "_" || seen[id.Pos()] || obj.Parent() == nil || obj.Parent() == pkg.Scope() || obj.Parent().Parent() == nil {
			return
		}
		if v, isVar := obj.(*types.Var); isVar && v.IsField() {
			return
		}
		scope, outer := obj.Parent().Parent().LookupParent(id.Name, obj.Pos())
		if outer == nil {
			return
		}
		seen[id.Pos()] = true
		what := "a builtin"
		switch {
		case scope == types.Universe:
		case scope == pkg.Scope():
			what = "a package-level declaration"
		default:
			if _, isImport := outer.(*types.PkgName); isImport {
				what = "an import"
			} else {
				what = fmt.Sprintf("a variable declared at line %d", fset.Position(outer.Pos()).Line)
			}
		}
		after := false
		if ov, isVar := outer.(*types.Var); isVar && scope != pkg.Scope() && scope != types.Universe {
			for use, used := range info.Uses {
				if used == ov && use.Pos() > obj.Parent().End() {
					after = true
				}
			}
		}
		out = append(out, finding{pos: fset.Position(id.Pos()), text: fmt.Sprintf("%s shadows %s", id.Name, what), after: after})
	}
	for id, obj := range info.Defs {
		switch obj.(type) {
		case *types.Label, *types.PkgName:
			continue
		}
		report(id, obj)
	}
	// switch v := x.(type) declares an implicit variable in every clause.
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			ts, isSwitch := n.(*ast.TypeSwitchStmt)
			if !isSwitch {
				return true
			}
			if as, isAssign := ts.Assign.(*ast.AssignStmt); isAssign {
				for _, c := range ts.Body.List {
					if obj := info.Implicits[c]; obj != nil {
						report(as.Lhs[0].(*ast.Ident), obj)
						break
					}
				}
			}
			return true
		})
	}
	return out
}
