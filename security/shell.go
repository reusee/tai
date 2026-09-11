package security

import (
	"fmt"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

const TheoryOfShellSecurity = `
Shell command execution runs any program: a program allowlist is hard to keep
correct and unnecessary, because the process already runs inside the container
sandbox, where the filesystem is read-only except for the working directory,
the Go and config directories, /tmp, and /dev/shm (see
TheoryOfContainerIsolation). The validator parses the command with
mvdan.cc/sh/v3 and walks the syntax tree.

Structural rules: only simple commands (CallExpr) and binary commands
(pipelines, &&, ||) are accepted; every other shell construct (if, while,
for, case, subshell, block, function definition, arithmetic command, test
clause, declaration) is rejected, so the walk stays total. Output
redirections (>, >>, <>, >&, >|, >>|, >&|, >>&, >>&|) are rejected because
they write files outside the change-block flow; input-only redirections
(<, <<, <&) are permitted. Background execution, coprocesses, and disown
are rejected because their output and lifetime cannot be captured.

Command substitutions, process substitutions, heredoc bodies, arithmetic
expansion, parameter expansion, and brace expansion are recursively
validated by the same rules, so a destructive command nested in another
command's argument is caught in the same walk.

Destructive-pattern filter: an rm command whose target is the filesystem root
(/ or /*), a top-level system directory (an absolute path of a single
component, such as /usr, /etc, or /home), the whole current directory (*, .,
.., ./*, ../*), or the home directory (~, $HOME, and their /* forms) is
rejected. Targets are matched on the argument's literal text after path
cleaning; a dynamic target such as an unexpanded variable passes. The filter
stays deliberately small — it catches the common catastrophic shapes and lets
every other command through — because enumerating dangerous commands is the
same error-prone exercise as the allowlist; the container sandbox remains the
real containment.
`

// ValidateShellCommand checks whether a command string is safe to execute.
// It uses mvdan.cc/sh/v3 to parse the command into an AST and walks the tree
// to enforce the destructive-pattern, redirection, and command substitution
// rules. Returns nil if the command passes all checks, or an error describing
// why the command was rejected.
// See TheoryOfShellSecurity.
func ValidateShellCommand(cmdStr string) error {
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return fmt.Errorf("empty command")
	}

	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(cmdStr), "")
	if err != nil {
		return fmt.Errorf("failed to parse command: %w", err)
	}

	for _, stmt := range file.Stmts {
		if err := validateStmt(stmt); err != nil {
			return err
		}
	}

	return nil
}

// validateStmt validates a single shell statement, checking for background
// execution, coprocesses, disown, output redirections, heredoc command
// substitutions, and recursively validating the command.
// See TheoryOfShellSecurity.
func validateStmt(stmt *syntax.Stmt) error {
	if stmt.Background {
		return fmt.Errorf("background execution (&) is not allowed")
	}
	if stmt.Coprocess {
		return fmt.Errorf("coprocess execution is not allowed")
	}
	if stmt.Disown {
		return fmt.Errorf("disown is not allowed")
	}
	for _, redir := range stmt.Redirs {
		if isOutputRedirOp(redir.Op) {
			return fmt.Errorf("output redirection (%s) is not allowed", redir.Op)
		}
		if redir.Word != nil {
			if err := validateWord(redir.Word); err != nil {
				return err
			}
		}
		if redir.Hdoc != nil {
			if err := validateWord(redir.Hdoc); err != nil {
				return err
			}
		}
	}
	return validateCmd(stmt.Cmd)
}

// validateCmd validates a shell command node. Only simple commands (CallExpr)
// and binary commands (pipelines, &&, ||) are permitted; all other command
// types are rejected. See TheoryOfShellSecurity.
func validateCmd(cmd syntax.Command) error {
	switch c := cmd.(type) {
	case *syntax.CallExpr:
		return validateCallExpr(c)
	case *syntax.BinaryCmd:
		if err := validateStmt(c.X); err != nil {
			return err
		}
		return validateStmt(c.Y)
	case nil:
		return nil
	default:
		return fmt.Errorf("command type %T is not allowed", cmd)
	}
}

