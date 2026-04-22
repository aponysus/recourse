package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type reasonSet struct {
	Static   map[string]struct{}
	Patterns map[string]struct{}
}

type constValue struct {
	Name  string
	Value string
}

type structField struct {
	Name  string
	Type  string
	JSON  string
	Notes string
}

type defaultExecutorConfig struct {
	Observer          string
	DefaultClassifier string
	Budgets           []string
	Triggers          []string
}

type executorFallbacks struct {
	Defaults           map[string]string
	ClassifierBuiltins bool
}

func newReasonSet() reasonSet {
	return reasonSet{
		Static:   make(map[string]struct{}),
		Patterns: make(map[string]struct{}),
	}
}

func main() {
	var reasonsOut string
	var policyOut string
	var defaultsOut string
	flag.StringVar(&reasonsOut, "reasons-out", "docs/reference/reason-codes.md", "output markdown path for reason codes")
	flag.StringVar(&policyOut, "policy-out", "docs/reference/policy-schema.md", "output markdown path for policy schema")
	flag.StringVar(&defaultsOut, "defaults-out", "docs/reference/defaults-safety.md", "output markdown path for defaults and safety model")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		fail(err)
	}

	if err := generateReasonCodes(root, reasonsOut); err != nil {
		fail(err)
	}
	if err := generatePolicySchema(root, policyOut); err != nil {
		fail(err)
	}
	if err := generateDefaultsSafety(root, defaultsOut); err != nil {
		fail(err)
	}
}

func generateReasonCodes(root, outPath string) error {
	budgetReasons, err := collectReasonConsts(filepath.Join(root, "budget", "reasons.go"))
	if err != nil {
		return err
	}
	circuitReasons, err := collectReasonConsts(filepath.Join(root, "circuit", "types.go"))
	if err != nil {
		return err
	}

	outcomeReasons := newReasonSet()
	paths := []string{
		filepath.Join(root, "classify"),
		filepath.Join(root, "retry"),
		filepath.Join(root, "integrations", "grpc"),
	}
	for _, dir := range paths {
		files, err := goFiles(dir)
		if err != nil {
			return err
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			if err := collectReasonAssignments(file, &outcomeReasons); err != nil {
				return err
			}
		}
	}

	modeReasons := make(map[string]struct{})
	modeStrings, err := collectFailureModeStrings(filepath.Join(root, "retry", "executor.go"))
	if err != nil {
		return err
	}
	for _, m := range modeStrings {
		modeReasons[m] = struct{}{}
	}

	modeAssignments, err := collectModeAssignments(filepath.Join(root, "retry", "budget.go"))
	if err != nil {
		return err
	}
	for _, m := range modeAssignments {
		modeReasons[m] = struct{}{}
	}

	structs, err := collectStructFields(filepath.Join(root, "observe", "types.go"), []string{"Timeline", "AttemptRecord", "BudgetDecisionEvent"})
	if err != nil {
		return err
	}

	content, err := renderReasonsMarkdown(budgetReasons, circuitReasons, outcomeReasons, modeReasons, structs)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, content, 0o644)
}

func generatePolicySchema(root, outPath string) error {
	structs := make(map[string][]structField)

	keyStructs, err := collectStructFields(filepath.Join(root, "policy", "key.go"), []string{"PolicyKey"})
	if err != nil {
		return err
	}
	mergeStructs(structs, keyStructs)

	schemaStructs, err := collectStructFields(filepath.Join(root, "policy", "schema.go"), []string{
		"BudgetRef",
		"RetryPolicy",
		"HedgePolicy",
		"CircuitPolicy",
		"NormalizationInfo",
		"Metadata",
		"EffectivePolicy",
	})
	if err != nil {
		return err
	}
	mergeStructs(structs, schemaStructs)

	defaults, err := collectDefaultPolicyValues(filepath.Join(root, "policy", "schema.go"))
	if err != nil {
		return err
	}

	jitterValues, err := collectTypedConstValues(filepath.Join(root, "policy", "schema.go"), "JitterKind")
	if err != nil {
		return err
	}
	policySources, err := collectTypedConstValues(filepath.Join(root, "policy", "schema.go"), "PolicySource")
	if err != nil {
		return err
	}

	limits, err := collectConstValues(filepath.Join(root, "policy", "schema.go"), []string{
		"maxRetryAttempts",
		"maxHedges",
		"minBackoffFloor",
		"minHedgeDelayFloor",
		"maxBackoffCeiling",
		"minTimeoutFloor",
		"maxBackoffMultiplier",
		"minCircuitThreshold",
		"minCircuitCooldown",
	})
	if err != nil {
		return err
	}

	content, err := renderPolicySchemaMarkdown(structs, defaults, jitterValues, policySources, limits)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, content, 0o644)
}

