package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

type Config struct {
	TelemetryImport string
	AllowedPackages []string
}

func (c Config) FromPackagesFn() func(string) bool {
	return func(p string) bool {
		for _, pkg := range c.AllowedPackages {
			if pkg == p {
				return true
			}
		}
		return false
	}
}

func main() {
	config := Config{
		TelemetryImport: "github.com/dlpco/example/extensions/telemetry",
		AllowedPackages: []string{"domain", "samples"},
	}

	instrument("./samples", config)
}

func instrument(dir string, cfg Config) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	isAllowedPkg := cfg.FromPackagesFn()
	for _, pkg := range pkgs {
		if isAllowedPkg(pkg.Name) {
			for fileName, file := range pkg.Files {
				analyzeFile(fset, file, cfg)

				buf := new(bytes.Buffer)
				err := format.Node(buf, fset, file)
				switch {
				case err != nil:
					fmt.Printf("error: %v\n", err)
					//ioutil.WriteFile(fileName, buf.Bytes(), 0664)
				case fileName[len(fileName)-8:] != "_test.go":
					fmt.Fprintln(os.Stdout, buf.String())
				}
			}
		}
	}
}

func analyzeFile(fset *token.FileSet, file *ast.File, cfg Config) {
	ast.Inspect(file, func(n ast.Node) bool {
		// current node is a function?
		if fn, ok := n.(*ast.FuncDecl); ok {
			if isHTTPHandler(fn) {
				astutil.AddImport(fset, file, cfg.TelemetryImport)
				otel := createOtelStatementsByOperation(fmt.Sprintf(`"%s.%s"`, file.Name.Name, fn.Name.Name), telemetryPackageID(cfg.TelemetryImport))
				fn.Body.List = append(otel, fn.Body.List...)
			}
			if isExportedWithContext(fn) {
				astutil.AddImport(fset, file, cfg.TelemetryImport)
				otel := createOtelStatementsByOperation(fmt.Sprintf(`"%s.%s"`, file.Name.Name, fn.Name.Name), telemetryPackageID(cfg.TelemetryImport))
				fn.Body.List = append(otel, fn.Body.List...)
			}
		}
		return true
	})
}

func telemetryPackageID(telemetryImport string) string {
	lastIndex := strings.LastIndex(telemetryImport, "/")
	return telemetryImport[lastIndex+1:]
}

func isHTTPHandler(fn *ast.FuncDecl) bool {
	// retreive function's parameter list
	params := fn.Type.Params.List
	// we are only interested in functions with exactly 2 parameters
	if len(params) == 2 {
		isResponseWriter := FormatNode(params[0].Type) == "http.ResponseWriter"
		isHTTPRequest := FormatNode(params[1].Type) == "*http.Request"
		return isHTTPRequest && isResponseWriter
	}
	return false
}

func isExportedWithContext(fn *ast.FuncDecl) bool {
	if strings.ToUpper(fn.Name.Name[:1]) != fn.Name.Name[:1] {
		return false
	}
	// look for context
	for _, param := range fn.Type.Params.List {
		if FormatNode(param.Type) == "context.Context" {
			return true
		}
	}

	return false
}

func FormatNode(node ast.Node) string {
	buf := new(bytes.Buffer)
	_ = format.Node(buf, token.NewFileSet(), node)
	return buf.String()
}

func createOtelStatementsByOperation(op string, telemetryPackage string) []ast.Stmt {
	// first statement is the assignment:
	// ctx, span := telemetry.FromContext(r.Context()).Start(r.Context(), operation)
	a1 := ast.AssignStmt{
		// token.DEFINE is :=
		Tok: token.DEFINE,
		// left hand side has two identifiers, span and ctx
		Lhs: []ast.Expr{
			&ast.Ident{Name: "ctx"},
			&ast.Ident{Name: "span"},
		},
		// right hand is a call to function
		Rhs: []ast.Expr{
			&ast.CallExpr{
				// function is taken from a package 'telemetry'
				Fun: &ast.SelectorExpr{
					X: &ast.CallExpr{
						Fun: &ast.SelectorExpr{
							X:   &ast.Ident{Name: telemetryPackage},
							Sel: &ast.Ident{Name: "FromContext"},
						},
						Args: []ast.Expr{
							&ast.Ident{Name: "ctx"},
						},
					},
					Sel: &ast.Ident{Name: "Start"},
				},
				// function has two arguments
				Args: []ast.Expr{
					// r.Context()
					&ast.Ident{Name: "ctx"},
					&ast.BasicLit{Kind: token.STRING, Value: op},
				},
			},
		},
	}

	// last statement is 'defer'
	a2 := ast.DeferStmt{
		// what function call should be deferred?
		Call: &ast.CallExpr{
			// Finish from 'span' identifier
			Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "span"},
				Sel: &ast.Ident{Name: "End"},
			},
		},
	}

	return []ast.Stmt{&a1, &a2}
}
