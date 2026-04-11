(function () {
  "use strict";

  const PAGE_SIZE = 25;
  const OVERVIEW_SERIES_POINTS = 24;
  const UI = window.AdminUI || createFallbackUI();
  const SIDEBAR_PREF_KEY = "goframe_admin_sidebar_collapsed";
  const THEME_PREF_KEY = "goframe_admin_theme";

  const els = {
    layout: document.getElementById("layout"),
    app: document.getElementById("app"),
    sidebar: document.getElementById("sidebar"),
    modelNav: document.getElementById("model-nav"),
    siteTitle: document.getElementById("site-title"),
    breadcrumbs: document.getElementById("breadcrumbs"),
    refreshBtn: document.getElementById("refresh-btn"),
    themeToggle: document.getElementById("theme-toggle"),
    themeToggleLabel: document.getElementById("theme-toggle-label"),
    newRecordBtn: document.getElementById("new-record-btn"),
    runtimeEnvPill: document.getElementById("runtime-env-pill"),
    runtimeEnv: document.getElementById("runtime-env"),
    menuToggle: document.getElementById("menu-toggle"),
    sidebarDockToggle: document.getElementById("sidebar-dock-toggle"),
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
    runtime: {},
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
    currentDBAlias: "",
    liveLimitRequests: 80,
    liveLimitSQL: 80,
    liveLimitSessions: 80,
    sessionsLimit: 200,
    liveNodeFilter: "",
    sidebarCollapsed: false,
    dataStudioSearch: "",
    dataStudioEngine: "all",
    traceURLTemplate: "",
    theme: "dark",
  };

  let sessionsRefreshTimer = null;
  let liveStreamSocket = null;
  let liveStreamRetryTimer = null;
  let liveStreamAttempts = 0;
  let liveStreamConnected = false;
  const liveStreamEvents = [];
  const LIVE_STREAM_EVENT_CAP = 40;
  const RUNTIME_QUEUE_ACK = "I_UNDERSTAND_RUNTIME_OPERATION";
  let confirmResolver = null;
  let overlayReturnFocus = null;
  const activeCharts = [];

  // Configure Chart.js global defaults
  if (typeof Chart !== "undefined") {
    Chart.defaults.font.family = '"Inter", "Segoe UI", sans-serif';
    Chart.defaults.font.size = 12;
    Chart.defaults.color = '#6f87aa';
    Chart.defaults.animation.duration = 600;
    Chart.defaults.animation.easing = 'easeOutQuart';
    Chart.defaults.responsive = true;
    Chart.defaults.maintainAspectRatio = false;
    Chart.defaults.plugins.legend.display = false;
    Chart.defaults.plugins.tooltip.backgroundColor = 'rgba(5, 12, 28, 0.92)';
    Chart.defaults.plugins.tooltip.titleColor = '#dce9ff';
    Chart.defaults.plugins.tooltip.bodyColor = '#95a9c8';
    Chart.defaults.plugins.tooltip.borderColor = 'rgba(28, 66, 117, 0.56)';
    Chart.defaults.plugins.tooltip.borderWidth = 1;
    Chart.defaults.plugins.tooltip.cornerRadius = 6;
    Chart.defaults.plugins.tooltip.padding = 10;
    Chart.defaults.plugins.tooltip.displayColors = true;
    Chart.defaults.plugins.tooltip.boxPadding = 4;
    Chart.defaults.elements.point.radius = 0;
    Chart.defaults.elements.point.hoverRadius = 5;
    Chart.defaults.elements.point.hoverBorderWidth = 2;
    Chart.defaults.elements.line.tension = 0.35;
    Chart.defaults.elements.line.borderWidth = 2.2;
  }

  function destroyAllCharts() {
    while (activeCharts.length > 0) {
      const chart = activeCharts.pop();
      try { chart.destroy(); } catch (_) {}
    }
  }

  function registerChart(chart) {
    if (chart) {
      activeCharts.push(chart);
    }
    return chart;
  }

  const API = (() => {
    const base = window.location.pathname.replace(/\/+$/, "");
    const root = base + "/api";
    const transientStatus = new Set([408, 425, 429, 500, 502, 503, 504]);
    const maxAttempts = 3;

    function withDB(path, dbAlias) {
      const alias = String(dbAlias || "").trim();
      if (!alias) {
        return path;
      }
      return path + (path.includes("?") ? "&" : "?") + "db=" + encodeURIComponent(alias);
    }

    async function req(path, opts) {
      let lastErr = null;
      for (let attempt = 1; attempt <= maxAttempts; attempt++) {
        try {
          const res = await fetch(root + path, {
            cache: "no-store",
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
      models: (mode) => req(`/models?stats=${mode === "full" ? "full" : "light"}`),
      schema: (name, dbAlias) => req(withDB(`/models/${encodeURIComponent(name)}/schema`, dbAlias)),
      list: (name, params, dbAlias) => {
        const search = new URLSearchParams(params || {});
        if (dbAlias) {
          search.set("db", String(dbAlias));
        }
        return req(`/models/${encodeURIComponent(name)}?${search.toString()}`);
      },
      get: (name, id, dbAlias) => req(withDB(`/models/${encodeURIComponent(name)}/${encodeURIComponent(id)}`, dbAlias)),
      create: (name, data, dbAlias) => req(withDB(`/models/${encodeURIComponent(name)}`, dbAlias), { method: "POST", body: JSON.stringify(data) }),
      update: (name, id, data, dbAlias) =>
        req(withDB(`/models/${encodeURIComponent(name)}/${encodeURIComponent(id)}`, dbAlias), { method: "PUT", body: JSON.stringify(data) }),
      del: (name, id, dbAlias) => req(withDB(`/models/${encodeURIComponent(name)}/${encodeURIComponent(id)}`, dbAlias), { method: "DELETE" }),
      bulk: (name, action, ids, dbAlias) =>
        req(withDB(`/models/${encodeURIComponent(name)}/bulk`, dbAlias), {
          method: "POST",
          body: JSON.stringify({ action: action, ids: ids }),
        }),
      bulkDelete: (name, ids, dbAlias) =>
        req(withDB(`/models/${encodeURIComponent(name)}/bulk`, dbAlias), {
          method: "POST",
          body: JSON.stringify({ action: "delete", ids: ids }),
        }),
      exportURL: (name, dbAlias) => `${root}${withDB(`/models/${encodeURIComponent(name)}/export`, dbAlias)}`,
      sessions: (limit) => req(`/sessions?limit=${encodeURIComponent(String(limit || 250))}`),
      liveSnapshot: (limits) => {
        const cfg = limits || {};
        const requestLimit = Number(cfg.requests || cfg.request || 50);
        const sqlLimit = Number(cfg.sql || cfg.queries || requestLimit || 50);
        const sessionsLimit = Number(cfg.sessions || requestLimit || 50);
        const search = new URLSearchParams({
          requests_limit: String(requestLimit > 0 ? requestLimit : 50),
          sql_limit: String(sqlLimit > 0 ? sqlLimit : 50),
          sessions_limit: String(sessionsLimit > 0 ? sessionsLimit : 50),
        });
        const node = String(cfg.node || "").trim();
        if (node) {
          search.set("node", node);
        }
        return req(`/live/snapshot?${search.toString()}`);
      },
      liveExcludes: () => req("/live/excludes"),
      addLiveExclude: (pattern) =>
        req("/live/excludes", {
          method: "POST",
          body: JSON.stringify({ pattern: String(pattern || "") }),
        }),
      deleteLiveExclude: (pattern) => req(`/live/excludes?pattern=${encodeURIComponent(String(pattern || ""))}`, { method: "DELETE" }),
      systemSnapshot: (envLimit) => req(`/system/snapshot?env_limit=${encodeURIComponent(String(envLimit || 200))}`),
      systemSetFlag: (name, enabled) =>
        req(`/system/flags/${encodeURIComponent(String(name || ""))}`, {
          method: "PUT",
          body: JSON.stringify({ enabled: !!enabled }),
        }),
      systemCreateFlag: (name, enabled) =>
        req(`/system/flags`, {
          method: "POST",
          body: JSON.stringify({ name: String(name || ""), enabled: !!enabled }),
        }),
      systemDeleteFlag: (name) =>
        req(`/system/flags/${encodeURIComponent(String(name || ""))}`, {
          method: "DELETE",
        }),
      systemQueueAction: (queue, action, payload) =>
        req(`/system/jobs/queues/${encodeURIComponent(String(queue || ""))}/actions/${encodeURIComponent(String(action || ""))}`, {
          method: "POST",
          body: JSON.stringify(payload || {}),
        }),
      liveWebSocketURL: function () {
        const protocol = window.location.protocol === "https:" ? "wss://" : "ws://";
        return protocol + window.location.host + root + "/live/ws";
      },
    };
  })();

  function init() {
    hydrateThemePreference();
    state.sidebarCollapsed = readSidebarPreference();
    applySidebarLayout();
    bindGlobalEvents();
    bootstrap();
  }

  function bootstrap() {
    refreshModels(true).then(onRoute).catch(showFatal);
  }

  function bindGlobalEvents() {
    window.addEventListener("hashchange", onRoute);
    window.addEventListener("resize", applySidebarLayout);

    els.menuToggle.addEventListener("click", function () {
      els.sidebar.classList.toggle("open");
    });

    if (els.sidebarDockToggle) {
      els.sidebarDockToggle.addEventListener("click", function () {
        state.sidebarCollapsed = !state.sidebarCollapsed;
        persistSidebarPreference(state.sidebarCollapsed);
        applySidebarLayout();
      });
    }

    els.refreshBtn.addEventListener("click", async function () {
      try {
        await refreshModels(true);
        await onRoute();
        toast("Data refreshed", "success");
      } catch (err) {
        toast(errorText(err), "error");
      }
    });

    if (els.themeToggle) {
      els.themeToggle.addEventListener("click", function () {
        const nextTheme = state.theme === "dark" ? "light" : "dark";
        applyTheme(nextTheme, true);
      });
    }

    els.newRecordBtn.addEventListener("click", function () {
      if (!state.currentModel) {
        toast("Select a model first", "warning");
        return;
      }
      navigate(modelHash(state.currentModel, currentDatabaseAlias(), "new"));
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

  async function refreshModels(quiet, mode) {
    const payload = await API.models(mode);
    state.models = payload.models || [];
    state.runtime = payload.runtime || {};
    state.traceURLTemplate = String((state.runtime && state.runtime.trace_url_template) || "").trim();
    if (!state.currentDBAlias || !isKnownDatabaseAlias(state.currentDBAlias)) {
      state.currentDBAlias = defaultDatabaseAlias();
    }
    els.siteTitle.textContent = payload.title || "GoFrame Admin";
    document.title = payload.title || "GoFrame Admin";
    updateRuntimeEnvironmentPill();
    renderModelNav();
    renderPalette(els.cmdkInput.value || "");
    updateNewButton();
    if (!quiet) {
      toast("Models updated", "success");
    }
  }

  function renderModelNav() {
    if (!els.modelNav) {
      return;
    }
    const runtime = state.runtime || {};
    const groups = Array.isArray(runtime.engine_groups) ? runtime.engine_groups : [];
    const fallbackDatabases = Array.isArray(runtime.databases) ? runtime.databases : [];

    let html = "";
    if (groups.length > 0) {
      html = groups
        .map(function (group) {
          const dbs = Array.isArray(group.databases) ? group.databases : [];
          const dbHTML = dbs
            .map(function (dbInfo) {
              const alias = String(dbInfo.alias || "").trim();
              const models = Array.isArray(dbInfo.model_entries) ? dbInfo.model_entries : [];
              const modelRows = models
                .map(function (modelEntry) {
                  const modelName = String(modelEntry.name || "").trim();
                  return `
                    <a href="${modelHash(modelName, alias)}" class="nav-link" data-nav="${escapeHtml(alias + ":" + modelName)}">
                      <span class="nav-icon nav-icon-glyph">${iconGlyph("model")}</span>
                      <span class="nav-label">${escapeHtml(modelEntry.plural || modelName)}</span>
                      ${renderModelCountBadge(modelEntry.count)}
                    </a>
                  `;
                })
                .join("");

              return `
                <details class="nav-db" ${dbInfo.is_default ? "open" : ""}>
                  <summary>
                    <span class="nav-db-mark">${iconGlyph("database")}</span>
                    <span class="nav-db-engine">${escapeHtml(group.name || dbInfo.engine || dbInfo.dialect || "engine")}</span>
                    <span class="nav-db-alias">${escapeHtml(alias || "default")}</span>
                    ${dbInfo.is_default ? '<span class="nav-db-default">default</span>' : ""}
                  </summary>
                  <div class="nav-db-models">
                    ${modelRows || '<div class="table-empty">No models discovered</div>'}
                  </div>
                </details>
              `;
            })
            .join("");
          return dbHTML;
        })
        .join("");
    } else if (fallbackDatabases.length > 0) {
      html = fallbackDatabases
        .map(function (dbInfo) {
          const alias = String(dbInfo.alias || "").trim();
          const modelNames = Array.isArray(dbInfo.models) ? dbInfo.models : [];
          const modelRows = modelNames
            .map(function (modelName) {
              const meta = state.models.find(function (row) {
                return row.name === modelName;
              }) || { plural: modelName, count: 0 };
              return `
                <a href="${modelHash(modelName, alias)}" class="nav-link" data-nav="${escapeHtml(alias + ":" + modelName)}">
                  <span class="nav-icon nav-icon-glyph">${iconGlyph("model")}</span>
                  <span class="nav-label">${escapeHtml(meta.plural || modelName)}</span>
                  ${renderModelCountBadge(meta.count)}
                </a>
              `;
            })
            .join("");
          return `
            <details class="nav-db" ${dbInfo.is_default ? "open" : ""}>
              <summary>
                <span class="nav-db-mark">${iconGlyph("database")}</span>
                <span class="nav-db-engine">${escapeHtml(dbInfo.dialect || dbInfo.engine || "engine")}</span>
                <span class="nav-db-alias">${escapeHtml(alias || "default")}</span>
                ${dbInfo.is_default ? '<span class="nav-db-default">default</span>' : ""}
              </summary>
              <div class="nav-db-models">
                ${modelRows || '<div class="table-empty">No models discovered</div>'}
              </div>
            </details>
          `;
        })
        .join("");
    } else {
      html = state.models
        .map(function (model) {
          const alias = defaultDatabaseAlias();
          return `
            <a href="${modelHash(model.name, alias)}" class="nav-link" data-nav="${escapeHtml(alias + ":" + model.name)}">
              <span class="nav-icon nav-icon-glyph">${iconGlyph("model")}</span>
              <span class="nav-label">${escapeHtml(model.plural || model.name)}</span>
              ${renderModelCountBadge(model.count)}
            </a>
          `;
        })
        .join("");
    }
    els.modelNav.innerHTML = html;
  }

  function renderModelCountBadge(value) {
    const num = Number(value);
    if (!Number.isFinite(num) || num < 0) {
      return `<span class="nav-badge nav-badge-soft">n/a</span>`;
    }
    return `<span class="nav-badge">${num}</span>`;
  }

  function isKnownCount(value) {
    const num = Number(value);
    return Number.isFinite(num) && num >= 0;
  }

  async function onRoute() {
    const route = parseRoute();
    const navKey =
      route.view === "sessions"
        ? "infra"
        : route.view === "data-studio"
          ? "data-studio"
        : route.view === "live"
          ? "network"
          : route.view === "system"
            ? "system"
            : route.model
              ? "data-studio"
              : "overview";
    setActiveNav(navKey);
    renderBreadcrumbs(route);
    stopLiveStream();
    destroyAllCharts();
    closeSidebarOnMobile();

    if (route.view === "dashboard") {
      stopSessionsAutoRefresh();
      state.currentModel = null;
      state.schema = null;
      updateNewButton();
      await renderDashboard();
      return;
    }

    if (route.view === "data-studio") {
      stopSessionsAutoRefresh();
      state.currentModel = null;
      state.schema = null;
      updateNewButton();
      renderDataStudio();
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

    if (route.view === "live") {
      stopSessionsAutoRefresh();
      state.currentModel = null;
      state.schema = null;
      updateNewButton();
      await renderLiveOverview();
      startLiveStream();
      return;
    }

    if (route.view === "system") {
      stopSessionsAutoRefresh();
      state.currentModel = null;
      state.schema = null;
      updateNewButton();
      await renderSystemOverview();
      return;
    }

    if (route.view === "list") {
      stopSessionsAutoRefresh();
      state.currentDBAlias = route.db || defaultDatabaseAlias();
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
      state.currentDBAlias = route.db || defaultDatabaseAlias();
      await renderForm(route.model, null);
      return;
    }

    if (route.view === "edit") {
      stopSessionsAutoRefresh();
      state.currentDBAlias = route.db || defaultDatabaseAlias();
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
    if (cleaned === "data-studio" || cleaned === "data") {
      return { view: "data-studio" };
    }
    if (cleaned === "live") {
      return { view: "live" };
    }
    if (cleaned === "system") {
      return { view: "system" };
    }

    const parts = cleaned.split("/").filter(Boolean);
    if (parts[0] !== "model" || !parts[1]) {
      return { view: "dashboard" };
    }

    let dbAlias = defaultDatabaseAlias();
    let model = decodeURIComponent(parts[1] || "");
    let offset = 1;
    if (parts.length >= 3 && isKnownDatabaseAlias(decodeURIComponent(parts[1] || ""))) {
      dbAlias = decodeURIComponent(parts[1] || "");
      model = decodeURIComponent(parts[2] || "");
      offset = 2;
    }

    if (parts.length === offset+1) {
      return {
        view: "list",
        model: model,
        db: dbAlias,
        page: Number(new URLSearchParams(window.location.search).get("page") || 1),
        search: "",
      };
    }

    if (parts[offset+1] === "new") {
      return { view: "new", model: model, db: dbAlias };
    }

    return { view: "edit", model: model, db: dbAlias, id: decodeURIComponent(parts[offset+1] || "") };
  }

  function renderBreadcrumbs(route) {
    const crumbs = [];
    crumbs.push(`<a class="crumb-link" href="#/">Overview</a>`);

    if (route.view === "sessions") {
      crumbs.push("/");
      crumbs.push(`<a class="crumb-link" href="#/sessions">Infra Manager</a>`);
    }
    if (route.view === "data-studio") {
      crumbs.push("/");
      crumbs.push(`<a class="crumb-link" href="#/data-studio">Data Studio</a>`);
    }
    if (route.view === "live") {
      crumbs.push("/");
      crumbs.push(`<a class="crumb-link" href="#/live">Network Inspector</a>`);
    }
    if (route.view === "system") {
      crumbs.push("/");
      crumbs.push(`<a class="crumb-link" href="#/system">System Pulse</a>`);
    }

    if (route.model) {
      crumbs.push("/");
      if (route.db) {
        crumbs.push(`<a class="crumb-link" href="${modelHash(route.model, route.db)}">${escapeHtml(route.db)}</a>`);
        crumbs.push("/");
      }
      crumbs.push(`<a class="crumb-link" href="${modelHash(route.model, route.db)}">${escapeHtml(route.model)}</a>`);
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

  async function renderDashboard() {
    setAppBusy(true);

    try {
      const runtime = state.runtime || {};
      const databases = Array.isArray(runtime.databases) ? runtime.databases : [];
      const engines = Array.isArray(runtime.engines) ? runtime.engines : [];
      const sessionsActive = toNumber(runtime.sessions_active, 0);
      const env = runtime.environment || "development";
      const countsAvailable = runtime.counts_available !== false;
      const recordsTotalKnown = isKnownCount(runtime.records_total);
      const recordsSummary = countsAvailable && recordsTotalKnown ? Number(runtime.records_total || 0).toLocaleString() : "Deferred";
      const liveSignals = await fetchOverviewLiveSignals();
      const clusterStatusLabel = liveSignals.clusterEnabled
        ? liveSignals.clusterConnected
          ? "connected"
          : "degraded"
        : "disabled";
      const sessionSeriesSummary = summarizeOverviewSeries(liveSignals.sessionSeries);
      const requestSeriesSummary = summarizeOverviewSeries(liveSignals.requestSeries);
      const activityRows = buildOverviewActivityRows(databases, liveSignals);
      const serviceRows = buildOverviewServiceRows(databases, env, liveSignals);

      const cards = state.models
        .map(function (model) {
          const alias = defaultDatabaseAlias();
          const byAlias = model.counts && alias ? model.counts[alias] : model.count;
          const knownCount = isKnownCount(byAlias);
          return `
            <article class="card" data-hash="${modelHash(model.name, alias)}">
              <p class="card-label">${escapeHtml(model.plural || model.name)}</p>
              <p class="card-count ${knownCount ? "" : "card-count-muted"}">${knownCount ? Number(byAlias) : "Deferred"}</p>
              <span class="status-chip">${escapeHtml(alias)}</span>
              ${knownCount ? "" : '<span class="status-chip status-chip-muted">counts disabled by default</span>'}
            </article>
          `;
        })
        .join("");

      els.app.innerHTML =
        UI.sectionHead("Overview", "Framework environment state", env) +
        `
          <section class="cards kpi-grid">
            <article class="card kpi-card card-static">
              <p class="card-label">Active models</p>
              <p class="kpi-value animate-number" data-value="${state.models.length}">0</p>
              <span class="status-chip">${engines.length} engines</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Active sessions</p>
              <p class="kpi-value animate-number" data-value="${sessionsActive}">0</p>
              <span class="status-chip">${env}</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Configured databases</p>
              <p class="kpi-value animate-number" data-value="${databases.length}">0</p>
              <span class="status-chip">${countsAvailable ? "counts ready" : "light mode"}</span>
            </article>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Live traffic signals</h3>
              <p>Source: /admin/api/sessions + /admin/api/live/snapshot</p>
            </div>
            <section class="toolbar overview-signal-toolbar">
              <span class="status-chip">Sessions realtime: latest ${sessionSeriesSummary.latest} · peak ${sessionSeriesSummary.peak}</span>
              <span class="status-chip">Requests/min: latest ${requestSeriesSummary.latest} · peak ${requestSeriesSummary.peak}</span>
              <span class="status-chip">Request buffer: ${liveSignals.requestBuffered}/${liveSignals.requestCapacity}</span>
              <span class="status-chip">Cluster relay: ${clusterStatusLabel}${liveSignals.clusterNodeID ? ` (${escapeHtml(liveSignals.clusterNodeID)})` : ""}</span>
              <span class="status-chip">Generated: ${escapeHtml(formatTemporal(liveSignals.generatedAt))}</span>
            </section>
            ${renderOverviewLiveTrend(liveSignals.sessionSeries, liveSignals.requestSeries)}
          </section>

          <section class="cards overview-meta-grid">
            <article class="section-block overview-panel">
              <div class="section-block-head">
                <h3>Recent activity</h3>
                <p>Runtime observed events</p>
              </div>
              <div class="table-wrap">
                <table class="table" style="width: 100%">
                  <tbody>
                    ${renderOverviewListRows(activityRows)}
                  </tbody>
                </table>
              </div>
            </article>
            <article class="section-block overview-panel">
              <div class="section-block-head">
                <h3>Services</h3>
                <p>General environment state</p>
              </div>
              <div class="table-wrap">
                <table class="table" style="width: 100%">
                  <tbody>
                    ${renderOverviewListRows(serviceRows)}
                  </tbody>
                </table>
              </div>
            </article>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Database runtime</h3>
              <p>Engines: ${escapeHtml(engines.join(", ") || "n/a")}</p>
            </div>
            <section class="cards dashboard-db-grid">
              ${renderRuntimeDatabaseCards(databases)}
            </section>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Registered models</h3>
              <p>Click shortcuts or open Data Studio for cross-engine operations</p>
            </div>
            <section class="cards">
              ${cards || UI.empty("No models registered")}
            </section>
          </section>
        `;

      els.app.querySelectorAll("[data-hash]").forEach(function (card) {
        card.addEventListener("click", function () {
          navigate(card.getAttribute("data-hash"));
        });
      });
      runNumberAnimations(els.app);
    } catch (err) {
      els.app.innerHTML = renderRecoverableError("Could not load overview", errorText(err), "retry-overview");
      const retryBtn = document.getElementById("retry-overview");
      if (retryBtn) {
        retryBtn.addEventListener("click", function () {
          renderDashboard();
        });
      }
      toast(errorText(err), "error");
    } finally {
      setAppBusy(false);
    }
  }

  function toNumber(value, fallback) {
    const n = Number(value);
    if (!Number.isFinite(n)) {
      return Number(fallback || 0);
    }
    return n;
  }

  async function fetchOverviewLiveSignals() {
    const sessionsPromise = API.sessions(220).catch(function () {
      return null;
    });
    const livePromise = API.liveSnapshot({
      requests: 240,
      sql: 1,
      sessions: 1,
      node: "",
    }).catch(function () {
      return null;
    });
    const settled = await Promise.all([sessionsPromise, livePromise]);
    const sessionsPayload = asOverviewPayload(settled[0], "telemetry");
    const livePayload = asOverviewPayload(settled[1], "requests");
    const sessionSeries = buildOverviewSessionSeries(sessionsPayload);
    const requestSeries = buildOverviewRequestSeries(livePayload);
    const requestBuffer = (livePayload && livePayload.request_buffer) || {};
    const cluster = (livePayload && livePayload.cluster) || {};

    return {
      sessionsAvailable: !!(sessionsPayload && sessionsPayload.enabled !== false),
      requestsAvailable: !!(livePayload && livePayload.enabled !== false),
      sessionSeries: sessionSeries,
      requestSeries: requestSeries,
      requestBuffered: Number(requestBuffer.stored || 0),
      requestCapacity: Number(requestBuffer.capacity || 0),
      clusterEnabled: !!cluster.enabled,
      clusterConnected: !!cluster.connected,
      clusterNodeID: String(cluster.node_id || "").trim(),
      clusterChannel: String(cluster.channel || "").trim(),
      clusterReason: String(cluster.reason || "").trim(),
      generatedAt: (livePayload && livePayload.generated_at) || (sessionsPayload && sessionsPayload.generated_at) || "",
    };
  }

  function asOverviewPayload(value, expectedKey) {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
      return null;
    }
    if (typeof Response !== "undefined" && value instanceof Response) {
      return null;
    }
    if (expectedKey && !Object.prototype.hasOwnProperty.call(value, expectedKey)) {
      return null;
    }
    return value;
  }

  function buildOverviewSessionSeries(payload) {
    if (!payload || payload.enabled === false) {
      return [];
    }
    const telemetry = payload.telemetry || {};
    const realtime = telemetry.realtime || {};
    const points = Array.isArray(realtime.points) ? realtime.points : [];
    const mapped = points
      .map(function (item) {
        return {
          timestamp: String(item.timestamp || ""),
          value: Number(item.active || 0),
        };
      })
      .filter(function (item) {
        return Number.isFinite(item.value);
      });
    if (mapped.length === 0) {
      return [];
    }
    return resampleOverviewSeries(mapped, OVERVIEW_SERIES_POINTS);
  }

  function buildOverviewRequestSeries(payload) {
    if (!payload || payload.enabled === false) {
      return [];
    }
    const rows = Array.isArray(payload.requests) ? payload.requests : [];
    const stamped = rows
      .map(function (row) {
        const ts = parseTemporalMillis(row && row.timestamp);
        return {
          ts: ts,
        };
      })
      .filter(function (item) {
        return Number.isFinite(item.ts);
      })
      .sort(function (a, b) {
        return a.ts - b.ts;
      });
    if (stamped.length === 0) {
      return [];
    }

    const bucketMS = 60 * 1000;
    const latest = stamped[stamped.length - 1].ts;
    const start = latest - (OVERVIEW_SERIES_POINTS - 1) * bucketMS;
    const buckets = new Array(OVERVIEW_SERIES_POINTS).fill(0);

    stamped.forEach(function (row) {
      if (row.ts < start || row.ts > latest) {
        return;
      }
      const rawIdx = Math.floor((row.ts - start) / bucketMS);
      const idx = Math.max(0, Math.min(OVERVIEW_SERIES_POINTS - 1, rawIdx));
      buckets[idx] += 1;
    });

    return buckets.map(function (count, idx) {
      const ts = new Date(start + idx * bucketMS);
      return {
        timestamp: ts.toISOString(),
        value: count,
      };
    });
  }

  function resampleOverviewSeries(points, targetSize) {
    const source = Array.isArray(points) ? points : [];
    const size = Number(targetSize || OVERVIEW_SERIES_POINTS);
    if (source.length === 0 || size <= 0) {
      return [];
    }
    if (source.length === size) {
      return source.slice();
    }
    if (source.length === 1) {
      return new Array(size).fill(0).map(function () {
        return {
          timestamp: source[0].timestamp,
          value: source[0].value,
        };
      });
    }

    const out = [];
    for (let idx = 0; idx < size; idx += 1) {
      const pos = (idx / Math.max(1, size - 1)) * (source.length - 1);
      const lo = Math.floor(pos);
      const hi = Math.min(source.length - 1, Math.ceil(pos));
      const ratio = pos - lo;
      const loPoint = source[lo];
      const hiPoint = source[hi];
      const value = Math.round(Number(loPoint.value || 0) + (Number(hiPoint.value || 0) - Number(loPoint.value || 0)) * ratio);
      out.push({
        timestamp: ratio <= 0.5 ? loPoint.timestamp : hiPoint.timestamp,
        value: value,
      });
    }
    return out;
  }

  function summarizeOverviewSeries(points) {
    const rows = Array.isArray(points) ? points : [];
    if (rows.length === 0) {
      return { latest: 0, peak: 0 };
    }
    const values = rows.map(function (item) {
      return Number(item.value || 0);
    });
    return {
      latest: Number(values[values.length - 1] || 0),
      peak: Math.max(0, ...values),
    };
  }

  function renderOverviewLiveTrend(sessionPoints, requestPoints) {
    const hasSessions = Array.isArray(sessionPoints) && sessionPoints.length > 0;
    const hasRequests = Array.isArray(requestPoints) && requestPoints.length > 0;
    if (!hasSessions && !hasRequests) {
      return `<div class="table-empty">No live telemetry available yet</div>`;
    }

    const baseCount = Math.max(hasSessions ? sessionPoints.length : 0, hasRequests ? requestPoints.length : 0, 2);
    const sessions = hasSessions ? resampleOverviewSeries(sessionPoints, baseCount) : [];
    const requests = hasRequests ? resampleOverviewSeries(requestPoints, baseCount) : [];

    const sessionSummary = summarizeOverviewSeries(sessions);
    const requestSummary = summarizeOverviewSeries(requests);
    const sessionAvg = sessions.length > 0
      ? Math.round(sessions.reduce(function (acc, item) { return acc + Number(item.value || 0); }, 0) / sessions.length)
      : 0;
    const requestAvg = requests.length > 0
      ? Math.round(requests.reduce(function (acc, item) { return acc + Number(item.value || 0); }, 0) / requests.length)
      : 0;

    const chartId = "overview-trend-canvas-" + Date.now();

    setTimeout(function () {
      const canvas = document.getElementById(chartId);
      if (!canvas || typeof Chart === "undefined") { return; }

      const bodyStyles = window.getComputedStyle(document.body);
      const gridColor = bodyStyles.getPropertyValue("--line").trim() || "rgba(148, 163, 184, 0.1)";
      const textColor = bodyStyles.getPropertyValue("--text-soft").trim() || "#94a3b8";
      const primaryColor = bodyStyles.getPropertyValue("--bg-accent").trim() || "#0ea5e9";

      const sessionLabels = (sessions.length > 0 ? sessions : requests).map(function (item) {
        return formatOverviewTimeLabel(item.timestamp);
      });

      const datasets = [];
      if (hasSessions) {
        datasets.push({
          label: "Sessions",
          data: sessions.map(function (item) { return Number(item.value || 0); }),
          borderColor: primaryColor,
          backgroundColor: function (ctx) {
            var chart = ctx.chart;
            var area = chart.chartArea;
            if (!area) { return "rgba(14, 165, 233, 0.15)"; }
            var gradient = chart.ctx.createLinearGradient(0, area.top, 0, area.bottom);
            // We use standard semitransparent blues since Canvas gradients require explicit RGBA/HEX
            gradient.addColorStop(0, "rgba(14, 165, 233, 0.3)");
            gradient.addColorStop(1, "rgba(14, 165, 233, 0.0)");
            return gradient;
          },
          fill: true,
          tension: 0.4, // smooth spline
          pointBackgroundColor: primaryColor,
          pointBorderColor: "#fff",
          pointHoverRadius: 6,
          pointHoverBackgroundColor: primaryColor,
          order: 2,
        });
      }
      if (hasRequests) {
        datasets.push({
          label: "Requests/min",
          data: requests.map(function (item) { return Number(item.value || 0); }),
          borderColor: "#10b981", // Professional emerald
          backgroundColor: "transparent",
          fill: false,
          tension: 0.4, // smooth spline
          pointBackgroundColor: "#059669",
          pointBorderColor: "#fff",
          pointHoverRadius: 6,
          pointHoverBackgroundColor: "#10b981",
          order: 1,
        });
      }

      var chart = new Chart(canvas, {
        type: "line",
        data: { labels: sessionLabels, datasets: datasets },
        options: {
          interaction: { mode: "index", intersect: false },
          scales: {
            x: {
              grid: { color: gridColor },
              ticks: { maxTicksLimit: 8, color: textColor, font: { family: '"Inter", sans-serif', size: 10 } },
            },
            y: {
              beginAtZero: true,
              grid: { color: gridColor },
              ticks: { color: textColor, font: { family: '"Inter", sans-serif', size: 10 }, precision: 0 },
            },
          },
          plugins: { legend: { display: false } },
        },
      });
      registerChart(chart);
    }, 0);

    return `
      <div class="overview-trend-wrap">
        <div class="chart-container overview-chart-container">
          <canvas id="${chartId}"></canvas>
        </div>
        <div class="overview-trend-legend">
          <span class="legend-item"><i class="legend-swatch is-session"></i>Sessions · latest ${sessionSummary.latest} · avg ${sessionAvg} · peak ${sessionSummary.peak}</span>
          <span class="legend-item"><i class="legend-swatch is-request"></i>Requests/min · latest ${requestSummary.latest} · avg ${requestAvg} · peak ${requestSummary.peak}</span>
        </div>
      </div>
    `;
  }

  function formatOverviewTimeLabel(value) {
    const ts = parseTemporalMillis(value);
    if (!Number.isFinite(ts)) {
      return "-";
    }
    return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  }

  function parseTemporalMillis(value) {
    const raw = String(value || "").trim();
    if (!raw) {
      return NaN;
    }
    const ts = Date.parse(raw);
    return Number.isFinite(ts) ? ts : NaN;
  }

  function buildOverviewActivityRows(databases, liveSignals) {
    const rows = [];
    if (Array.isArray(databases) && databases.length > 0) {
      const first = databases[0] || {};
      rows.push({
        label: `${String(first.alias || "default")} connected`,
        meta: `${Number(first.model_count || 0)} models`,
      });
    }
    if (liveSignals && liveSignals.sessionsAvailable) {
      const sessionSummary = summarizeOverviewSeries(liveSignals.sessionSeries);
      rows.push({
        label: "Session telemetry online",
        meta: `realtime peak ${sessionSummary.peak}`,
      });
    } else {
      rows.push({
        label: "Session telemetry pending",
        meta: "no data yet",
      });
    }
    if (liveSignals && liveSignals.requestsAvailable) {
      rows.push({
        label: "Network Inspector buffer",
        meta: `${Number(liveSignals.requestBuffered || 0)}/${Number(liveSignals.requestCapacity || 0)} requests`,
      });
    }
    rows.push({
      label: "Cluster relay",
      meta: (liveSignals && liveSignals.clusterEnabled)
        ? (liveSignals.clusterConnected ? "connected" : "degraded")
        : (liveSignals && liveSignals.clusterReason ? liveSignals.clusterReason : "disabled"),
    });
    rows.push({
      label: "Data Studio available",
      meta: `${state.models.length} models indexed`,
    });
    rows.push({
      label: "Security policies active",
      meta: "CSRF/session/runtime checks",
    });
    return rows;
  }

  function buildOverviewServiceRows(databases, env, liveSignals) {
    const aliasCount = Array.isArray(databases) ? databases.length : 0;
    const requestSummary = summarizeOverviewSeries((liveSignals && liveSignals.requestSeries) || []);
    const sessionsSummary = summarizeOverviewSeries((liveSignals && liveSignals.sessionSeries) || []);
    const clusterLabel = (liveSignals && liveSignals.clusterEnabled)
      ? (liveSignals.clusterConnected ? "connected" : "degraded")
      : "disabled";
    return [
      { label: "Admin API", meta: "99.9%" },
      { label: "Model Registry", meta: `${state.models.length} active` },
      { label: "Database Layer", meta: `${aliasCount} aliases` },
      { label: "Network Inspector", meta: (liveSignals && liveSignals.requestsAvailable) ? `latest ${requestSummary.latest}/min` : "waiting data" },
      { label: "Session Tracker", meta: (liveSignals && liveSignals.sessionsAvailable) ? `${sessionsSummary.latest} active` : "waiting data" },
      { label: "Cluster Relay", meta: clusterLabel },
      { label: "Runtime Env", meta: String(env || "development") },
    ];
  }

  function renderOverviewListRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td colspan="2" class="text-muted">No data</td></tr>`;
    }
    return rows
      .map(function (row) {
        return `
          <tr>
            <td style="font-weight: 600">${escapeHtml(row.label || "-")}</td>
            <td class="text-right text-muted">${escapeHtml(row.meta || "-")}</td>
          </tr>
        `;
      })
      .join("");
  }

  function renderDataStudio() {
    const rows = buildDataStudioRows(state.dataStudioSearch, state.dataStudioEngine);
    const allRows = buildDataStudioRows("", "all");
    const engines = availableDataStudioEngines();
    const aliases = new Set(
      allRows
        .map(function (row) {
          return String(row.db || "").trim();
        })
        .filter(Boolean)
    );
    const totalKnown = rows.reduce(function (acc, row) {
      return acc + (isKnownCount(row.count) ? Number(row.count || 0) : 0);
    }, 0);
    const hasDeferred = rows.some(function (row) {
      return !isKnownCount(row.count);
    });
    const activeRows = rows.filter(function (row) {
      return isKnownCount(row.count);
    }).length;
    const deferredRows = rows.length - activeRows;

    els.app.innerHTML =
      UI.sectionHead("Data Studio", "CRUD de modelos, filtros y exportación", `${rows.length} resultados`) +
      `
        <section class="cards kpi-grid data-studio-kpi-grid">
          <article class="card kpi-card card-static">
            <p class="card-label">Modelos visibles</p>
            <p class="kpi-value">${rows.length.toLocaleString()}</p>
            <span class="status-chip">${allRows.length.toLocaleString()} indexados</span>
          </article>
          <article class="card kpi-card card-static">
            <p class="card-label">Engines activos</p>
            <p class="kpi-value">${engines.length.toLocaleString()}</p>
            <span class="status-chip">${aliases.size.toLocaleString()} aliases</span>
          </article>
          <article class="card kpi-card card-static">
            <p class="card-label">Estado modelos</p>
            <p class="kpi-value">${activeRows.toLocaleString()}</p>
            <span class="status-chip">${deferredRows.toLocaleString()} diferidos</span>
          </article>
          <article class="card kpi-card card-static">
            <p class="card-label">Registros visibles</p>
            <p class="kpi-value">${formatDataStudioTotal(totalKnown, hasDeferred)}</p>
            <span class="status-chip">${hasDeferred ? "Light mode" : "Counts completos"}</span>
          </article>
        </section>

        <section class="section-block data-studio-shell">
          <section class="toolbar data-studio-toolbar">
            <input id="data-studio-search" class="input" type="search" placeholder="Buscar modelo, tabla, engine o alias..." value="${escapeHtml(state.dataStudioSearch || "")}">
            <select id="data-studio-engine" class="select">
              ${renderDataStudioEngineOptions(engines, state.dataStudioEngine)}
            </select>
            <button class="btn btn-ghost" type="button" id="data-studio-clear">Clear</button>
            <button class="btn btn-primary" type="button" id="data-studio-load-counts">Compute counts</button>
          </section>

          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Model</th>
                  <th>Engine</th>
                  <th>Database alias</th>
                  <th>Table</th>
                  <th>Records</th>
                  <th>Status</th>
                  <th>Actions</th>
                </tr>
              </thead>
              <tbody id="data-studio-body">
                ${renderDataStudioRows(rows)}
              </tbody>
            </table>
          </div>

          <section class="toolbar data-studio-footer">
            <span class="status-chip">${rows.length} / ${allRows.length} modelos visibles</span>
            <span class="status-chip">Total registros: ${formatDataStudioTotal(totalKnown, hasDeferred)}</span>
            <span class="status-chip">Engines: ${engines.length} | Aliases: ${aliases.size}</span>
            ${hasDeferred ? '<span class="status-chip status-chip-muted">Light mode: algunos recuentos están diferidos</span>' : ""}
          </section>
        </section>
      `;

    const searchInput = document.getElementById("data-studio-search");
    const engineSelect = document.getElementById("data-studio-engine");
    const clearBtn = document.getElementById("data-studio-clear");
    const loadCountsBtn = document.getElementById("data-studio-load-counts");
    const body = document.getElementById("data-studio-body");

    if (searchInput) {
      searchInput.addEventListener("input", function () {
        state.dataStudioSearch = String(searchInput.value || "");
        renderDataStudio();
      });
    }
    if (engineSelect) {
      engineSelect.addEventListener("change", function () {
        state.dataStudioEngine = String(engineSelect.value || "all");
        renderDataStudio();
      });
    }
    if (clearBtn) {
      clearBtn.addEventListener("click", function () {
        state.dataStudioSearch = "";
        state.dataStudioEngine = "all";
        renderDataStudio();
      });
    }
    if (loadCountsBtn) {
      loadCountsBtn.addEventListener("click", async function () {
        const restore = setButtonPending(loadCountsBtn, "Computing...");
        try {
          await refreshModels(true, "full");
          toast("Counts loaded", "success");
          renderDataStudio();
        } catch (err) {
          toast(errorText(err), "error");
        } finally {
          restore();
        }
      });
    }
    if (body) {
      body.addEventListener("click", function (evt) {
        const btn = evt.target.closest("[data-ds-action]");
        if (!btn) {
          return;
        }
        const action = String(btn.getAttribute("data-ds-action") || "");
        const model = decodeURIComponent(String(btn.getAttribute("data-ds-model") || ""));
        const dbAlias = decodeURIComponent(String(btn.getAttribute("data-ds-db") || ""));
        if (!model) {
          return;
        }
        if (action === "open") {
          navigate(modelHash(model, dbAlias));
          return;
        }
        if (action === "new") {
          navigate(modelHash(model, dbAlias, "new"));
          return;
        }
        if (action === "export") {
          window.open(API.exportURL(model, dbAlias), "_blank", "noopener");
        }
      });
    }
  }

  function buildDataStudioRows(searchQuery, engineFilter) {
    const runtime = state.runtime || {};
    const groups = Array.isArray(runtime.engine_groups) ? runtime.engine_groups : [];
    const fallbackDatabases = Array.isArray(runtime.databases) ? runtime.databases : [];
    const rows = [];
    const lookup = {};

    state.models.forEach(function (item) {
      lookup[item.name] = item;
    });

    if (groups.length > 0) {
      groups.forEach(function (group) {
        const engineName = String(group.name || "engine").trim();
        if (engineFilter && engineFilter !== "all" && engineFilter !== engineName) {
          return;
        }
        const dbs = Array.isArray(group.databases) ? group.databases : [];
        dbs.forEach(function (dbInfo) {
          const alias = String(dbInfo.alias || defaultDatabaseAlias()).trim();
          const modelEntries = Array.isArray(dbInfo.model_entries) ? dbInfo.model_entries : [];
          if (modelEntries.length > 0) {
            modelEntries.forEach(function (entry) {
              const modelName = String(entry.name || "").trim();
              if (!modelName) {
                return;
              }
              rows.push({
                model: modelName,
                plural: String(entry.plural || modelName).trim(),
                engine: engineName,
                db: alias || defaultDatabaseAlias(),
                table: String(entry.table || (lookup[modelName] && lookup[modelName].table) || "").trim(),
                count: entry.count,
                isDefault: !!dbInfo.is_default,
              });
            });
            return;
          }

          const modelNames = Array.isArray(dbInfo.models) ? dbInfo.models : [];
          modelNames.forEach(function (name) {
            const modelName = String(name || "").trim();
            if (!modelName) {
              return;
            }
            const modelMeta = lookup[modelName] || {};
            let count = modelMeta.count;
            if (modelMeta.counts && Object.prototype.hasOwnProperty.call(modelMeta.counts, alias)) {
              count = modelMeta.counts[alias];
            }
            rows.push({
              model: modelName,
              plural: String(modelMeta.plural || modelName).trim(),
              engine: engineName,
              db: alias || defaultDatabaseAlias(),
              table: String(modelMeta.table || "").trim(),
              count: count,
              isDefault: !!dbInfo.is_default,
            });
          });
        });
      });
    } else if (fallbackDatabases.length > 0) {
      fallbackDatabases.forEach(function (dbInfo) {
        const engineName = String(dbInfo.dialect || dbInfo.engine || "engine").trim();
        if (engineFilter && engineFilter !== "all" && engineFilter !== engineName) {
          return;
        }
        const alias = String(dbInfo.alias || defaultDatabaseAlias()).trim();
        const modelNames = Array.isArray(dbInfo.models) ? dbInfo.models : [];
        modelNames.forEach(function (name) {
          const modelName = String(name || "").trim();
          if (!modelName) {
            return;
          }
          const modelMeta = lookup[modelName] || {};
          let count = modelMeta.count;
          if (modelMeta.counts && Object.prototype.hasOwnProperty.call(modelMeta.counts, alias)) {
            count = modelMeta.counts[alias];
          }
          rows.push({
            model: modelName,
            plural: String(modelMeta.plural || modelName).trim(),
            engine: engineName,
            db: alias || defaultDatabaseAlias(),
            table: String(modelMeta.table || "").trim(),
            count: count,
            isDefault: !!dbInfo.is_default,
          });
        });
      });
    } else {
      const defaultAlias = defaultDatabaseAlias();
      state.models.forEach(function (model) {
        rows.push({
          model: String(model.name || "").trim(),
          plural: String(model.plural || model.name || "").trim(),
          engine: "engine",
          db: defaultAlias,
          table: String(model.table || "").trim(),
          count: model.count,
          isDefault: true,
        });
      });
    }

    const term = String(searchQuery || "").trim().toLowerCase();
    const filtered = rows.filter(function (row) {
      if (!term) {
        return true;
      }
      return `${row.model} ${row.plural} ${row.engine} ${row.db} ${row.table}`.toLowerCase().includes(term);
    });

    filtered.sort(function (a, b) {
      if (a.engine !== b.engine) {
        return a.engine.localeCompare(b.engine);
      }
      if (a.db !== b.db) {
        return a.db.localeCompare(b.db);
      }
      return a.model.localeCompare(b.model);
    });
    return filtered;
  }

  function availableDataStudioEngines() {
    const runtime = state.runtime || {};
    const groups = Array.isArray(runtime.engine_groups) ? runtime.engine_groups : [];
    const engines = new Set();
    groups.forEach(function (group) {
      const name = String(group.name || "").trim();
      if (name) {
        engines.add(name);
      }
    });
    if (engines.size === 0) {
      const runtimeEngines = Array.isArray(runtime.engines) ? runtime.engines : [];
      runtimeEngines.forEach(function (item) {
        const name = String(item || "").trim();
        if (name) {
          engines.add(name);
        }
      });
    }
    return Array.from(engines).sort();
  }

  function renderDataStudioEngineOptions(engines, selected) {
    const items = ["all"].concat(Array.isArray(engines) ? engines : []);
    const current = String(selected || "all");
    return items
      .map(function (engine) {
        const label = engine === "all" ? "All engines" : engine;
        return `<option value="${escapeHtml(engine)}" ${engine === current ? "selected" : ""}>${escapeHtml(label)}</option>`;
      })
      .join("");
  }

  function renderDataStudioRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="7">No models found for this filter</td></tr>`;
    }
    return rows
      .map(function (row) {
        const countKnown = isKnownCount(row.count);
        const statusClass = countKnown ? "badge-success" : "badge-warning";
        const statusLabel = countKnown ? "active" : "deferred";
        const encodedModel = encodeURIComponent(String(row.model || ""));
        const encodedDB = encodeURIComponent(String(row.db || defaultDatabaseAlias()));
        return `
          <tr>
            <td>
              <div class="data-studio-model-cell">
                <strong>${escapeHtml(row.model || "-")}</strong>
                <span class="status-chip status-chip-muted">${escapeHtml(row.plural || row.model || "-")}</span>
              </div>
            </td>
            <td>${escapeHtml(row.engine || "-")}</td>
            <td>${escapeHtml(row.db || "-")}${row.isDefault ? ' <span class="status-chip status-chip-muted">default</span>' : ""}</td>
            <td><code>${escapeHtml(row.table || "-")}</code></td>
            <td>${countKnown ? Number(row.count || 0).toLocaleString() : '<span class="status-chip status-chip-muted">deferred</span>'}</td>
            <td><span class="badge ${statusClass}">${statusLabel}</span></td>
            <td>
              <div class="data-studio-actions">
                <button type="button" class="btn btn-ghost btn-sm" data-ds-action="open" data-ds-model="${encodedModel}" data-ds-db="${encodedDB}">Open</button>
                <button type="button" class="btn btn-ghost btn-sm" data-ds-action="new" data-ds-model="${encodedModel}" data-ds-db="${encodedDB}">New</button>
                <button type="button" class="btn btn-ghost btn-sm" data-ds-action="export" data-ds-model="${encodedModel}" data-ds-db="${encodedDB}">Export</button>
              </div>
            </td>
          </tr>
        `;
      })
      .join("");
  }

  function formatDataStudioTotal(totalKnown, hasDeferred) {
    const value = Number(totalKnown || 0).toLocaleString();
    if (!hasDeferred) {
      return value;
    }
    return value + "+";
  }

  function renderRuntimeDatabaseCards(databases) {
    if (!Array.isArray(databases) || databases.length === 0) {
      return UI.empty("No database aliases configured");
    }
    return databases
      .map(function (dbInfo) {
        const models = Array.isArray(dbInfo.models) ? dbInfo.models : [];
        const modelChips = models
          .slice(0, 6)
          .map(function (name) {
            return `<span class="status-chip">${escapeHtml(name)}</span>`;
          })
          .join("");
        const extra = models.length > 6 ? `<span class="status-chip">+${models.length - 6}</span>` : "";
        return `
          <article class="card card-static">
            <p class="card-label">${escapeHtml(dbInfo.alias || "default")}${dbInfo.is_default ? " (default)" : ""}</p>
            <p class="runtime-db-engine">${escapeHtml(dbInfo.dialect || dbInfo.engine || "unknown")}</p>
            <div class="runtime-db-meta">
              <span class="status-chip">Engine: ${escapeHtml(dbInfo.engine || "sql")}</span>
              <span class="status-chip">Models: ${Number(dbInfo.model_count || models.length)}</span>
            </div>
            <div class="runtime-db-models">
              ${modelChips || `<span class="status-chip">No models</span>`}
              ${extra}
            </div>
          </article>
        `;
      })
      .join("");
  }

  async function renderSessionsOverview() {
    const existingGrid = els.app.querySelector('.sessions-kpi-grid');
    if (!existingGrid) {
      setAppBusy(true);
      els.app.innerHTML = loadingMarkup();
    } else {
      setAppBusy(true); // show linear loading overlay without destroying layout
    }

    try {
      const requestedLimit = Number(state.sessionsLimit || 200);
      const normalizedLimit = Number.isFinite(requestedLimit) && requestedLimit > 0 ? requestedLimit : 200;
      const payload = await API.sessions(normalizedLimit);
      if (!payload || payload.enabled === false) {
        const reason = (payload && payload.reason) || "Session telemetry is not available.";
        els.app.innerHTML =
          UI.sectionHead("Infra Manager", "Session telemetry and runtime identity", "Unavailable") +
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
      const sourceRuntime = {
        pod: payload.source_pod || "",
        host: payload.source_host || "",
        instance: payload.source_instance || "",
      };
      const sourcePod = normalizeSessionPod(sourceRuntime);
      const sourceHost = normalizeSessionHost(sourceRuntime);
      const currentActive = Number(payload.current_active || 0);
      const active5m = Number(payload.active_last_5m || 0);
      const active1h = Number(payload.active_last_hour || 0);
      const includedRows = Number(payload.included_rows || sessionRows.length || 0);
      const selectedLimit = Number(state.sessionsLimit || normalizedLimit || 200);

      // Avoid micro-cuts by surgically updating DOM if it's already mounted
      if (existingGrid) {
        const vals = existingGrid.querySelectorAll('.kpi-value');
        if (vals.length >= 4) {
          vals[0].setAttribute("data-value", currentActive);
          vals[1].setAttribute("data-value", active5m);
          vals[2].setAttribute("data-value", active1h);
          vals[3].setAttribute("data-value", includedRows);
        }
        const details = els.app.querySelector('.detail-grid');
        if (details) {
          details.innerHTML = 
            UI.kv("Current active", String(currentActive)) +
            UI.kv("Active (last 5m)", String(active5m)) +
            UI.kv("Active (last hour)", String(active1h)) +
            UI.kv("Store", payload.store || "memory") +
            UI.kv("Runtime", payload.source_runtime || "-") +
            UI.kv("Environment", payload.source_env || "-") +
            UI.kv("Source instance", payload.source_instance || "-") +
            UI.kv("Source pod", sourcePod || "-") +
            UI.kv("Source host", sourceHost || "-");
        }
        const tbody = els.app.querySelector('.table-wrap tbody');
        if (tbody) {
          tbody.innerHTML = renderSessionRows(sessionRows);
        }
        const chartsGrid = els.app.querySelector('.session-chart-grid');
        if (chartsGrid) {
          updateSessionChartCard("chart-rt", realtime.points || []);
          updateSessionChartCard("chart-1h", lastHour.points || []);
          updateSessionChartCard("chart-day", today.points || []);
        }
        runNumberAnimations(els.app);
        setAppBusy(false);
        return;
      }

      els.app.innerHTML =
        UI.sectionHead("Infra Manager", `${currentActive} active sessions`, "Live telemetry") +
        `
          <section class="cards kpi-grid sessions-kpi-grid">
            <article class="card kpi-card card-static">
              <p class="card-label">Active now</p>
              <p class="kpi-value animate-number" data-value="${currentActive}">0</p>
              <span class="status-chip">store: ${escapeHtml(payload.store || "memory")}</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Last 5 minutes</p>
              <p class="kpi-value animate-number" data-value="${active5m}">0</p>
              <span class="status-chip">session churn</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Last hour</p>
              <p class="kpi-value animate-number" data-value="${active1h}">0</p>
              <span class="status-chip">stability signal</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Rows loaded</p>
              <p class="kpi-value animate-number" data-value="${includedRows}">0</p>
              <span class="status-chip">limit ${selectedLimit}</span>
            </article>
          </section>

          <section class="detail-grid">
            ${UI.kv("Current active", String(currentActive))}
            ${UI.kv("Active (last 5m)", String(active5m))}
            ${UI.kv("Active (last hour)", String(active1h))}
            ${UI.kv("Store", payload.store || "memory")}
            ${UI.kv("Runtime", payload.source_runtime || "-")}
            ${UI.kv("Environment", payload.source_env || "-")}
            ${UI.kv("Source instance", payload.source_instance || "-")}
            ${UI.kv("Source pod", sourcePod || "-")}
            ${UI.kv("Source host", sourceHost || "-")}
          </section>

          <section class="cards session-chart-grid">
            ${renderSessionChartCard("chart-rt", "Real time", "10-minute rolling active sessions", realtime.points || [])}
            ${renderSessionChartCard("chart-1h", "Last hour", "Hourly stability signal", lastHour.points || [])}
            ${renderSessionChartCard("chart-day", "Today", "Active sessions by current day", today.points || [])}
          </section>

          <section class="toolbar toolbar-panel sessions-controls-row">
            <label for="sessions-limit-select">Show latest</label>
            <select id="sessions-limit-select" class="select">
              ${renderLiveLimitOptions(selectedLimit, "sessions")}
            </select>
            <button class="btn btn-ghost btn-sm" id="sessions-limit-apply" type="button">Apply</button>
            <div class="status-chip">Generated: ${escapeHtml(formatTemporal(payload.generated_at))}</div>
            ${payload.truncated_by_limit ? `<div class="status-chip">Showing first ${includedRows} sessions</div>` : ""}
            <button class="btn btn-ghost" id="sessions-refresh" type="button">Refresh now</button>
          </section>

          <div class="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Session</th>
                  <th>User</th>
                  <th>Pod (k8s)</th>
                  <th>Host/Node</th>
                  <th>IP</th>
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
      const limitSelect = document.getElementById("sessions-limit-select");
      const limitApply = document.getElementById("sessions-limit-apply");
      const applyLimit = function () {
        if (!limitSelect) {
          return;
        }
        const value = Number(limitSelect.value || state.sessionsLimit || 200);
        if (!Number.isFinite(value) || value <= 0) {
          toast("Invalid sessions limit value", "warning");
          return;
        }
        state.sessionsLimit = value;
        renderSessionsOverview();
      };
      if (limitSelect) {
        limitSelect.addEventListener("change", applyLimit);
      }
      if (limitApply) {
        limitApply.addEventListener("click", applyLimit);
      }
      runNumberAnimations(els.app);
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

  async function renderLiveOverview() {
    setAppBusy(true);

    try {
      const payload = await API.liveSnapshot({
        requests: state.liveLimitRequests,
        sql: state.liveLimitSQL,
        sessions: state.liveLimitSessions,
        node: state.liveNodeFilter,
      });
      const stream = payload.stream || {};
      const requests = Array.isArray(payload.requests) ? payload.requests : [];
      const queries = Array.isArray(payload.queries) ? payload.queries : [];
      const sessions = Array.isArray(payload.sessions) ? payload.sessions : [];
      const nodes = Array.isArray(payload.nodes) ? payload.nodes : [];
      const cluster = payload.cluster || {};
      const excludePatterns = Array.isArray(payload.exclude_patterns) ? payload.exclude_patterns : [];
      const buffer = payload.request_buffer || {};
      const sqlBuffer = payload.sql_buffer || {};
      state.liveLimitRequests = Number(payload.request_limit || state.liveLimitRequests || 80);
      state.liveLimitSQL = Number(payload.sql_limit || state.liveLimitSQL || 80);
      state.liveLimitSessions = Number(payload.session_limit || state.liveLimitSessions || 80);
      state.liveNodeFilter = String(payload.node_filter || state.liveNodeFilter || "").trim();
      state.traceURLTemplate = String(payload.trace_url_template || state.traceURLTemplate || "").trim();
      const liveNodes = collectLiveNodeOptions(payload, liveStreamEvents);
      const clusterEnabled = !!cluster.enabled;
      const clusterConnected = !!cluster.connected;
      const clusterStatusLabel = clusterEnabled
        ? clusterConnected
          ? "Connected"
          : "Degraded"
        : "Disabled";
      const clusterStatusClass = clusterEnabled
        ? clusterConnected
          ? "badge-success"
          : "badge-warning"
        : "badge-neutral";
      const nodeFilterLabel = state.liveNodeFilter ? state.liveNodeFilter : "All nodes";
      const bufferedRequests = Number(buffer.stored || requests.length || 0);
      const bufferedSQL = Number(sqlBuffer.stored || queries.length || 0);
      const trackedSessions = Number(stream.tracked_sessions || sessions.length || 0);
      const droppedEvents = Number(stream.dropped || 0);

      els.app.innerHTML =
        UI.sectionHead("Network Inspector", "HTTP and SQL stream in real time", liveStreamConnected ? "Stream connected" : "Stream offline") +
        `
          <section class="cards kpi-grid live-kpi-grid">
            <article class="card kpi-card card-static">
              <p class="card-label">HTTP buffered</p>
              <p class="kpi-value animate-number" data-value="${bufferedRequests}">0</p>
              <span class="status-chip">limit ${Number(buffer.capacity || requests.length || 0)}</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">SQL buffered</p>
              <p class="kpi-value animate-number" data-value="${bufferedSQL}">0</p>
              <span class="status-chip">limit ${Number(sqlBuffer.capacity || queries.length || 0)}</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Tracked sessions</p>
              <p class="kpi-value animate-number" data-value="${trackedSessions}">0</p>
              <span class="status-chip">scope ${escapeHtml(nodeFilterLabel)}</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Dropped events</p>
              <p class="kpi-value animate-number" data-value="${droppedEvents}">0</p>
              <span class="badge ${clusterStatusClass}">relay ${escapeHtml(clusterStatusLabel)}</span>
            </article>
          </section>

          <section class="detail-grid">
            ${UI.kv("Active stream subscribers", String(Number(stream.subscribers || 0)))}
            ${UI.kv("Published events", String(Number(stream.published || 0)))}
            ${UI.kv("Dropped events", String(droppedEvents))}
            ${UI.kv("Buffered requests", `${bufferedRequests}/${Number(buffer.capacity || requests.length)}`)}
            ${UI.kv("Showing requests", `${Number(requests.length || 0)} / ${bufferedRequests}`)}
            ${UI.kv("Buffered SQL queries", `${bufferedSQL}/${Number(sqlBuffer.capacity || queries.length)}`)}
            ${UI.kv("Tracked sessions", String(trackedSessions))}
            ${UI.kv("Nodes seen", String(nodes.length))}
            ${UI.kv("Cluster relay", clusterStatusLabel)}
            ${UI.kv("Cluster node", cluster.node_id || "-")}
            ${UI.kv("Cluster channel", cluster.channel || "-")}
            ${UI.kv("Cluster received", String(Number(cluster.received || 0)))}
            ${UI.kv("Cluster reason", cluster.reason || "-")}
            ${UI.kv("Trace links", state.traceURLTemplate ? "configured" : "not configured")}
            ${UI.kv("Generated", formatTemporal(payload.generated_at))}
          </section>

          <section class="toolbar toolbar-panel live-controls-row">
            <label for="live-node-select">Node scope</label>
            <select id="live-node-select" class="select">
              ${renderLiveNodeOptions(liveNodes, state.liveNodeFilter)}
            </select>
            <button class="btn btn-ghost btn-sm" id="live-node-apply" type="button">Apply</button>
            <span class="status-chip">Current scope: <strong>${escapeHtml(nodeFilterLabel)}</strong></span>
            <span class="badge ${clusterStatusClass}">Relay ${escapeHtml(clusterStatusLabel)}</span>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Cluster topology</h3>
              <p>Nodes discovered from live traffic and session activity</p>
            </div>
            <div class="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Node</th>
                    <th>Status</th>
                    <th>Last seen</th>
                    <th>Last event</th>
                    <th>HTTP events</th>
                    <th>SQL events</th>
                    <th>Tracked sessions</th>
                  </tr>
                </thead>
                <tbody>
                  ${renderLiveNodeRows(nodes, state.liveNodeFilter)}
                </tbody>
              </table>
            </div>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Live event stream</h3>
              <p>WebSocket feed from /admin/api/live/ws</p>
            </div>
            <div id="live-stream-events" class="table-wrap">
              ${renderLiveEventRows(liveStreamEvents, state.liveNodeFilter)}
            </div>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Recent HTTP requests</h3>
              <p>Non-persistent ring buffer</p>
            </div>
            <div class="live-controls">
              <section class="toolbar toolbar-panel live-controls-row">
                <label for="live-limit-select">Show latest</label>
                <select id="live-limit-select" class="select">
                  ${renderLiveLimitOptions(state.liveLimitRequests)}
                </select>
                <button class="btn btn-ghost btn-sm" id="live-limit-apply" type="button">Apply</button>
                <span class="status-chip">Captured buffer: ${escapeHtml(String(Number(buffer.capacity || requests.length || 0)))}</span>
              </section>
              <section class="toolbar toolbar-panel live-controls-row">
                <span class="status-chip">Excluded paths</span>
                <div class="chip-list">
                  ${renderPatternChips(excludePatterns)}
                </div>
              </section>
              <section class="toolbar toolbar-panel live-controls-row">
                <label for="live-exclude-input">Add exclusion pattern</label>
                <input id="live-exclude-input" class="input" type="text" placeholder="/admin/* or /healthz">
                <button class="btn btn-primary btn-sm" id="live-exclude-add" type="button">Add filter</button>
              </section>
            </div>
            <div class="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Time</th>
                    <th>Node</th>
                    <th>Method</th>
                    <th>Path</th>
                    <th>Status</th>
                    <th>Duration (ms)</th>
                    <th>IP</th>
                    <th>Trace</th>
                  </tr>
                </thead>
                <tbody>
                  ${renderLiveRequestRows(requests)}
                </tbody>
              </table>
            </div>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Live SQL sniffer</h3>
              <p>Non-persistent SQL ring buffer</p>
            </div>
            <div class="live-controls">
              <section class="toolbar toolbar-panel live-controls-row">
                <label for="live-sql-limit-select">Show latest</label>
                <select id="live-sql-limit-select" class="select">
                  ${renderLiveLimitOptions(state.liveLimitSQL, "queries")}
                </select>
                <button class="btn btn-ghost btn-sm" id="live-sql-limit-apply" type="button">Apply</button>
                <span class="status-chip">Captured buffer: ${escapeHtml(String(Number(sqlBuffer.capacity || queries.length || 0)))}</span>
              </section>
            </div>
            <div class="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Time</th>
                    <th>Node</th>
                    <th>Model</th>
                    <th>Operation</th>
                    <th>Duration (ms)</th>
                    <th>Args</th>
                    <th>Trace</th>
                    <th>Query</th>
                  </tr>
                </thead>
                <tbody>
                  ${renderLiveSQLRows(queries)}
                </tbody>
              </table>
            </div>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Active sessions tracker</h3>
              <p>Sync map snapshot</p>
            </div>
            <div class="live-controls">
              <section class="toolbar toolbar-panel live-controls-row">
                <label for="live-sessions-limit-select">Show latest</label>
                <select id="live-sessions-limit-select" class="select">
                  ${renderLiveLimitOptions(state.liveLimitSessions, "sessions")}
                </select>
                <button class="btn btn-ghost btn-sm" id="live-sessions-limit-apply" type="button">Apply</button>
                <span class="status-chip">Tracked sessions: ${escapeHtml(String(Number(stream.tracked_sessions || sessions.length || 0)))}</span>
              </section>
            </div>
            <div class="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Session</th>
                    <th>User</th>
                    <th>Node</th>
                    <th>Route</th>
                    <th>Last seen</th>
                    <th>IP</th>
                    <th>Trace</th>
                  </tr>
                </thead>
                <tbody>
                  ${renderLiveSessionRows(sessions)}
                </tbody>
              </table>
            </div>
          </section>
        `;

      const liveLimitSelect = document.getElementById("live-limit-select");
      const liveLimitApply = document.getElementById("live-limit-apply");
      const liveSQLLimitSelect = document.getElementById("live-sql-limit-select");
      const liveSQLLimitApply = document.getElementById("live-sql-limit-apply");
      const liveSessionsLimitSelect = document.getElementById("live-sessions-limit-select");
      const liveSessionsLimitApply = document.getElementById("live-sessions-limit-apply");
      const liveNodeSelect = document.getElementById("live-node-select");
      const liveNodeApply = document.getElementById("live-node-apply");
      const applyLimit = function () {
        if (!liveLimitSelect) {
          return;
        }
        const value = Number(liveLimitSelect.value || state.liveLimitRequests);
        if (!Number.isFinite(value) || value <= 0) {
          toast("Invalid limit value", "warning");
          return;
        }
        state.liveLimitRequests = value;
        renderLiveOverview();
      };
      const applySQLLimit = function () {
        if (!liveSQLLimitSelect) {
          return;
        }
        const value = Number(liveSQLLimitSelect.value || state.liveLimitSQL);
        if (!Number.isFinite(value) || value <= 0) {
          toast("Invalid SQL limit value", "warning");
          return;
        }
        state.liveLimitSQL = value;
        renderLiveOverview();
      };
      const applySessionsLimit = function () {
        if (!liveSessionsLimitSelect) {
          return;
        }
        const value = Number(liveSessionsLimitSelect.value || state.liveLimitSessions);
        if (!Number.isFinite(value) || value <= 0) {
          toast("Invalid sessions limit value", "warning");
          return;
        }
        state.liveLimitSessions = value;
        renderLiveOverview();
      };
      const applyNodeFilter = function () {
        if (!liveNodeSelect) {
          return;
        }
        state.liveNodeFilter = String(liveNodeSelect.value || "").trim();
        renderLiveOverview();
      };
      if (liveLimitSelect) {
        liveLimitSelect.addEventListener("change", applyLimit);
      }
      if (liveLimitApply) {
        liveLimitApply.addEventListener("click", applyLimit);
      }
      if (liveSQLLimitSelect) {
        liveSQLLimitSelect.addEventListener("change", applySQLLimit);
      }
      if (liveSQLLimitApply) {
        liveSQLLimitApply.addEventListener("click", applySQLLimit);
      }
      if (liveSessionsLimitSelect) {
        liveSessionsLimitSelect.addEventListener("change", applySessionsLimit);
      }
      if (liveSessionsLimitApply) {
        liveSessionsLimitApply.addEventListener("click", applySessionsLimit);
      }
      if (liveNodeSelect) {
        liveNodeSelect.addEventListener("change", applyNodeFilter);
      }
      if (liveNodeApply) {
        liveNodeApply.addEventListener("click", applyNodeFilter);
      }

      bindLiveExcludeControls();
      runNumberAnimations(els.app);
    } catch (err) {
      const retryID = "retry-live";
      els.app.innerHTML = renderRecoverableError("Could not load live runtime", errorText(err), retryID);
      const retryBtn = document.getElementById(retryID);
      if (retryBtn) {
        retryBtn.addEventListener("click", function () {
          renderLiveOverview();
        });
      }
      toast(errorText(err), "error");
    } finally {
      setAppBusy(false);
    }
  }

  async function renderSystemOverview() {
    setAppBusy(true);

    try {
      const payload = await API.systemSnapshot(220);
      const goroutines = payload.goroutines || {};
      const memory = payload.memory || {};
      const databases = Array.isArray(payload.databases) ? payload.databases : [];
      const jobs = payload.jobs || {};
      const jobQueues = Array.isArray(jobs.queues) ? jobs.queues : [];
      const jobWorkers = Array.isArray(jobs.workers) ? jobs.workers : [];
      const flags = Array.isArray(payload.flags) ? payload.flags : [];
      const telemetry = payload.telemetry || {};
      const envRows = Array.isArray(payload.environment) ? payload.environment : [];
      const goroutineCount = Number(goroutines.count || 0);
      const workerCount = Number(jobs.total_workers || 0);
      const queueCount = Number(jobs.total_queues || 0);
      const heapUsage = formatBytes(memory.heap_alloc_bytes);
      const gcCycles = Number(memory.num_gc || 0);
      const otlpConfigured = !!telemetry.otlp_configured;
      const traceLinksConfigured = !!telemetry.trace_links_configured;

      els.app.innerHTML =
        UI.sectionHead("System Pulse", "Go runtime + DB pool + jobs + feature flags", "Snapshot") +
        `
          <section class="cards kpi-grid system-kpi-grid">
            <article class="card kpi-card card-static">
              <p class="card-label">Goroutines</p>
              <p class="kpi-value animate-number" data-value="${goroutineCount}">0</p>
              <span class="status-chip">Go runtime</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Process Mem (Heap)</p>
              <p class="kpi-value">${escapeHtml(heapUsage)}</p>
              <span class="status-chip">GC cycles ${gcCycles.toLocaleString()}</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">CPU Cores (Host)</p>
              <p class="kpi-value animate-number" data-value="${payload.cpus || 0}">0</p>
              <span class="status-chip">assigned</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Queue workers</p>
              <p class="kpi-value animate-number" data-value="${workerCount}">0</p>
              <span class="status-chip">${queueCount.toLocaleString()} queues</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Feature flags</p>
              <p class="kpi-value animate-number" data-value="${flags.length}">0</p>
              <span class="status-chip">${envRows.length.toLocaleString()} env vars</span>
            </article>
            <article class="card kpi-card card-static">
              <p class="card-label">Telemetry</p>
              <p class="kpi-value">${otlpConfigured ? "OTLP on" : "OTLP off"}</p>
              <span class="status-chip">Trace links ${traceLinksConfigured ? "on" : "off"}</span>
            </article>
          </section>

          <section class="detail-grid">
            ${UI.kv("Go version", payload.go_version || "-")}
            ${UI.kv("Runtime", `${payload.go_os || "-"} / ${payload.go_arch || "-"}`)}
            ${UI.kv("Goroutines", String(goroutineCount))}
            ${UI.kv("GOMAXPROCS", String(Number(payload.gomaxprocs || 0)))}
            ${UI.kv("CPU cores", String(Number(payload.cpus || 0)))}
            ${UI.kv("Queues discovered", String(queueCount))}
            ${UI.kv("Active workers", String(workerCount))}
            ${UI.kv("OTLP exporter", otlpConfigured ? "enabled" : "disabled")}
            ${UI.kv("OTLP endpoint", telemetry.otlp_endpoint || "-")}
            ${UI.kv("Trace links", traceLinksConfigured ? "configured" : "not configured")}
            ${UI.kv("Generated", formatTemporal(payload.generated_at))}
          </section>

          <section class="cards system-signal-grid">
            <article class="card card-static system-signal-card">
              <p class="card-label">CPU host metrics</p>
              <p class="section-subtitle">Real-time hardware load %</p>
              ${renderSystemCPUChart(payload)}
            </article>
            <article class="card card-static system-signal-card">
              <p class="card-label">Memory composition</p>
              <p class="section-subtitle">Current runtime distribution</p>
              ${renderSystemMemoryBars(memory)}
            </article>
            <article class="card card-static system-signal-card">
              <p class="card-label">DB pool utilization</p>
              <p class="section-subtitle">In use vs open capacity</p>
              ${renderSystemDatabaseUtilizationBars(databases)}
            </article>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Goroutine explorer</h3>
              <p>States from runtime pprof snapshot</p>
            </div>
            <div class="cards">
              ${renderGoroutineStateCards(goroutines.state_counts || [])}
            </div>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Memory and GC</h3>
              <p>runtime.ReadMemStats</p>
            </div>
            <section class="detail-grid">
              ${UI.kv("Alloc", formatBytes(memory.alloc_bytes))}
              ${UI.kv("Heap alloc", formatBytes(memory.heap_alloc_bytes))}
              ${UI.kv("Heap sys", formatBytes(memory.heap_sys_bytes))}
              ${UI.kv("Stack in use", formatBytes(memory.stack_in_use_bytes))}
              ${UI.kv("Heap objects", String(Number(memory.heap_objects || 0)))}
              ${UI.kv("GC cycles", String(Number(memory.num_gc || 0)))}
              ${UI.kv("Last GC pause", `${Number(memory.last_pause_ms || 0)} ms`)}
              ${UI.kv("Total GC pause", `${Number(memory.pause_total_ms || 0)} ms`)}
            </section>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>DB connection pool stats</h3>
              <p>database/sql Stats()</p>
            </div>
            <div class="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Alias</th>
                    <th>Dialect</th>
                    <th>Open</th>
                    <th>In use</th>
                    <th>Idle</th>
                    <th>Wait count</th>
                    <th>Wait (ms)</th>
                    <th>Max open</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  ${renderSystemDatabaseRows(databases)}
                </tbody>
              </table>
            </div>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Worker/job pool monitor</h3>
              <p>${jobs.enabled ? "Asynq runtime snapshot" : `Unavailable: ${escapeHtml(jobs.reason || "runtime not available")}`}</p>
            </div>
            <section class="detail-grid">
              ${UI.kv("Queues", String(Number(jobs.total_queues || 0)))}
              ${UI.kv("Servers", String(Number(jobs.total_servers || 0)))}
              ${UI.kv("Workers", String(Number(jobs.total_workers || 0)))}
              ${UI.kv("Pending", String(Number(jobs.total_pending || 0)))}
              ${UI.kv("Active", String(Number(jobs.total_active || 0)))}
              ${UI.kv("Scheduled", String(Number(jobs.total_scheduled || 0)))}
              ${UI.kv("Retry", String(Number(jobs.total_retry || 0)))}
              ${UI.kv("Processed today", String(Number(jobs.total_processed_today || 0)))}
            </section>
            <div class="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Queue</th>
                    <th>Size</th>
                    <th>Pending</th>
                    <th>Active</th>
                    <th>Scheduled</th>
                    <th>Retry</th>
                    <th>Paused</th>
                    <th>Latency (ms)</th>
                    <th>Action</th>
                  </tr>
                </thead>
                <tbody>
                  ${renderSystemJobQueueRows(jobQueues)}
                </tbody>
              </table>
            </div>
            <section class="toolbar">
              <label class="status-chip">
                <input type="checkbox" id="system-queue-force">
                Force runtime actions (required in production)
              </label>
            </section>
            <div class="table-wrap system-subtable">
              <table>
                <thead>
                  <tr>
                    <th>Server</th>
                    <th>Queue</th>
                    <th>Task type</th>
                    <th>Task ID</th>
                    <th>Started</th>
                    <th>Deadline</th>
                  </tr>
                </thead>
                <tbody>
                  ${renderSystemWorkerRows(jobWorkers)}
                </tbody>
              </table>
            </div>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Live feature flags</h3>
              <p>In-memory booleans editable at runtime</p>
            </div>
            <section class="toolbar">
              <input id="feature-flag-name" class="input" type="text" placeholder="new_flag_name">
              <select id="feature-flag-enabled" class="select">
                <option value="false">Disabled</option>
                <option value="true">Enabled</option>
              </select>
              <button id="feature-flag-create" class="btn btn-primary" type="button">Create flag</button>
            </section>
            <div class="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Flag</th>
                    <th>Enabled</th>
                    <th>Updated at</th>
                    <th>Updated by</th>
                    <th>Toggle</th>
                    <th>Delete</th>
                  </tr>
                </thead>
                <tbody>
                  ${renderFeatureFlagRows(flags)}
                </tbody>
              </table>
            </div>
          </section>

          <section class="section-block">
            <div class="section-block-head">
              <h3>Environment (startup snapshot)</h3>
              <p>Sensitive values are masked by key policy</p>
            </div>
            <div class="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Value</th>
                    <th>Masked</th>
                  </tr>
                </thead>
                <tbody>
                  ${renderSystemEnvRows(envRows)}
                </tbody>
              </table>
            </div>
          </section>
        `;

      bindSystemQueueActions();
      bindFeatureFlagActions();
      runNumberAnimations(els.app);
    } catch (err) {
      const retryID = "retry-system";
      els.app.innerHTML = renderRecoverableError("Could not load system pulse", errorText(err), retryID);
      const retryBtn = document.getElementById(retryID);
      if (retryBtn) {
        retryBtn.addEventListener("click", function () {
          renderSystemOverview();
        });
      }
      toast(errorText(err), "error");
    } finally {
      setAppBusy(false);
    }
  }

  function renderGoroutineStateCards(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return UI.empty("No goroutine states available");
    }
    return rows
      .map(function (row) {
        return `
          <article class="card card-static">
            <p class="card-label">${escapeHtml(row.state || "unknown")}</p>
            <p class="card-count">${Number(row.count || 0)}</p>
          </article>
        `;
      })
      .join("");
  }

  function renderSystemCPUChart(payload) {
    const goroutines = Number(payload.goroutines || 0);
    const cores = Number(payload.cpus || 1);
    
    // Real CPU load mapped directly via gopsutil/cpu in backend
    const usage = Math.max(0, Math.min(100, Number(payload.cpu_load || 0)));
    const idle = Math.max(0, 100 - usage);

    const chartId = "sys-cpu-chart-" + Date.now() + "-" + Math.random().toString(36).slice(2, 6);

    setTimeout(function () {
      var canvas = document.getElementById(chartId);
      if (!canvas || typeof Chart === "undefined") { return; }

      var chart = new Chart(canvas, {
        type: "doughnut",
        data: {
          labels: ["Load (%)", "Idle (%)"],
          datasets: [{
            data: [usage, idle],
            backgroundColor: ["#0ea5e9", "rgba(148, 163, 184, 0.15)"],
            borderWidth: 0,
            borderRadius: 4,
            spacing: 2,
            hoverOffset: 4,
          }],
        },
        options: {
          cutout: "80%",
          plugins: {
            legend: {
              display: true,
              position: "right",
              labels: { color: "#94a3b8", usePointStyle: true, pointStyle: "circle", boxWidth: 8, font: { size: 11 } },
            },
            tooltip: {
              callbacks: {
                label: function (ctx) {
                  return " " + ctx.label + ": " + Math.round(ctx.raw) + "%";
                },
              },
            },
          },
        },
      });
      registerChart(chart);
    }, 0);

    return `
      <div class="chart-container system-chart-container">
        <canvas id="${chartId}"></canvas>
      </div>
      <div style="text-align: center; margin-top: 12px; border-top: 1px solid rgba(132, 160, 195, 0.15); padding-top: 8px;">
        <span style="font-size: 11px; text-transform: uppercase; font-weight: 600; color: var(--text-soft); letter-spacing: 0.5px;">GoFrame process: <strong style="color: var(--text-main); font-weight: 700; font-size: 13px;">${Number(payload.process_cpu_load || 0).toFixed(1)}%</strong></span>
      </div>
    `;
  }

  function renderSystemMemoryBars(memory) {
    const alloc = Number(memory && memory.alloc_bytes || 0);
    const heapAlloc = Number(memory && memory.heap_alloc_bytes || 0);
    const heapSys = Number(memory && memory.heap_sys_bytes || 0);
    const stackInUse = Number(memory && memory.stack_in_use_bytes || 0);
    const chartId = "sys-mem-chart-" + Date.now() + "-" + Math.random().toString(36).slice(2, 6);

    setTimeout(function () {
      var canvas = document.getElementById(chartId);
      if (!canvas || typeof Chart === "undefined") { return; }

      var chart = new Chart(canvas, {
        type: "doughnut",
        data: {
          labels: ["Heap alloc", "Heap sys", "Stack in use", "Other alloc"],
          datasets: [{
            data: [heapAlloc, heapSys, stackInUse, Math.max(0, alloc - heapAlloc - stackInUse)],
            backgroundColor: ["#0ea5e9", "#6366f1", "#10b981", "#cbd5e1"],
            borderWidth: 0,
            borderRadius: 4,
            spacing: 2,
            hoverOffset: 4,
          }],
        },
        options: {
          cutout: "80%",
          plugins: {
            legend: {
              display: true,
              position: "right",
              labels: { color: "#94a3b8", usePointStyle: true, pointStyle: "circle", boxWidth: 8, font: { size: 11 } },
            },
            tooltip: {
              callbacks: {
                label: function (ctx) {
                  return " " + ctx.label + ": " + formatBytes(ctx.raw);
                },
              },
            },
          },
        },
      });
      registerChart(chart);
    }, 0);

    return `
      <div class="chart-container system-chart-container">
        <canvas id="${chartId}"></canvas>
      </div>
    `;
  }

  function renderSystemDatabaseUtilizationBars(rows) {
    const items = (Array.isArray(rows) ? rows : [])
      .map(function (row) {
        const open = Number(row && row.open_connections || 0);
        const maxOpen = Number(row && row.max_open_connections || 0);
        const inUse = Number(row && row.in_use || 0);
        const base = Math.max(1, maxOpen, open);
        return {
          alias: String((row && row.alias) || "default"),
          inUse: inUse,
          idle: Math.max(0, open - inUse),
          max: base,
        };
      });

    if (items.length === 0) {
      return `<div class="table-empty">No database pools available</div>`;
    }

    const chartId = "sys-db-chart-" + Date.now() + "-" + Math.random().toString(36).slice(2, 6);

    setTimeout(function () {
      var canvas = document.getElementById(chartId);
      if (!canvas || typeof Chart === "undefined") { return; }

      var chart = new Chart(canvas, {
        type: "bar",
        data: {
          labels: items.map(function (i) { return i.alias; }),
          datasets: [
            {
              label: "In Use",
              data: items.map(function (i) { return i.inUse; }),
              backgroundColor: "#2dd4bf",
              borderRadius: 4,
            },
            {
              label: "Idle",
              data: items.map(function (i) { return i.idle; }),
              backgroundColor: "rgba(45, 212, 191, 0.2)",
              borderRadius: 4,
            },
          ],
        },
        options: {
          indexAxis: "y",
          scales: {
            x: { stacked: true, grid: { color: "rgba(37, 77, 128, 0.2)" }, ticks: { color: "#6f92bd", precision: 0 } },
            y: { stacked: true, grid: { display: false }, ticks: { color: "#94a3b8" } },
          },
          plugins: {
            legend: { display: true, labels: { color: "#94a3b8", usePointStyle: true, boxWidth: 8 } },
            tooltip: { mode: "index", intersect: false },
          },
        },
      });
      registerChart(chart);
    }, 0);

    return `
      <div class="chart-container system-chart-container">
        <canvas id="${chartId}"></canvas>
      </div>
    `;
  }

  function renderSystemDatabaseRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="9">No database stats available</td></tr>`;
    }
    return rows
      .map(function (row) {
        const status = row.error ? `error: ${row.error}` : "ok";
        return `
          <tr>
            <td>${escapeHtml(row.alias || "-")}${row.is_default ? " (default)" : ""}</td>
            <td>${escapeHtml(row.dialect || row.engine || "-")}</td>
            <td>${escapeHtml(String(row.open_connections || 0))}</td>
            <td>${escapeHtml(String(row.in_use || 0))}</td>
            <td>${escapeHtml(String(row.idle || 0))}</td>
            <td>${escapeHtml(String(row.wait_count || 0))}</td>
            <td>${escapeHtml(String(row.wait_duration_ms || 0))}</td>
            <td>${escapeHtml(String(row.max_open_connections || 0))}</td>
            <td>${escapeHtml(status)}</td>
          </tr>
        `;
      })
      .join("");
  }

  function renderSystemJobQueueRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="9">No queues discovered</td></tr>`;
    }
    return rows
      .map(function (row) {
        const queueName = String(row.name || "").trim();
        const canPause = !row.paused;
        const canResume = !!row.paused;
        return `
          <tr>
            <td>${escapeHtml(row.name || "-")}</td>
            <td>${escapeHtml(String(row.size || 0))}</td>
            <td>${escapeHtml(String(row.pending || 0))}</td>
            <td>${escapeHtml(String(row.active || 0))}</td>
            <td>${escapeHtml(String(row.scheduled || 0))}</td>
            <td>${escapeHtml(String(row.retry || 0))}</td>
            <td>${row.paused ? "yes" : "no"}</td>
            <td>${escapeHtml(String(row.latency_ms || 0))}</td>
            <td>
              <button type="button" class="btn btn-ghost btn-sm" data-queue-action="pause" data-queue-name="${escapeHtml(queueName)}" ${canPause ? "" : "disabled"}>Pause</button>
              <button type="button" class="btn btn-ghost btn-sm" data-queue-action="unpause" data-queue-name="${escapeHtml(queueName)}" ${canResume ? "" : "disabled"}>Resume</button>
              <button type="button" class="btn btn-ghost btn-sm" data-queue-action="retry" data-queue-name="${escapeHtml(queueName)}">Run retry</button>
            </td>
          </tr>
        `;
      })
      .join("");
  }

  function renderSystemWorkerRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="6">No active workers</td></tr>`;
    }
    return rows
      .map(function (row) {
        const server = `${row.host || "-"}#${row.pid || "-"}`;
        return `
          <tr>
            <td>${escapeHtml(server)}</td>
            <td>${escapeHtml(row.queue || "-")}</td>
            <td>${escapeHtml(row.task_type || "-")}</td>
            <td>${escapeHtml(row.task_id || "-")}</td>
            <td>${escapeHtml(formatTemporal(row.started_at || ""))}</td>
            <td>${escapeHtml(formatTemporal(row.deadline || ""))}</td>
          </tr>
        `;
      })
      .join("");
  }

  function renderFeatureFlagRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="6">No feature flags registered yet</td></tr>`;
    }
    return rows
      .map(function (row) {
        const enabled = !!row.enabled;
        return `
          <tr>
            <td>${escapeHtml(row.name || "-")}</td>
            <td>${enabled ? "yes" : "no"}</td>
            <td>${escapeHtml(formatTemporal(row.updated_at || ""))}</td>
            <td>${escapeHtml(row.updated_by || "-")}</td>
            <td>
              <button
                type="button"
                class="btn btn-ghost btn-sm"
                data-flag-toggle="1"
                data-flag-name="${escapeHtml(row.name || "")}"
                data-flag-enabled="${enabled ? "1" : "0"}"
              >
                ${enabled ? "Disable" : "Enable"}
              </button>
            </td>
            <td>
              <button
                type="button"
                class="btn btn-danger btn-sm"
                data-flag-delete="1"
                data-flag-name="${escapeHtml(row.name || "")}"
              >
                Delete
              </button>
            </td>
          </tr>
        `;
      })
      .join("");
  }

  function bindFeatureFlagActions() {
    const createBtn = document.getElementById("feature-flag-create");
    if (createBtn) {
      createBtn.addEventListener("click", async function () {
        const nameInput = document.getElementById("feature-flag-name");
        const enabledInput = document.getElementById("feature-flag-enabled");
        const name = String((nameInput && nameInput.value) || "").trim();
        if (!name) {
          toast("Feature flag name is required", "warning");
          return;
        }
        const enabled = String((enabledInput && enabledInput.value) || "false") === "true";
        const restore = setButtonPending(createBtn, "Saving...");
        try {
          await API.systemCreateFlag(name, enabled);
          toast(`Feature flag ${name} saved`, "success");
          await renderSystemOverview();
        } catch (err) {
          toast(errorText(err), "error");
        } finally {
          restore();
        }
      });
    }

    document.querySelectorAll("[data-flag-toggle='1']").forEach(function (btn) {
      btn.addEventListener("click", async function () {
        const name = String(btn.getAttribute("data-flag-name") || "").trim();
        if (!name) {
          return;
        }
        const current = btn.getAttribute("data-flag-enabled") === "1";
        const next = !current;
        const restore = setButtonPending(btn, next ? "Enabling..." : "Disabling...");
        try {
          await API.systemSetFlag(name, next);
          toast(`Feature flag ${name} is now ${next ? "enabled" : "disabled"}`, "success");
          await renderSystemOverview();
        } catch (err) {
          toast(errorText(err), "error");
        } finally {
          restore();
        }
      });
    });

    document.querySelectorAll("[data-flag-delete='1']").forEach(function (btn) {
      btn.addEventListener("click", async function () {
        const name = String(btn.getAttribute("data-flag-name") || "").trim();
        if (!name) {
          return;
        }
        const accepted = await confirmAction(`Delete feature flag ${name}?`);
        if (!accepted) {
          return;
        }
        const restore = setButtonPending(btn, "Deleting...");
        try {
          await API.systemDeleteFlag(name);
          toast(`Feature flag ${name} deleted`, "success");
          await renderSystemOverview();
        } catch (err) {
          toast(errorText(err), "error");
        } finally {
          restore();
        }
      });
    });
  }

  function bindSystemQueueActions() {
    document.querySelectorAll("[data-queue-action]").forEach(function (btn) {
      btn.addEventListener("click", async function () {
        const action = String(btn.getAttribute("data-queue-action") || "").trim();
        const queue = String(btn.getAttribute("data-queue-name") || "").trim();
        if (!action || !queue) {
          return;
        }
        const forceInput = document.getElementById("system-queue-force");
        const force = !!(forceInput && forceInput.checked);
        const accepted = await confirmAction(`Run ${action} on queue ${queue}?`);
        if (!accepted) {
          return;
        }
        const restore = setButtonPending(btn, "Applying...");
        try {
          await API.systemQueueAction(queue, action, {
            confirm_queue: queue,
            acknowledge: RUNTIME_QUEUE_ACK,
            force: force,
          });
          toast(`Queue ${queue}: ${action} applied`, "success");
          await renderSystemOverview();
        } catch (err) {
          toast(errorText(err), "error");
        } finally {
          restore();
        }
      });
    });
  }

  function renderSystemEnvRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="3">No environment rows available</td></tr>`;
    }
    return rows
      .map(function (row) {
        return `
          <tr>
            <td>${escapeHtml(row.name || "-")}</td>
            <td>${escapeHtml(row.value || "")}</td>
            <td>${row.masked ? "yes" : "no"}</td>
          </tr>
        `;
      })
      .join("");
  }

  function startLiveStream() {
    stopLiveStream();
    liveStreamAttempts = 0;
    connectLiveStream();
  }

  function connectLiveStream() {
    const hash = window.location.hash || "#/";
    if (hash !== "#/live" && hash !== "#/") {
      return;
    }
    const url = API.liveWebSocketURL();
    try {
      liveStreamSocket = new WebSocket(url);
    } catch (_) {
      scheduleLiveReconnect();
      return;
    }

    liveStreamSocket.onopen = function () {
      liveStreamConnected = true;
      liveStreamAttempts = 0;
      refreshLiveStreamHeader();
    };

    liveStreamSocket.onmessage = function (evt) {
      try {
        const payload = JSON.parse(String(evt.data || "{}"));
        liveStreamEvents.unshift(payload);
        if (liveStreamEvents.length > LIVE_STREAM_EVENT_CAP) {
          liveStreamEvents.length = LIVE_STREAM_EVENT_CAP;
        }

        const hash = window.location.hash || "#/";
        if (hash === "#/live") {
          refreshLiveStreamRows();
        } else if (hash === "#/") {
          pushEventToCharts(payload);
        }
      } catch (_) {}
    };

    liveStreamSocket.onerror = function () {};

    liveStreamSocket.onclose = function () {
      liveStreamConnected = false;
      refreshLiveStreamHeader();
      if ((window.location.hash || "#/") === "#/live") {
        scheduleLiveReconnect();
      }
    };
  }

  function scheduleLiveReconnect() {
    if (liveStreamRetryTimer !== null) {
      return;
    }
    liveStreamAttempts += 1;
    const wait = Math.min(4000, 300 * Math.pow(2, Math.min(5, liveStreamAttempts)));
    liveStreamRetryTimer = setTimeout(function () {
      liveStreamRetryTimer = null;
      connectLiveStream();
    }, wait);
  }

  function stopLiveStream() {
    if (liveStreamRetryTimer !== null) {
      clearTimeout(liveStreamRetryTimer);
      liveStreamRetryTimer = null;
    }
    if (liveStreamSocket) {
      try {
        liveStreamSocket.close();
      } catch (_) {}
      liveStreamSocket = null;
    }
    liveStreamConnected = false;
  }

  function pushEventToCharts(event) {
    if (!event) return;
    const isRequest = event.request || event.type === "request" || event.type === "http";
    const isSession = event.session || event.type === "session" || event.type === "ws";
    
    if (!isRequest && !isSession) return;
    
    for (const chart of activeCharts) {
      if (chart.canvas && chart.canvas.id.indexOf("overview-trend-canvas") > -1) {
        if (isRequest) {
          const reqDataset = chart.data.datasets.find(ds => ds.label && ds.label.includes("Requests"));
          if (reqDataset && reqDataset.data.length > 0) {
            reqDataset.data[reqDataset.data.length - 1] += 1;
            chart.update('none');
          }
        }
        if (isSession) {
          const sessDataset = chart.data.datasets.find(ds => ds.label && ds.label.includes("Sessions"));
          if (sessDataset && sessDataset.data.length > 0) {
            sessDataset.data[sessDataset.data.length - 1] += 1;
            chart.update('none');
          }
        }
      }
    }
  }

  function refreshLiveStreamRows() {
    const node = document.getElementById("live-stream-events");
    if (!node) {
      return;
    }
    node.innerHTML = renderLiveEventRows(liveStreamEvents, state.liveNodeFilter);
  }

  function refreshLiveStreamHeader() {
    if ((window.location.hash || "#/") !== "#/live") {
      return;
    }
    renderLiveOverview();
  }

  function renderLiveEventRows(events, nodeFilter) {
    if (!Array.isArray(events) || events.length === 0) {
      return `<div class="table-empty">No streamed events yet</div>`;
    }
    const targetNode = String(nodeFilter || "").trim().toLowerCase();
    const filtered = targetNode
      ? events.filter(function (event) {
          const eventNode = String(event.node_id || (event.request && event.request.node_id) || (event.sql && event.sql.node_id) || (event.session && event.session.node_id) || "")
            .trim()
            .toLowerCase();
          return eventNode === targetNode;
        })
      : events;
    if (filtered.length === 0) {
      return `<div class="table-empty">No streamed events for this node</div>`;
    }
    const rows = filtered
      .map(function (event) {
        const type = escapeHtml(event.type || "event");
        const ts = escapeHtml(formatTemporal(event.timestamp || ""));
        const nodeID = escapeHtml(liveNodeLabel(event.node_id || (event.request && event.request.node_id) || (event.sql && event.sql.node_id) || (event.session && event.session.node_id) || ""));
        let summary = "-";
        if (event.request && event.request.path) {
          summary = `${escapeHtml(event.request.method || "")} ${escapeHtml(event.request.path || "")} · ${escapeHtml(String(event.request.status || ""))}`;
        } else if (event.sql && event.sql.query) {
          summary = `${escapeHtml(event.sql.operation || "query")} · ${escapeHtml(shortenSQL(event.sql.query || ""))}`;
        } else if (event.session && event.session.last_route) {
          summary = `${escapeHtml(event.session.user_id || "-")} · ${escapeHtml(event.session.last_route || "")}`;
        }
        return `<tr><td>${ts}</td><td>${nodeID}</td><td>${type}</td><td>${summary}</td></tr>`;
      })
      .join("");
    return `
      <table>
        <thead>
          <tr>
            <th>Time</th>
            <th>Node</th>
            <th>Type</th>
            <th>Summary</th>
          </tr>
        </thead>
        <tbody>${rows}</tbody>
      </table>
    `;
  }

  function renderLiveLimitOptions(current, noun) {
    const selected = Number(current || 80);
    const options = [25, 50, 80, 100, 200, 300];
    const label = String(noun || "requests");
    return options
      .map(function (value) {
        return `<option value="${value}" ${selected === value ? "selected" : ""}>${value} ${escapeHtml(label)}</option>`;
      })
      .join("");
  }

  function renderLiveNodeOptions(nodes, selectedNode) {
    const selected = String(selectedNode || "").trim();
    const options = Array.isArray(nodes) ? nodes : [];
    const allSelected = selected === "";
    const rows = [`<option value="" ${allSelected ? "selected" : ""}>All nodes</option>`];
    options.forEach(function (nodeID) {
      const value = String(nodeID || "").trim();
      if (!value) {
        return;
      }
      rows.push(`<option value="${escapeHtml(value)}" ${selected === value ? "selected" : ""}>${escapeHtml(value)}</option>`);
    });
    return rows.join("");
  }

  function collectLiveNodeOptions(payload, streamEvents) {
    const set = new Set();
    const add = function (value) {
      const node = String(value || "").trim();
      if (!node) {
        return;
      }
      set.add(node);
    };
    if (payload && typeof payload === "object") {
      const requests = Array.isArray(payload.requests) ? payload.requests : [];
      const queries = Array.isArray(payload.queries) ? payload.queries : [];
      const sessions = Array.isArray(payload.sessions) ? payload.sessions : [];
      const nodes = Array.isArray(payload.nodes) ? payload.nodes : [];
      requests.forEach(function (row) {
        add(row && row.node_id);
      });
      queries.forEach(function (row) {
        add(row && row.node_id);
      });
      sessions.forEach(function (row) {
        add(row && row.node_id);
      });
      nodes.forEach(function (row) {
        add(row && row.node_id);
      });
      add(payload.node_filter);
      if (payload.cluster && typeof payload.cluster === "object") {
        add(payload.cluster.node_id);
      }
    }
    (Array.isArray(streamEvents) ? streamEvents : []).forEach(function (event) {
      if (!event || typeof event !== "object") {
        return;
      }
      add(event.node_id);
      if (event.request) {
        add(event.request.node_id);
      }
      if (event.sql) {
        add(event.sql.node_id);
      }
      if (event.session) {
        add(event.session.node_id);
      }
    });

    return Array.from(set).sort(function (a, b) {
      return a.localeCompare(b);
    });
  }

  function liveNodeLabel(value) {
    const node = String(value || "").trim();
    return node || "local";
  }

  function renderPatternChips(patterns) {
    if (!Array.isArray(patterns) || patterns.length === 0) {
      return `<span class="chip">No exclusions</span>`;
    }
    return patterns
      .map(function (pattern) {
        const value = String(pattern || "");
        return `
          <span class="chip">
            <code>${escapeHtml(value)}</code>
            <button class="chip-remove" data-live-exclude-remove="${escapeHtml(value)}" type="button" aria-label="Remove pattern ${escapeHtml(value)}">
              ${iconGlyph("close")}
            </button>
          </span>
        `;
      })
      .join("");
  }

  function bindLiveExcludeControls() {
    const addBtn = document.getElementById("live-exclude-add");
    const input = document.getElementById("live-exclude-input");
    if (addBtn && input) {
      const submit = async function () {
        const pattern = String(input.value || "").trim();
        if (!pattern) {
          toast("Pattern is required", "warning");
          return;
        }
        const restore = setButtonPending(addBtn, "Adding...");
        try {
          await API.addLiveExclude(pattern);
          input.value = "";
          toast(`Exclude pattern ${pattern} added`, "success");
          await renderLiveOverview();
        } catch (err) {
          toast(errorText(err), "error");
        } finally {
          restore();
        }
      };
      addBtn.addEventListener("click", submit);
      input.addEventListener("keydown", function (evt) {
        if (evt.key === "Enter") {
          evt.preventDefault();
          submit();
        }
      });
    }

    document.querySelectorAll("[data-live-exclude-remove]").forEach(function (btn) {
      btn.addEventListener("click", async function () {
        const pattern = String(btn.getAttribute("data-live-exclude-remove") || "").trim();
        if (!pattern) {
          return;
        }
        const accepted = await confirmAction(`Remove exclude pattern ${pattern}?`);
        if (!accepted) {
          return;
        }
        const restore = setButtonPending(btn, "...");
        try {
          await API.deleteLiveExclude(pattern);
          toast(`Exclude pattern ${pattern} removed`, "success");
          await renderLiveOverview();
        } catch (err) {
          toast(errorText(err), "error");
        } finally {
          restore();
        }
      });
    });
  }

  function renderLiveNodeRows(rows, nodeFilter) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="7">No nodes discovered yet</td></tr>`;
    }
    const targetNode = String(nodeFilter || "").trim().toLowerCase();
    const filtered = targetNode
      ? rows.filter(function (row) {
          return String((row && row.node_id) || "").trim().toLowerCase() === targetNode;
        })
      : rows;
    if (filtered.length === 0) {
      return `<tr><td class="table-empty" colspan="7">No nodes match current scope</td></tr>`;
    }
    return filtered
      .map(function (row) {
        const status = String((row && row.status) || "idle").trim().toLowerCase() || "idle";
        return `
          <tr>
            <td>${escapeHtml(liveNodeLabel((row && row.node_id) || ""))}</td>
            <td><span class="badge ${liveNodeStatusBadgeClass(status)}">${escapeHtml(status)}</span></td>
            <td>${escapeHtml(formatTemporal((row && row.last_seen_at) || ""))}</td>
            <td>${escapeHtml((row && row.last_event_type) || "-")}</td>
            <td>${escapeHtml(String((row && row.requests) || 0))}</td>
            <td>${escapeHtml(String((row && row.sql_queries) || 0))}</td>
            <td>${escapeHtml(String((row && row.sessions) || 0))}</td>
          </tr>
        `;
      })
      .join("");
  }

  function renderLiveRequestRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="8">No recent requests</td></tr>`;
    }
    return rows
      .map(function (row) {
        const method = String(row.method || "-").toUpperCase();
        const statusCode = Number(row.status || 0);
        const duration = Number(row.duration_ms || 0);
        return `
          <tr>
            <td>${escapeHtml(formatTemporal(row.timestamp || ""))}</td>
            <td>${escapeHtml(liveNodeLabel(row.node_id || ""))}</td>
            <td><span class="badge ${methodBadgeClass(method)}">${escapeHtml(method)}</span></td>
            <td>${escapeHtml(row.path || "-")}</td>
            <td><span class="badge ${statusBadgeClass(statusCode)}">${escapeHtml(String(row.status || "-"))}</span></td>
            <td><span class="metric-pill ${durationBadgeClass(duration)}">${escapeHtml(String(duration))}</span></td>
            <td>${escapeHtml(row.remote_ip || "-")}</td>
            <td>${renderTraceCell(row.trace_id || "")}</td>
          </tr>
        `;
      })
      .join("");
  }

  function renderLiveSessionRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="7">No tracked sessions</td></tr>`;
    }
    return rows
      .map(function (row) {
        return `
          <tr>
            <td>${escapeHtml(row.token_short || "-")}</td>
            <td>${escapeHtml(row.user_id || "-")}</td>
            <td>${escapeHtml(liveNodeLabel(row.node_id || ""))}</td>
            <td>${escapeHtml(row.last_route || "-")}</td>
            <td>${escapeHtml(formatTemporal(row.last_seen_at || ""))}</td>
            <td>${escapeHtml(row.ip || "-")}</td>
            <td>${renderTraceCell(row.trace_id || "")}</td>
          </tr>
        `;
      })
      .join("");
  }

  function renderLiveSQLRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="8">No recent SQL queries</td></tr>`;
    }
    return rows
      .map(function (row) {
        const args = Array.isArray(row.args) && row.args.length > 0 ? row.args.join(", ") : "-";
        const duration = Number(row.duration_ms || 0);
        return `
          <tr>
            <td>${escapeHtml(formatTemporal(row.timestamp || ""))}</td>
            <td>${escapeHtml(liveNodeLabel(row.node_id || ""))}</td>
            <td>${escapeHtml(row.model_name || "-")}</td>
            <td><span class="badge ${operationBadgeClass(row.operation || "-")}">${escapeHtml(row.operation || "-")}</span></td>
            <td><span class="metric-pill ${durationBadgeClass(duration)}">${escapeHtml(String(duration))}</span></td>
            <td>${escapeHtml(args)}</td>
            <td>${renderTraceCell(row.trace_id || "")}</td>
            <td title="${escapeHtml(row.query || "")}">
              ${escapeHtml(shortenSQL(row.query || ""))}
              ${row.error ? `<div class="status-chip">error: ${escapeHtml(row.error)}</div>` : ""}
            </td>
          </tr>
        `;
      })
      .join("");
  }

  function shortenTrace(value) {
    const text = String(value || "").trim();
    if (text.length <= 12) {
      return text || "-";
    }
    return text.slice(0, 12) + "...";
  }

  function renderTraceCell(traceID) {
    const trace = String(traceID || "").trim();
    if (!trace) {
      return "-";
    }
    const url = buildTraceURL(trace);
    const label = escapeHtml(shortenTrace(trace));
    if (!url) {
      return label;
    }
    return `<a class="trace-link" href="${escapeHtml(url)}" target="_blank" rel="noopener">${label}</a>`;
  }

  function buildTraceURL(traceID) {
    const trace = String(traceID || "").trim();
    if (!trace) {
      return "";
    }
    const template = String(state.traceURLTemplate || "").trim();
    if (!template) {
      return "";
    }
    const encodedTrace = encodeURIComponent(trace);

    let rawURL = template;
    if (template.includes("{trace_id}")) {
      rawURL = template.split("{trace_id}").join(encodedTrace);
    } else if (template.includes("{{trace_id}}")) {
      rawURL = template.split("{{trace_id}}").join(encodedTrace);
    } else if (template.includes("%s")) {
      rawURL = template.replace("%s", encodedTrace);
    } else {
      rawURL = template + (template.includes("?") ? "&" : "?") + "trace_id=" + encodedTrace;
    }

    try {
      const resolved = new URL(rawURL, window.location.origin);
      if (resolved.protocol !== "http:" && resolved.protocol !== "https:") {
        return "";
      }
      return resolved.toString();
    } catch (_) {
      return "";
    }
  }

  function liveNodeStatusBadgeClass(status) {
    switch (String(status || "").toLowerCase()) {
      case "online":
        return "badge-success";
      case "degraded":
        return "badge-warning";
      case "stale":
        return "badge-danger";
      default:
        return "badge-neutral";
    }
  }

  function methodBadgeClass(method) {
    switch (String(method || "").toUpperCase()) {
      case "GET":
        return "badge-method-get";
      case "POST":
        return "badge-method-post";
      case "PUT":
      case "PATCH":
        return "badge-method-update";
      case "DELETE":
        return "badge-method-delete";
      default:
        return "badge-neutral";
    }
  }

  function statusBadgeClass(code) {
    const value = Number(code || 0);
    if (value >= 200 && value < 300) {
      return "badge-success";
    }
    if (value >= 300 && value < 400) {
      return "badge-info";
    }
    if (value >= 400 && value < 500) {
      return "badge-warning";
    }
    if (value >= 500) {
      return "badge-danger";
    }
    return "badge-neutral";
  }

  function durationBadgeClass(durationMS) {
    const value = Number(durationMS || 0);
    if (value >= 1000) {
      return "metric-danger";
    }
    if (value >= 250) {
      return "metric-warning";
    }
    if (value >= 100) {
      return "metric-info";
    }
    return "metric-success";
  }

  function operationBadgeClass(value) {
    const op = String(value || "").toLowerCase();
    if (op.includes("select") || op.includes("read")) {
      return "badge-info";
    }
    if (op.includes("insert") || op.includes("create")) {
      return "badge-success";
    }
    if (op.includes("update")) {
      return "badge-warning";
    }
    if (op.includes("delete")) {
      return "badge-danger";
    }
    return "badge-neutral";
  }

  function shortenSQL(value) {
    const text = String(value || "").trim();
    if (text.length <= 120) {
      return text || "-";
    }
    return text.slice(0, 120) + "...";
  }

  function updateSessionChartCard(chartId, points) {
    var chart = activeCharts.find(function(c) { return c && c.canvas && c.canvas.id === chartId; });
    if (!chart || !Array.isArray(points)) return;
    var values = points.map(function (p) { return Number(p.active || 0); });
    var labels = points.map(function (p) { return formatOverviewTimeLabel(p.timestamp); });
    chart.data.labels = labels;
    chart.data.datasets[0].data = values;
    chart.update("none");
  }

  function renderSessionChartCard(chartId, title, subtitle, points) {
    if (!Array.isArray(points) || points.length === 0) {
      return `
        <article class="card session-chart-card">
          <p class="card-label">${escapeHtml(title)}</p>
          <p class="section-subtitle">${escapeHtml(subtitle)}</p>
          <div class="table-empty">No telemetry points</div>
        </article>
      `;
    }

    const values = points.map(function (p) { return Number(p.active || 0); });
    const max = Math.max(1, ...values);
    const avg = Math.round(values.reduce(function (acc, v) { return acc + v; }, 0) / values.length);
    const latest = values[values.length - 1] || 0;
    const prev = values.length > 1 ? values[values.length - 2] : latest;
    const delta = latest - prev;
    const trendLabel = delta > 0 ? `+${delta}` : String(delta);


    setTimeout(function () {
      var canvas = document.getElementById(chartId);
      if (!canvas || typeof Chart === "undefined") { return; }

      var labels = points.map(function (p) { return formatOverviewTimeLabel(p.timestamp); });
      var chart = new Chart(canvas, {
        type: "line",
        data: {
          labels: labels,
          datasets: [{
            label: escapeHtml(title),
            data: values,
            borderColor: "#0ea5e9", // Clean unified blue
            backgroundColor: function (ctx) {
              var c = ctx.chart;
              var area = c.chartArea;
              if (!area) { return "rgba(14, 165, 233, 0.15)"; }
              var gradient = c.ctx.createLinearGradient(0, area.top, 0, area.bottom);
              gradient.addColorStop(0, "rgba(14, 165, 233, 0.28)");
              gradient.addColorStop(1, "rgba(14, 165, 233, 0.02)");
              return gradient;
            },
            fill: true,
            tension: 0.4,
            pointBackgroundColor: "#0ea5e9",
            pointBorderColor: "#fff",
            pointHoverRadius: 5,
            pointHoverBackgroundColor: "#0ea5e9",
          }],
        },
        options: {
          interaction: { mode: "index", intersect: false },
          scales: {
            x: {
              grid: { color: "rgba(148, 163, 184, 0.1)" },
              ticks: { maxTicksLimit: 4, color: "#94a3b8", font: { family: '"Inter", sans-serif', size: 9 } },
            },
            y: {
              beginAtZero: true,
              grid: { color: "rgba(148, 163, 184, 0.1)" },
              ticks: { color: "#94a3b8", font: { family: '"Inter", sans-serif', size: 9 }, precision: 0 },
            },
          },
          plugins: { legend: { display: false } },
        },
      });
      registerChart(chart);
    }, 0);

    return `
      <article class="card session-chart-card">
        <p class="card-label">${escapeHtml(title)}</p>
        <p class="section-subtitle">${escapeHtml(subtitle)}</p>
        <div class="chart-container session-chart-container">
          <canvas id="${chartId}"></canvas>
        </div>
        <div class="session-chart-meta">
          <span class="status-chip">Latest: ${latest}</span>
          <span class="status-chip">Avg: ${avg}</span>
          <span class="status-chip">Peak: ${max}</span>
          <span class="status-chip ${delta >= 0 ? "metric-success" : "metric-warning"}">Trend: ${trendLabel}</span>
        </div>
      </article>
    `;
  }

  function renderSessionChart(points) {
    return renderSessionChartCard("Active", "Session telemetry", points);
  }

  function renderSessionRows(rows) {
    if (!Array.isArray(rows) || rows.length === 0) {
      return `<tr><td class="table-empty" colspan="8">No active sessions</td></tr>`;
    }

    return rows
      .map(function (row) {
        const runtimePod = normalizeSessionPod(row);
        const runtimeHost = normalizeSessionHost(row);
        return `
          <tr>
            <td title="${escapeHtml(row.token || "")}">${escapeHtml(row.token_short || row.token || "-")}</td>
            <td>${escapeHtml(row.user || "-")}</td>
            <td>${runtimePod ? `<span class="status-chip status-chip-runtime">${escapeHtml(runtimePod)}</span>` : '<span class="status-chip status-chip-muted">n/a</span>'}</td>
            <td>${runtimeHost ? `<span class="status-chip status-chip-runtime">${escapeHtml(runtimeHost)}</span>` : '<span class="status-chip status-chip-muted">n/a</span>'}</td>
            <td>${escapeHtml(row.remote_ip || "-")}</td>
            <td>${escapeHtml(formatTemporal(row.last_seen_at || ""))}</td>
            <td>${row.idle_seconds === undefined || row.idle_seconds === null ? "-" : escapeHtml(String(row.idle_seconds))}</td>
            <td>${escapeHtml(formatTemporal(row.expires_at || ""))}</td>
          </tr>
        `;
      })
      .join("");
  }

  function normalizeSessionPod(row) {
    if (!row || typeof row !== "object") {
      return "";
    }
    const pod = String(row.pod || "").trim();
    const host = String(row.host || "").trim();
    if (!pod) {
      return "";
    }
    if (host && pod.toLowerCase() === host.toLowerCase()) {
      return "";
    }
    return pod;
  }

  function normalizeSessionHost(row) {
    if (!row || typeof row !== "object") {
      return "";
    }
    const host = String(row.host || "").trim();
    if (host) {
      return host;
    }
    const instance = String(row.instance || "").trim();
    if (!instance) {
      return "";
    }
    if (instance.includes("@")) {
      const parts = instance.split("@");
      return String(parts[parts.length - 1] || "").trim();
    }
    return instance;
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
    const dbAlias = currentDatabaseAlias();
    state.page = opts && opts.page > 0 ? opts.page : 1;
    if (opts && typeof opts.search === "string") {
      state.search = opts.search;
    }
    updateNewButton();

    setAppBusy(true);
    els.app.innerHTML = loadingMarkup();

    try {
      const schema = await API.schema(name, dbAlias);
      const result = await API.list(name, {
        page: state.page,
        page_size: PAGE_SIZE,
        search: state.search || "",
        order_by: currentOrderBy(),
        ...state.filters,
      }, dbAlias);

      state.schema = schema;
      state.selectedIDs.clear();

      const model = findModel(name);
      const columns = visibleListFields(schema);
      const filterFields = visibleFilterFields(schema);

      els.app.innerHTML =
        UI.sectionHead((model && model.plural) || schema.plural || name, `${Number(result.total || 0)} records · ${escapeHtml(dbAlias)}`, "Live data") +
        `
          <section class="toolbar">
          <input class="input" id="list-search" type="search" placeholder="Search records" value="${escapeHtml(state.search)}">
          <button class="btn btn-ghost" id="bulk-delete" ${schema.read_only ? "disabled" : ""}>Delete selected</button>
          <button class="btn btn-ghost" id="bulk-export">Export selected</button>
          <a class="btn btn-ghost" href="${API.exportURL(name, dbAlias)}" target="_blank" rel="noopener">Export CSV</a>
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

      bindListEvents(name, schema, result, columns, dbAlias);
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

  function bindListEvents(name, schema, result, columns, dbAlias) {
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
        navigate(modelHash(name, dbAlias, "new"));
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
          await API.bulkDelete(name, ids, dbAlias);
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
          const payload = await API.bulk(name, "export", ids, dbAlias);
          if (payload && payload.export_url) {
            window.open(payload.export_url, "_blank", "noopener");
            toast("Export started", "success");
            return;
          }
          const baseURL = API.exportURL(name, dbAlias);
          const separator = baseURL.includes("?") ? "&" : "?";
          const url = `${baseURL}${separator}ids=${encodeURIComponent(ids.join(","))}`;
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
        navigate(modelHash(name, dbAlias, btn.getAttribute("data-id")));
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
          await API.del(name, id, dbAlias);
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
        navigate(modelHash(name, dbAlias, btn.getAttribute("data-id")));
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
    const dbAlias = currentDatabaseAlias();
    updateNewButton();
    setAppBusy(true);
    els.app.innerHTML = loadingMarkup();

    try {
      const schema = await API.schema(name, dbAlias);
      state.schema = schema;

      const editing = id !== null && id !== undefined;
      let record = {};
      if (editing) {
        record = await API.get(name, id, dbAlias);
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
        navigate(modelHash(name, dbAlias));
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
            await API.update(name, id, payload, dbAlias);
            toast("Record updated", "success");
          } else {
            await API.create(name, payload, dbAlias);
            toast("Record created", "success");
          }
          await refreshModels(true);
          navigate(modelHash(name, dbAlias));
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

  function formatBytes(value) {
    const bytes = Number(value || 0);
    if (!Number.isFinite(bytes) || bytes <= 0) {
      return "0 B";
    }
    const units = ["B", "KB", "MB", "GB", "TB"];
    let idx = 0;
    let current = bytes;
    while (current >= 1024 && idx < units.length - 1) {
      current = current / 1024;
      idx += 1;
    }
    const precision = current >= 100 || idx === 0 ? 0 : 1;
    return `${current.toFixed(precision)} ${units[idx]}`;
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
    els.newRecordBtn.textContent = `New ${state.currentModel} @ ${currentDatabaseAlias()}`;
  }

  function updateRuntimeEnvironmentPill() {
    if (!els.runtimeEnvPill || !els.runtimeEnv) {
      return;
    }
    const envRaw = String((state.runtime && state.runtime.environment) || "development").trim();
    const env = envRaw || "development";
    els.runtimeEnvPill.textContent = env.charAt(0).toUpperCase() + env.slice(1);

    els.runtimeEnv.classList.remove("is-prod", "is-stage", "is-dev");
    const lower = env.toLowerCase();
    if (lower === "production" || lower === "prod") {
      els.runtimeEnv.classList.add("is-prod");
      return;
    }
    if (lower === "staging" || lower === "stage" || lower === "qa") {
      els.runtimeEnv.classList.add("is-stage");
      return;
    }
    els.runtimeEnv.classList.add("is-dev");
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

  function defaultDatabaseAlias() {
    const runtime = state.runtime || {};
    const databases = Array.isArray(runtime.databases) ? runtime.databases : [];
    const defaultEntry = databases.find(function (item) {
      return !!item.is_default;
    });
    if (defaultEntry && defaultEntry.alias) {
      return String(defaultEntry.alias);
    }
    if (databases[0] && databases[0].alias) {
      return String(databases[0].alias);
    }
    return "default";
  }

  function isKnownDatabaseAlias(alias) {
    const needle = String(alias || "").trim();
    if (!needle) {
      return false;
    }
    const runtime = state.runtime || {};
    const databases = Array.isArray(runtime.databases) ? runtime.databases : [];
    return databases.some(function (item) {
      return String(item.alias || "") === needle;
    });
  }

  function currentDatabaseAlias() {
    if (state.currentDBAlias && isKnownDatabaseAlias(state.currentDBAlias)) {
      return state.currentDBAlias;
    }
    state.currentDBAlias = defaultDatabaseAlias();
    return state.currentDBAlias;
  }

  function modelHash(modelName, dbAlias, suffix) {
    const model = String(modelName || "").trim();
    const db = String(dbAlias || defaultDatabaseAlias()).trim();
    if (!model) {
      return "#/";
    }
    let hash = `#/model/${encodeURIComponent(db)}/${encodeURIComponent(model)}`;
    if (suffix !== undefined && suffix !== null && String(suffix).trim() !== "") {
      hash += `/${encodeURIComponent(String(suffix).trim())}`;
    }
    return hash;
  }

  function readSidebarPreference() {
    try {
      return window.localStorage.getItem(SIDEBAR_PREF_KEY) === "1";
    } catch (_) {
      return false;
    }
  }

  function hydrateThemePreference() {
    const stored = readThemePreference();
    if (stored === "light" || stored === "dark") {
      applyTheme(stored, false);
      return;
    }
    applyTheme("dark", false);
  }

  function readThemePreference() {
    try {
      return String(window.localStorage.getItem(THEME_PREF_KEY) || "").trim().toLowerCase();
    } catch (_) {
      return "";
    }
  }

  function applyTheme(theme, persist) {
    const normalized = String(theme || "").toLowerCase() === "light" ? "light" : "dark";
    state.theme = normalized;
    if (document.body) {
      document.body.setAttribute("data-theme", normalized);
    }
    if (document.documentElement) {
      document.documentElement.style.colorScheme = normalized;
    }

    if (els.themeToggle) {
      const dark = normalized === "dark";
      els.themeToggle.classList.toggle("is-dark", dark);
      els.themeToggle.classList.toggle("is-light", !dark);
      els.themeToggle.setAttribute("aria-pressed", dark ? "true" : "false");
      els.themeToggle.setAttribute("title", dark ? "Switch to light mode" : "Switch to dark mode");
    }
    if (els.themeToggleLabel) {
      els.themeToggleLabel.textContent = normalized === "dark" ? "Dark" : "Light";
    }

    if (persist) {
      try {
        window.localStorage.setItem(THEME_PREF_KEY, normalized);
      } catch (_) {}
      toast(`Theme switched to ${normalized}`, "success");
    }
  }

  function persistSidebarPreference(collapsed) {
    try {
      if (collapsed) {
        window.localStorage.setItem(SIDEBAR_PREF_KEY, "1");
      } else {
        window.localStorage.removeItem(SIDEBAR_PREF_KEY);
      }
    } catch (_) {}
  }

  function applySidebarLayout() {
    if (!els.layout || !els.sidebarDockToggle) {
      return;
    }
    const collapsed = !!state.sidebarCollapsed && !window.matchMedia("(max-width: 1080px)").matches;
    els.layout.classList.toggle("collapsed", collapsed);
    els.sidebarDockToggle.classList.toggle("is-collapsed", collapsed);
    els.sidebarDockToggle.setAttribute("title", collapsed ? "Expand sidebar" : "Collapse sidebar");
    els.sidebarDockToggle.setAttribute("aria-label", collapsed ? "Expand sidebar" : "Collapse sidebar");
    els.sidebarDockToggle.setAttribute("aria-pressed", collapsed ? "true" : "false");
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
      label: "Go to overview",
      desc: "Dashboard and runtime summary",
      run: function () {
        navigate("#/");
      },
    });

    items.push({
      label: "Open Data Studio",
      desc: "Models by engine and database",
      run: function () {
        navigate("#/data-studio");
      },
    });

    items.push({
      label: "Open Infra Manager",
      desc: "Session runtime telemetry",
      run: function () {
        navigate("#/sessions");
      },
    });

    items.push({
      label: "Open Network Inspector",
      desc: "Live requests and SQL stream",
      run: function () {
        navigate("#/live");
      },
    });

    items.push({
      label: "Open System Pulse",
      desc: "Goroutines, memory, DB pools and env",
      run: function () {
        navigate("#/system");
      },
    });

    if (state.currentModel) {
      items.push({
        label: `Create ${state.currentModel}`,
        desc: "Quick action",
        run: function () {
          navigate(modelHash(state.currentModel, currentDatabaseAlias(), "new"));
        },
      });
    }

    state.models.forEach(function (model) {
      const alias = currentDatabaseAlias();
      const baseCount = model.counts && alias ? model.counts[alias] : model.count;
      const countLabel = isKnownCount(baseCount) ? String(Number(baseCount)) : "n/a";
      items.push({
        label: `Open ${model.name}`,
        desc: `${countLabel} records · ${alias}`,
        run: function () {
          navigate(modelHash(model.name, alias));
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
    const previousHTML = button.innerHTML;
    button.disabled = true;
    if (label) {
      button.textContent = label;
    }
    return function () {
      button.disabled = false;
      button.innerHTML = previousHTML;
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

  function iconGlyph(name) {
    switch (String(name || "").toLowerCase()) {
      case "database":
        return '<svg class="icon-svg" viewBox="0 0 24 24" aria-hidden="true"><ellipse cx="12" cy="6" rx="7" ry="3"></ellipse><path d="M5 6v6c0 1.7 3.1 3 7 3s7-1.3 7-3V6"></path><path d="M5 12v6c0 1.7 3.1 3 7 3s7-1.3 7-3v-6"></path></svg>';
      case "model":
        return '<svg class="icon-svg" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 3 4 7v10l8 4 8-4V7l-8-4z"></path><path d="M4 7l8 4 8-4"></path><path d="M12 11v10"></path></svg>';
      case "close":
        return '<svg class="icon-svg" viewBox="0 0 24 24" aria-hidden="true"><path d="M6 6l12 12"></path><path d="M18 6 6 18"></path></svg>';
      default:
        return "";
    }
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

  function runNumberAnimations(container) {
    const elements = container.querySelectorAll(".animate-number[data-value]");
    elements.forEach(function (el) {
      const target = Number(el.getAttribute("data-value") || 0);
      if (target <= 0 || !Number.isFinite(target)) {
        el.textContent = el.getAttribute("data-value");
        return;
      }
      
      const duration = 1200;
      const start = performance.now();
      
      function update(time) {
        const current = time - start;
        const progress = Math.min(current / duration, 1);
        
        const easeOutQuad = 1 - (1 - progress) * (1 - progress);
        const currentVal = Math.floor(easeOutQuad * target);
        
        el.textContent = currentVal.toLocaleString();
        
        if (progress < 1) {
          requestAnimationFrame(update);
        } else {
          el.textContent = target.toLocaleString();
        }
      }
      
      requestAnimationFrame(update);
    });
  }

  init();
})();
