# Vercel Serverless Deployment Design

Status: **Design proposal**  
Target branch: `design/vercel-serverless-postgres`  
Implementation status: **Not implemented**

## 1. Summary

This document defines a low-intrusion design for adding a supported Vercel deployment mode to Pixel Telo Mast Self-host.

The current application is designed as a long-running process with local persistent state. It reads `config.yaml`, stores an authentication token and runtime state on disk, uses SQLite for query caching and metadata, optionally manages a local baseline SQLite database, starts background goroutines, and listens on a TCP port.

Vercel Functions use a request-driven serverless execution model and do not provide durable local filesystem storage. Therefore, the Vercel deployment must not attempt to emulate the existing local runtime directly.

The proposed solution is to keep the existing domain, provider, query and HTTP layers largely unchanged, while introducing two deployment adapters:

1. a PostgreSQL implementation of the existing runtime repository interfaces;
2. a Vercel HTTP entry point that builds and invokes the existing router without calling `net.Listen`.

The first Vercel release intentionally excludes baseline synchronization and the temporary pairing page. This keeps the implementation small, predictable and testable.

The expected end-user flow is:

1. Click **Deploy with Vercel**.
2. Create or attach a PostgreSQL database, preferably Neon through Vercel Marketplace.
3. Set `MAST_TOKEN`.
4. Deploy.
5. Enter the Vercel HTTPS URL and `MAST_TOKEN` in Pixel Telo.

No `config.yaml`, local initialization command, certificate generation, local data directory or manual database migration command should be required for Vercel deployments.

---

## 2. Goals

The implementation MUST:

- support deployment as a Vercel Go Function;
- preserve the existing Docker and binary deployment behavior;
- reuse the existing HTTP API, query service and provider implementations;
- use PostgreSQL for durable runtime query cache and runtime metadata;
- support any PostgreSQL-compatible service through `DATABASE_URL`;
- automatically create or migrate required PostgreSQL tables at application initialization;
- read Vercel-specific configuration from environment variables;
- rely on Vercel for public HTTPS termination;
- write logs to stdout/stderr instead of rotating local log files;
- avoid background work that is required to continue after an HTTP request completes;
- clearly advertise that baseline synchronization is not supported by the first Vercel implementation;
- clearly advertise that the temporary pairing page is not supported by the first Vercel implementation;
- provide a simple deployment path suitable for Vercel Hobby and a free PostgreSQL offering such as Neon when available.

The implementation SHOULD:

- minimize branching inside the existing core packages;
- keep deployment-specific code close to composition roots/adapters;
- make the PostgreSQL adapter reusable outside Vercel;
- make later support for additional serverless runtimes possible;
- preserve current query semantics and API compatibility.

---

## 3. Non-goals

The first implementation MUST NOT attempt to:

- run the current long-lived `serve` command inside Vercel;
- persist SQLite files in Vercel `/tmp`;
- make `/tmp` SQLite the authoritative cache or metadata store;
- support local TLS certificates inside the Function;
- run periodic baseline synchronization in a background goroutine;
- import the baseline SQLite database into PostgreSQL;
- provide a distributed implementation of the five-minute pairing session;
- provide distributed/global rate limiting across all Vercel Function instances;
- redesign the public Mast API;
- replace Gin;
- rewrite providers in another language;
- change Docker or binary users to PostgreSQL by default.

Baseline support and a distributed pairing flow may be designed separately after the first Vercel deployment is stable.

---

## 4. Existing Architecture Relevant to This Design

The current application already contains an important abstraction that makes this change practical.

The query service depends on `query/port.QueryRepository`, which currently exposes only:

```go
type QueryRepository interface {
    ListByPhone(ctx context.Context, phone string) ([]*domain.Record, error)
    ListByPhoneAndSources(ctx context.Context, phone string, sources []string) ([]*domain.Record, error)
    SaveBatch(ctx context.Context, records []*domain.Record) error
}
```

The current SQLite runtime repository implements that interface and additionally stores runtime metadata such as the instance ID and baseline pointers.

