# LightEx — Project Memory / Developer Handoff

> **Purpose:** This file is the persistent project handoff document for LightEx. A developer, AI coding assistant, or future version of the project should be able to read this file and understand what LightEx is, how the backend is structured, what has been completed, what was lost during the OS migration, what has been restored, exactly where development currently stands, and what remains to be built.
>
> **Last updated:** 2026-10-30
> **Current environment:** Linux, Docker, PostgreSQL 16, Go
> **Current backend path used in the latest session:** `/mnt/grace/LightEx/light-ex-backend`

---

## 1. Project Identity

**Project:** LightEx

**Organization:** Light Group

**Primary purpose:** LightEx is intended to become a Ghana/West Africa-focused custodial financial platform for crypto and local-money conversion, with a lower-friction user experience than traditional exchange P2P flows. The broader product direction includes wallet services, deposits/withdrawals, conversion/exchange, savings/loans, FX, and later spending controls.

### Initial product direction

- Custodial crypto ↔ local-money platform.
- Ghana/West Africa as the initial market context.
- KYC before users become eligible for deposit/withdrawal functionality.
- Small conversions may use LightEx's own reserves; larger conversions may use liquidity providers.
- Initial transaction/usage limits are intended to remain below approximately USD 10,000 during the early stage.
- UX should remain simple and low-friction rather than copying a complex exchange interface.

> The above describes the intended product direction discussed during planning. It is not a statement that all of these features already exist in the codebase.

---

# 2. Engineering Direction

## Backend technology

- **Language:** Go
- **HTTP framework:** Gin
- **Database:** PostgreSQL
- **Database driver:** pgx via `database/sql`
- **Migrations:** `golang-migrate`
- **Identifiers:** UUID
- **Money:** `shopspring/decimal` rather than `float64`
- **Authentication:** JWT access + refresh tokens
- **Containerization:** Docker / Docker Compose

## Architecture

LightEx is being built as a **modular monolith**, not microservices.

The established architectural direction is:

```text
HTTP / Handler
      ↓
Application / Use Case
      ↓
Service / Business Logic
      ↓
Repository Interface
      ↓
PostgreSQL Repository
      ↓
Database
```

The project should continue using the existing Clean Architecture direction, module boundaries, dependency injection, repository interfaces, service layer, application layer where needed, middleware, and PostgreSQL transaction boundaries.

### Important architectural rule

**Do not redesign the backend from scratch.** Extend the existing architecture and preserve established conventions.

Financial operations must eventually be coordinated atomically. A deposit, withdrawal, or transfer should not be implemented as an arbitrary direct balance update from an HTTP handler.

Conceptually:

```text
Financial Use Case
      ↓
PostgreSQL transaction
      ├── Payment
      ├── Transaction
      ├── Ledger entries
      └── Wallet balance
      ↓
    COMMIT
```

If a critical operation fails, the database transaction should be able to roll back the whole operation.

---

# 3. Current Project Status — Executive Summary

## ✅ Completed / Restored

### Platform foundation

- Go backend repository initialized.
- Configuration/environment loading exists.
- PostgreSQL connection abstraction exists.
- PostgreSQL connection pool configuration exists.
- Migration runner exists.
- Logger exists.
- HTTP server exists.
- Middleware foundation exists.
- JWT security package exists.

### Authentication

- User domain exists.
- User registration exists.
- Login exists.
- Password hashing exists.
- JWT access token generation exists.
- Refresh token flow exists.
- JWT middleware exists.
- Auth HTTP handlers/routes exist.

### Wallet module (Rebuilt)

- Wallet domain exists (`internal/modules/wallet/domain`).
- Balance invariants, credit, debit, lock, unlock, debit locked, statuses implemented using `shopspring/decimal`.
- Wallet repository interface exists (`internal/modules/wallet/repository`).
- PostgreSQL wallet repository exists (`internal/modules/wallet/repository/postgres`) with pessimistic locking (`FOR UPDATE`) and `DBTX` support for atomic transactions.
- Wallet service exists (`internal/modules/wallet/service`).
- Wallet HTTP handlers/routes exist (`GET /api/v1/wallets/me`, `POST /api/v1/wallets`, `GET /api/v1/wallets/:id`) protected by JWT authentication middleware.
- Full unit and PostgreSQL integration test suite passing.

### Database recovery

