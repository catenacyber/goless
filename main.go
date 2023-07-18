package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"go/ast"
	"go/printer"
	"go/token"

	"golang.org/x/tools/go/packages"
)

func inspect(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.SelectorExpr:
		ident, ok := x.X.(*ast.Ident)
		if ok {
			l, ok := pkgUsages[ident.Name]
			if ok {
				l.Uses[x.Sel.Name] = true
			}
		}
	}
	return true
}

type PkgUsing struct {
	Path string
	Uses map[string]bool
}

var pkgUsages map[string]*PkgUsing

func PackageFromName(pkgname string) (*packages.Package, error) {
	cfg := &packages.Config{Mode: packages.NeedFiles | packages.NeedSyntax | packages.NeedCompiledGoFiles | packages.NeedName}
	pkgs, err := packages.Load(cfg, pkgname)
	if err != nil {
		return nil, err
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("Unexpectedly got %d packages", len(pkgs))
	}
	return pkgs[0], nil
}

func processPackageNeededExports(exports *PkgUsing) error {
	pkg, err := PackageFromName(exports.Path)
	if err != nil {
		return err
	}

	toprocess := make([]string, 0, len(exports.Uses))
	for k, _ := range exports.Uses {
		toprocess = append(toprocess, k)
	}

NextProcess:
	for i := 0; i < len(toprocess); i++ {
		use := toprocess[i]
		for s := range pkg.Syntax {
			for d := range pkg.Syntax[s].Decls {
				switch f := pkg.Syntax[s].Decls[d].(type) {
				//TODOwhenNeeded other declarations types
				case *ast.FuncDecl:
					if use == f.Name.Name {
						//TODOwhenNeeded process f.Recv and f.Type
						for b := range f.Body.List {
							ast.Inspect(f.Body.List[b], func(n ast.Node) bool {
								switch x := n.(type) {
								/*case *ast.AssignStmt:
								x.Lhs = x.Lhs[:0]*/
								//TODOwhenNeeded other types
								case *ast.IndexExpr:
									id, ok := x.X.(*ast.Ident)
									if ok {
										_, already := exports.Uses[id.Name]
										if !already {
											toprocess = append(toprocess, id.Name)
											exports.Uses[id.Name] = true
										}
									}
								case *ast.CallExpr:
									id, ok := x.Fun.(*ast.Ident)
									if ok {
										switch id.Name {
										case "make", "len", "string":
										//skip
										default:
											_, already := exports.Uses[id.Name]
											if !already {
												toprocess = append(toprocess, id.Name)
												exports.Uses[id.Name] = true
											}
										}
									}
								}
								return true
							})
						}
						continue NextProcess
					}
				}
			}
		}
	}
	return nil
}

func processPackageRewrite(exports *PkgUsing) error {
	pkg, err := PackageFromName(exports.Path)
	if err != nil {
		return err
	}

	for s := range pkg.Syntax {
		fcopy, err := os.Create(filepath.Base(pkg.CompiledGoFiles[s]))
		if err != nil {
			log.Printf("Failed creating file : %s", err)
			return err
		}
		ast.FilterFile(pkg.Syntax[s], func(s string) bool {
			_, used := exports.Uses[s]
			return used
		})
		pkg.Syntax[s].Comments = pkg.Syntax[s].Comments[:0]
		fset := token.NewFileSet()
		printer.Fprint(fcopy, fset, pkg.Syntax[s])
		defer fcopy.Close()
	}
	return nil
}

func main() {
	// only works with local package
	pkg, err := PackageFromName(".")
	if err != nil {
		log.Fatalf("Failed to load package : %s", err)
	}

	// build this structures of imports usages
	pkgUsages = make(map[string]*PkgUsing)

	//TODOwhenNeeded take into account variable shadowing a package name
	for s := range pkg.Syntax {
		for p := range pkg.Syntax[s].Imports {
			name := pkg.Syntax[s].Imports[p].Path.Value
			if pkg.Syntax[s].Imports[p].Name != nil {
				name = pkg.Syntax[s].Imports[p].Name.Name
			} else {
				name = name[1 : len(name)-1]
				path := strings.Split(name, "/")
				name = path[len(path)-1]
			}
			//TODO2 remove
			if name == "stdhex" {
				pkgUsages[name] = &PkgUsing{pkg.Syntax[s].Imports[p].Path.Value[1 : len(pkg.Syntax[s].Imports[p].Path.Value)-1], make(map[string]bool)}
			}
		}
		ast.Inspect(pkg.Syntax[s], inspect)
	}

	err = os.Mkdir("goless", 0750)
	if err != nil && !os.IsExist(err) {
		log.Fatal(err)
	}
	os.Chdir("goless")
	basepath, _ := os.Getwd()
	for k, v := range pkgUsages {
		err = processPackageNeededExports(v)
		if err != nil {
			log.Fatalf("Failed to process package %s : %s", k, err)
		}
	}
	for k, v := range pkgUsages {
		err := os.MkdirAll(v.Path, 0750)
		if err != nil && !os.IsExist(err) {
			log.Fatal(err)
		}
		os.Chdir(v.Path)
		err = processPackageRewrite(v)
		os.Chdir(basepath)
		if err != nil {
			log.Fatalf("Failed to rewrite package %s : %s", k, err)
		}
	}

	// rewrite our current package to use reduced imports
	for s := range pkg.Syntax {
		for p := range pkg.Syntax[s].Imports {
			pkg.Syntax[s].Imports[p].Path.Value = `"` + filepath.Join(pkg.PkgPath, "goless", pkg.Syntax[s].Imports[p].Path.Value[1:])
		}
		fset := token.NewFileSet()
		fcopy, err := os.Create("goless" + filepath.Base(pkg.CompiledGoFiles[s]))
		if err != nil && !os.IsExist(err) {
			log.Fatal(err)
		}
		printer.Fprint(fcopy, fset, pkg.Syntax[s])
		fcopy.Close()
	}
}
