(() => {
  const token = { value: "" };
  let currentUser = null;
  let jobs = [];
  let agents = [];
  let events = [];
  const $ = (selector) => document.querySelector(selector);

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
    if (!response.ok) throw new Error(result.error || `Request failed (${response.status})`);
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
      jobs: "Plans & jobs",
      agents: "Registered agents",
      activity: "Audit trail",
      users: "Access management",
    };
    document.querySelectorAll(".view").forEach((view) => view.classList.add("hidden"));
    $(`#${name}-view`)?.classList.remove("hidden");
    document.querySelectorAll(".nav-item").forEach((item) => item.classList.toggle("active", item.dataset.view === name));
    $("#page-title").textContent = labels[name] || labels.overview;
    $("#view-label").textContent = name.toUpperCase();
    if (name === "agents") loadAgents();
    if (name === "activity") loadEvents();
    if (name === "users") loadUsers();
  }

  function formatDate(value) {
    if (!value) return "—";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return "—";
    return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
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
    if (action === "new-plan") $("#plan-dialog").showModal();
    if (action === "refresh") refreshData();
    if (action === "cancel-job" || action === "retry-job") changeJob(control.dataset.id, action === "cancel-job" ? "cancel" : "retry");
    if (action === "toggle-user") toggleUser(control.dataset.id, control.dataset.disabled === "true");
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
  $("#new-plan").addEventListener("click", () => $("#plan-dialog").showModal());
  $("#close-dialog").addEventListener("click", () => $("#plan-dialog").close());
  $("#cancel-plan").addEventListener("click", () => $("#plan-dialog").close());

  $("#plan-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    $("#plan-error").textContent = "";
    const button = event.currentTarget.querySelector("button[type=submit]");
    button.disabled = true;
    try {
      await request("/jobs", { method: "POST", body: JSON.stringify({ input: event.currentTarget.elements.input.value }) });
      $("#plan-dialog").close();
      event.currentTarget.reset();
      selectView("jobs");
      await refreshData();
      showNotice("Planning job created. No infrastructure changes were executed.");
    } catch (error) { $("#plan-error").textContent = error.message; }
    finally { button.disabled = false; }
  });

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