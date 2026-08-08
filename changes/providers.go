package changes

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/reusee/tai/blocks"
	"github.com/reusee/tai/debugs"
	"github.com/reusee/tai/pathutil"
	"golang.org/x/tools/imports"
)

const TheoryOfDscopeProvidedApplyFunctions = `
All apply functions are dscope-provided function types. WriteErrorLog is never
passed as a parameter; it is captured from the dscope scope at provider
resolution time. This follows the core dscope principle: static dependencies
(WriteErrorLog, resolved once from the scope and unchanged during execution) are
provided via dscope, not passed as parameters. Only dynamic parameters (runtime
values like the target store/root and change block content) are passed as
function arguments.

The public types (ApplyChangeBlock, ApplyChangeBlockStore, ApplyChangeBlocks,
ApplyChangeBlocksStore, ApplyDiffFile, BuildChangeBlockHandler) are
dscope-provided function types with no WriteErrorLog in their signatures.

Internal helpers (CallWriteErrorLog, ParseAndFormat, ApplySpecialTargetModify,
ApplyFileLevelOp, ApplyTextLevelOp, ApplyGoModification) are exported
dscope-provided types that decompose the apply logic into focused units. They
must be exported because dscope uses reflect to discover provider methods. The
dependency chain flows from WriteErrorLog through CallWriteErrorLog to
ParseAndFormat, then to ApplySpecialTargetModify and ApplyGoModification, and
finally to ApplyChangeBlockStore, ApplyChangeBlock, ApplyChangeBlocks,
ApplyChangeBlocksStore, ApplyDiffFile, and BuildChangeBlockHandler.
`

// Public dscope-provided function types. These are the types callers inject
// via dscope and call with only runtime parameters. WriteErrorLog is never
// in the signature; it is captured at provider resolution time.

// FileWriteTimes provider: the process-wide write-time tracker shared by
// every root store created through the dscope graph. A scope's providers
// are re-evaluated on dscope.Reset, so independent sessions (e.g., goal
// loops) start with a fresh tracker: each session loads the current
// filesystem state, and conflict detection covers writes within one
// session. See TheoryOfWriteConflictDetection.
func (Module) FileWriteTimes() *FileWriteTimes {
	return NewFileWriteTimes()
}

// ApplyChangeBlock applies a change block to the given root.
type ApplyChangeBlock func(root *os.Root, h ChangeBlock) error

// ApplyChangeBlockStore applies a change block to the given FileStore.
type ApplyChangeBlockStore func(store FileStore, h ChangeBlock) error

// ApplyChangeBlocks applies all change blocks to the given root.
type ApplyChangeBlocks func(bs []blocks.Block, root *os.Root) error

// ApplyChangeBlocksStore applies all change blocks to the given FileStore.
type ApplyChangeBlocksStore func(bs []blocks.Block, store FileStore) error

// ApplyDiffFile streams change blocks from a boundary-delimited diff file,
// applies each one to the working tree, and removes successfully applied
// change blocks from the diff file.
type ApplyDiffFile func(root *os.Root, diffFilePath string) iter.Seq2[ChangeBlock, error]

// Internal dscope-provided function types. These decompose the apply logic
// into focused units, each capturing its dependencies via dscope provider
// parameters. They must be exported because dscope uses reflect to discover
// provider methods, and reflect only finds exported methods.
// See TheoryOfDscopeProvidedApplyFunctions.

// CallWriteErrorLog writes a structured error log entry when a change block
// application fails. WriteErrorLog is captured from the dscope scope.
type CallWriteErrorLog func(h ChangeBlock, src []byte, modified []byte, applyErr error)

// ParseAndFormat parses the modified Go source to catch syntax errors before
// goimports, then runs goimports for formatting and import synchronization.
// CallWriteErrorLog is captured from the dscope scope.
type ParseAndFormat func(path string, h ChangeBlock, src []byte, modified []byte, prefixLen int) ([]byte, error)

// ApplySpecialTargetModify handles MODIFY operations for the special Go-only
// targets "package" and "import". ParseAndFormat is captured from the dscope
// scope.
type ApplySpecialTargetModify func(store FileStore, path string, src []byte, f *ast.File, fset *token.FileSet, prefixLen int, h ChangeBlock) error

