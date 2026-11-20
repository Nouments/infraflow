#!/usr/bin/env node
// Browser end-to-end tests of the Provider web console.
//
// Starts a throwaway Provider (fresh database and artifacts in a temporary
// directory, loopback only), drives headless Chromium through chromedriver's
// W3C WebDriver HTTP API, and writes a JSON and a text report. No test
// framework or npm dependency is needed.
//
// Usage: node scripts/qa/web_e2e.mjs <provider-binary> <inputs.yaml> <out-dir>
// Environment: CHROMEDRIVER (default chromedriver), CHROMIUM (default chromium).
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { randomBytes } from "node:crypto";

const [providerBinary, inputs, outDir] = process.argv.slice(2);
if (!providerBinary || !inputs || !outDir) {
  console.error("usage: web_e2e.mjs <provider-binary> <inputs.yaml> <out-dir>");
  process.exit(2);
}
mkdirSync(outDir, { recursive: true });
const work = mkdtempSync(join(tmpdir(), "infraflow-web-e2e-"));
const apiPort = 18000 + Math.floor(Math.random() * 1000);
const grpcPort = apiPort + 1000;
const driverPort = apiPort + 2000;
const base = `http://127.0.0.1:${apiPort}`;
const agentToken = randomBytes(24).toString("hex");
const results = [];
let provider;
let driver;

function writeConfig() {
  const config = `listen_address: 127.0.0.1:${grpcPort}
api_listen_address: 127.0.0.1:${apiPort}
web_ui_enabled: true
artifact_directory: ${work}/artifacts
database_path: ${work}/users.sqlite3
token_env: WEB_E2E_AGENT_TOKEN
admin_username: admin
admin_credential_file: ${work}/.admin-password
admin_credential_script: ${work}/get-admin-password.sh
known_hosts_file: ${work}/known_hosts
logging:
  directory: ${work}/log
`;
  writeFileSync(join(work, "provider.yaml"), config, { mode: 0o600 });
  writeFileSync(join(work, "known_hosts"), "", { mode: 0o600 });
  const generated = spawnSync(providerBinary, ["generate-all", "-f", inputs, "-out", join(work, "artifacts")], { encoding: "utf8" });
  if (generated.status !== 0) throw new Error("generate-all failed: " + generated.stdout + generated.stderr);
}

async function startProvider() {
  provider = spawn(providerBinary, ["serve", "-config", join(work, "provider.yaml")], {
    env: { ...process.env, WEB_E2E_AGENT_TOKEN: agentToken }, stdio: ["ignore", "pipe", "pipe"],
  });
  let output = "";
  provider.stdout.on("data", (chunk) => { output += chunk; });
  provider.stderr.on("data", (chunk) => { output += chunk; });
  for (let i = 0; i < 60; i += 1) {
    try { if ((await fetch(`${base}/api/v1/health`)).ok) return; } catch (_) { /* starting */ }
    await sleep(250);
  }
  throw new Error("Provider did not start: " + output.slice(-2000));
}

