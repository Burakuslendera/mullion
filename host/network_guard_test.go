package host

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type networkPolicyFinding struct {
	path   string
	rule   string
	detail string
}

type parsedNetworkGoFile struct {
	rel  string
	file *ast.File
}

type networkPackagePolicy struct {
	constants map[string][]packageConstantExpression
	types     map[string][]packageTypeExpression
	loaders   map[string][]packageLoaderExpression
}

// networkPolicyReadFile is replaceable only so the real named guard can be run
// in a child process with a deterministic selected-file failure. Production
// guard execution leaves it as os.ReadFile.
var networkPolicyReadFile = os.ReadFile

// prohibitedNetworkSymbols is intentionally keyed by import path and exported
// symbol, not by source spelling. Issue #94 showed why a substring is not an
// authority: net.Listener was a false positive while an aliased net.Listen,
// http.Server through a receiver variable, and net.FileListener were invisible.
// Inspecting SelectorExpr nodes also covers function values and type references,
// so a caller cannot bypass the rule merely by delaying the call.
//
// Keep this table synchronized with the pinned x/sys version. Do not add generic
// receiver names such as Serve: without package/type provenance that would ban
// unrelated application abstractions and recreate the old false-positive class.
var prohibitedNetworkSymbols = map[string]map[string]bool{
	"net": {
		"FileListener": true, "FilePacketConn": true, "Listen": true,
		"ListenConfig": true, "ListenIP": true, "ListenMulticastUDP": true,
		"ListenPacket": true, "ListenTCP": true, "ListenUDP": true,
		"ListenUnix": true, "ListenUnixgram": true,
	},
	"net/http": {
		"ListenAndServe": true, "ListenAndServeTLS": true, "Serve": true,
		"ServeTLS": true, "Server": true,
	},
	"net/http/httptest": {
		"NewServer": true, "NewTLSServer": true, "NewUnstartedServer": true,
	},
	"crypto/tls": {
		"Listen": true, "NewListener": true,
	},
	"syscall": {
		"Accept": true, "Accept4": true, "Bind": true, "Listen": true, "Socket": true,
	},
	"golang.org/x/sys/windows": {
		"Accept": true, "AcceptEx": true, "Bind": true, "Listen": true,
		"Socket": true, "WSASocket": true,
	},
}

// moduleRelativePath is the exception authority. Issue #94's previous
// filepath.Base checks let any nested leak_test.go disable the whole guard and
// any nested loopback.go inherit the loopback exception. A normalized path below
// the located module root is the minimum identity an exception may trust.
func moduleRelativePath(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", &os.PathError{Op: "relative path escaped module root", Path: path, Err: os.ErrInvalid}
	}
	return filepath.ToSlash(rel), nil
}

