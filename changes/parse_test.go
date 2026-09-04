package changes

import (
	"strings"
	"testing"
)

func TestParseFirstBoundaryChangeBlock(t *testing.T) {
	content := "<<龘靐 change:?op=MODIFY&target=myFunc&file-path=%2Ffile.go\nfunc myFunc() {}\n龘靐\n"
	h, start, end, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok")
	}
	if h.Op != "MODIFY" {
		t.Fatalf("expected MODIFY, got %s", h.Op)
	}
	if h.Target != "myFunc" {
		t.Fatalf("expected myFunc, got %s", h.Target)
	}
	if h.FilePath != "/file.go" {
		t.Fatalf("expected /file.go, got %s", h.FilePath)
	}
	if !strings.Contains(h.Body, "func myFunc() {}") {
		t.Fatal("body does not contain expected code")
	}
	expectedEnd := len(content)
	if end != expectedEnd {
		t.Fatalf("expected end %d, got %d", expectedEnd, end)
	}
	_ = start

	content2 := "<<齉爩 change:?op=MODIFY&target=myFunc&file-path=%2Ffile.go\nop: MODIFY // comment in body\nfunc myFunc() {}\n齉爩\n"
	h2, _, _, ok2, err2 := ParseFirstBoundaryChangeBlock([]byte(content2))
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if !ok2 {
		t.Fatal("expected ok for content2")
	}
	if h2.Op != "MODIFY" {
		t.Fatal("op should be MODIFY")
	}
	if !strings.Contains(h2.Body, "op: MODIFY // comment in body") {
		t.Fatal("body should contain the header-like line")
	}

	t.Run("RENAME", func(t *testing.T) {
		content := "<<龘靐 change:?op=RENAME&target=new.go&file-path=old.go\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok")
		}
		if h.Op != "RENAME" {
			t.Fatalf("expected RENAME, got %s", h.Op)
		}
		if h.Target != "new.go" {
			t.Fatalf("expected new.go, got %s", h.Target)
		}
		if h.FilePath != "old.go" {
			t.Fatalf("expected old.go, got %s", h.FilePath)
		}
	})

	t.Run("HeaderFormatRejected", func(t *testing.T) {
		content := "<<龘靐 change\nop: MODIFY\ntarget: myFunc\nfile-path: /file.go\n\nfunc myFunc() {}\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ok {
			t.Fatal("bare kind without a query carries no op parameter and must be rejected")
		}
	})
}

func TestParseFirstBoundaryChangeBlockXML(t *testing.T) {
	content := "<<龘靐 change:?op=MODIFY&target=Foo&file-path=%2Ftest.go\nfunc Foo() {}\n龘靐\n"
	h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok")
	}
	if h.Op != "MODIFY" {
		t.Fatalf("expected MODIFY, got %s", h.Op)
	}
	if h.Target != "Foo" {
		t.Fatalf("expected Foo, got %s", h.Target)
	}
	if h.FilePath != "/test.go" {
		t.Fatalf("expected /test.go, got %s", h.FilePath)
	}
	if h.Body != "func Foo() {}" {
		t.Fatalf("unexpected body: %q", h.Body)
	}
}

func TestParseFirstBoundaryChangeBlockXMLRename(t *testing.T) {
	content := "<<龘靐 change:?op=RENAME&target=new.go&file-path=old.go\n龘靐\n"
	h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok")
	}
	if h.Op != "RENAME" || h.Target != "new.go" || h.FilePath != "old.go" {
		t.Fatalf("unexpected change block: %+v", h)
	}
	if h.Body != "" {
		t.Fatalf("body should be empty, got %q", h.Body)
	}
}

func TestParseFirstBoundaryChangeBlockWrite(t *testing.T) {
	content := "<<龘靐 change:?op=WRITE&file-path=%2Ftest.go\npackage x\n\nfunc New() {}\n龘靐\n"
	h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok")
	}
	if h.Op != "WRITE" {
		t.Fatalf("expected WRITE, got %s", h.Op)
	}
	if h.FilePath != "/test.go" {
		t.Fatalf("expected /test.go, got %s", h.FilePath)
	}
	if !strings.Contains(h.Body, "package x") {
		t.Fatalf("body should contain package declaration: %q", h.Body)
	}
	if !strings.Contains(h.Body, "func New() {}") {
		t.Fatalf("body should contain func New: %q", h.Body)
	}
}

