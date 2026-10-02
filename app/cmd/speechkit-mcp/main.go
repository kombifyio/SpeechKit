package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	skclient "github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

func main() {
	modeFlag := flag.String("mode", "docs", "comma-separated modes: docs,management,test")
	serverURL := flag.String("server", firstNonEmpty(os.Getenv("SPEECHKIT_SERVER_URL"), "http://localhost:8080"), "SpeechKit server URL")
	token := flag.String("token", "", "SpeechKit bearer token")
	transport := flag.String("transport", "stdio", "transport: stdio or http")
	addr := flag.String("addr", "127.0.0.1:8090", "HTTP listen address when --transport=http")
	mcpToken := flag.String("mcp-token", "", "Bearer token required by the MCP HTTP transport")
	flag.Parse()

	opts := serverOptions{
		modes:     parseModes(*modeFlag),
		serverURL: *serverURL,
		token:     firstNonEmpty(*token, os.Getenv("SPEECHKIT_TOKEN"), os.Getenv("SPEECHKIT_SERVER_TOKEN")),
		transport: strings.ToLower(strings.TrimSpace(*transport)),
		mcpToken:  firstNonEmpty(*mcpToken, os.Getenv("SPEECHKIT_MCP_TOKEN")),
	}
	c, err := skclient.New(skclient.Options{BaseURL: opts.serverURL, Token: opts.token, UserAgent: "speechkit-mcp/0.1"})
	if err != nil {
		log.Fatal(err)
	}
	app := &speechkitMCP{opts: opts, client: c, docs: loadDocs()}
	server := app.newServer()

	switch opts.transport {
	case "http":
		if opts.modes["management"] && !isLoopbackListenAddr(*addr) && opts.mcpToken == "" {
			log.Fatal("--mcp-token or SPEECHKIT_MCP_TOKEN is required when management mode is exposed over non-loopback HTTP")
		}
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
			SessionTimeout: 30 * time.Minute,
		})
		var httpHandler http.Handler = handler
		if opts.mcpToken != "" {
			httpHandler = requireMCPToken(opts.mcpToken, httpHandler)
		}
		log.Fatal(newHTTPServer(*addr, httpHandler).ListenAndServe())
	default:
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			log.Fatal(err)
		}
	}
}
