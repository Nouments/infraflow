(() => {
  const token = { value: "" };
  let currentUser = null;
  let jobs = [];
  let agents = [];
  let events = [];
  let technicalLogs = [];
  let technicalLogsStream = null;
  let editorConfig = { sites: [] };
  let selectedSiteIndex = 0;
  let selectedResource = null;
  let previewSource = "";
  let editorInitialized = false;
  let autoPreviewTimer = null;
  const topologyLayouts = new Map();
  let topologyDrag = null;
  const $ = (selector) => document.querySelector(selector);
  const SVG_NS = "http://www.w3.org/2000/svg";
  const exampleInfrastructure = `sites:
  - name: lab
    bootstrap:
      network: 192.168.100.0/24
    services:
      dhcp: true
      tftp: true
      pxe: true
    devices:
      - name: R1
        role: router
        vendor: cisco
        model: csr1000v
        management:
          ipv4: 192.168.100.10
      - name: SW1
        role: switch
        vendor: cisco
        model: ios-xe
        management:
          ipv4: 192.168.100.11
    links:
      - a: R1:Gi1
        b: SW1:Gi1
        network: 10.0.0.0/30
`;

  function node(tag, className, text) {
    const element = document.createElement(tag);
    if (className) element.className = className;
    if (text !== undefined) element.textContent = text;
    return element;
  }

  function showNotice(message, isError = false) {
    const notice = $("#notice");
    notice.textContent = message;
    notice.classList.toggle("error", isError);
    notice.classList.remove("hidden");
    window.clearTimeout(showNotice.timer);
    showNotice.timer = window.setTimeout(() => notice.classList.add("hidden"), 5500);
  }

  async function request(path, options = {}) {
    const headers = new Headers(options.headers || {});
    if (token.value) headers.set("Authorization", `Bearer ${token.value}`);
    if (options.body) headers.set("Content-Type", "application/json");
    const response = await fetch(`/api/v1${path}`, { ...options, headers });
    if (response.status === 204) return null;
    const result = await response.json().catch(() => ({}));
    if (response.status === 401 && path !== "/auth/login") {
      token.value = "";
      currentUser = null;
      showLogin("Your session expired. Please sign in again.");
    }
    if (!response.ok) {
      const error = new Error(result.error || `Request failed (${response.status})`);
      error.payload = result;
      throw error;
    }
    return result;
  }

  function showLogin(message = "") {
    $("#console").classList.add("hidden");
    $("#login").classList.remove("hidden");
    $("#login-error").textContent = message;
    $("#login-form").elements.password.value = "";
  }

  function showConsole(user) {
    currentUser = user;
    $("#login").classList.add("hidden");
    $("#console").classList.remove("hidden");
    $("#user-name").textContent = user.username;
    $("#user-role").textContent = user.role;
    $("#user-initial").textContent = user.username.slice(0, 1).toUpperCase();
    document.querySelectorAll(".admin-only").forEach((item) => item.classList.toggle("hidden", user.role !== "admin"));
    selectView("overview");
    refreshData();
  }

  function selectView(name) {
    const labels = {
      overview: "Infrastructure overview",
      editor: "Infrastructure editor",
      jobs: "Plans & jobs",
      agents: "Registered agents",
      activity: "Audit trail",
      logs: "Technical logs",
      users: "Access management",
    };
    stopTechnicalLogsStream();
    document.querySelectorAll(".view").forEach((view) => view.classList.add("hidden"));
    $(`#${name}-view`)?.classList.remove("hidden");
    document.querySelectorAll(".nav-item").forEach((item) => item.classList.toggle("active", item.dataset.view === name));
    $("#page-title").textContent = labels[name] || labels.overview;
    $("#view-label").textContent = name.toUpperCase();
    if (name === "editor") {
      renderEditor();
      if (!editorInitialized) {
        editorInitialized = true;
        loadExampleInfrastructure();
      }
    }
    if (name === "agents") loadAgents();
    if (name === "activity") loadEvents();
    if (name === "logs") loadTechnicalLogs();
    if (name === "users") loadUsers();
  }

  function stopTechnicalLogsStream() {
    if (technicalLogsStream) {
      technicalLogsStream.abort();
      technicalLogsStream = null;
    }
  }

  function logMessageFromEntry(entry) {
    return [entry.message, entry.error, entry.line, entry.event].find((value) => value && String(value).trim()) || "—";
  }

  function renderTechnicalLogs() {
    const table = $("#logs-table");
    table.replaceChildren();
    $("#logs-empty").classList.toggle("hidden", technicalLogs.length > 0);
    technicalLogs.forEach((entry) => {
      const row = node("tr");
      row.append(
        node("td", "mono", entry.id || "—"),
        node("td", "mono", entry.timestamp ? formatDate(entry.timestamp) : "—"),
        node("td", "", statusTag(entry.level || "info")),
        node("td", "mono", entry.service || "—"),
        node("td", "mono", entry.run_id || "—"),
        node("td", "mono", logMessageFromEntry(entry)),
      );
      table.append(row);
    });
  }

  async function loadTechnicalLogs() {
    if (currentUser.role !== "admin") return;
    const params = new URLSearchParams();
    const query = $("#logs-query")?.value?.trim() || "";
    const runID = $("#logs-run-id")?.value?.trim() || "";
    const level = $("#logs-level")?.value || "";
    if (query) params.set("q", query);
    if (runID) params.set("run_id", runID);
    if (level) params.set("level", level);
    params.set("limit", "100");
    try {
      const result = await request(`/logs?${params.toString()}`);
      technicalLogs = Array.isArray(result.events) ? result.events : [];
      renderTechnicalLogs();
      startTechnicalLogsStream();
    } catch (error) {
      showNotice(error.message, true);
    }
  }

  async function startTechnicalLogsStream() {
    if (currentUser.role !== "admin") return;
    stopTechnicalLogsStream();
    const params = new URLSearchParams();
    const query = $("#logs-query")?.value?.trim() || "";
    const runID = $("#logs-run-id")?.value?.trim() || "";
    const level = $("#logs-level")?.value || "";
    if (query) params.set("q", query);
    if (runID) params.set("run_id", runID);
    if (level) params.set("level", level);
    const controller = new AbortController();
    technicalLogsStream = controller;
    try {
      const response = await fetch(`/api/v1/logs/stream?${params.toString()}`, { headers: { Authorization: `Bearer ${token.value}` }, signal: controller.signal });
      if (!response.ok) throw new Error(`Stream unavailable (${response.status})`);
      const reader = response.body?.getReader();
      if (!reader) throw new Error("Event stream reader is unavailable");
      const decoder = new TextDecoder();
      let buffer = "";
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const chunks = buffer.split("\n\n");
        buffer = chunks.pop() || "";
        chunks.forEach((chunk) => {
          const dataLine = chunk.split("\n").find((line) => line.startsWith("data: "));
          if (!dataLine) return;
          const raw = dataLine.slice(6).trim();
          if (!raw || raw === ": keep-alive") return;
          try {
            const event = JSON.parse(raw);
            if (!event.id) return;
            technicalLogs = [event, ...technicalLogs.filter((current) => current.id !== event.id)].slice(0, 100);
            renderTechnicalLogs();
          } catch (_) { /* ignore malformed SSE frames */ }
        });
      }
    } catch (error) {
      if (error.name !== "AbortError") showNotice(error.message, true);
    } finally {
      if (technicalLogsStream === controller) technicalLogsStream = null;
    }
  }

  function formatDate(value) {
    if (!value) return "—";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "—";
    return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
  }

  function yamlScalar(value) {
    if (typeof value === "string") return JSON.stringify(value);
    if (value === null) return "null";
    return String(value);
  }

  function yamlValue(value, indentation = 0) {
    const padding = " ".repeat(indentation);
    if (Array.isArray(value)) {
      if (!value.length) return `${padding}[]`;
      return value.map((item) => {
        if (item && typeof item === "object") {
          const lines = yamlValue(item, indentation + 2).split("\n");
          return `${padding}- ${lines[0].slice(indentation + 2)}${lines.length > 1 ? `\n${lines.slice(1).join("\n")}` : ""}`;
        }
        return `${padding}- ${yamlScalar(item)}`;
      }).join("\n");
    }
    if (value && typeof value === "object") {
      const entries = Object.entries(value).filter(([, child]) => child !== undefined);
      if (!entries.length) return `${padding}{}`;
      return entries.map(([key, child]) => {
        const safeKey = /^[A-Za-z_][A-Za-z0-9_-]*$/.test(key) ? key : JSON.stringify(key);
        const complex = child && typeof child === "object";
        if (complex && (Array.isArray(child) ? child.length : Object.keys(child).length)) {
          return `${padding}${safeKey}:\n${yamlValue(child, indentation + 2)}`;
        }
        return `${padding}${safeKey}: ${complex ? (Array.isArray(child) ? "[]" : "{}") : yamlScalar(child)}`;
      }).join("\n");
    }
    return `${padding}${yamlScalar(value)}`;
  }

  function escapeHTML(value) {
    return String(value).replace(/[&<>"']/g, (character) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[character]);
  }

  function yamlCommentOffset(line) {
    let quote = "";
    for (let index = 0; index < line.length; index += 1) {
      const character = line[index];
      if (quote === '"' && character === "\\") { index += 1; continue; }
      if (quote === "'" && character === "'" && line[index + 1] === "'") { index += 1; continue; }
      if (quote && character === quote) { quote = ""; continue; }
      if (!quote && (character === '"' || character === "'")) { quote = character; continue; }
      if (!quote && character === "#" && (index === 0 || /\s/.test(line[index - 1]))) return index;
    }
    return line.length;
  }

  function yamlKeyOffset(line) {
    let quote = "";
    for (let index = 0; index < line.length; index += 1) {
      const character = line[index];
      if (quote === '"' && character === "\\") { index += 1; continue; }
      if (quote === "'" && character === "'" && line[index + 1] === "'") { index += 1; continue; }
      if (quote && character === quote) { quote = ""; continue; }
      if (!quote && (character === '"' || character === "'")) { quote = character; continue; }
      if (!quote && character === ":" && (index === line.length - 1 || /\s/.test(line[index + 1]))) return index;
    }
    return -1;
  }

  function highlightYamlValue(value) {
    const tokens = /"(?:\\.|[^"\\])*"|'(?:''|[^'])*'|\b(?:true|false|null|yes|no|on|off)\b|-?\d+(?:\.\d+)?(?:\/\d+)?|[{}\[\],]|[^\s{}\[\],]+/gi;
    let output = "";
    let offset = 0;
    for (const match of value.matchAll(tokens)) {
      output += escapeHTML(value.slice(offset, match.index));
      const token = match[0];
      const className = /^['"]/.test(token) ? "yaml-string"
        : /^(true|false|null|yes|no|on|off)$/i.test(token) ? "yaml-keyword"
          : /^-?\d/.test(token) ? "yaml-number"
            : /^[{}\[\],]$/.test(token) ? "yaml-punctuation" : "yaml-string";
      output += `<span class="${className}">${escapeHTML(token)}</span>`;
      offset = match.index + token.length;
    }
    return output + escapeHTML(value.slice(offset));
  }

  function highlightYamlLine(line) {
    const commentOffset = yamlCommentOffset(line);
    const content = line.slice(0, commentOffset);
    const comment = line.slice(commentOffset);
    const keyOffset = yamlKeyOffset(content);
    let output;
    if (keyOffset >= 0) {
      const prefix = content.slice(0, keyOffset);
      const keyMatch = prefix.match(/^(\s*(?:-\s+)?)(.*?)(\s*)$/);
      output = `${escapeHTML(keyMatch[1])}<span class="yaml-key">${escapeHTML(keyMatch[2])}</span>${escapeHTML(keyMatch[3])}<span class="yaml-punctuation">:</span>${highlightYamlValue(content.slice(keyOffset + 1))}`;
    } else output = highlightYamlValue(content);
    if (comment) output += `<span class="yaml-comment">${escapeHTML(comment)}</span>`;
    return output;
  }

  function updateYamlHighlight() {
    const source = $("#editor-source");
    const highlight = $("#editor-highlight");
    highlight.innerHTML = `${source.value.split("\n").map(highlightYamlLine).join("\n")}\n`;
    highlight.scrollTop = source.scrollTop;
    highlight.scrollLeft = source.scrollLeft;
  }

  function currentEditorSite() {
    return editorConfig.sites[selectedSiteIndex] || null;
  }

  function invalidateEditorPreview() {
    previewSource = "";
    $("#editor-status").textContent = "Preview pending";
    $("#editor-source-state").textContent = "AUTO PREVIEW QUEUED";
    $("#editor-error").textContent = "";
    $("#editor-plan").classList.add("hidden");
    $("#editor-submit").disabled = true;
    $("#editor-generate").disabled = true;
  }

  function scheduleEditorPreview(delay = 650) {
    window.clearTimeout(autoPreviewTimer);
    autoPreviewTimer = window.setTimeout(() => { previewEditorPlan(); }, delay);
  }

  function syncEditorSource() {
    $("#editor-source").value = `${yamlValue(editorConfig).trim()}\n`;
    updateYamlHighlight();
    invalidateEditorPreview();
    renderEditor(false);
    scheduleEditorPreview();
  }

  function svgElement(tag, attributes = {}) {
    const element = document.createElementNS(SVG_NS, tag);
    Object.entries(attributes).forEach(([name, value]) => element.setAttribute(name, String(value)));
    return element;
  }

  function endpointDevice(value) {
    return String(value || "").split(":", 1)[0];
  }

  function topologySiteKey(site) {
    return `${selectedSiteIndex}:${site?.name || "site"}`;
  }

  function topologyDevicePosition(site, device, index) {
    const siteKey = topologySiteKey(site);
    if (!topologyLayouts.has(siteKey)) topologyLayouts.set(siteKey, new Map());
    const layout = topologyLayouts.get(siteKey);
    if (layout.has(device.name)) return layout.get(device.name);

    let seed = Array.from(`${siteKey}:${device.name}:${index}`).reduce((value, character) => value + character.charCodeAt(0), 2166136261) >>> 0;
    const random = () => {
      seed = (seed * 1664525 + 1013904223) >>> 0;
      return seed / 4294967296;
    };
    let position;
    for (let attempt = 0; attempt < 40; attempt += 1) {
      const candidate = { x: 110 + random() * 780, y: 65 + random() * 390 };
      if ([...layout.values()].every((other) => Math.abs(other.x - candidate.x) > 190 || Math.abs(other.y - candidate.y) > 88)) {
        position = candidate;
        break;
      }
    }
    if (!position) {
      const angle = index * 2.39996;
      const radius = 70 + index * 24;
      position = {
        x: Math.max(100, Math.min(900, 500 + Math.cos(angle) * radius)),
        y: Math.max(55, Math.min(465, 260 + Math.sin(angle) * radius * 0.48)),
      };
    }
    layout.set(device.name, position);
    return position;
  }

  function topologyPoint(event, canvas) {
    const point = canvas.createSVGPoint();
    point.x = event.clientX;
    point.y = event.clientY;
    return point.matrixTransform(canvas.getScreenCTM().inverse());
  }

  function renderTopology(site) {
    const canvas = $("#editor-topology");
    canvas.replaceChildren();
    const devices = site?.devices || [];
    const positions = new Map(devices.map((device, index) => [device.name, topologyDevicePosition(site, device, index)]));
    (site?.links || []).forEach((link, index) => {
      const from = positions.get(endpointDevice(link.a));
      const to = positions.get(endpointDevice(link.b));
      if (!from || !to) return;
      const group = svgElement("g", { class: `topology-link${selectedResource?.kind === "link" && selectedResource.index === index ? " selected" : ""}`, role: "button", tabindex: "0", "aria-label": `Edit link ${link.a} to ${link.b}` });
      group.append(svgElement("line", { x1: from.x, y1: from.y, x2: to.x, y2: to.y, class: "topology-link-hit" }));
      group.append(svgElement("line", { x1: from.x, y1: from.y, x2: to.x, y2: to.y, class: "topology-link-visible" }));
      const label = svgElement("text", { x: (from.x + to.x) / 2, y: (from.y + to.y) / 2 - 9, class: "topology-link-label" });
      label.textContent = link.network || "Network not set";
      group.append(label);
      group.addEventListener("click", () => selectEditorResource({ kind: "link", index }));
      group.addEventListener("keydown", (event) => { if (event.key === "Enter" || event.key === " ") selectEditorResource({ kind: "link", index }); });
      canvas.append(group);
    });
    devices.forEach((device, index) => {
      const position = positions.get(device.name);
      const group = svgElement("g", { class: `topology-device${selectedResource?.kind === "device" && selectedResource.index === index ? " selected" : ""}`, "data-index": index, transform: `translate(${position.x - 88} ${position.y - 34})`, role: "button", tabindex: "0", "aria-label": `Drag or edit device ${device.name}` });
      group.append(svgElement("rect", { width: 176, height: 68, rx: 5 }));
      const name = svgElement("text", { x: 12, y: 25, class: "topology-device-name" });
      name.textContent = device.name || "Unnamed device";
      group.append(name);
      const detail = svgElement("text", { x: 12, y: 47, class: "topology-device-detail" });
      detail.textContent = [device.vendor || "vendor unset", device.model || "model unset"].join(" · ");
      group.append(detail);
      group.addEventListener("click", () => selectEditorResource({ kind: "device", index }));
      group.addEventListener("keydown", (event) => { if (event.key === "Enter" || event.key === " ") selectEditorResource({ kind: "device", index }); });
      canvas.append(group);
    });
    $("#editor-topology-empty").classList.toggle("hidden", devices.length > 0);
  }

  function propertyField(container, labelText, value, onInput, type = "text") {
    const label = node("label", "editor-field");
    label.append(node("span", "", labelText));
    const input = node("input");
    input.type = type;
    input.value = value ?? "";
    input.addEventListener("input", () => onInput(input.value));
    label.append(input);
    container.append(label);
  }

  function setNestedValue(target, path, value) {
    const parts = path.split(".");
    let current = target;
    parts.slice(0, -1).forEach((part) => { current[part] ||= {}; current = current[part]; });
    const last = parts[parts.length - 1];
    if (value === "") {
      delete current[last];
      if (parts.length > 1 && Object.keys(current).length === 0) {
        let parent = target;
        parts.slice(0, -2).forEach((part) => { parent = parent[part]; });
        delete parent[parts[parts.length - 2]];
      }
    } else current[last] = value;
  }

  function updateEditorProperty(target, path, value) {
    if (path === "name" && target.name !== value && currentEditorSite()) {
      const previousName = target.name;
      (currentEditorSite().links || []).forEach((link) => {
        ["a", "b"].forEach((key) => {
          if (endpointDevice(link[key]) === previousName) link[key] = `${value}:${String(link[key]).split(":").slice(1).join(":")}`;
        });
      });
    }
    setNestedValue(target, path, value);
    syncEditorSource();
  }

  function removeButton(label, onClick) {
    const button = node("button", "button button-danger", label);
    button.type = "button";
    button.addEventListener("click", onClick);
    return button;
  }

  function renderInspector() {
    const container = $("#editor-inspector");
    container.replaceChildren();
    const site = currentEditorSite();
    const resource = selectedResource;
    if (!site) {
      $("#editor-inspector-title").textContent = "Selection";
      container.append(node("p", "muted", "Add a site to begin editing."));
      return;
    }
    if (!resource) {
      $("#editor-inspector-title").textContent = site.name || "Site properties";
      propertyField(container, "Site name", site.name, (value) => updateEditorProperty(site, "name", value));
      propertyField(container, "Mode", site.mode, (value) => updateEditorProperty(site, "mode", value));
      propertyField(container, "Bootstrap network", site.bootstrap?.network, (value) => updateEditorProperty(site, "bootstrap.network", value));
      propertyField(container, "Bootstrap gateway", site.bootstrap?.gateway, (value) => updateEditorProperty(site, "bootstrap.gateway", value));
      const services = node("fieldset", "service-fields");
      services.append(node("legend", "", "Services"));
      ["dhcp", "tftp", "pxe"].forEach((name) => {
        const label = node("label", "service-toggle");
        const input = node("input");
        input.type = "checkbox";
        input.checked = site.services?.[name] === true;
        input.addEventListener("change", () => {
          site.services ||= {};
          if (input.checked) site.services[name] = true;
          else delete site.services[name];
          if (!Object.keys(site.services).length) delete site.services;
          syncEditorSource();
        });
        label.append(input, node("span", "", name.toUpperCase()));
        services.append(label);
      });
      container.append(services);
      container.append(removeButton("Remove site", () => {
        editorConfig.sites.splice(selectedSiteIndex, 1);
        selectedSiteIndex = Math.max(0, selectedSiteIndex - 1);
        selectedResource = null;
        syncEditorSource();
        renderEditor();
      }));
      return;
    }
    if (resource.kind === "device") {
      const device = site.devices?.[resource.index];
      if (!device) { selectedResource = null; renderInspector(); return; }
      $("#editor-inspector-title").textContent = device.name || "Device properties";
      [["Name", "name"], ["Role", "role"], ["Vendor", "vendor"], ["Family", "family"], ["Model", "model"], ["Management IPv4", "management.ipv4"], ["Serial", "identity.serial"], ["Provisioning method", "provisioning.method"]].forEach(([label, path]) => {
        const value = path.split(".").reduce((current, key) => current?.[key], device);
        propertyField(container, label, value, (next) => updateEditorProperty(device, path, next));
      });
      const macLabel = node("label", "editor-field");
      macLabel.append(node("span", "", "MAC addresses (one per line)"));
      const macInput = node("textarea", "editor-small-textarea");
      macInput.value = (device.identity?.macs || []).join("\n");
      macInput.addEventListener("input", () => {
        device.identity ||= {};
        const values = macInput.value.split("\n").map((value) => value.trim()).filter(Boolean);
        if (values.length) device.identity.macs = values;
        else delete device.identity.macs;
        if (!Object.keys(device.identity).length) delete device.identity;
        syncEditorSource();
      });
      macLabel.append(macInput);
      container.append(macLabel);
      container.append(removeButton("Remove device", () => {
        const name = device.name;
        site.devices.splice(resource.index, 1);
        site.links = (site.links || []).filter((link) => endpointDevice(link.a) !== name && endpointDevice(link.b) !== name);
        selectedResource = null;
        syncEditorSource();
        renderEditor();
      }));
      return;
    }
    const link = site.links?.[resource.index];
    if (!link) { selectedResource = null; renderInspector(); return; }
    $("#editor-inspector-title").textContent = "Link properties";
    [["Endpoint A (device:interface)", "a"], ["Endpoint B (device:interface)", "b"], ["Network CIDR", "network"], ["Link ID", "id"]].forEach(([label, path]) => {
      propertyField(container, label, link[path], (value) => updateEditorProperty(link, path, value));
    });
    container.append(removeButton("Remove link", () => {
      site.links.splice(resource.index, 1);
      selectedResource = null;
      syncEditorSource();
      renderEditor();
    }));
  }

  function selectEditorResource(resource) {
    selectedResource = resource;
    renderEditor();
  }

  function renderEditor(renderProperties = true) {
    const sites = editorConfig.sites || [];
    const sitePicker = $("#editor-site");
    sitePicker.replaceChildren();
    sites.forEach((site, index) => {
      const option = node("option", "", site.name || `Site ${index + 1}`);
      option.value = String(index);
      sitePicker.append(option);
    });
    if (sites.length) {
      selectedSiteIndex = Math.min(selectedSiteIndex, sites.length - 1);
      sitePicker.value = String(selectedSiteIndex);
    } else {
      sitePicker.append(node("option", "", "No sites"));
      sitePicker.value = "";
    }
    const site = currentEditorSite();
    const devices = site?.devices || [];
    const links = site?.links || [];
    $("#editor-topology-title").textContent = site?.name || "Select a site";
    $("#editor-count").textContent = `${devices.length} DEVICES · ${links.length} LINKS`;
    renderTopology(site);
    const linkList = $("#editor-links");
    linkList.replaceChildren();
    links.forEach((link, index) => {
      const button = node("button", `link-chip${selectedResource?.kind === "link" && selectedResource.index === index ? " active" : ""}`, `${link.a || "?"} to ${link.b || "?"}`);
      button.type = "button";
      button.addEventListener("click", () => selectEditorResource({ kind: "link", index }));
      linkList.append(button);
    });
    if (renderProperties) renderInspector();
  }

  function renderPlanPreview(result) {
    const panel = $("#editor-plan");
    panel.replaceChildren();
    const heading = node("div", "editor-panel-heading");
    const title = node("div");
    title.append(node("p", "eyebrow", "PLAN PREVIEW"), node("h3", "", `${result.plan.status} · ${result.plan.tasks.length} tasks`));
    heading.append(title, statusTag(result.plan.status));
    panel.append(heading);
    if (result.generation_status) {
      panel.append(node("p", "generation-summary", `Generation ${result.generation_status} · Execution ${result.execution_status} · Verification ${result.verification_status}`));
      (result.tasks || []).forEach((task) => {
        (task.artifacts || []).forEach((artifact) => {
          const row = node("div", "generation-artifact");
          row.append(node("b", "", `${artifact.device} · ${artifact.type}`));
          row.append(node("span", "mono", artifact.path));
          row.append(node("span", "mono", `SHA-256 ${artifact.sha256}`));
          row.append(node("span", "quiet-label", `template ${artifact.template_version} · ${artifact.status}`));
          panel.append(row);
        });
      });
    }
    const list = node("div", "preview-task-list");
    (result.plan.tasks || []).forEach((task) => {
      const row = node("div", "preview-task");
      const detail = node("div");
      detail.append(node("b", "", task.target || task.site), node("span", "", `${task.site} · ${task.action}`));
      row.append(detail, statusTag(task.status));
      if (task.reason) row.append(node("p", "preview-reason", task.reason));
      list.append(row);
    });
    if (!result.plan.tasks.length) list.append(node("p", "muted", "No tasks were produced for this configuration."));
    panel.append(list);
    panel.classList.remove("hidden");
  }

  async function previewEditorPlan() {
    const source = $("#editor-source").value;
    $("#editor-error").textContent = "";
    $("#editor-status").textContent = "Validating…";
    try {
      const result = await request("/plan/preview", { method: "POST", body: JSON.stringify({ input: source }) });
      if ($("#editor-source").value !== source) {
        $("#editor-status").textContent = "Draft changed during validation";
        $("#editor-error").textContent = "The source changed while validation was running. Validate the current draft again.";
        $("#editor-submit").disabled = true;
        $("#editor-generate").disabled = true;
        $("#editor-plan").classList.add("hidden");
        return;
      }
      editorConfig = result.infrastructure || { sites: [] };
      selectedSiteIndex = Math.min(selectedSiteIndex, Math.max(0, editorConfig.sites.length - 1));
      selectedResource = null;
      previewSource = source;
      $("#editor-status").textContent = `Valid · ${result.plan.tasks.length} tasks`;
      $("#editor-source-state").textContent = "VALIDATED BY PROVIDER";
      $("#editor-submit").disabled = false;
      $("#editor-generate").disabled = false;
      renderEditor();
      renderPlanPreview(result);
    } catch (error) {
      $("#editor-status").textContent = "Validation failed";
      $("#editor-error").textContent = error.message;
      $("#editor-submit").disabled = true;
      $("#editor-generate").disabled = true;
      $("#editor-plan").classList.add("hidden");
    }
  }

  function loadExampleInfrastructure() {
    $("#editor-source").value = exampleInfrastructure;
    updateYamlHighlight();
    invalidateEditorPreview();
    scheduleEditorPreview(80);
  }

  function addEditorSite() {
    editorConfig.sites ||= [];
    const index = editorConfig.sites.length + 1;
    editorConfig.sites.push({ name: `site-${index}`, devices: [], links: [] });
    selectedSiteIndex = editorConfig.sites.length - 1;
    selectedResource = null;
    syncEditorSource();
    renderEditor();
  }

  function addEditorDevice() {
    const site = currentEditorSite();
    if (!site) { showNotice("Add a site before adding devices.", true); return; }
    site.devices ||= [];
    const index = site.devices.length + 1;
    site.devices.push({ name: `device-${index}`, role: "router", management: {} });
    selectedResource = { kind: "device", index: site.devices.length - 1 };
    syncEditorSource();
    renderEditor();
  }

  function addEditorLink() {
    const site = currentEditorSite();
    const devices = site?.devices || [];
    if (devices.length < 2) { showNotice("Add at least two devices before connecting them.", true); return; }
    site.links ||= [];
    const index = site.links.length + 1;
    site.links.push({ a: `${devices[0].name}:eth0`, b: `${devices[1].name}:eth0`, network: `10.10.${index}.0/30` });
    selectedResource = { kind: "link", index: site.links.length - 1 };
    syncEditorSource();
    renderEditor();
  }

  async function createEditorPlan() {
    if (!previewSource || previewSource !== $("#editor-source").value) {
      showNotice("Validate the current draft before creating a plan.", true);
      return;
    }
    const button = $("#editor-submit");
    button.disabled = true;
    try {
      await request("/jobs", { method: "POST", body: JSON.stringify({ input: $("#editor-source").value }) });
      selectView("jobs");
      await refreshData();
      showNotice("Planning job created. No infrastructure changes were executed.");
    } catch (error) {
      showNotice(error.message, true);
      button.disabled = false;
    }
  }

  async function generateEditorArtifacts() {
    if (!previewSource || previewSource !== $("#editor-source").value) {
      showNotice("Validate the current draft before generating artifacts.", true);
      return;
    }
    const button = $("#editor-generate");
    button.disabled = true;
    $("#editor-status").textContent = "Generating planned artifacts…";
    try {
      const source = previewSource;
      const result = await request("/plan/generate", { method: "POST", body: JSON.stringify({ input: source }) });
      if ($("#editor-source").value !== source) {
        $("#editor-status").textContent = "Draft changed during generation";
        $("#editor-error").textContent = "The source changed while artifacts were being generated. Validate the current draft again.";
        return;
      }
      $("#editor-status").textContent = `Generation ${result.generation_status}`;
      $("#editor-source-state").textContent = "GENERATION RESULT FROM PROVIDER";
      renderPlanPreview(result);
      showNotice(`Generation ${result.generation_status}; execution was not requested and verification was not performed.`);
    } catch (error) {
      const result = error.payload?.result;
      if (result?.plan) renderPlanPreview(result);
      $("#editor-status").textContent = result ? `Generation ${result.generation_status}` : "Generation failed";
      $("#editor-error").textContent = error.payload?.error || error.message;
      showNotice(error.payload?.error || error.message, true);
    } finally {
      button.disabled = !previewSource || previewSource !== $("#editor-source").value;
    }
  }

  function statusTag(status) {
    const label = status || "unknown";
    const tag = node("span", `status status-${label}`, label);
    return tag;
  }

  function taskDetails(plan) {
    const details = node("details", "task-details");
    const summary = node("summary", "row-action", `${(plan?.tasks || []).length} task(s) · inspect`);
    details.append(summary);
    const list = node("ul");
    (plan?.tasks || []).forEach((task) => {
      const item = node("li");
      item.textContent = `${task.status || "pending"}  ${task.site || "site"} / ${task.target || task.id}  ·  ${task.action}${task.reason ? `  ·  ${task.reason}` : ""}`;
      list.append(item);
    });
    details.append(list);
    return details;
  }

  function jobRow(job, expanded = false) {
    const row = node("tr");
    const id = node("td", "mono", job.id);
    const status = node("td");
    status.append(statusTag(job.status));
    const site = node("td", "mono", [...new Set((job.plan?.tasks || []).map((task) => task.site).filter(Boolean))].join(", ") || "—");
    const tasks = node("td");
    tasks.append(expanded ? taskDetails(job.plan) : node("span", "mono", String((job.plan?.tasks || []).length)));
    const updated = node("td", "", formatDate(job.updated_at || job.created_at));
    row.append(id, status, ...(expanded ? [site] : []), tasks, updated);
    if (expanded) {
      const actions = node("td");
      if (job.status === "planned" || job.status === "blocked") actions.append(actionButton("Cancel", "cancel-job", job.id));
      if (job.status === "cancelled" || job.status === "failed" || job.status === "blocked") actions.append(actionButton("Retry", "retry-job", job.id));
      row.append(actions);
    }
    return row;
  }

  function actionButton(label, action, id) {
    const button = node("button", "row-action", label);
    button.type = "button";
    button.dataset.action = action;
    button.dataset.id = id;
    return button;
  }

  function renderJobs() {
    const overviewBody = $("#overview-jobs");
    const jobsBody = $("#jobs-table");
    overviewBody.replaceChildren();
    jobsBody.replaceChildren();
    const recent = jobs.slice(0, 5);
    recent.forEach((job) => overviewBody.append(jobRow(job)));
    jobs.forEach((job) => jobsBody.append(jobRow(job, true)));
    $("#overview-empty").classList.toggle("hidden", jobs.length > 0);
    $("#jobs-empty").classList.toggle("hidden", jobs.length > 0);
    $("#stat-jobs").textContent = String(jobs.length);
    const blocked = jobs.reduce((count, job) => count + (job.plan?.tasks || []).filter((task) => task.status === "blocked").length, 0);
    $("#stat-blocked").textContent = String(blocked);
    renderSignals();
  }

  function renderSignals() {
    const target = $("#plan-signals");
    target.replaceChildren();
    const counts = { planned: 0, blocked: 0, failed: 0, cancelled: 0 };
    jobs.forEach((job) => (job.plan?.tasks || []).forEach((task) => {
      if (Object.hasOwn(counts, task.status)) counts[task.status] += 1;
    }));
    const total = Math.max(1, Object.values(counts).reduce((sum, value) => sum + value, 0));
    Object.entries(counts).forEach(([name, count]) => {
      const line = node("div", "signal");
      line.append(node("span", "", name), node("b", "", String(count)));
      const track = node("div", "signal-track");
      const fill = node("i", name === "blocked" || name === "failed" ? "warn" : "");
      fill.style.width = `${Math.min(100, Math.round((count / total) * 100))}%`;
      track.append(fill);
      line.append(track);
      target.append(line);
    });
  }

  function renderAgents() {
    const body = $("#agents-table");
    const map = $("#agent-map");
    body.replaceChildren();
    map.replaceChildren();
    $("#agents-empty").classList.toggle("hidden", agents.length > 0);
    $("#stat-agents").textContent = currentUser.role === "admin" ? String(agents.length) : "—";
    $("#stat-agent-detail").textContent = currentUser.role === "admin" ? "registered site agents" : "administrator access required";
    $("#map-count").textContent = currentUser.role === "admin" ? `${agents.length} REGISTERED` : "ADMIN VIEW";
    const controller = node("div", "map-controller");
    controller.append(node("span", "map-core", "IF"), node("span", "", "PROVIDER"));
    map.append(controller, node("div", "map-links"));
    const agentList = node("div", "map-agents");
    if (currentUser.role !== "admin") {
      agentList.append(node("span", "map-empty", "Administrator role required to view agents."));
    } else if (!agents.length) {
      agentList.append(node("span", "map-empty", "No agents registered."));
    }
    agents.forEach((agent) => {
      const row = node("div", `map-agent ${agent.status === "online" ? "" : "offline"}`);
      row.append(node("b"), node("span", "", `${agent.site_id} · ${agent.id}`));
      agentList.append(row);
      const tableRow = node("tr");
      tableRow.append(
        node("td", "mono", agent.id),
        node("td", "mono", agent.site_id),
        Object.assign(node("td"), { innerText: "" }),
        node("td", "mono", agent.version || "—"),
        node("td", "mono", String(agent.queue_depth)),
        node("td", "", formatDate(agent.last_seen_at)),
      );
      tableRow.children[2].append(statusTag(agent.status));
      body.append(tableRow);
    });
    map.append(agentList);
  }

  function renderEvents() {
    const list = $("#events-list");
    list.replaceChildren();
    $("#events-empty").classList.toggle("hidden", events.length > 0);
    events.slice().reverse().forEach((event) => {
      const row = node("div", "event-row");
      row.append(node("span", "event-time", formatDate(event.timestamp)), node("span", "event-type", event.type));
      const subject = [event.site_id, event.agent_id, event.job_id].filter(Boolean).join(" · ");
      row.append(node("span", "event-detail", subject || event.event_id));
      list.append(row);
    });
    $("#stat-activity").textContent = events.length ? formatDate(events[events.length - 1].timestamp) : "—";
  }

  function renderUsers(users) {
    const table = $("#users-table");
    table.replaceChildren();
    $("#users-empty").classList.toggle("hidden", users.length > 0);
    users.forEach((user) => {
      const row = node("tr");
      row.append(node("td", "", user.username), node("td", "", user.role), node("td", "", user.disabled ? "disabled" : "active"), node("td", "", formatDate(user.created_at)));
      const actionCell = node("td");
      if (user.id !== currentUser.id || user.role !== "admin") {
        const button = actionButton(user.disabled ? "Enable" : "Disable", "toggle-user", user.id);
        button.dataset.disabled = String(user.disabled);
        actionCell.append(button);
      }
      row.append(actionCell);
      table.append(row);
    });
  }

  async function loadJobs() {
    jobs = await request("/jobs");
    renderJobs();
  }

  async function loadAgents() {
    if (currentUser.role !== "admin") return;
    try {
      agents = await request("/agents");
      renderAgents();
    } catch (error) { showNotice(error.message, true); }
  }

  async function loadEvents() {
    if (currentUser.role !== "admin") return;
    try {
      events = await request("/events?limit=100");
      renderEvents();
    } catch (error) { showNotice(error.message, true); }
  }

  async function loadUsers() {
    if (currentUser.role !== "admin") return;
    try { renderUsers(await request("/users")); }
    catch (error) { showNotice(error.message, true); }
  }

  async function refreshData() {
    try {
      await loadJobs();
      if (currentUser.role === "admin") {
        const [agentResult, eventResult] = await Promise.allSettled([request("/agents"), request("/events?limit=100")]);
        if (agentResult.status === "fulfilled") agents = agentResult.value;
        if (eventResult.status === "fulfilled") events = eventResult.value;
        renderAgents();
        renderEvents();
        if (eventResult.status === "rejected") showNotice(eventResult.reason.message, true);
      } else {
        agents = [];
        events = [];
        renderAgents();
      }
      const active = document.querySelector(".nav-item.active")?.dataset.view;
      if (active === "users") loadUsers();
      if (active === "agents") renderAgents();
      if (active === "activity") renderEvents();
    } catch (error) {
      showNotice(error.message, true);
    }
  }

  $("#login-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    const submit = form.querySelector("button[type=submit]");
    submit.disabled = true;
    $("#login-error").textContent = "";
    try {
      const session = await request("/auth/login", { method: "POST", body: JSON.stringify({ username: form.elements.username.value, password: form.elements.password.value }) });
      token.value = session.token;
      showConsole(session.user);
      form.reset();
    } catch (error) {
      $("#login-error").textContent = error.message;
    } finally { submit.disabled = false; }
  });

  $("#logout").addEventListener("click", async () => {
    try { await request("/auth/logout", { method: "POST" }); } catch (_) { /* local sign-out still clears the token */ }
    token.value = "";
    currentUser = null;
    jobs = [];
    agents = [];
    events = [];
    showLogin();
  });

  $("#navigation").addEventListener("click", (event) => {
    const button = event.target.closest("[data-view]");
    if (button) selectView(button.dataset.view);
  });
  document.body.addEventListener("click", (event) => {
    const control = event.target.closest("[data-action]");
    const action = control?.dataset.action;
    if (action === "new-plan") selectView("editor");
    if (action === "refresh") refreshData();
    if (action === "refresh-logs") loadTechnicalLogs();
    if (action === "cancel-job" || action === "retry-job") changeJob(control.dataset.id, action === "cancel-job" ? "cancel" : "retry");
    if (action === "toggle-user") toggleUser(control.dataset.id, control.dataset.disabled === "true");
  });

  $("#logs-apply")?.addEventListener("click", () => {
    loadTechnicalLogs();
  });

  async function changeJob(id, operation) {
    try {
      await request(`/jobs/${encodeURIComponent(id)}/${operation}`, { method: "POST" });
      await refreshData();
      showNotice(`Job ${operation === "cancel" ? "cancelled" : "retried"}.`);
    } catch (error) { showNotice(error.message, true); }
  }

  async function toggleUser(id, disabled) {
    try {
      await request(`/users/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify({ disabled: !disabled }) });
      await loadUsers();
      showNotice(`User ${disabled ? "enabled" : "disabled"}.`);
    } catch (error) { showNotice(error.message, true); }
  }
  const topologyCanvas = $("#editor-topology");
  topologyCanvas.addEventListener("pointerdown", (event) => {
    const group = event.target.closest(".topology-device");
    if (!group || event.button !== 0) return;
    const site = currentEditorSite();
    const index = Number(group.dataset.index);
    const device = site?.devices?.[index];
    if (!device) return;
    const point = topologyPoint(event, topologyCanvas);
    const position = topologyDevicePosition(site, device, index);
    topologyDrag = { device, site, pointerId: event.pointerId, offsetX: position.x - point.x, offsetY: position.y - point.y };
    selectedResource = { kind: "device", index };
    group.classList.add("selected");
    topologyCanvas.setPointerCapture(event.pointerId);
    renderInspector();
  });
  topologyCanvas.addEventListener("pointermove", (event) => {
    if (!topologyDrag || topologyDrag.pointerId !== event.pointerId) return;
    const point = topologyPoint(event, topologyCanvas);
    const layout = topologyLayouts.get(topologySiteKey(topologyDrag.site));
    layout.set(topologyDrag.device.name, {
      x: Math.max(88, Math.min(912, point.x + topologyDrag.offsetX)),
      y: Math.max(34, Math.min(486, point.y + topologyDrag.offsetY)),
    });
    renderTopology(topologyDrag.site);
  });
  const finishTopologyDrag = (event) => {
    if (!topologyDrag || topologyDrag.pointerId !== event.pointerId) return;
    topologyDrag = null;
    if (topologyCanvas.hasPointerCapture(event.pointerId)) topologyCanvas.releasePointerCapture(event.pointerId);
  };
  topologyCanvas.addEventListener("pointerup", finishTopologyDrag);
  topologyCanvas.addEventListener("pointercancel", finishTopologyDrag);

  $("#new-plan").addEventListener("click", () => selectView("editor"));
  $("#editor-site").addEventListener("change", (event) => {
    selectedSiteIndex = Number(event.target.value) || 0;
    selectedResource = null;
    renderEditor();
  });
  $("#editor-add-site").addEventListener("click", addEditorSite);
  $("#editor-add-device").addEventListener("click", addEditorDevice);
  $("#editor-add-link").addEventListener("click", addEditorLink);
  $("#editor-example").addEventListener("click", loadExampleInfrastructure);
  $("#editor-generate").addEventListener("click", generateEditorArtifacts);
  $("#editor-submit").addEventListener("click", createEditorPlan);
  $("#editor-source").addEventListener("input", () => {
    updateYamlHighlight();
    invalidateEditorPreview();
    scheduleEditorPreview();
  });
  $("#editor-source").addEventListener("scroll", updateYamlHighlight);

  $("#user-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    const form = event.currentTarget;
    const button = form.querySelector("button[type=submit]");
    button.disabled = true;
    try {
      await request("/users", { method: "POST", body: JSON.stringify({ username: form.elements.username.value, password: form.elements.password.value, role: form.elements.role.value }) });
      form.reset();
      await loadUsers();
      showNotice("User created.");
    } catch (error) { showNotice(error.message, true); }
    finally { button.disabled = false; }
  });
})();