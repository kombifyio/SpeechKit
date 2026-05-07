package speechkitdocs

import "embed"

//go:embed server/openapi.v1.yaml server/asyncapi.v1.yaml server/README.md speechkit-architecture-v2.md mcp/README.md mcp/distribution.md mcp/examples/curl/dictation.md mcp/examples/go/dictation.md mcp/examples/python/dictation.md mcp/examples/typescript/dictation.md agent/llms.txt agent/llms-full.txt agent/llms-snippets.txt agent/getting-started/technical.md agent/getting-started/agents.md agent/install/server.md agent/mcp/speechkit-mcp.md
var FS embed.FS
