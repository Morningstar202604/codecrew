package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedDiff_NoChange(t *testing.T) {
	text := "line1\nline2\nline3"
	diff := UnifiedDiff("a", "b", text, text, 100)
	if diff != "" {
		t.Errorf("expected empty diff for identical content, got %q", diff)
	}
}

func TestUnifiedDiff_Addition(t *testing.T) {
	old := "line1\nline2"
	newText := "line1\nline2\nline3"
	diff := UnifiedDiff("old", "new", old, newText, 100)

	if !strings.Contains(diff, "--- old") {
		t.Error("diff should contain old file header")
	}
	if !strings.Contains(diff, "+++ new") {
		t.Error("diff should contain new file header")
	}
	if !strings.Contains(diff, "@@") {
		t.Error("diff should contain hunk header")
	}
	if !strings.Contains(diff, "+line3") {
		t.Errorf("diff should contain added line, got %q", diff)
	}
}

func TestUnifiedDiff_Deletion(t *testing.T) {
	old := "line1\nline2\nline3"
	newText := "line1\nline3"
	diff := UnifiedDiff("old", "new", old, newText, 100)

	if !strings.Contains(diff, "-line2") {
		t.Errorf("diff should contain deleted line, got %q", diff)
	}
}

func TestUnifiedDiff_Modification(t *testing.T) {
	old := "func foo() {\n\treturn 1\n}"
	newText := "func foo() {\n\treturn 2\n}"
	diff := UnifiedDiff("old", "new", old, newText, 100)

	if !strings.Contains(diff, "-\treturn 1") {
		t.Errorf("diff should contain old line, got %q", diff)
	}
	if !strings.Contains(diff, "+\treturn 2") {
		t.Errorf("diff should contain new line, got %q", diff)
	}
}

func TestUnifiedDiff_NewFile(t *testing.T) {
	diff := UnifiedDiff("old", "new", "", "line1\nline2", 100)
	if !strings.Contains(diff, "+line1") {
		t.Errorf("diff should show all lines as added, got %q", diff)
	}
	if !strings.Contains(diff, "+line2") {
		t.Errorf("diff should show all lines as added, got %q", diff)
	}
}

func TestUnifiedDiff_Truncation(t *testing.T) {
	var oldLines, newLines []string
	for i := 0; i < 50; i++ {
		oldLines = append(oldLines, "old line")
		newLines = append(newLines, "new line")
	}
	diff := UnifiedDiff("a", "b", strings.Join(oldLines, "\n"), strings.Join(newLines, "\n"), 10)
	if !strings.Contains(diff, "已截断") {
		t.Error("diff should be truncated with maxLines=10")
	}
}

func TestComputeHunks_ContextLines(t *testing.T) {
	old := "a\nb\nc\nd\ne\nf\ng"
	newText := "a\nb\nc\nX\ne\nf\ng"
	hunks := computeHunks(strings.Split(old, "\n"), strings.Split(newText, "\n"), 3)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	h := hunks[0]
	// 上下文 3 行：变更在第 4 行（d→X），hunk 应从第 1 行开始
	if h.OldStart != 1 {
		t.Errorf("expected OldStart=1, got %d", h.OldStart)
	}
	if h.NewStart != 1 {
		t.Errorf("expected NewStart=1, got %d", h.NewStart)
	}
}

func TestPreviewWrite_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	result := PreviewWrite(path, "hello\nworld")
	if !strings.Contains(result, "新建文件") {
		t.Errorf("expected new file message, got %q", result)
	}
}

func TestPreviewWrite_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	os.WriteFile(path, []byte("old content\n"), 0o644)
	result := PreviewWrite(path, "new content\n")
	if !strings.Contains(result, "-old content") {
		t.Errorf("expected diff with old content, got %q", result)
	}
	if !strings.Contains(result, "+new content") {
		t.Errorf("expected diff with new content, got %q", result)
	}
}

func TestPreviewEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644)

	diff, err := PreviewEdit(path, "line2", "LINE2_MODIFIED")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-line2") {
		t.Errorf("expected old line in diff, got %q", diff)
	}
	if !strings.Contains(diff, "+LINE2_MODIFIED") {
		t.Errorf("expected new line in diff, got %q", diff)
	}
}

func TestPreviewEdit_NotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("line1\n"), 0o644)

	_, err := PreviewEdit(path, "nonexistent", "x")
	if err == nil {
		t.Error("expected error for nonexistent old_text")
	}
}

func TestPreviewEdit_MultipleMatches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "edit.txt")
	os.WriteFile(path, []byte("dup\ndup\n"), 0o644)

	_, err := PreviewEdit(path, "dup", "x")
	if err == nil {
		t.Error("expected error for multiple matches")
	}
}
