/* GoFrame Admin SPA — Vanilla JS ES2020+ */
(function () {
  "use strict";

  const API = (() => {
    const base = window.location.pathname.replace(/\/+$/, "");
    const api = base + "/api";
    async function req(path, opts = {}) {
      const r = await fetch(api + path, {
        headers: { "Content-Type": "application/json", ...opts.headers },
        ...opts,
      });
      if (!r.ok) {
        const err = await r.json().catch(() => ({ error: { message: r.statusText } }));
        throw new Error(err.error?.message || r.statusText);
      }
      return r.headers.get("content-type")?.includes("json") ? r.json() : r;
    }
    return {
      models: () => req("/models"),
      schema: (n) => req(`/models/${n}/schema`),
      list: (n, params) => req(`/models/${n}?${new URLSearchParams(params)}`),
      get: (n, id) => req(`/models/${n}/${id}`),
      create: (n, data) => req(`/models/${n}`, { method: "POST", body: JSON.stringify(data) }),
      update: (n, id, data) => req(`/models/${n}/${id}`, { method: "PUT", body: JSON.stringify(data) }),
      del: (n, id) => req(`/models/${n}/${id}`, { method: "DELETE" }),
      bulk: (n, action, ids) => req(`/models/${n}/bulk`, { method: "POST", body: JSON.stringify({ action, ids }) }),
      exportURL: (n) => api + `/models/${n}/export`,
    };
  })();

  /* State */
  let state = { models: [], currentModel: null, schema: null };

  /* Toast */
  function toast(msg, type = "success") {
    const el = document.createElement("div");
    el.className = `toast ${type}`;
    el.textContent = msg;
    document.getElementById("toasts").appendChild(el);
    setTimeout(() => el.remove(), 3000);
  }

  /* Router */
  function navigate(hash) {
    window.location.hash = hash;
  }

  async function route() {
    const hash = window.location.hash || "#/";
    const parts = hash.slice(2).split("/");

    document.querySelectorAll(".sidebar a").forEach((a) => a.classList.remove("active"));

    if (parts[0] === "" || parts[0] === undefined) {
      document.querySelector('[data-nav="dashboard"]')?.classList.add("active");
      await renderDashboard();
    } else if (parts[0] === "model" && parts[1] && parts[2] === "new") {
      await renderForm(parts[1]);
    } else if (parts[0] === "model" && parts[1] && parts[2]) {
      await renderForm(parts[1], parts[2]);
    } else if (parts[0] === "model" && parts[1]) {
      document.querySelector(`[data-nav="${parts[1]}"]`)?.classList.add("active");
      await renderList(parts[1]);
    }
  }

  /* Init */
  async function init() {
    try {
      const data = await API.models();
      state.models = data.models || [];
      document.getElementById("site-title").textContent = data.title || "Admin";
      document.title = data.title || "GoFrame Admin";
      renderNav();
      window.addEventListener("hashchange", route);
      await route();
    } catch (e) {
      document.getElementById("app").innerHTML = `<p>Error loading admin: ${e.message}</p>`;
    }
  }

  function renderNav() {
    const nav = document.getElementById("model-nav");
    nav.innerHTML = state.models
      .map(
        (m) =>
          `<a href="#/model/${m.name}" data-nav="${m.name}">${m.icon || "&#128196;"} ${m.plural} <span class="badge">${m.count}</span></a>`
      )
      .join("");
  }

  /* Dashboard */
  async function renderDashboard() {
    const app = document.getElementById("app");
    app.innerHTML = `<h2>Dashboard</h2><div class="cards">${state.models
      .map(
        (m) =>
          `<div class="card" onclick="location.hash='#/model/${m.name}'">
            <div class="icon">${m.icon || "&#128196;"}</div>
            <div class="count">${m.count}</div>
            <div class="label">${m.plural}</div>
          </div>`
      )
      .join("")}</div>`;
  }

  /* List View */
  async function renderList(name) {
    const app = document.getElementById("app");
    app.innerHTML = "<p>Loading...</p>";

    try {
      const [schema, data] = await Promise.all([
        API.schema(name),
        API.list(name, { page: 1, page_size: 25 }),
      ]);
      state.schema = schema;
      state.currentModel = name;

      const listFields = schema.fields.filter((f) => f.is_list && !f.is_excluded);
      const cols = listFields.length > 0 ? listFields : schema.fields.filter((f) => !f.is_excluded).slice(0, 6);

      let html = `<h2>${schema.icon || ""} ${schema.plural || name}</h2>`;
      html += `<div class="toolbar">`;
      html += `<input type="search" id="search" placeholder="Search..." value="">`;
      if (!schema.read_only) {
        html += `<button class="btn btn-primary" onclick="location.hash='#/model/${name}/new'">+ New</button>`;
      }
      html += `<a class="btn btn-sm" href="${API.exportURL(name)}" target="_blank">Export CSV</a>`;
      html += `</div>`;

      html += `<div class="table-wrap"><table><thead><tr>`;
      if (!schema.read_only) html += `<th><input type="checkbox" id="selectAll"></th>`;
      cols.forEach((c) => (html += `<th data-col="${c.column}">${c.label}</th>`));
      html += `<th>Actions</th></tr></thead><tbody id="tbody">`;
      html += renderRows(data.items || [], cols, schema.read_only);
      html += `</tbody></table></div>`;

      html += renderPagination(data.page, data.total_pages, data.total);
      app.innerHTML = html;

      // Search
      let debounce;
      document.getElementById("search").addEventListener("input", (e) => {
        clearTimeout(debounce);
        debounce = setTimeout(() => loadPage(name, 1, e.target.value), 300);
      });

      // Select all
      document.getElementById("selectAll")?.addEventListener("change", (e) => {
        document.querySelectorAll(".row-check").forEach((c) => (c.checked = e.target.checked));
      });
    } catch (e) {
      app.innerHTML = `<p>Error: ${e.message}</p>`;
    }
  }

  function renderRows(items, cols, readOnly) {
    if (!items || items.length === 0) return "<tr><td colspan='99'>No records found</td></tr>";
    return items
      .map((item) => {
        let row = "<tr>";
        if (!readOnly) row += `<td><input type="checkbox" class="row-check" value="${item.id}"></td>`;
        cols.forEach((c) => {
          let val = item[c.column] ?? item[c.name] ?? item[c.name.toLowerCase()] ?? "";
          if (typeof val === "boolean") val = val ? "Yes" : "No";
          if (c.html_type === "datetime-local" && val) val = new Date(val).toLocaleString();
          row += `<td>${escapeHtml(String(val))}</td>`;
        });
        row += `<td>`;
        if (!readOnly) {
          row += `<button class="btn btn-sm btn-primary" onclick="location.hash='#/model/${state.currentModel}/${item.id}'">Edit</button> `;
          row += `<button class="btn btn-sm btn-danger" onclick="deleteRecord('${state.currentModel}',${item.id})">Delete</button>`;
        } else {
          row += `<button class="btn btn-sm" onclick="location.hash='#/model/${state.currentModel}/${item.id}'">View</button>`;
        }
        row += `</td></tr>`;
        return row;
      })
      .join("");
  }

  function renderPagination(page, totalPages, total) {
    if (totalPages <= 1) return `<div class="pagination"><span class="info">${total} records</span></div>`;
    let html = `<div class="pagination">`;
    html += `<button ${page <= 1 ? "disabled" : ""} onclick="goPage(${page - 1})">Prev</button>`;
    for (let i = 1; i <= Math.min(totalPages, 10); i++) {
      html += `<button class="${i === page ? "active" : ""}" onclick="goPage(${i})">${i}</button>`;
    }
    html += `<button ${page >= totalPages ? "disabled" : ""} onclick="goPage(${page + 1})">Next</button>`;
    html += `<span class="info">${total} records</span></div>`;
    return html;
  }

  async function loadPage(name, page, search = "") {
    try {
      const params = { page, page_size: 25 };
      if (search) params.search = search;
      const data = await API.list(name, params);
      const schema = state.schema;
      const cols = schema.fields.filter((f) => f.is_list && !f.is_excluded);
      const useCols = cols.length > 0 ? cols : schema.fields.filter((f) => !f.is_excluded).slice(0, 6);
      document.getElementById("tbody").innerHTML = renderRows(data.items || [], useCols, schema.read_only);
    } catch (e) {
      toast(e.message, "error");
    }
  }

  /* Form View */
  async function renderForm(name, id) {
    const app = document.getElementById("app");
    app.innerHTML = "<p>Loading...</p>";

    try {
      const schema = await API.schema(name);
      let record = {};
      if (id) {
        record = await API.get(name, id);
      }

      const isEdit = !!id;
      const title = isEdit ? `Edit ${schema.name} #${id}` : `New ${schema.name}`;

      let html = `<h2>${schema.icon || ""} ${title}</h2>`;
      html += `<div class="form"><form id="recordForm">`;

      for (const f of schema.fields) {
        if (f.is_excluded || f.is_pk) continue;
        if (isEdit && f.is_readonly && f.name !== "ID") {
          html += `<div class="form-group"><label>${f.label}</label><input value="${escapeHtml(String(record[f.column] ?? record[f.name] ?? ""))}" disabled></div>`;
          continue;
        }
        if (f.is_readonly) continue;

        const val = record[f.column] ?? record[f.name] ?? record[f.name.toLowerCase()] ?? "";
        html += `<div class="form-group"><label>${f.label}${f.is_required ? " *" : ""}</label>`;

        if (f.choices && f.choices.length > 0) {
          html += `<select name="${f.column}" ${f.is_required ? "required" : ""}>`;
          html += `<option value="">-- Select --</option>`;
          f.choices.forEach((c) => {
            html += `<option value="${c.value}" ${val == c.value ? "selected" : ""}>${c.label}</option>`;
          });
          html += `</select>`;
        } else if (f.html_type === "textarea") {
          html += `<textarea name="${f.column}" ${f.is_required ? "required" : ""}>${escapeHtml(String(val))}</textarea>`;
        } else if (f.html_type === "checkbox") {
          html += `<input type="checkbox" name="${f.column}" ${val ? "checked" : ""}>`;
        } else {
          html += `<input type="${f.html_type || "text"}" name="${f.column}" value="${escapeHtml(String(val))}" ${f.is_required ? "required" : ""}>`;
        }
        html += `</div>`;
      }

      html += `<div class="form-actions">`;
      if (!schema.read_only) html += `<button type="submit" class="btn btn-primary">${isEdit ? "Save" : "Create"}</button>`;
      html += `<button type="button" class="btn" onclick="location.hash='#/model/${name}'">Cancel</button>`;
      html += `</div></form></div>`;

      app.innerHTML = html;

      document.getElementById("recordForm").addEventListener("submit", async (e) => {
        e.preventDefault();
        const formData = new FormData(e.target);
        const data = {};
        for (const [key, value] of formData.entries()) {
          data[key] = value;
        }
        // Handle checkboxes
        e.target.querySelectorAll('input[type="checkbox"]').forEach((cb) => {
          data[cb.name] = cb.checked;
        });
        // Convert numeric strings
        for (const f of schema.fields) {
          if (data[f.column] !== undefined && (f.type === "uint" || f.type === "int" || f.type === "int64" || f.type === "float64")) {
            data[f.column] = Number(data[f.column]);
          }
        }

        try {
          if (isEdit) {
            await API.update(name, id, data);
            toast("Record updated");
          } else {
            await API.create(name, data);
            toast("Record created");
          }
          navigate(`#/model/${name}`);
        } catch (err) {
          toast(err.message, "error");
        }
      });
    } catch (e) {
      app.innerHTML = `<p>Error: ${e.message}</p>`;
    }
  }

  /* Global functions */
  window.goPage = (page) => {
    const search = document.getElementById("search")?.value || "";
    loadPage(state.currentModel, page, search);
  };

  window.deleteRecord = async (name, id) => {
    if (!confirm("Are you sure you want to delete this record?")) return;
    try {
      await API.del(name, id);
      toast("Record deleted");
      await renderList(name);
    } catch (e) {
      toast(e.message, "error");
    }
  };

  function escapeHtml(s) {
    const div = document.createElement("div");
    div.textContent = s;
    return div.innerHTML;
  }

  init();
})();
