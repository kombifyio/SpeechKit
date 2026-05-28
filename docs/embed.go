package speechkitdocs

import "embed"

//go:embed server/openapi.v1.yaml server/asyncapi.v1.yaml server/README.md architecture/sdk-surface-boundary.md speechkit-framework-api.md
//go:embed wakeword.md voice-companion.md
//go:embed mcp/README.md mcp/distribution.md mcp/examples/curl/dictation.md mcp/examples/go/dictation.md mcp/examples/python/dictation.md mcp/examples/typescript/dictation.md
//go:embed agent/llms.txt agent/llms-full.txt agent/llms-snippets.txt
//go:embed agent/getting-started/technical.md agent/getting-started/agents.md
//go:embed agent/getting-started/agents/tri-mode-web-demo.md agent/getting-started/agents/voice-game-moderator.md agent/getting-started/agents/android-memo-app.md agent/getting-started/agents/go-framework-integration.md
//go:embed agent/install/server.md agent/install-server/docker-compose.example.yml agent/install-server/config.browser.example.toml
//go:embed agent/mcp/speechkit-mcp.md
//go:embed agent/schemas/speechkit-one-shot-manifest.schema.json agent/schemas/speechkit-one-shot-functional-result.schema.json
var FS embed.FS
