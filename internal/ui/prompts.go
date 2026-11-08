package ui

import (
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/xseman/pando/internal/proto"
)

// prompt is a saved prompt: a Markdown file in promptsDir, named by the file.
type prompt struct {
	name, path, text string
}

// promptsMsg carries the saved prompts read for the picker.
type promptsMsg struct {
	prompts []prompt
	err     error
}

// promptsDir holds the saved prompts, one file each, shared by every instance.
func promptsDir() string { return filepath.Join(proto.ConfigDir(), "prompts") }

// readPrompts is alt+i: the saved prompts by name, read fresh for the
// picker on every open; no folder yet is none.
func readPrompts() tea.Msg {
	dir := promptsDir()

	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return promptsMsg{}
	}

	if err != nil {
		return promptsMsg{err: err}
	}

	var out []prompt

	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok {
			continue
		}

		path := filepath.Join(dir, e.Name())

		// A folder or a dangling link, such as Emacs's .#name.md lock, is no
		// prompt; a file that fails to read still says so.
		if st, err := os.Stat(path); err != nil || !st.Mode().IsRegular() {
			continue
		}

		b, err := os.ReadFile(path)
		if err != nil {
			return promptsMsg{err: err}
		}

		// Trailing blank lines would submit the paste in an app that reads
		// a newline as Enter; a CRLF file's \r would send the row's cursor
		// back to column 0 and land in the paste.
		text := strings.TrimRightFunc(strings.ReplaceAll(string(b), "\r\n", "\n"), unicode.IsSpace)
		out = append(out, prompt{name: name, path: path, text: text})
	}

	// A–Z as a person reads it, not ReadDir's bytes: "Banana" after
	// "apple", "Ábc" next to "abc".
	slices.SortStableFunc(out, func(a, b prompt) int { return cmp.Compare(unaccented(a.name), unaccented(b.name)) })

	return promptsMsg{prompts: out}
}

// showPrompts opens the picker over the prompts read: the one inserted last
// on top, the rest by name, matched by words in the name and the text.
func (m *Model) showPrompts(ps []prompt) {
	if i := slices.IndexFunc(ps, func(p prompt) bool { return p.name == m.lastPrompt }); i > 0 {
		last := ps[i]
		ps = slices.Insert(slices.Delete(ps, i, i+1), 0, last)
	}

	byName := map[string]prompt{}
	for _, p := range ps {
		byName[p.name] = p
	}

	md := newPicker("Prompts", nil)
	md.note = "⏎ insert · alt+⏎ send · ^e edit · ⇧del delete"

	selected := func() (prompt, bool) {
		if md.l.sel < 0 || md.l.sel >= len(md.disp) {
			return prompt{}, false
		}

		p, ok := byName[md.disp[md.l.sel].label]

		return p, ok
	}

	fill := func(q string) {
		md.items = nil // a new slice, so the box measures its rows again (wOf)

		// A query's name matches come first: typing a prompt's name and ⏎
		// inserts it, not a longer one that only mentions the word.
		matched := slices.DeleteFunc(slices.Clone(ps), func(p prompt) bool { return !hits(q, p.name+" "+p.text) })
		slices.SortStableFunc(matched, func(a, b prompt) int {
			return cmp.Compare(b2i(!hits(q, a.name)), b2i(!hits(q, b.name)))
		})

		for _, p := range matched {
			first, _, _ := strings.Cut(p.text, "\n")
			first = ansi.Strip(strings.ReplaceAll(first, "\t", " ")) // a tab or an escape is no width a row can count
			first = ansi.Truncate(first, max(m.w/2, 20), "…")        // row drops a hint wider than the box, not cuts it
			md.items = append(md.items, item{
				label: p.name, hint: first, always: true,
				segs:  markRuns(p.name, wordRuns(q, p.name), plain),
				right: markRuns(first, wordRuns(q, first), dim),
				run:   func(m *Model) tea.Cmd { return m.insertPrompt(p) },
			})
		}

		if len(ps) == 0 {
			md.items = append(md.items, item{label: "no prompts yet · " + promptsDir(), sep: true, always: true})
		}

		md.items = append(md.items, item{label: "+ New Prompt…", always: true, run: func(m *Model) tea.Cmd { return m.addPrompt() }})
	}

	md.change = func(_ *Model, v string) tea.Cmd {
		fill(v)

		return nil
	}

	md.alt = func(m *Model) tea.Cmd {
		p, ok := selected()
		if !ok {
			return nil
		}

		m.modal = nil

		return m.sendPrompt(p)
	}

	md.keys = map[string]func(m *Model) tea.Cmd{
		"ctrl+e": func(m *Model) tea.Cmd {
			p, ok := selected()
			if !ok {
				return nil
			}

			m.modal, m.focus = nil, onMain // typing goes to the file, not to a view's keys

			return m.openFile(p.path, false)
		},
		"shift+delete": func(m *Model) tea.Cmd {
			p, ok := selected()
			if !ok {
				return nil
			}

			m.modal = deletePrompt(p)

			return nil
		},
	}

	fill("")
	md.refilter()
	m.modal = md
}

