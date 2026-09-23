package ui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// clipTool is a desktop's clipboard as a pair of commands; env names the
// variable a session of that desktop sets, "" for one that is always there.
type clipTool struct {
	env         string
	copy, paste []string
}

// clipTools are tried in order, copying and pasting alike, so what pando
// copies is what it pastes back and what every other app on the desktop sees.
var clipTools = []clipTool{
	{"WAYLAND_DISPLAY", []string{"wl-copy"}, []string{"wl-paste", "--no-newline"}},
	{"DISPLAY", []string{"xclip", "-selection", "clipboard"}, []string{"xclip", "-o", "-selection", "clipboard"}},
	{"DISPLAY", []string{"xsel", "-ib"}, []string{"xsel", "-ob"}},
	{"", []string{"pbcopy"}, []string{"pbpaste"}},
}

// errNoClipTool is a session with none of clipTools: over ssh, or a desktop
// without wl-clipboard, xclip or xsel.
var errNoClipTool = errors.New("no clipboard tool (wl-copy, xclip, xsel, pbcopy)")

// usableClipTools are the tools this session can run.
func usableClipTools() []clipTool {
	var out []clipTool

	for _, t := range clipTools {
		if t.env != "" && os.Getenv(t.env) == "" {
			continue
		}

		if _, err := exec.LookPath(t.copy[0]); err == nil {
			out = append(out, t)
		}
	}

	return out
}

// writeClipboard puts text on the system clipboard with the first tool that
// takes it. wl-copy and xclip stay behind to serve it; they get no pipe of
// ours to hold, so the call returns once they have read it.
func writeClipboard(text string) error {
	tools := usableClipTools()
	if len(tools) == 0 {
		return errNoClipTool
	}

	var err error

	for _, t := range tools {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, t.copy[0], t.copy[1:]...)
		cmd.Stdin = strings.NewReader(text)

		err = cmd.Run()

		cancel()

		if err == nil {
			return nil
		}
	}

	return err
}

// clipboardText reads the system clipboard with the first tool that answers.
// ponytail: an OSC 52 read is left out, most terminals refuse it.
func clipboardText() (string, error) {
	tools := usableClipTools()
	if len(tools) == 0 {
		return "", errNoClipTool
	}

	var err error

	for _, t := range tools {
		var out []byte
		if out, err = exec.Command(t.paste[0], t.paste[1:]...).Output(); err == nil {
			return string(out), nil
		}
	}

	return "", err
}

// setClipboard copies text for every app: to the system clipboard, and as
// OSC 52 too, for a terminal that honours it where no tool reaches the
// desktop (over ssh). done is the flash once it is copied, "" for none.
// VTE terminals ignore OSC 52, so the tool is what makes a copy paste there.
func setClipboard(text, done string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		err := writeClipboard(text)

		switch {
		case errors.Is(err, errNoClipTool):
			if done == "" {
				return nil
			}

			return flashMsg{done + " (OSC 52 only: " + err.Error() + ")", false}

		case err != nil:
			return flashMsg{"clipboard: " + err.Error(), true}
		case done == "":
			return nil
		}

		return flashMsg{done, false}
	})
}

// pasteClipboard pastes the system clipboard where a terminal's own paste
// would land: ctrl+v in an editor, VS Code's paste, reads it itself.
func pasteClipboard() tea.Msg {
	text, err := clipboardText()
	if err != nil {
		return flashMsg{"paste: " + err.Error(), true}
	}

	return tea.PasteMsg{Content: text}
}
