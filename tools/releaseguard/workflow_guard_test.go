package releaseguard_test

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve current file path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readRepoFile(t *testing.T, relativePath string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(repoRoot(t), relativePath))
	if err != nil {
		t.Fatalf("read %s: %v", relativePath, err)
	}

	return string(content)
}

func readRepoFileIfExists(t *testing.T, relativePath string) (string, bool) {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(repoRoot(t), relativePath))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		t.Fatalf("read %s: %v", relativePath, err)
	}

	return string(content), true
}

func assertContains(t *testing.T, content, needle string) {
	t.Helper()

	if !strings.Contains(content, needle) {
		t.Fatalf("expected content to include %q", needle)
	}
}

func assertNotContains(t *testing.T, content, needle string) {
	t.Helper()

	if strings.Contains(content, needle) {
		t.Fatalf("expected content not to include %q", needle)
	}
}

func TestReleaseWorkflowChecksOutRequestedTag(t *testing.T) {
	workflow := readRepoFile(t, filepath.Join(".github", "workflows", "release.yml"))

	assertContains(t, workflow, "checkout_ref=")
	assertContains(t, workflow, "gh release view \"$tag\" --repo \"${{ github.repository }}\" --json isDraft --jq '.isDraft'")
	assertContains(t, workflow, "ref: ${{ needs.prepare.outputs.checkout_ref }}")
	assertContains(t, workflow, "fetch-depth: 0")
	assertContains(t, workflow, "fetch-tags: true")
	assertContains(t, workflow, "SHA256SUMS.txt")
	assertContains(t, workflow, "actions/attest-build-provenance@")
	assertContains(t, workflow, "Resolve Windows signing mode")
	assertContains(t, workflow, "REQUIRE_SIGNED_WINDOWS_RELEASES")
	assertContains(t, workflow, "UNSIGNED-WINDOWS-RELEASE.txt")
	assertContains(t, workflow, "SpeechKit.sbom.json")
	assertContains(t, workflow, "vars.ENABLE_BUILD_ATTESTATIONS != 'false'")
	assertNotContains(t, workflow, "ALLOW_UNSIGNED_WINDOWS_RELEASES")
	assertContains(t, workflow, "release_windows_portable")
	assertContains(t, workflow, "release_windows_installer")
}

func TestReleaseWorkflowMarksUnsignedWindowsReleasesManualOnly(t *testing.T) {
	workflow := readRepoFile(t, filepath.Join(".github", "workflows", "release.yml"))

	assertContains(t, workflow, "UNSIGNED-WINDOWS-RELEASE.txt")
	assertContains(t, workflow, "manual-only")
	assertContains(t, workflow, "Auto-update requires signed Windows installers.")
	assertContains(t, workflow, "in-app updater will not offer this installer")
}

func TestPrivateReleaseDryRunScriptNeverPublishes(t *testing.T) {
	script, ok := readRepoFileIfExists(t, filepath.Join("scripts", "private-release-dry-run.ps1"))
	if !ok {
		t.Skip("private release dry-run script is not part of the public export")
	}
	packageJSON := readRepoFile(t, "package.json")

	assertContains(t, packageJSON, `"release:dry-run:private"`)
	assertContains(t, script, "Private release dry-run")
	assertContains(t, script, "NoOSSPublish")
	assertContains(t, script, "NoGitHubRelease")
	assertContains(t, script, "scripts/release/render-release-notes.mjs")
	assertContains(t, script, "release-notes.md")
	assertContains(t, script, "cyclonedx-gomod")
	assertContains(t, script, "SpeechKit.sbom.json")
	assertContains(t, script, "SHA256SUMS.txt")
	assertContains(t, script, "PRIVATE-RELEASE-DRY-RUN.txt")
	assertContains(t, script, "dist/release-dry-run")
	assertContains(t, script, "github.com/kombifyio/SpeechKit")
	assertNotContains(t, script, "kombifyio/SpeechKit")
	assertNotContains(t, script, "git push")
	assertNotContains(t, script, "gh release create")
	assertNotContains(t, script, "gh release edit")
	assertNotContains(t, script, "gh workflow run")
}

