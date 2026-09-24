package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// keyCtx is where the keyboard is, VS Code's focus context keys: a chord
// means what the focused part makes of it, and one that part types or uses
// never reaches pando's own bindings.
type keyCtx uint8

const (
	ctxList     keyCtx = 1 << iota // a sidebar view's list (sideBarFocus)
	ctxEditor                      // a diff, a rendering or a read-only file (editorFocus)
	ctxText                        // an editable file: its keys are text (editorTextFocus)
	ctxInput                       // a text box: commit message, filter, search, find (inputFocus)
	ctxTerminal                    // a session or the Terminal panel (terminalFocus)

	// ctxPanels are pando's own panels, where a key that is not text is a command.
	ctxPanels = ctxList | ctxEditor | ctxText
	// ctxNotTerm is everywhere but a terminal, which takes every key it can type.
	ctxNotTerm = ctxPanels | ctxInput
	ctxAny     = ctxNotTerm | ctxTerminal
)

// keyContext is the context the focused part gives a key.
func (m *Model) keyContext() keyCtx {
	if t, _ := m.focusedTerm(); t != nil && t.find.editing {
		return ctxInput // the terminal's find widget has the keyboard
	}

	switch {
	case m.focus == onPanel || m.termFocused() || m.sessFocused(), m.focus == onMain && m.showsSession():
		return ctxTerminal
	case m.typing():
		return ctxInput
	case m.focus != onMain:
		return ctxList
	case m.pk == nil && m.pv.editable():
		return ctxText
	}

	return ctxEditor
}

// keybinding is a chord bound to a command where when says: VS Code's
// keybinding with its when clause. A terminal only gives up what the table
// binds with ctxTerminal, VS Code's terminal.integrated.commandsToSkipShell.
type keybinding struct {
	keys []string
	when keyCtx
	cond func(m *Model) bool // nil: always, where when allows
	run  func(m *Model, key string) tea.Cmd
}

