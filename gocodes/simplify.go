package gocodes

import (
	"bytes"
	"context"
	"go/ast"
	"go/token"
	"runtime"
	"strings"
	"sync"

	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/vars"
	"golang.org/x/tools/go/ast/astutil"
)

type SimplifyFiles func(files []*File, maxTokens int, countTokens func(string) (int, error)) ([]*File, error)

func (Module) SimplifyFiles(getFileSet GetFileSet, logger logs.Logger) SimplifyFiles {
	return func(files []*File, maxTokens int, countTokens func(string) (int, error)) ([]*File, error) {
		fset, err := getFileSet()
		if err != nil {
			return nil, err
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		jobChan := make(chan *File, len(files))
		wg := new(sync.WaitGroup)
		startTokenCounters(ctx, jobChan, fset, countTokens, wg)

		for _, file := range files {
			file.transformCond = sync.NewCond(new(sync.Mutex))
			if file.IsGoFile {
				// initial transform
				file.Transform = &Transform{
					Set: file.AstFile,
				}
			} else {
				buf := new(bytes.Buffer)
				err := formatContentForPrompt(buf, file.Content, file.PackageIsRoot, file.Path)
				if err != nil {
					return nil, err
				}
				file.Transform = &Transform{
					SetContent: buf.Bytes(),
				}
			}
			jobChan <- file
		}

		allTokens := 0
		for _, file := range files {
			file.transformCond.L.Lock()
			for file.Transform != nil {
				// wait transform done
				file.transformCond.Wait()
			}
			if file.Pending != nil {
				if *debug {
					logger.InfoContext(ctx, "file operation confirmed",
						"path", file.Path,
						"what", file.Pending.What,
					)
				}
				// confirm
				allTokens += file.Pending.NumTokens
				file.Confirmed = file.Pending
				file.Pending = nil
			}
			file.transformCond.Broadcast()
			file.transformCond.L.Unlock()
		}
		logger.Info("initial tokens", "tokens", allTokens)

		transforms := makeTransforms()

		sema := make(chan bool, runtime.NumCPU()*8)
		wg.Go(func() {
			for _, transform := range transforms {
				for i := range files {

					select {
					case sema <- true:
					case <-ctx.Done():
						return
					}

					file := files[i]
					if file == nil {
						continue
					}

					// set next transform and send job to workers
					file.transformCond.L.Lock()
					for file.Transform != nil || file.Pending != nil {
						select {
						case <-ctx.Done():
							file.transformCond.L.Unlock()
							return
						default:
						}
						// wait last transform to be confirmed
						file.transformCond.Wait()
					}
					file.Transform = transform
					file.transformCond.L.Unlock()
					file.transformCond.Broadcast()
					select {
					case jobChan <- file:
					case <-ctx.Done():
						return
					}

				}
			}
		})

		var numFilesFromRootPackageDeleted int
	loop_ops:
		for range transforms {
			for i := range files {
				<-sema

				if *debug {
					logger.Info("count tokens", "tokens", allTokens)
				}
				if allTokens < maxTokens {
					break loop_ops
				}

				file := files[i]
				if file == nil {
					continue
				}

				file.transformCond.L.Lock()
				for file.Transform != nil {
					// wait transform done
					file.transformCond.Wait()
				}
				if file.Pending != nil {
					// confirm
					if file.Confirmed != nil {
						allTokens -= file.Confirmed.NumTokens
					}
					if *debug {
						logger.InfoContext(ctx, "file operation confirmed",
							"path", file.Path,
							"what", file.Pending.What,
						)
					}
					if file.Pending.Ast == nil && len(file.Pending.Content) == 0 {
						// delete
						files[i] = nil
						file.Confirmed = nil
						if file.PackageIsRoot {
							numFilesFromRootPackageDeleted++
						}
					} else {
						// updated
						allTokens += file.Pending.NumTokens
						file.Confirmed = file.Pending
					}
					file.Pending = nil
					file.transformCond.Broadcast()
				}
				file.transformCond.L.Unlock()

			}
		}

		cancel()
		wg.Wait()

		if numFilesFromRootPackageDeleted > 0 {
			logger.Warn("files from root packages deleted",
				"num", numFilesFromRootPackageDeleted,
			)
		}

		retFiles := files[:0]
		for _, file := range files {
			if file == nil {
				continue
			}
			retFiles = append(retFiles, file)
			if *debug {
				logger.Info("use file", "path", file.Path)
			}
		}

		return retFiles, nil
	}
}

func startTokenCounters(ctx context.Context, jobChan chan *File, fset *token.FileSet, counter generators.TokenCounter, wg *sync.WaitGroup) {
	for range runtime.NumCPU() {
		wg.Go(func() {
			for {
				select {

				case <-ctx.Done():
					return

				case file := <-jobChan:
					file.applyTransform(fset, counter)

				}
			}
		})
	}
}

type Transform struct {
	MatchModuleIsRoot  *bool
	MatchPackageIsRoot *bool

	Set                *ast.File
	SetContent         []byte
	DeleteTestFiles    bool
	DeleteComments     bool
	DeleteStructTags   bool
	DeleteFunctionBody bool
	DeleteFile         bool
}

func makeTransforms() (ops []*Transform) {

	// non-root module
	ops = append(ops, &Transform{
		MatchModuleIsRoot: vars.PtrTo(false),
		DeleteTestFiles:   true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot: vars.PtrTo(false),
		DeleteComments:    true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot: vars.PtrTo(false),
		DeleteStructTags:  true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot:  vars.PtrTo(false),
		DeleteFunctionBody: true,
	})

	// root module, non-root package
	ops = append(ops, &Transform{
		MatchModuleIsRoot:  vars.PtrTo(true),
		MatchPackageIsRoot: vars.PtrTo(false),
		DeleteTestFiles:    true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot:  vars.PtrTo(true),
		MatchPackageIsRoot: vars.PtrTo(false),
		DeleteComments:     true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot:  vars.PtrTo(true),
		MatchPackageIsRoot: vars.PtrTo(false),
		DeleteStructTags:   true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot:  vars.PtrTo(true),
		MatchPackageIsRoot: vars.PtrTo(false),
		DeleteFunctionBody: true,
	})

	// delete non-root files
	ops = append(ops, &Transform{
		MatchModuleIsRoot: vars.PtrTo(false),
		DeleteFile:        true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot:  vars.PtrTo(true),
		MatchPackageIsRoot: vars.PtrTo(false),
		DeleteFile:         true,
	})

	// root package
	ops = append(ops, &Transform{
		MatchPackageIsRoot: vars.PtrTo(true),
		DeleteFile:         true,
	})

	return
}

// applyTransform apply f.Transform and set f.Pending
func (f *File) applyTransform(fset *token.FileSet, counter generators.TokenCounter) {
	f.transformCond.L.Lock()
	defer f.transformCond.L.Unlock()
	defer f.transformCond.Broadcast()

	if f.Transform == nil {
		// no transform
		return
	}
	defer func() {
		f.Transform = nil
	}()

	if f.Pending != nil {
		// Pending must be confirmed before calling applyTransform
		panic("pending is not null")
	}
	defer func() {
		if f.Pending == nil {
			return
		}
		var content string
		if f.Pending.Ast != nil {
			buf := new(bytes.Buffer)
			if err := formatASTForPrompt(buf, f.Pending.Ast, fset, f.PackageIsRoot, f.Path); err != nil {
				panic(err)
			}
			content = buf.String()
			f.Pending.Content = buf.Bytes()
		} else if len(f.Pending.Content) > 0 {
			content = string(f.Pending.Content)
		}
		n, err := counter(content)
		if err != nil {
			panic(err)
		}
		f.Pending.NumTokens = n
	}()

	// match
	if f.Transform.MatchModuleIsRoot != nil && *f.Transform.MatchModuleIsRoot != f.ModuleIsRoot {
		// not matched, no change
		return
	}
	if f.Transform.MatchPackageIsRoot != nil && *f.Transform.MatchPackageIsRoot != f.PackageIsRoot {
		// not matched, no change
		return
	}

	// set
	if f.Transform.Set != nil {
		f.Pending = &Transformed{
			What: "set initial ast",
			Ast:  f.Transform.Set,
		}
		return
	}
	if f.Transform.SetContent != nil {
		f.Pending = &Transformed{
			What:    "set initial content",
			Content: f.Transform.SetContent,
		}
		return
	}

	// delete test file
	if f.Transform.DeleteTestFiles && strings.HasSuffix(f.Path, "_test.go") {
		f.Pending = &Transformed{
			What: "delete test file",
		}
		return
	}

	// delete comments
	if f.Transform.DeleteComments && f.Confirmed != nil {
		if !f.IsGoFile {
			return
		}
		simplified := deleteComments(f.Confirmed.Ast)
		if simplified == nil {
			f.Pending = &Transformed{
				What: "delete empty file after delete comments",
			}
			return
		}
		f.Pending = &Transformed{
			What: "delete comments",
			Ast:  simplified,
		}
		return
	}

	// delete struct tags
	if f.Transform.DeleteStructTags && f.Confirmed != nil {
		if !f.IsGoFile {
			return
		}
		f.Pending = &Transformed{
			What: "delete struct tags",
			Ast:  deleteStructTags(f.Confirmed.Ast),
		}
		return
	}

	// delete function body
	if f.Transform.DeleteFunctionBody && f.Confirmed != nil {
		if !f.IsGoFile {
			return
		}
		f.Pending = &Transformed{
			What: "delete function body",
			Ast:  deleteFunctionBody(f.Confirmed.Ast),
		}
		return
	}

	// delete file
	if f.Transform.DeleteFile {
		f.Pending = &Transformed{
			What: "delete file",
		}
		return
	}

}

func deleteComments(file *ast.File) *ast.File {
	// Apply transformations to remove comments from within declarations.
	newFile := astutil.Apply(file, func(cursor *astutil.Cursor) bool {
		if _, ok := cursor.Node().(*ast.CommentGroup); ok {
			cursor.Replace((*ast.CommentGroup)(nil))
		}
		return true
	}, nil).(*ast.File)

	if newFile == nil {
		return nil
	}

	// Explicitly clear top-level comments as astutil.Apply does not traverse them.
	// If newFile is the same as file, create a copy to modify its Comments field.
	if newFile == file {
		clone := *file
		clone.Comments = nil
		newFile = &clone
	} else {
		// If newFile is already a copy (because Apply made changes elsewhere),
		// just clear its comments.
		newFile.Comments = nil
	}

	// If the file has no declarations and no comments (after clearing), return nil.
	if len(newFile.Decls) == 0 && len(newFile.Comments) == 0 {
		return nil
	}

	return newFile
}

func deleteFunctionBody(file *ast.File) *ast.File {
	return astutil.Apply(file, func(cursor *astutil.Cursor) bool {
		if decl, ok := cursor.Node().(*ast.FuncDecl); ok {
			if decl.Body == nil {
				return true
			}
			newDecl := *decl
			newDecl.Body = &ast.BlockStmt{
				List: []ast.Stmt{
					&ast.ExprStmt{
						X: &ast.CallExpr{
							Fun: &ast.Ident{Name: "panic"},
							Args: []ast.Expr{
								&ast.BasicLit{
									Kind:  token.STRING,
									Value: `"function body omitted"`,
								},
							},
						},
					},
				},
			}
			cursor.Replace(&newDecl)
		}
		return true
	}, nil).(*ast.File)
}

func deleteStructTags(file *ast.File) *ast.File {
	return astutil.Apply(file, func(cursor *astutil.Cursor) bool {
		if field, ok := cursor.Node().(*ast.Field); ok {
			if field.Tag != nil {
				newField := *field
				newField.Tag = nil
				cursor.Replace(&newField)
			}
		}
		return true
	}, nil).(*ast.File)
}