func TestPrivateReleaseDryRunWorkflowIsPrivateOnlyAndReadOnly(t *testing.T) {
	workflow, ok := readRepoFileIfExists(t, filepath.Join(".github", "workflows", "private-release-dry-run.yml"))
	if !ok {
		t.Skip("private release dry-run workflow is not part of the public export")
	}

	assertContains(t, workflow, "name: Private Release Dry Run")
	assertContains(t, workflow, "workflow_dispatch:")
	assertContains(t, workflow, "github.repository == 'Soulcreek/kombify-SpeechKit'")
	assertContains(t, workflow, "vars.WINDOWS_BUILD_RUNNER != ''")
	assertContains(t, workflow, "contents: read")
	assertContains(t, workflow, "actions: read")
	assertContains(t, workflow, "./scripts/private-release-dry-run.ps1")
	assertContains(t, workflow, "release-dry-run-${{ github.run_id }}")
	assertContains(t, workflow, "actions/upload-artifact@")
	assertNotContains(t, workflow, "contents: write")
	assertNotContains(t, workflow, "attestations: write")
	assertNotContains(t, workflow, "id-token: write")
	assertNotContains(t, workflow, "push:")
	assertNotContains(t, workflow, "tags: ['v*']")
	assertNotContains(t, workflow, "publish-oss")
	assertNotContains(t, workflow, "RELEASE_APP_PRIVATE_KEY")
	assertNotContains(t, workflow, "RELEASE_APP_ID")
	assertNotContains(t, workflow, "OSS_REPO")
	assertNotContains(t, workflow, "kombifyio/SpeechKit")
	assertNotContains(t, workflow, "git push")
	assertNotContains(t, workflow, "gh release create")
	assertNotContains(t, workflow, "gh release edit")
	assertNotContains(t, workflow, "gh workflow run")
}

func TestPublishOssWorkflowPublishesFromResolvedTag(t *testing.T) {
	workflow, ok := readRepoFileIfExists(t, filepath.Join(".github", "workflows", "publish-oss.yml"))
	if !ok {
		t.Skip("private OSS publisher workflow is not part of the public export")
	}

	assertContains(t, workflow, "checkout_ref=")
	assertContains(t, workflow, "ref: ${{ needs.prepare.outputs.checkout_ref }}")
	assertContains(t, workflow, "publish-source:")
	assertContains(t, workflow, "needs: [prepare, publish-source]")
	assertContains(t, workflow, "RELEASE_AUTH_TOKEN")
	assertContains(t, workflow, "workflows_backup")
	assertContains(t, workflow, "does not yet have Workflows write permission")
	assertContains(t, workflow, "git -c \"http.https://github.com/.extraheader=AUTHORIZATION: basic ${auth_header}\" \\")
	assertContains(t, workflow, "clone \"https://github.com/${OSS_REPO}.git\" oss-repo")
	assertContains(t, workflow, "git remote set-url origin \"https://github.com/${OSS_REPO}.git\"")
	assertNotContains(t, workflow, "https://x-access-token:${RELEASE_AUTH_TOKEN}@github.com/${OSS_REPO}.git")
	assertNotContains(t, workflow, "path: /tmp/oss-repo")
	assertNotContains(t, workflow, "gh release download")
	assertNotContains(t, workflow, "GIT_ASKPASS=${RUNNER_TEMP}/oss-git-askpass.sh")
	assertNotContains(t, workflow, "OSS_PUBLISH_TOKEN_FALLBACK")
	assertNotContains(t, workflow, "OSS_REPO_SSH_KEY")
	assertNotContains(t, workflow, "ssh-add - <<< \"$OSS_REPO_SSH_KEY\" >/dev/null")
	assertNotContains(t, workflow, "git@github.com:${OSS_REPO}.git")
	assertNotContains(t, workflow, "create-release:")
	assertContains(t, workflow, "Create or update public draft GitHub Release with changelog notes")
	assertContains(t, workflow, "await github('POST', `/repos/${owner}/${repo}/releases`, {")
	assertContains(t, workflow, "await github('PATCH', `/repos/${owner}/${repo}/releases/${release.id}`, {")
	assertContains(t, workflow, "publishedRelease")
	assertContains(t, workflow, "will not be moved")
	assertContains(t, workflow, "--remote-tags-url \"https://github.com/${OSS_REPO}.git\"")
	assertContains(t, workflow, "deploy-render-server:")
	assertContains(t, workflow, "actions/workflows/${workflow}/runs")
	assertContains(t, workflow, "const workflow = 'release-server-docker.yml';")
	assertContains(t, workflow, "ghcr.io/kombifyio/speechkit-server:${{ needs.prepare.outputs.tag }}")
	assertNotContains(t, workflow, "ghcr.io/kombifyio/speechkit"+"-voice:${tag}")
	assertContains(t, workflow, "allow_website_deploy_skip")
	assertContains(t, workflow, "source_ref")
	assertContains(t, workflow, "checkout_ref=\"$source_ref\"")
	assertContains(t, workflow, "website deployment continue from")
	assertContains(t, workflow, "release_windows_installer")
	assertContains(t, workflow, "release_windows_portable")
	assertContains(t, workflow, "OSS_REPO: kombifyio/SpeechKit")
}

