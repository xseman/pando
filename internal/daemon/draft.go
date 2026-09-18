package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/xseman/pando/internal/proto"
)

// Drafts are the editors' unsaved text, VS Code's hot exit. They live as one
// JSON file each under the data directory rather than in state.json, which
// stays a small file of paths and cursors however much is typed.
//
//	<data>/drafts/<hash of the workspace>/<hash of the draft key>.json
//
// The key is the file's path, or "untitled:Untitled-1"; the file holds the
// whole proto.Draft, so listing a workspace is a ReadDir and needs no index.

// hash16 names a directory or a file after what it holds, whatever characters
// that contains.
func hash16(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func (d *Daemon) draftDir(ws string) string {
	return filepath.Join(d.dataDir, "drafts", hash16(ws))
}

func (d *Daemon) listDrafts(ws string) ([]proto.Draft, error) {
	entries, err := os.ReadDir(d.draftDir(ws))
	if errors.Is(err, os.ErrNotExist) {
		return []proto.Draft{}, nil
	}

	if err != nil {
		return nil, err
	}

	out := []proto.Draft{}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}

		b, err := os.ReadFile(filepath.Join(d.draftDir(ws), e.Name()))
		if err != nil {
			continue
		}

		var dr proto.Draft
		if json.Unmarshal(b, &dr) == nil && dr.Key() != "" {
			out = append(out, dr)
		}
	}

	return out, nil
}

// setDraft writes one draft, or removes it when the text is empty — the same
// "empty value forgets it" rule state.set uses for drafts and editors.
func (d *Daemon) setDraft(dr proto.Draft) error {
	if dr.WS == "" {
		return errors.New("draft: no workspace")
	}

	if dr.Path == "" && dr.Name == "" {
		return errors.New("draft: no path and no name")
	}

	path := filepath.Join(d.draftDir(dr.WS), hash16(dr.Key())+".json")
	if dr.Text == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}

		_ = os.Remove(d.draftDir(dr.WS)) // empties itself away; a used one just fails

		return nil
	}

	b, err := json.Marshal(dr)
	if err != nil {
		return err
	}

	return writeFile(path, b)
}