The original Windows PostgreSQL data was lost during an OS migration. The original Wallet/Payment Go source was not recoverable from Git or the Grace drive either.

The database schema was therefore reconstructed from the known project architecture and verified against PostgreSQL.

Current migration chain:

```text
000001_create_users_table
000002_create_wallets_table
000003_create_transactions_table
000004_create_ledger_entries_table
000005_create_payments_table
```

Current PostgreSQL migration state at the time of this document:

```text
version = 5
dirty   = false
```

Verified database tables:

```text
schema_migrations
users
wallets
transactions
ledger_entries
payments
```

### Docker PostgreSQL

A dedicated LightEx PostgreSQL container has been created:

```text
container: lightex-db
image: postgres:16
host port: 5432
```

The database environment was configured around:

```text
DB_HOST=localhost
DB_PORT=5432
DB_NAME=lightex
DB_USER=lightex_app
```

The local development password was configured consistently in `.env` and Docker Compose. **Do not record or commit actual credentials/secrets in this document.**

The Docker volume is persistent:

```text
lightex_postgres_data
```

An unrelated `learnmate-db` PostgreSQL container previously occupied host port 5432; it was safely stopped because that project was not currently in use.

---

# 4. Important Recovery History

## What happened

The project was originally developed on Windows with a PostgreSQL instance and additional Wallet/Payment Go modules. The OS was later switched to Linux.

The Linux checkout was connected to the GitHub repository, but Git contained only the earlier Authentication checkpoint.

Git history showed only:

```text
665ba53 Complete authentication module with JWT middleware and refresh tokens
6682761 chore: initialize LightEx backend repository
```

There were no other branches or tags containing the later Wallet/Payment work.

Searches of the Grace drive did not recover the lost Wallet/Payment source.

## Recovery decision

Instead of continuing on a broken/incomplete database state, the project was restored deliberately:

1. Docker PostgreSQL was configured.
2. A fresh `lightex` database was initialized.
3. Migration `000001` was verified/fixed.
4. Migrations `000002`–`000005` were reconstructed and applied.
5. The migration filename typo for Payment DOWN migration was corrected.
6. The restored database foundation was committed to Git.

Recovery checkpoint commit:

```text
e4aba94 chore: restore financial database migrations
```

At the time of the checkpoint, `git status` was clean.

---

# 5. Current Repository Structure

The current Linux repository contains the platform and Auth code, plus the restored migrations and Docker setup.

Known current structure:

```text
light-ex-backend/
├── api.http
├── cmd/
│   └── api/
│       ├── main.go
│       └── routes.go
├── docker-compose.yml
├── Dockerfile
├── docs/
│   └── engineering/
│       ├── backing.md
│       └── CHANGELOG.md
├── .env
├── .env.example
├── .gitignore
├── go.mod
├── go.sum
├── internal/
│   ├── modules/
│   │   └── auth/
│   │       ├── module.go
│   │       ├── domain/
│   │       │   └── user.go
│   │       ├── handler/
│   │       ├── repository/
│   │       │   ├── repository.go
│   │       │   └── postgres/
│   │       │       └── repository.go
│   │       └── service/
│   │           ├── auth.go
│   │           ├── errors.go
│   │           ├── login.go
│   │           ├── password.go
│   │           └── refresh.go
│   │   └── wallet/
│   │       ├── module.go
│   │       ├── domain/
│   │       │   ├── wallet.go
│   │       │   └── wallet_test.go
│   │       ├── handler/
│   │       │   ├── http.go
│   │       │   ├── http_test.go
│   │       │   ├── request.go
│   │       │   └── response.go
│   │       ├── repository/
│   │       │   ├── repository.go
│   │       │   └── postgres/
│   │       │       ├── repository.go
│   │       │       └── repository_test.go
│   │       └── service/
│   │           ├── errors.go
│   │           ├── wallet.go
│   │           └── wallet_test.go
│   └── platform/
│       ├── bootstrap/
│       ├── config/
│       ├── database/
│       ├── logger/
│       ├── middleware/
│       └── server/
├── migrations/
│   ├── 000001_create_users_table.down.sql
│   ├── 000001_create_users_table.up.sql
│   ├── 000002_create_wallets_table.down.sql
│   ├── 000002_create_wallets_table.up.sql
│   ├── 000003_create_transactions_table.down.sql
│   ├── 000003_create_transactions_table.up.sql
│   ├── 000004_create_ledger_entries_table.down.sql
│   ├── 000004_create_ledger_entries_table.up.sql
│   ├── 000005_create_payments_table.down.sql
│   └── 000005_create_payments_table.up.sql
├── README.md
├── Makefile
└── test.http
```

