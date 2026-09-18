package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/xseman/pando/internal/proto"
)

// Untitled buffers and drafts, VS Code's New Untitled File and hot exit.
//
// An untitled editor is an ordinary file preview with no path: it types,
// undoes and saves like any other, and ⌃s asks where to put it. Whatever is
// unsaved — an untitled buffer, or edits to a file that exists — is kept by
// the daemon as a draft, so closing the TUI, or losing it, does not lose the
// text. A draft goes as soon as its editor is clean again.

type draftsMsg []proto.Draft

// untitledName is the lowest Untitled-N no editor is using.
func (m *Model) untitledName() string {
	for n := 1; ; n++ {
		name := fmt.Sprintf("Untitled-%d", n)
		if !slices.ContainsFunc(m.editors, func(e preview) bool { return e.name == name }) {
			return name
		}
	}
}

// newUntitled opens an empty editor to type into.
func (m *Model) newUntitled() tea.Cmd {
	m.focus = onMain
	return m.setPreview(preview{kind: pvFile, name: m.untitledName()})
}

// draftOf is the wire draft for an editor, with an empty text — the value
// that forgets it — when there is nothing unsaved.
func (p *preview) draftOf(ws string) proto.Draft {
	d := proto.Draft{WS: ws, Path: p.path, Name: p.name}
	switch {
	case p.dirty():
		d.Text, d.Mod = p.unsavedOr(p.buf.text()), p.buf.modAt
		if d.Text == "" { // an emptied buffer still has to come back empty
			d.Text = "\n"
		}

	case p.draft != "": // restored but not opened yet: the draft is still it
		d.Text, d.Mod = p.draft, p.draftM
	}

	return d
}

// forgetDraft drops one editor's draft, for a tab closed without saving.
func (m *Model) forgetDraft(p preview) tea.Cmd {
	d := p.draftOf(m.ws)
	if d.Text == "" && m.savedDrafts[d.Key()] == "" {
		return nil
	}

	d.Text = ""
	delete(m.savedDrafts, d.Key())

	return do("draft.set", d)
}

// saveDrafts tells the daemon what is unsaved, and what stopped being. Like
// saveEditors it diffs against what this TUI sent, not against the daemon's
// copy, so two TUIs on one workspace do not rewrite each other every tick.
func (m *Model) saveDrafts() tea.Cmd {
	if m.ws == "" {
		return nil
	}

	if m.savedDrafts == nil {
		m.savedDrafts = map[string]string{}
	}

	m.saveSpot()

	var cmds []tea.Cmd

	live := map[string]bool{}

	for _, e := range m.editors {
		if e.kind != pvFile {
			continue
		}

		d := e.draftOf(m.ws)

		live[d.Key()] = true
		if m.savedDrafts[d.Key()] == d.Text {
			continue
		}

		if d.Text == "" {
			delete(m.savedDrafts, d.Key())
		} else {
			m.savedDrafts[d.Key()] = d.Text
		}

		cmds = append(cmds, do("draft.set", d))
	}
	// A tab that closed takes its draft with it.
	for key := range m.savedDrafts {
		if live[key] {
			continue
		}

		d := proto.Draft{WS: m.ws, Path: key}
		if name, ok := strings.CutPrefix(key, "untitled:"); ok {
			d.Path, d.Name = "", name
		}

		delete(m.savedDrafts, key)

		cmds = append(cmds, do("draft.set", d))
	}

	return tea.Batch(cmds...)
}

// loadDrafts asks for the workspace's unsaved text.
func (m *Model) loadDrafts() tea.Cmd {
	ws := m.ws
	if ws == "" {
		return nil
	}

	return func() tea.Msg {
		var ds []proto.Draft
		if err := proto.Call("draft.list", map[string]string{"ws": ws}, &ds); err != nil {
			return flashMsg{"draft.list: " + err.Error(), true}
		}

		return draftsMsg(ds)
	}
}

