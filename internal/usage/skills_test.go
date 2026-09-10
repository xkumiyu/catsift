package usage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectStructuredSkillEvidence(t *testing.T) {
	source := SourceRef{Path: "fixture", Line: 1}
	evidence := DetectInjectedSkills(`<skill name="report">body</skill>`, "s", "t", time.Unix(1, 0), source)
	if len(evidence) != 1 || evidence[0].SkillName != "report" || evidence[0].State != StateConfirmed {
		t.Fatalf("injected evidence = %#v", evidence)
	}
	request := DetectExplicitRequest("  $report summarize this", "s", "t", time.Unix(1, 0), source)
	if len(request) != 1 || request[0].SkillName != "report" || request[0].State != StateUnconfirmed {
		t.Fatalf("request evidence = %#v", request)
	}
	namespaced := DetectExplicitRequest("$data-analytics:index summarize this", "s", "t", time.Unix(1, 0), source)
	if len(namespaced) != 1 || namespaced[0].SkillName != "data-analytics:index" {
		t.Fatalf("namespaced request evidence = %#v", namespaced)
	}
	if got := DetectExplicitRequest("please use $report", "s", "t", time.Unix(1, 0), source); len(got) != 0 {
		t.Fatalf("prose was detected: %#v", got)
	}
}

func TestDetectSkillContentBlock(t *testing.T) {
	evidence := DetectInjectedSkills(`<skill_content name="report">body</skill_content>`, "s", "t", time.Unix(1, 0), SourceRef{Path: "fixture"})
	if len(evidence) != 1 || evidence[0].SkillName != "report" || evidence[0].State != StateConfirmed {
		t.Fatalf("skill content evidence = %#v", evidence)
	}
}

func TestInjectedSkillCanKeepUnknownActivationMode(t *testing.T) {
	evidence := DetectInjectedSkillsWithMode(
		`<skill name="report">body</skill>`,
		"s", "t", ModeUnknown, time.Unix(1, 0), SourceRef{Path: "fixture", Line: 1},
	)
	if len(evidence) != 1 || evidence[0].Mode != ModeUnknown || evidence[0].Method != MethodSkillInjection || evidence[0].State != StateConfirmed {
		t.Fatalf("unknown injection evidence = %#v", evidence)
	}
	selected := DetectSelectedSkillInstructions(
		`<skill name="report">body</skill>`,
		"s", "t", time.Unix(1, 0), SourceRef{Path: "fixture", Line: 2},
	)
	if len(selected) != 1 || selected[0].Mode != ModeUnknown || selected[0].Method != MethodSelectedSkillInstructions {
		t.Fatalf("selected injection evidence = %#v", selected)
	}
}

func TestDetectRuntimeSkillItems(t *testing.T) {
	item := map[string]any{"type": "UserMessage", "content": []any{
		map[string]any{"type": "text", "text": "$report"},
		map[string]any{"type": "skill", "name": "report", "path": "/fixture-home/.agents/skills/report/SKILL.md"},
	}}
	got := DetectRuntimeSkillItems(item, "s", "t", time.Unix(1, 0), SourceRef{})
	if len(got) != 1 || got[0].SkillName != "report" || got[0].Mode != ModeUnknown || got[0].Method != MethodRuntimeSkillItem || got[0].State != StateConfirmed {
		t.Fatalf("runtime skill evidence = %#v", got)
	}
}

func TestDetectStructuredSkillToolRequiresUniqueTarget(t *testing.T) {
	tool := ToolObservation{RawName: "skills.read", SessionID: "s", TurnID: "t", Arguments: `{"skill":"report"}`, Timestamp: time.Unix(1, 0)}
	got := DetectStructuredSkillTool(tool)
	if len(got) != 1 || got[0].SkillName != "report" || got[0].State != StateConfirmed {
		t.Fatalf("structured evidence = %#v", got)
	}
	tool.Arguments = `{"skill":"report","name":"other"}`
	if got := DetectStructuredSkillTool(tool); len(got) != 0 {
		t.Fatalf("ambiguous evidence = %#v", got)
	}
}

