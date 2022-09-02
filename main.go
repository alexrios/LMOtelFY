package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

type Config struct {
	TelemetryImport        string
	AllowedDirFragments    []string
	DisallowedDirFragments []string
	DryRun                 bool
}

func PathContainsAnyFragment(allowedDirFragments []string, fullPath string) bool {
	for _, fragment := range allowedDirFragments {
		if strings.Contains(fullPath, fragment) {
			return true
		}
	}
	return false
}

func main() {
	var path string
	var importLine string
	var dirFragmentsFlag string
	var disallowedDirFragmentsFlag string
	var dryrun bool

	flag.StringVar(&path, "path", ".", "project path")
	flag.StringVar(&importLine, "import", "github.com/example/extensions/telemetry", "telemtry import line")
	flag.StringVar(&dirFragmentsFlag, "allowed-dirs", "samples", "allowed dir fragments")
	flag.StringVar(&disallowedDirFragmentsFlag, "disallowed-dirs", ".", "allowed dir fragments")
	flag.BoolVar(&dryrun, "dry-run", true, "dry run")

	flag.Parse()

	dirFragments := strings.Split(dirFragmentsFlag, ",")
	dirDisallowedFragments := strings.Split(disallowedDirFragmentsFlag, ",")

	config := Config{
		TelemetryImport:        importLine,
		AllowedDirFragments:    dirFragments,
		DisallowedDirFragments: dirDisallowedFragments,
		DryRun:                 dryrun,
	}

	err := os.Chdir(path)
	if err != nil {
		panic(err)
	}

	err = filepath.WalkDir(path, func(s string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && PathContainsAnyFragment(config.DisallowedDirFragments, s) {
			return filepath.SkipDir
		}
		if d.IsDir() && PathContainsAnyFragment(config.AllowedDirFragments, s) {
			instrument(s, config)
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
}

func instrument(dir string, cfg Config) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, parser.ParseComments)
	if err != nil {
		panic(err)
	}
	for _, pkg := range pkgs {
		for fileName, file := range pkg.Files {
			analyzeFile(fset, file, cfg)

			buf := new(bytes.Buffer)
			err := format.Node(buf, fset, file)
			switch {
			case err != nil:
				fmt.Printf("error: %v\n", err)
				if !cfg.DryRun {
					os.WriteFile(fileName, buf.Bytes(), 0664)
				}
			case fileName[len(fileName)-8:] != "_test.go":
				if cfg.DryRun {
					fmt.Fprintln(os.Stdout, buf.String())
				} else {
					os.WriteFile(fileName, buf.Bytes(), 0664)
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
				for _, v := range fn.Body.List {
					if exp, ok := v.(*ast.IfStmt); ok && isIfErrBlock(exp) {
						exp.Body.List = append(createRecordErrorStmt(), exp.Body.List...)
					}
				}
			}
			if isExportedWithContext(fn) {
				astutil.AddImport(fset, file, cfg.TelemetryImport)
				otel := createOtelStatementsByOperation(fmt.Sprintf(`"%s.%s"`, file.Name.Name, fn.Name.Name), telemetryPackageID(cfg.TelemetryImport))
				fn.Body.List = append(otel, fn.Body.List...)
				for _, v := range fn.Body.List {
					if exp, ok := v.(*ast.IfStmt); ok && isIfErrBlock(exp) {
						exp.Body.List = append(createRecordErrorStmt(), exp.Body.List...)
					}
				}
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

func isIfErrBlock(ifStmt *ast.IfStmt) bool {
	// is: (err != nil)
	if binExpr, ok := ifStmt.Cond.(*ast.BinaryExpr); ok {
		// is: !=
		if binExpr.Op != token.NEQ {
			return false
		}
		// Check left hand identifier (err)
		if ident, ok := binExpr.X.(*ast.Ident); ok {
			if ident.Obj == nil {
				return false
			}
			if ident.Obj.Kind != ast.Var || ident.Name != "err" {
				return false
			}
		}
		// Check right hand identifier (nil)
		if ident, ok := binExpr.Y.(*ast.Ident); ok {
			if ident.Obj != nil || ident.Name != "nil" {
				return false
			}
		}
		return true
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
				// function has one argument
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

func createRecordErrorStmt() []ast.Stmt {
	// thes statement is a call expr:
	// span.RecordError(err)
	x := ast.ExprStmt{
		X: &ast.CallExpr{
			// Finish from 'span' identifier
			Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "span"},
				Sel: &ast.Ident{Name: "RecordError"},
			},
			Args: []ast.Expr{
				&ast.Ident{Name: "err"},
			},
		},
	}
	return []ast.Stmt{&x}
}