## Current missing application modules

These are **not currently present in the Linux checkout** and must be rebuilt:

```text
internal/modules/transaction/
internal/modules/ledger/
internal/modules/payment/
```

Additional financial modules/use cases may be introduced later as the architecture requires, but they should be added deliberately rather than assumed to exist.

---

# 6. Existing Platform Conventions

## Database abstraction

`internal/platform/database/postgres.go` wraps `*sql.DB` and exposes:

- `New(...)`
- `SQL()`
- `Health(...)`
- `Close()`

It uses pgx through `database/sql`.

## Migration behavior

`internal/platform/database/migrate.go` runs the files in the `migrations/` directory through `golang-migrate`.

The application startup sequence includes:

```text
Load config
   ↓
Create logger
   ↓
Connect PostgreSQL
   ↓
Run migrations
   ↓
Construct application
   ↓
Start HTTP server
```

## Auth module pattern

`internal/modules/auth/module.go` establishes the module construction convention:

```text
PostgreSQL repository
      ↓
Password service / JWT service
      ↓
Authentication service
      ↓
HTTP handler
      ↓
Route registration
```

The module receives configuration and `*sql.DB` from the outside and wires its dependencies internally.

## Repository pattern

Auth's repository package contains an interface only. PostgreSQL implementation lives separately under `repository/postgres`.

The PostgreSQL repository uses a small `DBTX` abstraction implemented by both:

```text
*sql.DB
*sql.Tx
```

This pattern should be preserved and reused for financial modules.

---

# 7. Database Schema — Current Source of Truth

## 7.1 users

Current migration fields:

```text
id               UUID PRIMARY KEY
email            VARCHAR(254) NOT NULL UNIQUE
password_hash    TEXT NOT NULL
email_verified   BOOLEAN NOT NULL DEFAULT FALSE
status           VARCHAR(20) NOT NULL
created_at       TIMESTAMPTZ NOT NULL
updated_at       TIMESTAMPTZ NOT NULL
```

## 7.2 wallets

```text
id                  UUID PRIMARY KEY
user_id             UUID NOT NULL UNIQUE → users(id)
available_balance   NUMERIC(20,8) NOT NULL DEFAULT 0
locked_balance      NUMERIC(20,8) NOT NULL DEFAULT 0
currency            VARCHAR(10) NOT NULL DEFAULT 'USD'
status              VARCHAR(20) NOT NULL DEFAULT 'ACTIVE'
created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
```

Current model: one wallet per user because `user_id` is UNIQUE.

## 7.3 transactions

```text
id             UUID PRIMARY KEY
wallet_id      UUID NOT NULL → wallets(id)
type           VARCHAR(30) NOT NULL
amount         NUMERIC(20,8) NOT NULL
currency       VARCHAR(10) NOT NULL
reference      VARCHAR(120) UNIQUE NOT NULL
status         VARCHAR(30) NOT NULL
description    TEXT
metadata       JSONB
created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
```

Indexes exist for wallet, reference, and status.

## 7.4 ledger_entries

```text
id               UUID PRIMARY KEY
transaction_id   UUID NOT NULL → transactions(id)
wallet_id        UUID NOT NULL → wallets(id)
entry_type       VARCHAR(10) NOT NULL
amount           NUMERIC(20,8) NOT NULL
currency         VARCHAR(10) NOT NULL
created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
```

Indexes exist for transaction and wallet.

## 7.5 payments

```text
id                    UUID PRIMARY KEY
user_id               UUID NOT NULL → users(id)
wallet_id             UUID NOT NULL → wallets(id)
direction             VARCHAR(20) NOT NULL
method                VARCHAR(30) NOT NULL
amount                NUMERIC(20,8) NOT NULL
currency              VARCHAR(10) NOT NULL
status                VARCHAR(30) NOT NULL
reference             VARCHAR(120) UNIQUE NOT NULL
provider_reference    VARCHAR(120)
description           TEXT
metadata              JSONB
created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
```

Indexes exist for user, wallet, status, and provider reference.

---

# 8. Authentication Domain Details