// validateCallExpr validates a simple command (CallExpr): it recursively
// validates command and process substitutions in assignments and arguments,
// then applies the destructive-pattern filter. Any program may run.
// See TheoryOfShellSecurity.
func validateCallExpr(call *syntax.CallExpr) error {
	// Validate command/process substitutions in assignment values.
	for _, assign := range call.Assigns {
		if assign.Value != nil {
			if err := validateWord(assign.Value); err != nil {
				return err
			}
		}
	}

	if len(call.Args) == 0 {
		if len(call.Assigns) > 0 {
			return fmt.Errorf("variable assignment is not allowed")
		}
		return nil
	}

	// Validate command/process substitutions in all arguments.
	for _, arg := range call.Args {
		if err := validateWord(arg); err != nil {
			return err
		}
	}

	return checkDangerousCommand(call)
}

// checkDangerousCommand applies the destructive-pattern filter to a simple
// command. Only rm is filtered: an rm whose target is the filesystem root, a
// top-level system directory, the whole current directory, or the home
// directory is rejected. Every other program passes untouched; see
// TheoryOfShellSecurity for why the filter stays this small.
func checkDangerousCommand(call *syntax.CallExpr) error {
	if len(call.Args) == 0 || filepath.Base(wordString(call.Args[0])) != "rm" {
		return nil
	}
	for _, arg := range call.Args[1:] {
		target := wordString(arg)
		// A flag, or the empty rendering of a fully dynamic argument, is
		// not a target.
		if target == "" || strings.HasPrefix(target, "-") {
			continue
		}
		if isDangerousRMTarget(target) {
			return fmt.Errorf("deleting %q is not allowed: it would remove the filesystem root, a top-level system directory, the whole current directory, or the home directory", target)
		}
	}
	return nil
}

// isDangerousRMTarget reports whether a deletion target names something that
// must not be removed wholesale: the filesystem root, a top-level system
// directory, the whole contents of the current directory, or the home
// directory. The target is path-cleaned first, so "//" and "../." resolve to
// their canonical forms; a non-matching target passes.
func isDangerousRMTarget(target string) bool {
	clean := filepath.Clean(target)
	switch clean {
	case "/", "*", ".", "..", "./*", "../*", "~", "~/*", "$HOME", "$HOME/*", "${HOME}", "${HOME}/*":
		return true
	}
	if rest, ok := strings.CutPrefix(clean, "/"); ok {
		rest = strings.TrimSuffix(rest, "/*")
		// A single-component absolute path is a top-level system
		// directory such as /usr, /etc, or /home.
		return rest != "" && !strings.Contains(rest, "/")
	}
	return false
}

// validateWord recursively validates a shell word for command/process
// substitutions, ensuring nested commands pass the same security checks.
// See TheoryOfShellSecurity.
func validateWord(w *syntax.Word) error {
	if w == nil {
		return nil
	}
	for _, part := range w.Parts {
		if err := validateWordPart(part); err != nil {
			return err
		}
	}
	return nil
}