func generateDefaultsSafety(root, outPath string) error {
	defaults, err := collectDefaultPolicyValues(filepath.Join(root, "policy", "schema.go"))
	if err != nil {
		return err
	}
	limits, err := collectConstValues(filepath.Join(root, "policy", "schema.go"), []string{
		"maxRetryAttempts",
		"maxHedges",
		"minBackoffFloor",
		"minHedgeDelayFloor",
		"maxBackoffCeiling",
		"minTimeoutFloor",
		"maxBackoffMultiplier",
		"minCircuitThreshold",
		"minCircuitCooldown",
	})
	if err != nil {
		return err
	}

	execFallbacks, err := collectExecutorFallbacks(filepath.Join(root, "retry", "executor.go"))
	if err != nil {
		return err
	}
	modeDefaults, err := collectExecutorModeDefaults(filepath.Join(root, "retry", "executor.go"))
	if err != nil {
		return err
	}
	builtins, err := collectBuiltinClassifierNames(filepath.Join(root, "classify", "builtins.go"))
	if err != nil {
		return err
	}
	defaultExec, err := collectDefaultExecutorConfig(filepath.Join(root, "retry", "defaults.go"))
	if err != nil {
		return err
	}
	hedgeFallback, err := detectFixedDelayFallback(filepath.Join(root, "retry", "group.go"))
	if err != nil {
		return err
	}
	usesDefaultExecutor, err := detectDefaultExecutorUsage(filepath.Join(root, "retry", "global.go"))
	if err != nil {
		return err
	}

	content, err := renderDefaultsSafetyMarkdown(defaults, limits, execFallbacks, modeDefaults, builtins, defaultExec, hedgeFallback, usesDefaultExecutor)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, content, 0o644)
}

func mergeStructs(dst, src map[string][]structField) {
	for k, v := range src {
		dst[k] = v
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func goFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".go") {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out, nil
}

func collectReasonConsts(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	values := make(map[string]struct{})
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "Reason") {
					continue
				}
				if len(vs.Values) <= i {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					return nil, err
				}
				values[val] = struct{}{}
			}
		}
	}
	return setToSorted(values), nil
}

func collectReasonAssignments(path string, rs *reasonSet) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return err
	}

	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.KeyValueExpr:
			if keyIdent, ok := v.Key.(*ast.Ident); ok && keyIdent.Name == "Reason" {
				addReasonExpr(v.Value, rs)
			}
		case *ast.AssignStmt:
			for i, lhs := range v.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || sel.Sel == nil || sel.Sel.Name != "Reason" {
					continue
				}
				if len(v.Rhs) <= i {
					continue
				}
				addReasonExpr(v.Rhs[i], rs)
			}
		}
		return true
	})
	return nil
}

func addReasonExpr(expr ast.Expr, rs *reasonSet) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return
		}
		val, err := strconv.Unquote(e.Value)
		if err != nil {
			return
		}
		rs.Static[val] = struct{}{}
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return
		}
		prefix, ok := stringLiteral(e.X)
		if !ok {
			return
		}
		pattern := prefix + "<dynamic>"
		if prefix == "http_" {
			pattern = "http_<status>"
		} else if prefix == "grpc_" {
			pattern = "grpc_<code>"
		}
		rs.Patterns[pattern] = struct{}{}
	}
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	val, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return val, true
}