The current Auth user domain includes:

### User statuses

```text
PENDING
ACTIVE
SUSPENDED
LOCKED
```

### User operations

- `NewUser(...)`
- `VerifyEmail()`
- `Suspend()`
- `Lock()`
- `Activate()`

The Auth domain intentionally contains authentication-related identity data only. Wallets, KYC, trading limits, balances, and profile data belong to other modules.

---

# 9. Payment Engine — Original Planned Direction

The Payment Engine is intended to be the central coordinator for deposits and withdrawals rather than a replacement for Wallet or Ledger.

## Original planned Phase 4

```text
4.1 Deposit orchestration
4.2 Withdrawal orchestration
4.3 Treasury integration
4.4 Internal settlement
4.5 Ledger posting
4.6 Payment workflow
4.7 Idempotency
4.8 Transaction state machine
```

## Original planned Phase 5

```text
External payment providers
Mobile Money
Bank transfer
Crypto deposit
Crypto withdrawal
Liquidity provider
Webhooks
Callbacks
Retry mechanism
```

The exact internal decomposition may evolve slightly while preserving the architectural principles above, because the Wallet/Payment source from the Windows environment was lost and is being rebuilt from the recovered architecture and schema.

---

# 10. Financial Design Principles

These rules should guide all future implementation.

## Rule 1 — Never use floating-point for money

Use `decimal.Decimal` for monetary arithmetic.

## Rule 2 — Financial writes are transactional

A balance change should be coordinated with its transaction/ledger/payment records inside a database transaction where the operation requires atomicity.

## Rule 3 — Keep responsibilities separated

```text
Wallet      → owns wallet state/balances
Transaction → records a financial operation
Ledger      → records accounting entries
Payment     → represents external/internal payment lifecycle
Provider    → handles external payment network integration
```

## Rule 4 — API handlers do not perform financial accounting directly

Handlers should validate/request/response concerns and invoke application/service logic.

## Rule 5 — Preserve repository abstraction

Business logic should depend on repository interfaces, not PostgreSQL details.

## Rule 6 — Payment providers belong later

Mobile Money, banks, crypto networks, webhooks, and retries belong in the external-provider phase after the core financial engine is correct.

---

# 11. Current Development Position

## 🔄 CURRENT MILESTONE

**Phase 5 — External Payment Infrastructure.**

Phase 4 (Core Financial Engine) was completed and verified:
- Transaction domain, repository, and service with atomic boundaries.
- Ledger entry posting and invariant checks; double-entry accounting remains a future design decision.
- Payment domain, repository, and service lifecycle.
- Deposit orchestration, withdrawal orchestration, and atomic internal settlement.
- Idempotency and transaction state machines.

Phase 5 implementation status:
- Generic `provider.Provider` abstraction (`Collect`, `Disburse`, request validation).
- MTN Mobile Money client and webhook parser exist; live sandbox verification, authentication of callbacks and route/settlement wiring remain incomplete.
- Bank and crypto providers are in-memory simulations; neither connects to a bank or blockchain or moves funds.
- Network retry mechanism and a reconciliation worker exist; their presence does not establish an operational external payment flow.

Remaining in Phase 5:
- Real liquidity provider integration (liquidity sourcing and rate feeds). A development-only simulator now exists; its invented rates and orders are not real liquidity.
- External provider webhook processing: PostgreSQL-backed MTN deduplication, read-only callback verification, verified settlement (`mtn.SettlementProcessor` + `payment/service.SettleCallback`), and asynchronous deposit initiation (`payment/service.Collector.InitiateDeposit`: record PENDING in its own transaction after the same wallet checks deposits use, call the provider with no database transaction open, then confirm PROCESSING with the provider reference, marking FAILED if the provider call fails) are implemented. The verification-first settlement route is mounted at `POST /api/v1/payments/webhooks/mtn` via `cmd/api/routes.go` — but only when MTN credentials are configured (`MTN_COLLECTION_*` or `MTN_DISBURSEMENT_*`), so it stays absent without a real provider. End-to-end PostgreSQL test: initiate with the MTN client → provider callback → wallet credited. Outstanding: no HTTP endpoints or production callers for `InitiateDeposit`/`InitiateWithdrawal` yet, explicit callback sender authentication (authenticity rests entirely on the authenticated provider status lookup), and out-of-order status policy beyond the verifier's PROCESSING gate.

