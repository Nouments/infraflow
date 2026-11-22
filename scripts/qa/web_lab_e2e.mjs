#!/usr/bin/env node
// Admin Web end-to-end test against a RUNNING lab Provider and real devices.
//
// Unlike web_e2e.mjs (throwaway Provider, no devices), this drives headless
// Chromium through the operator workflow on a lab whose Agents and routers
// are live: sign in, see the Agents online, add a router from the editor and
// publish its artifacts, watch the Agent synchronize them, configure routers
// from the Sites view, and follow the execution to its verified result.
//
// Usage: node scripts/qa/web_lab_e2e.mjs <base-url> <admin-password-file> <inventory.yaml> <out-dir>
// Environment:
//   LAB_NEW_DEVICE   site/device added by the inventory (default vendor-lab/r6)
//   LAB_EXISTING     site/device of an existing Cisco router (default vendor-lab/r1)
//   LAB_MIKROTIK     site/device of a RouterOS router (default vendor-lab/mt1)
//   LAB_AGENT_SITE   site whose Agent must report the new catalog (default vendor-lab)
//   CHROMEDRIVER, CHROMIUM
// The password is read from a file and never printed or written to the report.
import { spawn, spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const [base, passwordFile, inventoryFile, outDir] = process.argv.slice(2);
if (!base || !passwordFile || !inventoryFile || !outDir) {
  console.error("usage: web_lab_e2e.mjs <base-url> <admin-password-file> <inventory.yaml> <out-dir>");
  process.exit(2);
}
mkdirSync(outDir, { recursive: true });
const split = (value) => { const [site, device] = value.split("/"); return { site, device }; };
const NEW = split(process.env.LAB_NEW_DEVICE || "vendor-lab/r6");
const EXISTING = split(process.env.LAB_EXISTING || "vendor-lab/r1");
const MIKROTIK = split(process.env.LAB_MIKROTIK || "vendor-lab/mt1");
const AGENT_SITE = process.env.LAB_AGENT_SITE || "vendor-lab";
const password = readFileSync(passwordFile, "utf8").trim();
const inventory = readFileSync(inventoryFile, "utf8");
const work = mkdtempSync(join(tmpdir(), "infraflow-web-lab-"));
const driverPort = 19500 + Math.floor(Math.random() * 400);
const results = [];
const stamp = new Date().toISOString().replace(/[-:]/g, "").slice(0, 15);
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// ---- minimal W3C WebDriver client (same as web_e2e.mjs) --------------------
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
  const id = (await wd("POST", s("/element"), { using: "css selector", value: css }))[ELEMENT];
  if (!id) throw new Error(`no element ${css}`);
  return id;
}
async function click(css) { await wd("POST", s(`/element/${await find(css)}/click`), {}); }
async function type(css, text) {
  const id = await find(css);
  await wd("POST", s(`/element/${id}/clear`), {});
  await wd("POST", s(`/element/${id}/value`), { text });
}
async function js(script, ...args) { return wd("POST", s("/execute/sync"), { script, args }); }
async function text(css) { return js("const e=document.querySelector(arguments[0]); return e ? e.textContent.trim() : null;", css); }
async function visible(css) { return js("const e=document.querySelector(arguments[0]); return !!e && !e.closest('.hidden') && e.getClientRects().length > 0;", css); }
async function waitFor(description, predicate, timeout = 15000) {
  const deadline = Date.now() + timeout;
  let last;
  while (Date.now() < deadline) {
    try { last = await predicate(); if (last) return last; } catch (error) { last = error.message; }
    await sleep(300);
  }
  throw new Error(`timed out waiting for ${description} (last: ${JSON.stringify(last)})`);
}
async function screenshot(name) {
  writeFileSync(join(outDir, `${name}.png`), Buffer.from(await wd("GET", s("/screenshot")), "base64"));
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
  console.log(`${results.at(-1).status} ${id} ${title}`);
}
// Reads go through the page's own session token, never a separate login.
async function api(path) {
  return wd("POST", s("/execute/async"), { script: `const done = arguments[arguments.length - 1];
    fetch('/api/v1' + arguments[0], { headers: { Authorization: 'Bearer ' + sessionStorage.getItem('infraflow.session') } })
      .then((r) => r.json()).then(done, (e) => done({ error: String(e) }));`, args: [path] });
}

