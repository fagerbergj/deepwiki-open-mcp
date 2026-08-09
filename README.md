# deepwiki-open-mcp

A thin MCP server that wraps a self-hosted [deepwiki-open](https://github.com/AsyncFuncAI/deepwiki-open) instance and exposes the same three tools as Devin's closed-source [DeepWiki MCP server](https://mcp.deepwiki.com/mcp): `read_wiki_structure`, `read_wiki_contents`, and `ask_question`, with matching names, descriptions, and input schemas. Any MCP client already configured for Devin's server works against this one by swapping the URL — answers just come from your own deepwiki-open instance instead.

## Environment variables

| Var | Required | Default | Purpose |
|---|---|---|---|
| `DEEPWIKI_URL` | yes | - | Base URL of deepwiki-open's frontend (e.g. `http://deepwiki:3000`) |
| `DEEPWIKI_PROVIDER` | no | `openrouter` | Model provider passed through to deepwiki-open's chat endpoint |
| `DEEPWIKI_MODEL` | no | `qwen3.6-35b` | Model name passed through to deepwiki-open's chat endpoint |
| `PORT` | no | `8080` | Port the server listens on |

Missing `DEEPWIKI_URL` is a startup error.

## Docker Compose

```yaml
services:
  deepwiki-open-mcp:
    image: ghcr.io/fagerbergj/deepwiki-open-mcp:latest
    environment:
      DEEPWIKI_URL: http://deepwiki:3000
      DEEPWIKI_PROVIDER: openrouter
      DEEPWIKI_MODEL: qwen3.6-35b
    ports:
      - "8080:8080"
```

## Calling it

The MCP endpoint is `/mcp` (Streamable HTTP JSON-RPC); `/healthz` returns `200 ok`. `initialize` returns an `Mcp-Session-Id` response header that must be echoed on every subsequent request:

```bash
SID=$(curl -si http://localhost:8080/mcp \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"cli","version":"0"}}}' \
  | grep -i mcp-session-id | awk '{print $2}' | tr -d '\r')

curl -s http://localhost:8080/mcp \
  -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"ask_question","arguments":{"repoName":"AsyncFuncAI/deepwiki-open","question":"What does this project do?"}}}'
```

## License

MIT, see [LICENSE](LICENSE).
