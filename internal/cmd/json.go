package cmd

import (
	"encoding/json"
	"os"
	"time"

	"github.com/austenstone/gh-copilot-config/internal/profile"
)

// emitJSON writes v to stdout as indented JSON. Used by every --json branch so
// machine consumers (the Copilot app canvas extension) get a stable shape.
func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// profileJSON is one profile in a list payload.
type profileJSON struct {
	Name      string `json:"name"`
	Created   string `json:"created"`
	Modified  string `json:"modified"`
	Size      int64  `json:"size"`
	SizeHuman string `json:"sizeHuman"`
	Active    bool   `json:"active"`
	Last      bool   `json:"last"`
}

// listJSON is the full listing picture, so the UI needs a single call.
type listJSON struct {
	Dir      string        `json:"dir"`
	Active   string        `json:"active"`
	Last     string        `json:"last"`
	Profiles []profileJSON `json:"profiles"`
}

func newListJSON(m *profile.Manager, ps []profile.Profile) listJSON {
	out := listJSON{Dir: m.Dir, Active: m.Active(), Last: m.Last(), Profiles: []profileJSON{}}
	for _, p := range ps {
		out.Profiles = append(out.Profiles, profileJSON{
			Name:      p.Name,
			Created:   rfc3339(p.Created),
			Modified:  rfc3339(p.Modified),
			Size:      p.Size,
			SizeHuman: profile.HumanSize(p.Size),
			Active:    p.Active,
			Last:      p.Last,
		})
	}
	return out
}

// statusJSON mirrors the human status command. Drift is nil when there is no
// active profile to compare against.
type statusJSON struct {
	Active string `json:"active"`
	Last   string `json:"last"`
	Dir    string `json:"dir"`
	Exists bool   `json:"exists"`
	Drift  *bool  `json:"drift"`
	InSync *bool  `json:"inSync"`
}

// diffJSON is the result of comparing live config to a profile.
type diffJSON struct {
	Name  string `json:"name"`
	Drift bool   `json:"drift"`
	Patch string `json:"patch"`
}

// inspectJSON is a profile's inventory grouped surface→feature for browsing.
type inspectJSON struct {
	Name     string        `json:"name"`
	Surfaces []surfaceJSON `json:"surfaces"`
}

type surfaceJSON struct {
	Token    string        `json:"token"`
	Label    string        `json:"label"`
	Total    int           `json:"total"`
	Features []featureJSON `json:"features"`
}

type featureJSON struct {
	Feature string     `json:"feature"`
	Items   []itemJSON `json:"items"`
}

type itemJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func newInspectJSON(name string, inv profile.Inventory) inspectJSON {
	out := inspectJSON{Name: name, Surfaces: []surfaceJSON{}}
	for _, s := range inv.Surfaces() {
		sj := surfaceJSON{Token: s.Token(), Label: string(s), Total: inv.SurfaceTotal(s), Features: []featureJSON{}}
		for _, cat := range profile.Categories {
			items := inv.Items[s][cat]
			if len(items) == 0 {
				continue
			}
			fj := featureJSON{Feature: cat, Items: []itemJSON{}}
			for _, it := range items {
				fj.Items = append(fj.Items, itemJSON{Name: it.Name, Path: it.Path})
			}
			sj.Features = append(sj.Features, fj)
		}
		out.Surfaces = append(out.Surfaces, sj)
	}
	return out
}
