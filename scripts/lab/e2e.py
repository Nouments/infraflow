#!/usr/bin/env python3
"""Reusable InfraFlow GNS3 end-to-end lab driver.

External lab tooling: it prepares the isolated lab host node (a Docker node in
a GNS3 project), runs the InfraFlow Provider and Agents inside it, drives the
Provider API, and records machine-readable results. It never deletes GNS3
projects or nodes it did not create.

Secrets (Agent tokens, device passwords) are generated per lab, stored only in
LAB_STATE (default: a 0600 file outside the repository) and inside the lab
container in /lab/secrets.env (0600). They are never printed.

Environment: GNS3_USER, GNS3_PASSWORD (see gns3lab.py), LAB_PROJECT,
LAB_CONTAINER, LAB_STATE, LAB_BIN (directory with static infraflow binaries).

Usage:
  e2e.py setup <inputs.yaml>       copy binaries, address the host, write configs, generate
  e2e.py provider-start|provider-stop
  e2e.py agent-run <agent-id>      one synchronization run (exit code of the Agent)
  e2e.py services <site>           start serve-dhcp and serve-tftp for a site
  e2e.py services-stop
  e2e.py secret <name> [length]    create (once) and print nothing; value goes to secrets.env
  e2e.py login                     check API login
  e2e.py exec <json-file|-> [idempotency-key]   submit an execution and wait; prints JSON
  e2e.py known-host <ip> <keyfile> add a verified host key line to the Provider known_hosts
  e2e.py sh <command>              run a shell command in the lab container
"""
import json
import os
import secrets
import subprocess
import sys
import time

CONTAINER = os.environ.get("LAB_CONTAINER", "")
STATE = os.environ.get("LAB_STATE", os.path.expanduser("~/.cache/infraflow-lab/state.json"))
BIN = os.environ.get("LAB_BIN", "")
API = "http://127.0.0.1:8080"


def sh(command, check=True, stdin=None, timeout=600):
    """Run a shell command inside the lab container."""
    proc = subprocess.run(["docker", "exec", "-i", CONTAINER, "sh", "-c", command],
                          input=stdin, capture_output=True, text=True, timeout=timeout)
    if check and proc.returncode != 0:
        raise SystemExit(f"lab command failed ({proc.returncode}): {command}\n{proc.stdout}{proc.stderr}")
    return proc


def load_state():
    try:
        with open(STATE) as handle:
            return json.load(handle)
    except FileNotFoundError:
        return {"secrets": {}}


def save_state(state):
    os.makedirs(os.path.dirname(STATE), exist_ok=True)
    fd = os.open(STATE + ".tmp", os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as handle:
        json.dump(state, handle)
    os.replace(STATE + ".tmp", STATE)


def secret(name, length=24, alphabet=None):
    state = load_state()
    if name not in state["secrets"]:
        alphabet = alphabet or "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
        state["secrets"][name] = "".join(secrets.choice(alphabet) for _ in range(length))
        save_state(state)
    push_secrets(state)
    return state["secrets"][name]


def push_secrets(state=None):
    state = state or load_state()
    body = "".join(f"export {k}='{v}'\n" for k, v in sorted(state["secrets"].items()))
    sh("umask 077; mkdir -p /lab; cat > /lab/secrets.env", stdin=body)


AGENTS = {
    "agent-iosv": "iosv-lab",
    "agent-vendor": "vendor-lab",
}


def setup(inputs):
    if not BIN:
        raise SystemExit("LAB_BIN must point at the static infraflow binaries")
    sh("ip link set eth0 up; ip addr replace 192.0.2.2/24 dev eth0; ip addr replace 203.0.113.1/32 dev lo; mkdir -p /lab/pdata /lab/log")
    for binary in ("infraflow-provider", "infraflow-agent"):
        subprocess.run(["docker", "cp", os.path.join(BIN, binary), f"{CONTAINER}:/usr/local/bin/{binary}"], check=True)
    subprocess.run(["docker", "cp", inputs, f"{CONTAINER}:/lab/inputs.yaml"], check=True)
    secret("INFRAFLOW_AGENT_TOKEN", 40)
    for agent in AGENTS:
        secret("TOKEN_" + agent.upper().replace("-", "_"), 40)
    expires = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(time.time() + 7 * 86400))
    creds = "".join(
        f"  - agent_id: {agent}\n    site_id: {site}\n    token_env: TOKEN_{agent.upper().replace('-', '_')}\n    expires_at: {expires}\n"
        for agent, site in AGENTS.items())
    provider = f"""listen_address: 127.0.0.1:8443
api_listen_address: 127.0.0.1:8080
web_ui_enabled: true
artifact_directory: /lab/pdata/artifacts
database_path: /lab/pdata/users.sqlite3
token_env: INFRAFLOW_AGENT_TOKEN
admin_username: admin
admin_credential_file: /lab/pdata/.admin-password
admin_credential_script: /lab/pdata/get-admin-password.sh
agent_heartbeat_timeout_seconds: 30
known_hosts_file: /lab/pdata/known_hosts
ssh_legacy_sha1_targets: []
agent_credentials:
{creds}logging:
  directory: /lab/log/provider
"""
    sh("cat > /lab/provider.yaml", stdin=provider)
    for agent, site in AGENTS.items():
        sh(f"cat > /lab/{agent}.yaml", stdin=f"""agent:
  id: {agent}
  site_id: {site}
  heartbeat_interval_seconds: 10
  capabilities: [inventory, topology]
  state_directory: /lab/adata-{agent}
provider:
  address: 127.0.0.1:8443
  api_address: http://127.0.0.1:8080
  token_env: TOKEN_{agent.upper().replace('-', '_')}
  tls:
    enabled: false
""")
    sh("touch /lab/pdata/known_hosts; chmod 600 /lab/pdata/known_hosts")
    out = sh("infraflow-provider generate-all -f /lab/inputs.yaml -out /lab/pdata/artifacts 2>&1", check=False)
    print(out.stdout[-3000:])
    return out.returncode


