#!/usr/bin/env python3
"""Minimal GNS3 v3 REST + telnet-console harness for InfraFlow lab tests.

This is external lab tooling. It is not part of the InfraFlow runtime and
InfraFlow never calls GNS3. Credentials are read from the environment
(GNS3_USER / GNS3_PASSWORD, both required; controller GNS3_URL, default
http://127.0.0.1:3080) and are never written to disk or printed.

Usage (CLI):
  gns3lab.py project-create <name>
  gns3lab.py node-from-template <project> <template-id> <name> [json-overrides]
  gns3lab.py node-docker <project> <image> <name> [adapters]
  gns3lab.py link <project> <nodeA> <adapterA> <portA> <nodeB> <adapterB> <portB>
  gns3lab.py start|stop|reload <project> <node>
  gns3lab.py nodes <project>
  gns3lab.py expect <project> <node> <regex> [timeout] [send-before]
  gns3lab.py cmd <project> <node> <command> [prompt-regex] [timeout]
  gns3lab.py capture-start|capture-stop <project> <link-id>
"""
import json
import os
import re
import socket
import sys
import time
import urllib.request

BASE = os.environ.get("GNS3_URL", "http://127.0.0.1:3080").rstrip("/") + "/v3"
_TOKEN = None


def _token():
    global _TOKEN
    if _TOKEN is None:
        user, password = os.environ.get("GNS3_USER"), os.environ.get("GNS3_PASSWORD")
        if not user or not password:
            raise SystemExit("GNS3_USER and GNS3_PASSWORD must be set in the environment")
        body = json.dumps({"username": user, "password": password}).encode()
        req = urllib.request.Request(BASE + "/access/users/authenticate", data=body,
                                     headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            _TOKEN = json.load(resp)["access_token"]
    return _TOKEN


def api(method, path, payload=None, timeout=120):
    data = None if payload is None else json.dumps(payload).encode()
    req = urllib.request.Request(BASE + path, data=data, method=method, headers={
        "Authorization": "Bearer " + _token(), "Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as err:
        raise SystemExit(f"GNS3 {method} {path}: HTTP {err.code}: {err.read()[:400]!r}")


def project_id(name_or_id):
    for p in api("GET", "/projects"):
        if name_or_id in (p["name"], p["project_id"]):
            return p["project_id"]
    raise SystemExit(f"project {name_or_id!r} not found")


def node(project, name):
    for n in api("GET", f"/projects/{project}/nodes"):
        if name in (n["name"], n["node_id"]):
            return n
    raise SystemExit(f"node {name!r} not found")


class Console:
    """Raw telnet console with IAC negotiation stripped."""

    def __init__(self, port, host="127.0.0.1"):
        self.sock = socket.create_connection((host, port), timeout=5)
        self.buf = b""

    def _read(self, timeout):
        self.sock.settimeout(timeout)
        try:
            chunk = self.sock.recv(65536)
        except socket.timeout:
            return b""
        out, i = bytearray(), 0
        while i < len(chunk):
            if chunk[i] == 255 and i + 1 < len(chunk):
                cmd = chunk[i + 1]
                if cmd in (251, 252, 253, 254) and i + 2 < len(chunk):
                    opt = chunk[i + 2]
                    reply = {253: 252, 251: 254}.get(cmd)  # refuse DO/WILL
                    if reply:
                        self.sock.sendall(bytes([255, reply, opt]))
                    i += 3
                    continue
                i += 2
                continue
            out.append(chunk[i])
            i += 1
        return bytes(out)

    def send(self, text):
        self.sock.sendall(text.encode())

    def expect(self, pattern, timeout=60):
        rx = re.compile(pattern.encode(), re.S)
        deadline = time.time() + timeout
        while time.time() < deadline:
            m = rx.search(self.buf)
            if m:
                out, self.buf = self.buf[:m.end()], self.buf[m.end():]
                return out.decode(errors="replace")
            self.buf += self._read(min(1.0, max(0.05, deadline - time.time())))
        raise TimeoutError(f"pattern {pattern!r} not seen; tail={self.buf[-600:].decode(errors='replace')!r}")

    def ios_enable(self, timeout=60):
        """Reach an IOS privileged prompt from any idle console state."""
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.send("\r")
            try:
                out = self.expect(r"(\[yes/no\]:\s*$|[\w.-]+>\s*$|[\w.-]+#\s*$)", 3)
            except TimeoutError:
                continue
            if "[yes/no]" in out:
                self.send("no\r")
            elif out.rstrip().endswith(">"):
                self.send("enable\r")
            elif out.rstrip().endswith("#"):
                self.send("terminal length 0\r")
                self.expect(r"#\s*$", 5)
                return
        raise TimeoutError("no IOS privileged prompt")

    def ios(self, command, timeout=30, prompt=r"[\w.-]+(\(config[^)]*\))?#\s*$"):
        self.send(command + "\r")
        return self.expect(prompt, timeout)

    def drain(self, seconds=1.0):
        end = time.time() + seconds
        while time.time() < end:
            self.buf += self._read(0.2)
        out, self.buf = self.buf, b""
        return out.decode(errors="replace")


def main(argv):
    if not argv:
        print(__doc__)
        return 2
    op, args = argv[0], argv[1:]
    if op == "project-create":
        existing = [p for p in api("GET", "/projects") if p["name"] == args[0]]
        if existing:
            print(existing[0]["project_id"])
            return 0
        print(api("POST", "/projects", {"name": args[0]})["project_id"])
    elif op == "node-from-template":
        pid = project_id(args[0])
        payload = {"x": 0, "y": 0, "name": args[2]}
        n = api("POST", f"/projects/{pid}/templates/{args[1]}", payload)
        if len(args) > 3:
            n = api("PUT", f"/projects/{pid}/nodes/{n['node_id']}", json.loads(args[3]))
        print(n["node_id"], n["name"], n.get("console"))
    elif op == "node-docker":
        pid = project_id(args[0])
        n = api("POST", f"/projects/{pid}/nodes", {
            "name": args[2], "node_type": "docker", "compute_id": "local", "console_type": "telnet",
            "properties": {"image": args[1], "adapters": int(args[3]) if len(args) > 3 else 1,
                           "console_type": "telnet"}})
        print(n["node_id"], n["name"], n.get("console"))
    elif op == "link":
        pid = project_id(args[0])
        a, b = node(pid, args[1]), node(pid, args[4])
        link = api("POST", f"/projects/{pid}/links", {"nodes": [
            {"node_id": a["node_id"], "adapter_number": int(args[2]), "port_number": int(args[3])},
            {"node_id": b["node_id"], "adapter_number": int(args[5]), "port_number": int(args[6])}]})
        print(link["link_id"])
    elif op in ("start", "stop", "reload"):
        pid = project_id(args[0])
        api("POST", f"/projects/{pid}/nodes/{node(pid, args[1])['node_id']}/{op}", {}, timeout=300)
        print(op, args[1], "ok")
    elif op == "nodes":
        pid = project_id(args[0])
        for n in api("GET", f"/projects/{pid}/nodes"):
            print(n["name"], n["node_type"], n["status"], n.get("console"), n["node_id"])
        for link in api("GET", f"/projects/{pid}/links"):
            print("link", link["link_id"], [(x["node_id"][:8], x["adapter_number"], x["port_number"]) for x in link["nodes"]],
                  "capturing" if link.get("capturing") else "")
    elif op == "expect":
        pid = project_id(args[0])
        con = Console(node(pid, args[1])["console"])
        if len(args) > 4:
            con.send(args[4].encode().decode("unicode_escape"))
        print(con.expect(args[2], float(args[3]) if len(args) > 3 else 60))
    elif op == "cmd":
        pid = project_id(args[0])
        con = Console(node(pid, args[1])["console"])
        prompt = args[3] if len(args) > 3 else r"[>#]\s*$"
        con.drain(0.5)
        con.send(args[2] + "\r")
        print(con.expect(prompt, float(args[4]) if len(args) > 4 else 30))
    elif op in ("capture-start", "capture-stop"):
        pid = project_id(args[0])
        path = f"/projects/{pid}/links/{args[1]}/" + ("capture/start" if op == "capture-start" else "capture/stop")
        res = api("POST", path, {"capture_file_name": args[2], "data_link_type": "DLT_EN10MB"} if op == "capture-start" and len(args) > 2 else {})
        print(json.dumps(res) if res else "ok")
    else:
        print(__doc__)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
