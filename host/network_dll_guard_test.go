package host

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// winsockLoaders names every direct DLL-loading entry point exported by the two
// packages this repository supports. The table is package-specific: a local
// method called LoadLibrary is unrelated, and a new package must not inherit an
// exception merely by reusing one of these spellings. LoadLibraryEx still takes
// the DLL name as argument zero, so all entries share the same argument rule.
var winsockLoaders = map[string]map[string]bool{
	"syscall": {
		"LoadDLL": true, "LoadLibrary": true, "MustLoadDLL": true,
		"NewLazyDLL": true,
	},
	"golang.org/x/sys/windows": {
		"LoadDLL": true, "LoadLibrary": true, "LoadLibraryEx": true,
		"MustLoadDLL": true, "NewLazyDLL": true, "NewLazySystemDLL": true,
	},
}

type packageConstantExpression struct {
	expr      ast.Expr
	constants map[*ast.Object]ast.Expr
}

type packageLoaderExpression struct {
	expr    ast.Expr
	imports map[string]string
	loaders map[*ast.Object][]ast.Expr
}

type packageTypeExpression struct {
	expr    ast.Expr
	imports map[string]string
}

// packageTypeExpressions restores package-level type identity across files.
// go/parser only links declarations inside one file, while build-tag alternatives
// require retaining every selected definition rather than choosing a build.
func packageTypeExpressions(files []*ast.File) map[string][]packageTypeExpression {
	types := make(map[string][]packageTypeExpression)
	for _, file := range files {
		imports := make(map[string]string)
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			name := importPath[strings.LastIndex(importPath, "/")+1:]
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = importPath
		}
		for _, declaration := range file.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, rawSpec := range gen.Specs {
				spec, ok := rawSpec.(*ast.TypeSpec)
				if ok {
					types[spec.Name.Name] = append(types[spec.Name.Name], packageTypeExpression{expr: spec.Type, imports: imports})
				}
			}
		}
	}
	return types
}

// stringConstantExpressions preserves lexical identity through ast.Object. It
// covers explicit and inherited const specifications in package or function
// scope without type-loading mutually exclusive Windows build variants.
func stringConstantExpressions(file *ast.File) map[*ast.Object]ast.Expr {
	constants := make(map[*ast.Object]ast.Expr)
	ast.Inspect(file, func(node ast.Node) bool {
		declaration, ok := node.(*ast.GenDecl)
		if !ok || declaration.Tok != token.CONST {
			return true
		}
		recordConstantSpecs(declaration.Specs, func(name *ast.Ident, expr ast.Expr) {
			if name.Obj != nil {
				constants[name.Obj] = expr
			}
		})
		return true
	})
	return constants
}

// packageStringConstantExpressions is the cross-file half of constant identity.
// The Go parser links identifiers only within one file, so package references in
// sibling selected files have nil ast.Object values. Keep every definition and
// evaluate alternatives when the identifier is used; this preserves build-tagged
// alternatives without pretending to select one build configuration.
func packageStringConstantExpressions(files []*ast.File) map[string][]packageConstantExpression {
	constants := make(map[string][]packageConstantExpression)
	for _, file := range files {
		local := stringConstantExpressions(file)
		for _, declaration := range file.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			recordConstantSpecs(gen.Specs, func(name *ast.Ident, expr ast.Expr) {
				constants[name.Name] = append(constants[name.Name], packageConstantExpression{expr: expr, constants: local})
			})
		}
	}
	return constants
}

func recordConstantSpecs(specs []ast.Spec, record func(*ast.Ident, ast.Expr)) {
	var inherited []ast.Expr
	for _, rawSpec := range specs {
		spec, ok := rawSpec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if len(spec.Values) > 0 {
			inherited = spec.Values
		}
		if len(inherited) != len(spec.Names) {
			continue
		}
		for index, name := range spec.Names {
			record(name, inherited[index])
		}
	}
}

func loaderFunctionExpressions(file *ast.File) map[*ast.Object][]ast.Expr {
	loaders := make(map[*ast.Object][]ast.Expr)
	ast.Inspect(file, func(node ast.Node) bool {
		switch declaration := node.(type) {
		case *ast.ValueSpec:
			if len(declaration.Names) != len(declaration.Values) {
				return true
			}
			for index, name := range declaration.Names {
				if name.Obj != nil {
					loaders[name.Obj] = append(loaders[name.Obj], declaration.Values[index])
				}
			}
		case *ast.AssignStmt:
			if len(declaration.Lhs) != len(declaration.Rhs) {
				return true
			}
			for index, left := range declaration.Lhs {
				name, ok := assignedIdentifier(left)
				if ok && name.Obj != nil {
					loaders[name.Obj] = append(loaders[name.Obj], declaration.Rhs[index])
				}
			}
		}
		return true
	})
	return loaders
}

