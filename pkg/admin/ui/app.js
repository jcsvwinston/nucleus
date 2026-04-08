(function () {
  "use strict";

  const PAGE_SIZE = 25;
  const UI = window.AdminUI || createFallbackUI();

  const els = {
    app: document.getElementById("app"),
    sidebar: document.getElementById("sidebar"),
    modelNav: document.getElementById("model-nav"),
    siteTitle: document.getElementById("site-title"),
    breadcrumbs: document.getElementById("breadcrumbs"),
    refreshBtn: document.getElementById("refresh-btn"),
    newRecordBtn: document.getElementById("new-record-btn"),
    menuToggle: document.getElementById("menu-toggle"),
    cmdkOpen: document.getElementById("cmdk-open"),
    cmdkModal: document.getElementById("command-palette"),
    cmdkClose: document.getElementById("cmdk-close"),
    cmdkInput: document.getElementById("cmdk-input"),
    cmdkList: document.getElementById("cmdk-list"),
    confirmModal: document.getElementById("confirm-modal"),
    confirmText: document.getElementById("confirm-text"),
    confirmAccept: document.getElementById("confirm-accept"),
    confirmCancel: document.getElementById("confirm-cancel"),
    toasts: document.getElementById("toasts"),
  };

  const state = {
    models: [],
    currentModel: null,
    schema: null,
    page: 1,
    search: "",
    filters: {},
    sortColumn: "",
    sortDir: "asc",
    selectedIDs: new Set(),
    paletteItems: [],
    paletteIndex: 0,
  };

  let sessionsRefreshTimer = null;
  let confirmResolver = null;
  let overlayReturnFocus = null;

  const API = (() => {
    const base = window.location.pathname.replace(/\/+$/, "");
    const root = base + "/api";
    const transientStatus = new Set([408, 425, 429, 500, 502, 503, 504]);
    const maxAttempts = 3;

    async function req(path, opts) {
      let lastErr = null;
      for (let attempt = 1; attempt <= maxAttempts; attempt++) {
        try {
          const res = await fetch(root + path, {
            headers: { "Content-Type": "application/json", ...(opts && opts.headers) },
            ...opts,
          });

          if (!res.ok) {
            let msg = res.statusText;
            try {
              const payload = await res.json();
              msg = (payload && payload.error && payload.error.message) || msg;
            } catch (_) {}

            if (transientStatus.has(res.status) && attempt < maxAttempts) {
              await sleep(backoff(attempt));
              continue;
            }

            throw new Error(msg || "Request failed");
          }

          const ct = res.headers.get("content-type") || "";
          if (ct.includes("application/json")) {
            return res.json();
          }
          return res;
        } catch (err) {
          lastErr = err;
          const networkErr = isNetworkFailure(err);
          if (!networkErr || attempt >= maxAttempts) {
            break;
          }
          await sleep(backoff(attempt));
        }
      }

      throw lastErr || new Error("Request failed");
    }

    function sleep(ms) {
      return new Promise(function (resolve) {
        setTimeout(resolve, ms);
      });
    }

    function backoff(attempt) {
      return 250 * Math.pow(2, attempt - 1);
    }

    function isNetworkFailure(err) {
      if (!err) {
        return false;
      }
      const msg = String(err.message || "").toLowerCase();
      if (msg.includes("network") || msg.includes("failed to fetch")) {
        return true;
      }
      if (typeof navigator !== "undefined" && navigator.onLine === false) {
        return true;
      }
      return false;
    }

    return {
      models: () => req("/models"),
      schema: (name) => req(`/models/${encodeURIComponent(name)}/schema`),
      list: (name, params) => req(`/models/${encodeURIComponent(name)}?${new URLSearchParams(params)}`),
      get: (name, id) => req(`/models/${encodeURIComponent(name)}/${encodeURIComponent(id)}`),
      create: (name, data) => req(`/models/${encodeURIComponent(name)}`, { method: "POST", body: JSON.stringify(data) }),
      update: (name, id, data) => req(`/models/${encodeURIComponent(name)}/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(data) }),
      del: (name, id) => req(`/models/${encodeURIComponent(name)}/${encodeURIComponent(id)}`, { method: "DELETE" }),
      bulk: (name, action, ids) =>
        req(`/models/${encodeURIComponent(name)}/bulk`, {
          method: "POST",
          body: JSON.stringify({ action: action, ids: ids }),
        }),
      bulkDelete: (name, ids) =>
        req(`/models/${encodeURIComponent(name)}/bulk`, {
          method: "POST",
          body: JSON.stringify({ action: "delete", ids: ids }),
        }),
      exportURL: (name) => `${root}/models/${encodeURIComponent(name)}/export`,
      sessions: (limit) => req(`/sessions?limit=${encodeURIComponent(String(limit || 250))}`),
    };
  })();

  function init() {
    bindGlobalEvents();
    bootstrap();
  }

  function bootstrap() {
    refreshModels(true).then(onRoute).catch(showFatal);
  }

  function bindGlobalEvents() {
    window.addEventListener("hashchange", onRoute);

    els.menuToggle.addEventListener("click", function () {
      els.sidebar.classList.toggle("open");
    });

    els.refreshBtn.addEventListener("click", async function () {
      try {
        await refreshModels(true);
        await onRoute();
        toast("Data refreshed", "success");
      } catch (err) {
        toast(errorText(err), "error");
      }
    });

    els.newRecordBtn.addEventListener("click", function () {
      if (!state.currentModel) {
        toast("Select a model first", "warning");
        return;
      }
      navigate(`#/model/${state.currentModel}/new`);
    });

    els.cmdkOpen.addEventListener("click", openPalette);
    els.cmdkClose.addEventListener("click", closePalette);
    els.cmdkModal.addEventListener("click", function (evt) {
      if (evt.target === els.cmdkModal) {
        closePalette();
      }
    });

    els.cmdkInput.addEventListener("input", function () {
      renderPalette(this.value);
    });

    els.cmdkInput.addEventListener("keydown", function (evt) {
      if (evt.key === "ArrowDown") {
        evt.preventDefault();
        movePalette(1);
      }
      if (evt.key === "ArrowUp") {
        evt.preventDefault();
        movePalette(-1);
      }
      if (evt.key === "Enter") {
        evt.preventDefault();
        runPaletteItem(state.paletteItems[state.paletteIndex]);
      }
    });

    els.cmdkList.addEventListener("click", function (evt) {
      const btn = evt.target.closest("button[data-palette-index]");
      if (!btn) {
        return;
      }
      const idx = Number(btn.getAttribute("data-palette-index"));
      runPaletteItem(state.paletteItems[idx]);
    });

    document.addEventListener("keydown", function (evt) {
      if ((evt.ctrlKey || evt.metaKey) && evt.key.toLowerCase() === "k") {
        evt.preventDefault();
        openPalette();
      }
      if (evt.key === "Escape") {
        closePalette();
        resolveConfirm(false);
      }
    });

    els.confirmCancel.addEventListener("click", function () {
      resolveConfirm(false);
    });

    els.confirmAccept.addEventListener("click", function () {
      resolveConfirm(true);
    });
  }

  async function refreshModels(quiet) {
    const payload = await API.models();
    state.models = payload.models || [];
    els.siteTitle.textContent = payload.title || "GoFrame Admin";
    document.title = payload.title || "GoFrame Admin";
    renderModelNav();
    renderPalette(els.cmdkInput.value || "");
    updateNewButton();
    if (!quiet) {
      toast("Models updated", "success");
    }
  }

  function renderModelNav() {
    const html = state.models
      .map(function (model) {
        return `
          <a href="#/model/${escapeHtml(model.name)}" class="nav-link" data-nav="${escapeHtml(model.name)}">
            <span class="nav-icon">mdl</span>
            <span>${escapeHtml(model.plural || model.name)}</span>
            <span class="nav-badge">${Number(model.count || 0)}</span>
          </a>
        `;
      })
      .join("");
    els.modelNav.innerHTML = html;
  }

  async function onRoute() {
    const route = parseRoute();
    setActiveNav(route.view === "sessions" ? "sessions" : route.model || "dashboard");
    renderBreadcrumbs(route);
    closeSidebarOnMobile();

    if (route.view === "dashboard") {
      stopSessionsAutoRefresh();
      state.currentModel = null;
      state.schema = null;
      updateNewButton();
      renderDashboard();
      return;
    }

    if (route.view === "sessions") {
      stopSessionsAutoRefresh();
      state.currentModel = null;
      state.schema = null;
      updateNewButton();
      await renderSessionsOverview();
      startSessionsAutoRefresh();
      return;
    }

    if (route.view === "list") {
      stopSessionsAutoRefresh();
      if (state.currentModel !== route.model) {
        state.search = "";
        state.filters = {};
        state.sortColumn = "";
        state.sortDir = "asc";
      }
      await renderList(route.model, { page: route.page, search: route.search });
      return;
    }

    if (route.view === "new") {
      stopSessionsAutoRefresh();
      await renderForm(route.model, null);
      return;
    }

    if (route.view === "edit") {
      stopSessionsAutoRefresh();
      await renderForm(route.model, route.id);
      return;
    }

    stopSessionsAutoRefresh();
    navigate("#/");
  }

  function parseRoute() {
    const hash = window.location.hash || "#/";
    const cleaned = hash.replace(/^#\/?/, "");
    if (cleaned === "") {
      return { view: "dashboard" };
    }

    if (cleaned === "sessions") {
      return { view: "sessions" };
    }

    const parts = cleaned.split("/").filter(Boolean);
    if (parts[0] !== "model" || !parts[1]) {
      return { view: "dashboard" };
    }

    const model = parts[1];

    if (parts.length === 2) {
      return {
        view: "list",
        model: model,
        page: Number(new URLSearchParams(window.location.search).get("page") || 1),
        search: "",
      };
    }

    if (parts[2] === "new") {
      return { view: "new", model: model };
    }

    return { view: "edit", model: model, id: parts[2] };
  }

  function renderBreadcrumbs(route) {
    const crumbs = [];
    crumbs.push(`<a class="crumb-link" href="#/">Dashboard</a>`);

    if (route.view === "sessions") {
      crumbs.push("/");
      crumbs.push(`<a class="crumb-link" href="#/sessions">Sessions</a>`);
    }

    if (route.model) {
      crumbs.push("/");
      crumbs.push(`<a class="crumb-link" href="#/model/${escapeHtml(route.model)}">${escapeHtml(route.model)}</a>`);
    }

    if (route.view === "new") {
      crumbs.push("/");
      crumbs.push("New");
    }

    if (route.view === "edit") {
      crumbs.push("/");
      crumbs.push("Edit");
    }

    els.breadcrumbs.innerHTML = crumbs.join(" ");
  }

  function setActiveNav(name) {
    document.querySelectorAll(".nav-link").forEach(function (link) {
      link.classList.remove("active");
      if (link.getAttribute("data-nav") === name) {
        link.classList.add("active");
      }
    });
  }

  function renderDashboard() {
    const totalRecords = state.models.reduce(function (acc, model) {
      return acc + Number(model.count || 0);
    }, 0);

    const cards = state.models
      .map(function (model) {
        return `
          <article class="card" data-hash="#/model/${escapeHtml(model.name)}">
            <p class="card-label">${escapeHtml(model.plural || model.name)}</p>
            <p class="card-count">${Number(model.count || 0)}</p>
            <span class="status-chip">Open model</span>
          </article>
        `;
      })
      .join("");

    els.app.innerHTML =
      UI.sectionHead("Control center", `${state.models.length} models, ${totalRecords} records total`) +
      `
        <section class="cards">
          ${cards || UI.empty("No models registered")}
        </section>
      `;

    els.app.querySelectorAll("[data-hash]").forEach(function (card) {
      card.addEventListener("click", function () {
        navigate(card.getAttribute("data-hash"));
      });
    });
  }

  async function renderSessionsOverview() {
    setAppBusy(true);
    els.app.innerHTML = loadingMarkup();

    try {
      const payload = await API.sessions(400);
      if (!payload || payload.enabled === false) {
        const reason = (payload && payload.reason) || "Session telemetry is not available.";
        els.app.innerHTML =
          UI.sectionHead("Sessions", "Runtime telemetry", "Unavailable") +
          renderRecoverableError("Session telemetry unavailable", reason, "retry-sessions");
        const retryBtn = document.getElementById("retry-sessions");
        if (retryBtn) {
          retryBtn.addEventListener("click", function () {
            renderSessionsOverview();
          });
        }
        return;
      }

      const sessionRows = Array.isArray(payload.sessions) ? payload.sessions : [];
      const telemetry = payload.telemetry || {};
      const realtime = telemetry.realtime || { points: [] };
      const lastHour = telemetry.last_hour || { points: [] };
      const today = telemetry.today || { points: [] };

      els.app.innerHTML =
        UI.sectionHead("Sessions", `${Number(payload.current_active || 0)} active sessions`, "Live telemetry") +
        `
          <section class="detail-grid">
            ${UI.kv("Current active", String(Number(payload.current_active || 0)))}
            ${UI.kv("Active (last 5m)", String(Number(payload.active_last_5m || 0)))}
            ${UI.kv("Active (last hour)", String(Number(payload.active_last_hour || 0)))}
            ${UI.kv("Store", payload.store || "memory")}
            ${UI.kv("Source pod", payload.source_pod || "-")}
            ${UI.kv("Source host", payload.source_host || "-")}
          </section>

          <section class="cards session-chart-grid">
            ${renderSessionChartCard("Real time", "10-minute rolling active sessions", realtime.points || [])}
            ${renderSessionChartCard("Last hour", "Hourly stability signal", lastHour.points || [])}
            ${renderSessionChartCard("Today", "Active sessions by current day", today.points || [])}
          </section>

          <section class="toolbar">
            <div class="status-chip">Generated: ${escapeHtml(formatTemporal(payload.generated_at))}</div>
            ${payload.truncated_by_limit ? `<div class="status-chip">Showing first ${Number(payload.included_rows || sessionRows.length)} sessions</div>` : ""}
            <button class="btn btn-ghost" id="sessions-refresh" type="button">Refresh now</button>
          </section>

          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Session</th>
                  <th>User</th>
                  <th>Pod</th>
                  <th>Host</th>
                  <th>Last seen</th>
                  <th>Idle (s)</th>
                  <th>Expires</th>
                </tr>
              </thead>
              <tbody>
                ${renderSessionRows(sessionRows)}
              </tbody>
            </table>
          </div>
        `;

      const refreshBtn = document.getElementById("sessions-refresh");
      if (refreshBtn) {
        refreshBtn.addEventListener("click", function () {
          renderSessionsOverview();
        });
      }
    } catch (err) {
      els.app.innerHTML = renderRecoverableError("Could not load sessions", errorText(err), "retry-sessions");
      const retryBtn = document.getElementById("retry-sessions");
      if (retryBtn) {
        retryBtn.addEventListener("click", function () {
          renderSessionsOverview();
        });
      }
      toast(errorText(err), "error");
    } finally {
      setAppBusy(false);
    }
  }

  function renderSessionChartCard(title, subtitle, points) {
    return `
      <article class="card session-chart-card">
        <p class="card-label">${escapeHtml(title)}</p>
        <p class="section-subtitle">${escapeHtml(subtitle)}</p>
        ${renderSessionChart(points)}
      </article>
    `;
  }

  function renderSessionChart(points) {
    if (!Array.isArray(points) || points.length === 0) {
      return `<div class="table-empty">No telemetry points</div>`;
    }

    const width = 320;
    const height = 130;
    const padX = 10;
    const padY = 10;
    const graphW = width - padX * 2;
    const graphH = height - padY * 2;

    const values = points.map(function (p) {
      return Number(p.active || 0);
    });
    const max = Math.max(1, ...values);

    const coords = values.map(function (value, idx) {
      const x = padX + (idx / Math.max(1, values.length - 1)) * graphW;
      const y = padY + graphH - (value / max) * graphH;
      return [x, y];
    });

    const path = coords
      .map(function (c, idx) {
        return `${idx === 0 ? "M" : "L"}${c[0].toFixed(1)} ${c[1].toFixed(1)}`;
      })
      .join(" ");

    const areaPath = `${path} L${(padX + graphW).toFixed(1)} ${(padY + graphH).toFixed(1)} L${padX.toFixed(1)} ${(padY + graphH).toFixed(1)} Z`;
    const latest = values[values.length - 1] || 0;

    return `
      <div class="session-chart-wrap">
        <svg viewBox="0 0 ${width} ${height}" class="session-chart" role="img" aria-label="Session active chart">
          <path class="session-chart-area" d="${areaPath}"></path>
          <path class="session-chart-line" d="${path}"></path>
        </svg>
        <div class="session-chart-meta">
          <span class="status-chip">Latest: ${latest}</span>
          <span class="status-chip">Peak: ${max}</span>
        </div>
      </div>
    `;
  }

  function renderSessionRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="7">No active sessions</td></tr>`;
    }

    return rows
      .map(function (row) {
        const runtimePod = row.pod || row.instance || "-";
        const runtimeHost = row.host || "-";
        return `
          <tr>
            <td title="${escapeHtml(row.token || "")}">${escapeHtml(row.token_short || row.token || "-")}</td>
            <td>${escapeHtml(row.user || "-")}</td>
            <td>${escapeHtml(runtimePod)}</td>
            <td>${escapeHtml(runtimeHost)}</td>
            <td>${escapeHtml(formatTemporal(row.last_seen_at || ""))}</td>
            <td>${row.idle_seconds === undefined || row.idle_seconds === null ? "-" : escapeHtml(String(row.idle_seconds))}</td>
            <td>${escapeHtml(formatTemporal(row.expires_at || ""))}</td>
          </tr>
        `;
      })
      .join("");
  }

  function startSessionsAutoRefresh() {
    stopSessionsAutoRefresh();
    sessionsRefreshTimer = setTimeout(function tick() {
      if ((window.location.hash || "#/") !== "#/sessions") {
        stopSessionsAutoRefresh();
        return;
      }
      renderSessionsOverview().finally(function () {
        startSessionsAutoRefresh();
      });
    }, 5000);
  }

  function stopSessionsAutoRefresh() {
    if (sessionsRefreshTimer !== null) {
      clearTimeout(sessionsRefreshTimer);
      sessionsRefreshTimer = null;
    }
  }

  async function renderList(name, opts) {
    state.currentModel = name;
    state.page = opts && opts.page > 0 ? opts.page : 1;
    if (opts && typeof opts.search === "string") {
      state.search = opts.search;
    }
    updateNewButton();

    setAppBusy(true);
    els.app.innerHTML = loadingMarkup();

    try {
      const schema = await API.schema(name);
      const result = await API.list(name, {
        page: state.page,
        page_size: PAGE_SIZE,
        search: state.search || "",
        order_by: currentOrderBy(),
        ...state.filters,
      });

      state.schema = schema;
      state.selectedIDs.clear();

      const model = findModel(name);
      const columns = visibleListFields(schema);
      const filterFields = visibleFilterFields(schema);

      els.app.innerHTML =
        UI.sectionHead((model && model.plural) || schema.plural || name, `${Number(result.total || 0)} records`, "Live data") +
        `
          <section class="toolbar">
          <input class="input" id="list-search" type="search" placeholder="Search records" value="${escapeHtml(state.search)}">
          <button class="btn btn-ghost" id="bulk-delete" ${schema.read_only ? "disabled" : ""}>Delete selected</button>
          <button class="btn btn-ghost" id="bulk-export">Export selected</button>
          <a class="btn btn-ghost" href="${API.exportURL(name)}" target="_blank" rel="noopener">Export CSV</a>
          ${schema.read_only ? "" : `<div class="status-chip" id="selection-hint">0 selected</div>`}
          <button class="btn btn-primary" id="list-new" ${schema.read_only ? "disabled" : ""}>New</button>
          </section>

          ${renderFilterBar(filterFields)}

          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  ${schema.read_only ? "" : "<th><input type='checkbox' id='check-all'></th>"}
                  ${columns.map((col) => sortableHeader(col)).join("")}
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody id="list-body">
                ${renderRows(result.items || [], columns, schema.read_only)}
              </tbody>
            </table>
          </div>

          <div class="pagination">
            <div class="status-chip">Page ${Number(result.page || 1)} of ${Math.max(1, Number(result.total_pages || 1))}</div>
            <div class="page-actions">
              <button class="btn btn-ghost btn-sm" id="page-prev" ${Number(result.page || 1) <= 1 ? "disabled" : ""}>Prev</button>
              <button class="btn btn-ghost btn-sm" id="page-next" ${Number(result.page || 1) >= Number(result.total_pages || 1) ? "disabled" : ""}>Next</button>
            </div>
          </div>
        `;

      bindListEvents(name, schema, result, columns);
    } catch (err) {
      els.app.innerHTML = renderRecoverableError("Could not load records", errorText(err), "retry-list");
      const retryBtn = document.getElementById("retry-list");
      if (retryBtn) {
        retryBtn.addEventListener("click", function () {
          renderList(name, { page: state.page, search: state.search });
        });
      }
      toast(errorText(err), "error");
    } finally {
      setAppBusy(false);
    }
  }

  function bindListEvents(name, schema, result, columns) {
    const searchInput = document.getElementById("list-search");
    const bulkDelete = document.getElementById("bulk-delete");
    const bulkExport = document.getElementById("bulk-export");
    const selectionHint = document.getElementById("selection-hint");
    const checkAll = document.getElementById("check-all");

    function syncSelectionState() {
      const selectedCount = state.selectedIDs.size;
      if (selectionHint) {
        selectionHint.textContent = `${selectedCount} selected`;
      }
      if (bulkDelete) {
        bulkDelete.disabled = schema.read_only || selectedCount === 0;
      }
      if (bulkExport) {
        bulkExport.disabled = selectedCount === 0;
      }
      if (checkAll) {
        const rowChecks = Array.from(document.querySelectorAll(".row-check"));
        const total = rowChecks.length;
        const selected = rowChecks.filter(function (cb) {
          return cb.checked;
        }).length;
        checkAll.checked = total > 0 && selected === total;
        checkAll.indeterminate = selected > 0 && selected < total;
      }
    }

    let timer = null;
    searchInput.addEventListener("input", function () {
      clearTimeout(timer);
      timer = setTimeout(async function () {
        state.search = searchInput.value.trim();
        await renderList(name, { page: 1, search: state.search });
      }, 260);
    });

    const newBtn = document.getElementById("list-new");
    if (newBtn) {
      newBtn.addEventListener("click", function () {
        navigate(`#/model/${name}/new`);
      });
    }

    const prev = document.getElementById("page-prev");
    if (prev) {
      prev.addEventListener("click", function () {
        renderList(name, { page: Math.max(1, Number(result.page || 1) - 1), search: state.search });
      });
    }

    const next = document.getElementById("page-next");
    if (next) {
      next.addEventListener("click", function () {
        renderList(name, { page: Number(result.page || 1) + 1, search: state.search });
      });
    }

    if (checkAll) {
      checkAll.addEventListener("change", function () {
        state.selectedIDs.clear();
        document.querySelectorAll(".row-check").forEach(function (cb) {
          cb.checked = checkAll.checked;
          if (checkAll.checked) {
            state.selectedIDs.add(cb.value);
          }
        });
        syncSelectionState();
      });
    }

    document.querySelectorAll(".row-check").forEach(function (cb) {
      cb.addEventListener("change", function () {
        if (cb.checked) {
          state.selectedIDs.add(cb.value);
        } else {
          state.selectedIDs.delete(cb.value);
        }
        syncSelectionState();
      });
    });

    if (bulkDelete) {
      bulkDelete.addEventListener("click", async function () {
        if (state.selectedIDs.size === 0) {
          toast("Select at least one record", "warning");
          return;
        }
        const accepted = await confirmAction(`Delete ${state.selectedIDs.size} selected record(s)?`);
        if (!accepted) {
          return;
        }
        const restore = setButtonPending(bulkDelete, "Deleting...");
        try {
          const ids = Array.from(state.selectedIDs).map(function (id) {
            return Number(id);
          });
          await API.bulkDelete(name, ids);
          toast("Records deleted", "success");
          await refreshModels(true);
          await renderList(name, { page: 1, search: state.search });
        } catch (err) {
          toast(errorText(err), "error");
        } finally {
          restore();
        }
      });
    }

    if (bulkExport) {
      bulkExport.addEventListener("click", async function () {
        if (state.selectedIDs.size === 0) {
          toast("Select at least one record", "warning");
          return;
        }
        const restore = setButtonPending(bulkExport, "Exporting...");
        try {
          const ids = Array.from(state.selectedIDs).map(function (id) {
            return Number(id);
          });
          const payload = await API.bulk(name, "export", ids);
          if (payload && payload.export_url) {
            window.open(payload.export_url, "_blank", "noopener");
            toast("Export started", "success");
            return;
          }
          const url = `${API.exportURL(name)}?ids=${encodeURIComponent(ids.join(","))}`;
          window.open(url, "_blank", "noopener");
          toast("Export started", "success");
        } catch (err) {
          toast(errorText(err), "error");
        } finally {
          restore();
        }
      });
    }
    syncSelectionState();

    document.querySelectorAll("[data-sort-col]").forEach(function (th) {
      th.addEventListener("click", function () {
        const col = th.getAttribute("data-sort-col");
        if (!col) {
          return;
        }

        if (state.sortColumn === col) {
          state.sortDir = state.sortDir === "asc" ? "desc" : "asc";
        } else {
          state.sortColumn = col;
          state.sortDir = "asc";
        }
        renderList(name, { page: 1, search: state.search });
      });
    });

    document.querySelectorAll("[data-filter-key]").forEach(function (input) {
      const key = input.getAttribute("data-filter-key");
      if (!key) {
        return;
      }
      input.addEventListener("change", function () {
        const value = String(input.value || "").trim();
        if (value === "") {
          delete state.filters[key];
        } else {
          state.filters[key] = value;
        }
        renderList(name, { page: 1, search: state.search });
      });
    });

    const clearFilters = document.getElementById("clear-filters");
    if (clearFilters) {
      clearFilters.addEventListener("click", function () {
        state.filters = {};
        renderList(name, { page: 1, search: state.search });
      });
    }

    document.querySelectorAll("[data-action='edit']").forEach(function (btn) {
      btn.addEventListener("click", function () {
        navigate(`#/model/${name}/${btn.getAttribute("data-id")}`);
      });
    });

    document.querySelectorAll("[data-action='delete']").forEach(function (btn) {
      btn.addEventListener("click", async function () {
        const id = btn.getAttribute("data-id");
        const accepted = await confirmAction(`Delete record #${id}?`);
        if (!accepted) {
          return;
        }
        try {
          await API.del(name, id);
          toast(`Record #${id} deleted`, "success");
          await refreshModels(true);
          await renderList(name, { page: 1, search: state.search });
        } catch (err) {
          toast(errorText(err), "error");
        }
      });
    });

    document.querySelectorAll("[data-action='view']").forEach(function (btn) {
      btn.addEventListener("click", function () {
        navigate(`#/model/${name}/${btn.getAttribute("data-id")}`);
      });
    });
  }

  function renderRows(items, columns, readOnly) {
    if (!items || items.length === 0) {
      const span = columns.length + (readOnly ? 1 : 2);
      return `<tr><td class="table-empty" colspan="${span}">No records found</td></tr>`;
    }

    return items
      .map(function (item) {
        const id = valueForID(item);
        const cells = columns
          .map(function (field) {
            const raw = valueFromItem(item, field);
            return `<td>${escapeHtml(formatValue(raw, field))}</td>`;
          })
          .join("");

        let actions;
        if (readOnly) {
          actions = `<button class="btn btn-ghost btn-sm" data-action="view" data-id="${escapeHtml(String(id))}">View</button>`;
        } else {
          actions = `
            <button class="btn btn-ghost btn-sm" data-action="edit" data-id="${escapeHtml(String(id))}">Edit</button>
            <button class="btn btn-danger btn-sm" data-action="delete" data-id="${escapeHtml(String(id))}">Delete</button>
          `;
        }

        const checkbox = readOnly
          ? ""
          : `<td><input class="row-check" type="checkbox" value="${escapeHtml(String(id))}" aria-label="Select row"></td>`;

        return `<tr>${checkbox}${cells}<td>${actions}</td></tr>`;
      })
      .join("");
  }

  async function renderForm(name, id) {
    state.currentModel = name;
    updateNewButton();
    setAppBusy(true);
    els.app.innerHTML = loadingMarkup();

    try {
      const schema = await API.schema(name);
      state.schema = schema;

      const editing = id !== null && id !== undefined;
      let record = {};
      if (editing) {
        record = await API.get(name, id);
      }

      const formFields = schema.fields
        .filter(function (field) {
          return !field.is_excluded && !field.is_pk;
        })
        .slice();

      const grouped = buildFormGroups(formFields);
      const useTabs = shouldUseTabs(formFields, grouped);
      const activeTab = grouped[0] ? grouped[0].key : "main";

      const tabHead = useTabs ? renderFormTabs(grouped, activeTab) : "";
      const panelMarkup = useTabs
        ? grouped
            .map(function (group, idx) {
              return renderTabPanel(group.key, idx === 0, renderFieldGrid(group.fields, record, editing));
            })
            .join("")
        : renderFieldGrid(formFields, record, editing);

      const detailPanels = editing ? renderDetailPanels(record) : "";

      els.app.innerHTML =
        UI.sectionHead(`${editing ? "Edit" : "New"} ${schema.name || name}`, schema.table || "", "Detail panel") +
        `
        ${detailPanels}
        <form class="form" id="record-form">
          ${tabHead}
          ${panelMarkup}
          <div class="form-actions">
            ${schema.read_only ? "" : `<button class="btn btn-primary" type="submit">${editing ? "Save changes" : "Create record"}</button>`}
            <button class="btn btn-ghost" type="button" id="form-cancel">Back</button>
          </div>
        </form>
      `;

      const cancelBtn = document.getElementById("form-cancel");
      cancelBtn.addEventListener("click", function () {
        navigate(`#/model/${name}`);
      });

      bindTabEvents();

      const form = document.getElementById("record-form");
      form.addEventListener("submit", async function (evt) {
        evt.preventDefault();
        const payload = collectFormPayload(form, schema.fields);
        const submitBtn = form.querySelector("button[type='submit']");
        const restore = setButtonPending(submitBtn, editing ? "Saving..." : "Creating...");

        try {
          if (editing) {
            await API.update(name, id, payload);
            toast("Record updated", "success");
          } else {
            await API.create(name, payload);
            toast("Record created", "success");
          }
          await refreshModels(true);
          navigate(`#/model/${name}`);
        } catch (err) {
          toast(errorText(err), "error");
        } finally {
          restore();
        }
      });
    } catch (err) {
      const retryID = "retry-form";
      els.app.innerHTML = renderRecoverableError("Could not load form", errorText(err), retryID);
      const retryBtn = document.getElementById(retryID);
      if (retryBtn) {
        retryBtn.addEventListener("click", function () {
          renderForm(name, id);
        });
      }
      toast(errorText(err), "error");
    } finally {
      setAppBusy(false);
    }
  }

  function buildFormGroups(fields) {
    const main = [];
    const attributes = [];
    const system = [];

    fields.forEach(function (field) {
      if (field.is_readonly) {
        system.push(field);
        return;
      }

      const looksRelated = !!field.is_fk || /_id$/i.test(field.column || field.name);
      const looksState = field.type === "bool" || (Array.isArray(field.choices) && field.choices.length > 0);

      if (looksRelated || looksState) {
        attributes.push(field);
      } else {
        main.push(field);
      }
    });

    const groups = [];
    if (main.length > 0) {
      groups.push({ key: "main", label: "Main", fields: main });
    }
    if (attributes.length > 0) {
      groups.push({ key: "attributes", label: "Attributes", fields: attributes });
    }
    if (system.length > 0) {
      groups.push({ key: "system", label: "System", fields: system });
    }
    if (groups.length === 0) {
      groups.push({ key: "all", label: "All", fields: fields });
    }
    return groups;
  }

  function shouldUseTabs(allFields, grouped) {
    return allFields.length >= 8 || grouped.length > 1;
  }

  function renderFormTabs(groups, activeTab) {
    return `
      <div class="tab-strip" role="tablist" aria-label="Form sections">
        ${groups
          .map(function (group) {
            const active = group.key === activeTab ? "active" : "";
            const selected = group.key === activeTab ? "true" : "false";
            const tabIdx = group.key === activeTab ? "0" : "-1";
            return `
              <button
                type="button"
                id="tab-${escapeHtml(group.key)}"
                class="tab-btn ${active}"
                data-tab-target="${escapeHtml(group.key)}"
                role="tab"
                aria-controls="panel-${escapeHtml(group.key)}"
                aria-selected="${selected}"
                tabindex="${tabIdx}"
              >
                ${escapeHtml(group.label)}
              </button>
            `;
          })
          .join("")}
      </div>
    `;
  }

  function renderTabPanel(key, active, inner) {
    return `
      <section
        id="panel-${escapeHtml(key)}"
        class="tab-panel ${active ? "active" : ""}"
        data-tab-panel="${escapeHtml(key)}"
        role="tabpanel"
        aria-labelledby="tab-${escapeHtml(key)}"
        ${active ? "" : "hidden"}
      >
        ${inner}
      </section>
    `;
  }

  function renderFieldGrid(fields, record, editing) {
    const rows = fields
      .map(function (field) {
        const value = valueFromItem(record, field);
        const readonly = editing && field.is_readonly;
        return renderFormField(field, value, readonly);
      })
      .join("");
    return `<div class="form-grid">${rows}</div>`;
  }

  function bindTabEvents() {
    const buttons = Array.from(document.querySelectorAll("[data-tab-target]"));
    if (buttons.length === 0) {
      return;
    }

    function activate(tabKey) {
      buttons.forEach(function (btn) {
        const active = btn.getAttribute("data-tab-target") === tabKey;
        btn.classList.toggle("active", active);
        btn.setAttribute("aria-selected", active ? "true" : "false");
        btn.setAttribute("tabindex", active ? "0" : "-1");
      });
      document.querySelectorAll("[data-tab-panel]").forEach(function (panel) {
        const active = panel.getAttribute("data-tab-panel") === tabKey;
        panel.classList.toggle("active", active);
        panel.hidden = !active;
      });
    }

    function byOffset(currentBtn, delta) {
      const idx = buttons.indexOf(currentBtn);
      if (idx < 0) {
        return buttons[0];
      }
      const next = (idx + delta + buttons.length) % buttons.length;
      return buttons[next];
    }

    buttons.forEach(function (btn) {
      btn.addEventListener("click", function () {
        activate(btn.getAttribute("data-tab-target"));
      });

      btn.addEventListener("keydown", function (evt) {
        let nextBtn = null;
        if (evt.key === "ArrowRight") {
          nextBtn = byOffset(btn, 1);
        } else if (evt.key === "ArrowLeft") {
          nextBtn = byOffset(btn, -1);
        } else if (evt.key === "Home") {
          nextBtn = buttons[0];
        } else if (evt.key === "End") {
          nextBtn = buttons[buttons.length - 1];
        } else if (evt.key === " " || evt.key === "Enter") {
          activate(btn.getAttribute("data-tab-target"));
          return;
        }

        if (!nextBtn) {
          return;
        }
        evt.preventDefault();
        activate(nextBtn.getAttribute("data-tab-target"));
        nextBtn.focus();
      });
    });
  }

  function renderDetailPanels(record) {
    const id = valueForID(record);
    const createdAt = pickFirstValue(record, ["created_at", "CreatedAt"]);
    const updatedAt = pickFirstValue(record, ["updated_at", "UpdatedAt"]);

    return `
      <section class="detail-grid">
        ${UI.kv("Record ID", id ? String(id) : "-")}
        ${UI.kv("Created", formatTemporal(createdAt))}
        ${UI.kv("Updated", formatTemporal(updatedAt))}
      </section>
    `;
  }

  function renderFormField(field, value, readonly) {
    const required = field.is_required ? "required" : "";
    const disabled = readonly ? "disabled" : "";
    const full = field.html_type === "textarea" ? " full" : "";
    const fieldName = escapeHtml(field.column || field.name);
    const label = `${escapeHtml(field.label || field.name)}${field.is_required ? " *" : ""}`;

    let control = "";

    if (field.choices && field.choices.length > 0) {
      const options = field.choices
        .map(function (choice) {
          const selected = String(choice.value) === String(value) ? "selected" : "";
          return `<option value="${escapeHtml(String(choice.value))}" ${selected}>${escapeHtml(choice.label)}</option>`;
        })
        .join("");
      control = `<select class="select" name="${fieldName}" ${required} ${disabled}><option value="">Select</option>${options}</select>`;
    } else if (field.html_type === "textarea") {
      control = `<textarea name="${fieldName}" ${required} ${disabled}>${escapeHtml(value === undefined || value === null ? "" : String(value))}</textarea>`;
    } else if (field.html_type === "checkbox") {
      const checked = value === true || value === "true" || value === 1 || value === "1" ? "checked" : "";
      control = `<input type="checkbox" name="${fieldName}" ${checked} ${disabled}>`;
    } else {
      const inputType = resolveInputType(field.html_type);
      control = `<input class="input" type="${inputType}" name="${fieldName}" value="${escapeHtml(value === undefined || value === null ? "" : String(value))}" ${required} ${disabled}>`;
    }

    return `<div class="form-group${full}"><label>${label}</label>${control}</div>`;
  }

  function collectFormPayload(form, fields) {
    const formData = new FormData(form);
    const payload = {};

    fields.forEach(function (field) {
      if (field.is_pk || field.is_excluded || field.is_readonly) {
        return;
      }

      const key = field.column || field.name;

      if (field.html_type === "checkbox") {
        const checkbox = form.querySelector(`[name='${cssEscape(key)}']`);
        payload[key] = !!(checkbox && checkbox.checked);
        return;
      }

      const raw = formData.get(key);
      if (raw === null) {
        return;
      }

      if (field.type === "int" || field.type === "int64" || field.type === "uint" || field.type === "uint64") {
        payload[key] = raw === "" ? null : Number(raw);
        return;
      }

      if (field.type === "float32" || field.type === "float64") {
        payload[key] = raw === "" ? null : Number(raw);
        return;
      }

      payload[key] = raw;
    });

    return payload;
  }

  function visibleListFields(schema) {
    const list = (schema.fields || []).filter(function (field) {
      return field.is_list && !field.is_excluded;
    });

    if (list.length > 0) {
      return list;
    }

    return (schema.fields || []).filter(function (field) {
      return !field.is_excluded;
    }).slice(0, 6);
  }

  function visibleFilterFields(schema) {
    return (schema.fields || []).filter(function (field) {
      if (field.is_excluded || field.is_pk) {
        return false;
      }
      return !!field.is_filter;
    });
  }

  function renderFilterBar(fields) {
    if (!fields || fields.length === 0) {
      return "";
    }

    const controls = fields.map(function (field) {
      return renderFilterControl(field);
    });

    return `
      <section class="toolbar filter-grid">
        ${controls.join("")}
        <button class="btn btn-ghost" id="clear-filters" type="button">Clear filters</button>
      </section>
    `;
  }

  function renderFilterControl(field) {
    const key = escapeHtml(runtimeColumn(field.column || field.name));
    const value = state.filters[runtimeColumn(field.column || field.name)] || "";
    const label = escapeHtml(field.label || field.name);

    if (Array.isArray(field.choices) && field.choices.length > 0) {
      const options = field.choices
        .map(function (choice) {
          const selected = String(choice.value) === String(value) ? "selected" : "";
          return `<option value="${escapeHtml(String(choice.value))}" ${selected}>${escapeHtml(choice.label)}</option>`;
        })
        .join("");

      return `
        <label class="filter-item">
          <span>${label}</span>
          <select class="select" data-filter-key="${key}">
            <option value="">Any</option>
            ${options}
          </select>
        </label>
      `;
    }

    if (field.type === "bool") {
      return `
        <label class="filter-item">
          <span>${label}</span>
          <select class="select" data-filter-key="${key}">
            <option value="">Any</option>
            <option value="1" ${value === "1" ? "selected" : ""}>True</option>
            <option value="0" ${value === "0" ? "selected" : ""}>False</option>
          </select>
        </label>
      `;
    }

    return `
      <label class="filter-item">
        <span>${label}</span>
        <input class="input" data-filter-key="${key}" value="${escapeHtml(value)}" placeholder="Exact match">
      </label>
    `;
  }

  function sortableHeader(col) {
    const rawCol = runtimeColumn(col.column || col.name);
    const active = state.sortColumn === rawCol;
    const marker = active ? (state.sortDir === "asc" ? "ASC" : "DESC") : "";
    const pressed = active ? "true" : "false";
    return `
      <th class="sortable">
        <button
          type="button"
          class="sort-btn"
          data-sort-col="${escapeHtml(rawCol)}"
          aria-pressed="${pressed}"
          aria-label="Sort by ${escapeHtml(col.label || col.name)}"
        >
          ${escapeHtml(col.label || col.name)}
          <span class="sort-indicator">${marker}</span>
        </button>
      </th>
    `;
  }

  function currentOrderBy() {
    if (!state.sortColumn) {
      return "";
    }
    return `${state.sortColumn} ${state.sortDir || "asc"}`;
  }

  function valueForID(item) {
    if (!item || typeof item !== "object") {
      return "";
    }
    return item.id || item.ID || item.i_d || "";
  }

  function valueFromItem(item, field) {
    if (!item || typeof item !== "object") {
      return "";
    }

    const candidates = [];
    if (field && field.column) {
      candidates.push(field.column);
    }
    if (field && field.name) {
      candidates.push(field.name);
      candidates.push(field.name.toLowerCase());
    }

    for (let i = 0; i < candidates.length; i++) {
      const key = candidates[i];
      if (Object.prototype.hasOwnProperty.call(item, key)) {
        return item[key];
      }
    }

    return "";
  }

  function pickFirstValue(item, keys) {
    if (!item || typeof item !== "object" || !Array.isArray(keys)) {
      return "";
    }
    for (let i = 0; i < keys.length; i++) {
      const key = keys[i];
      if (Object.prototype.hasOwnProperty.call(item, key)) {
        return item[key];
      }
    }
    return "";
  }

  function formatTemporal(value) {
    if (!value) {
      return "-";
    }
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) {
      return String(value);
    }
    return date.toLocaleString();
  }

  function formatValue(value, field) {
    if (value === null || value === undefined || value === "") {
      return "-";
    }

    if (typeof value === "boolean") {
      return value ? "Yes" : "No";
    }

    if (field && field.html_type === "datetime-local") {
      const date = new Date(value);
      if (!Number.isNaN(date.getTime())) {
        return date.toLocaleString();
      }
    }

    return String(value);
  }

  function updateNewButton() {
    if (!state.currentModel) {
      els.newRecordBtn.textContent = "New record";
      els.newRecordBtn.disabled = true;
      return;
    }
    els.newRecordBtn.disabled = false;
    els.newRecordBtn.textContent = `New ${state.currentModel}`;
  }

  function findModel(name) {
    return state.models.find(function (model) {
      return model.name === name;
    });
  }

  function loadingMarkup() {
    return UI.loading();
  }

  function navigate(hash) {
    window.location.hash = hash;
  }

  function openPalette() {
    overlayReturnFocus = document.activeElement;
    renderPalette(els.cmdkInput.value || "");
    els.cmdkModal.setAttribute("aria-hidden", "false");
    els.cmdkInput.setAttribute("aria-expanded", "true");
    els.cmdkModal.classList.remove("hidden");
    els.cmdkInput.focus();
    els.cmdkInput.select();
  }

  function closePalette() {
    if (els.cmdkModal.classList.contains("hidden")) {
      return;
    }
    els.cmdkModal.setAttribute("aria-hidden", "true");
    els.cmdkInput.setAttribute("aria-expanded", "false");
    els.cmdkInput.removeAttribute("aria-activedescendant");
    els.cmdkModal.classList.add("hidden");
    restoreOverlayFocus();
  }

  function renderPalette(query) {
    const q = (query || "").trim().toLowerCase();
    const items = [];

    items.push({
      label: "Go to dashboard",
      desc: "Overview",
      run: function () {
        navigate("#/");
      },
    });

    items.push({
      label: "Open sessions",
      desc: "Runtime telemetry",
      run: function () {
        navigate("#/sessions");
      },
    });

    if (state.currentModel) {
      items.push({
        label: `Create ${state.currentModel}`,
        desc: "Quick action",
        run: function () {
          navigate(`#/model/${state.currentModel}/new`);
        },
      });
    }

    state.models.forEach(function (model) {
      items.push({
        label: `Open ${model.name}`,
        desc: `${model.count || 0} records`,
        run: function () {
          navigate(`#/model/${model.name}`);
        },
      });
    });

    state.paletteItems = items.filter(function (item) {
      if (!q) {
        return true;
      }
      return `${item.label} ${item.desc}`.toLowerCase().includes(q);
    });

    state.paletteIndex = 0;

    if (state.paletteItems.length === 0) {
      els.cmdkInput.removeAttribute("aria-activedescendant");
      els.cmdkList.innerHTML = `<div class="table-empty">No command found</div>`;
      return;
    }

    els.cmdkList.innerHTML = state.paletteItems
      .map(function (item, idx) {
        const optionID = `cmdk-item-${idx}`;
        return `
          <button
            class="palette-item ${idx === state.paletteIndex ? "active" : ""}"
            data-palette-index="${idx}"
            id="${optionID}"
            type="button"
            role="option"
            aria-selected="${idx === state.paletteIndex ? "true" : "false"}"
          >
            <strong>${escapeHtml(item.label)}</strong><br>
            <small>${escapeHtml(item.desc)}</small>
          </button>
        `;
      })
      .join("");
    els.cmdkInput.setAttribute("aria-activedescendant", `cmdk-item-${state.paletteIndex}`);
  }

  function movePalette(delta) {
    if (state.paletteItems.length === 0) {
      return;
    }

    state.paletteIndex = (state.paletteIndex + delta + state.paletteItems.length) % state.paletteItems.length;

    els.cmdkList.querySelectorAll(".palette-item").forEach(function (node, idx) {
      const active = idx === state.paletteIndex;
      node.classList.toggle("active", active);
      node.setAttribute("aria-selected", active ? "true" : "false");
    });
    els.cmdkInput.setAttribute("aria-activedescendant", `cmdk-item-${state.paletteIndex}`);
  }

  function runPaletteItem(item) {
    if (!item || typeof item.run !== "function") {
      return;
    }
    closePalette();
    item.run();
  }

  function confirmAction(text) {
    overlayReturnFocus = document.activeElement;
    els.confirmText.textContent = text || "Are you sure?";
    els.confirmModal.setAttribute("aria-hidden", "false");
    els.confirmModal.classList.remove("hidden");
    els.confirmCancel.focus();

    return new Promise(function (resolve) {
      confirmResolver = resolve;
    });
  }

  function resolveConfirm(value) {
    if (confirmResolver) {
      confirmResolver(!!value);
      confirmResolver = null;
    }
    els.confirmModal.setAttribute("aria-hidden", "true");
    els.confirmModal.classList.add("hidden");
    restoreOverlayFocus();
  }

  function closeSidebarOnMobile() {
    if (window.matchMedia("(max-width: 1080px)").matches) {
      els.sidebar.classList.remove("open");
    }
  }

  function toast(message, type) {
    const el = document.createElement("div");
    el.className = `toast ${type || "success"}`;
    el.textContent = message;
    el.setAttribute("role", "status");
    els.toasts.appendChild(el);
    setTimeout(function () {
      el.remove();
    }, 2800);
  }

  function showFatal(err) {
    const retryID = "retry-bootstrap";
    els.app.innerHTML = renderRecoverableError("Unable to load admin", errorText(err), retryID);
    const retryBtn = document.getElementById(retryID);
    if (retryBtn) {
      retryBtn.addEventListener("click", bootstrap);
    }
  }

  function setAppBusy(value) {
    els.app.setAttribute("aria-busy", value ? "true" : "false");
  }

  function setButtonPending(button, label) {
    if (!button) {
      return function () {};
    }
    const previousText = button.textContent;
    button.disabled = true;
    if (label) {
      button.textContent = label;
    }
    return function () {
      button.disabled = false;
      button.textContent = previousText;
    };
  }

  function renderRecoverableError(title, message, retryID) {
    if (UI && typeof UI.error === "function") {
      return UI.error(title, message, "Retry", retryID);
    }
    return `
      <section class="error-state" role="alert">
        <h3>${escapeHtml(title || "Request failed")}</h3>
        <p>${escapeHtml(message || "Unexpected error.")}</p>
        <button class="btn btn-primary" type="button" id="${escapeHtml(retryID || "error-retry")}">Retry</button>
      </section>
    `;
  }

  function restoreOverlayFocus() {
    if (overlayReturnFocus && typeof overlayReturnFocus.focus === "function") {
      overlayReturnFocus.focus();
    }
    overlayReturnFocus = null;
  }

  function errorText(err) {
    return err && err.message ? err.message : "Unexpected error";
  }

  function resolveInputType(htmlType) {
    if (!htmlType) {
      return "text";
    }

    if (htmlType === "datetime-local" || htmlType === "email" || htmlType === "url" || htmlType === "number") {
      return htmlType;
    }

    return "text";
  }

  function escapeHtml(value) {
    if (UI && typeof UI.escapeHtml === "function") {
      return UI.escapeHtml(value);
    }
    const div = document.createElement("div");
    div.textContent = value === null || value === undefined ? "" : String(value);
    return div.innerHTML;
  }

  function cssEscape(value) {
    if (window.CSS && typeof window.CSS.escape === "function") {
      return window.CSS.escape(value);
    }
    return String(value).replace(/(['"\\.#:[\],=])/g, "\\$1");
  }

  function runtimeColumn(column) {
    if (column === "i_d") {
      return "id";
    }
    return column;
  }

  function createFallbackUI() {
    return {
      escapeHtml: function (value) {
        const div = document.createElement("div");
        div.textContent = value === null || value === undefined ? "" : String(value);
        return div.innerHTML;
      },
      sectionHead: function (title, subtitle, badge) {
        const badgeHTML = badge ? `<span class="status-chip">${this.escapeHtml(badge)}</span>` : "";
        return `
          <section class="section-head">
            <div>
              <h2 class="section-title">${this.escapeHtml(title || "")}</h2>
              <p class="section-subtitle">${this.escapeHtml(subtitle || "")}</p>
            </div>
            ${badgeHTML}
          </section>
        `;
      },
      loading: function () {
        return `
          <div class="loading-lines">
            <div class="loading-line"></div>
            <div class="loading-line"></div>
            <div class="loading-line"></div>
          </div>
        `;
      },
      empty: function (message) {
        return `<div class="table-empty">${this.escapeHtml(message || "No data")}</div>`;
      },
      error: function (title, message, actionLabel, actionID) {
        const btn = actionLabel
          ? `<button class="btn btn-primary" type="button" id="${this.escapeHtml(actionID || "error-retry")}">${this.escapeHtml(actionLabel)}</button>`
          : "";
        return `
          <section class="error-state" role="alert">
            <h3>${this.escapeHtml(title || "Request failed")}</h3>
            <p>${this.escapeHtml(message || "Unexpected error.")}</p>
            ${btn}
          </section>
        `;
      },
      kv: function (label, value) {
        return `
          <article class="detail-card">
            <p class="detail-label">${this.escapeHtml(label || "")}</p>
            <p class="detail-value">${this.escapeHtml(value || "-")}</p>
          </article>
        `;
      },
    };
  }

  init();
})();