async function stopProvider() {
  if (!provider || provider.exitCode !== null) return;
  const exited = new Promise((resolve) => provider.once("exit", resolve));
  provider.kill("SIGTERM");
  await Promise.race([exited, sleep(5000)]);
}

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// ---- minimal W3C WebDriver client ------------------------------------------
let session;
async function wd(method, path, body) {
  const response = await fetch(`http://127.0.0.1:${driverPort}${path}`, {
    method, headers: { "Content-Type": "application/json" }, body: body === undefined ? undefined : JSON.stringify(body),
  });
  const payload = await response.json();
  if (!response.ok) throw new Error(`webdriver ${method} ${path}: ${JSON.stringify(payload.value).slice(0, 400)}`);
  return payload.value;
}
const s = (path) => `/session/${session}${path}`;
const ELEMENT = "element-6066-11e4-a52e-4f735466cecf";
async function find(css) {
  const found = await wd("POST", s("/element"), { using: "css selector", value: css });
  const id = found[ELEMENT];
  if (!id) throw new Error(`no element ${css}: ${JSON.stringify(found)}`);
  return id;
}
async function click(css) { await wd("POST", s(`/element/${await find(css)}/click`), {}); }
async function type(css, text) {
  const id = await find(css);
  await wd("POST", s(`/element/${id}/clear`), {});
  await wd("POST", s(`/element/${id}/value`), { text });
}
async function js(script, ...args) { return wd("POST", s("/execute/sync"), { script, args }); }
async function jsAsync(script, ...args) { return wd("POST", s("/execute/async"), { script, args }); }
async function text(css) { return js("const e=document.querySelector(arguments[0]); return e ? e.textContent.trim() : null;", css); }
async function visible(css) { return js("const e=document.querySelector(arguments[0]); return !!e && !e.closest('.hidden') && e.getClientRects().length > 0;", css); }
async function waitFor(description, predicate, timeout = 10000) {
  const deadline = Date.now() + timeout;
  let last;
  while (Date.now() < deadline) {
    try { last = await predicate(); if (last) return last; } catch (error) { last = error.message; }
    await sleep(200);
  }
  throw new Error(`timed out waiting for ${description} (last: ${JSON.stringify(last)})`);
}
async function screenshot(name) {
  const png = await wd("GET", s("/screenshot"));
  writeFileSync(join(outDir, `${name}.png`), Buffer.from(png, "base64"));
}

async function scenario(id, title, body) {
  const started = Date.now();
  try {
    const observed = await body();
    results.push({ id, title, status: "PASS", observed, duration_ms: Date.now() - started });
  } catch (error) {
    await screenshot(`${id}-failure`).catch(() => {});
    results.push({ id, title, status: "FAIL", error: error.message, duration_ms: Date.now() - started });
  }
}

async function login(username, password) {
  await waitFor("login form", () => visible("#login-form"));
  await type("#login-form input[name=username]", username);
  await type("#login-form input[name=password]", password);
  await click("#login-form button[type=submit]");
}

