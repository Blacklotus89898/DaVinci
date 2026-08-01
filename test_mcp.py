#!/usr/bin/env python3
"""MCP usability test for knowledge-service."""
import subprocess, json, sys, os, io, time

sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8', errors='replace')

env = os.environ.copy()
env["DB_PATH"] = "knowledge.db"
env["EMBED_PROVIDER"] = "ollama"
env["EMBED_URL"] = "http://localhost:11434"
env["EMBED_MODEL"] = "nomic-embed-text"
env["LOG_LEVEL"] = "warn"

proc = subprocess.Popen(
    [r".\bin\knowledge-service.exe"],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.PIPE,
    env=env,
    text=True,
    encoding="utf-8",
    bufsize=1,
)

_id = 0

def rpc(method, params=None, expects_reply=True):
    global _id
    msg = {"jsonrpc": "2.0", "method": method}
    if expects_reply:
        _id += 1
        msg["id"] = _id
    if params:
        msg["params"] = params
    proc.stdin.write(json.dumps(msg) + "\n")
    proc.stdin.flush()
    if not expects_reply:
        return None
    line = proc.stdout.readline()
    return json.loads(line)

def tool(name, args):
    r = rpc("tools/call", {"name": name, "arguments": args})
    if "error" in r:
        return None, r["error"]["message"]
    return r["result"]["content"][0]["text"], None

P = "✓"
F = "✗"

print("\n=== knowledge-service MCP assessment ===\n")

# 1. Initialize handshake
r = rpc("initialize", {"protocolVersion": "2025-03-26", "clientInfo": {"name": "test", "version": "1"}})
info = r["result"]["serverInfo"]
print(f"{P} initialize: {info['name']} {info['version']}")
rpc("notifications/initialized", expects_reply=False)

# 2. list_knowledge
text, err = tool("list_knowledge", {})
if err:
    print(f"{F} list_knowledge: {err}")
else:
    lines = [l for l in text.splitlines() if l.strip()]
    print(f"\n{P} list_knowledge: {lines[0]}")
    for l in lines[1:7]:
        print(f"     {l}")

# 3. search — exact SRE term
text, err = tool("search_knowledge", {"query": "OOMKilled pod memory", "limit": 3})
if err:
    print(f"\n{F} search OOMKilled: {err}")
else:
    found = "No results" not in text
    print(f"\n{P if found else F} search 'OOMKilled pod memory': {'hit' if found else 'MISS'}")
    for l in text.splitlines()[:4]:
        if l.strip(): print(f"     {l}")

# 4. search — paraphrase (semantic channel test)
text, err = tool("search_knowledge", {"query": "disk full no space left on device", "limit": 3})
if err:
    print(f"\n{F} search paraphrase: {err}")
else:
    found = "No results" not in text
    print(f"\n{P if found else F} search 'disk full / no space left' (semantic): {'hit' if found else 'MISS'}")
    for l in text.splitlines()[:3]:
        if l.strip(): print(f"     {l}")

# 5. write_knowledge (valid)
print("\n[write_knowledge — writing test runbook...]")
text, err = tool("write_knowledge", {
    "path": "runbooks/test-oom-runbook.md",
    "heading": "OOM Pod Restart Fix",
    "content": "## Symptom\nPod OOMKilled repeatedly.\n## Fix\n```bash\nkubectl set resources deployment myapp --limits memory=512Mi\n```\n## Prevention\nAdd resource limits to all deployments."
})
if err:
    print(f"{F} write_knowledge: {err}")
else:
    print(f"{P} write_knowledge: {text}")

# 6. search after write
time.sleep(1)
text, err = tool("search_knowledge", {"query": "oom pod restart kubectl memory limits", "limit": 3})
found = text and "test-oom-runbook" in text
print(f"\n{P if found else F} search finds newly written runbook: {'yes' if found else 'NO - ' + str(text)[:80]}")

# 7. empty heading must be rejected
text, err = tool("write_knowledge", {"path": "test/x.md", "heading": "", "content": "test"})
rejected = err is not None
print(f"\n{P if rejected else F} empty heading rejected: {'yes (' + err + ')' if rejected else 'NOT REJECTED - bug!'}")

# 8. write a tool entry
print("\n[write_knowledge — writing kubectl drain tool...]")
text, err = tool("write_knowledge", {
    "path": "tools/k8s-drain.md",
    "heading": "Drain Kubernetes Node",
    "content": "Safely evict all pods before node maintenance.\n\n```bash\nkubectl drain <node> --ignore-daemonsets --delete-emptydir-data\nkubectl uncordon <node>\n```"
})
print(f"{P if not err else F} write tool: {text or err}")

# 9. get_tool
time.sleep(1)
text, err = tool("get_tool", {"name": "drain node maintenance kubernetes"})
has_cmd = text and "kubectl drain" in text
no_result = text and "No tool found" in text
print(f"\n{P if has_cmd else F} get_tool 'drain node': {'got kubectl drain' if has_cmd else ('no tools/ match' if no_result else str(text)[:80])}")
if has_cmd:
    for l in text.splitlines()[:6]:
        if l.strip(): print(f"     {l}")

# 10. delete and confirm gone
text, err = tool("delete_knowledge", {"path": "runbooks/test-oom-runbook.md"})
print(f"\n{P if not err else F} delete runbook: {text or err}")

text, err = tool("delete_knowledge", {"path": "tools/k8s-drain.md"})
print(f"{P if not err else F} delete tool entry: {text or err}")

text, err = tool("search_knowledge", {"query": "OOM Pod Restart Fix", "limit": 3})
gone = text and "test-oom-runbook" not in text
print(f"{P if gone else F} deleted doc absent from search: {'confirmed' if gone else 'STILL THERE'}")

# 11. tools/list
r = rpc("tools/list")
tools = [t["name"] for t in r["result"]["tools"]]
print(f"\n{P} tools/list: {tools}")

proc.stdin.close()
proc.wait(timeout=10)
print("\n=== Done ===\n")