// ApplyFileLevelOp handles RENAME, WRITE, and DELETE * operations. Returns
// (handled, error). When handled is false, the caller continues to the main
// modification path. ParseAndFormat is captured from the dscope scope for
// WRITE operations on Go files.
type ApplyFileLevelOp func(store FileStore, path string, h ChangeBlock) (bool, error)

// ApplyTextLevelOp handles REPLACE, INSERT_BEFORE, INSERT_AFTER for non-Go
// text files. CallWriteErrorLog is captured from the dscope scope.
type ApplyTextLevelOp func(store FileStore, path string, src []byte, h ChangeBlock) error

// ApplyGoModification handles structural Go file modifications (MODIFY,
// ADD_BEFORE, ADD_AFTER, DELETE, special targets). CallWriteErrorLog,
// ParseAndFormat, and ApplySpecialTargetModify are captured from the dscope
// scope.
type ApplyGoModification func(store FileStore, path string, src []byte, h ChangeBlock) error

// CallWriteErrorLog provider: captures WriteErrorLog from the dscope scope.
func (Module) CallWriteErrorLog(
	writeErrorLog debugs.WriteErrorLog,
) CallWriteErrorLog {
	return func(h ChangeBlock, src []byte, modified []byte, applyErr error) {
		if writeErrorLog == nil {
			return
		}
		_ = writeErrorLog(debugs.ErrorLogContext{
			Operation:    h.Op,
			Target:       h.Target,
			FilePath:     h.FilePath,
			Find:         h.Find,
			ChangeBlock:  h.Body,
			SourceFile:   string(src),
			ModifiedFile: string(modified),
			Error:        applyErr.Error(),
		})
	}
}

// ParseAndFormat provider: captures CallWriteErrorLog from the dscope scope.
func (Module) ParseAndFormat(
	callWriteErrorLog CallWriteErrorLog,
) ParseAndFormat {
	return func(path string, h ChangeBlock, src []byte, modified []byte, prefixLen int) ([]byte, error) {
		if _, parseErr := parser.ParseFile(token.NewFileSet(), path, modified, parser.ParseComments); parseErr != nil {
			callWriteErrorLog(h, src, modified, parseErr)
			return nil, fmt.Errorf("parse error after apply: %w", parseErr)
		}
		formatted, err := imports.Process(path, modified, nil)
		if err != nil {
			callWriteErrorLog(h, src, modified, err)
			return nil, fmt.Errorf("goimports: %w", err)
		}
		if prefixLen > 0 {
			formatted = formatted[prefixLen:]
		}
		return formatted, nil
	}
}

// ApplySpecialTargetModify provider: captures ParseAndFormat from the dscope
// scope.
func (Module) ApplySpecialTargetModify(
	parseAndFormat ParseAndFormat,
) ApplySpecialTargetModify {
	return func(store FileStore, path string, src []byte, f *ast.File, fset *token.FileSet, prefixLen int, h ChangeBlock) error {
		var newSrc []byte

		switch h.Target {
		case "package":
			newPkgName := strings.TrimSpace(h.Body)
			fset2 := token.NewFileSet()
			if f2, err := parser.ParseFile(fset2, "", newPkgName, parser.PackageClauseOnly); err == nil && f2 != nil {
				newPkgName = f2.Name.Name
			} else if after, found := strings.CutPrefix(newPkgName, "package "); found {
				newPkgName = strings.TrimSpace(after)
			}
			if newPkgName == "" {
				return fmt.Errorf("empty package name in MODIFY package body")
			}

			start := fset.Position(f.Pos()).Offset - prefixLen
			end := fset.Position(f.Name.End()).Offset - prefixLen
			newSrc = make([]byte, 0, len(src)+len("package ")+len(newPkgName))
			newSrc = append(newSrc, src[:start]...)
			newSrc = append(newSrc, []byte("package "+newPkgName)...)
			newSrc = append(newSrc, src[end:]...)

		case "import":
			body := strings.TrimSpace(h.Body)

			var start, end int
			found := false
			for _, decl := range f.Decls {
				if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
					s := fset.Position(genDecl.Pos()).Offset - prefixLen
					e := fset.Position(genDecl.End()).Offset - prefixLen
					if !found || s < start {
						start = s
					}
					if !found || e > end {
						end = e
					}
					found = true
				}
			}
			if !found {
				pkgEnd := fset.Position(f.Name.End()).Offset - prefixLen
				start = pkgEnd
				end = pkgEnd
			}

			if body == "" {
				newSrc = make([]byte, 0, len(src))
				newSrc = append(newSrc, src[:start]...)
				newSrc = append(newSrc, src[end:]...)
			} else {
				_, parseErr := parser.ParseFile(token.NewFileSet(), "", "package p\n"+body, parser.ImportsOnly)
				if parseErr != nil {
					return fmt.Errorf("import body is not valid Go import syntax: %w", parseErr)
				}

				if !strings.HasPrefix(body, "import ") && !strings.HasPrefix(body, "import(") {
					body = "import (\n" + body + "\n)"
				}

				newSrc = make([]byte, 0, len(src)+len(body)+4)
				newSrc = append(newSrc, src[:start]...)
				if !found {
					newSrc = append(newSrc, '\n')
				}
				newSrc = append(newSrc, []byte(body)...)
				newSrc = append(newSrc, '\n')
				newSrc = append(newSrc, src[end:]...)
			}

		default:
			return fmt.Errorf("unknown special target: %q", h.Target)
		}

		formatted, err := parseAndFormat(path, h, src, newSrc, 0)
		if err != nil {
			return err
		}

		return store.WriteFile(path, finalizeContent(formatted), 0644)
	}
}