func TestImplicitAccessAndFrontmatterFallback(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".agents", "skills", "dir-name")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: canonical-name\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := DetectImplicitAccess("cat '"+path+"'", "s", "t", time.Unix(1, 0), SourceRef{})
	if len(evidence) != 1 || evidence[0].SkillName != "canonical-name" || evidence[0].State != StateInferred {
		t.Fatalf("implicit evidence = %#v", evidence)
	}
	if got := SkillNameFromPath("/skills/removed/scripts/run.sh"); got != "removed" {
		t.Fatalf("directory fallback = %q", got)
	}
	otherDir := filepath.Join(root, "skills", "untrusted")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(otherDir, "SKILL.md")
	if err := os.WriteFile(otherPath, []byte("name: should-not-be-read\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if evidence := DetectImplicitAccess("cat "+otherPath, "s", "t", time.Unix(1, 0), SourceRef{}); len(evidence) != 1 || evidence[0].SkillName != "untrusted" {
		t.Fatalf("untrusted path evidence = %#v", evidence)
	}
	if evidence := DetectImplicitAccess("rg -o 'skills/ctx/SKILL.md' history.jsonl", "s", "t", time.Unix(1, 0), SourceRef{}); len(evidence) != 0 {
		t.Fatalf("search pattern was detected as access = %#v", evidence)
	}
	if evidence := DetectImplicitAccess("printf 'skills/{ctx,report}/SKILL.md'", "s", "t", time.Unix(1, 0), SourceRef{}); len(evidence) != 0 {
		t.Fatalf("glob pattern was detected as access = %#v", evidence)
	}
	if got := SkillNameFromPath("/fixture-home/.codex/skills/.system/openai-docs/SKILL.md"); got != "openai-docs" {
		t.Fatalf("system skill name = %q", got)
	}
	if got := SkillNameFromPath("/fixture-home/.agents/skills/{ctx,report}/SKILL.md"); got != "" {
		t.Fatalf("glob skill name = %q", got)
	}
	if got := SkillNameFromPath("/fixture-home/.agents/skills/report/../other/SKILL.md"); got != "other" {
		t.Fatalf("cleaned skill name = %q", got)
	}
}

func TestSkillPathPreservesPluginNamespaceAndRejectsRootAliasSegments(t *testing.T) {
	pluginSkill := "/fixture-home/.codex/plugins/cache/example-plugin/data-analytics/0.2.35-build/skills/index/SKILL.md"
	if got := SkillNameFromPath(pluginSkill); got != "data-analytics:index" {
		t.Fatalf("plugin skill name = %q", got)
	}
	if got := SkillNameFromPath("/fixture-home/.codex/plugins/cache/example-plugin/data-analytics/0.2.35-build/skills/index/scripts/render.py"); got != "data-analytics:index" {
		t.Fatalf("plugin script skill name = %q", got)
	}

	for _, path := range []string{
		"/fixture-home/.codex/skills/r0/ponytail/SKILL.md",
		"/fixture-home/.codex/skills/r4/agent-browser/SKILL.md",
		"/fixture-home/.codex/skills/r17/custom/SKILL.md",
	} {
		if got := SkillNameFromPath(path); got != "" {
			t.Errorf("invalid root alias path %q resolved to %q", path, got)
		}
	}
}

func TestPluginFrontmatterKeepsNamespace(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, ".codex", "plugins", "cache", "example-plugin", "data-analytics", "0.2.35-build", "skills", "router")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: index\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	evidence := DetectImplicitAccess("cat "+path, "s", "t", time.Unix(1, 0), SourceRef{})
	if len(evidence) != 1 || evidence[0].SkillName != "data-analytics:index" {
		t.Fatalf("plugin frontmatter evidence = %#v", evidence)
	}
}