func assignedIdentifier(expr ast.Expr) (*ast.Ident, bool) {
	for {
		parenthesized, ok := expr.(*ast.ParenExpr)
		if !ok {
			name, ident := expr.(*ast.Ident)
			return name, ident
		}
		expr = parenthesized.X
	}
}

func packageLoaderFunctionExpressions(files []*ast.File) map[string][]packageLoaderExpression {
	loaders := make(map[string][]packageLoaderExpression)
	packageVariables := make(map[*ast.Object]bool)
	for _, file := range files {
		imports := make(map[string]string)
		for _, spec := range file.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			name := importPath[strings.LastIndex(importPath, "/")+1:]
			if spec.Name != nil {
				name = spec.Name.Name
			}
			imports[name] = importPath
		}
		local := loaderFunctionExpressions(file)
		for _, declaration := range file.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok || gen.Tok != token.VAR {
				continue
			}
			for _, rawSpec := range gen.Specs {
				spec, ok := rawSpec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range spec.Names {
					if name.Obj != nil {
						packageVariables[name.Obj] = true
					}
				}
				if len(spec.Names) != len(spec.Values) {
					continue
				}
				for index, name := range spec.Names {
					loaders[name.Name] = append(loaders[name.Name], packageLoaderExpression{
						expr: spec.Values[index], imports: imports, loaders: local,
					})
				}
			}
		}
		// Package references used from a sibling file have no ast.Object. Record
		// ordinary assignments as additional possible bindings; `:=` remains
		// lexical and is already represented by loaderFunctionExpressions.
		ast.Inspect(file, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok || assignment.Tok != token.ASSIGN || len(assignment.Lhs) != len(assignment.Rhs) {
				return true
			}
			for index, left := range assignment.Lhs {
				name, ok := assignedIdentifier(left)
				if ok && name.Name != "_" && (name.Obj == nil || packageVariables[name.Obj]) {
					loaders[name.Name] = append(loaders[name.Name], packageLoaderExpression{
						expr: assignment.Rhs[index], imports: imports, loaders: local,
					})
				}
			}
			return true
		})
	}
	return loaders
}

func dynamicWinsockLoad(call *ast.CallExpr, imports map[string]string, constants map[*ast.Object]ast.Expr, packageConstants map[string][]packageConstantExpression, loaders map[*ast.Object][]ast.Expr, packageTypes map[string][]packageTypeExpression, packageLoaders map[string][]packageLoaderExpression) bool {
	if len(call.Args) == 0 || !winsockLoaderExpression(call.Fun, imports, loaders, packageTypes, packageLoaders, make(map[*ast.Object]bool), make(map[string]bool)) {
		return false
	}
	return containsWinsockName(constantStrings(call.Args[0], constants, packageConstants, packageTypes, make(map[*ast.Object]bool), make(map[string]bool)))
}

func winsockLoaderExpression(expr ast.Expr, imports map[string]string, loaders map[*ast.Object][]ast.Expr, packageTypes map[string][]packageTypeExpression, packageLoaders map[string][]packageLoaderExpression, seenObjects map[*ast.Object]bool, seenNames map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.ParenExpr:
		return winsockLoaderExpression(value.X, imports, loaders, packageTypes, packageLoaders, seenObjects, seenNames)
	case *ast.SelectorExpr:
		ident, ok := value.X.(*ast.Ident)
		return ok && ident.Obj == nil && winsockLoaders[imports[ident.Name]][value.Sel.Name]
	case *ast.CallExpr:
		if len(value.Args) != 1 || !localFunctionType(value.Fun, packageTypes, make(map[*ast.Object]bool), make(map[string]bool)) {
			return false
		}
		return winsockLoaderExpression(value.Args[0], imports, loaders, packageTypes, packageLoaders, seenObjects, seenNames)
	case *ast.Ident:
		if value.Obj != nil {
			if seenObjects[value.Obj] {
				return false
			}
			seenObjects[value.Obj] = true
			for _, bound := range loaders[value.Obj] {
				if winsockLoaderExpression(bound, imports, loaders, packageTypes, packageLoaders, seenObjects, seenNames) {
					delete(seenObjects, value.Obj)
					return true
				}
			}
			delete(seenObjects, value.Obj)
			return false
		}
		if seenNames[value.Name] {
			return false
		}
		seenNames[value.Name] = true
		for _, bound := range packageLoaders[value.Name] {
			if winsockLoaderExpression(bound.expr, bound.imports, bound.loaders, packageTypes, packageLoaders, seenObjects, seenNames) {
				delete(seenNames, value.Name)
				return true
			}
		}
		delete(seenNames, value.Name)
		return false
	default:
		return false
	}
}
func dynamicWinsockLiteral(literal *ast.CompositeLit, imports map[string]string, constants map[*ast.Object]ast.Expr, packageConstants map[string][]packageConstantExpression, packageTypes map[string][]packageTypeExpression) bool {
	if !lazyDLLType(literal.Type, imports, packageTypes, make(map[*ast.Object]bool), make(map[string]bool)) {
		return false
	}
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := field.Key.(*ast.Ident)
		if ok && name.Name == "Name" &&
			containsWinsockName(constantStrings(field.Value, constants, packageConstants, packageTypes, make(map[*ast.Object]bool), make(map[string]bool))) {
			return true
		}
	}
	return false
}