// ApplyFileLevelOp provider: captures ParseAndFormat from the dscope scope
// for WRITE operations on Go files.
func (Module) ApplyFileLevelOp(
	parseAndFormat ParseAndFormat,
) ApplyFileLevelOp {
	return func(store FileStore, path string, h ChangeBlock) (bool, error) {
		// RENAME
		if h.Op == "RENAME" {
			newPath := h.Target
			if filepath.IsAbs(newPath) {
				cwd, err := os.Getwd()
				if err != nil {
					return false, err
				}
				rel, err := filepath.Rel(cwd, newPath)
				if err != nil || pathutil.EscapesDir(rel) {
					return false, fmt.Errorf("new path outside of current directory: %s", newPath)
				}
				newPath = rel
			}
			if pathutil.EscapesDir(filepath.Clean(newPath)) {
				return false, fmt.Errorf("new path escapes current directory: %s", newPath)
			}
			return true, store.Rename(path, newPath)
		}

		// WRITE
		if h.Op == "WRITE" {
			content := []byte(h.Body)
			if strings.HasSuffix(path, ".go") {
				formatted, err := parseAndFormat(path, h, nil, content, 0)
				if err != nil {
					return true, err
				}
				content = formatted
			}
			return true, store.WriteFile(path, finalizeContent(content), 0644)
		}

		// DELETE *
		if h.Op == "DELETE" && h.Target == "*" {
			if err := store.Remove(path); err != nil {
				if os.IsNotExist(err) {
					return true, nil
				}
				return true, err
			}
			return true, nil
		}

		return false, nil
	}
}

// ApplyTextLevelOp provider: captures CallWriteErrorLog from the dscope scope.
func (Module) ApplyTextLevelOp(
	callWriteErrorLog CallWriteErrorLog,
) ApplyTextLevelOp {
	return func(store FileStore, path string, src []byte, h ChangeBlock) error {
		if isGoFile(path) {
			return fmt.Errorf("Go file %q does not support text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER); use structural operations (MODIFY, ADD_BEFORE, ADD_AFTER, DELETE) instead", path)
		}
		newContent, editErr := applyTextEdit(src, h)
		if editErr != nil {
			callWriteErrorLog(h, src, nil, editErr)
			return editErr
		}
		return store.WriteFile(path, finalizeContent(newContent), 0644)
	}
}