// promptTarget is the session a prompt goes to: the terminal with the
// keyboard, else the session shown in the active editor group.
func (m *Model) promptTarget() string {
	if m.termFocused() && m.tv.id != "" { // the panel's shell on screen, not Problems or Output over it
		return m.tv.id
	}

	if m.showsSession() {
		return m.sess
	}

	return ""
}

// insertPrompt types p into the target session as one paste, waking it
// first, and leaves the Enter to the user.
func (m *Model) insertPrompt(p prompt) tea.Cmd {
	id := m.promptTarget()
	if id == "" {
		return flash("no session open to insert "+p.name+" into", true)
	}

	m.lastPrompt = p.name

	if id == m.tv.id {
		m.tv.scroll, m.tv.sized = 0, termSize{}
	} else {
		m.term.scroll, m.term.sized = 0, termSize{}
		m.focus = onMain
	}

	m.inputs <- proto.InputParams{ID: id, Paste: p.text, Wake: true}

	return nil
}

// sendPrompt inserts p and submits it.
func (m *Model) sendPrompt(p prompt) tea.Cmd {
	id := m.promptTarget()

	cmd := m.insertPrompt(p)
	if id == "" {
		return cmd
	}

	inputs := m.inputs

	// A separate, slightly later Enter, as pando session send's: line
	// editors read "\r" inside a fast burst as a pasted newline.
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		inputs <- proto.InputParams{ID: id, Text: "\r"}

		return nil
	})
}

// addPrompt asks for a name and opens that prompt's file in the editor,
// created empty when it does not exist yet.
func (m *Model) addPrompt() tea.Cmd {
	m.modal = newPrompt("New prompt · its name", "", func(m *Model, v string) tea.Cmd {
		name := strings.TrimSuffix(v, ".md")
		if name == "" {
			return nil
		}

		if filepath.Base(name) != name || name == "." || name == ".." {
			return flash("a prompt's name is a file name, without "+string(filepath.Separator), true)
		}

		path := filepath.Join(promptsDir(), name+".md")

		err := os.MkdirAll(promptsDir(), 0o755)
		if err == nil {
			var f *os.File
			if f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644); err == nil {
				_ = f.Close() // nothing was written to it
			}
		}

		if err != nil && !errors.Is(err, fs.ErrExist) { // an existing one opens as it is
			return flash(err.Error(), true)
		}

		m.focus = onMain // typing goes to the file, not to a view's keys

		return m.openFile(path, false)
	})

	return nil
}

// deletePrompt asks before p's file goes, as Explorer's Delete does.
func deletePrompt(p prompt) *modal {
	return newDialog("Delete "+filepath.Base(p.path)+"?",
		item{label: "Delete permanently", run: func(m *Model) tea.Cmd {
			if err := os.Remove(p.path); err != nil {
				return flash(err.Error(), true)
			}

			if m.lastPrompt == p.name {
				m.lastPrompt = ""
			}

			return flash("deleted "+p.name, false)
		}},
		cancelItem())
}
