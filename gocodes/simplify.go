package gocodes

import (
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"golang.org/x/tools/go/ast/astutil"
)

const (
	SimplifyTheory = `Simplification keeps operative files primary and dependency context secondary.
Context is useful for explanation and cross-file reasoning, but its budget must remain tightly bounded so large repositories cannot crowd out the files being actively changed.
The budget rule is kept separate from the concurrent transform pipeline so policy changes stay testable and reviewable.
Formatting uses goimports to ensure that imports remain synchronized with the code after subtractions (like deleting function bodies or unused types).
Comment deletion does not affect import usage, so goimports is skipped for comment-only transforms to avoid redundant parsing.
Files explicitly requested via patterns (extra context) bypass the simplification logic to ensure their full content is available as requested, while still being accounted for in the token budget.
File ordering (see FileOrderingTheory in files.go) places stable context files first and volatile focus files last, maximizing the common prefix between consecutive requests for LLM prefix caching.

The context token budget is fixed at a constant value (maximumContextTokenBudget) rather than derived from focus file size.
A fixed budget ensures that context files are simplified consistently across requests regardless of changes to focus files,
preserving the prefix cache. A variable budget tied to focus file size would cause context file inclusion/exclusion to vary
whenever focus files are edited, defeating prefix caching for the entire prompt.`

	maximumContextTokenBudget = 32 << 10
)

var showTokenCounts = cmds.Switch("-show-token-counts")

type SimplifyFiles func(files []*File, maxTokens int, countTokens func(string) (int, error)) ([]*File, error)

// calculateMaxContextTokens returns the fixed token budget for context (non-root) files.
// The budget is constant (maximumContextTokenBudget) to ensure that context files are
// simplified to the same level every request, preserving the LLM prefix cache.
// When focus files change, only focus files differ in the prompt; all preceding context
// content remains byte-identical and fully cacheable.
func calculateMaxContextTokens() int {
	return maximumContextTokenBudget
}

