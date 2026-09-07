package shop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jcsvwinston/nucleus/pkg/nucleus"
	"github.com/jcsvwinston/orbit/quarkbridge"
	"github.com/jcsvwinston/quark"
)

// module carries the Quark clients. base is built in main (it also backs
// Orbit's Data Studio); bridged is derived in OnStart, once the Runtime is
// available: it wraps every statement with the quarkbridge middleware, so the
// SQL the HTTP handlers run shows up in Orbit's live feed correlated to the
// request (RequestID/TraceID/UserID from the ctx).
type module struct {
	base    *quark.Client
	bridged *quark.Client
}

// Module returns the shop feature as a nucleus module. The request handlers
// use the bridged client; Data Studio (wired in main via quarkdatasource)
// uses the base client, so admin browsing does not flood the live feed.
func Module(base *quark.Client) nucleus.ModuleSpec {
	m := &module{base: base}

	return nucleus.Module[struct{}]{
		Name: "shop",

		// The module opens its own routes under the default-deny enforcer;
		// nothing in rbac_policy.csv mentions /api. Anonymous callers can
		// read both lists and CREATE an article — the create row is a
		// development default so the first `curl -X POST` lands; scope it
		// to a role ({Subject: "editor", ...}) before the app faces a
		// network. An operator deny in the host CSV always overrides.
		Policies: []nucleus.PolicyRule{
			{Subject: "anonymous", Object: "/api/authors", Action: "read"},
			{Subject: "anonymous", Object: "/api/articles", Action: "read"},
			{Subject: "anonymous", Object: "/api/articles", Action: "create"},
		},
		// A header-token JSON API is not CSRF-forgeable; the exemption keeps
		// curl and SDK clients working while the browser routes stay guarded.
		CSRFExempt: []string{"/api/"},

		OnStart: func(ctx context.Context, rt nucleus.Runtime, _ struct{}) error {
			bridged, err := m.base.WithOptions(
				quark.WithMiddleware(quarkbridge.New(rt.Observability())),
			)
			if err != nil {
				return fmt.Errorf("shop: derive bridged quark client: %w", err)
			}
			m.bridged = bridged
			rt.Logger().Info("shop: quark client bridged to the live SQL feed")
			return nil
		},

		Routes: func(r nucleus.Router, _ struct{}) {
			r.Get("/api/authors", m.listAuthors)
			r.Get("/api/articles", m.listArticles)
			r.Post("/api/articles", m.createArticle)
		},
	}.Build()
}

// listPageSize bounds both list endpoints. Quark caps an unbounded List()
// at 100 rows anyway, but it logs a WARN on every call that leaves the cap
// implicit; naming the page size keeps the app log clean and makes the
// bound a decision this module owns (replace it with a query parameter
// when the lists grow).
const listPageSize = 100

func (m *module) listAuthors(c *nucleus.Context) error {
	authors, err := quark.For[Author](c.Request.Context(), m.bridged).OrderBy("name", "ASC").Limit(listPageSize).List()
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"authors": authors, "count": len(authors)})
}

func (m *module) listArticles(c *nucleus.Context) error {
	q := quark.For[Article](c.Request.Context(), m.bridged).OrderBy("id", "DESC").Limit(listPageSize)
	if author := c.Query("author_id"); author != "" {
		q = q.Where("author_id", "=", author)
	}
	articles, err := q.List()
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"articles": articles, "count": len(articles)})
}

func (m *module) createArticle(c *nucleus.Context) error {
	var in struct {
		AuthorID int64  `json:"author_id"`
		Title    string `json:"title"`
		Body     string `json:"body"`
	}
	if err := json.NewDecoder(c.Request.Body).Decode(&in); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
	}
	if in.Title == "" || in.AuthorID == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "author_id and title are required"})
	}
	a := Article{AuthorID: in.AuthorID, Title: in.Title, Body: in.Body}
	if err := quark.For[Article](c.Request.Context(), m.bridged).Create(&a); err != nil {
		// Titles are unique. Quark recognises the engine's duplicate-key
		// error only when its driver module is linked (see main.go); with
		// the bare database/sql driver this branch is dead code and the
		// caller gets a 500 — the test below posts a duplicate to prove the
		// classifier is wired.
		if quark.IsUniqueViolation(err) {
			return c.JSON(http.StatusConflict, map[string]string{"error": "an article with that title already exists"})
		}
		return err
	}
	return c.JSON(http.StatusCreated, a)
}

// Migrate creates the tables from the model registry and seeds a first
// author/article pair when the table is empty, so the app has data on first
// boot. Called from main before the server starts.
func Migrate(ctx context.Context, client *quark.Client) error {
	if err := client.RegisterModel(&Author{}, &Article{}); err != nil {
		return err
	}
	if err := client.MigrateRegistered(ctx); err != nil {
		return err
	}
	n, err := quark.For[Author](ctx, client).Count()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	ada := Author{Name: "Ada Lovelace"}
	if err := quark.For[Author](ctx, client).Create(&ada); err != nil {
		return err
	}
	first := Article{AuthorID: ada.ID, Title: "Hello, Quantum", Body: "Nucleus + Quark + Orbit, wired together."}
	return quark.For[Article](ctx, client).Create(&first)
}