async function main() {
  writeConfig();
  await startProvider();
  const adminPassword = readFileSync(join(work, ".admin-password"), "utf8").trim();
  driver = spawn(process.env.CHROMEDRIVER || "chromedriver", [`--port=${driverPort}`], { stdio: "ignore" });
  await waitFor("chromedriver", async () => (await fetch(`http://127.0.0.1:${driverPort}/status`)).ok);
  session = (await wd("POST", "/session", { capabilities: { alwaysMatch: { browserName: "chrome", "goog:chromeOptions": {
    binary: process.env.CHROMIUM || "/usr/bin/chromium",
    args: ["--headless=new", "--no-sandbox", "--disable-gpu", "--window-size=1440,1000", `--user-data-dir=${work}/chrome`],
  } } } })).sessionId;
  await wd("POST", s("/url"), { url: base + "/" });

  await scenario("W01", "console loads and shows the sign-in form", async () => {
    await waitFor("login visible", () => visible("#login-form"));
    if (await visible("#console")) throw new Error("console visible before login");
    return { title: await js("return document.title") };
  });
  await scenario("W02", "wrong password is rejected with a visible error", async () => {
    await login("admin", "not-the-password");
    const message = await waitFor("login error", async () => (await text("#login-error")) || false);
    if (await visible("#console")) throw new Error("console opened with a wrong password");
    return { login_error: message };
  });
  await scenario("W03", "administrator signs in", async () => {
    await login("admin", adminPassword);
    await waitFor("console", () => visible("#console"));
    const user = await text("#user-name");
    const adminNav = await visible('[data-view="users"]');
    if (user !== "admin" || !adminNav) throw new Error(`user=${user} adminNav=${adminNav}`);
    await screenshot("W03-overview");
    return { user, role: await text("#user-role") };
  });
  await scenario("W04", "page refresh keeps the tab session", async () => {
    await wd("POST", s("/refresh"), {});
    await waitFor("console after refresh", () => visible("#console"));
    return { user: await text("#user-name") };
  });
  await scenario("W05", "Provider connection indicator reports reachable", async () => {
    const chip = await waitFor("reachable chip", async () => ((await text("#connection-text")) || "").includes("reachable") && (await text("#connection-text")));
    if (chip.includes("unreachable")) throw new Error(chip);
    return { connection: chip };
  });
  await scenario("W06", "create, disable and re-enable a user (no delete exists)", async () => {
    await click('[data-view="users"]');
    await waitFor("users view", () => visible("#user-form"));
    await type("#user-form input[name=username]", "qa-operator");
    await type("#user-form input[name=password]", "Qa-operator-pass-2026");
    await js("document.querySelector('#user-form select[name=role]').value='user'");
    await click("#user-form button[type=submit]");
    const row = () => js("return [...document.querySelectorAll('#users-table tr')].map(r=>r.textContent).find(t=>t.includes('qa-operator'))||null");
    await waitFor("user row", row);
    await js("[...document.querySelectorAll('#users-table tr')].find(r=>r.textContent.includes('qa-operator')).querySelector('button').click()");
    await waitFor("disabled", async () => ((await row()) || "").includes("disabled"));
    const disabledRow = await row();
    await js("[...document.querySelectorAll('#users-table tr')].find(r=>r.textContent.includes('qa-operator')).querySelector('button').click()");
    await waitFor("active again", async () => ((await row()) || "").includes("active"));
    return { after_disable: disabledRow, after_enable: await row() };
  });
  await scenario("W07", "editor: validation error is shown for invalid input", async () => {
    await click('[data-view="editor"]');
    await waitFor("editor", () => visible("#editor-source"));
    await js("const e=document.querySelector('#editor-source'); e.value='sites: [ {name: \"bad site\", devices: 3 }'; e.dispatchEvent(new Event('input'));");
    const error = await waitFor("editor error", async () => (await text("#editor-error")) || false, 8000);
    return { editor_error: error.slice(0, 200) };
  });
  await scenario("W08", "editor: example NOT_REQUIRED, routed lab devices refused, interface fixture GENERATED", async () => {
    await click("#editor-example");
    await waitFor("example preview ready", async () => !(await js("return document.querySelector('#editor-generate').disabled")), 10000);
    await click("#editor-generate");
    const exampleStatus = await waitFor("example generation", async () => { const t = await text("#editor-status"); return t && t.startsWith("Generation ") && t; }, 15000);
    // The built-in example declares no provisioning method, so its plan has
    // no tasks: NOT_REQUIRED is the correct answer for it.
    const generateFrom = async (source, previous) => {
      await js("const e=document.querySelector('#editor-source'); e.value=arguments[0]; e.dispatchEvent(new Event('input'));", source);
      await waitFor("preview ready", async () => !(await js("return document.querySelector('#editor-generate').disabled")), 15000);
      await click("#editor-generate");
      return waitFor("generation", async () => { const t = await text("#editor-status"); return t && t.startsWith("Generation ") && t !== previous && t; }, 20000);
    };
    // Lab devices with static routes are reported as unsupported by the web
    // path (never silently dropped); route-free devices still generate.
    const labStatus = await generateFrom(readFileSync(inputs, "utf8"), exampleStatus);
    const labPlan = (await text("#editor-plan")) || "";
    const status = await generateFrom(readFileSync(new URL("./fixtures/web-generate.yaml", import.meta.url), "utf8"), labStatus);
    await screenshot("W08-generation");
    const plan = (await text("#editor-plan")) || "";
    if (exampleStatus !== "Generation NOT_REQUIRED" || !["Generation UNSUPPORTED", "Generation PARTIAL"].includes(labStatus) || !labPlan.includes("static routes") ||
        status !== "Generation GENERATED" || !plan.includes("mt-web")) {
      throw new Error(`example=${exampleStatus} lab=${labStatus} fixture=${status} error=${await text("#editor-error")}`);
    }
    return { example_status: exampleStatus, lab_input_status: labStatus, fixture_status: status, plan_excerpt: plan.slice(0, 300) };
  });
  await scenario("W09", "editor: plan submission creates a job visible in Plans & jobs", async () => {
    await waitFor("submit enabled", async () => !(await js("return document.querySelector('#editor-submit').disabled")));
    await click("#editor-submit");
    await waitFor("jobs view", () => visible("#jobs-table"));
    const rows = await waitFor("job row", async () => (await js("return document.querySelectorAll('#jobs-table tr').length")) || false);
    return { job_rows: rows };
  });
  let executionId;
  await scenario("W10", "executions list, status filter and detail", async () => {
    const token = await js("return sessionStorage.getItem(Object.keys(sessionStorage)[0])");
    const response = await fetch(`${base}/api/v1/executions`, { method: "POST", headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json", "Idempotency-Key": "web-e2e-" + randomBytes(8).toString("hex") },
      body: JSON.stringify({ tasks: [{ id: "verify", type: "artifact.verify", parameters: { path: "missing/does-not-exist.cfg" } }] }) });
    executionId = (await response.json()).id;
    if (response.status !== 202) throw new Error(`execution submit HTTP ${response.status}`);
    await click('[data-view="executions"]');
    await waitFor("execution row", async () => (await text("#exec-table")) && (await text("#exec-table")).includes(executionId.slice(0, 12)), 15000);
    await waitFor("execution finished", async () => { await click('[data-exec-filter="failed"]'); await sleep(500); return ((await text("#exec-table")) || "").includes(executionId.slice(0, 12)); }, 15000);
    await click('[data-exec-filter="succeeded"]');
    await sleep(800);
    const hiddenBySucceededFilter = !((await text("#exec-table")) || "").includes(executionId.slice(0, 12));
    await click('[data-exec-filter=""]');
    await waitFor("row back", async () => ((await text("#exec-table")) || "").includes(executionId.slice(0, 12)));
    await js("[...document.querySelectorAll('#exec-table tr')].find(r=>r.textContent.includes(arguments[0])).querySelector('button').click()", executionId.slice(0, 12));
    const detail = await waitFor("detail", async () => { const t = await text("#exec-detail-content"); return t && t.includes("failed") && t; }, 10000);
    await screenshot("W10-execution-detail");
    if (!hiddenBySucceededFilter) throw new Error("succeeded filter still shows the failed execution");
    return { execution: executionId, filter_succeeded_hides_failed: hiddenBySucceededFilter, detail_excerpt: detail.slice(0, 240) };
  });
  await scenario("W11", "technical logs view loads and filters by level", async () => {
    await click('[data-view="logs"]');
    await waitFor("logs view", () => visible("#logs-apply"));
    await js("document.querySelector('#logs-level').value='ERROR'");
    await click("#logs-apply");
    await sleep(1500);
    const error = await text("#logs-error");
    if (error) throw new Error(error);
    const levels = await js("return [...document.querySelectorAll('#logs-table tr')].map(r=>r.textContent).filter(t=>/\\b(INFO|DEBUG|WARN)\\b/.test(t)).length");
    if (levels !== 0) throw new Error(`${levels} non-ERROR rows shown with level=ERROR`);
    return { rows: await js("return document.querySelectorAll('#logs-table tr').length"), state: await text("#logs-stream-status") };
  });
  await scenario("W12", "diagnostics form: required-field error and a Provider report", async () => {
    await click('[data-view="diagnostics"]');
    await waitFor("diagnostics", () => visible("#diagnostics-form"));
    await click("#diagnostics-form button[type=submit]");
    const required = await waitFor("required error", async () => (await text("#diagnostics-error")) || false);
    await type("#diagnostics-form input[name=site_id]", "vendor-lab");
    await click("#diagnostics-form button[type=submit]");
    const report = await waitFor("report", async () => { const t = await text("#diagnostics-result"); return t && !t.includes("Correlating") && t; }, 10000);
    return { required_error: required, report_excerpt: report.slice(0, 200) };
  });
  await scenario("W13", "server-side session revocation sends the browser back to sign-in", async () => {
    const token = await js("return sessionStorage.getItem(Object.keys(sessionStorage)[0])");
    const revoked = await fetch(`${base}/api/v1/auth/logout`, { method: "POST", headers: { Authorization: `Bearer ${token}` } });
    await click('[data-view="users"]');
    const message = await waitFor("expired message", async () => (await visible("#login-form")) && (await text("#login-error")), 15000);
    if (!/expired/i.test(message)) throw new Error(message);
    return { revoke_http: revoked.status, login_message: message };
  });
  await scenario("W14", "non-admin user: admin views hidden and admin API refused", async () => {
    await login("qa-operator", "Qa-operator-pass-2026");
    await waitFor("console", () => visible("#console"));
    const adminNavVisible = await visible('[data-view="users"]');
    const status = await jsAsync("const done=arguments[arguments.length-1]; const t=sessionStorage.getItem(Object.keys(sessionStorage)[0]); fetch('/api/v1/users',{headers:{Authorization:'Bearer '+t}}).then(r=>done(r.status)).catch(e=>done(String(e)));");
    if (adminNavVisible || status !== 403) throw new Error(`adminNav=${adminNavVisible} /users=${status}`);
    return { role: await text("#user-role"), admin_nav_visible: adminNavVisible, users_api_status: status };
  });
  await scenario("W15", "sign out clears the session; refresh stays signed out", async () => {
    await click("#logout");
    await waitFor("login", () => visible("#login-form"));
    const stored = await js("return Object.keys(sessionStorage).map(k=>sessionStorage.getItem(k)).filter(Boolean).length");
    await wd("POST", s("/refresh"), {});
    await waitFor("still login", () => visible("#login-form"));
    if (stored !== 0 || (await visible("#console"))) throw new Error(`stored tokens=${stored}`);
    return { stored_tokens: stored };
  });
  await scenario("W16", "Provider unavailable is shown, and recovery is detected", async () => {
    await login("admin", adminPassword);
    await waitFor("console", () => visible("#console"));
    await stopProvider();
    const down = await waitFor("unreachable chip", async () => { const t = await text("#connection-text"); return t && t.includes("unreachable") && t; }, 25000);
    await screenshot("W16-provider-unreachable");
    await startProvider();
    const up = await waitFor("reachable again", async () => { const t = await text("#connection-text"); return t && !t.includes("unreachable") && t.includes("reachable") && t; }, 25000);
    const signedIn = await visible("#console");
    return { while_down: down, after_restart: up, still_signed_in_after_restart: signedIn };
  });
}

