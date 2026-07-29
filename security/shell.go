package security

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

const TheoryOfShellSecurity = `
Shell command execution is protected by a command allowlist policy enforced
through AST-level parsing with mvdan.cc/sh/v3. The shell parser produces a
syntax tree that is walked to validate each command, providing accurate
detection of redirections, command substitutions, and complex shell constructs
that string-based splitting cannot reliably handle. Only CallExpr (simple
commands) and BinaryCmd (pipelines, &&, ||) are permitted; all other shell
constructs (if, while, for, case, subshell, block, function definitions, arithmetic
commands, test clauses, declarations) are rejected as unnecessary for read-only
diagnostic operations.

The validator checks each statement's redirections for output operators (>,
>>, <>, >&, >|, >>|, >&|, >>&, >>&|). Unlike the prior string-contains approach,
the AST parser correctly distinguishes > characters inside quoted strings
(e.g., echo "a > b") from actual redirection operators, eliminating false
positives while maintaining the same security boundary. Input-only redirections
(<, <<, <&) are permitted.

Command substitutions ($(cmd) and <(cmd)/>(cmd)) are recursively validated: the
nested statements are subject to the same allowlist and redirection checks. This
prevents bypassing the allowlist via substitution, e.g., echo $(rm -rf /) is
rejected because rm is not in the allowed list. The recursive validation extends
into double-quoted strings ("$(cmd)") since command substitution is active inside
double quotes.

The find command's -exec, -execdir, -ok, and -okdir flags remain explicitly
forbidden to prevent arbitrary command execution through find. Commands with
absolute paths (e.g., /usr/bin/ls) are normalized via filepath.Base before
allowlist lookup. Commands that can execute arbitrary code (awk, sed) are
excluded from the allowlist entirely. This is a conservative security boundary:
it is better to reject a safe command than to allow a dangerous one. Rejected
commands return an error message as user content so the model can adjust and
retry with an allowed command.

Additional security checks extend the boundary beyond the original allowlist and
redirection policy. Background processes (cmd &) and coprocesses (coproc cmd)
are rejected because they start detached processes that bypass the timeout and
output capture. The Disown flag is rejected for the same reason. Heredoc bodies
(redir.Hdoc) are validated for command substitutions, closing a bypass where
cat with a heredoc containing $(cmd) would execute cmd inside the heredoc.

Arithmetic expansion, parameter expansion, and brace expansion are recursively
validated for nested command and process substitutions. In bash, arithmetic
expansion can contain command substitutions, and parameter expansion can contain
nested expansions, array index arithmetic, and replacement patterns that include
command substitutions. Brace expansion elements can also contain command
substitutions. Without recursive validation, these constructs bypass the
allowlist.

Interpreter inline execution flags are blocked for scripting languages that can
execute arbitrary code from command-line arguments: python and python3 with -c
(inline code) or -m (module execution); node with -e/--eval (inline code),
-p/--print (evaluate and print), or -r/--require (module loading). The go -exec
flag is blocked because it specifies an external command to run test binaries,
enabling arbitrary command execution. The cargo subcommand list is restricted to
read and diagnostic operations, explicitly excluding cargo run which executes the
compiled program. The java command is restricted to version-only flags because
java -jar and java ClassName both execute arbitrary code.

The env command is restricted to environment variable inspection only. env can
be used to run commands (env VAR=value command), bypassing the allowlist because
the command name appears as an argument to env rather than as the command
itself. Arguments to env that are not flags (starting with -) and not VAR=value
assignments are rejected as potential command execution.
`

// allowedCommands defines the set of commands permitted for shell block
// execution and their optional subcommand constraints. When the subcommand
// list is nil, all subcommands are allowed. When non-empty, only the listed
// subcommands are permitted.
// See TheoryOfShellSecurity.
var allowedCommands = map[string][]string{
	// File viewing (read-only)
	"ls":   nil,
	"cat":  nil,
	"head": nil,
	"tail": nil,
	"wc":   nil,
	"file": nil,
	"stat": nil,
	"tree": nil,
	"du":   nil,
	"df":   nil,

	// Search (read-only)
	"grep":    nil,
	"rg":      nil,
	"find":    nil, // -exec is checked separately
	"which":   nil,
	"whereis": nil,

	// Text processing (read-only)
	"sort":   nil,
	"uniq":   nil,
	"cut":    nil,
	"tr":     nil,
	"diff":   nil,
	"comm":   nil,
	"paste":  nil,
	"column": nil,

	// System information (read-only)
	"pwd":      nil,
	"echo":     nil,
	"printf":   nil,
	"env":      nil,
	"printenv": nil,
	"date":     nil,
	"uname":    nil,
	"hostname": nil,
	"whoami":   nil,
	"uptime":   nil,
	"free":     nil,
	"ps":       nil,

	// Git read-only subcommands
	"git": {"status", "diff", "log", "show", "blame", "ls-files", "ls-tree",
		"describe", "rev-parse", "help", "version"},

	// Go toolchain (read-only/diagnostic subcommands)
	"go": {"test", "build", "vet", "list", "doc", "version", "env", "help"},

	// Package managers (read-only subcommands)
	"npm":  {"list", "view", "info", "outdated", "audit", "ls"},
	"yarn": {"list", "info", "outdated"},
	"pnpm": {"list", "info", "outdated"},

	// Version information
	"node":    nil,
	"python":  nil,
	"python3": nil,
	"java":    {"--version", "-version"},
	"rustc":   nil,
	"cargo":   {"build", "test", "check", "vet", "metadata", "tree", "info", "search", "clean", "doc", "fetch", "--version", "-V"},
	"gcc":     nil,
	"make":    nil,
	"cmake":   nil,
}