Live sandbox compatibility is now verified (2026-10-30, `TestMTNSandboxIntegration` + `TestMTNSandboxCollectionContract` in `payment/provider/mtn/integration_test.go`, run from the package directory which loads `../../../../../.env.mtn`): OAuth2 tokens acquired for both collection and disbursement; a real sandbox `requesttopay` collection was accepted and read back through `LookupTransaction` with all fields strict verification depends on present (externalId, amount, currency, financialTransactionId, status) — the sandbox auto-transitioned the record to SUCCESSFUL. Two findings: the sandbox target environment rejects `GHS` with `INVALID_CURRENCY`, so only sandbox-supported currencies (e.g. EUR) can be exercised there, and real GHS compatibility depends on the configured production target environment; the sandbox completes collections on its own, which is useful for rehearsing the full settlement path. Not yet exercised against the live sandbox: disbursement transfers and real callback delivery (`MTN_CALLBACK_URL` is still unset in `.env.mtn`).

---

# 12. Full Roadmap

## ✅ Phase 0 — Repository/Foundation

- [x] Initialize Go backend repository
- [x] Establish project structure
- [x] Configuration/environment handling
- [x] Logger
- [x] HTTP server
- [x] PostgreSQL abstraction
- [x] Migration framework
- [x] Middleware foundation
- [x] Docker PostgreSQL development environment

## ✅ Phase 1 — Authentication

- [x] User domain
- [x] Password hashing
- [x] Registration
- [x] Login
- [x] JWT access token
- [x] Refresh token
- [x] JWT middleware
- [x] Auth routes

## ✅ Phase 2 — Database/Financial Schema Foundation

- [x] Users migration
- [x] Wallets migration
- [x] Transactions migration
- [x] Ledger entries migration
- [x] Payments migration
- [x] Migration version 5 verified clean
- [x] Recovery commit created

## ✅ Phase 3 — Wallet Rebuild

- [x] Wallet domain
- [x] Wallet repository interface
- [x] PostgreSQL wallet repository
- [x] Wallet service
- [x] Wallet HTTP handler
- [x] Wallet routes
- [x] Wallet integration tests

## 🔄 Phase 4 — Core Financial Engine

### 4.1 Wallet / financial primitives
- [x] Wallet domain completion
- [x] Balance rules
- [x] Locked-balance rules

### 4.2 Transaction engine
- [x] Transaction domain (`internal/modules/transaction/domain`, unit tests passing)
- [x] Transaction repository (interface + PostgreSQL impl with `WithTx`/`FindByIDForUpdate`, integration tests passing)
- [x] Transaction lifecycle (Processing / Completed / Failed / Cancelled transitions)
- [x] Reference handling (`GenerateReference`)
- [x] Transaction service (`internal/modules/transaction/service`: create, complete, fail, get)
- [x] Atomic transaction boundaries (`SQLStore.WithinTx` locks the transaction and wallet rows, commits together, and rolls back on failure; verified against PostgreSQL)

