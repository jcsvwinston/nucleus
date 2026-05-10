import type {ReactNode} from 'react';
import Link from '@docusaurus/Link';
import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import Layout from '@theme/Layout';

import styles from './index.module.css';

type Feature = {
  title: string;
  body: ReactNode;
  icon: ReactNode;
};

const FEATURES: Feature[] = [
  {
    title: 'Stdlib-first runtime',
    body: (
      <>
        Built on <code>net/http</code>, <code>database/sql</code>,{' '}
        <code>log/slog</code> and <code>context</code>. New third-party
        deps require an ADR and a dependency-impact review.
      </>
    ),
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M4 7h16M4 12h16M4 17h10"/>
      </svg>
    ),
  },
  {
    title: 'Compatibility by contract',
    body: (
      <>
        Public symbols in <code>pkg/*</code>, registered CLI commands and
        config keys are pinned by freeze tests under <code>contracts/</code>.
        No silent removals.
      </>
    ),
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
        <path d="m9 12 2 2 4-4"/>
      </svg>
    ),
  },
  {
    title: 'Embedded admin panel',
    body: (
      <>
        React + TypeScript admin auto-generated from your registered
        models. CRUD, bulk actions, audit log, RBAC, multi-tenant filters
        — all in the binary.
      </>
    ),
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <rect width="18" height="18" x="3" y="3" rx="2"/>
        <path d="M9 3v18M3 9h18"/>
      </svg>
    ),
  },
  {
    title: 'SQL-first data layer',
    body: (
      <>
        Plain <code>database/sql</code> with health checks, telemetry, and
        SQL migrations. No ORM. Postgres, MySQL, SQLite by default; MSSQL
        and Oracle as build tags.
      </>
    ),
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <ellipse cx="12" cy="5" rx="9" ry="3"/>
        <path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/>
        <path d="M3 12c0 1.66 4 3 9 3s9-1.34 9-3"/>
      </svg>
    ),
  },
  {
    title: 'Secure by default',
    body: (
      <>
        Argon2id passwords, CSRF on for state-changing form posts, secure
        cookies in production, CORS that denies unknown origins, rate
        limiting on every public route.
      </>
    ),
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M12 22s8-4 8-10V6l-8-4-8 4v6c0 6 8 10 8 10z"/>
      </svg>
    ),
  },
  {
    title: 'Observability built in',
    body: (
      <>
        Structured <code>slog</code> logging, OpenTelemetry traces and
        metrics, deterministic <code>/healthz</code> endpoints.
        Configure once in <code>nucleus.yml</code>.
      </>
    ),
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <path d="M3 12h4l3-9 4 18 3-9h4"/>
      </svg>
    ),
  },
];

const SUBSYSTEMS: {name: string; pkg: string; desc: string}[] = [
  {name: 'Application', pkg: 'pkg/app',     desc: 'Composition root. Lifecycle, extensions, request scope.'},
  {name: 'Router',      pkg: 'pkg/router',  desc: 'HTTP router + middleware: CORS, CSRF, rate limit, OTel.'},
  {name: 'Database',    pkg: 'pkg/db',      desc: 'database/sql wrapper with telemetry, health, migrations.'},
  {name: 'Models',      pkg: 'pkg/model',   desc: 'Metadata-driven registry. Drives admin and CRUD.'},
  {name: 'Auth',        pkg: 'pkg/auth',    desc: 'JWT, Argon2id, session manager (memory/SQL/Redis).'},
  {name: 'Admin',       pkg: 'pkg/admin',   desc: 'Embedded React admin generated from your models.'},
  {name: 'Storage',     pkg: 'pkg/storage', desc: 'Local, S3, GCS, Azure — one provider-agnostic API.'},
  {name: 'Tasks',       pkg: 'pkg/tasks',   desc: 'Background jobs on Asynq + Redis with outbox.'},
];

function Hero(): ReactNode {
  return (
    <header className={styles.hero}>
      <div className={`container ${styles.heroInner}`}>
        <div>
          <span className={styles.eyebrow}>
            <span className={styles.dot}/>
            Pre-1.0 — APIs stabilising. Built in the open.
          </span>
          <h1 className={styles.heroTitle}>
            The Go framework for serious{' '}
            <span className={styles.heroAccent}>web apps and APIs</span>.
          </h1>
          <p className={styles.heroSubtitle}>
            Nucleus is an MVC + REST framework built on Go's standard
            library. Explicit lifecycle, SQL-first data layer, embedded
            admin, and a public surface governed by contracts. Production
            defaults from day one.
          </p>
          <div className={styles.heroCtas}>
            <Link className={styles.primaryCta} to="/docs/getting-started/quickstart">
              Quickstart →
            </Link>
            <Link
              className={styles.secondaryCta}
              href="https://github.com/jcsvwinston/nucleus"
              target="_blank"
              rel="noopener noreferrer">
              <svg width="16" height="16" viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
                <path d="M12 .5a12 12 0 0 0-3.79 23.4c.6.11.82-.26.82-.58v-2.06c-3.34.73-4.04-1.61-4.04-1.61-.55-1.4-1.34-1.78-1.34-1.78-1.1-.75.08-.74.08-.74 1.21.09 1.85 1.25 1.85 1.25 1.08 1.85 2.83 1.31 3.52 1 .11-.78.42-1.31.76-1.61-2.67-.31-5.47-1.34-5.47-5.95 0-1.31.47-2.39 1.24-3.23-.13-.31-.54-1.55.12-3.23 0 0 1.01-.32 3.3 1.23a11.4 11.4 0 0 1 6 0c2.29-1.55 3.3-1.23 3.3-1.23.66 1.68.25 2.92.12 3.23.78.84 1.24 1.92 1.24 3.23 0 4.62-2.81 5.64-5.49 5.94.43.37.81 1.1.81 2.22v3.29c0 .32.22.7.83.58A12 12 0 0 0 12 .5z"/>
              </svg>
              View on GitHub
            </Link>
          </div>
          <div className={styles.heroMetaRow}>
            <span><strong>Go 1.25+</strong> · stdlib runtime</span>
            <span><strong>SQLite · Postgres · MySQL</strong> by default</span>
            <span><strong>MIT</strong> licensed</span>
          </div>
        </div>
        <div>
          <div className={styles.terminal} aria-hidden="true">
            <div className={styles.terminalHeader}>
              <span className={styles.terminalDot}/>
              <span className={styles.terminalDot}/>
              <span className={styles.terminalDot}/>
              <span className={styles.terminalTitle}>~ / myapp</span>
            </div>
            <div className={styles.terminalBody}>
              <span className="term-comment"># Install the CLI</span>{'\n'}
              <span className="term-prompt">$</span>{' '}
              <span className="term-cmd">go install</span>{' '}
              <span className="term-arg">github.com/jcsvwinston/nucleus/cmd/nucleus@latest</span>{'\n\n'}
              <span className="term-comment"># Scaffold and run</span>{'\n'}
              <span className="term-prompt">$</span>{' '}
              <span className="term-cmd">nucleus</span>{' '}
              <span className="term-arg">new myapp</span>{'\n'}
              <span className="term-prompt">$</span>{' '}
              <span className="term-cmd">cd</span>{' '}
              <span className="term-arg">myapp</span>{'\n'}
              <span className="term-prompt">$</span>{' '}
              <span className="term-cmd">nucleus</span>{' '}
              <span className="term-arg">migrate</span>{'\n'}
              <span className="term-prompt">$</span>{' '}
              <span className="term-cmd">nucleus</span>{' '}
              <span className="term-arg">serve</span>{'\n'}
              <span className="term-ok">→ listening on :8080  /  /api  /  /admin</span>
            </div>
          </div>
        </div>
      </div>
    </header>
  );
}

function Features(): ReactNode {
  return (
    <section className={styles.section}>
      <div className="container">
        <div className={styles.sectionHeader}>
          <div className={styles.sectionEyebrow}>Why Nucleus</div>
          <h2 className={styles.sectionTitle}>Production-grade, Go-idiomatic, no surprises.</h2>
          <p className={styles.sectionLede}>
            Six properties define the framework. Each is enforced by tests,
            not by aspirations in a README.
          </p>
        </div>
        <div className={styles.featureGrid}>
          {FEATURES.map((f) => (
            <article key={f.title} className={styles.featureCard}>
              <div className={styles.featureIcon}>{f.icon}</div>
              <h3 className={styles.featureTitle}>{f.title}</h3>
              <p className={styles.featureBody}>{f.body}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

function CodeShowcase(): ReactNode {
  return (
    <section className={`${styles.section} ${styles.sectionAlt}`}>
      <div className="container">
        <div className={styles.showcase}>
          <div>
            <div className={styles.sectionEyebrow}>Fluent or full-MVC</div>
            <h2 className={styles.showcaseTitle}>One construction call. Everything wired.</h2>
            <p className={styles.showcaseBody}>
              The fluent entry point assembles a full application — config,
              database, model registry, router, handlers — in a builder you
              can read top-to-bottom. For real projects, the same wiring
              lives in <code>cmd/server/main.go</code>.
            </p>
            <ul className={styles.showcaseList}>
              <li>No init-time side effects. No hidden globals.</li>
              <li>Multiple <code>App</code> instances per process for tests.</li>
              <li>Lifecycle observable: <code>Run</code>, <code>Shutdown</code>, hooks.</li>
              <li>Same primitives the scaffold templates emit.</li>
            </ul>
          </div>
          <pre className={styles.codeBlock}>
            <code>
              <span className="code-keyword">package</span>{' '}
              <span className="code-ident">main</span>{'\n\n'}
              <span className="code-keyword">import</span>{' '}
              <span className="code-string">"github.com/jcsvwinston/nucleus/pkg/nucleus"</span>{'\n\n'}
              <span className="code-keyword">type</span>{' '}
              <span className="code-type">Article</span>{' '}
              <span className="code-keyword">struct</span> {'{'}{'\n'}
              {'    '}<span className="code-ident">ID</span>{'    '}<span className="code-type">int64</span>{'   '}<span className="code-tag">{'`json:"id"    db:"id,primary"`'}</span>{'\n'}
              {'    '}<span className="code-ident">Title</span>{' '}<span className="code-type">string</span>{'  '}<span className="code-tag">{'`json:"title" db:"title" validate:"required"`'}</span>{'\n'}
              {'}'}{'\n\n'}
              <span className="code-keyword">func</span>{' '}
              <span className="code-fn">main</span>() {'{'}{'\n'}
              {'    '}<span className="code-package">nucleus</span>.<span className="code-fn">New</span>().{'\n'}
              {'        '}<span className="code-fn">Port</span>(<span className="code-type">8080</span>).{'\n'}
              {'        '}<span className="code-fn">SQLite</span>(<span className="code-string">"app.db"</span>).{'\n'}
              {'        '}<span className="code-fn">Model</span>(&<span className="code-type">Article</span>{'{}'}).{'\n'}
              {'        '}<span className="code-fn">AutoMigrate</span>().{'\n'}
              {'        '}<span className="code-fn">Get</span>(<span className="code-string">"/api/articles"</span>, <span className="code-fn">listArticles</span>).{'\n'}
              {'        '}<span className="code-fn">Run</span>(){'\n'}
              {'}'}{'\n'}
            </code>
          </pre>
        </div>
      </div>
    </section>
  );
}

function Subsystems(): ReactNode {
  return (
    <section className={styles.section}>
      <div className="container">
        <div className={styles.sectionHeader}>
          <div className={styles.sectionEyebrow}>What's in the box</div>
          <h2 className={styles.sectionTitle}>Subsystems, all governed by the same contract.</h2>
          <p className={styles.sectionLede}>
            Each subsystem ships with sane defaults, structured logging,
            health hooks and a place in the embedded admin.
          </p>
        </div>
        <div className={styles.subsystemGrid}>
          {SUBSYSTEMS.map((s) => (
            <div key={s.name} className={styles.subsystem}>
              <div className={styles.subsystemName}>
                {s.name}
                <code>{s.pkg}</code>
              </div>
              <p className={styles.subsystemBody}>{s.desc}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}

function FinalCta(): ReactNode {
  return (
    <section className={styles.finalCta}>
      <div className="container">
        <h2>Ship a real app this afternoon.</h2>
        <p>
          Install the CLI, scaffold a project, and have a database, an
          admin panel, and a REST API running locally before the kettle
          boils.
        </p>
        <div className={styles.heroCtas}>
          <Link className={styles.primaryCta} to="/docs/getting-started/installation">
            Install the CLI →
          </Link>
          <Link
            className={styles.secondaryCta}
            to="/docs/architecture/principles">
            Read the principles
          </Link>
        </div>
      </div>
    </section>
  );
}

export default function Home(): ReactNode {
  const {siteConfig} = useDocusaurusContext();
  return (
    <Layout
      title={siteConfig.title}
      description={siteConfig.tagline as string}>
      <Hero/>
      <main>
        <Features/>
        <CodeShowcase/>
        <Subsystems/>
        <FinalCta/>
      </main>
    </Layout>
  );
}