The current router is also already separated from the TCP listener. `internal/app.NewRouter(...)` returns a Gin engine, while `App.Start(...)` owns `net.Listen` and `http.Server.Serve`.

This means the Vercel implementation should adapt storage and application construction, not rewrite the API stack.

---

## 5. Proposed Runtime Modes

The repository will continue to support the existing persistent self-host runtime and add a serverless runtime.

### 5.1 Persistent self-host mode

Used by Docker and binary deployments.

Characteristics:

- YAML configuration;
- token file;
- SQLite runtime cache;
- optional baseline database;
- local TLS or reverse-proxy mode;
- rotating file logs;
- long-lived process;
- asynchronous cache writer;
- temporary in-memory pairing page.

Existing behavior should remain unchanged unless a small internal refactor is necessary.

### 5.2 Vercel serverless mode

Characteristics:

- environment configuration;
- token from `MAST_TOKEN`;
- PostgreSQL runtime repository;
- baseline disabled;
- TLS handled by Vercel;
- stdout/stderr logging;
- request-driven HTTP handler;
- synchronous cache persistence;
- pairing page disabled.

This is not intended to be a second product implementation. It is another composition root for the same core application.

---

## 6. Proposed Source Layout

Exact names may be adjusted during implementation if a cleaner package boundary is found, but the intended structure is:

```text
api/
  index.go                         # Vercel Function entry point

internal/
  app/
    app.go                         # refactor for injected dependencies
    router.go                      # existing router
  config/
    ...                            # existing YAML config
    vercel.go                      # env -> runtime config builder
  storage/
    runtime/                       # existing SQLite implementation
    postgres/
      repository.go                # QueryRepository + metadata implementation
      migrations.go                # idempotent PostgreSQL migrations
      repository_test.go

query/
  service/
    service.go                     # add serverless-safe save behavior

vercel.json                        # only if required/useful
README.md
DEPLOY.md
```

Do not create duplicated Vercel-specific copies of provider, query, HTTP or domain packages.

---

## 7. PostgreSQL Repository

### 7.1 Driver

Use `pgx/v5` through `database/sql` unless implementation constraints reveal a compelling reason to use the native pgx API.

The connection string MUST come from:

```text
DATABASE_URL
```

The code must not assume Neon-specific connection string formats. Neon is the recommended deployment path, not a hard dependency.

### 7.2 Interfaces

The PostgreSQL repository MUST implement:

```go
query/port.QueryRepository
```

It SHOULD also implement the metadata operations currently required by application/runtime initialization:

```go
GetMetadata(context.Context, string) (string, error)
SetMetadata(context.Context, string, string) error
DeleteMetadata(context.Context, string) error
EnsureInstanceID(context.Context) (string, error)
```

If these methods are currently coupled directly to the SQLite concrete type, refactor toward a small interface owned by the composition layer or a suitable package.

Do not make the query service aware of PostgreSQL.

### 7.3 Schema

The PostgreSQL schema should preserve the semantics of the existing SQLite schema.

Recommended initial schema:

```sql
CREATE TABLE IF NOT EXISTS query_records (
    phone_number TEXT NOT NULL,
    source TEXT NOT NULL,
    tag TEXT NOT NULL,
    confidence INTEGER NOT NULL,
    hit_count INTEGER NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (phone_number, source)
);

CREATE INDEX IF NOT EXISTS idx_query_records_phone_number
    ON query_records (phone_number);

CREATE TABLE IF NOT EXISTS runtime_metadata (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
```

The implementation should use real PostgreSQL timestamps rather than storing RFC3339 strings unless preserving strings materially simplifies compatibility.

### 7.4 Upserts

`SaveBatch` MUST preserve the current cache behavior. A record for the same `(phone_number, source)` should be updated atomically.

Use PostgreSQL `INSERT ... ON CONFLICT (...) DO UPDATE`.

Prefer a transaction for a batch.

### 7.5 Migrations

Users should not need to run a separate migration command.

The Vercel application initialization path MUST perform idempotent migrations before serving queries.