// scanNetworkPolicy keeps selection and inspection in one error-returning path.
// There is no "best effort" mode: a walk, read or parse failure means the selected
// source was not proved clean and must reach TestNoNetworkListener as a fatal
// error. Detector findings are data only so the real named test owns the verdict.
func scanNetworkPolicy(root string) ([]networkPolicyFinding, error) {
	var findings []networkPolicyFinding
	var goFiles []parsedNetworkGoFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := moduleRelativePath(root, path)
		if err != nil {
			return err
		}
		ext := strings.ToLower(filepath.Ext(rel))
		if ext != ".go" && !shippedTextExtensions[ext] {
			return nil
		}
		data, err := networkPolicyReadFile(path)
		if err != nil {
			return err
		}
		if ext == ".go" {
			file, err := parseNetworkGoFile(rel, data)
			if err != nil {
				return err
			}
			goFiles = append(goFiles, parsedNetworkGoFile{rel: rel, file: file})
			return nil
		}
		if detail := endpointPolicyFinding(string(data)); detail != "" && !endpointFindingAllowed(rel, string(data)) {
			findings = append(findings, networkPolicyFinding{path: rel, rule: "local endpoint literal", detail: detail})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	groups := make(map[string][]*ast.File)
	for _, selected := range goFiles {
		key := filepath.ToSlash(filepath.Dir(selected.rel)) + "\x00" + selected.file.Name.Name
		groups[key] = append(groups[key], selected.file)
	}
	policies := make(map[string]networkPackagePolicy, len(groups))
	for key, files := range groups {
		policies[key] = networkPackagePolicy{
			constants: packageStringConstantExpressions(files),
			types:     packageTypeExpressions(files),
			loaders:   packageLoaderFunctionExpressions(files),
		}
	}
	for _, selected := range goFiles {
		key := filepath.ToSlash(filepath.Dir(selected.rel)) + "\x00" + selected.file.Name.Name
		policy := policies[key]
		goFindings, err := scanParsedGoNetworkPolicy(selected.rel, selected.file, policy.constants, policy.types, policy.loaders)
		if err != nil {
			return nil, err
		}
		findings = append(findings, goFindings...)
	}
	return findings, nil
}

// scanGoNetworkPolicy parses source without applying build constraints. A dormant
// build-tagged file or test can still create a listener when its build path is
// selected, so the repository promise covers it even when the current GOOS would
// exclude it. Parser errors fail closed rather than silently shrinking that set.
func scanGoNetworkPolicy(rel string, source []byte) ([]networkPolicyFinding, error) {
	file, err := parseNetworkGoFile(rel, source)
	if err != nil {
		return nil, err
	}
	return scanParsedGoNetworkPolicy(rel, file, packageStringConstantExpressions([]*ast.File{file}), packageTypeExpressions([]*ast.File{file}), packageLoaderFunctionExpressions([]*ast.File{file}))
}

func parseNetworkGoFile(rel string, source []byte) (*ast.File, error) {
	file, err := parser.ParseFile(token.NewFileSet(), rel, source, parser.AllErrors)
	if err != nil {
		return nil, &os.PathError{Op: "parse selected Go source", Path: rel, Err: err}
	}
	return file, nil
}

func scanParsedGoNetworkPolicy(rel string, file *ast.File, packageConstants map[string][]packageConstantExpression, packageTypes map[string][]packageTypeExpression, packageLoaders map[string][]packageLoaderExpression) ([]networkPolicyFinding, error) {
	// Import aliases are local spellings, not identities. Resolve them once so
	// n.Listen and net.Listen have the same policy result. Dot imports of a
	// capable package are findings because this deliberately avoids go/types and
	// cannot otherwise distinguish an unqualified prohibited symbol reliably.
	imports := make(map[string]string)
	var findings []networkPolicyFinding
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, &os.PathError{Op: "parse Go import", Path: rel, Err: err}
		}
		name := filepath.Base(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "." && prohibitedNetworkSymbols[importPath] != nil {
			findings = append(findings, networkPolicyFinding{path: rel, rule: "network API", detail: "dot import of " + importPath})
			continue
		}
		if name != "_" {
			imports[name] = importPath
		}
	}

	constants := stringConstantExpressions(file)
	loaders := loaderFunctionExpressions(file)

	// Selector references, rather than only CallExpr nodes, are the semantic
	// compromise that keeps this guard dependency-free and cross-build: they catch
	// calls, method/function values and prohibited types. Local wrappers must still
	// reference one of these symbols somewhere; wrappers implemented wholly in an
	// external dependency remain part of the documented ceiling.
	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.SelectorExpr:
			ident, ok := value.X.(*ast.Ident)
			// Imported package identifiers have no local ast.Object. A non-nil
			// object means a lexical declaration shadows the import spelling.
			if !ok || ident.Obj != nil {
				return true
			}
			importPath := imports[ident.Name]
			if prohibitedNetworkSymbols[importPath][value.Sel.Name] {
				findings = append(findings, networkPolicyFinding{
					path: rel, rule: "network API", detail: importPath + "." + value.Sel.Name,
				})
			}
		case *ast.CallExpr:
			if dynamicWinsockLoad(value, imports, constants, packageConstants, loaders, packageTypes, packageLoaders) {
				findings = append(findings, networkPolicyFinding{path: rel, rule: "network API", detail: "dynamic Winsock load"})
			}
		case *ast.CompositeLit:
			if dynamicWinsockLiteral(value, imports, constants, packageConstants, packageTypes) {
				findings = append(findings, networkPolicyFinding{path: rel, rule: "network API", detail: "Winsock LazyDLL literal"})
			}
		case *ast.BasicLit:
			if value.Kind != token.STRING {
				return true
			}
			literal, err := strconv.Unquote(value.Value)
			if err != nil {
				findings = append(findings, networkPolicyFinding{path: rel, rule: "unreadable string literal", detail: err.Error()})
				return true
			}
			if detail := endpointPolicyFinding(literal); detail != "" && !endpointFindingAllowed(rel, literal) {
				findings = append(findings, networkPolicyFinding{path: rel, rule: "local endpoint literal", detail: detail})
			}
		}
		return true
	})
	return findings, nil
}

// TestNoNetworkListener is intentionally thin. Its authority comes from returning
// every selection/inspection error as fatal and every finding as a test failure.
// TestNoNetworkListenerExercisesRealTraversalAndVerdict runs this exact entrypoint
// in child processes; do not replace that outer lock with helper-only tables.
func TestNoNetworkListener(t *testing.T) {
	findings, err := scanNetworkPolicy(moduleRoot(t))
	if err != nil {
		t.Fatalf("network policy inspection failed: %v", err)
	}
	for _, finding := range findings {
		t.Errorf("%s: %s: %s", finding.path, finding.rule, finding.detail)
	}
}