func (Module) SimplifyFiles(
	getFileSet GetFileSet,
	logger logs.Logger,
) SimplifyFiles {
	return func(files []*File, maxTokens int, countTokens func(string) (int, error)) ([]*File, error) {
		fset, err := getFileSet()
		if err != nil {
			return nil, err
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		originalFiles := make([]*File, len(files))
		copy(originalFiles, files)

		// Clean up file state on exit to avoid leaking state into cached File objects
		defer func() {
			for _, f := range originalFiles {
				if f == nil {
					continue
				}
				f.transformCond.L.Lock()
				f.Transform = nil
				f.Pending = nil
				f.DoNotSimplify = false
				f.transformCond.Broadcast()
				f.transformCond.L.Unlock()
			}
		}()

		jobChan := make(chan *File, len(files))
		wg := new(sync.WaitGroup)
		startTokenCounters(ctx, jobChan, fset, countTokens, wg)

		for _, file := range files {
			if file.IsGoFile {
				// initial transform
				file.Transform = &Transform{
					Set: file.AstFile,
				}
			} else {
				buf := new(bytes.Buffer)
				if err := formatContentForPrompt(buf, file.Content, file.PackageIsRoot, file.Path); err != nil {
					return nil, err
				}
				file.Transform = &Transform{
					SetContent: buf.Bytes(),
				}
			}
			jobChan <- file
		}

		allTokens := 0
		contextTokens := 0
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
				if !file.PackageIsRoot {
					contextTokens += file.Pending.NumTokens
				}
				file.Confirmed = file.Pending
				file.Pending = nil
			}
			file.transformCond.Broadcast()
			file.transformCond.L.Unlock()
		}

		focusTokens := allTokens - contextTokens
		maxContextTokens := calculateMaxContextTokens()
		logger.InfoContext(ctx, "initial tokens",
			"all tokens", allTokens,
			"context tokens", contextTokens,
			"focus tokens", focusTokens,
			"max context tokens", maxContextTokens,
		)

		transforms := makeTransforms()

		sema := make(chan bool, runtime.NumCPU()*8)

		// Track number of pending semaphore acquisitions for cleanup on early exit.
		// We use a pointer int64 to share state between the dispatch and confirmation loops.
		pendingSema := new(int64)

		wg.Go(func() {
			for _, transform := range transforms {
				for i := range files {
					file := files[i]
					if file == nil {
						continue
					}
					if file.DoNotSimplify {
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

					select {
					case sema <- true:
						atomic.AddInt64(pendingSema, 1)
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
				file := files[i]
				if file == nil {
					continue
				}

				// Stop simplifying as soon as context tokens fall within the fixed
				// budget. Only the context budget gates simplification — the total
				// token count (allTokens) is intentionally excluded so that context
				// files are simplified to the same level every request regardless of
				// focus file size, preserving the LLM prefix cache. The maxTokens
				// parameter does not influence context simplification depth; it is
				// used solely by the caller (CodeProvider.Parts) for the extra-files
				// budget.
				if contextTokens <= maxContextTokens {
					cancel()
					break loop_ops
				}

				if file.DoNotSimplify {
					continue
				}

				select {
				case <-sema:
					atomic.AddInt64(pendingSema, -1)
				case <-ctx.Done():
					break loop_ops
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
						if !file.PackageIsRoot {
							contextTokens -= file.Confirmed.NumTokens
						}
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
						if !file.PackageIsRoot {
							contextTokens += file.Pending.NumTokens
						}
						file.Confirmed = file.Pending
					}
					file.Pending = nil
					file.transformCond.Broadcast()
				}
				file.transformCond.L.Unlock()

			}
		}

		// Drain any pending semaphore slots after early exit to prevent resource leak.
		for atomic.LoadInt64(pendingSema) > 0 {
			select {
			case <-sema:
				atomic.AddInt64(pendingSema, -1)
			default:
				// No more available, all drained
				atomic.StoreInt64(pendingSema, 0)
			}
		}

		cancel()
		// keep broadcasting to wake up goroutines potentially stuck in Wait()
		allDone := make(chan struct{})
		go func() {
			ticker := time.NewTicker(time.Millisecond * 50)
			defer ticker.Stop()
			for {
				select {
				case <-allDone:
					return
				case <-ticker.C:
					for _, f := range originalFiles {
						if f != nil {
							f.transformCond.Broadcast()
						}
					}
				}
			}
		}()
		wg.Wait()
		close(allDone)

		if numFilesFromRootPackageDeleted > 0 {
			return nil, fmt.Errorf("files from root packages deleted: %d", numFilesFromRootPackageDeleted)
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
	// Cap the number of concurrent token‑counting workers to a fixed limit
	// rather than using runtime.NumCPU(). Formatting large ASTs and running
	// the tokenizer both allocate substantial memory; unbounded concurrency
	// on high‑core machines can cause sporadic OOM when many large files
	// happen to be processed simultaneously.
	const maxTokenCounterWorkers = 8
	numWorkers := min(runtime.NumCPU(), maxTokenCounterWorkers)
	for range numWorkers {
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

	Set                 *ast.File
	SetContent          []byte
	DeleteTestFiles     bool
	DeleteMarkdownFiles bool
	DeleteComments      bool
	DeleteFunctionBody  bool
	DeleteFile          bool

	// SkipImports skips goimports processing when the transform cannot
	// change import usage (e.g., comment deletion), avoiding redundant
	// parsing and formatting overhead.
	SkipImports bool
}

func makeTransforms() (ops []*Transform) {

	// non-root module
	ops = append(ops, &Transform{
		MatchModuleIsRoot:   new(false),
		DeleteTestFiles:     true,
		DeleteMarkdownFiles: true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot: new(false),
		DeleteComments:    true,
		SkipImports:       true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot:  new(false),
		DeleteFunctionBody: true,
	})

	// root module, non-root package
	ops = append(ops, &Transform{
		MatchModuleIsRoot:   new(true),
		MatchPackageIsRoot:  new(false),
		DeleteTestFiles:     true,
		DeleteMarkdownFiles: true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot:  new(true),
		MatchPackageIsRoot: new(false),
		DeleteComments:     true,
		SkipImports:        true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot:  new(true),
		MatchPackageIsRoot: new(false),
		DeleteFunctionBody: true,
	})

	// delete non-root files
	ops = append(ops, &Transform{
		MatchModuleIsRoot: new(false),
		DeleteFile:        true,
	})
	ops = append(ops, &Transform{
		MatchModuleIsRoot:  new(true),
		MatchPackageIsRoot: new(false),
		DeleteFile:         true,
	})

	// root package
	ops = append(ops, &Transform{
		MatchPackageIsRoot: new(true),
		DeleteFile:         true,
	})

	return
}

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

	// Capture skipImports before the formatting defer runs so it is
	// available even after f.Transform is set to nil by a later defer.
	skipImports := f.Transform.SkipImports
	defer func() {
		if f.Pending == nil {
			return
		}
		var content string
		if f.Pending.Ast != nil {
			buf := new(bytes.Buffer)
			if err := formatASTForPrompt(buf, f.Pending.Ast, fset, f.PackageIsRoot, f.Path, skipImports); err != nil {
				panic(err)
			}
			content = buf.String()
			f.Pending.Content = buf.Bytes()
			// Release the AST immediately after formatting so the GC can
			// reclaim it before the confirmation loop clears f.Pending.
			// This reduces the peak live heap when many files are in flight.
			f.Pending.Ast = nil
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

	// delete markdown file
	if f.Transform.DeleteMarkdownFiles && strings.HasSuffix(strings.ToLower(f.Path), ".md") {
		f.Pending = &Transformed{
			What: "delete markdown file",
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

// matchPattern reports whether the relative path matches the glob pattern,
// using standard filepath.Match semantics with added support for ** matching
// zero or more directory components.
func matchPattern(name, pattern string) bool {
	return matchParts(splitPath(name), splitPath(pattern))
}

func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	return strings.Split(filepath.Clean(p), string(filepath.Separator))
}

func matchParts(nameParts, patternParts []string) bool {
	if len(patternParts) == 0 {
		return len(nameParts) == 0
	}
	if len(nameParts) == 0 {
		for _, p := range patternParts {
			if p != "**" {
				return false
			}
		}
		return true
	}
	p := patternParts[0]
	if p == "**" {
		// match zero or more directories
		for i := 0; i <= len(nameParts); i++ {
			if matchParts(nameParts[i:], patternParts[1:]) {
				return true
			}
		}
		return false
	}
	ok, err := filepath.Match(p, nameParts[0])
	if err != nil || !ok {
		return false
	}
	return matchParts(nameParts[1:], patternParts[1:])
}