For the initial implementation, a small embedded migration mechanism is preferred over introducing a large migration framework.

Concurrency must be considered because multiple cold Function instances may initialize at the same time. Migration SQL therefore must be idempotent and, where necessary, protected by a PostgreSQL advisory lock or another simple database-side lock.

The implementation must not depend on only one Vercel instance starting at a time.

### 7.6 Connection lifecycle

The package should create a `*sql.DB` once per warm Function instance and reuse it across requests.

Use conservative pool settings suitable for serverless workloads. Do not inherit the SQLite connection pool assumptions.

Initial suggested limits:

```text
MaxOpenConns: 2-4
MaxIdleConns: 1-2
```

Exact values should be validated against Neon/Vercel behavior during implementation.

The design must avoid opening a brand-new PostgreSQL connection pool for every HTTP request.

---

## 8. Application Dependency Injection

`internal/app.Build` currently creates the SQLite runtime repository internally. That prevents a Vercel composition root from supplying PostgreSQL without duplicating App construction logic.

Refactor `app.Options` so storage dependencies may be injected.

Conceptually:

```go
type RuntimeRepository interface {
    port.QueryRepository
    GetMetadata(context.Context, string) (string, error)
    SetMetadata(context.Context, string, string) error
    DeleteMetadata(context.Context, string) error
    EnsureInstanceID(context.Context) (string, error)
    Close() error
}

type Options struct {
    Config     *config.Config
    Token      []byte
    Runtime    RuntimeRepository
    Version    string
    Commit     string
    InstanceID string
    // existing optional dependencies...
}
```

This is illustrative, not a mandatory exact interface shape.

Important compatibility requirement:

- when `Runtime` is absent in the traditional composition root, the existing SQLite runtime behavior should still be constructed exactly as today;
- alternatively, move SQLite construction entirely into the CLI composition root and require `App.Build` to always receive dependencies. This is architecturally cleaner but is a larger refactor. Prefer the smaller safe refactor unless tests show the cleaner option is equally low-risk.

Do not introduce Vercel environment checks throughout `app.Build`.

---

## 9. Serverless-safe Query Cache Writes

The current query service writes cache entries through an asynchronous in-process writer.

That behavior is correct for a long-running process but is unsafe for Vercel because the platform may freeze or terminate execution after the response completes. Cache writes that are merely queued in a goroutine are not guaranteed to finish.

Add an explicit save mode to `service.Options`.

Example:

```go
type SaveMode int

const (
    SaveAsync SaveMode = iota
    SaveSync
)

type Options struct {
    ...
    SaveMode SaveMode
}
```

An equivalent boolean such as `SynchronousSave` is acceptable, but an enum is preferable if the code remains clear.

Behavior:

- Docker/binary: `SaveAsync` (current behavior);
- Vercel: `SaveSync`.

In synchronous mode, provider results must be persisted before returning the successful HTTP response.

A cache persistence failure SHOULD NOT necessarily convert a successful provider lookup into an API failure. Preserve current cache-as-optimization semantics where possible:

1. obtain provider result;
2. attempt synchronous cache save;
3. log cache save failure;
4. still return the provider result unless current semantics require otherwise.

Tests must explicitly cover this behavior.

Do not leave a background writer goroutine running in Vercel mode when it has no purpose.

---

## 10. Vercel Configuration

The Vercel mode should not require `config.yaml`.

Create a dedicated configuration builder that produces the existing runtime `config.Config` (or a smaller deployment-neutral configuration after refactor) from environment variables.

### 10.1 Required variables

#### `DATABASE_URL`

PostgreSQL connection string.

If missing, initialization must fail with a concise actionable error.

#### `MAST_TOKEN`

Bearer token shared with Pixel Telo.

If missing or empty, initialization must fail.

Do not automatically generate a token in Vercel mode because generated local state would not be reliably discoverable or durable.

The deployment documentation should recommend generating a high-entropy random token.

### 10.2 Optional variables

#### `MAST_PROVIDER_IDS`

Comma-separated provider IDs.

Example:

```text
sogou,360
```

Recommended default:

```text
sogou
```

Provider enablement must continue to be explicit and compatible with the project's existing provider-use expectations.

Additional tuning environment variables may be added only if they provide clear value. Avoid mirroring every YAML option into an environment variable in the first release.

### 10.3 Fixed/default serverless values

The first Vercel implementation should set reasonable internal defaults rather than making users configure them:

```text
baseline.enabled            = false
baseline.sync_on_start      = false
tls.mode                    = off
query.timeout               = 2s
query.max_concurrent        = 4
rate_limit.requests_per_sec = 1
rate_limit.burst            = 5
logging                     = stdout/stderr
cache save mode             = synchronous
```

The actual internal `server.listen` value should be irrelevant to the Vercel handler and must not cause validation failures if no listener is started.

If the existing config validation requires a listen address, either provide a harmless internal value or separate listener validation from handler configuration. Prefer the smaller change.

---

## 11. Public URL and TLS

The Go application MUST NOT terminate TLS in Vercel mode.

Vercel serves the Function behind HTTPS.

Therefore:

```text
tls.mode = off
```

means only that the Go process itself does not own certificates.

The externally visible service URL remains:

```text
https://<deployment>.vercel.app
```

The first implementation does not need a globally configured public URL because the pairing page is disabled.

If a public URL is needed by another API path during implementation, derive it from trusted Vercel request headers or Vercel system environment variables rather than requiring users to enter it manually.

Do not use `http://` in client-facing pairing/configuration instructions.

---

## 12. Vercel HTTP Entry Point

Add a Vercel Go Function entry point under `api/`.

The entry point should:

1. initialize the application lazily or during package initialization;
2. build the PostgreSQL repository;
3. run migrations;
4. build the serverless configuration;
5. build the existing query/provider/HTTP stack;
6. obtain the existing Gin router;
7. pass each request to `router.ServeHTTP`.

Conceptual shape:

```go
package handler

var (
    once    sync.Once
    router  http.Handler
    initErr error
)

func Handler(w http.ResponseWriter, r *http.Request) {
    once.Do(func() {
        router, initErr = buildVercelRouter()
    })
    if initErr != nil {
        // log internal details; return a safe 500 response
        http.Error(w, "service initialization failed", http.StatusInternalServerError)
        return
    }
    router.ServeHTTP(w, r)
}
```

A retryable initialization strategy may be preferable to `sync.Once` if transient database failures during cold start would permanently poison a warm instance. The implementation should deliberately decide this rather than blindly using the sketch above.

Do not call:

```go
net.Listen
http.Server.Serve
App.Start
```

from the Vercel entry point.

---

## 13. Application Lifetime and Cleanup

Traditional mode owns a deterministic process shutdown path and calls `App.Close`.

Vercel does not provide the same lifecycle guarantees.

The serverless composition root should therefore avoid requiring per-request `Close` calls.

It is acceptable for a warm Function instance to retain:

- the PostgreSQL connection pool;
- provider dispatcher state;
- in-memory rate limiter state;
- query service state;
- Gin router.

These are instance-local optimizations/state only.

Do not use request-scoped `defer app.Close(...)` because that would rebuild all dependencies and reconnect to PostgreSQL on each request.

Serverless mode must not start lifecycle-critical background goroutines.

---

## 14. Baseline Behavior

Baseline support is explicitly disabled in the first Vercel release.

Reasons:

- the current baseline flow downloads archives;
- extracts and validates a SQLite database;
- switches active local database files;
- records active/pending file paths in runtime metadata;
- runs periodic background synchronization.

These assumptions do not map cleanly to Vercel Functions.

Required behavior in Vercel mode:

```text
baseline.enabled = false
baseline.sync_on_start = false
```

The Vercel documentation must state that queries use enabled live providers plus PostgreSQL runtime cache only.

Do not partially run baseline synchronization using `/tmp` in the first release.

A future design may migrate baseline data to a relational representation or load immutable snapshots through another mechanism.

---

## 15. Pairing Behavior

The existing pairing page is an in-memory five-minute session stored on a single Handler instance.