// keybindings are pando's own chords, first match wins. Anything they leave
// goes to the focused part: the shell, the editor, the input or the list.
var keybindings = []keybinding{
	// Ways out of a terminal and the chords VS Code keeps from its shell.
	{keys: []string{"ctrl+]"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { m.cycleFocus(); return m.fetchScreen() }},
	{keys: []string{"ctrl+shift+p"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return m.commandPalette() }},
	// VS Code's terminal find: ⌃f opens it over the shell, F3 walks its
	// matches, esc closes it while it shows.
	{keys: []string{"ctrl+f"}, when: ctxTerminal, run: func(m *Model, _ string) tea.Cmd {
		t, id := m.focusedTerm()
		return t.openFind(m, id)
	}},
	{keys: []string{"f3", "shift+f3", "esc"}, when: ctxTerminal, cond: termFindOn, run: func(m *Model, s string) tea.Cmd {
		t, id := m.focusedTerm()

		switch s {
		case "esc":
			t.closeFind()
			return nil

		case "f3":
			return t.findGo(m, id, 1)
		}

		return t.findGo(m, id, -1)
	}},
	{keys: []string{"ctrl+shift+f"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return tea.Batch(m.showView(viewSearch), m.sr.focus()) }},
	// ⌃space is VS Code's Trigger Suggest, which an editable file gets first.
	// Outside the kitty protocol a terminal sends NUL for ⌃` too, so the
	// terminal stays on ⌃j there, and on ⌃` where the protocol tells them apart.
	{keys: []string{"ctrl+space", "ctrl+@"}, when: ctxText, run: func(m *Model, _ string) tea.Cmd {
		if m.ambiguous && !m.saidCtrlJ { // it may have been ⌃` meaning the panel
			m.saidCtrlJ = true
			m.flash("this terminal sends one key for ⌃` and ⌃space — the panel is on ⌃j", false)
		}

		return m.pv.suggest(m, "", true)
	}},
	{keys: []string{"ctrl+`", "ctrl+space", "ctrl+@"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return m.toggleTerminal() }},
	// In a terminal ⌃j is the app's line feed: a newline in claude's prompt,
	// accept-line in readline. VS Code skips the shell for it; pando does not.
	{keys: []string{"ctrl+j"}, when: ctxNotTerm, run: func(m *Model, _ string) tea.Cmd { return m.toggleTerminal() }},
	{keys: []string{"ctrl+shift+up", "ctrl+shift+down"}, when: ctxAny, run: func(m *Model, s string) tea.Cmd {
		return m.maximizeTerminal(s == "ctrl+shift+up" && !m.termMax)
	}},
	{keys: []string{"ctrl+shift+`"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return tea.Batch(m.openTerminalPanel(), m.newTerm()) }},
	{keys: []string{"ctrl+b"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return m.toggleSidebars() }},
	{keys: []string{"ctrl+,"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { m.modal = settingsModal(m); return nil }},
	{keys: []string{"ctrl+shift+e"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return m.showView(viewFiles) }},
	{keys: []string{"ctrl+shift+g"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return m.showView(viewGit) }},
	{keys: []string{"ctrl+shift+h"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd {
		m.sr.showReplace = true
		return tea.Batch(m.showView(viewSearch), m.sr.focus())
	}},
	{keys: []string{"ctrl+0"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return m.focusSidebar() }},
	{keys: []string{"ctrl+1"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { m.focus = onMain; return m.fetchScreen() }},
	{keys: []string{"alt+t"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return m.agentNavigator() }},
	{keys: []string{"ctrl+pgdown"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return m.cycleEditor(1) }},
	{keys: []string{"ctrl+pgup"}, when: ctxAny, run: func(m *Model, _ string) tea.Cmd { return m.cycleEditor(-1) }},
	{keys: []string{"alt+1", "alt+2", "alt+3", "alt+4", "alt+5", "alt+6", "alt+7", "alt+8", "alt+9"}, when: ctxAny, run: func(m *Model, s string) tea.Cmd {
		return m.showEditor(int(s[4] - '1'))
	}},

	// Everywhere but a terminal, where these are the shell's or the app's:
	// ⌃⏎ a newline to an agent, F1 help, ⇧⌃v a paste, ⌃s the terminal's own.
	{keys: []string{"f1"}, when: ctxNotTerm, run: func(m *Model, _ string) tea.Cmd { return m.commandPalette() }},
	{keys: []string{"ctrl+shift+v"}, when: ctxNotTerm, run: func(m *Model, _ string) tea.Cmd { return m.toggleRendered() }},
	{keys: []string{"ctrl+enter"}, when: ctxNotTerm, run: func(m *Model, _ string) tea.Cmd { return m.scm.commit(m) }},
	{keys: []string{"ctrl+tab"}, when: ctxNotTerm, run: func(m *Model, _ string) tea.Cmd { return m.cycleEditor(1) }},
	{keys: []string{"ctrl+shift+tab"}, when: ctxNotTerm, run: func(m *Model, _ string) tea.Cmd { return m.cycleEditor(-1) }},
	{keys: []string{"ctrl+s"}, when: ctxNotTerm, run: func(m *Model, _ string) tea.Cmd {
		if !m.showsPreview() {
			return flash("no file open", true)
		}

		return m.pv.save(m, false)
	}},
	{keys: []string{"ctrl+shift+o"}, when: ctxNotTerm, cond: (*Model).showsPreview, run: func(m *Model, _ string) tea.Cmd { return m.gotoSymbol("@") }},

	// pando's panels, where these are not text: a text box keeps them.
	{keys: []string{"5"}, when: ctxList | ctxEditor, run: func(m *Model, _ string) tea.Cmd { return m.toggleTerminal() }},
	{keys: []string{"ctrl+p"}, when: ctxPanels, run: func(m *Model, _ string) tea.Cmd { return m.loadIndex(true) }},
	{keys: []string{"ctrl+g"}, when: ctxPanels, cond: (*Model).showsPreview, run: func(m *Model, _ string) tea.Cmd { return m.gotoLineQuery(":") }},
	{keys: []string{"ctrl+n"}, when: ctxPanels, run: func(m *Model, _ string) tea.Cmd { return m.newUntitled() }},
	// VS Code's paste in a file: the terminal's own paste (ctrl+shift+v) lands
	// here too, as a bracketed paste. A shell keeps ctrl+v, claude its image paste.
	{keys: []string{"ctrl+v"}, when: ctxText, run: func(*Model, string) tea.Cmd { return pasteClipboard }},
	{keys: []string{"ctrl+w"}, when: ctxEditor | ctxText, run: func(m *Model, _ string) tea.Cmd { return m.closeEditor(m.edIdx) }},
	// Go Back / Go Forward: VS Code's Linux ctrl+alt+- and ctrl+shift+-, its
	// Mac ctrl+-, and alt+, alt+. ctrl+← ctrl+→ move by words in an editor and
	// a shell, as they do everywhere else.
	{keys: []string{"ctrl+alt+-", "ctrl+-", "alt+,", "super+left"}, when: ctxPanels, run: func(m *Model, _ string) tea.Cmd { return m.navGo(-1) }},
	{keys: []string{"ctrl+shift+-", "alt+.", "super+right"}, when: ctxPanels, run: func(m *Model, _ string) tea.Cmd { return m.navGo(1) }},
}

// bindingFor is the binding key has in context c, if any.
func (m *Model) bindingFor(c keyCtx, key string) (keybinding, bool) {
	for _, b := range keybindings {
		if b.when&c != 0 && slices.Contains(b.keys, key) && (b.cond == nil || b.cond(m)) {
			return b, true
		}
	}

	return keybinding{}, false
}

// termFindOn reports the find widget showing over the focused terminal.
func termFindOn(m *Model) bool {
	t, _ := m.focusedTerm()
	return t != nil && t.find.on
}
