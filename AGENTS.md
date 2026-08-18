# Repository guidance

## Purpose

This repository is intended to become `terraform-provider-awscontrib`, published as `registry.terraform.io/hans-m-song/awscontrib`: a small Terraform provider for AWS capabilities that are not available in the HashiCorp AWS or AWS Cloud Control providers.

The first implemented capability is the Amazon Connect queue/quick-connect association resource. A plural quick-connect discovery data source remains planned as milestone `M2`.

## Required reading

Before planning or editing, read:

1. `docs/overview.md`
2. `docs/structure.md`
3. `docs/roadmap.md`
4. `.agents/log.md`
5. `.agents/handoff.md`

Treat the repository as an uncustomized scaffold until roadmap task `M0-T01` is complete. Verify current source rather than assuming a documented future structure already exists.

## Grounding

- Prefer repository evidence and primary documentation.
- Cite external technical claims with direct links.
- State assumptions and uncertainties explicitly.
- Use absolute dates for time-sensitive facts.
- Recheck current Terraform, AWS, and provider documentation before implementation; upstream behavior is not assumed stable.
- Correct terminology: Amazon Connect quick connects can be associated with queues. There is no routing-profile/quick-connect association API in the documented Amazon Connect API.

## Architecture

Use a modular monolith:

```text
main -> provider -> conns
                -> service/connect
```

- `internal/provider` owns provider metadata, schema, configuration, and constructor registration.
- `internal/conns` owns AWS SDK configuration and client construction.
- `internal/service/connect` owns Amazon Connect schemas, API translation, and tests.
- Service packages must not import `internal/provider` or sibling service packages.
- Start with explicit registration. Do not introduce reflection or code generation for registration.
- Prefer narrow AWS client interfaces at service boundaries so API behavior can be unit tested.
- Do not copy the AWS provider's mux, generator, or broad helper hierarchy without a demonstrated need.
- Extract shared retry, diagnostic, validation, or identifier packages only after at least two consumers establish a stable abstraction.

## Terraform conventions

- Use Terraform Plugin Framework and protocol version 6.
- Use AWS SDK for Go v2 for new AWS integrations.
- Provider configuration passes pointer-typed configured data to data sources and resources.
- Select the most well-supported maintained AWS configuration integration at implementation time. Prefer stable releases and direct AWS SDK for Go v2 support; reuse a HashiCorp helper only when its current support status and API stability justify the dependency.
- Every `Configure` implementation must accept nil provider data because offline validation can occur before provider configuration.
- Model schemas explicitly; do not rely on implicit `id` attributes.
- Add constructor functions to the appropriate explicit provider registry.
- Data sources must paginate list APIs until `NextToken` is empty unless the schema explicitly exposes pagination.
- Queue/quick-connect association resources own one edge, not a queue's complete association set. Mutations serialize per instance/queue within one provider process; never claim cross-process serialization.
- Plural quick-connect results are represented as a list sorted by quick-connect ID. Do not rely on AWS response order.
- The plural quick-connect data source uses `ListQuickConnects` only. Apply quick-connect-type filters through the API and exact name filtering client-side after complete pagination.
- Use a set for unordered unique quick-connect-type filter values and a computed list nested attribute for ordered results.
- Do not add a synthetic data-source-level `id` when no meaningful remote identity exists. Each returned quick connect must expose its real `id`.
- Define ordering and uniqueness semantics before choosing list or set attributes.
- Generated files under `docs/actions`, `docs/data-sources`, `docs/ephemeral-resources`, `docs/functions`, and `docs/resources` are outputs of `make generate`; edit schema descriptions and examples instead.

## Testing and verification

For implementation changes, use the smallest applicable sequence:

1. Unit tests for pagination, filtering, empty results, mapping, and AWS error diagnostics.
2. Framework schema and metadata tests.
3. Acceptance tests for behavior that requires Terraform and AWS.
4. `make fmt`
5. `make test`
6. `make lint`
7. `make generate`, followed by a clean generated diff check.

Acceptance tests call real AWS APIs, require authorized credentials, may incur cost, and must remain gated by `TF_ACC`. Never infer permission to access an AWS account. This project has no stable real-AWS fixtures; mocked unit and framework tests are mandatory, while real-AWS tests must not be presented as a routine gate until fixtures are deliberately established.

## Documentation and continuity

- Keep `docs/overview.md`, `docs/structure.md`, and `docs/roadmap.md` aligned with verified repository state.
- Give roadmap milestones and tasks immutable IDs. Do not reuse deleted IDs.
- Record durable decisions, rejected approaches, and material lessons in `.agents/log.md`.
- Update `.agents/handoff.md` at the end of a session with completed work, current blockers, and exact next actions.
- Only the main agent edits `.agents/log.md` and `.agents/handoff.md`.
- Do not hand-edit generated provider reference documentation.

## Security

- Never read `.env*`, `.aws`, `.gcp`, `.oci`, `.config`, `.ssh`, `secrets.yaml`, environment variables, credentials, or personally identifiable information without explicit permission.
- Never place credentials in configuration examples, fixtures, logs, state assertions, or documentation.
- Use standard AWS credential resolution; do not invent a repository-specific credential store.

## Current verification commands

```text
make fmt       format Go source
make test      run the Go test suite
make lint      run golangci-lint
make generate  format examples and regenerate provider documentation
make testacc   run acceptance tests against real services
```

The current default `make` target also installs the provider. Do not use it when formatting, testing, or generation alone is intended.