func collectFailureModeStrings(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	values := make(map[string]struct{})
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "failureModeString" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok || len(ret.Results) == 0 {
				return true
			}
			for _, res := range ret.Results {
				if lit, ok := res.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					val, err := strconv.Unquote(lit.Value)
					if err == nil {
						values[val] = struct{}{}
					}
				}
			}
			return true
		})
	}
	return setToSorted(values), nil
}

func collectModeAssignments(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	values := make(map[string]struct{})
	ast.Inspect(f, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for i, lhs := range assign.Lhs {
			sel, ok := lhs.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "Mode" {
				continue
			}
			if len(assign.Rhs) <= i {
				continue
			}
			if lit, ok := assign.Rhs[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				val, err := strconv.Unquote(lit.Value)
				if err == nil {
					values[val] = struct{}{}
				}
			}
		}
		return true
	})
	return setToSorted(values), nil
}

func collectStructFields(path string, names []string) (map[string][]structField, error) {
	want := make(map[string]struct{})
	for _, name := range names {
		want[name] = struct{}{}
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	out := make(map[string][]structField)
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.TYPE {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, ok := want[ts.Name.Name]; !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			fields := make([]structField, 0, len(st.Fields.List))
			for _, field := range st.Fields.List {
				typeStr := exprString(field.Type)
				notes := joinComments(field.Doc, field.Comment)
				jsonTag := ""
				if field.Tag != nil {
					if tag, err := strconv.Unquote(field.Tag.Value); err == nil {
						jsonTag = strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
					}
				}
				if len(field.Names) == 0 {
					fields = append(fields, structField{Name: typeStr, Type: "", JSON: jsonTag, Notes: notes})
					continue
				}
				for _, name := range field.Names {
					fields = append(fields, structField{Name: name.Name, Type: typeStr, JSON: jsonTag, Notes: notes})
				}
			}
			out[ts.Name.Name] = fields
		}
	}
	return out, nil
}

func collectTypedConstValues(path, typeName string) ([]constValue, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	var values []constValue
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != typeName {
				continue
			}
			for i, name := range vs.Names {
				if len(vs.Values) <= i {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					return nil, err
				}
				values = append(values, constValue{Name: name.Name, Value: val})
			}
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Name < values[j].Name })
	return values, nil
}

func collectConstValues(path string, names []string) (map[string]string, error) {
	want := make(map[string]struct{})
	for _, name := range names {
		want[name] = struct{}{}
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if _, ok := want[name.Name]; !ok {
					continue
				}
				if len(vs.Values) == 0 {
					continue
				}
				idx := i
				if idx >= len(vs.Values) {
					idx = len(vs.Values) - 1
				}
				out[name.Name] = exprString(vs.Values[idx])
			}
		}
	}
	return out, nil
}

func collectDefaultPolicyValues(path string) (map[string]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	defaults := make(map[string]string)
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != "DefaultPolicyFor" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok || len(ret.Results) == 0 {
				return true
			}
			lit, ok := ret.Results[0].(*ast.CompositeLit)
			if !ok {
				return false
			}
			parseCompositeLit("", lit, defaults)
			return false
		})
	}
	return defaults, nil
}

func parseCompositeLit(prefix string, lit *ast.CompositeLit, defaults map[string]string) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		keyIdent, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		field := keyIdent.Name
		path := field
		if prefix != "" {
			path = prefix + "." + field
		}
		if nested, ok := kv.Value.(*ast.CompositeLit); ok {
			parseCompositeLit(path, nested, defaults)
			continue
		}
		defaults[path] = exprString(kv.Value)
	}
}

func collectExecutorModeDefaults(path string) (map[string]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	fn := funcDeclByName(f, "NewExecutorFromOptions")
	if fn == nil {
		return out, nil
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		ident, ok := cl.Type.(*ast.Ident)
		if !ok || ident.Name != "Executor" {
			return true
		}
		for _, elt := range cl.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			keyIdent, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch keyIdent.Name {
			case "missingPolicyMode", "missingClassifierMode", "missingBudgetMode", "missingTriggerMode":
				def := defaultFailureMode(kv.Value)
				if def == "" {
					def = exprString(kv.Value)
				}
				out[keyIdent.Name] = def
			}
		}
		return false
	})
	return out, nil
}