let exitCode = 0;
try {
  await main();
} catch (error) {
  results.push({ id: "SETUP", title: "harness setup", status: "FAIL", error: error.message });
} finally {
  if (session) await wd("DELETE", `/session/${session}`).catch(() => {});
  driver?.kill();
  await stopProvider();
  rmSync(work, { recursive: true, force: true });
}
const summary = { pass: results.filter((r) => r.status === "PASS").length, fail: results.filter((r) => r.status !== "PASS").length };
const report = { generated_at: new Date().toISOString(), chromium: spawnSync(process.env.CHROMIUM || "chromium", ["--version"], { encoding: "utf8" }).stdout.trim(), summary, results };
writeFileSync(join(outDir, "web-e2e.json"), JSON.stringify(report, null, 2) + "\n");
const lines = results.map((r) => `${r.status.padEnd(4)} ${r.id} ${r.title}${r.error ? "\n     " + r.error : ""}`);
writeFileSync(join(outDir, "web-e2e.txt"), `${report.chromium}\n${lines.join("\n")}\nPASS ${summary.pass} FAIL ${summary.fail}\n`);
console.log(lines.join("\n"));
console.log(`PASS ${summary.pass} FAIL ${summary.fail}`);
if (summary.fail) exitCode = 1;
process.exit(exitCode);
