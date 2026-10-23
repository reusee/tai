package main

import (
	"strings"
	"testing"

	"github.com/reusee/tai/flags"
	"github.com/reusee/tai/sandboxfs"
)

func TestSelectChanges(t *testing.T) {
	changes := []sandboxfs.Change{
		{Path: "/base/a.txt", Kind: sandboxfs.ChangeModified},
		{Path: "/base/d/f.txt", Kind: sandboxfs.ChangeAdded},
		{Path: "/base/e/g.txt", Kind: sandboxfs.ChangeAdded},
	}
	cases := []struct {
		name string
		sel  []string
		want []string
	}{
		{
			name: "exact file",
			sel:  []string{"/base/a.txt"},
			want: []string{"/base/a.txt"},
		},
		{
			name: "directory prefix selects its whole subtree",
			sel:  []string{"/base/d"},
			want: []string{"/base/d/f.txt"},
		},
		{
			name: "the working directory selects every change beneath it",
			sel:  []string{"/base"},
			want: []string{"/base/a.txt", "/base/d/f.txt", "/base/e/g.txt"},
		},
		{
			name: "a sibling with a shared prefix does not match",
			sel:  []string{"/basement"},
			want: nil,
		},
		{
			name: "no selection selects nothing",
			sel:  nil,
			want: nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pred := selectChanges(changes, c.sel)
			var got []string
			for _, change := range changes {
				if pred(change) {
					got = append(got, change.Path)
				}
			}
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestSandboxSelectionPathsResolveAgainstCwd(t *testing.T) {
	files := flags.Files{"pkg": true, "a.txt": true}
	got := sandboxSelectionPaths(files, "/base")
	want := "/base/a.txt,/base/pkg"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v want %s", got, want)
	}
}