func collectExecutorFallbacks(path string) (executorFallbacks, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return executorFallbacks{}, err
	}
	out := executorFallbacks{Defaults: make(map[string]string)}
	fn := funcDeclByName(f, "NewExecutorFromOptions")
	if fn == nil || fn.Body == nil {
		return out, nil
	}
	for _, stmt := range fn.Body.List {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok {
			continue
		}
		field := fieldFromNilCheck(ifStmt.Cond)
		if field == "" {
			continue
		}
		fallback := ""
		for _, bodyStmt := range ifStmt.Body.List {
			switch s := bodyStmt.(type) {
			case *ast.AssignStmt:
				if len(s.Lhs) != 1 || len(s.Rhs) != 1 {
					continue
				}
				sel, ok := s.Lhs[0].(*ast.SelectorExpr)
				if !ok || !isIdent(sel.X, "e") || sel.Sel == nil || sel.Sel.Name != field {
					continue
				}
				fallback = exprString(s.Rhs[0])
			case *ast.ExprStmt:
				if field != "classifiers" {
					continue
				}
				call, ok := s.X.(*ast.CallExpr)
				if !ok {
					continue
				}
				if isSelectorCall(call, "classify", "RegisterBuiltins") {
					out.ClassifierBuiltins = true
				}
			}
		}
		if fallback != "" {
			out.Defaults[field] = fallback
		}
	}
	return out, nil
}

func collectBuiltinClassifierNames(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	consts := make(map[string]string)
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if len(vs.Values) == 0 {
					continue
				}
				idx := i
				if idx >= len(vs.Values) {
					idx = len(vs.Values) - 1
				}
				lit, ok := vs.Values[idx].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					return nil, err
				}
				consts[name.Name] = val
			}
		}
	}

	values := make(map[string]struct{})
	fn := funcDeclByName(f, "RegisterBuiltins")
	if fn == nil || fn.Body == nil {
		return setToSorted(values), nil
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil || sel.Sel.Name != "Register" {
			return true
		}
		if !isIdent(sel.X, "reg") {
			return true
		}
		if len(call.Args) == 0 {
			return true
		}
		switch arg := call.Args[0].(type) {
		case *ast.BasicLit:
			if arg.Kind != token.STRING {
				return true
			}
			val, err := strconv.Unquote(arg.Value)
			if err == nil {
				values[val] = struct{}{}
			}
		case *ast.Ident:
			if val, ok := consts[arg.Name]; ok {
				values[val] = struct{}{}
			}
		}
		return true
	})
	return setToSorted(values), nil
}

func collectDefaultExecutorConfig(path string) (defaultExecutorConfig, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return defaultExecutorConfig{}, err
	}
	cfg := defaultExecutorConfig{}
	budgets := make(map[string]struct{})
	triggers := make(map[string]struct{})
	fn := funcDeclByName(f, "NewDefaultExecutor")
	if fn == nil || fn.Body == nil {
		return cfg, nil
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			switch fun.Name {
			case "WithObserver":
				if cfg.Observer == "" && len(call.Args) > 0 {
					cfg.Observer = exprString(call.Args[0])
				}
			case "WithDefaultClassifier":
				if cfg.DefaultClassifier == "" && len(call.Args) > 0 {
					cfg.DefaultClassifier = exprString(call.Args[0])
				}
			}
		case *ast.SelectorExpr:
			if fun.Sel == nil || len(call.Args) == 0 {
				return true
			}
			switch {
			case isIdent(fun.X, "budgetReg") && (fun.Sel.Name == "MustRegister" || fun.Sel.Name == "Register"):
				if name, ok := stringLiteral(call.Args[0]); ok {
					budgets[name] = struct{}{}
				}
			case isIdent(fun.X, "triggerReg") && fun.Sel.Name == "Register":
				if name, ok := stringLiteral(call.Args[0]); ok {
					triggers[name] = struct{}{}
				}
			}
		}
		return true
	})
	cfg.Budgets = setToSorted(budgets)
	cfg.Triggers = setToSorted(triggers)
	return cfg, nil
}

