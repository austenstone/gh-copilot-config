package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tailscale/hujson"
)

// extractToProfile writes the managed top-level keys of a live JSONC file to
// prof, preserving the comments above each key. If nothing matches, prof is
// removed so the asset reads as "absent".
func extractToProfile(live, prof string, re *regexp.Regexp) error {
	src, err := os.ReadFile(live)
	if err != nil {
		return err
	}
	out, err := extractKeys(src, re)
	if err != nil {
		return fmt.Errorf("%s: %w", live, err)
	}
	if out == nil {
		return remove(prof)
	}
	if err := os.MkdirAll(filepath.Dir(prof), 0o755); err != nil {
		return err
	}
	return os.WriteFile(prof, out, 0o644)
}

// extractKeys returns a formatted JWCC object of the managed members of src, or
// nil when none match.
func extractKeys(src []byte, re *regexp.Regexp) ([]byte, error) {
	v, err := hujson.Parse(src)
	if err != nil {
		return nil, err
	}
	obj, ok := v.Value.(*hujson.Object)
	if !ok {
		return nil, nil
	}
	var kept []hujson.ObjectMember
	for _, mem := range obj.Members {
		if re.MatchString(memberName(mem)) {
			kept = append(kept, mem)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}
	out := hujson.Value{Value: &hujson.Object{Members: kept}}
	out.Format()
	return withTrailingNewline(out.Pack()), nil
}

// applyKeysFile rewrites a live JSONC file: it strips every managed key, then
// (when prof != "") merges the profile's managed keys back in with their
// comments. Unmanaged keys are preserved.
func applyKeysFile(live, prof string, re *regexp.Regexp) error {
	var liveText []byte
	if b, err := os.ReadFile(live); err == nil {
		liveText = b
	} else if !os.IsNotExist(err) {
		return err
	}
	var profKeys []byte
	if prof != "" {
		b, err := os.ReadFile(prof)
		if err != nil {
			return err
		}
		profKeys = b
	}
	out, err := mergeKeys(liveText, profKeys, re)
	if err != nil {
		return fmt.Errorf("%s: %w", live, err)
	}
	if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
		return err
	}
	return os.WriteFile(live, out, 0o644)
}

// mergeKeys removes managed members from liveText and prepends the members of
// profKeys (when non-empty), preserving comments throughout.
func mergeKeys(liveText, profKeys []byte, re *regexp.Regexp) ([]byte, error) {
	src := liveText
	if len(strings.TrimSpace(string(src))) == 0 {
		src = []byte("{}")
	}
	v, err := hujson.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse live: %w", err)
	}
	obj, ok := v.Value.(*hujson.Object)
	if !ok {
		return nil, fmt.Errorf("root is not a JSON object")
	}
	kept := obj.Members[:0]
	for _, mem := range obj.Members {
		if !re.MatchString(memberName(mem)) {
			kept = append(kept, mem)
		}
	}
	obj.Members = kept

	if len(profKeys) > 0 {
		pv, err := hujson.Parse(profKeys)
		if err != nil {
			return nil, fmt.Errorf("parse profile keys: %w", err)
		}
		if pobj, ok := pv.Value.(*hujson.Object); ok && len(pobj.Members) > 0 {
			obj.Members = append(append([]hujson.ObjectMember{}, pobj.Members...), obj.Members...)
		}
	}
	v.Format()
	return withTrailingNewline(v.Pack()), nil
}

func memberName(m hujson.ObjectMember) string {
	if lit, ok := m.Name.Value.(hujson.Literal); ok {
		return lit.String()
	}
	return ""
}

func withTrailingNewline(b []byte) []byte {
	if len(b) == 0 || b[len(b)-1] != '\n' {
		return append(b, '\n')
	}
	return b
}