// ApplyGoModification provider: captures CallWriteErrorLog, ParseAndFormat,
// and ApplySpecialTargetModify from the dscope scope.
func (Module) ApplyGoModification(
	callWriteErrorLog CallWriteErrorLog,
	parseAndFormat ParseAndFormat,
	applySpecialTargetModify ApplySpecialTargetModify,
) ApplyGoModification {
	return func(store FileStore, path string, src []byte, h ChangeBlock) error {
		fset := token.NewFileSet()
		var f *ast.File
		var prefixLen int
		if len(src) > 0 {
			var err error
			f, prefixLen, err = parseGoSource(fset, path, src)
			if err != nil {
				callWriteErrorLog(h, src, nil, err)
				return err
			}
		}

		// Special targets (package, import)
		if h.Target == "package" || h.Target == "import" {
			if h.Op != "MODIFY" {
				return fmt.Errorf("target %q only supports MODIFY, got op=%q", h.Target, h.Op)
			}
			if f == nil {
				return fmt.Errorf("target %q: file %s could not be parsed", h.Target, path)
			}
			return applySpecialTargetModify(store, path, src, f, fset, prefixLen, h)
		}

		bodyInfo, _ := getBodyInfo(h.Body)
		if bodyInfo != nil {
			h.Body = string(bodyInfo.Src[bodyInfo.PrefixLen:])
		}
		bodyName := getChangeBlockBodyNameFromInfo(bodyInfo)

		var start, end int
		var finalBody string = h.Body

		// ADD-as-MODIFY conversion
		if (h.Op == "ADD_BEFORE" || h.Op == "ADD_AFTER") && bodyName != "" {
			if s, e, fb, err := findTargetRange(fset, f, ChangeBlock{Op: "MODIFY", Target: bodyName, Body: h.Body}, bodyInfo, len(src), prefixLen); err == nil {
				h.Op = "MODIFY"
				h.Target = bodyName
				start, end, finalBody = s, e, fb
			}
		}

		// Resolve target range
		if start == 0 && end == 0 {
			var err error
			start, end, finalBody, err = findTargetRange(fset, f, h, bodyInfo, len(src), prefixLen)
			if err != nil {
				if h.Op == "MODIFY" || h.Op == "DELETE" {
					return nil
				}
				start, end = len(src), len(src)
			}
		}

		// ADD keyword re-add
		if (h.Op == "ADD_BEFORE" || h.Op == "ADD_AFTER") && bodyInfo != nil && bodyInfo.Keyword != "" {
			finalBody = bodyInfo.Keyword + " " + finalBody
		}

		// Build range items (includes multi-entity duplicate removal)
		items := buildRangeItems(src, start, end, finalBody, h, bodyInfo, f, fset, prefixLen)

		// Build modified source
		newSrc := buildModifiedSource(src, items, h)

		// Parse and format
		outputSrc := newSrc
		outputPrefixLen := 0
		if !hasPackage(newSrc) {
			outputSrc = append([]byte("package p\n"), newSrc...)
			outputPrefixLen = len("package p\n")
		}
		formatted, err := parseAndFormat(path, h, src, outputSrc, outputPrefixLen)
		if err != nil {
			return err
		}

		return store.WriteFile(path, finalizeContent(formatted), 0644)
	}
}

// ApplyChangeBlockStore provider: the main entry point for applying a single
// change block to a FileStore. Path resolution, file-level ops, text-level
// ops, non-Go handling, and Go modification are delegated to focused
// dscope-provided functions.
func (Module) ApplyChangeBlockStore(
	applyFileLevelOp ApplyFileLevelOp,
	applyTextLevelOp ApplyTextLevelOp,
	applyGoModification ApplyGoModification,
) ApplyChangeBlockStore {
	return func(store FileStore, h ChangeBlock) error {
		path := h.FilePath
		if filepath.IsAbs(path) {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(cwd, path)
			if err != nil || pathutil.EscapesDir(rel) {
				return fmt.Errorf("path outside of current directory: %s", path)
			}
			path = rel
		}
		if pathutil.EscapesDir(filepath.Clean(path)) {
			return fmt.Errorf("path escapes current directory: %s", path)
		}

		// File-level operations (RENAME, WRITE, DELETE *)
		handled, err := applyFileLevelOp(store, path, h)
		if err != nil {
			return err
		}
		if handled {
			return nil
		}

		// Read file
		src, err := store.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}

		// Text-level operations (REPLACE, INSERT_BEFORE, INSERT_AFTER)
		if isTextLevelOperation(h.Op) {
			if err != nil {
				return err
			}
			return applyTextLevelOp(store, path, src, h)
		}

		// Non-Go file handling
		if !strings.HasSuffix(path, ".go") {
			if os.IsNotExist(err) && h.Op == "ADD_BEFORE" && h.Target == "BEGIN" {
				body := h.Body
				return store.WriteFile(path, []byte(body), 0644)
			}
			return fmt.Errorf("only .go files are supported for modification: %s", path)
		}

		// Go file modification
		return applyGoModification(store, path, src, h)
	}
}