func detectFixedDelayFallback(path string) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return false, err
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		switch t := cl.Type.(type) {
		case *ast.SelectorExpr:
			if t.Sel != nil && t.Sel.Name == "FixedDelayTrigger" {
				found = true
				return false
			}
		case *ast.Ident:
			if t.Name == "FixedDelayTrigger" {
				found = true
				return false
			}
		}
		return true
	})
	return found, nil
}

func detectDefaultExecutorUsage(path string) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return false, err
	}
	fn := funcDeclByName(f, "DefaultExecutor")
	if fn == nil || fn.Body == nil {
		return false, nil
	}
	found := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "NewDefaultExecutor" {
			found = true
			return false
		}
		return true
	})
	return found, nil
}

func defaultFailureMode(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "normalizeFailureMode" {
		return ""
	}
	if len(call.Args) < 2 {
		return ""
	}
	return exprString(call.Args[1])
}

func funcDeclByName(f *ast.File, name string) *ast.FuncDecl {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Name.Name != name {
			continue
		}
		return fn
	}
	return nil
}

func fieldFromNilCheck(expr ast.Expr) string {
	bin, ok := expr.(*ast.BinaryExpr)
	if !ok || bin.Op != token.EQL {
		return ""
	}
	if isNilIdent(bin.Y) {
		return selectorOnIdent(bin.X, "e")
	}
	if isNilIdent(bin.X) {
		return selectorOnIdent(bin.Y, "e")
	}
	return ""
}

func selectorOnIdent(expr ast.Expr, name string) string {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || !isIdent(sel.X, name) {
		return ""
	}
	return sel.Sel.Name
}

func isNilIdent(expr ast.Expr) bool {
	return isIdent(expr, "nil")
}

func isIdent(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func isSelectorCall(call *ast.CallExpr, pkg, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != name {
		return false
	}
	return isIdent(sel.X, pkg)
}

func exprString(expr ast.Expr) string {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, token.NewFileSet(), expr)
	return buf.String()
}

func joinComments(groups ...*ast.CommentGroup) string {
	var parts []string
	for _, g := range groups {
		if g == nil {
			continue
		}
		text := strings.TrimSpace(g.Text())
		if text != "" {
			parts = append(parts, strings.ReplaceAll(text, "\n", " "))
		}
	}
	return strings.Join(parts, " ")
}

