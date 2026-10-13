<!--
Draft of this repo's project prompt, for agent-store. Each "#" heading is a
level-1 group with no text, each "##" heading a level-2 section. The tags for
each section are in the table at the end; they are section metadata, not text.
Once the collection exists and renders AGENTS.md and CLAUDE.md, delete this file.
-->

# About eval-store

## What it owns

Port 8323 (`EVAL_STORE_ADDR` in `eval-store.service`, loopback only). Judged runs of several models on a fixed set of real model calls: **suites** (a task and how to judge it), **cases** (frozen real calls), **runs** (which models answer, which judge, which cases, at what prices), and their **answers**, **verdicts** and **checks**. It replaced a Python bench in `/tmp` that set scheduler's model roles for email filing and demo mail. `CONTRACT.md` is the route table.

## Cases hold real mail

⚠️ An email-filing case is a copy of a real prompt: the operator's own mail and cards. The database lives at `~/.config/eval-store/eval-store.db`, directory 700 and file 600, and `Open` refuses a directory others can read. **Nothing derived from a case — a prompt, an answer, a verdict reason, a fixture copied from one — goes in this repo, its tests or a commit message**: the repo is public. Tests use synthetic cases only. Lists leave prompts and answers out; `GET /cases/{id}` and `include_bodies=true` are the only ways to read them.

## Where this prompt lives

These sections are stored in agent-store as a project prompt collection and rendered, with identical text, to `AGENTS.md` and `CLAUDE.md` at the root of this repo. Edit them on dash `/files`, or edit either rendered file and let the 15-minute scan carry the edit back.

# How it works

## A run, from start to report

A run answers, checks, then judges. Every candidate answers every case, except a candidate named `original_answer_model_id`, whose answers are the cases' originals. The suite's checker runs over the answers. Then every judge sees each case's answers under a **blind shuffle** seeded by the case id alone (`BlindLabelMapping`), so all judges of a case see one order and a resumed run rebuilds the same prompt. A verdict counts only when its ranking and its verdicts each name every label exactly once; the bench held verdicts to the same rule.

## Every model call is a bridge operation

Candidates and judges are called through llm-bridge-server's `POST /operations` (`llm.completion`), never a harness binary. Calls carry the service token and `X-Principal-Id` set to `EVAL_STORE_PRINCIPAL_ID`, so the bridge applies that principal's membership and grants and bills the run's `organization_id`. The operation's `model` is the model-store id. The idempotency key is `eval-store:<run>:<case>:<answer|judge>:<model>:<attempt>`: **a resumed run sends the same keys and is handed the first receipts**, so nothing is paid for twice. A failed call is retried only on `resume` with `retry_failed`, under the next attempt's key, because the bridge answers a reused key with the old failed receipt.

## The budget guard

Before each batch of `EVAL_STORE_BUDGET_CHECK_EVERY_CALLS` calls a run reads usage-store's `GET /api/usage/limits` and stops as `stopped_budget` when a subscription window is above the run's `budget_limits` (default codex weekly 60%, anthropic seven_day 40%), when a reading is stale or missing, or when the bridge refuses for budget. Resume picks up where it stopped. A run can overshoot by one batch.

## Prices are frozen on the run

A run reads every model's provider and list price from model-store's `GET /api/models` when it is created and never again. Reports price from that copy. The cache-adjusted cost (reads at 10% of input, Anthropic writes at 125%) is an assumption, since model-store lists no cache prices, and the report says so.

## Checkers and cost models belong to suites

A checker (`checker.go`) computes a case's features at import, checks an answer, and says what an answer chose per unit for agreement with the baseline. `email-filing` ports the bench's `mechanical.py`; `demo-mail` checks message keys and flags **a sender greeting themselves by name**, the defect the bench found in gpt-5.6-luna and which claude-haiku-5-5 shares. The cost projection is generic: which features exist, which coefficients are pinned, and how a timed task batches its work are fields of the suite's `cost_model`, never constants in `cost_projection.go`.

## The imported bench

Two suites and eight completed runs (`origin` `legacy-import`) came from the 2026-09-30..10-08 bench by `eval-store import-legacy`. Their reports reproduce the bench's `analyze.py`, `analyze5.py` and `analyze7.py` exactly in acceptance counts, mean ranks, tokens, latency and cost, with today's model-store prices, and the projection reproduces `cost_model.py`. The private copy of the bench is `~/.local/share/eval-store/import-scheduler-model-compare/`.

# Access and operations

## Who may call it

No authentication; the unit binds `127.0.0.1` because cases hold real mail. Nothing proxies it yet. Unit `eval-store.service` (user), binary `~/bin/eval-store serve`. The service token, `EVAL_STORE_PRINCIPAL_ID` and `EVAL_STORE_ORGANIZATION_ID` come from host-local drop-ins in `~/.config/systemd/user/eval-store.service.d/`, never from the tracked unit.

## Importing from operations needs a bridge route

`POST /suites/{id}/cases/import-operations` reads each operation's intent, where the prompts are, from `GET /operations/{id}/intent` on llm-bridge-server. **That route does not exist yet**; until it does the import answers 502 naming it. The legacy import does not need it.

# Working in this repo

## Build, test and deploy

Module `github.com/kayushkin/eval-store`, root package `evalstore`, one binary `cmd/eval-store` (`serve`, `import-legacy`, and a client verb per route against `EVAL_STORE_URL`). Every environment variable is declared once in `settings.go` with llm-bridge `servicesettings` (hence `replace ../llm-bridge` in `go.mod`); a set `EVAL_STORE_` variable nobody declared stops the service at boot, and `deploy.sh` checks the running service's environment first. The listen address has no default: the port lives in the unit. SQLite transactions begin IMMEDIATE because a run records answers from several goroutines. `go test ./...`; deploy only with `./deploy.sh` from `main`.

<!--
Tags per section:

| Section | Tags |
|---|---|
| What it owns | eval-store, model-evaluation |
| Cases hold real mail | eval-store, privacy |
| Where this prompt lives | eval-store, prompts |
| A run, from start to report | eval-store, model-evaluation, judging |
| Every model call is a bridge operation | eval-store, llm-bridge, operations, idempotency |
| The budget guard | eval-store, usage-store, budget |
| Prices are frozen on the run | eval-store, model-store, cost |
| Checkers and cost models belong to suites | eval-store, scheduler, email-classifier, demo-mail, cost |
| The imported bench | eval-store, scheduler, model-roles |
| Who may call it | eval-store, access, systemd |
| Importing from operations needs a bridge route | eval-store, llm-bridge, operations |
| Build, test and deploy | eval-store, build, deploy, servicesettings |
-->