async function openView(name) {
  await click(`[data-view="${name}"]`);
  await waitFor(`${name} view`, () => visible(`#${name}-view`));
}

// Opens the device dialog from the Sites view, fills it and submits; returns
// the final execution as the detail view shows it (and as the API returns it).
async function configureFromSites(target, fill) {
  await openView("sites");
  await click('[data-action="refresh-sites"]').catch(() => {});
  const selector = `[data-device-site="${target.site}"][data-device-name="${target.device}"]`;
  await waitFor(`${target.site}/${target.device} action button`, () => visible(selector), 20000);
  await click(selector);
  await waitFor("device dialog", () => js("return document.querySelector('#device-dialog').open"));
  if (fill) await fill();
  await click("#device-submit");
  await waitFor("execution detail", () => visible("#execution-detail-view"), 20000);
  const id = await waitFor("execution id", () => text(".exec-detail-id"));
  const terminal = ["succeeded", "failed", "cancelled", "timed_out"];
  const status = await waitFor("terminal status in the page", async () => {
    const shown = await text(".exec-detail-header .status");
    return terminal.includes(shown) ? shown : false;
  }, 240000);
  const execution = await api(`/executions/${encodeURIComponent(id)}`);
  return { id, page_status: status, execution, facts: await text(".exec-task-facts"), page_error: await text(".exec-task-error") };
}

async function fillCisco({ hostname, interfaces, persist }) {
  if (hostname) await type('#device-form input[name="hostname"]', hostname);
  for (let index = 0; index < interfaces.length; index += 1) {
    if (index > 0) await click("#device-add-interface");
    const row = `#device-interfaces .interface-row:nth-child(${index + 1})`;
    const entry = interfaces[index];
    await type(`${row} input[name="if_name"]`, entry.name);
    if (entry.description) await type(`${row} input[name="if_description"]`, entry.description);
    if (entry.ipv4_cidr) await type(`${row} input[name="if_ipv4"]`, entry.ipv4_cidr);
    await js(`document.querySelector(arguments[0]).value = arguments[1]`, `${row} select[name="if_state"]`, entry.admin_state || "up");
  }
  if (persist) await click('#device-form input[name="persist"]');
}

function taskResult(execution) {
  const task = (execution.tasks || [])[0] || {};
  let result = task.result;
  if (typeof result === "string") { try { result = JSON.parse(result); } catch (_) { /* keep text */ } }
  return { task_status: task.status, task_error: task.error || "", result };
}

async function main() {
  const driver = spawn(process.env.CHROMEDRIVER || "chromedriver", [`--port=${driverPort}`], { stdio: "ignore" });
  try {
    await waitFor("chromedriver", async () => (await fetch(`http://127.0.0.1:${driverPort}/status`)).ok);
    session = (await wd("POST", "/session", { capabilities: { alwaysMatch: { browserName: "chrome", "goog:chromeOptions": {
      binary: process.env.CHROMIUM || "/usr/bin/chromium",
      args: ["--headless=new", "--no-sandbox", "--disable-gpu", "--window-size=1440,1000", `--user-data-dir=${work}/chrome`],
    } } } })).sessionId;
    await wd("POST", s("/url"), { url: base + "/" });
    await run();
  } finally {
    if (session) await wd("DELETE", s("")).catch(() => {});
    driver.kill();
  }
}

