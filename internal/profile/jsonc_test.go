package profile

import (
	"strings"
	"testing"
)

func TestExtractKeysPreservesComments(t *testing.T) {
	src := []byte(`{
  // editor stuff the user owns
  "editor.tabSize": 4,
  // copilot agent toggle
  "chat.agent.enabled": true,
  "github.copilot.enable": { "*": true },
  "files.autoSave": "off"
}`)

	out, err := extractKeys(src, copilotKeys)
	if err != nil {
		t.Fatalf("extractKeys: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "chat.agent.enabled") || !strings.Contains(got, "github.copilot.enable") {
		t.Fatalf("managed keys missing:\n%s", got)
	}
	if strings.Contains(got, "editor.tabSize") || strings.Contains(got, "files.autoSave") {
		t.Fatalf("unmanaged keys leaked:\n%s", got)
	}
	if !strings.Contains(got, "// copilot agent toggle") {
		t.Fatalf("leading comment dropped:\n%s", got)
	}
}

func TestExtractKeysNoMatch(t *testing.T) {
	out, err := extractKeys([]byte(`{ "editor.tabSize": 2 }`), copilotKeys)
	if err != nil {
		t.Fatalf("extractKeys: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil for no matches, got %q", out)
	}
}

func TestMergeKeysRoundTrip(t *testing.T) {
	live := []byte(`{
  // user editor config
  "editor.tabSize": 4,
  "chat.agent.enabled": false
}`)
	profileKeys := []byte(`{
  // copilot agent toggle
  "chat.agent.enabled": true
}`)

	out, err := mergeKeys(live, profileKeys, copilotKeys)
	if err != nil {
		t.Fatalf("mergeKeys: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "editor.tabSize") {
		t.Fatalf("unmanaged key removed:\n%s", got)
	}
	if !strings.Contains(got, "// user editor config") {
		t.Fatalf("unmanaged comment dropped:\n%s", got)
	}
	if !strings.Contains(got, "// copilot agent toggle") {
		t.Fatalf("managed comment dropped:\n%s", got)
	}
	if strings.Count(got, "chat.agent.enabled") != 1 {
		t.Fatalf("managed key duplicated:\n%s", got)
	}
	if !strings.Contains(got, "true") {
		t.Fatalf("profile value not applied:\n%s", got)
	}
}

func TestMergeKeysStrip(t *testing.T) {
	live := []byte(`{
  "editor.tabSize": 4,
  "chat.agent.enabled": true,
  "github.copilot.enable": { "*": true }
}`)
	out, err := mergeKeys(live, nil, copilotKeys)
	if err != nil {
		t.Fatalf("mergeKeys: %v", err)
	}
	got := string(out)
	if strings.Contains(got, "chat.agent.enabled") || strings.Contains(got, "github.copilot") {
		t.Fatalf("managed keys not stripped:\n%s", got)
	}
	if !strings.Contains(got, "editor.tabSize") {
		t.Fatalf("unmanaged key removed:\n%s", got)
	}
}

func TestMergeKeysEmptyLive(t *testing.T) {
	out, err := mergeKeys(nil, []byte(`{ "chat.agent.enabled": true }`), copilotKeys)
	if err != nil {
		t.Fatalf("mergeKeys: %v", err)
	}
	if !strings.Contains(string(out), "chat.agent.enabled") {
		t.Fatalf("key not inserted into empty live:\n%s", out)
	}
}