// validateWordPart validates a single word part for command/process
// substitutions and nested expansions. Command substitutions ($(cmd)),
// process substitutions (<(cmd), >(cmd)), arithmetic expansion ($((expr))),
// parameter expansion (${var}), and brace expansion ({a,b}) are recursively
// validated against the allowlist. Double-quoted parts are checked
// recursively because command substitution is active inside double quotes.
// Single-quoted parts and literals are safe (no substitution is active) and
// skipped. See TheoryOfShellSecurity.
func validateWordPart(part syntax.WordPart) error {
	switch p := part.(type) {
	case *syntax.CmdSubst:
		for _, stmt := range p.Stmts {
			if err := validateStmt(stmt); err != nil {
				return err
			}
		}
	case *syntax.ProcSubst:
		for _, stmt := range p.Stmts {
			if err := validateStmt(stmt); err != nil {
				return err
			}
		}
	case *syntax.DblQuoted:
		for _, dp := range p.Parts {
			if err := validateWordPart(dp); err != nil {
				return err
			}
		}
	case *syntax.ArithmExp:
		if p.X != nil {
			if err := validateArithmExpr(p.X); err != nil {
				return err
			}
		}
	case *syntax.ParamExp:
		if err := validateParamExp(p); err != nil {
			return err
		}
	case *syntax.BraceExp:
		for _, elem := range p.Elems {
			if err := validateWord(elem); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateArithmExpr recursively validates an arithmetic expression tree for
// nested command and process substitutions. Arithmetic expressions can contain
// Words (which may contain CmdSubst) in bash, e.g., $(( $(cmd) )).
// See TheoryOfShellSecurity.
func validateArithmExpr(expr syntax.ArithmExpr) error {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *syntax.BinaryArithm:
		if err := validateArithmExpr(e.X); err != nil {
			return err
		}
		return validateArithmExpr(e.Y)
	case *syntax.UnaryArithm:
		return validateArithmExpr(e.X)
	case *syntax.ParenArithm:
		return validateArithmExpr(e.X)
	case *syntax.FlagsArithm:
		return validateArithmExpr(e.X)
	case *syntax.Word:
		return validateWord(e)
	}
	return nil
}

// wordString extracts the literal text of a shell word: Lit, SglQuoted, and
// DblQuoted parts are concatenated, and a plain parameter expansion ($name or
// ${name}) renders as its source text so the destructive-pattern filter can
// recognize targets such as $HOME. Every other part (command substitution,
// brace expansion, parameter expansion with modifiers) contributes nothing,
// so a dynamic word cannot accidentally match a dangerous target.
func wordString(w *syntax.Word) string {
	if w == nil {
		return ""
	}
	var sb strings.Builder
	for _, part := range w.Parts {
		writeWordPartString(&sb, part)
	}
	return sb.String()
}

// writeWordPartString writes the literal text of a word part to the builder.
// It handles Lit, SglQuoted, and DblQuoted parts, and renders a plain
// parameter expansion as its source text: "$NAME" or "${NAME}". Every other
// part type produces no output.
func writeWordPartString(sb *strings.Builder, part syntax.WordPart) {
	switch p := part.(type) {
	case *syntax.Lit:
		sb.WriteString(p.Value)
	case *syntax.SglQuoted:
		sb.WriteString(p.Value)
	case *syntax.DblQuoted:
		for _, dp := range p.Parts {
			writeWordPartString(sb, dp)
		}
	case *syntax.ParamExp:
		if p.Param == nil {
			return
		}
		if p.Short {
			sb.WriteString("$")
			sb.WriteString(p.Param.Value)
		} else {
			sb.WriteString("${")
			sb.WriteString(p.Param.Value)
			sb.WriteString("}")
		}
	}
}

// validateParamExp recursively validates a parameter expansion for nested
// command and process substitutions. Parameter expansions can contain nested
// WordParts (NestedParam), array index arithmetic (Index, Slice), and
// replacement patterns (Repl.Orig, Repl.With) that include command
// substitutions, e.g., ${var/$(cmd)/x}. See TheoryOfShellSecurity.
func validateParamExp(pe *syntax.ParamExp) error {
	if pe.NestedParam != nil {
		if err := validateWordPart(pe.NestedParam); err != nil {
			return err
		}
	}
	if pe.Index != nil {
		if err := validateArithmExpr(pe.Index); err != nil {
			return err
		}
	}
	if pe.Slice != nil {
		if pe.Slice.Offset != nil {
			if err := validateArithmExpr(pe.Slice.Offset); err != nil {
				return err
			}
		}
		if pe.Slice.Length != nil {
			if err := validateArithmExpr(pe.Slice.Length); err != nil {
				return err
			}
		}
	}
	if pe.Repl != nil {
		if pe.Repl.Orig != nil {
			if err := validateWord(pe.Repl.Orig); err != nil {
				return err
			}
		}
		if pe.Repl.With != nil {
			if err := validateWord(pe.Repl.With); err != nil {
				return err
			}
		}
	}
	if pe.Exp != nil && pe.Exp.Word != nil {
		if err := validateWord(pe.Exp.Word); err != nil {
			return err
		}
	}
	return nil
}

// isOutputRedirOp reports whether a redirection operator writes to a file or
// duplicates an output file descriptor. Input-only operators (<, <<, <&) are
// excluded. See TheoryOfShellSecurity.
func isOutputRedirOp(op syntax.RedirOperator) bool {
	switch op {
	case syntax.RdrOut, syntax.AppOut, syntax.RdrInOut, syntax.DplOut,
		syntax.RdrClob, syntax.AppClob, syntax.RdrAll, syntax.RdrAllClob,
		syntax.AppAll, syntax.AppAllClob:
		return true
	}
	return false
}