def provider_start(extra_config=None):
    sh("pid=$(cat /lab/provider.pid 2>/dev/null) && kill $pid 2>/dev/null; sleep 1; true")
    sh(". /lab/secrets.env; nohup infraflow-provider serve -config /lab/provider.yaml >>/lab/log/provider.out 2>&1 & echo $! > /lab/provider.pid")
    for _ in range(40):
        if sh(f"curl -sf {API}/api/v1/health", check=False).returncode == 0:
            print("provider ready")
            return 0
        time.sleep(0.5)
    print(sh("tail -20 /lab/log/provider.out", check=False).stdout)
    return 1


def provider_stop():
    sh("pid=$(cat /lab/provider.pid 2>/dev/null) && kill $pid 2>/dev/null; rm -f /lab/provider.pid; true")
    return 0


def agent_run(agent):
    proc = sh(f". /lab/secrets.env; infraflow-agent run -config /lab/{agent}.yaml 2>&1", check=False, timeout=300)
    print(proc.stdout[-4000:])
    return proc.returncode


def services(site):
    base = f"/lab/adata-{[a for a, s in AGENTS.items() if s == site][0]}/{site}/bootstrap"
    sh(f"nohup infraflow-agent serve-dhcp -config {base}/dhcp/config.json -interface eth0 -listen 192.0.2.2 >>/lab/log/dhcp.log 2>&1 & echo $! > /lab/dhcp.pid")
    sh(f"nohup infraflow-agent serve-tftp -config {base}/tftp/config.json -root {base} -interface eth0 -listen 192.0.2.2 >>/lab/log/tftp.log 2>&1 & echo $! > /lab/tftp.pid")
    time.sleep(1)
    print(sh("tail -3 /lab/log/dhcp.log /lab/log/tftp.log", check=False).stdout)
    return 0


def services_stop():
    sh("for f in /lab/dhcp.pid /lab/tftp.pid; do kill $(cat $f 2>/dev/null) 2>/dev/null; rm -f $f; done; true")
    return 0


def api_token():
    password = sh("cat /lab/pdata/.admin-password").stdout.strip()
    body = json.dumps({"username": "admin", "password": password})
    out = sh(f"curl -sf -H 'Content-Type: application/json' --data-binary @- {API}/api/v1/auth/login", stdin=body)
    return json.loads(out.stdout)["token"]


def http(method, path, token, body=None, headers=None):
    """Call the Provider API from inside the lab host; returns (status, json)."""
    resp = "/tmp/resp-" + secrets.token_hex(8) + ".json"  # unique: callers may run concurrently
    args = ["curl", "-s", "-o", resp, "-w", "%{http_code}", "-X", method,
            "-H", f"Authorization: Bearer {token}", "-H", "Content-Type: application/json"]
    for key, value in (headers or {}).items():
        args += ["-H", f"{key}: {value}"]
    if body is not None:
        args += ["--data-binary", "@-"]
    args.append(API + path)
    proc = subprocess.run(["docker", "exec", "-i", CONTAINER] + args, input=body, capture_output=True, text=True)
    status = int(proc.stdout or 0)
    raw = sh(f"cat {resp}; rm -f {resp}", check=False).stdout
    try:
        return status, json.loads(raw)
    except ValueError:
        return status, raw


def submit_and_wait(request_body, key, timeout=180):
    token = api_token()
    status, created = http("POST", "/api/v1/executions", token, request_body, {"Idempotency-Key": key})
    result = {"submit_status": status, "submit": created}
    if status not in (200, 201, 202) or not isinstance(created, dict):
        return result
    execution_id = created.get("id") or created.get("execution", {}).get("id")
    deadline = time.time() + timeout
    while time.time() < deadline:
        status, current = http("GET", f"/api/v1/executions/{execution_id}", token)
        if isinstance(current, dict) and current.get("status") in ("succeeded", "failed", "cancelled"):
            result["final"] = current
            return result
        time.sleep(2)
    result["final"] = "timeout waiting for completion"
    return result


def main(argv):
    global CONTAINER
    if not CONTAINER:
        raise SystemExit("LAB_CONTAINER must name the lab host container")
    if len(argv) < 2:
        print(__doc__)
        return 2
    cmd, args = argv[1], argv[2:]
    if cmd == "setup":
        return setup(args[0])
    if cmd == "provider-start":
        return provider_start()
    if cmd == "provider-stop":
        return provider_stop()
    if cmd == "agent-run":
        return agent_run(args[0])
    if cmd == "services":
        return services(args[0])
    if cmd == "services-stop":
        return services_stop()
    if cmd == "secret":
        secret(args[0], int(args[1]) if len(args) > 1 else 20)
        return 0
    if cmd == "login":
        api_token()
        print("login ok")
        return 0
    if cmd == "exec":
        body = sys.stdin.read() if args[0] == "-" else open(args[0]).read()
        key = args[1] if len(args) > 1 else "lab-" + secrets.token_hex(12)
        print(json.dumps(submit_and_wait(body, key), indent=1))
        return 0
    if cmd == "known-host":
        sh("cat >> /lab/pdata/known_hosts", stdin=open(args[1]).read().strip() + "\n")
        return 0
    if cmd == "sh":
        proc = sh(" ".join(args), check=False)
        print(proc.stdout + proc.stderr, end="")
        return proc.returncode
    print(__doc__)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
