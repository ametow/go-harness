# A tiny MCP server for trying out the harness: it offers one tool, "add".
# MCP over stdio is just one JSON-RPC message per line on stdin/stdout.
# Used by the sample mcp.json. Run from the project root: go run .
import json, sys
for line in sys.stdin:
    r = json.loads(line)
    if "id" not in r:
        continue
    m = r["method"]
    if m == "initialize":
        res = {"protocolVersion": "2025-06-18", "capabilities": {"tools": {}}, "serverInfo": {"name": "adder", "version": "1"}}
    elif m == "tools/list":
        res = {"tools": [{"name": "add", "description": "Add two numbers and return the sum.",
               "inputSchema": {"type": "object", "properties": {"a": {"type": "number"}, "b": {"type": "number"}}, "required": ["a", "b"]}}]}
    elif m == "tools/call":
        a = r["params"]["arguments"]
        res = {"content": [{"type": "text", "text": str(a["a"] + a["b"])}]}
    else:
        print(json.dumps({"jsonrpc": "2.0", "id": r["id"], "error": {"code": -32601, "message": "nope"}}), flush=True)
        continue
    print(json.dumps({"jsonrpc": "2.0", "id": r["id"], "result": res}), flush=True)