func TestParseFirstBoundaryChangeBlockNonGoFileRestriction(t *testing.T) {
	t.Run("WriteSucceeds", func(t *testing.T) {
		content := "<<龘靐 change:?op=WRITE&file-path=%2Ftest.md\n# Markdown\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for WRITE on non-Go file")
		}
		if h.Op != "WRITE" {
			t.Fatalf("expected WRITE, got %s", h.Op)
		}
	})

	t.Run("ModifyOnUnregisteredFileFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=MODIFY&target=someDecl&file-path=%2Ftest.zqx\nModified\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for MODIFY on a file without a registered grammar")
		}
		if ok {
			t.Fatal("expected ok=false for MODIFY on a file without a registered grammar")
		}
	})

	t.Run("ModifyOnRegisteredNonGoFileValidates", func(t *testing.T) {
		content := "<<龘靐 change:?op=MODIFY&target=someHeading&file-path=%2Ftest.md\n# Modified\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for MODIFY on a registered non-Go file")
		}
		if h.Op != "MODIFY" {
			t.Fatalf("expected MODIFY, got %s", h.Op)
		}
	})

	t.Run("AddBeforeOnUnregisteredFileFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=ADD_BEFORE&target=someDecl&file-path=%2Ftest.zqx\nnew content\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for ADD_BEFORE on a file without a registered grammar")
		}
		if ok {
			t.Fatal("expected ok=false for ADD_BEFORE on a file without a registered grammar")
		}
	})

	t.Run("AddAfterOnUnregisteredFileFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=ADD_AFTER&target=someDecl&file-path=%2Ftest.zqx\nnew content\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for ADD_AFTER on a file without a registered grammar")
		}
		if ok {
			t.Fatal("expected ok=false for ADD_AFTER on a file without a registered grammar")
		}
	})

	t.Run("DeleteAllSucceeds", func(t *testing.T) {
		content := "<<龘靐 change:?op=DELETE&target=%2A&file-path=%2Ftest.md\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for DELETE * on non-Go file")
		}
		if h.Op != "DELETE" || h.Target != "*" {
			t.Fatalf("unexpected change block: %+v", h)
		}
	})

	t.Run("DeleteSpecificOnUnregisteredFileFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=DELETE&target=someDecl&file-path=%2Ftest.zqx\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for DELETE with a specific target on a file without a registered grammar")
		}
		if ok {
			t.Fatal("expected ok=false for DELETE with a specific target on a file without a registered grammar")
		}
	})

	t.Run("DeleteSpecificOnRegisteredFileValidates", func(t *testing.T) {
		content := "<<龘靐 change:?op=DELETE&target=someHeading&file-path=%2Ftest.md\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for DELETE by outline path on a registered non-Go file")
		}
		if h.Target != "someHeading" {
			t.Fatalf("unexpected target: %s", h.Target)
		}
	})

	t.Run("RenameSucceeds", func(t *testing.T) {
		content := "<<龘靐 change:?op=RENAME&target=%2Fnew.md&file-path=%2Fold.md\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for RENAME on non-Go file")
		}
		if h.Op != "RENAME" {
			t.Fatalf("expected RENAME, got %s", h.Op)
		}
	})

	t.Run("ModifyGoFileSucceeds", func(t *testing.T) {
		content := "<<龘靐 change:?op=MODIFY&target=Foo&file-path=%2Ftest.go\nfunc Foo() {}\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for MODIFY on Go file")
		}
		if h.Op != "MODIFY" {
			t.Fatalf("expected MODIFY, got %s", h.Op)
		}
	})
}

