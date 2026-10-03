package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"regexp"
)

// claudeBackground is `pando claude ARGS`: claude as a background session,
// which Claude Code's own daemon runs, with this terminal attached to it. A
// pando restart then stops only the attach, and the conversation runs on with
// its subagents; pando attaches to it again ([resume_job]). A claude too old
// for --bg runs as it is.
func claudeBackground(args []string) error {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return err
	}

	out, err := exec.Command(bin, append([]string{"--bg"}, args...)...).CombinedOutput()

	id := backgroundID(out)
	if id == "" {
		if bytes.Contains(out, []byte("unknown option")) {
			return execv(bin, append([]string{"claude"}, args...))
		}

		_, _ = os.Stderr.Write(out) // what claude said instead of an id

		if err == nil {
			err = errors.New("claude --bg printed no session id")
		}

		return err
	}

	return execv(bin, []string{"claude", "attach", id})
}

var (
	ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[\x20-\x2f]*[\x40-\x7e]`) // a CSI sequence: parameters, intermediates, final byte
	// bgStarted is the line `claude --bg` prints first; bgAttach its hint,
	// the fallback should the first line change.
	bgStarted = regexp.MustCompile(`(?m)^backgrounded\s*·\s*([0-9a-f]+)\b`)
	bgAttach  = regexp.MustCompile(`(?m)^\s*claude attach ([0-9a-f]+)\s+open in this terminal`)
)

// backgroundID is the short id `claude --bg` printed for the session it
// started, "" when it printed none. A note about the copy it started of a
// session already running names the original too; the id is the new one's.
func backgroundID(out []byte) string {
	plain := ansiSeq.ReplaceAll(out, nil)

	for _, re := range []*regexp.Regexp{bgStarted, bgAttach} {
		if m := re.FindSubmatch(plain); m != nil {
			return string(m[1])
		}
	}

	return ""
}