func localFunctionType(expr ast.Expr, packageTypes map[string][]packageTypeExpression, seenObjects map[*ast.Object]bool, seenNames map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.ParenExpr:
		return localFunctionType(value.X, packageTypes, seenObjects, seenNames)
	case *ast.IndexExpr:
		return localFunctionType(value.X, packageTypes, seenObjects, seenNames)
	case *ast.IndexListExpr:
		return localFunctionType(value.X, packageTypes, seenObjects, seenNames)
	case *ast.FuncType:
		return true
	case *ast.Ident:
		if value.Obj != nil {
			if value.Obj.Kind != ast.Typ || seenObjects[value.Obj] {
				return false
			}
			spec, ok := value.Obj.Decl.(*ast.TypeSpec)
			if !ok {
				return false
			}
			seenObjects[value.Obj] = true
			found := localFunctionType(spec.Type, packageTypes, seenObjects, seenNames)
			delete(seenObjects, value.Obj)
			return found
		}
		if seenNames[value.Name] {
			return false
		}
		seenNames[value.Name] = true
		for _, bound := range packageTypes[value.Name] {
			if localFunctionType(bound.expr, packageTypes, seenObjects, seenNames) {
				delete(seenNames, value.Name)
				return true
			}
		}
		delete(seenNames, value.Name)
	}
	return false
}

func lazyDLLType(expr ast.Expr, imports map[string]string, packageTypes map[string][]packageTypeExpression, seenObjects map[*ast.Object]bool, seenNames map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.ParenExpr:
		return lazyDLLType(value.X, imports, packageTypes, seenObjects, seenNames)
	case *ast.IndexExpr:
		return lazyDLLType(value.X, imports, packageTypes, seenObjects, seenNames)
	case *ast.IndexListExpr:
		return lazyDLLType(value.X, imports, packageTypes, seenObjects, seenNames)
	case *ast.SelectorExpr:
		ident, ok := value.X.(*ast.Ident)
		if !ok || ident.Obj != nil || value.Sel.Name != "LazyDLL" {
			return false
		}
		importPath := imports[ident.Name]
		return importPath == "syscall" || importPath == "golang.org/x/sys/windows"
	case *ast.Ident:
		if value.Obj != nil {
			if value.Obj.Kind != ast.Typ || seenObjects[value.Obj] {
				return false
			}
			spec, ok := value.Obj.Decl.(*ast.TypeSpec)
			if !ok {
				return false
			}
			seenObjects[value.Obj] = true
			found := lazyDLLType(spec.Type, imports, packageTypes, seenObjects, seenNames)
			delete(seenObjects, value.Obj)
			return found
		}
		if seenNames[value.Name] {
			return false
		}
		seenNames[value.Name] = true
		for _, bound := range packageTypes[value.Name] {
			if lazyDLLType(bound.expr, bound.imports, packageTypes, seenObjects, seenNames) {
				delete(seenNames, value.Name)
				return true
			}
		}
		delete(seenNames, value.Name)
	}
	return false
}

// Windows loader APIs append ".DLL" when the caller supplies no extension.
// Keep the exact base name and its explicit extension; paths and other suffixes
// remain outside this source guard rather than being guessed.
func containsWinsockName(values []string) bool {
	for _, value := range values {
		if strings.EqualFold(value, "ws2_"+"32") || strings.EqualFold(value, "ws2_"+"32.dll") {
			return true
		}
	}
	return false
}

