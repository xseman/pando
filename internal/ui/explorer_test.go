package ui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// submit presses enter in a prompt and delivers what its command produces.
func submit(m *Model) {
	_, cmd := m.Update(keyMsg("enter"))
	fire(m, cmd)
}

func TestExplorerContextMenu(t *testing.T) {
	m := testModel(t)
	top := m.bodyTop(viewFiles) // row 0 is src, row 1 README.md

	for _, c := range []struct {
		name string
		y    int
		want []string
	}{
		{"file", top + 1, []string{
			"New File…", "New Folder…", "",
			"Open Containing Folder", "Open with Default App", "Edit in $EDITOR", "",
			"Cut", "Copy", "Paste", "",
			"Copy Name", "Copy Path", "Copy Relative Path", "",
			"Duplicate…", "Rename…", "Delete…", "",
			"Stage Changes", "Collapse All",
		}},
		{"folder", top, []string{
			"New File…", "New Folder…", "",
			"Open Containing Folder", "Find in Folder…", "",
			"Cut", "Copy", "Paste", "",
			"Copy Name", "Copy Path", "Copy Relative Path", "",
			"Duplicate…", "Rename…", "Delete…", "",
			"Stage Changes", "Collapse All",
		}},
		{"below the tree", top + 5, []string{
			"New File…", "New Folder…", "",
			"Open Containing Folder", "Find in Folder…", "",
			"Paste", "",
			"Copy Name", "Copy Path", "",
			"Refresh", "Show Hidden Files", "Collapse All",
		}},
	} {
		click(m, 5, c.y, tea.MouseRight)

		if m.modal == nil || !slices.Equal(labels(m), c.want) {
			t.Fatalf("%s menu = %q, want %q", c.name, labels(m), c.want)
		}

		checkWidths(t, m)
		m.modal = nil
	}

	if m.ex.selected() != nil {
		t.Fatal("a right click below the tree clears the selection, so the menu acts on the root")
	}

	for key, root := range map[string]bool{"O": true, "c": true, "y": true, "F": true, "Y": false, "R": false, "D": false, "d": false, "s": false} {
		if got := m.ex.action(key) != nil; got != root {
			t.Errorf("with nothing selected, action %q applies = %v, want %v", key, got, root)
		}
	}
	// The keys walk over the rule between groups.
	click(m, 5, top+1, tea.MouseRight)
	press(m, "down", "down")

	if got := m.modal.disp[m.modal.l.sel].label; got != "Open Containing Folder" {
		t.Fatalf("down from New Folder… lands on %q", got)
	}
}

func TestExplorerDuplicate(t *testing.T) {
	m := testModel(t)
	readme, src := filepath.Join(m.ws, "README.md"), filepath.Join(m.ws, "src")

	m.ex.reveal(m, readme)
	press(m, "d")

	if m.modal == nil || m.modal.input.Value() != "README copy.md" {
		t.Fatalf("duplicate prompt: %+v", m.modal)
	}

	submit(m)

	if got := mustRead(t, filepath.Join(m.ws, "README copy.md")); got != "# hi\n" {
		t.Fatalf("copy holds %q", got)
	}

	if n := m.ex.selected(); n == nil || n.name != "README copy.md" {
		t.Fatalf("the copy is selected: %+v", n)
	}

	m.ex.reveal(m, readme)
	press(m, "d")

	if v := m.modal.input.Value(); v != "README copy 2.md" {
		t.Fatalf("second copy is offered as %q", v)
	}

	m.modal = nil
	// A folder is copied with everything in it.
	m.ex.reveal(m, src)
	press(m, "d")
	submit(m)

	if got := mustRead(t, filepath.Join(m.ws, "src copy", "deep", "x.go")); got != "package x\n" {
		t.Fatalf("folder copy holds %q", got)
	}
	// Into itself it refuses rather than copying forever.
	m.ex.reveal(m, src)
	press(m, "d")
	m.modal.input.SetValue("src/inner")
	submit(m)

	if _, err := os.Lstat(filepath.Join(src, "inner")); err == nil || !m.msgErr {
		t.Fatalf("duplicating a folder into itself: err=%v flash=%q", err, m.msg)
	}
}

func TestDuplicateKeepsModeAndLinks(t *testing.T) {
	dir := t.TempDir()
	script, link := filepath.Join(dir, "run.sh"), filepath.Join(dir, "latest")
	mustWrite(t, script, "#!/bin/sh\n")

	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("run.sh", link); err != nil {
		t.Fatal(err)
	}

	if err := duplicate(script, script+".bak"); err != nil {
		t.Fatal(err)
	}

	if st, err := os.Stat(script + ".bak"); err != nil || st.Mode().Perm() != 0o755 {
		t.Fatalf("copy mode: %v %v", st, err)
	}

	if err := duplicate(link, link+"2"); err != nil {
		t.Fatal(err)
	}

	if target, err := os.Readlink(link + "2"); err != nil || target != "run.sh" {
		t.Fatalf("a symlink is copied as a link: %q %v", target, err)
	}

	if err := duplicate(script, link); err == nil {
		t.Fatal("duplicate must not overwrite")
	}
}