func TestParseFirstBoundaryChangeBlockPackageImportTargets(t *testing.T) {
	t.Run("ModifyPackageSucceeds", func(t *testing.T) {
		content := "<<龘靐 change:?op=MODIFY&target=package&file-path=%2Ftest.go\npackage newpkg\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for MODIFY package on Go file")
		}
		if h.Op != "MODIFY" || h.Target != "package" {
			t.Fatalf("unexpected change block: %+v", h)
		}
	})

	t.Run("ModifyImportSucceeds", func(t *testing.T) {
		content := "<<龘靐 change:?op=MODIFY&target=import&file-path=%2Ftest.go\nimport \"fmt\"\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for MODIFY import on Go file")
		}
		if h.Op != "MODIFY" || h.Target != "import" {
			t.Fatalf("unexpected change block: %+v", h)
		}
	})

	t.Run("DeletePackageFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=DELETE&target=package&file-path=%2Ftest.go\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for non-MODIFY op on package target")
		}
		if ok {
			t.Fatal("expected ok=false for non-MODIFY on package")
		}
	})

	t.Run("AddBeforeImportFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=ADD_BEFORE&target=import&file-path=%2Ftest.go\nsome text\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for ADD_BEFORE on import target")
		}
		if ok {
			t.Fatal("expected ok=false for non-MODIFY on import")
		}
	})

	t.Run("ModifyPackageOnNonGoFileFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=MODIFY&target=package&file-path=%2Ftest.md\n# Title\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for MODIFY package on non-Go file")
		}
		if ok {
			t.Fatal("expected ok=false for package target on non-Go file")
		}
	})

	t.Run("ModifyImportOnNonGoFileFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=MODIFY&target=import&file-path=%2Ftest.md\n# Title\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for MODIFY import on non-Go file")
		}
		if ok {
			t.Fatal("expected ok=false for import target on non-Go file")
		}
	})
}

func TestParseFirstBoundaryChangeBlockTextLevelOps(t *testing.T) {
	t.Run("ReplaceOnNonGoFile", func(t *testing.T) {
		content := "<<龘靐 change:?op=REPLACE&find=old%20text&file-path=%2Ftest.md\nnew text\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for REPLACE on non-Go file")
		}
		if h.Op != "REPLACE" {
			t.Fatalf("expected REPLACE, got %s", h.Op)
		}
		if h.Find != "old text" {
			t.Fatalf("expected find 'old text', got %q", h.Find)
		}
		if h.FilePath != "/test.md" {
			t.Fatalf("expected /test.md, got %s", h.FilePath)
		}
		if h.Body != "new text" {
			t.Fatalf("expected body 'new text', got %q", h.Body)
		}
	})

	t.Run("InsertBeforeOnNonGoFile", func(t *testing.T) {
		content := "<<龘靐 change:?op=INSERT_BEFORE&find=anchor&file-path=%2Ftest.md\ninserted text\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for INSERT_BEFORE on non-Go file")
		}
		if h.Op != "INSERT_BEFORE" {
			t.Fatalf("expected INSERT_BEFORE, got %s", h.Op)
		}
		if h.Find != "anchor" {
			t.Fatalf("expected find 'anchor', got %q", h.Find)
		}
	})

	t.Run("InsertAfterOnNonGoFile", func(t *testing.T) {
		content := "<<龘靐 change:?op=INSERT_AFTER&find=anchor&file-path=%2Ftest.md\ninserted text\n龘靐\n"
		h, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !ok {
			t.Fatal("expected ok for INSERT_AFTER on non-Go file")
		}
		if h.Op != "INSERT_AFTER" {
			t.Fatalf("expected INSERT_AFTER, got %s", h.Op)
		}
		if h.Find != "anchor" {
			t.Fatalf("expected find 'anchor', got %q", h.Find)
		}
	})

	t.Run("ReplaceWithoutFindFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=REPLACE&file-path=%2Ftest.md\nnew text\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for REPLACE without find parameter")
		}
		if ok {
			t.Fatal("expected ok=false for REPLACE without find parameter")
		}
	})

	t.Run("InsertBeforeWithoutFindFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=INSERT_BEFORE&file-path=%2Ftest.md\ninserted text\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for INSERT_BEFORE without find parameter")
		}
		if ok {
			t.Fatal("expected ok=false for INSERT_BEFORE without find parameter")
		}
	})

	t.Run("ReplaceOnGoFileFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=REPLACE&find=old%20string&file-path=%2Ftest.go\nnew string\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for REPLACE on Go file")
		}
		if ok {
			t.Fatal("expected ok=false for REPLACE on Go file")
		}
		if !strings.Contains(err.Error(), "does not support text-level operations") {
			t.Fatalf("expected text-level operations rejection error, got: %v", err)
		}
	})

	t.Run("InsertBeforeOnGoFileFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=INSERT_BEFORE&find=old%20string&file-path=%2Ftest.go\nnew string\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for INSERT_BEFORE on Go file")
		}
		if ok {
			t.Fatal("expected ok=false for INSERT_BEFORE on Go file")
		}
		if !strings.Contains(err.Error(), "does not support text-level operations") {
			t.Fatalf("expected text-level operations rejection error, got: %v", err)
		}
	})

	t.Run("InsertAfterOnGoFileFails", func(t *testing.T) {
		content := "<<龘靐 change:?op=INSERT_AFTER&find=old%20string&file-path=%2Ftest.go\nnew string\n龘靐\n"
		_, _, _, ok, err := ParseFirstBoundaryChangeBlock([]byte(content))
		if err == nil {
			t.Fatal("expected error for INSERT_AFTER on Go file")
		}
		if ok {
			t.Fatal("expected ok=false for INSERT_AFTER on Go file")
		}
		if !strings.Contains(err.Error(), "does not support text-level operations") {
			t.Fatalf("expected text-level operations rejection error, got: %v", err)
		}
	})
}