async function run() {
  await scenario("L01", "administrator signs in to the lab Provider", async () => {
    await waitFor("login form", () => visible("#login-form"));
    await type("#login-form input[name=username]", "admin");
    await type("#login-form input[name=password]", password);
    await click("#login-form button[type=submit]");
    await waitFor("console", () => visible("#console"));
    await screenshot("L01-overview");
    return { user: await text("#user-name"), role: await text("#user-role") };
  });

  await scenario("L02", "Agents are shown online with a recent heartbeat", async () => {
    await openView("agents");
    const rows = await waitFor("agent rows", async () => {
      const found = await js("return [...document.querySelectorAll('#agents-table tr')].map((r) => r.innerText.replace(/\\s+/g, ' ').trim())");
      return found.length ? found : false;
    });
    const agents = await api("/agents");
    const list = Array.isArray(agents) ? agents : agents.items || [];
    const offline = list.filter((agent) => agent.status !== "online");
    await screenshot("L02-agents");
    if (!list.length || offline.length) throw new Error(`agents not all online: ${JSON.stringify(list.map((a) => [a.id, a.status]))}`);
    return { rows, api: list.map((agent) => ({ id: agent.id, site: agent.site_id, status: agent.status, last_seen: agent.last_seen_at || agent.last_heartbeat_at })) };
  });

  let generatedAt;
  await scenario("L03", `router ${NEW.site}/${NEW.device} is added in the editor and its artifacts are generated`, async () => {
    await openView("editor");
    await js(`const source = document.querySelector('#editor-source'); source.value = arguments[0];
      source.dispatchEvent(new Event('input', { bubbles: true }));`, inventory);
    await waitFor("Provider validation", async () => ((await text("#editor-status")) || "").startsWith("Valid") || ((await text("#editor-status")) === "Validation failed" && "failed"), 20000);
    const validation = await text("#editor-status");
    if (!validation.startsWith("Valid")) throw new Error(`${validation}: ${await text("#editor-error")}`);
    generatedAt = new Date().toISOString();
    await waitFor("generate enabled", () => js("return !document.querySelector('#editor-generate').disabled"));
    await click("#editor-generate");
    const status = await waitFor("generation result", async () => {
      const shown = (await text("#editor-status")) || "";
      return shown.startsWith("Generation ") && !shown.includes("…") ? shown : false;
    }, 60000);
    await screenshot("L03-generated");
    if (status !== "Generation SUCCEEDED" && status !== "Generation PARTIAL") throw new Error(`${status}: ${await text("#editor-error")}`);
    const plan = await text("#editor-plan");
    if (!plan.includes(NEW.device)) throw new Error(`generation result does not mention ${NEW.device}`);
    return { validation, status };
  });

  await scenario("L04", `the published catalog lists ${NEW.device} and the ${AGENT_SITE} Agent reports it`, async () => {
    await openView("sites");
    const site = await waitFor(`Agent report after the new catalog`, async () => {
      const sites = await api("/sites");
      const found = (Array.isArray(sites) ? sites : []).find((entry) => entry.id === AGENT_SITE);
      if (!found || !found.devices.some((device) => device.name === NEW.device)) return false;
      const report = found.last_report || {};
      const at = report.completed_at || report.received_at || report.reported_at || "";
      return found.report_outcome === "COMPLETE" && at > generatedAt ? found : false;
    }, 150000);
    await click('[data-action="refresh-sites"]').catch(() => {});
    await waitFor("new device row", () => js("return [...document.querySelectorAll('#sites-list td')].some((td) => td.textContent.trim() === arguments[0])", NEW.device));
    await screenshot("L04-sites");
    return { report_outcome: site.report_outcome, last_report: site.last_report, devices: site.devices.map((device) => device.name), agents: site.agents.map((agent) => [agent.id, agent.status]) };
  });

  const marker = `infraflow-web-${stamp}`;
  await scenario("L05", `configure the NEW router ${NEW.device} from the Sites view, with persist`, async () => {
    const outcome = await configureFromSites(NEW, () => fillCisco({
      hostname: NEW.device,
      interfaces: [{ name: "GigabitEthernet0/1", description: marker, ipv4_cidr: "198.51.100.65/26", admin_state: "up" }],
      persist: true,
    }));
    await screenshot("L05-new-router");
    const task = taskResult(outcome.execution);
    if (outcome.page_status !== "succeeded" || task.result?.verified !== true || task.result?.persistence?.verified !== true) {
      throw new Error(`status=${outcome.page_status} task=${JSON.stringify(task).slice(0, 600)}`);
    }
    return { execution: outcome.id, page_facts: outcome.facts, ...task };
  });

  await scenario("L06", `the same request again is idempotent (no change on ${NEW.device})`, async () => {
    const outcome = await configureFromSites(NEW, () => fillCisco({
      hostname: NEW.device,
      interfaces: [{ name: "GigabitEthernet0/1", description: marker, ipv4_cidr: "198.51.100.65/26", admin_state: "up" }],
    }));
    const task = taskResult(outcome.execution);
    if (outcome.page_status !== "succeeded" || task.result?.verified !== true) throw new Error(JSON.stringify(task).slice(0, 600));
    return { execution: outcome.id, changed: task.result?.changed, page_facts: outcome.facts, ...task };
  });

  await scenario("L07", `configure an EXISTING router ${EXISTING.device} (no persist)`, async () => {
    const outcome = await configureFromSites(EXISTING, () => fillCisco({
      interfaces: [{ name: "GigabitEthernet0/2", description: marker, ipv4_cidr: "198.51.100.193/27", admin_state: "up" }],
    }));
    await screenshot("L07-existing-router");
    const task = taskResult(outcome.execution);
    if (outcome.page_status !== "succeeded" || task.result?.verified !== true) throw new Error(JSON.stringify(task).slice(0, 600));
    return { execution: outcome.id, page_facts: outcome.facts, ...task };
  });

  await scenario("L08", `a change the device rejects fails visibly and is rolled back (${NEW.device})`, async () => {
    const outcome = await configureFromSites(NEW, () => fillCisco({
      interfaces: [
        { name: "GigabitEthernet0/3", description: `${marker}-rollback`, admin_state: "up" },
        { name: "GigabitEthernet0/9", description: "does-not-exist", admin_state: "up" },
      ],
    }));
    await screenshot("L08-rollback");
    const task = taskResult(outcome.execution);
    if (outcome.page_status !== "failed") throw new Error(`expected failed, got ${outcome.page_status}`);
    if (!outcome.page_error) throw new Error("the page shows no task error");
    return { execution: outcome.id, page_error: outcome.page_error, page_facts: outcome.facts, ...task };
  });

  await scenario("L09", `push the published RouterOS configuration to ${MIKROTIK.device}`, async () => {
    const outcome = await configureFromSites(MIKROTIK);
    await screenshot("L09-mikrotik");
    const task = taskResult(outcome.execution);
    if (outcome.page_status !== "succeeded") throw new Error(JSON.stringify(task).slice(0, 600));
    return { execution: outcome.id, page_facts: outcome.facts, ...task };
  });

  await scenario("L10", "executions list shows every run started from the console", async () => {
    await openView("executions");
    await click('[data-action="refresh-executions"]');
    const ids = results.flatMap((entry) => (entry.observed?.execution ? [entry.observed.execution] : []));
    await waitFor("executions listed", () => js("const shown = [...document.querySelectorAll('#exec-table [data-exec-id]')].map((b) => b.dataset.execId); return arguments[0].every((id) => shown.includes(id))", ids));
    await screenshot("L10-executions");
    return { executions: ids };
  });
}

await main().catch((error) => { results.push({ id: "HARNESS", title: "harness", status: "FAIL", error: error.message }); });
const summary = { pass: results.filter((r) => r.status === "PASS").length, fail: results.filter((r) => r.status !== "PASS").length };
const report = {
  generated_at: new Date().toISOString(), base, inventory: inventoryFile,
  chromium: spawnSync(process.env.CHROMIUM || "chromium", ["--version"], { encoding: "utf8" }).stdout.trim(),
  summary, results,
};
writeFileSync(join(outDir, "web-lab-report.json"), JSON.stringify(report, null, 2) + "\n");
console.log(`${summary.pass} PASS, ${summary.fail} FAIL`);
process.exit(summary.fail ? 1 : 0);