func TestCIWorkflowRunsRaceTestsForCriticalGoPackages(t *testing.T) {
	workflow := readRepoFile(t, filepath.Join(".github", "workflows", "ci.yml"))
	assertContains(t, workflow, "go test -race")
	assertContains(t, workflow, "./pkg/speechkit/...")
	assertContains(t, workflow, "./internal/router/...")
	assertContains(t, workflow, "./cmd/speechkit-cli/...")
	assertContains(t, workflow, "./cmd/speechkit-mcp/...")
	assertContains(t, workflow, "./internal/scaffold/...")
}

func TestCIWorkflowRunsLinuxServerChecks(t *testing.T) {
	workflow := readRepoFile(t, filepath.Join(".github", "workflows", "ci.yml"))

	assertContains(t, workflow, "name: Linux Server Checks")
	assertContains(t, workflow, "GOOS: linux")
	assertContains(t, workflow, "GOARCH: amd64")
	assertContains(t, workflow, "go-version: '1.26.3'")
	assertContains(t, workflow, "Test Linux server packages")
	assertContains(t, workflow, "go test -race")
	assertContains(t, workflow, "./cmd/speechkit-server/...")
	assertContains(t, workflow, "./cmd/speechkit-mcp/...")
	assertContains(t, workflow, "./cmd/sk-e2e/...")
	assertContains(t, workflow, "./internal/server/...")
	assertContains(t, workflow, "./internal/serverclient/...")
}

// TestAutoDeployDevWorkflowRetired asserts that the legacy
// auto-deploy-dev.yml workflow has been removed in favour of the
// managed preview path. The two
// previous tests that verified the workflow's content
// (TestAutoDeployDevCoversRuntimeAndRunsLiveSmoke and
// TestAutoDeployDevMasksMultilineSSHSecretsSafely) were removed when
// the workflow was retired in v0.29.0; this single guard remains so
// the workflow doesn't sneak back without an explicit decision.
func TestAutoDeployDevWorkflowRetired(t *testing.T) {
	path := filepath.Join(".github", "workflows", "auto-deploy-dev.yml")
	if _, err := os.Stat(filepath.Join(repoRoot(t), path)); err == nil {
		t.Fatalf("auto-deploy-dev.yml is back; the workflow was retired in v0.29.0 - re-add intentionally and update this guard")
	}
}

func TestServerLinuxWorkflowRunsComposeSmokeStack(t *testing.T) {
	workflow := readRepoFile(t, filepath.Join(".github", "workflows", "server-linux.yml"))

	assertContains(t, workflow, "docker compose -f deploy/docker/docker-compose.test.yml up")
	assertContains(t, workflow, "--exit-code-from test-client")
	assertContains(t, workflow, "SPEECHKIT_SERVER_IMAGE: speechkit-server:ci")
	assertContains(t, workflow, "go test -race ./internal/server/...")
	assertContains(t, workflow, "Build speechkit-server image")
	assertNotContains(t, workflow, "Build speechkit"+"-voice image")
	assertNotContains(t, workflow, "speechkit"+"-voice")
	assertNotContains(t, workflow, "matrix.target")
}

func TestCloudflareQualityGateWorkflowStaysShadowOnly(t *testing.T) {
	workflow, ok := readRepoFileIfExists(t, filepath.Join(".github", "workflows", "cloudflare-quality-gate.yml"))
	if !ok {
		t.Skip("cloudflare quality gate workflow is not part of the public workflow export")
	}

	assertContains(t, workflow, "name: Cloudflare SpeechKit Quality Gate")
	assertContains(t, workflow, "workflow_dispatch:")
	assertContains(t, workflow, "workflow_call:")
	assertContains(t, workflow, "SPEECHKIT_CLOUDFLARE_QUALITY_GATE_URL")
	assertContains(t, workflow, "SPEECHKIT_CLOUDFLARE_GATE_TOKEN")
	assertContains(t, workflow, "node scripts/cloudflare-quality-gate.mjs")
	assertContains(t, workflow, "--release-version")
	assertNotContains(t, workflow, "--require-pass")
}