func renderReasonsMarkdown(budgetReasons, circuitReasons []string, outcome reasonSet, modes map[string]struct{}, structs map[string][]structField) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("<!-- Generated by scripts/gen_reference.go; do not edit by hand. -->\n")
	buf.WriteString("# Reason codes and timeline fields\n\n")

	buf.WriteString("Generated from: `budget/reasons.go`, `circuit/types.go`, `classify/`, `retry/`, `integrations/grpc/grpc.go`, `observe/types.go`.\n\n")
	buf.WriteString("These reason codes and timeline fields are part of the v1 telemetry contract. Changes are breaking.\n\n")

	buf.WriteString("## Outcome reasons\n\n")
	buf.WriteString("These values appear in `observe.AttemptRecord.Outcome.Reason`.\n\n")

	static := setToSorted(outcome.Static)
	if len(static) > 0 {
		buf.WriteString("### Static reasons\n\n")
		for _, reason := range static {
			buf.WriteString("- `" + reason + "`\n")
		}
		buf.WriteString("\n")
	}

	patterns := setToSorted(outcome.Patterns)
	if len(patterns) > 0 {
		buf.WriteString("### Pattern reasons\n\n")
		for _, reason := range patterns {
			buf.WriteString("- `" + reason + "`\n")
		}
		buf.WriteString("\n")
	}

	buf.WriteString("## Budget reasons\n\n")
	buf.WriteString("These values appear in `observe.BudgetDecisionEvent.Reason` and `observe.AttemptRecord.BudgetReason`.\n\n")
	for _, reason := range budgetReasons {
		buf.WriteString("- `" + reason + "`\n")
	}
	buf.WriteString("\n")

	buf.WriteString("## Circuit reasons\n\n")
	buf.WriteString("These values appear on `retry.CircuitOpenError.Reason`.\n\n")
	for _, reason := range circuitReasons {
		buf.WriteString("- `" + reason + "`\n")
	}
	buf.WriteString("\n")

	buf.WriteString("## Budget decision modes\n\n")
	buf.WriteString("These values appear in `observe.BudgetDecisionEvent.Mode`.\n\n")
	for _, mode := range setToSorted(modes) {
		buf.WriteString("- `" + mode + "`\n")
	}
	buf.WriteString("\n")

	buf.WriteString("## Timeline fields\n\n")
	writeStruct(&buf, "Timeline", structs["Timeline"])
	writeStruct(&buf, "AttemptRecord", structs["AttemptRecord"])
	writeStruct(&buf, "BudgetDecisionEvent", structs["BudgetDecisionEvent"])

	buf.WriteString("## Timeline attribute keys\n\n")
	buf.WriteString("### Policy resolution attributes\n\n")
	buf.WriteString("These keys appear in `observe.Timeline.Attributes`.\n\n")
	buf.WriteString("| Attribute | Allowed values / format | Notes |\n")
	buf.WriteString("|---|---|---|\n")
	buf.WriteString("| `policy_mode` | `standard`, `fallback`, `allow`, `deny` | Resolution path used for the final execution outcome. |\n")
	buf.WriteString("| `policy_source` | `static`, `remote`, `lkg`, `default`, `unknown` | Source of the final policy used for execution, or `unknown` when no concrete policy was applied. |\n")
	buf.WriteString("| `policy_normalized` | `true`, `false` | Whether normalization changed the final policy used for execution. |\n")
	buf.WriteString("| `policy_normalized_fields` | comma-separated sorted field paths | Present only when `policy_normalized=true`; omitted when normalization changed no fields. |\n")

	return buf.Bytes(), nil
}

func renderPolicySchemaMarkdown(structs map[string][]structField, defaults map[string]string, jitterValues, policySources []constValue, limits map[string]string) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("<!-- Generated by scripts/gen_reference.go; do not edit by hand. -->\n")
	buf.WriteString("# Policy schema reference\n\n")
	buf.WriteString("Generated from: `policy/key.go`, `policy/schema.go`.\n\n")

	buf.WriteString("## Types\n\n")
	writeStructWithTags(&buf, "policy.PolicyKey", structs["PolicyKey"])
	writeStructWithTags(&buf, "policy.BudgetRef", structs["BudgetRef"])
	writeStructWithTags(&buf, "policy.RetryPolicy", structs["RetryPolicy"])
	writeStructWithTags(&buf, "policy.HedgePolicy", structs["HedgePolicy"])
	writeStructWithTags(&buf, "policy.CircuitPolicy", structs["CircuitPolicy"])
	writeStructWithTags(&buf, "policy.NormalizationInfo", structs["NormalizationInfo"])
	writeStructWithTags(&buf, "policy.Metadata", structs["Metadata"])
	writeStructWithTags(&buf, "policy.EffectivePolicy", structs["EffectivePolicy"])

	buf.WriteString("## Default policy values\n\n")
	buf.WriteString("Defaults are taken from `policy.DefaultPolicyFor`. Normalization may adjust values when fields are zero or out of bounds.\n\n")
	buf.WriteString("| Field path | Default |\n")
	buf.WriteString("|---|---|\n")
	for _, path := range sortedKeys(defaults) {
		buf.WriteString("| `" + path + "` | `" + defaults[path] + "` |\n")
	}
	buf.WriteString("\n")

	if len(jitterValues) > 0 {
		buf.WriteString("## JitterKind values\n\n")
		buf.WriteString("| Name | Value |\n")
		buf.WriteString("|---|---|\n")
		for _, v := range jitterValues {
			buf.WriteString("| `" + v.Name + "` | `" + v.Value + "` |\n")
		}
		buf.WriteString("\n")
	}

	if len(policySources) > 0 {
		buf.WriteString("## PolicySource values\n\n")
		buf.WriteString("| Name | Value |\n")
		buf.WriteString("|---|---|\n")
		for _, v := range policySources {
			buf.WriteString("| `" + v.Name + "` | `" + v.Value + "` |\n")
		}
		buf.WriteString("\n")
	}

	if len(limits) > 0 {
		buf.WriteString("## Normalization limits\n\n")
		buf.WriteString("Values are defined in `policy/schema.go` and used by `EffectivePolicy.Normalize`.\n\n")
		buf.WriteString("| Constant | Value |\n")
		buf.WriteString("|---|---|\n")
		for _, name := range sortedKeys(limits) {
			buf.WriteString("| `" + name + "` | `" + limits[name] + "` |\n")
		}
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}