// stringType is part of the #94/#99 false-success boundary, not a general Go
// type checker. The missed compiling form was dllName("ws2_32.dll") where
// dllName, including dllName[T], ultimately named string: the loader was visible
// but constantStrings discarded its argument because the conversion callee was
// not the unresolved built-in identifier. Parentheses and generic indices are
// peeled before following same-file TypeSpec objects. go/parser does not link a
// sibling-file name, so packageTypes supplies every build-tagged alternative;
// accepting any string definition is conservative because the guard must reject
// a Winsock load reachable in any selected build. Object/name sets terminate a
// cyclic alias as unresolved; such an alias cannot be a compiling conversion.
func stringType(expr ast.Expr, packageTypes map[string][]packageTypeExpression, seenObjects map[*ast.Object]bool, seenNames map[string]bool) bool {
	switch value := expr.(type) {
	case *ast.ParenExpr:
		return stringType(value.X, packageTypes, seenObjects, seenNames)
	case *ast.IndexExpr:
		return stringType(value.X, packageTypes, seenObjects, seenNames)
	case *ast.IndexListExpr:
		return stringType(value.X, packageTypes, seenObjects, seenNames)
	case *ast.Ident:
		if value.Obj != nil {
			if value.Obj.Kind != ast.Typ || seenObjects[value.Obj] {
				return false
			}
			spec, ok := value.Obj.Decl.(*ast.TypeSpec)
			if !ok {
				return false
			}
			seenObjects[value.Obj] = true
			found := stringType(spec.Type, packageTypes, seenObjects, seenNames)
			delete(seenObjects, value.Obj)
			return found
		}
		if value.Name == "string" {
			return true
		}
		if seenNames[value.Name] {
			return false
		}
		seenNames[value.Name] = true
		for _, bound := range packageTypes[value.Name] {
			if stringType(bound.expr, packageTypes, seenObjects, seenNames) {
				delete(seenNames, value.Name)
				return true
			}
		}
		delete(seenNames, value.Name)
	}
	return false
}

// constantStrings evaluates only compile-time source forms that remain
// unambiguous without go/types. Package identifiers can have several build-tagged
// definitions, so every reachable value is retained and checked.
func constantStrings(expr ast.Expr, constants map[*ast.Object]ast.Expr, packageConstants map[string][]packageConstantExpression, packageTypes map[string][]packageTypeExpression, seenObjects map[*ast.Object]bool, seenNames map[string]bool) []string {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return nil
		}
		text, err := strconv.Unquote(value.Value)
		if err != nil {
			return nil
		}
		return []string{text}
	case *ast.ParenExpr:
		return constantStrings(value.X, constants, packageConstants, packageTypes, seenObjects, seenNames)
	case *ast.CallExpr:
		// Do not reduce this to `value.Fun.(*ast.Ident).Name == "string"`.
		// That was the exact green bypass for named and instantiated aliases.
		// stringType proves only the conversion's underlying string identity;
		// constantStrings still evaluates the sole argument independently.
		if len(value.Args) != 1 ||
			!stringType(value.Fun, packageTypes, make(map[*ast.Object]bool), make(map[string]bool)) {
			return nil
		}
		return constantStrings(value.Args[0], constants, packageConstants, packageTypes, seenObjects, seenNames)
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return nil
		}
		left := constantStrings(value.X, constants, packageConstants, packageTypes, seenObjects, seenNames)
		right := constantStrings(value.Y, constants, packageConstants, packageTypes, seenObjects, seenNames)
		var joined []string
		for _, prefix := range left {
			for _, suffix := range right {
				candidate := prefix + suffix
				if !containsString(joined, candidate) {
					joined = append(joined, candidate)
				}
			}
		}
		return joined
	case *ast.Ident:
		if value.Obj != nil {
			if seenObjects[value.Obj] {
				return nil
			}
			constant, ok := constants[value.Obj]
			if !ok {
				return nil
			}
			seenObjects[value.Obj] = true
			texts := constantStrings(constant, constants, packageConstants, packageTypes, seenObjects, seenNames)
			delete(seenObjects, value.Obj)
			return texts
		}
		if seenNames[value.Name] {
			return nil
		}
		seenNames[value.Name] = true
		var texts []string
		for _, definition := range packageConstants[value.Name] {
			for _, text := range constantStrings(definition.expr, definition.constants, packageConstants, packageTypes, seenObjects, seenNames) {
				if !containsString(texts, text) {
					texts = append(texts, text)
				}
			}
		}
		delete(seenNames, value.Name)
		return texts
	default:
		return nil
	}
}

func containsString(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