func TestCIWorkflowFailsWhenCoverageDropsBelowMinimum(t *testing.T) {
	workflow := readRepoFile(t, filepath.Join(".github", "workflows", "ci.yml"))

	assertContains(t, workflow, "SPEECHKIT_COVERAGE_MIN: '60.0'")
	assertContains(t, workflow, "coverage gate")
	assertContains(t, workflow, "total coverage")
	assertContains(t, workflow, `gsub(/%/, "", $NF)`)
}

func TestCIWorkflowRunsWebsiteChecksWhenWebsiteExists(t *testing.T) {
	workflow := readRepoFile(t, filepath.Join(".github", "workflows", "ci.yml"))

	if _, err := os.Stat(filepath.Join(repoRoot(t), "Website")); err != nil {
		if os.IsNotExist(err) {
			assertContains(t, workflow, "name: Detect website workspace")
			assertContains(t, workflow, "Website workspace is not part of this repository; skipping Website checks.")
			assertContains(t, workflow, "if: steps.website-workspace.outputs.present == 'true'")
			return
		}
		t.Fatalf("stat Website: %v", err)
	}

	assertContains(t, workflow, "name: Website Checks")
	assertContains(t, workflow, "name: Detect website workspace")
	assertContains(t, workflow, "if: steps.website-workspace.outputs.present == 'true'")
	assertContains(t, workflow, "working-directory: Website")
	assertContains(t, workflow, "cache-dependency-path: Website/package-lock.json")
	assertContains(t, workflow, "npm run check")
	assertContains(t, workflow, "npm test")
	assertContains(t, workflow, "npm run build")
	assertContains(t, workflow, "Website/**")
	assertNotContains(t, workflow, "marketing-site-svelte")
}

func TestReleaseBuildWorkflowsDoNotSkipVerification(t *testing.T) {
	for _, workflowName := range []string{"build.yml", "release.yml", "windows-build.yml"} {
		workflow := readRepoFile(t, filepath.Join(".github", "workflows", workflowName))
		assertNotContains(t, workflow, "-SkipVerification")
	}
}

func TestDependabotOnlyReferencesExistingProjectDirectories(t *testing.T) {
	dependabot := readRepoFile(t, filepath.Join(".github", "dependabot.yml"))

	assertContains(t, dependabot, "directory: /frontend/app")
	if _, err := os.Stat(filepath.Join(repoRoot(t), "Website")); err == nil {
		assertContains(t, dependabot, "directory: /Website")
	} else {
		assertNotContains(t, dependabot, "directory: /Website")
	}
	assertNotContains(t, dependabot, "directory: /marketing-site-svelte")
}

func TestSecurityWorkflowRunsRequiredGates(t *testing.T) {
	workflow, ok := readRepoFileIfExists(t, filepath.Join(".github", "workflows", "security.yml"))
	if !ok {
		t.Skip("private security workflow is not part of the public export")
	}

	assertContains(t, workflow, "name: Security")
	assertContains(t, workflow, "Run TruffleHog")
	assertContains(t, workflow, "OSV dependency scan")
	assertContains(t, workflow, "Trivy repository scan")
	assertContains(t, workflow, "govulncheck ./...")
	assertContains(t, workflow, "staticcheck ./...")
	assertContains(t, workflow, "gosec -quiet -severity medium -confidence medium ./...")
	assertNotContains(t, workflow, "-exclude=G101,G104")
	assertNotContains(t, workflow, "-exclude=G115,G118")
	assertContains(t, workflow, "security-passed")
	assertContains(t, workflow, "Verify security gates")
}

func TestDeploymentStandardsDocumentBranchProtectionGates(t *testing.T) {
	docs, ok := readRepoFileIfExists(t, filepath.Join("docs", "deployment-standards.md"))
	if !ok {
		t.Skip("private deployment standards are not part of the public export")
	}

	assertContains(t, docs, "## Branch Protection")
	assertContains(t, docs, "The `main` branch is protected")
	assertContains(t, docs, "`Go Analysis`")
	assertContains(t, docs, "`Frontend Checks`")
	assertContains(t, docs, "`Website Checks`")
	assertContains(t, docs, "`Dependency Review`")
	assertContains(t, docs, "`security-passed`")
	assertContains(t, docs, "`Windows Bundle`")
	assertContains(t, docs, "owner, date, and removal criterion")
}

func TestServerReferenceConfigRequiresProductionAuthByDefault(t *testing.T) {
	config := readRepoFile(t, filepath.Join("deploy", "config", "server.example.toml"))
	docs := readRepoFile(t, filepath.Join("docs", "server", "README.md"))

	assertContains(t, config, `auth_mode              = "bearer_or_edge"`)
	assertContains(t, docs, "`bearer_or_edge` (Kombify production default)")
	assertContains(t, docs, "`none` — local development only")
	assertNotContains(t, docs, "`none` (default)")
}