### 4.3 Ledger
- [x] Ledger domain (`internal/modules/ledger/domain`: CREDIT/DEBIT entries, validation)
- [x] Ledger repository (interface + PostgreSQL implementation using the caller's SQL transaction)
- [x] Ledger service (posting done by Transaction service `CompleteTransaction` inside the same SQL transaction; verified rollback against PostgreSQL)
- [x] Accounting invariants (entry type is derived from transaction type in `EntryTypeForTransaction`; unsupported types cannot post; amount must be > 0; entries are posted only inside the completion SQL transaction)
- [ ] Double-entry/entry-balance validation as appropriate to the final model (deferred: the current model is one entry per transaction, so this needs the double-entry decision first)

### 4.4 Payment Engine
- [x] Payment domain
- [x] Payment repository
- [x] Payment service
- [x] Application layer (`internal/modules/payment/module.go` wires repository and service from `*sql.DB`)
- [x] Payment state transitions (domain state machine plus service tests confirming persisted transitions and rejected writes)

### 4.5 Deposit orchestration
- [x] Request validation (payment domain validation and wallet owner/currency checks run before any write)
- [x] Payment creation (`payment/service/deposit.go`, inside one SQL transaction)
- [x] Transaction creation (DEPOSIT transaction created in the same SQL transaction)
- [x] Wallet credit (row locked with `FOR UPDATE`, credited in the same SQL transaction)
- [x] Ledger posting (CREDIT entry posted in the same SQL transaction)
- [x] Atomic completion/failure handling (any failure rolls back payment, transaction, balance and ledger; verified against PostgreSQL)

### 4.6 Withdrawal orchestration
- [x] Withdrawal validation (payment domain validation and wallet owner/currency checks run before any write)
- [x] Balance availability checks (`Wallet.LockFunds` rejects insufficient funds before anything is written)
- [x] Funds locking (funds are reserved under a row lock and settled with `DebitLocked`, in one SQL transaction)
- [x] Transaction creation (WITHDRAWAL transaction created in the same SQL transaction)
- [ ] Provider handoff later
- [x] Ledger posting (DEBIT entry posted in the same SQL transaction)
- [x] Completion/failure/cancellation handling (any failure rolls back everything; insufficient funds and mid-unit failure are verified against PostgreSQL. Cancellation is not part of this unit because no payment is left pending)

### 4.7 Treasury/internal settlement
- [ ] Treasury domain/service
- [x] Internal settlement
- [ ] Reserve/liquidity handling

### 4.8 Idempotency
- [x] Idempotency strategy
- [x] Duplicate request protection
- [ ] Provider callback duplication protection (needs a provider; Phase 5)

### 4.9 State machine / workflow
- [x] Transaction state machine (explicit transition methods in transaction/domain; covered by TestTransactionStateTransitions)
- [x] Payment state machine (allowedTransitions table in payment/domain; covered by TestPaymentStateMachine_*)
- [x] Legal state transitions (illegal transitions rejected and status left unchanged; covered by the same tests)
- [x] Failure/cancellation/retry rules (FAILED is terminal; a retry uses a new request and reference, and a reused FAILED reference is rejected; covered by TestDeposit_FailedPaymentReferenceCannotBeReused)

## 🔄 Phase 5 — External Payment Infrastructure

### Simulated liquidity (development only)

`internal/modules/liquidity` contains provider-neutral pair, quote and order models, a provider interface, and a simulated provider. In `APP_ENV=development` the authenticated `/api/v1/simulated-liquidity` routes expose quote creation, order creation, order lookup and reconciliation. Pairs GHS/USDT and GHS/USDC in either direction use **invented fixed rates** (0.1 and 10); they are not market prices or executable quotes. Quotes expire after one minute. Orders support successful, pending, failed, rejected and uncertain timeout scenarios in tests; the HTTP module uses the success scenario. A provider success is not verified settlement. Simulation never interacts with wallets, transactions, payments, or ledger entries.

The service and simulated provider store quotes, orders and idempotency keys in process memory. Idempotency protects concurrent requests only in one process, does not survive a restart, and has no durable audit history; logs are informational only. Simulated reconciliation checks the same provider reference but is not a background worker. Do not use this service for real providers or financial execution. No migration was added for this simulator.

**Future HoneyCoin adapter (candidate only):** Public [onramp](https://docs.honeycoin.app/docs/onramps) and [offramp](https://docs.honeycoin.app/docs/offramps) documentation describes asynchronous operations, references, status lookups, wallet addresses, chains and payout details. Before a real adapter, confirm Ghana market eligibility, API version and rate response schemas (the public guide and API reference differ), quote expiry and fees, authentication, idempotency, callbacks and reconciliation, sandbox behavior, and supported settlement flows. The current provider contract and order model will need review for provider quote IDs, amount rounding/fees, address and payout data, and durable restart-safe reconciliation; no claim of drop-in compatibility is made.

**Still unresolved:** asset custody and settlement design, business registration and onboarding, provider approval and credentials, KYC/AML and regulatory eligibility, durable storage/audit and provider idempotency, production security, and independent settlement verification. No real-money execution, provider onboarding, or custody choice is implemented or approved.


- [x] Provider abstraction (payment/provider: Provider interface with Collect/Disburse and request validation; covered by provider_test.go)
- [x] Mobile Money integration (MTN MoMo client implementing `provider.Provider` with `Collect` via `requesttopay`, `Disburse` via `transfer`, OAuth2 token caching, and transaction status polling; covered by client_test.go, token_test.go, integration_test.go)
- [x] Webhooks (MTN MoMo webhook callback parser and Gin handler; covered by webhook_test.go)
- [x] Callback processing (CallbackNotification mapping to generic provider status and processor hook)
- [ ] Real bank transfer integration (`payment/provider/bank` is an in-memory simulation only)
- [ ] Real crypto deposit and withdrawal (`payment/provider/crypto` stores simulated transactions and hashes; it does not interact with a blockchain)
- [x] Retry mechanism (payment/provider/retry: Exponential backoff with jitter and network transient error classification; covered by retry_test.go)
- [x] Reconciliation (payment/reconciliation: Background worker finding stale processing payments and atomically settling or failing with balance unlock; covered by worker_test.go)
- [ ] Real liquidity provider integration (a standalone simulated liquidity module is implemented for development; no real provider, funds, rates, or settlement)
- [x] Provider callback processing (mtn.SettlementProcessor: verify callback against the stored payment and an authenticated MTN status lookup strictly before any write, then inside one transaction deduplicate the event and apply settlement via service.SettleCallback, which re-locks the payment row, revalidates facts, and settles deposit credit / withdrawal DebitLocked / failure UnlockFunds with ledger posting, mirroring the reconciliation worker. Duplicate redeliveries of an already-processed payload hash short-circuit to a no-op; conflicting payloads and unknown references are rejected before any write; pending callbacks record the dedup event but move no balances. Covered by webhook_test.go settlement tests against PostgreSQL with a mock MTN server. Remaining: callback sender authentication is not separately implemented, asynchronous payment initiation does not exist yet, and live sandbox compatibility is unverified.)

## ⏳ Phase 6 — KYC / Risk / Limits

- [ ] KYC workflow
- [ ] User eligibility states
- [ ] Deposit/withdrawal limits
- [ ] Risk controls
- [ ] Compliance-related transaction restrictions

## ⏳ Phase 7 — Exchange / Conversion Engine

- [ ] Quotes
- [ ] Pricing/rates
- [ ] Fiat/crypto conversion
- [ ] Liquidity sourcing
- [ ] Conversion transaction flow
- [ ] Exchange history


## ⏳ Phase 8 — Flutter Application

- [ ] Authentication UI
- [ ] Wallet UI
- [ ] Deposit UI
- [ ] Withdrawal UI
- [ ] Convert UI
- [ ] History
- [ ] Settings
- [ ] KYC flow
- [ ] Error/loading states
- [ ] Production mobile integration

## ⏳ Phase 9 — Admin / Operations / Production Hardening

- [ ] Admin operations
- [ ] Payment monitoring
- [ ] Transaction reconciliation
- [ ] Audit logging
- [ ] Security hardening
- [ ] Observability
- [ ] Backups/recovery
- [ ] Deployment
- [ ] Production secrets/configuration
- [ ] Automated testing/CI/CD

## ⏳ Phase 10 — Additional Financial Features

- [ ] FX
- [ ] Savings
- [ ] Loans
- [ ] Spending controls
- [ ] Additional wallet/product capabilities


---

# 13. Development Rules for Future Work

These rules were explicitly established during the project.

1. **Do not redesign the architecture from scratch.**
2. **Explain why a change is being made before writing the code.**
3. **Maintain the existing Clean Architecture, repository, service, application, DI, middleware, and transaction patterns.**
4. **Do not skip roadmap steps.**
5. **Every project-progress response should report:**
   - ✅ Completed
   - 🔄 Current
   - ⏳ Remaining
6. **Wait for approval after each milestone when the workflow calls for it.**
7. Run formatting/build verification after implementation steps:

```bash
gofmt -w <changed-files>
go build ./...
```

8. Verify database changes through migrations rather than manually creating tables.
9. Keep secrets out of Git and out of persistent documentation.
10. Create a Git checkpoint after significant recovery/architecture milestones.

---

# 14. Useful Development Commands

## Start PostgreSQL

```bash
docker compose up -d
```

## Stop PostgreSQL

```bash
docker compose down
```

## Check container

```bash
docker ps
```

## Check PostgreSQL

```bash
docker exec lightex-db pg_isready -U lightex_app -d lightex
```

## Enter database

```bash
docker exec -it lightex-db psql -U lightex_app -d lightex
```

## Build backend

```bash
go build ./...
```

## Run backend

```bash
go run ./cmd/api
```

## Check migration state

Inside `psql`:

```sql
SELECT version, dirty FROM schema_migrations;
```

## Check tables

```sql
\dt
```

## Git

```bash
git status
git log --oneline --decorate -10
```

---

# 15. Important Known Issues / Follow-up

## Source recovery

The old Windows Wallet/Payment Go implementation is unavailable. It must be rebuilt from the recovered architecture and database schema.

## Current `cmd/api` routing

The routes currently registered in the API:

```text
GET  /api/v1/health
POST /api/v1/auth/register
POST /api/v1/auth/login
POST /api/v1/auth/refresh
POST /api/v1/wallets
GET  /api/v1/wallets/me
GET  /api/v1/wallets/:id
```

Wallet routes are active and secured with the JWT authentication middleware. Transaction and Payment routes will be added in Phase 4.

## Current bootstrap

The current Linux `Application` structure contains configuration, logger, and database. Wallet module is instantiated and wired in `cmd/api/routes.go`. Transaction and Payment modules will be added as they are rebuilt.

## Docker Compose

The LightEx Compose file has been created for local PostgreSQL. Keep the database credentials synchronized with `.env`, but never commit real secrets.

## Migration consistency

The migration naming is now standardized through `000005`; the previous typo `00005_create_payments_table.down.sql` was corrected.

---

# 16. Immediate Next Actions

### 🔄 Now (Remaining in Phase 5 — External Payment Infrastructure)

1. Develop and evaluate simulated liquidity. Real provider onboarding is deferred; HoneyCoin is a candidate only, not an approved integration. Verify its rate endpoint and response schema and sandbox market availability before implementing or using real financial rates.
2. Expose asynchronous initiation over authenticated HTTP (`Collector.InitiateDeposit` → MTN Collect; `Collector.InitiateWithdrawal` → reserved funds via LockFunds, an outbound PENDING transaction, then MTN Disburse; both confirm PROCESSING with the provider reference and release/mark-FAILED atomically if the provider call fails, never holding a database transaction open across the network call). The settlement pipeline itself is done: verify-before-writes, dedup + settlement in one transaction with re-lock, migration 000006 applied before use, and PostgreSQL/mock-server tests covering success, pending, failure, withdrawal unlock, mismatch, forged-later-success rejection, concurrent redelivery, and the full initiate→callback→settlement paths for both directions.
3. Decide remaining callback-sender authentication (raw-body signature or allowlist) and out-of-order status policy; exercise the disbursement side and real callback delivery against the live sandbox (set `MTN_CALLBACK_URL`), then run the full live initiate→callback→settlement path once initiation routes exist.

### ⏳ Next (Phase 6 — KYC / Risk / Limits)

1. KYC submission and verification workflow.
2. User tier eligibility states.
3. Daily/monthly deposit and withdrawal limits.
4. Risk controls and compliance restrictions.

---

# 17. Definition of “Current Project State”

At the moment this document was updated:

```text
Infrastructure                 ✅
Authentication                ✅
PostgreSQL                    ✅
Database schema                ✅
Migration chain 1–5           ✅
Git recovery checkpoint       ✅
Wallet Go module               ✅ (Phase 3 Complete)
Transaction Go module          ✅ (Phase 4 Complete)
Ledger Go module               ✅ (Phase 4 Complete)
Payment Go module               ✅ (Phase 4 Complete)
Deposit orchestration          ✅ (Phase 4 Complete)
Withdrawal orchestration       ✅ (Phase 4 Complete)
Internal settlement            ✅ (Phase 4 Complete)
Idempotency                    ✅ (Phase 4 Complete)
State machines                 ✅ (Phase 4 Complete)
Mobile Money provider (MTN)    ✅ (Phase 5)
Bank transfer provider         ⏳ (simulation only)
Crypto provider (Deposit/Wth)  ⏳ (simulation only)
Retry mechanism (Backoff)      ✅ (Phase 5)
Reconciliation worker          ✅ (Phase 5)

Liquidity provider             ⏳ (Remaining in Phase 5)
KYC / Limits / Risk            ⏳ (Phase 6)
Exchange engine                ⏳ (Phase 7)
Flutter client                 ⏳ (Phase 8)
Production hardening           ⏳ (Phase 9)
```

### Current milestone

> **Phase 5 — External Payment Infrastructure → Liquidity provider integration & webhook callback deduplication.**

This file should be updated whenever a milestone is completed, an architecture decision changes, a recovery event occurs, or the project's current state materially changes.