That is unsafe across horizontally scaled Vercel Functions because the request that opens `/p/<code>` may arrive at a different instance from the request/initialization that created the code.

Therefore the first Vercel release must not advertise or rely on the temporary pairing page.

Deployment instructions should instead tell the user to configure Pixel Telo manually with:

```text
URL:   https://<project>.vercel.app
Token: <MAST_TOKEN>
```

The existing pairing feature remains unchanged for Docker/binary mode.

Possible future options include:

- storing pairing sessions in PostgreSQL with expiry;
- creating a deterministic protected setup page;
- adding a Vercel-specific setup endpoint.

These are out of scope for this implementation.

The Vercel application's capability metadata should not claim a pairing capability that is unavailable in serverless mode. If `spki_pairing` is currently advertised unconditionally, make capabilities configurable by the composition root.

---

## 16. Rate Limiting and Provider Concurrency

The current rate limiter and provider dispatcher operate in process memory.

On Vercel, each Function instance may have its own limiter and dispatcher, so configured limits are not globally enforced across horizontally scaled instances.

For the expected personal/household workload, this is acceptable for the first release.

Document the limitation.

Do not add Redis, distributed locks or PostgreSQL-based global rate limiting in the first implementation.

Keep conservative per-instance defaults.

If real usage later demonstrates provider-side throttling caused by horizontal scaling, design a distributed limiter separately.

---

## 17. Instance ID

The API currently exposes an instance ID.

In Vercel mode, instance identity must be stable across cold starts and deployments as long as the same PostgreSQL database is retained.

Store/retrieve it through `runtime_metadata` in PostgreSQL using the same semantic behavior as the SQLite `EnsureInstanceID` path.

Do not derive instance ID from Vercel Function instance identifiers or hostnames because those are ephemeral.

---

## 18. Logging

Vercel mode must log to stdout/stderr using `slog` so logs are visible through Vercel Logs.

Do not initialize the existing rotating-file logger in Vercel mode.

The HTTP access/recovery middleware should still receive a valid logger.

Existing Docker/binary logging behavior should remain unchanged.

Avoid logging:

- `MAST_TOKEN`;
- bearer authorization values;
- database passwords or complete `DATABASE_URL` values;
- full sensitive setup payloads.

---

## 19. Deploy Button and Database Provisioning

After the code path is implemented and tested, README should include a Vercel Deploy Button.

Preferred user experience:

1. click Deploy with Vercel;
2. fork/clone/import the repository through Vercel's deployment flow;
3. attach a PostgreSQL product, recommending Neon;
4. ensure the integration injects `DATABASE_URL`;
5. enter `MAST_TOKEN`;
6. optionally enter `MAST_PROVIDER_IDS`;
7. deploy.

The project must still support users who manually provide an existing PostgreSQL `DATABASE_URL`.

Do not make a marketplace-specific SDK part of application runtime code.

The documentation should describe both:

- recommended path: Vercel + Neon Marketplace;
- generic path: any externally reachable PostgreSQL instance.

---

## 20. Security Considerations

### Token

`MAST_TOKEN` is a secret and must be treated as such.

Deployment documentation should recommend a long random value, for example generated locally with an established cryptographic tool.

Do not provide a fixed example token that users might copy unchanged.

### Database

The database URL is also secret.

Use TLS as provided/recommended by the PostgreSQL provider. Do not disable certificate verification in application code.

### HTTPS

Only advertise the Vercel HTTPS URL to Pixel Telo.

### Pairing

Do not expose a public endpoint that returns `MAST_TOKEN` merely to reproduce the current local pairing UX.

### Error handling

Initialization failures returned to clients must not expose credentials, raw connection strings, SQL statements containing sensitive data, or internal stack traces.

---

## 21. Error Handling

Cold-start configuration/database failures should be distinguishable in logs.

Examples:

```text
DATABASE_URL is required
MAST_TOKEN is required
connect postgres: ...
migrate postgres: ...
build application: ...
```

Client response may remain a generic 500.

