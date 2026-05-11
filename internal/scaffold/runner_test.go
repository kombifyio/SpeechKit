package scaffold

import "testing"

func TestParseHookCommandAllowsKnownTemplateHooks(t *testing.T) {
	bin, args, err := parseHookCommand("npm install")
	if err != nil {
		t.Fatalf("parseHookCommand: %v", err)
	}
	if bin != "npm" {
		t.Fatalf("bin = %q, want npm", bin)
	}
	if len(args) != 1 || args[0] != "install" {
		t.Fatalf("args = %#v, want [install]", args)
	}
}

func TestParseHookCommandRejectsShellSyntax(t *testing.T) {
	for _, cmd := range []string{
		"npm install && calc",
		"npm install; calc",
		"npm install | more",
		"powershell -Command Invoke-WebRequest http://example.invalid",
	} {
		if _, _, err := parseHookCommand(cmd); err == nil {
			t.Fatalf("parseHookCommand(%q) succeeded, want error", cmd)
		}
	}
}
