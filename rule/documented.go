package rule

import (
	"fmt"
	"go/ast"
	"strings"

	"github.com/mgechev/revive/internal/typeparams"
	"github.com/mgechev/revive/lint"
)

// disabledDocumentedChecks stores which checks are disabled for the DocumentedRule
type disabledDocumentedChecks struct {
	Function bool
	Method   bool
}

// isDisabled returns true if the given check is disabled, false otherwise
func (dc *disabledDocumentedChecks) isDisabled(checkName string) bool {
	switch checkName {
	case "function":
		return dc.Function
	case "method":
		return dc.Method
	default:
		return false
	}
}

// DocumentedRule lints functions and methods to verify they have documentation comments.
// Unlike the exported rule, this rule checks all named functions and methods, not just exported ones.
type DocumentedRule struct {
	disabledChecks disabledDocumentedChecks
}

// Configure validates the rule configuration, and configures the rule accordingly.
//
// Configure makes the rule implement the [lint.ConfigurableRule] interface.
func (r *DocumentedRule) Configure(arguments lint.Arguments) error {
	r.disabledChecks = disabledDocumentedChecks{}
	for _, flag := range arguments {
		switch flag := flag.(type) {
		case string:
			switch {
			case isRuleOption(flag, "disableChecksOnFunctions"):
				r.disabledChecks.Function = true
			case isRuleOption(flag, "disableChecksOnMethods"):
				r.disabledChecks.Method = true
			default:
				return fmt.Errorf("unknown configuration flag %s for %s rule", flag, r.Name())
			}
		default:
			return fmt.Errorf("invalid argument for the %s rule: expecting a string, got %T", r.Name(), flag)
		}
	}

	return nil
}

// Apply applies the rule to given file.
func (r *DocumentedRule) Apply(file *lint.File, _ lint.Arguments) []lint.Failure {
	var failures []lint.Failure
	if file.IsTest() {
		return failures
	}

	walker := lintDocumented{
		file: file,
		onFailure: func(failure lint.Failure) {
			failures = append(failures, failure)
		},
		disabledChecks: r.disabledChecks,
	}

	ast.Walk(&walker, file.AST)

	return failures
}

// Name returns the rule name.
func (*DocumentedRule) Name() string {
	return "documented"
}

type lintDocumented struct {
	file           *lint.File
	onFailure      func(lint.Failure)
	disabledChecks disabledDocumentedChecks
}

func (w *lintDocumented) lintFuncDoc(fn *ast.FuncDecl) {
	// Skip functions generated for a generic instantiation
	if fn.Name == nil {
		return
	}

	// Skip unnamed functions
	if fn.Name.Name == "_" {
		return
	}

	// Skip main() and init() functions as they don't need documentation
	if fn.Name.Name == "main" || fn.Name.Name == "init" {
		if fn.Recv == nil { // Only if they're not methods
			return
		}
	}

	kind := "function"
	name := fn.Name.Name
	exported := ast.IsExported(name)

	if isMethod := fn.Recv != nil && len(fn.Recv.List) > 0; isMethod {
		kind = "method"
		recv := typeparams.ReceiverType(fn)
		name = recv + "." + name

		// Skip common implemented methods that are usually well-known
		if commonMethods[fn.Name.Name] {
			return
		}

		// Skip common sort.Interface methods
		switch fn.Name.Name {
		case "Len", "Less", "Swap":
			sortables := w.file.Pkg.Sortable()
			if sortables[recv] {
				return
			}
		}
	}

	if w.disabledChecks.isDisabled(kind) {
		return
	}

	firstCommentLine := firstCommentLine(fn.Doc)

	if firstCommentLine == "" {
		// Different message depending on whether the function is exported
		if exported {
			w.addFailuref(fn, 1, lint.FailureCategoryComments,
				"exported %s %s should have comment or be unexported", kind, name,
			)
		} else {
			w.addFailuref(fn, 1, lint.FailureCategoryComments,
				"%s %s should have comment", kind, name,
			)
		}
		return
	}

	prefix := fn.Name.Name + " "
	if !strings.HasPrefix(firstCommentLine, prefix) {
		if exported {
			w.addFailuref(fn.Doc, 0.8, lint.FailureCategoryComments,
				`comment on exported %s %s should be of the form "%s..."`, kind, name, prefix,
			)
		} else {
			w.addFailuref(fn.Doc, 0.8, lint.FailureCategoryComments,
				`comment on %s %s should be of the form "%s..."`, kind, name, prefix,
			)
		}
	}
}

func (w *lintDocumented) Visit(n ast.Node) ast.Visitor {
	switch v := n.(type) {
	case *ast.FuncDecl:
		w.lintFuncDoc(v)
		// Don't proceed inside funcs
		return nil
	}
	return w
}

func (w *lintDocumented) addFailuref(node ast.Node, confidence float64, category lint.FailureCategory, message string, args ...any) {
	w.onFailure(lint.Failure{
		Node:       node,
		Confidence: confidence,
		Category:   category,
		Failure:    fmt.Sprintf(message, args...),
	})
}