func TestValidateChangeBlockGoFileTextLevelOps(t *testing.T) {
	for _, op := range []string{"REPLACE", "INSERT_BEFORE", "INSERT_AFTER"} {
		t.Run(op, func(t *testing.T) {
			h := ChangeBlock{
				Op:       op,
				Find:     "some string",
				FilePath: "test.go",
				Body:     "new content",
			}
			err := ValidateChangeBlock(h)
			if err == nil {
				t.Fatal("expected error for text-level operation on Go file")
			}
			if !strings.Contains(err.Error(), "does not support text-level operations") {
				t.Fatalf("expected text-level operations rejection error, got: %v", err)
			}
		})
	}
}

func TestValidateChangeBlockNonGoFileTextLevelOpsAllowed(t *testing.T) {
	for _, op := range []string{"REPLACE", "INSERT_BEFORE", "INSERT_AFTER"} {
		t.Run(op, func(t *testing.T) {
			h := ChangeBlock{
				Op:       op,
				Find:     "some string",
				FilePath: "test.md",
				Body:     "new content",
			}
			err := ValidateChangeBlock(h)
			if err != nil {
				t.Fatalf("text-level operation on non-Go file should be allowed: %v", err)
			}
		})
	}
}

func TestChangeBlockPromptsUseUncommonChineseDelimiter(t *testing.T) {
	// The delimiter policy lives only in blocks.BlockFormatSystemPrompt.
	// The change block prompt references the general format and must not
	// restate the delimiter policy; ChangeBlockSystemPrompt() embeds the
	// unified format prompt. See TheoryOfBlockFormatGeneral.
	if strings.Contains(ChangeBlockPrompt, "uncommon Chinese two-character word") {
		t.Fatal("ChangeBlockPrompt must not restate the delimiter policy; the unified BlockFormatSystemPrompt covers it")
	}
	for _, legacy := range []string{"<<CHG1", "<<ENDRT", "<<DELIM1", "<<BLOCK1"} {
		if strings.Contains(ChangeBlockPrompt, legacy) {
			t.Fatalf("ChangeBlockPrompt must not display legacy example delimiter %s", legacy)
		}
	}
	if !strings.Contains(ChangeBlockSystemPrompt(), "uncommon Chinese two-character word") {
		t.Fatal("ChangeBlockSystemPrompt must embed the unified BlockFormatSystemPrompt, which states the delimiter policy")
	}
}

func TestChangeBlockPromptPrefersPreciseModifications(t *testing.T) {
	prompt := ChangeBlockSystemPrompt()
	if !strings.Contains(prompt, "Prefer Precise Modifications") {
		t.Fatal("ChangeBlockSystemPrompt should contain guidance to prefer precise modifications over WRITE")
	}
	if !strings.Contains(prompt, "WRITE should only be used when creating a new file") {
		t.Fatal("ChangeBlockSystemPrompt should explain when WRITE is appropriate")
	}
}