func renderDefaultsSafetyMarkdown(defaults, limits map[string]string, fallbacks executorFallbacks, modeDefaults map[string]string, builtins []string, defaultExec defaultExecutorConfig, hedgeFallback bool, usesDefaultExecutor bool) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("<!-- Generated by scripts/gen_reference.go; do not edit by hand. -->\n")
	buf.WriteString("# Defaults and safety model\n\n")
	buf.WriteString("Generated from: `policy/schema.go`, `retry/executor.go`, `retry/defaults.go`, `retry/budget.go`, `retry/group.go`, `classify/builtins.go`, `retry/global.go`.\n\n")

	if usesDefaultExecutor {
		buf.WriteString("`retry.DefaultExecutor` uses `retry.NewDefaultExecutor` unless a global executor is set.\n\n")
	}

	buf.WriteString("## Policy defaults\n\n")
	buf.WriteString("Defaults are taken from `policy.DefaultPolicyFor`. Normalization may adjust values when fields are zero or out of bounds.\n\n")
	buf.WriteString("| Field path | Default |\n")
	buf.WriteString("|---|---|\n")
	for _, path := range sortedKeys(defaults) {
		buf.WriteString("| `" + path + "` | `" + defaults[path] + "` |\n")
	}
	buf.WriteString("\n")

	if len(limits) > 0 {
		buf.WriteString("## Normalization limits\n\n")
		buf.WriteString("Values are defined in `policy/schema.go` and used by `EffectivePolicy.Normalize`.\n\n")
		buf.WriteString("| Constant | Value |\n")
		buf.WriteString("|---|---|\n")
		for _, name := range sortedKeys(limits) {
			buf.WriteString("| `" + name + "` | `" + limits[name] + "` |\n")
		}
		buf.WriteString("\n")
	}

	buf.WriteString("## Executor defaults (NewExecutorFromOptions)\n\n")
	buf.WriteString("When an option is nil, the executor supplies a fallback.\n\n")
	buf.WriteString("| Component | Default |\n")
	buf.WriteString("|---|---|\n")

	componentRows := []struct {
		Label string
		Field string
	}{
		{"Provider", "provider"},
		{"Observer", "observer"},
		{"Clock", "clock"},
		{"Sleep", "sleep"},
		{"Classifiers", "classifiers"},
		{"Triggers", "triggers"},
		{"Circuits", "circuits"},
		{"Default classifier", "defaultClassifier"},
		{"Budgets", "budgets"},
	}
	for _, row := range componentRows {
		value := fallbacks.Defaults[row.Field]
		if value == "" && row.Field == "budgets" {
			value = "nil"
		}
		formatted := "-"
		if value != "" {
			formatted = "`" + value + "`"
		}
		if row.Field == "classifiers" && fallbacks.ClassifierBuiltins {
			if len(builtins) > 0 {
				formatted += " + `classify.RegisterBuiltins` (" + joinBackticked(builtins) + ")"
			} else {
				formatted += " + `classify.RegisterBuiltins`"
			}
		}
		buf.WriteString("| " + row.Label + " | " + formatted + " |\n")
	}
	buf.WriteString("\n")

	buf.WriteString("## Default failure modes\n\n")
	buf.WriteString("Failure modes control what happens when policy, budgets, classifiers, or triggers are missing.\n\n")
	buf.WriteString("| Missing mode | Default | Notes |\n")
	buf.WriteString("|---|---|---|\n")
	modeRows := []struct {
		Label string
		Field string
		Note  string
	}{
		{"MissingPolicyMode", "missingPolicyMode", "Policy resolution errors fail closed (`NoPolicyError`)."},
		{"MissingBudgetMode", "missingBudgetMode", "Missing or invalid budgets deny attempts."},
		{"MissingClassifierMode", "missingClassifierMode", "Fallback to the default classifier."},
		{"MissingTriggerMode", "missingTriggerMode", "-"},
	}
	if hedgeFallback {
		modeRows[3].Note = "Missing trigger falls back to fixed delay hedging."
	}
	for _, row := range modeRows {
		value := modeDefaults[row.Field]
		if value == "" {
			value = "-"
		} else {
			value = "`" + value + "`"
		}
		note := row.Note
		if note == "" {
			note = "-"
		}
		buf.WriteString("| " + row.Label + " | " + value + " | " + escapePipes(note) + " |\n")
	}
	buf.WriteString("\n")

	buf.WriteString("## NewDefaultExecutor additions\n\n")
	buf.WriteString("NewDefaultExecutor builds on NewExecutorFromOptions with these defaults.\n\n")
	buf.WriteString("| Component | Value |\n")
	buf.WriteString("|---|---|\n")
	if len(builtins) > 0 {
		buf.WriteString("| Built-in classifiers | " + joinBackticked(builtins) + " |\n")
	}
	if defaultExec.DefaultClassifier != "" {
		buf.WriteString("| Default classifier | `" + defaultExec.DefaultClassifier + "` |\n")
	}
	if len(defaultExec.Budgets) > 0 {
		buf.WriteString("| Budget registry entries | " + joinBackticked(defaultExec.Budgets) + " |\n")
	}
	if len(defaultExec.Triggers) > 0 {
		buf.WriteString("| Hedge trigger registry entries | " + joinBackticked(defaultExec.Triggers) + " |\n")
	}
	if defaultExec.Observer != "" {
		buf.WriteString("| Observer | `" + defaultExec.Observer + "` |\n")
	}
	buf.WriteString("\n")

	return buf.Bytes(), nil
}