// ApplyChangeBlock provider: wraps root in a rootStore with write conflict
// detection and delegates to ApplyChangeBlockStore.
func (Module) ApplyChangeBlock(
	applyChangeBlockStore ApplyChangeBlockStore,
	writeTimes *FileWriteTimes,
) ApplyChangeBlock {
	return func(root *os.Root, h ChangeBlock) error {
		return applyChangeBlockStore(NewRootStoreWithWriteTimes(root, writeTimes), h)
	}
}

// ApplyChangeBlocksStore provider: iterates blocks, parses each, and delegates
// to ApplyChangeBlockStore.
func (Module) ApplyChangeBlocksStore(
	applyChangeBlockStore ApplyChangeBlockStore,
) ApplyChangeBlocksStore {
	return func(bs []blocks.Block, store FileStore) error {
		for _, block := range bs {
			h, parsedOk := ParseChangeBlock(block)
			if !parsedOk {
				return fmt.Errorf("unparseable change block with boundary %s", block.Boundary)
			}
			if err := applyChangeBlockStore(store, h); err != nil {
				return fmt.Errorf("apply change block %s %s: %w", h.Op, h.Target, err)
			}
		}
		return nil
	}
}

// ApplyChangeBlocks provider: wraps root in a rootStore with write conflict
// detection and delegates to ApplyChangeBlocksStore.
func (Module) ApplyChangeBlocks(
	applyChangeBlocksStore ApplyChangeBlocksStore,
	writeTimes *FileWriteTimes,
) ApplyChangeBlocks {
	return func(bs []blocks.Block, root *os.Root) error {
		return applyChangeBlocksStore(bs, NewRootStoreWithWriteTimes(root, writeTimes))
	}
}

// ApplyDiffFile provider: streams change blocks from a diff file, applies
// each via ApplyChangeBlock, and removes applied blocks from the diff file.
func (Module) ApplyDiffFile(
	applyChangeBlock ApplyChangeBlock,
) ApplyDiffFile {
	return func(root *os.Root, diffFilePath string) iter.Seq2[ChangeBlock, error] {
		return func(yield func(ChangeBlock, error) bool) {
			content, err := root.ReadFile(diffFilePath)
			if err != nil {
				content, err = os.ReadFile(diffFilePath)
				if err != nil {
					yield(ChangeBlock{}, err)
					return
				}
			}

			writeDiff := func() error {
				trimmed := bytes.TrimSpace(content)
				if err := root.WriteFile(diffFilePath, trimmed, 0644); err != nil {
					return os.WriteFile(diffFilePath, trimmed, 0644)
				}
				return nil
			}

			modified := false
			cursor := 0
			for {
				block, relStart, relEnd, ok, err := blocks.ParseFirstBlock(content[cursor:])
				if err != nil {
					if modified {
						writeDiff()
					}
					yield(ChangeBlock{}, err)
					return
				}
				if !ok {
					break
				}
				start := cursor + relStart
				end := cursor + relEnd
				if block.Kind != "change" {
					cursor = end
					continue
				}
				h, parsedOk := ParseChangeBlock(block)
				if !parsedOk {
					cursor = end
					continue
				}
				if err := applyChangeBlock(root, h); err != nil {
					if modified {
						writeDiff()
					}
					yield(h, fmt.Errorf("change block %s %s: %w", h.Op, h.Target, err))
					return
				}
				content = append(content[:start], content[end:]...)
				modified = true
				cursor = min(start, len(content))
				if !yield(h, nil) {
					writeDiff()
					return
				}
			}
			if modified {
				if err := writeDiff(); err != nil {
					yield(ChangeBlock{}, err)
					return
				}
			}
		}
	}
}
