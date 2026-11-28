package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"millennium/emu"
)

// Source is a firmware file or directory attached at runtime.
type Source struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"` // resolved absolute path
	Kind   string `json:"kind"` // "file" or "directory"
	Images int    `json:"images"`
}

type firmwareEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func realpathOr(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return abs
	}
	return real
}

// Attach registers an absolute file or directory path as a firmware source.
// Attaching the same resolved path twice is idempotent. Call with Mu held.
func (a *App) Attach(pathV any, nameV any) (*Source, []firmwareEntry, error) {
	s, ok := pathV.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil, nil, emu.Err("path is required")
	}
	resolved := realpathOr(s)
	st, err := os.Stat(resolved)
	if err != nil {
		return nil, nil, emu.Err("Attached path does not exist: " + s)
	}
	for _, src := range a.sources {
		if src.Path == resolved {
			images := a.sourceImages(src)
			return src, images, nil
		}
	}
	src := &Source{ID: a.nextSourceID, Name: filepath.Base(resolved), Path: resolved}
	a.nextSourceID++
	if st.IsDir() {
		src.Kind = "directory"
	} else {
		src.Kind = "file"
	}
	if n, isStr := nameV.(string); isStr && strings.TrimSpace(n) != "" {
		src.Name = n
	}
	images := a.sourceImages(src)
	src.Images = len(images)
	a.sources = append(a.sources, src)
	return src, images, nil
}

// Detach removes an attached source by id or resolved path. Call with Mu held.
func (a *App) Detach(idV any, pathV any) ([]*Source, error) {
	var id int
	haveID := false
	switch v := idV.(type) {
	case float64:
		id, haveID = int(v), true
	case string:
		if n, err := emu.Number(v, "id", 1, 1000000); err == nil {
			id, haveID = int(n), true
		}
	}
	var resolved string
	if p, ok := pathV.(string); ok && strings.TrimSpace(p) != "" {
		resolved = realpathOr(p)
	}
	if !haveID && resolved == "" {
		return nil, emu.Err("id or path is required")
	}
	for i, src := range a.sources {
		if (haveID && src.ID == id) || (resolved != "" && src.Path == resolved) {
			a.sources = append(a.sources[:i], a.sources[i+1:]...)
			return a.sources, nil
		}
	}
	return nil, emu.Err("Source is not attached")
}

// Sources lists attached sources with refreshed image counts. Call with Mu held.
func (a *App) Sources() []*Source {
	out := make([]*Source, 0, len(a.sources))
	for _, src := range a.sources {
		cp := *src
		cp.Images = len(a.sourceImages(src))
		out = append(out, &cp)
	}
	return out
}

func (a *App) sourceImages(src *Source) []firmwareEntry {
	if src.Kind == "file" {
		st, err := os.Stat(src.Path)
		if err != nil {
			return nil
		}
		return []firmwareEntry{{Path: src.Path, Size: st.Size()}}
	}
	found, err := discover(src.Path)
	if err != nil {
		return nil
	}
	return found
}