// dangerousFindFlags are find command flags that allow arbitrary command
// execution and must be explicitly forbidden.
// See TheoryOfShellSecurity.
var dangerousFindFlags = map[string]bool{
	"-exec":    true,
	"-execdir": true,
	"-ok":      true,
	"-okdir":   true,
}

// dangerousCommandFlags defines flags that enable inline code execution for
// interpreters and tools. When a command in the allowlist has these flags in
// its arguments, the command is rejected to prevent arbitrary code execution
// through command-line flags. See TheoryOfShellSecurity.
var dangerousCommandFlags = map[string]map[string]bool{
	"python":  {"-c": true, "-m": true},
	"python3": {"-c": true, "-m": true},
	"node":    {"-e": true, "--eval": true, "-p": true, "--print": true, "-r": true, "--require": true},
	"go":      {"-exec": true},
}

// ValidateShellCommand checks whether a command string is safe to execute.
// It uses mvdan.cc/sh/v3 to parse the command into an AST and walks the tree
// to enforce the allowlist, redirection, and command substitution policies.
// Returns nil if the command passes all security checks, or an error
// describing why the command was rejected.
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

// validateCallExpr validates a simple command (CallExpr) against the allowlist.
// It checks the command name, subcommand constraints, dangerous find flags,
// dangerous interpreter flags, env command bypass, and recursively validates
// command/process substitutions in arguments and assignments.
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

	cmdName := filepath.Base(wordString(call.Args[0]))
	allowedSubs, ok := allowedCommands[cmdName]
	if !ok {
		return fmt.Errorf("command %q is not in the allowed list", cmdName)
	}

	if allowedSubs != nil {
		if len(call.Args) < 2 {
			return fmt.Errorf("command %q requires a subcommand from: %s", cmdName, strings.Join(allowedSubs, ", "))
		}
		subcommand := wordString(call.Args[1])
		if !slices.Contains(allowedSubs, subcommand) {
			return fmt.Errorf("subcommand %q is not allowed for %q; allowed: %s", subcommand, cmdName, strings.Join(allowedSubs, ", "))
		}
	}

	// Check for dangerous find flags that allow arbitrary command execution.
	if cmdName == "find" {
		for _, arg := range call.Args[1:] {
			argStr := wordString(arg)
			if dangerousFindFlags[argStr] {
				return fmt.Errorf("find %s is not allowed for security reasons", argStr)
			}
		}
	}

	// Check for dangerous interpreter flags that allow inline code
	// execution (e.g., python -c, node -e, go test -exec).
	// See TheoryOfShellSecurity.
	if containsDangerousFlag(cmdName, call.Args[1:]) {
		return fmt.Errorf("command %q with inline execution flag is not allowed for security reasons", cmdName)
	}

	// env can be used to run commands (env VAR=value command), bypassing
	// the allowlist because the command name appears as an argument to
	// env rather than as the command itself. Reject any argument that is
	// not a flag or VAR=value assignment.
	// See TheoryOfShellSecurity.
	if cmdName == "env" {
		for _, arg := range call.Args[1:] {
			argStr := wordString(arg)
			if argStr == "" || strings.HasPrefix(argStr, "-") {
				continue
			}
			if idx := strings.Index(argStr, "="); idx > 0 {
				continue
			}
			return fmt.Errorf("env must not be used to execute commands")
		}
	}

	return nil
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

// wordString extracts the literal string value from a shell word by
// concatenating Lit, SglQuoted, and DblQuoted parts. Non-literal parts
// (parameter expansion, command substitution, brace expansion, etc.) produce
// empty contributions, which causes the resulting string to not match any
// allowlist entry — a conservative rejection for dynamic command names.
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

// writeWordPartString writes the literal string value of a word part to the
// builder. It handles Lit, SglQuoted, and DblQuoted parts; other part types
// (ParamExp, CmdSubst, BraceExp, etc.) produce no output.
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

// containsDangerousFlag checks if any argument matches a dangerous flag for
// the given command. Flags can appear as standalone (e.g., -c), combined with
// a value (e.g., -ccode for -c code), or in --flag=value form.
// See TheoryOfShellSecurity.
func containsDangerousFlag(cmdName string, args []*syntax.Word) bool {
	dangerousFlags, ok := dangerousCommandFlags[cmdName]
	if !ok {
		return false
	}
	for _, arg := range args {
		argStr := wordString(arg)
		for flag := range dangerousFlags {
			if argStr == flag {
				return true
			}
			if strings.HasPrefix(argStr, flag+"=") {
				return true
			}
			// Combined short flag with value (e.g., -ccode for -c code).
			// Only applies to single-dash flags, not --long flags.
			if !strings.HasPrefix(flag, "--") && len(argStr) > len(flag) && strings.HasPrefix(argStr, flag) {
				return true
			}
		}
	}
	return false
}