func TestCopyName(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.tar.gz copy"), "")

	for _, c := range []struct {
		name string
		dir  bool
		want string
	}{
		{"main.go", false, "main copy.go"},
		{".env", false, ".env copy"},
		{"Makefile", false, "Makefile copy"},
		{"v1.2", true, "v1.2 copy"},
		{"a.tar.gz", false, "a.tar copy.gz"},
	} {
		if got := copyName(filepath.Join(dir, c.name), c.dir); got != c.want {
			t.Errorf("copyName(%q) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFindInFolder(t *testing.T) {
	m := testModel(t)

	m.ex.reveal(m, filepath.Join(m.ws, "src", "deep"))
	press(m, "F")

	if v, _ := m.viewOn(m.colOf(viewSearch)); v != viewSearch || !m.sr.query.Focused() || !m.sr.showDetails || m.sr.include.Value() != "src/deep/**" {
		t.Fatalf("Find in Folder: view=%d include=%q details=%v", v, m.sr.include.Value(), m.sr.showDetails)
	}

	press(m, "esc", "1")
	m.ex.l.sel = -1
	press(m, "F")

	if m.sr.include.Value() != "" {
		t.Fatalf("the root searches everything, include=%q", m.sr.include.Value())
	}
}

// A folder named like a glob (Next.js app/[id]) matches itself, not app/i.
func TestFindInFolderQuotesGlobs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "app", "[id]", "page.go"), "needle\n")
	mustWrite(t, filepath.Join(root, "app", "i", "page.go"), "needle\n")
	mustGit(t, root, "init", "-q")

	engines := []string{"git"}
	if _, err := exec.LookPath("rg"); err == nil {
		engines = append(engines, "rg")
	}

	include := globQuote("app/[id]") + "/**"
	for _, e := range engines {
		files, _, err := runSearch(context.Background(), root, searchOpts{query: "needle", include: include}, e)

		var got []string
		for _, f := range files {
			got = append(got, f.path)
		}

		if err != nil || strings.Join(got, " ") != "app/[id]/page.go" {
			t.Errorf("%s include %q = %q (%v)", e, include, got, err)
		}
	}
}

// pressFire presses k and delivers what its command produces, once.
func pressFire(m *Model, k string) {
	_, cmd := m.Update(keyMsg(k))
	fire(m, cmd)
}

func TestExplorerCutCopyPaste(t *testing.T) {
	m := testModel(t)
	readme, src := filepath.Join(m.ws, "README.md"), filepath.Join(m.ws, "src")

	pasteItem := func() item {
		for _, it := range m.ex.items(m) {
			if it.label == "Paste" {
				return it
			}
		}

		t.Fatal("no Paste in the menu")

		return item{}
	}
	if pasteItem().run != nil || m.ex.action("ctrl+v") != nil {
		t.Fatal("Paste is greyed out until Cut or Copy")
	}
	// ^c in Explorer copies the entry instead of quitting; ^v pastes into
	// the selected folder, then beside the original under a copy's name.
	m.ex.reveal(m, readme)
	pressFire(m, "ctrl+c")

	if m.ex.clip != readme || m.ex.cut || pasteItem().run == nil {
		t.Fatalf("copy: clip=%q cut=%v", m.ex.clip, m.ex.cut)
	}

	m.ex.reveal(m, src)
	pressFire(m, "ctrl+v")

	if got := mustRead(t, filepath.Join(src, "README.md")); got != "# hi\n" || mustRead(t, readme) != "# hi\n" {
		t.Fatalf("pasted copy holds %q", got)
	}

	if n := m.ex.selected(); n == nil || n.path != filepath.Join(src, "README.md") {
		t.Fatalf("the pasted entry is selected: %+v", n)
	}

	pressFire(m, "ctrl+v") // on src/README.md: into src again, which has one now
	mustRead(t, filepath.Join(src, "README copy.md"))

	if m.ex.clip != readme {
		t.Fatal("a copy can be pasted again")
	}
	// ^x fades the entry; the paste moves it and empties the clipboard.
	m.ex.reveal(m, readme)
	pressFire(m, "ctrl+x")

	if !m.ex.cut || !strings.Contains(m.View().Content, fgParams(pal.ignored)) {
		t.Fatal("a cut entry is drawn faded")
	}

	m.ex.reveal(m, filepath.Join(src, "deep"))
	pressFire(m, "ctrl+v")

	if _, err := os.Lstat(readme); err == nil || mustRead(t, filepath.Join(src, "deep", "README.md")) != "# hi\n" {
		t.Fatalf("cut and paste moves the file: %v", err)
	}

	if m.ex.clip != "" || m.ex.cut {
		t.Fatalf("a moved entry is pasted once: clip=%q", m.ex.clip)
	}
	// A folder does not go into itself, copied or moved.
	for _, k := range []string{"ctrl+c", "ctrl+x"} {
		m.ex.reveal(m, src)
		pressFire(m, k)
		m.ex.reveal(m, filepath.Join(src, "deep"))
		pressFire(m, "ctrl+v")

		if !m.msgErr || !strings.Contains(m.msg, "into itself") {
			t.Fatalf("%s then paste into a subfolder: flash %q", k, m.msg)
		}

		mustRead(t, filepath.Join(src, "deep", "x.go"))
	}
	// A move never replaces what is there.
	mustWrite(t, filepath.Join(m.ws, "x.go"), "other\n")
	m.ex.rebuild(m)
	m.ex.reveal(m, filepath.Join(m.ws, "x.go"))
	pressFire(m, "ctrl+x")
	m.ex.reveal(m, filepath.Join(src, "deep", "x.go"))
	pressFire(m, "ctrl+v")

	if !m.msgErr || mustRead(t, filepath.Join(src, "deep", "x.go")) != "package x\n" {
		t.Fatalf("move onto a taken name: flash %q", m.msg)
	}

	if m.ex.clip != filepath.Join(m.ws, "x.go") || !m.ex.cut {
		t.Fatal("a move that failed keeps the cut for another try")
	}
}