Query cache failures should follow cache-as-optimization semantics where possible. A live provider result should not be discarded only because PostgreSQL cache persistence failed.

Provider failures and API error mappings should preserve existing behavior.

---

## 22. Testing Plan

Implementation is incomplete until the following tests exist.

### 22.1 PostgreSQL repository tests

Cover:

- migrations on an empty database;
- repeated/idempotent migration execution;
- concurrent migration startup if practical;
- `SaveBatch` insert;
- `SaveBatch` upsert;
- `ListByPhone`;
- `ListByPhoneAndSources`;
- not-found behavior compatible with the current domain error;
- metadata get/set/delete;
- stable `EnsureInstanceID`.

Prefer an integration test using a real PostgreSQL instance in CI if repository infrastructure permits. If not, keep unit coverage and document the missing integration test rather than pretending SQLite validates PostgreSQL behavior.

### 22.2 Query service tests

Add coverage for synchronous save mode:

- result is saved before method completion;
- cache write failure does not incorrectly hide an otherwise valid provider result;
- async mode remains unchanged;
- no unnecessary async writer is required in serverless mode if implementation removes it.

### 22.3 Vercel configuration tests

Cover:

- missing `DATABASE_URL`;
- missing `MAST_TOKEN`;
- default provider list;
- comma-separated providers;
- baseline forced disabled;
- TLS local termination disabled;
- invalid provider configuration.

### 22.4 Handler construction tests

Build the Vercel router with injected/fake dependencies where possible and verify:

- `/api/health` works;
- authenticated info endpoint works;
- sources endpoint works;
- `/api/v2/query` reaches the normal query service;
- missing/invalid bearer token is rejected;
- pairing is unavailable/not advertised as supported.

### 22.5 Regression tests

All existing Docker/binary tests must continue to pass.

No existing configuration file format should be broken solely for Vercel support.

---

## 23. Manual Validation Plan

Before merging implementation into `main`, manually validate at least:

### Local PostgreSQL validation

Run the Vercel composition root or an equivalent local handler against a disposable PostgreSQL database.

Verify:

- first startup creates tables;
- second startup does not fail migrations;
- query cache survives process restart;
- instance ID survives process restart;
- provider query writes cache;
- cached query is subsequently returned according to current behavior.

### Vercel validation

Deploy the implementation branch to Vercel using a real PostgreSQL database.

Verify:

- deployment succeeds from a clean project;
- no write access to repository filesystem is required;
- health endpoint works;
- authenticated info endpoint works;
- provider list works;
- Pixel Telo can query through the Vercel URL;
- PostgreSQL receives cache rows;
- a later cold start continues to use the same instance ID and cached records;
- logs contain useful errors but no secrets.

### Scale/cold-start sanity check

Trigger multiple requests and redeploy/restart where possible.

Confirm that correctness does not depend on one specific Function instance remaining alive.

---

## 24. CI Considerations

If PostgreSQL integration tests are added, prefer an ephemeral PostgreSQL service in GitHub Actions rather than depending on a developer-owned hosted database.

No production or shared database credentials should be required by pull-request CI.

Vercel deployment credentials are not required for ordinary unit/integration tests.

A live Vercel deployment remains a manual pre-merge validation step unless the project later chooses to add preview deployment automation.

---

## 25. Documentation Changes Required by Implementation PR

The implementation PR should update README/DEPLOY documentation with a clear deployment comparison.

Suggested presentation:

| Mode | Storage | Baseline | Pairing page | TLS | Best for |
| --- | --- | --- | --- | --- | --- |
| Docker | SQLite/local files | Yes | Yes | local or reverse proxy | NAS/VPS/home server |
| Binary | SQLite/local files | Yes | Yes | local or reverse proxy | desktop/server |
| Vercel | PostgreSQL | No (initial release) | No (initial release) | Vercel-managed HTTPS | easiest public cloud deployment |

The Vercel documentation must include:

- Deploy Button;
- database setup path;
- required environment variables;
- provider configuration;
- manual Pixel Telo URL/token configuration;
- unsupported features;
- troubleshooting for database connection/migration failures.

