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

func newReasonSet() reasonSet {
	return reasonSet{
		Static:   make(map[string]struct{}),
		Patterns: make(map[string]struct{}),
	}
}

func main() {
	var reasonsOut string
	var policyOut string
	flag.StringVar(&reasonsOut, "reasons-out", "docs/reference/reason-codes.md", "output markdown path for reason codes")
	flag.StringVar(&policyOut, "policy-out", "docs/reference/policy-schema.md", "output markdown path for policy schema")
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

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