func TestImplicitAccessParsesReadersShellWrappersAndScripts(t *testing.T) {
	path := "/fixture-home/.agents/skills/report/SKILL.md"
	commands := []string{
		"cat '" + path + "'",
		"rtk proxy cat " + path,
		"rtk run -c 'cat " + path + "'",
		`["/bin/zsh","-lc","sed -n '1,20p' ` + path + `"]`,
		"python3 /fixture-home/.agents/skills/report/scripts/render.py",
	}
	for _, command := range commands {
		if got := DetectImplicitAccess(command, "s", "t", time.Unix(1, 0), SourceRef{}); len(got) != 1 || got[0].SkillName != "report" {
			t.Errorf("command %q evidence = %#v", command, got)
		}
	}
	for _, command := range []string{
		"printf '" + path + "'",
		"echo " + path,
		"rg --files /fixture-home/.agents/skills",
		"python3 -c 'print(\"" + path + "\")'",
	} {
		if got := DetectImplicitAccess(command, "s", "t", time.Unix(1, 0), SourceRef{}); len(got) != 0 {
			t.Errorf("non-access command %q evidence = %#v", command, got)
		}
	}
	for _, command := range []string{
		"sudo -n cat " + path,
		"bash -e /fixture-home/.agents/skills/report/scripts/render.sh",
	} {
		if got := DetectImplicitAccess(command, "s", "t", time.Unix(1, 0), SourceRef{}); len(got) != 1 || got[0].SkillName != "report" {
			t.Errorf("wrapped access command %q evidence = %#v", command, got)
		}
	}
}

func TestMergeSkillEvidence(t *testing.T) {
	source := SourceRef{Path: "fixture", Line: 1}
	stamp := time.Unix(1, 0)
	evidence := []SkillEvidence{
		NewSkillEvidence("s", "t", "report", ModeExplicit, MethodExplicitRequest, StateUnconfirmed, stamp, source),
		NewSkillEvidence("s", "t", "report", ModeExplicit, MethodExplicitInjected, StateConfirmed, stamp.Add(time.Second), source),
		NewSkillEvidence("s", "t", "report", ModeImplicit, MethodImplicitAccess, StateInferred, stamp.Add(2*time.Second), source),
		NewSkillEvidence("s", "other", "report", ModeExplicit, MethodExplicitRequest, StateUnconfirmed, stamp, source),
	}
	uses := MergeSkillEvidence(evidence)
	if len(uses) != 2 {
		t.Fatalf("uses = %#v", uses)
	}
	var merged *SkillUse
	for i := range uses {
		if uses[i].TurnID == "t" {
			merged = &uses[i]
		}
	}
	if merged == nil || merged.State != StateConfirmed || merged.Mode != ModeExplicit || len(merged.Methods) != 3 {
		t.Fatalf("merged use = %#v", merged)
	}
	if !merged.HasMode(ModeExplicit) || !merged.HasMode(ModeImplicit) || len(merged.Modes) != 2 {
		t.Fatalf("merged modes = %#v", merged)
	}
}

func TestMergeSkillEvidenceKeepsAgentsSeparate(t *testing.T) {
	stamp := time.Unix(1, 0)
	evidence := []SkillEvidence{
		NewSkillEvidence("shared-session", "turn", "review", ModeExplicit, MethodStructuredTool, StateConfirmed, stamp, SourceRef{Source: SourceCtx, Agent: "codex"}),
		NewSkillEvidence("shared-session", "turn", "review", ModeExplicit, MethodStructuredTool, StateConfirmed, stamp, SourceRef{Source: SourceCtx, Agent: "opencode"}),
	}
	if got := MergeSkillEvidence(evidence); len(got) != 2 {
		t.Fatalf("merged evidence = %#v", got)
	}
}

func TestMergeResolvesInjectedOriginFromExplicitRequest(t *testing.T) {
	stamp := time.Unix(1, 0)
	evidence := []SkillEvidence{
		NewSkillEvidence("s", "t", "report", ModeUnknown, MethodSelectedSkillInstructions, StateConfirmed, stamp, SourceRef{}),
		NewSkillEvidence("s", "t", "report", ModeExplicit, MethodExplicitRequest, StateUnconfirmed, stamp.Add(time.Second), SourceRef{}),
	}
	uses := MergeSkillEvidence(evidence)
	if len(uses) != 1 || !uses[0].HasMode(ModeExplicit) || uses[0].HasMode(ModeUnknown) {
		t.Fatalf("resolved uses = %#v", uses)
	}
	if len(uses[0].Methods) != 2 {
		t.Fatalf("resolved methods = %#v", uses[0].Methods)
	}
}