func writeStruct(buf *bytes.Buffer, name string, fields []structField) {
	if len(fields) == 0 {
		return
	}
	buf.WriteString("### observe." + name + "\n\n")
	buf.WriteString("| Field | Type | Notes |\n")
	buf.WriteString("|---|---|---|\n")
	for _, field := range fields {
		note := field.Notes
		if note == "" {
			note = "-"
		}
		typeStr := field.Type
		if typeStr == "" {
			typeStr = "-"
		}
		buf.WriteString("| `" + field.Name + "` | `" + typeStr + "` | " + escapePipes(note) + " |\n")
	}
	buf.WriteString("\n")
}

func writeStructWithTags(buf *bytes.Buffer, name string, fields []structField) {
	if len(fields) == 0 {
		return
	}
	buf.WriteString("### " + name + "\n\n")
	buf.WriteString("| Field | Type | JSON | Notes |\n")
	buf.WriteString("|---|---|---|---|\n")
	for _, field := range fields {
		note := field.Notes
		if note == "" {
			note = "-"
		}
		jsonTag := field.JSON
		if jsonTag == "" {
			jsonTag = "-"
		}
		typeStr := field.Type
		if typeStr == "" {
			typeStr = "-"
		}
		buf.WriteString("| `" + field.Name + "` | `" + typeStr + "` | `" + jsonTag + "` | " + escapePipes(note) + " |\n")
	}
	buf.WriteString("\n")
}

func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

func setToSorted(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func joinBackticked(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, "`"+v+"`")
	}
	return strings.Join(parts, ", ")
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
