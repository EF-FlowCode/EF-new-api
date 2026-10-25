// Command release-impact decides whether an application change can reach the
// rolling worker. Blue-green releases keep the old worker image, so a backend
// change may skip maintenance only when nothing the worker can execute changed.
//
// It compares two source trees declaration by declaration (functions, methods,
// types, package-level vars/consts, init functions), builds an RTA call graph
// of the head tree rooted at the worker's startup path, and reports every
// changed declaration the worker can reach or use. Any load, parse or shape
// error exits non-zero; the caller must then treat the change as maintenance.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/tools/go/callgraph/rta"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

// Bump when the analysis rules change; the release plan binds this value.
const analysisVersion = 2

type decl struct {
	Kind string // func, type, var, const, init
	Hash string
	// Calls marks a var initializer that calls a function: it runs in every
	// process, whatever reads the variable.
	Calls bool
}

type finding struct {
	Key    string `json:"key"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type report struct {
	Version        int       `json:"analysis_version"`
	Changed        []finding `json:"changed"`
	WorkerAffected []finding `json:"worker_affected"`
	Roots          int       `json:"worker_roots"`
	Reachable      int       `json:"worker_reachable_functions"`
}

func main() {
	base := flag.String("base", "", "source tree the running worker was built from")
	head := flag.String("head", "", "candidate source tree")
	flag.Parse()
	if *base == "" || *head == "" {
		fail("both -base and -head are required")
	}
	before, err := scanTree(*base)
	if err != nil {
		fail("scan base: " + err.Error())
	}
	after, err := scanTree(*head)
	if err != nil {
		fail("scan head: " + err.Error())
	}
	changed := diff(before, after)
	rep := report{Version: analysisVersion, Changed: changed, WorkerAffected: []finding{}}
	if len(changed) > 0 {
		affected, roots, reachable, err := workerImpact(*head, changed, before, after)
		if err != nil {
			fail("worker reachability: " + err.Error())
		}
		rep.WorkerAffected, rep.Roots, rep.Reachable = affected, roots, reachable
	}
	out, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Println(string(out))
}

func fail(message string) {
	fmt.Fprintln(os.Stderr, "release-impact: "+message)
	os.Exit(1)
}

// ---------------------------------------------------------------- declarations

// buildContext matches the production image: linux, no cgo.
func buildContext() build.Context {
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = "linux", "amd64", false
	return ctx
}

// modules maps every go.mod directory (relative, "." for root) to its module path.
func modules(root string) (map[string]string, error) {
	found := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && skipDir(d.Name()) {
			return filepath.SkipDir
		}
		if d.IsDir() || d.Name() != "go.mod" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		mod := modfile.ModulePath(data)
		if mod == "" {
			return fmt.Errorf("%s has no module path", path)
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		found[filepath.ToSlash(rel)] = mod
		return nil
	})
	if found["."] == "" {
		return nil, fmt.Errorf("root go.mod missing")
	}
	return found, err
}

func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
		name == "testdata" || name == "vendor" || name == "node_modules" || name == "web" || name == "scripts"
}

func importPath(mods map[string]string, rel string) string {
	for dir := rel; ; dir = filepath.ToSlash(filepath.Dir(dir)) {
		if mod, ok := mods[dir]; ok {
			if dir == "." {
				if rel == "." {
					return mod
				}
				return mod + "/" + rel
			}
			if rel == dir {
				return mod
			}
			return mod + "/" + strings.TrimPrefix(rel, dir+"/")
		}
		if dir == "." {
			return ""
		}
	}
}

func scanTree(root string) (map[string]decl, error) {
	mods, err := modules(root)
	if err != nil {
		return nil, err
	}
	ctx := buildContext()
	decls := map[string]decl{}
	methods := map[string][]string{} // type key -> method names
	inits := map[string][]string{}   // package -> init body hashes
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir, name := filepath.Split(path)
		if ok, err := ctx.MatchFile(dir, name); err != nil || !ok {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.Dir(path))
		pkg := importPath(mods, filepath.ToSlash(rel))
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		if file.Name.Name == "main" && pkg != mods["."] {
			pkg += "#main" // separate binaries never run inside the server
		}
		imports := fileImports(file)
		for _, item := range file.Decls {
			switch item := item.(type) {
			case *ast.FuncDecl:
				item.Doc = nil
				hash := hashNode(fset, item, imports)
				if item.Recv == nil && item.Name.Name == "init" {
					inits[pkg] = append(inits[pkg], hash)
					continue
				}
				key := pkg + "." + item.Name.Name
				if item.Recv != nil && len(item.Recv.List) == 1 {
					recv := receiverName(item.Recv.List[0].Type)
					key = pkg + "." + recv + "." + item.Name.Name
					methods[pkg+"."+recv] = append(methods[pkg+"."+recv], item.Name.Name)
				}
				decls[key] = decl{Kind: "func", Hash: hash}
			case *ast.GenDecl:
				recordGenDecl(fset, item, pkg, imports, decls)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// A method set change alters interface satisfaction without touching the type spec.
	for key, names := range methods {
		if d, ok := decls[key]; ok {
			sort.Strings(names)
			d.Hash = sum(d.Hash + "|" + strings.Join(names, ","))
			decls[key] = d
		}
	}
	for pkg, hashes := range inits {
		sort.Strings(hashes)
		decls[pkg+".init"] = decl{Kind: "init", Hash: sum(strings.Join(hashes, "|"))}
	}
	return decls, nil
}

func recordGenDecl(fset *token.FileSet, gen *ast.GenDecl, pkg string, imports map[string]string, decls map[string]decl) {
	gen.Doc = nil
	switch gen.Tok {
	case token.TYPE:
		for _, spec := range gen.Specs {
			spec := spec.(*ast.TypeSpec)
			spec.Doc, spec.Comment = nil, nil
			decls[pkg+"."+spec.Name.Name] = decl{Kind: "type", Hash: hashNode(fset, spec, imports)}
		}
	case token.CONST:
		// iota and implicit repetition tie every name to the whole block.
		for _, spec := range gen.Specs {
			spec := spec.(*ast.ValueSpec)
			spec.Doc, spec.Comment = nil, nil
		}
		hash := hashNode(fset, gen, imports)
		for _, spec := range gen.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				if name.Name != "_" {
					decls[pkg+"."+name.Name] = decl{Kind: "const", Hash: hash}
				}
			}
		}
	case token.VAR:
		for _, spec := range gen.Specs {
			spec := spec.(*ast.ValueSpec)
			spec.Doc, spec.Comment = nil, nil
			hash := hashNode(fset, spec, imports)
			calls := false
			for _, value := range spec.Values {
				ast.Inspect(value, func(n ast.Node) bool {
					if _, ok := n.(*ast.CallExpr); ok {
						calls = true
					}
					return !calls
				})
			}
			for _, name := range spec.Names {
				key := pkg + "." + name.Name
				if name.Name == "_" {
					// Blank vars exist only for their initializer's side effects.
					key = fmt.Sprintf("%s._#%s", pkg, hash[:12])
				}
				decls[key] = decl{Kind: "var", Hash: hash, Calls: calls}
			}
		}
	}
}

func receiverName(expr ast.Expr) string {
	for {
		switch t := expr.(type) {
		case *ast.StarExpr:
			expr = t.X
		case *ast.IndexExpr:
			expr = t.X
		case *ast.IndexListExpr:
			expr = t.X
		case *ast.ParenExpr:
			expr = t.X
		case *ast.Ident:
			return t.Name
		default:
			return fmt.Sprintf("%T", expr)
		}
	}
}

// fileImports maps the local name of every import to its path.
func fileImports(file *ast.File) map[string]string {
	imports := map[string]string{}
	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		name := path[strings.LastIndex(path, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports[name] = path
	}
	return imports
}

// hashNode hashes the printed declaration (comments dropped) plus the import
// paths behind every package qualifier it uses, so re-pointing an import
// alias changes the hash even when the code text does not.
func hashNode(fset *token.FileSet, node ast.Node, imports map[string]string) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, node); err != nil {
		fail("print declaration: " + err.Error())
	}
	used := map[string]bool{}
	ast.Inspect(node, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				if path, ok := imports[id.Name]; ok {
					used[id.Name+"="+path] = true
				}
			}
		}
		return true
	})
	if path, ok := imports["."]; ok {
		used[".="+path] = true
	}
	qualifiers := make([]string, 0, len(used))
	for q := range used {
		qualifiers = append(qualifiers, q)
	}
	sort.Strings(qualifiers)
	return sum(buf.String() + "\x00" + strings.Join(qualifiers, ","))
}

func sum(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

func diff(before, after map[string]decl) []finding {
	var changed []finding
	for key, b := range before {
		if a, ok := after[key]; !ok {
			changed = append(changed, finding{Key: key, Kind: b.Kind, Reason: "removed"})
		} else if a.Hash != b.Hash {
			changed = append(changed, finding{Key: key, Kind: a.Kind, Reason: "modified"})
		}
	}
	for key, a := range after {
		if _, ok := before[key]; !ok {
			changed = append(changed, finding{Key: key, Kind: a.Kind, Reason: "added"})
		}
	}
	sort.Slice(changed, func(i, j int) bool { return changed[i].Key < changed[j].Key })
	return changed
}

// ---------------------------------------------------------------- reachability

func workerImpact(root string, changed []finding, before, after map[string]decl) ([]finding, int, int, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
			packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes,
		Dir: root,
		Env: append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0", "GOFLAGS=-mod=mod"),
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, 0, 0, err
	}
	if len(pkgs) != 1 || pkgs[0].Name != "main" {
		return nil, 0, 0, fmt.Errorf("expected the server main package at the module root")
	}
	var loadErrors []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			loadErrors = append(loadErrors, e.Error())
		}
	})
	if len(loadErrors) > 0 {
		return nil, 0, 0, fmt.Errorf("package errors: %s", strings.Join(loadErrors, "; "))
	}
	prog, ssaPkgs := ssautil.AllPackages(pkgs, ssa.InstantiateGenerics)
	prog.Build()

	byTypes := map[*types.Package]*packages.Package{}
	packages.Visit(pkgs, nil, func(p *packages.Package) { byTypes[p.Types] = p })
	mainPkg := pkgs[0]
	roots, err := workerRoots(prog, mainPkg)
	if err != nil {
		return nil, 0, 0, err
	}
	for _, p := range ssaPkgs {
		if p != nil {
			if init := p.Func("init"); init != nil {
				roots = append(roots, init)
			}
		}
	}
	result := rta.Analyze(roots, false)

	modulePrefix := mainPkg.PkgPath
	inModule := func(pkg *types.Package) bool {
		return pkg != nil && (pkg.Path() == modulePrefix || strings.HasPrefix(pkg.Path(), modulePrefix+"/"))
	}
	reachedFuncs := map[string]bool{}
	used := map[string]bool{}
	seenTypes := map[types.Type]bool{}
	var addType func(types.Type)
	addType = func(t types.Type) {
		if t == nil || seenTypes[t] {
			return
		}
		seenTypes[t] = true
		switch t := t.(type) {
		case *types.Named:
			if obj := t.Obj(); inModule(obj.Pkg()) {
				used[obj.Pkg().Path()+"."+obj.Name()] = true
			}
			for i := 0; i < t.TypeArgs().Len(); i++ {
				addType(t.TypeArgs().At(i))
			}
			addType(t.Underlying())
		case *types.Alias:
			if obj := t.Obj(); inModule(obj.Pkg()) {
				used[obj.Pkg().Path()+"."+obj.Name()] = true
			}
			addType(types.Unalias(t))
		case *types.Pointer:
			addType(t.Elem())
		case *types.Slice:
			addType(t.Elem())
		case *types.Array:
			addType(t.Elem())
		case *types.Map:
			addType(t.Key())
			addType(t.Elem())
		case *types.Chan:
			addType(t.Elem())
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				addType(t.Field(i).Type())
			}
		case *types.Signature:
			addType(t.Params())
			addType(t.Results())
			if t.Recv() != nil {
				addType(t.Recv().Type())
			}
		case *types.Tuple:
			for i := 0; i < t.Len(); i++ {
				addType(t.At(i).Type())
			}
		case *types.Interface:
			for i := 0; i < t.NumMethods(); i++ {
				addType(t.Method(i).Type())
			}
		}
	}
	addObject := func(obj types.Object) {
		if obj == nil || !inModule(obj.Pkg()) {
			return
		}
		switch obj := obj.(type) {
		case *types.TypeName:
			addType(obj.Type())
		case *types.Const:
			if obj.Parent() == obj.Pkg().Scope() {
				used[obj.Pkg().Path()+"."+obj.Name()] = true
			}
		case *types.Var:
			if obj.Parent() == obj.Pkg().Scope() {
				used[obj.Pkg().Path()+"."+obj.Name()] = true
			}
			addType(obj.Type())
		}
	}
	scanned := map[ast.Node]bool{}
	for fn := range result.Reachable {
		top := fn
		for top.Parent() != nil {
			top = top.Parent()
		}
		if origin := top.Origin(); origin != nil {
			top = origin
		}
		obj, _ := top.Object().(*types.Func)
		if obj == nil || !inModule(obj.Pkg()) {
			continue
		}
		reachedFuncs[funcKey(obj)] = true
		addType(obj.Type())
		syntax := top.Syntax()
		info := byTypes[obj.Pkg()]
		if syntax == nil || info == nil || scanned[syntax] {
			continue
		}
		scanned[syntax] = true
		ast.Inspect(syntax, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				addObject(info.TypesInfo.Uses[n])
				addObject(info.TypesInfo.Defs[n])
			case ast.Expr:
				if tv, ok := info.TypesInfo.Types[n]; ok {
					addType(tv.Type)
				}
			}
			return true
		})
	}

	affected := []finding{}
	for _, c := range changed {
		reason := ""
		switch {
		case c.Kind == "init":
			reason = "package init runs in every process"
		case c.Kind == "func" && reachedFuncs[c.Key]:
			reason = "reachable from the worker"
		case c.Kind == "func" && c.Reason == "removed":
			// Callers of a removed function changed too; they carry the verdict.
		case (c.Kind == "type" || c.Kind == "var" || c.Kind == "const") && used[c.Key]:
			reason = "used by worker-reachable code"
		case c.Kind == "var" && (after[c.Key].Calls || before[c.Key].Calls):
			reason = "initializer calls a function in every process"
		case c.Kind == "type" && c.Reason == "removed":
			// Removing a type forces every user to change.
		}
		if reason != "" {
			affected = append(affected, finding{Key: c.Key, Kind: c.Kind, Reason: reason})
		}
	}
	affected = append(affected, sharedDataWriters(prog, changed, reachedFuncs, used, inModule)...)
	return affected, len(roots), len(result.Reachable), nil
}

func funcKey(fn *types.Func) string {
	sig := fn.Type().(*types.Signature)
	if recv := sig.Recv(); recv != nil {
		t := recv.Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		switch t := types.Unalias(t).(type) {
		case *types.Named:
			return fn.Pkg().Path() + "." + t.Origin().Obj().Name() + "." + fn.Name()
		case *types.Interface:
			return ""
		}
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

// workerRoots collects every function the worker process can call before it
// parks: main() up to and including the `DeploymentRole == "worker"` block,
// plus everything those statements reference (calls, go/defer, closures and
// function values). The HTTP router after that block never runs in a worker.
func workerRoots(prog *ssa.Program, pkg *packages.Package) ([]*ssa.Function, error) {
	var mainDecl *ast.FuncDecl
	for _, file := range pkg.Syntax {
		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "main" {
				mainDecl = fn
			}
		}
	}
	if mainDecl == nil || mainDecl.Body == nil {
		return nil, fmt.Errorf("func main not found")
	}
	cut := -1
	for i, stmt := range mainDecl.Body.List {
		if isWorkerExit(stmt) {
			if cut >= 0 {
				return nil, fmt.Errorf("multiple worker exit blocks in main")
			}
			cut = i
		}
	}
	if cut < 0 {
		return nil, fmt.Errorf("worker exit block (if common.DeploymentRole == \"worker\" { ... return }) not found in main")
	}
	seen := map[*ssa.Function]bool{}
	var roots []*ssa.Function
	add := func(fn *ssa.Function) {
		if fn != nil && !seen[fn] {
			seen[fn] = true
			roots = append(roots, fn)
		}
	}
	for _, stmt := range mainDecl.Body.List[:cut+1] {
		ast.Inspect(stmt, func(n ast.Node) bool {
			// Interface methods have no body; RTA resolves those calls from
			// the concrete types the reachable code creates.
			if id, ok := n.(*ast.Ident); ok {
				if fn, ok := pkg.TypesInfo.Uses[id].(*types.Func); ok {
					if recv := fn.Type().(*types.Signature).Recv(); recv == nil || !types.IsInterface(recv.Type()) {
						add(prog.FuncValue(fn))
					}
				}
			}
			return true
		})
	}
	// Closures declared in main itself before the cut are anonymous members of main.
	if mainFn := prog.FuncValue(pkg.Types.Scope().Lookup("main").(*types.Func)); mainFn != nil {
		for _, anon := range mainFn.AnonFuncs {
			if pos := anon.Pos(); pos.IsValid() && pos < mainDecl.Body.List[cut].End() {
				add(anon)
			}
		}
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("no worker roots found")
	}
	return roots, nil
}

func isWorkerExit(stmt ast.Stmt) bool {
	ifStmt, ok := stmt.(*ast.IfStmt)
	if !ok || ifStmt.Init != nil || ifStmt.Else != nil {
		return false
	}
	cond, ok := ifStmt.Cond.(*ast.BinaryExpr)
	if !ok || cond.Op != token.EQL {
		return false
	}
	sel, ok := cond.X.(*ast.SelectorExpr)
	lit, ok2 := cond.Y.(*ast.BasicLit)
	if !ok || !ok2 || sel.Sel.Name != "DeploymentRole" || lit.Value != `"worker"` {
		return false
	}
	last := ifStmt.Body.List
	if len(last) == 0 {
		return false
	}
	_, ret := last[len(last)-1].(*ast.ReturnStmt)
	return ret
}