// onDrafts puts the text back: into the editors the strip restored, and into
// a tab of its own for an untitled buffer the strip did not remember.
func (m *Model) onDrafts(ds []proto.Draft) tea.Cmd {
	m.saveSpot() // anything typed while the drafts were on their way is newer
	m.savedDrafts = map[string]string{}

	var cmds []tea.Cmd

	for _, d := range ds {
		if d.Text == "" {
			continue
		}

		m.savedDrafts[d.Key()] = d.Text

		i := slices.IndexFunc(m.editors, func(e preview) bool {
			return e.kind == pvFile && e.path == d.Path && e.name == d.Name
		})
		if i < 0 {
			if d.Path != "" { // the file is gone, or was never in this strip
				if st, err := os.Stat(d.Path); err != nil || st.IsDir() {
					continue
				}
			}

			m.editors = append(m.editors, preview{kind: pvFile, path: d.Path, name: d.Name})
			i = len(m.editors) - 1
		}

		if m.editors[i].dirty() { // typed into already: that text is newer
			continue
		}

		e := &m.editors[i]
		e.draft, e.draftM = d.Text, d.Mod
		e.buf = newBuffer(d.Text, d.Mod)

		e.buf.dirty = true
		if m.edIdx == i && m.pv.id() == e.id() {
			m.pv.draft, m.pv.draftM, m.pv.buf = d.Text, d.Mod, e.buf
			cmds = append(cmds, m.pv.load(m))
		}
	}

	if m.edIdx < 0 && len(m.editors) > 0 {
		m.edIdx = len(m.editors) - 1
		if m.sess == "" {
			cmds = append(cmds, m.restore(m.editors[m.edIdx]))
		}
	}

	return tea.Batch(cmds...)
}

// saveAsPrompt asks where an editor should go, completing the path as fish
// does. then runs once the file is written, so a close can wait for it.
func (m *Model) saveAsPrompt(i int, then func(m *Model) tea.Cmd) tea.Cmd {
	p := &m.pv
	if i >= 0 && i < len(m.editors) && m.editors[i].id() != m.pv.id() {
		p = &m.editors[i]
	}

	if p.buf == nil {
		return flash("nothing to save", true)
	}

	start := m.ws + string(filepath.Separator)
	if p.path != "" {
		start = p.path
	}

	defer func() {
		md := m.modal
		md.complete, md.input.ShowSuggestions = completePath, true
		md.input.SetSuggestions(completePath(md.input.Value()))
	}()

	m.modal = newPrompt("Save as", start, func(m *Model, v string) tea.Cmd {
		if v == "" {
			return nil
		}

		to := expandHome(v)
		if !filepath.IsAbs(to) {
			to = filepath.Join(m.ws, to)
		}

		if st, err := os.Lstat(to); err == nil {
			if st.IsDir() {
				return flash(v+" is a directory", true)
			}

			m.modal = newMenu(filepath.Base(to)+" already exists", -1, 0,
				item{label: "Overwrite it", run: func(m *Model) tea.Cmd { return m.saveAs(i, to, then) }},
				cancelItem())

			return nil
		}

		return m.saveAs(i, to, then)
	})

	return nil
}

// saveAs writes editor i to path and re-points its tab there: the name goes,
// the draft goes, and the file is read back so chroma lexes it as what it now
// is rather than as plain text.
func (m *Model) saveAs(i int, path string, then func(m *Model) tea.Cmd) tea.Cmd {
	p := &m.pv

	onScreen := true
	if i >= 0 && i < len(m.editors) && m.editors[i].id() != m.pv.id() {
		p, onScreen = &m.editors[i], false
	}

	was := *p
	// The path decides the formatter and the lexer, so it goes on first;
	// format_on_save then runs the way an ordinary ⌃s runs it.
	p.path, p.name, p.draft = path, "", ""
	note := ""

	if onScreen && m.st.Settings.FmtSave && len(m.formatCommand(path)) > 0 {
		if err := p.format(m); err != nil { // saved unformatted, as VS Code saves it
			note = " — " + err.Error()
		}
	}

	if err := p.buf.save(path, true); err != nil {
		p.path, p.name = was.path, was.name // nothing was written: stay untitled
		return flash(err.Error(), true)
	}

	if i >= 0 && i < len(m.editors) {
		m.editors[i] = p.snapshot()
	}

	cmds := []tea.Cmd{
		m.forgetDraft(was), m.refreshGit(), m.saveEditors(),
		flash("saved "+filepath.Base(path)+note, note != ""),
	}
	if onScreen {
		m.ex.reveal(m, path)
		cmds = append(cmds, p.load(m))
	}

	if then != nil {
		cmds = append(cmds, then(m))
	}

	return tea.Batch(cmds...)
}

// completePath is completeDir with the files as well, so an existing file can
// be picked to overwrite.
func completePath(v string) []string { return completeEntries(v, false) }
