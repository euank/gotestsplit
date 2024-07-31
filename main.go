package main

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/format"
	"go/printer"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/stoewer/go-strcase"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/imports"
)

type flags struct {
	outputBase string
	snakeCase  bool
}

func main() {
	var f flags
	cmd := cobra.Command{
		Args: cobra.ExactArgs(2),
		Use: `
Split a provided go test file into multiple test files.

    gotestsplit github.com/my/go/package/path foo_test.go --snake-case=false --output-base=split

would result in three output files, 'split_NameOf1Test_test.go', 'split_NameOfAnotherTest_test.go', etc, with each file containing exactly one test.`,
		RunE: func(c *cobra.Command, args []string) error {
			return run(c.Context(), f, args[0], args[1])
		},
	}
	cmd.Flags().StringVarP(&f.outputBase, "output-base", "", "", "base name for output files")
	cmd.Flags().BoolVarP(&f.snakeCase, "snake-case", "", true, "snake case filenames")

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error running command: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, f flags, pkg, file string) error {
	dir := filepath.Dir(file)
	pkgs, err := packages.Load(&packages.Config{
		Context: ctx,
		Mode:    packages.NeedFiles | packages.NeedImports | packages.NeedTypesInfo | packages.NeedTypes | packages.NeedSyntax,
		Dir:     dir,
		Tests:   true,
	}, pkg)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	var p *packages.Package
	var fi *ast.File
	var seenFiles []string
	for _, pp := range pkgs {
		for _, f := range pp.Syntax {
			fpos := pp.Fset.Position(f.Pos())
			absFname, err := filepath.Abs(fpos.Filename)
			if err != nil {
				return err
			}
			seenFiles = append(seenFiles, absFname)
			if absFname == target {
				fi = f
				p = pp
			}
			if fi != nil {
				break
			}
		}
	}
	if fi == nil {
		return fmt.Errorf("could not find %q, saw %v", target, seenFiles)
	}

	fName := func(name string) string {
		name = strings.TrimPrefix(name, "Test")
		if f.snakeCase {
			return strcase.SnakeCase(name)
		}
		return name
	}

	// process the AST for each test

	importsStr := "import (\n"
	for _, imp := range fi.Imports {
		if imp.Name != nil {
			importsStr += imp.Name.String() + " "
		}
		importsStr += imp.Path.Value + "\n"
	}
	importsStr += ")"

	commentMap := ast.NewCommentMap(p.Fset, fi, fi.Comments)

	var newDecls []ast.Decl
	for _, decl := range fi.Decls {
		switch fn := decl.(type) {
		case *ast.FuncDecl:
			if !strings.HasPrefix(fn.Name.Name, "Test") {
				newDecls = append(newDecls, decl)
				continue
			}

			var out bytes.Buffer

			fmt.Fprintf(&out, `package %s

%s
`, fi.Name.Name, importsStr)

			comments := commentMap.Filter(fn)
			for _, comment := range comments.Comments() {
				fi.Comments = slices.DeleteFunc(fi.Comments, func(c *ast.CommentGroup) bool { return c == comment })
			}
			if err := format.Node(&out, p.Fset, &printer.CommentedNode{
				Node:     fn,
				Comments: comments.Comments(),
			}); err != nil {
				return err
			}

			outBytes, err := format.Source(out.Bytes())
			if err != nil {
				return err
			}

			filename := f.outputBase + "_" + fName(fn.Name.Name) + "_test.go"
			outBytes, err = imports.Process(filename, outBytes, nil)
			if err != nil {
				return err
			}
			err = os.WriteFile(filepath.Join(filepath.Dir(file), filename), outBytes, 0o644)
			if err != nil {
				return err
			}
		default:
			newDecls = append(newDecls, decl)
		}
	}

	// update the old file to only have decls that weren't tests
	fi.Decls = newDecls
	if len(fi.Decls) == 0 {
		// delete the file if it has nothing left
		os.Remove(target)
	} else {
		var out bytes.Buffer
		// otherwise overwrite it without the tests
		if err := format.Node(&out, p.Fset, fi); err != nil {
			return err
		}
		outBytes, err := imports.Process(target, out.Bytes(), nil)
		if err != nil {
			return err
		}
		err = os.WriteFile(target, outBytes, 0o644)
		if err != nil {
			return err
		}
	}

	return nil
}
