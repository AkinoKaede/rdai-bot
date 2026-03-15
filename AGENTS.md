# Repository Guidelines

## Project Structure & Module Organization
This repository is a single-binary Go service. Top-level `.go` files hold the main modules: [`main.go`](/Users/kaede/Developer/Utilities/rdai-bot/main.go) boots the app, [`http.go`](/Users/kaede/Developer/Utilities/rdai-bot/http.go) serves the web UI and API, [`telegram.go`](/Users/kaede/Developer/Utilities/rdai-bot/telegram.go) handles bot updates, [`store.go`](/Users/kaede/Developer/Utilities/rdai-bot/store.go) manages SQLite state, and [`axonhub.go`](/Users/kaede/Developer/Utilities/rdai-bot/axonhub.go) issues API keys. Templates live in [`templates/`](/Users/kaede/Developer/Utilities/rdai-bot/templates), GraphQL operations and generated client code live in [`graphql/`](/Users/kaede/Developer/Utilities/rdai-bot/graphql), and CI/release automation lives in [`.github/workflows/`](/Users/kaede/Developer/Utilities/rdai-bot/.github/workflows).

## Build, Test, and Development Commands
Run the app locally with `go run . --sqlite-path ./data/rdai-bot.db`. Format code with `gofmt -w *.go`. Run tests with `GOPATH=/tmp/go GOMODCACHE=/tmp/go/pkg/mod GOCACHE=/tmp/go-build-cache go test ./...`; the cache overrides match the documented local workflow and help in constrained environments. Build a container with `docker build -t rdai-bot .`, or start the full stack with `docker compose up -d` after filling in the required environment variables from [`README.md`](/Users/kaede/Developer/Utilities/rdai-bot/README.md).

## Coding Style & Naming Conventions
Follow standard Go formatting and imports; `gofmt` is the source of truth. Use tabs for indentation as emitted by `gofmt`. Keep package-level code in `package main`, use mixedCaps for exported names, camelCase for locals, and table-driven tests where practical. Do not hand-edit [`graphql/generated.go`](/Users/kaede/Developer/Utilities/rdai-bot/graphql/generated.go); treat it as generated output from [`graphql/genqlient.yaml`](/Users/kaede/Developer/Utilities/rdai-bot/graphql/genqlient.yaml).

## Testing Guidelines
Tests live beside the code as `*_test.go` files such as [`http_test.go`](/Users/kaede/Developer/Utilities/rdai-bot/http_test.go) and [`telegram_test.go`](/Users/kaede/Developer/Utilities/rdai-bot/telegram_test.go). Prefer focused end-to-end handler tests and isolated fakes for Telegram and key issuance. New tests should name the behavior under test clearly, for example `TestTelegramWebhookRejectsInvalidSecret`. Cover happy paths and failure cases for HTTP handlers, token lifecycle, and persistence changes.

## Commit & Pull Request Guidelines
Keep commit messages short, imperative, and scoped, following the existing history such as `feat: multi-arch docker` and `init`. For pull requests, include a concise summary, note any config or schema changes, link the relevant issue when available, and attach screenshots only for template or UI changes. Call out test coverage explicitly with the command you ran.

## Security & Configuration Tips
Never commit real bot tokens, AxonHub keys, or populated SQLite databases. Validate changes against the environment variable contract in [`README.md`](/Users/kaede/Developer/Utilities/rdai-bot/README.md), especially webhook settings and `HTTP_PATH_PREFIX`, because both affect runtime routing.
