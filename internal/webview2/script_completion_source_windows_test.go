//go:build windows

package webview2

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestRequiredScriptProductionRouteOwnsRuntimeAddAndMessagePumpEffects(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "script_completion_windows.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			functions[function.Name.Name] = function
		}
	}
	register := functions["registerDocumentCreatedScriptsWithWait"]
	wait := functions["waitForScriptCompletion"]
	invoke := functions["scriptCompletionInvoke"]
	if register == nil || wait == nil || invoke == nil {
		t.Fatal("required-script registration, production wait, or COM Invoke owner is missing")
	}
	calls := func(function *ast.FuncDecl, name string) []*ast.CallExpr {
		var found []*ast.CallExpr
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && scriptCompletionASTPath(call.Fun) == name {
				found = append(found, call)
			}
			return true
		})
		return found
	}
	requiredHandlers := calls(register, "newRequiredScriptCompletionHandler")
	runtimeAdds := calls(register, "core.AddScriptToExecuteOnDocumentCreated")
	waits := calls(register, "wait")
	results := calls(register, "handler.result")
	if len(requiredHandlers) != 1 || len(runtimeAdds) != 1 || len(waits) != 1 || len(results) < 1 {
		t.Fatalf("registration owners required_handler=%d Runtime_Add=%d wait=%d result=%d", len(requiredHandlers), len(runtimeAdds), len(waits), len(results))
	}
	if !(requiredHandlers[0].Pos() < runtimeAdds[0].Pos() && runtimeAdds[0].Pos() < waits[0].Pos() && waits[0].Pos() < results[0].Pos()) {
		t.Fatal("each required registration must construct handler, call Runtime Add, wait, then inspect completion")
	}

	productionWait := calls(wait, "waitForRequiredScriptCompletion")
	if len(productionWait) != 1 || len(productionWait[0].Args) != 7 {
		t.Fatalf("production required wait calls=%d args=%d, want 1 and 7", len(productionWait), func() int {
			if len(productionWait) == 0 {
				return 0
			}
			return len(productionWait[0].Args)
		}())
	}
	if scriptCompletionASTPath(productionWait[0].Args[0]) != "handler.done" || scriptCompletionASTPath(productionWait[0].Args[1]) != "browser.shutdown" || scriptCompletionASTPath(productionWait[0].Args[6]) != "messages.finish" {
		t.Fatal("production required wait must own handler completion, Browser cancellation, and pump finish")
	}
	for index, want := range []string{"messages.drain", "messages.step"} {
		literal, ok := productionWait[0].Args[index+4].(*ast.FuncLit)
		if !ok || len(calls(&ast.FuncDecl{Body: literal.Body}, want)) != 1 {
			t.Fatalf("production wait callback %d must call %s exactly once", index+1, want)
		}
	}

	delays := calls(invoke, "delayRequiredScriptCompletionPublication")
	if len(delays) != 1 || len(delays[0].Args) != 1 || scriptCompletionASTPath(delays[0].Args[0]) != "handler.publish" || len(calls(invoke, "handler.publish")) != 0 {
		t.Fatal("COM Invoke must route only required completion through the diagnostic delay decision and retain direct inline publication")
	}
	var requiredGuard bool
	ast.Inspect(invoke.Body, func(node ast.Node) bool {
		guard, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		hasRequired, hasDelay := false, false
		ast.Inspect(guard.Cond, func(condition ast.Node) bool {
			switch condition := condition.(type) {
			case *ast.SelectorExpr:
				hasRequired = hasRequired || scriptCompletionASTPath(condition) == "handler.required"
			case *ast.CallExpr:
				hasDelay = hasDelay || condition == delays[0]
			}
			return true
		})
		requiredGuard = requiredGuard || hasRequired && hasDelay
		return true
	})
	if !requiredGuard {
		t.Fatal("diagnostic delay decision must remain guarded by handler.required")
	}
}

func scriptCompletionASTPath(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.SelectorExpr:
		left := scriptCompletionASTPath(expression.X)
		if left == "" {
			return expression.Sel.Name
		}
		return left + "." + expression.Sel.Name
	case *ast.ParenExpr:
		return scriptCompletionASTPath(expression.X)
	default:
		return ""
	}
}