func TestComposeSmokeEscapesShellVariables(t *testing.T) {
	compose := readRepoFile(t, filepath.Join("deploy", "docker", "docker-compose.test.yml"))

	assertContains(t, compose, `ready_code=$$(curl`)
	assertContains(t, compose, `if [ "$$ready_code" != "200" ]`)
	assertContains(t, compose, `code=$$(curl`)
	assertContains(t, compose, `if [ "$$code" = "200" ]`)
	assertNotContains(t, compose, `ready_code=$(curl`)
	assertNotContains(t, compose, `code=$(curl`)
}

func TestServerTestUIUsesLiveBackendWithoutClientSetup(t *testing.T) {
	ui := readRepoFile(t, filepath.Join("internal", "server", "onboarding", "assets", "testui.html"))

	for _, forbidden := range []string{
		"Base URL",
		"baseUrl",
		"Auth",
		"Authorization",
		"Bearer Token",
		"Token",
		"token",
		"bearerToken",
		"X-Edge-Auth-Hmac",
		"edgeHmac",
		"authMode",
		"Persona ID",
		"Role ID",
		"Sequence ID",
		"TTS Voice",
		`new URL(path, base)`,
	} {
		assertNotContains(t, ui, forbidden)
	}

	assertContains(t, ui, `id="runSmoke"`)
	assertContains(t, ui, `fetch(path, opts)`)
	assertContains(t, ui, `"/healthz"`)
	assertContains(t, ui, `"/readyz"`)
	assertContains(t, ui, `"/api/v1/dictation/transcribe"`)
	assertContains(t, ui, `"/api/v1/assist/process"`)
	assertContains(t, ui, `copy last`)
	assertContains(t, ui, `insert last`)
	assertContains(t, ui, `summarize this`)
	assertContains(t, ui, `"/api/v1/voiceagent/sessions"`)
}

func TestAllWorkflowActionsArePinnedToCommitSHA(t *testing.T) {
	workflowsDir := filepath.Join(repoRoot(t), ".github", "workflows")
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		t.Fatalf("read workflows dir: %v", err)
	}

	usesPattern := regexp.MustCompile(`^\s*uses:\s*([^\s#]+)\s*$`)
	shaPattern := regexp.MustCompile(`^[0-9a-f]{40}$`)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if ext := filepath.Ext(entry.Name()); ext != ".yml" && ext != ".yaml" {
			continue
		}

		fullPath := filepath.Join(workflowsDir, entry.Name())
		file, openErr := os.Open(fullPath)
		if openErr != nil {
			t.Fatalf("open %s: %v", entry.Name(), openErr)
		}

		scanner := bufio.NewScanner(file)
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			matches := usesPattern.FindStringSubmatch(line)
			if len(matches) != 2 {
				continue
			}

			actionRef := strings.TrimSpace(matches[1])
			if strings.HasPrefix(actionRef, "./") || strings.HasPrefix(actionRef, "docker://") {
				continue
			}

			atIdx := strings.LastIndex(actionRef, "@")
			if atIdx <= 0 || atIdx == len(actionRef)-1 {
				_ = file.Close()
				t.Fatalf("%s:%d uses entry must include a commit SHA ref, got %q", entry.Name(), lineNo, actionRef)
			}

			ref := actionRef[atIdx+1:]
			if !shaPattern.MatchString(ref) {
				_ = file.Close()
				t.Fatalf("%s:%d uses entry must be pinned to a full 40-char SHA, got %q", entry.Name(), lineNo, actionRef)
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			_ = file.Close()
			t.Fatalf("scan %s: %v", entry.Name(), scanErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			t.Fatalf("close %s: %v", entry.Name(), closeErr)
		}
	}
}

func TestLegacyPublicExportGitlinkRemoved(t *testing.T) {
	command := exec.Command("git", "ls-files", "--stage", "--", ".public-export-v8")
	command.Dir = repoRoot(t)

	output, err := command.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && strings.Contains(string(exitErr.Stderr), "not a git repository") {
			t.Skip("git metadata is not available in this exported tree")
		}
		t.Fatalf("git ls-files failed: %v", err)
	}

	if trimmed := bytes.TrimSpace(output); len(trimmed) > 0 {
		t.Fatalf("legacy .public-export-v8 gitlink is still tracked:\n%s", trimmed)
	}
}
