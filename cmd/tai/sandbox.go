package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/reusee/tai/apps"
	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/modes"
	"github.com/reusee/tai/sandboxfs"
)

const TheoryOfSandboxCommand = `
The sandbox command is the review half of the filesystem environment: the
session records its writes in a sqlite file, and this command lists the net
changes and the audit trail, then applies the changes the user names.

A listing is the default: the net changes — added, modified, and deleted
paths, with directory, symbolic-link, and named-pipe kinds and any apply
conflict — and the audit trail of every path the session read or mutated.
The change list is the review surface; the audit answers which files the
session touched.

Selection reuses the -file flag: the named files and directories are the
changes to apply, a named directory selects everything beneath it, and the
current directory selects the whole project. No selection lists instead of
apply. A conflicting change is never applied silently: the command reports
it with its reason and leaves the host path untouched, so the user decides.
See sandboxfs.TheoryOfApplyConflict.

The database path is the session's default — sandboxfs.DefaultDBPath of the
working directory — unless the CAI_SANDBOX_DB environment names another
recorded file. A recorded session is browsable at any later time from that
file.
`

const sandboxDBEnv = "CAI_SANDBOX_DB"

// SandboxCommand reviews a recorded sandbox filesystem session and applies
// the changes the user selects. See TheoryOfSandboxCommand.
var SandboxCommand = apps.New("sandbox",
	"Review and apply the session filesystem changes",
	func(
		output Output,
		files flags.Files,
	) apps.MainErr {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		dbPath := os.Getenv(sandboxDBEnv)
		if dbPath == "" {
			dbPath = sandboxfs.DefaultDBPath(cwd)
		}
		if _, err := os.Stat(dbPath); err != nil {
			return fmt.Errorf("sandbox: no recorded session at %s: %w", dbPath, err)
		}
		store, err := sandboxfs.Open(dbPath, "/")
		if err != nil {
			return err
		}
		defer store.Close()

		changes, err := store.Changes()
		if err != nil {
			return err
		}
		selections := sandboxSelectionPaths(files, cwd)
		if len(selections) == 0 {
			if _, err := fmt.Fprint(output, formatSandboxChanges(dbPath, changes)); err != nil {
				return err
			}
			ops, err := store.Ops()
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(output, formatSandboxOps(ops))
			return err
		}

		pred := selectChanges(changes, selections)
		var conflicts []sandboxfs.Change
		for _, c := range changes {
			if pred(c) && c.Conflict != "" {
				conflicts = append(conflicts, c)
			}
		}
		applied, err := store.Apply(pred)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "applied %d change(s) to the host\n", len(applied)); err != nil {
			return err
		}
		for _, c := range applied {
			if _, err := fmt.Fprintf(output, "  %s %s\n", c.Kind, c.Path); err != nil {
				return err
			}
		}
		for _, c := range conflicts {
			if _, err := fmt.Fprintf(output, "  skipped %s %s: %s\n", c.Kind, c.Path, c.Conflict); err != nil {
				return err
			}
		}
		return nil
	},
	modes.ForProduction(),
)

// sandboxSelectionPaths converts the -file selections into the absolute
// sandbox paths the store records: a relative selection resolves against
// the working directory, and a directory selects everything beneath it.
// See TheoryOfSandboxCommand.
func sandboxSelectionPaths(files flags.Files, cwd string) []string {
	var out []string
	for name := range files {
		abs, err := filepath.Abs(filepath.Join(cwd, filepath.FromSlash(name)))
		if err != nil {
			continue
		}
		out = append(out, filepath.ToSlash(abs))
	}
	sort.Strings(out)
	return out
}

// selectChanges returns the apply predicate of the selections: a change is
// selected when its path equals a selection or lies beneath it. No
// selections select nothing; the caller lists instead of applying.
// See TheoryOfSandboxCommand.
func selectChanges(changes []sandboxfs.Change, selections []string) func(sandboxfs.Change) bool {
	return func(c sandboxfs.Change) bool {
		for _, sel := range selections {
			if c.Path == sel || strings.HasPrefix(c.Path, sel+"/") {
				return true
			}
		}
		return false
	}
}

// formatSandboxChanges renders the net change listing: the database path,
// one line per change with its kind and any conflict reason.
// See TheoryOfSandboxCommand.
func formatSandboxChanges(dbPath string, changes []sandboxfs.Change) string {
	var b strings.Builder
	fmt.Fprintf(&b, "sandbox %s\n", dbPath)
	if len(changes) == 0 {
		b.WriteString("no net changes\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%d net change(s):\n", len(changes))
	for _, c := range changes {
		fmt.Fprintf(&b, "  %-8s %s", c.Kind, c.Path)
		switch {
		case c.Dir:
			b.WriteString(" (directory)")
		case c.Symlink:
			b.WriteString(" (symbolic link)")
		case c.FIFO:
			b.WriteString(" (named pipe)")
		}
		if c.Conflict != "" {
			fmt.Fprintf(&b, " CONFLICT: %s", c.Conflict)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// formatSandboxOps renders the audit trail in application order.
// See TheoryOfSandboxCommand.
func formatSandboxOps(ops []sandboxfs.Op) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d audit record(s):\n", len(ops))
	for _, op := range ops {
		fmt.Fprintf(&b, "  %s %-7s %s", op.Time.Format("15:04:05"), op.Op, op.Path)
		if op.Detail != "" {
			fmt.Fprintf(&b, " %s", op.Detail)
		}
		b.WriteString("\n")
	}
	return b.String()
}