// Firmwares lists every image across attached sources, deduplicated and
// sorted by path. Call with Mu held.
func (a *App) Firmwares() []firmwareEntry {
	seen := map[string]bool{}
	out := []firmwareEntry{}
	for _, src := range a.sources {
		for _, f := range a.sourceImages(src) {
			key := strings.ToLower(f.Path)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// discover finds .bin files under dir, returning absolute paths. Symlinks,
// Symlinks, node_modules and dot-entries are skipped.
func discover(dir string) ([]firmwareEntry, error) {
	found := []firmwareEntry{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type()&os.ModeSymlink != 0 || name == "node_modules" || strings.HasPrefix(name, ".") {
			continue
		}
		file := filepath.Join(dir, name)
		if entry.IsDir() {
			sub, err := discover(file)
			if err != nil {
				return nil, err
			}
			found = append(found, sub...)
			continue
		}
		if strings.HasSuffix(strings.ToLower(name), ".bin") {
			st, err := os.Stat(file)
			if err != nil {
				return nil, err
			}
			found = append(found, firmwareEntry{Path: file, Size: st.Size()})
		}
	}
	return found, nil
}

// resolveImage validates a load/upload path: absolute, or relative to the
// server working directory, resolving symlinks; it must be an existing file.
func resolveImage(pathV any) (string, error) {
	s, ok := pathV.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", emu.Err("path is required")
	}
	abs, err := filepath.Abs(s)
	if err != nil {
		return "", emu.Err("path is required")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", emu.Err("Expected a firmware image file, not a directory")
	}
	return resolved, nil
}

// UploadDir returns the scratch directory for browser-uploaded images,
// attaching it as a source on first use. Call with Mu held.
func (a *App) UploadDir() (string, error) {
	dir := filepath.Join(os.TempDir(), "millennium-uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	resolved := realpathOr(dir)
	for _, src := range a.sources {
		if src.Path == resolved {
			return resolved, nil
		}
	}
	a.sources = append(a.sources, &Source{ID: a.nextSourceID, Name: "uploads",
		Path: resolved, Kind: "directory"})
	a.nextSourceID++
	return resolved, nil
}

type buildManifest struct {
	Sha256 string `json:"sha256"`
	Board  string `json:"board"`
}

// LoadResponse is the /api/load reply: the standard envelope plus how the
// board profile was chosen and which symbol map was picked up.
type LoadResponse struct {
	ResultData
	ProfileSource string `json:"profileSource"`
	Symbols       struct {
		Count       int     `json:"count"`
		Map         *string `json:"map"`
		Association string  `json:"association"`
	} `json:"symbols"`
	// Warning is set for an image that loads but cannot boot, e.g. an MTR2
	// flash with an empty reset sector (a bare U2 half without the X1 bootstrap).
	Warning string `json:"warning,omitempty"`
}

// Load installs a firmware image given by absolute path. Sidecar files next
// to the image are picked up automatically: a .build.json whose SHA-256
// matches may select the board profile under 'auto', and a same-stem .MAP
// supplies symbols (a matching basename is a loading convenience, not proof
// the map belongs to the image). Call with Mu held.
func (a *App) Load(name any, mapPath any, board any) (*LoadResponse, error) {
	boardStr := "auto"
	if s, ok := board.(string); ok && s != "" {
		boardStr = s
	}
	file, err := resolveImage(name)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	symbols := []emu.Symbol{}
	var mapName *string
	profileSource := "explicit selection"
	isBin := strings.HasSuffix(strings.ToLower(file), ".bin")
	base := strings.TrimSuffix(file, filepath.Ext(file))
	if boardStr == "auto" {
		profileSource = "image size"
		if isBin {
			if mRaw, rErr := os.ReadFile(base + ".build.json"); rErr == nil {
				var manifest buildManifest
				if jErr := json.Unmarshal(mRaw, &manifest); jErr != nil {
					return nil, jErr
				}
				if manifest.Sha256 == emu.Hash(data) {
					if _, ok := emu.Boards[manifest.Board]; !ok {
						return nil, emu.Err("Unsupported board in verified build manifest")
					}
					boardStr = manifest.Board
					profileSource = "verified build manifest"
				}
			}
		}
	}
	candidate := ""
	if s, ok := mapPath.(string); ok && s != "" {
		resolvedMap, mErr := resolveImage(s)
		if mErr != nil {
			return nil, mErr
		}
		candidate = resolvedMap
	} else if isBin {
		candidate = base + ".MAP"
	}
	if candidate != "" {
		if raw, rErr := os.ReadFile(candidate); rErr == nil {
			symbols = emu.ParseMap(string(raw))
			m := candidate
			mapName = &m
		} else if mapPath != nil {
			return nil, rErr
		}
	}
	a.Ncc.EndSession("firmware loaded")
	if err := a.Machine.Load(data, file, symbols, boardStr); err != nil {
		return nil, err
	}
	a.realtime.ClockHz = a.Machine.Board.ClockHz
	resp := &LoadResponse{ResultData: a.Result()}
	resp.ProfileSource = profileSource
	if a.Machine.BootSectorEmpty() {
		resp.Warning = "MTR2 image has an empty boot sector at flash 0xC0000: the CPU " +
			"fetches 0xFF from reset and runs away into RST 0x38. Load the full 1 MB flash " +
			"(both chips concatenated, e.g. U2+X1), not a single 512 KiB half."
	}
	resp.Symbols.Count = len(symbols)
	resp.Symbols.Map = mapName
	resp.Symbols.Association = "Companion map; verify it matches this ROM build"
	return resp, nil
}