Do not imply feature parity where it does not exist.

---

## 26. Suggested Implementation Sequence for Codex

Implement in small reviewable commits even if they remain on one feature branch.

### Phase 1: Storage abstraction cleanup

- define/inject the runtime repository dependency needed by `app.Build`;
- keep SQLite behavior unchanged;
- add/refine tests around the composition change.

### Phase 2: PostgreSQL adapter

- add pgx dependency;
- implement repository;
- implement migrations;
- implement metadata and stable instance ID;
- add repository tests.

### Phase 3: Serverless-safe query writes

- add synchronous save mode;
- preserve async mode for traditional deployments;
- add tests.

### Phase 4: Vercel configuration and composition root

- environment configuration;
- stdout logger;
- PostgreSQL initialization;
- capability selection;
- baseline disabled;
- no pairing session;
- construct router without starting TCP listener.

### Phase 5: Vercel Function entry point

- add `api/index.go`;
- ensure warm-instance dependency reuse;
- ensure safe handling of initialization errors;
- add handler tests.

### Phase 6: Deployment metadata and documentation

- add `vercel.json` only if needed;
- add Deploy Button;
- document Neon/generic PostgreSQL setup;
- document limitations.

### Phase 7: Manual validation

- deploy feature branch to Vercel;
- verify with real Pixel Telo;
- record any Vercel-specific constraints discovered during testing;
- update this design document if implementation must intentionally diverge.

Do not merge the feature branch into `main` until manual Vercel validation is complete.

---

## 27. Acceptance Criteria

The future implementation PR is ready to merge only when all of the following are true:

- [ ] Existing Docker deployment still works without PostgreSQL.
- [ ] Existing binary deployment still works without PostgreSQL.
- [ ] Vercel Function builds from the repository.
- [ ] Vercel mode requires only `DATABASE_URL` and `MAST_TOKEN` for the minimal supported configuration.
- [ ] PostgreSQL schema initializes automatically.
- [ ] Runtime cache survives Function cold starts.
- [ ] Instance ID survives Function cold starts.
- [ ] Query API behavior remains compatible with Pixel Telo.
- [ ] Provider results are synchronously persisted in serverless mode.
- [ ] Cache write failure does not incorrectly discard a valid provider result.
- [ ] Baseline is disabled and documented as unsupported in Vercel mode.
- [ ] Pairing page is disabled/not advertised and documented as unsupported in Vercel mode.
- [ ] Vercel logs do not expose secrets.
- [ ] Existing test suite passes.
- [ ] New PostgreSQL/serverless tests pass.
- [ ] A real Vercel deployment has been manually tested against a real PostgreSQL database.
- [ ] Pixel Telo has successfully queried the deployed Vercel endpoint.
- [ ] README contains a clear Vercel deployment path and Deploy Button.

---

## 28. Deferred Follow-ups

These should become separate issues/designs after the initial implementation:

1. **Serverless baseline support**
   - PostgreSQL baseline representation, object-storage snapshot, or another immutable lookup format.

2. **Distributed pairing page**
   - PostgreSQL-backed expiring pairing sessions or another serverless-safe setup flow.

3. **Distributed provider rate limiting**
   - only if real Vercel usage demonstrates a need.

4. **Additional serverless providers**
   - reuse PostgreSQL/runtime composition abstractions for other platforms where practical.

5. **Database cleanup/retention**
   - optional expiration of stale cache rows if database growth becomes meaningful.

---

## 29. Final Architectural Principle

The implementation should preserve this dependency direction:

```text
Vercel adapter ─┐
                ├─> application composition -> existing HTTP/query/provider core
PostgreSQL ─────┘

CLI/Docker adapter ─> same application composition -> existing HTTP/query/provider core
SQLite ──────────────┘
```

Vercel and PostgreSQL are adapters, not new core concepts.

If implementation starts requiring `if vercel { ... }` checks throughout provider/query/domain packages, stop and reconsider the boundary. The desired result is a small deployment adapter around the existing application, with PostgreSQL introduced behind interfaces that already largely exist.
