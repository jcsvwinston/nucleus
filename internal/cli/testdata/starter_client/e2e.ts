// The arc's gate (A10): the suite starter's API, consumed through the
// TypeScript client generated from the document the application serves.
// Run by TestSuiteStarterTypeScriptClient against the booted starter:
//
//   node e2e.ts http://127.0.0.1:<port>
//
// Every call below goes through the generated client — its types, its
// method names, its error class — and nothing else.
import { Client, ApiError } from "./client.ts";
import type { Article, ArticleList, AuthorList, CreateArticle } from "./client.ts";

function check(ok: boolean, what: string, got: unknown): void {
  if (!ok) throw new Error(what + ": " + JSON.stringify(got));
}

const api = new Client({ baseUrl: process.argv[2] });

const authors: AuthorList = await api.listAuthors();
check(authors.count >= 1 && authors.authors !== null, "the seeded author", authors);
const ada = authors.authors![0];

const seeded: ArticleList = await api.listArticles();
check(seeded.articles !== null && seeded.articles.some((a) => a.Title === "Hello, Quantum"), "the seeded article", seeded);

const input: CreateArticle = { author_id: ada.ID, title: "From the generated client", body: "typed end to end" };
const created: Article = await api.createArticle(input);
check(created.ID > 0 && created.Title === input.title && created.AuthorID === ada.ID, "the created article", created);

const mine: ArticleList = await api.listArticles({ author_id: ada.ID });
check(mine.articles !== null && mine.articles.some((a) => a.ID === created.ID), "the filter by author", mine);

try {
  await api.createArticle(input);
  throw new Error("a duplicate title was accepted");
} catch (err) {
  check(err instanceof ApiError && err.status === 409, "a duplicate title answers 409", err);
}

try {
  await api.createArticle({ author_id: ada.ID, title: "" });
  throw new Error("an article without a title was accepted");
} catch (err) {
  check(err instanceof ApiError && err.status >= 400 && err.status < 500, "a missing title is refused before the handler", err);
}

console.log("starter client e2e: ok");
