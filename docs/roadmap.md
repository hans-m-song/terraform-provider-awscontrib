# Roadmap

Task and milestone IDs are immutable. Removed IDs are not reused.

## Status vocabulary

| Status | Meaning |
|---|---|
| Proposed | Scope exists but is not approved for implementation. |
| Ready | Dependencies and decisions are complete. |
| In progress | Implementation has begun. |
| Blocked | A recorded external decision or dependency prevents progress. |
| Complete | Acceptance criteria and verification gates have passed. |

## M0 — Provider bootstrap

Goal: replace scaffold identity and example connectivity with a minimal, testable AWS provider foundation.

Status: Complete.

### M0-T01 — Fix the public contract

- Status: Complete.
- Goal: apply the decided provider source address, Terraform type name, Go module path, minimum Terraform version, and minimum Go version.
- Scope: naming and compatibility decisions only.
- Constraints: do not publish or preserve accidental `scaffolding_*` contracts.
- Acceptance criteria: the source address is `registry.terraform.io/hans-m-song/awscontrib`; the type name is `awscontrib`; the module is `github.com/hans-m-song/terraform-provider-awscontrib`; Terraform 1.0+ and Go 1.26 are documented; README, modules, server address, tests, examples, and generator naming agree.
- Roles: architect, main agent.
- Dependencies: none.
- Verification gates: repository-wide search finds no unintended scaffold identity; documentation is internally consistent.
- Blockers: none.
- Parallel boundaries: AWS configuration research may proceed; identity edits must remain centralized.

### M0-T02 — Establish AWS configuration

- Status: Complete.
- Goal: configure AWS SDK for Go v2 and expose a pointer-typed client factory to services.
- Scope: initial region, AWS shared configuration and credentials files, explicit `profile` selection, diagnostics, provider tests, and documentation.
- Constraints: no custom credential store; no unsupported promise of parity with every `aws` provider option.
- Acceptance criteria: configuration creates the intended AWS SDK config, standard INI files and explicit profile selection work, offline validation handles nil provider data, and tests cover configuration diagnostics.
- Roles: executor, tester.
- Dependencies: `M0-T01`.
- Verification gates: unit tests, framework provider tests, `make test`, `make lint`.
- Blockers: none. At implementation time, document a brief support comparison and select the most well-supported maintained option, preferring stable AWS SDK for Go v2 APIs. Assume-role, custom endpoints, custom retries, and user-agent configuration are outside the MVP unless required by the selected integration.
- Parallel boundaries: service interfaces may be designed against an abstract factory; concrete provider wiring waits for this task.

### M0-T03 — Introduce service boundaries

- Status: Complete.
- Goal: separate provider registration, AWS connections, and Amazon Connect behavior.
- Scope: create `internal/conns`, `internal/service/connect`, and explicit data-source registration.
- Constraints: no reflection, generated registration, or premature general-purpose helper packages.
- Acceptance criteria: dependency rules in `docs/structure.md` are enforced by package imports and tests.
- Roles: executor, tester.
- Dependencies: `M0-T01`, coordinated with `M0-T02`.
- Verification gates: `go test ./...`, lint, and import-cycle check through successful build.
- Blockers: none.
- Parallel boundaries: package skeleton and service contract tests can proceed independently of final provider wiring.

## M1 — Amazon Connect queue/quick-connect association

Goal: manage a set of queue-to-quick-connect associations per Terraform resource with safe same-process concurrency.

Status: Complete.

### M1-T01 — Define the association contract

- Status: Complete.
- Goal: define schema, identity, lifecycle, absence semantics, concurrency boundaries, and import format.
- Scope: required `instance_id`, `queue_id`, and `quick_connect_id`; replacement-only identity; one edge per resource.
- Constraints: AWS exposes no association object or identifier; the resource must not claim ownership of the queue's complete association set.
- Acceptance criteria: create uses `AssociateQueueQuickConnects`; read paginates `ListQueueQuickConnects`; delete uses `DisassociateQueueQuickConnects`; absent membership removes state; mutations serialize per instance/queue within one provider process.
- Roles: architect, project analyst, main agent.
- Dependencies: `M0-T01`.
- Verification gates: contract checked against current primary AWS API documentation and Terraform Framework resource conventions.
- Blockers: none.
- Parallel boundaries: implementation may proceed after this contract; import encoding remains internal until documented before release.

### M1-T02 — Implement the association resource

- Status: Complete.
- Goal: implement configuration, schema, lifecycle, import, registration, and queue-scoped mutation coordination.
- Scope: AWS SDK for Go v2 Connect client, narrow test interface, composite identity, membership reconciliation, and actionable diagnostics.
- Constraints: one API ID per mutation even though AWS permits batches; serialize only queues sharing `{instance_id, queue_id}`; do not retry all client errors.
- Acceptance criteria: create is membership-idempotent; read exhausts pagination and removes absent state; delete tolerates absent membership; replacement occurs for identity changes; import populates all required attributes.
- Roles: executor, main agent.
- Dependencies: `M0-T02`, `M0-T03`, `M1-T01`.
- Verification gates: focused unit and Framework tests pass.
- Blockers: none.
- Parallel boundaries: examples may be drafted against the frozen schema; generated docs wait for compiled implementation.

### M1-T03 — Test concurrency and failure semantics

- Status: Complete.
- Goal: verify queue-scoped serialization, independent-queue parallelism, pagination, drift, and uncertain outcomes without AWS fixtures.
- Scope: deterministic fake Connect client tests and Framework schema/import tests.
- Constraints: do not claim cross-process serialization; real-AWS tests are supplementary only.
- Acceptance criteria: tests demonstrate that same-queue mutations do not overlap, different queues are not globally locked, all pages are searched, and absent associations converge cleanly.
- Roles: tester, executor.
- Dependencies: `M1-T02`.
- Verification gates: race-enabled focused tests where practical, full unit suite, lint, and build.
- Blockers: none.
- Parallel boundaries: independent test review begins after the implementation checkpoint.

### M1-T04 — Document and verify the resource

- Status: Complete.
- Goal: provide an example, import syntax, generated reference documentation, and reproducible repository verification.
- Scope: schema descriptions, resource example, import example, generated docs, README alignment, log, and handoff.
- Constraints: generated reference files are not hand-edited; no credentials or real identifiers.
- Acceptance criteria: a clean second generation, build, unit tests, lint, and documentation review pass; lack of real-AWS fixture is disclosed.
- Roles: executor, scribe, tester, main agent.
- Dependencies: `M1-T02`, `M1-T03`.
- Verification gates: `make generate`, generated-diff inspection, `make test`, `make lint`, and build.
- Blockers: none.
- Parallel boundaries: maintained documentation and examples can be prepared independently; final generation and handoff are serialized.

### M1-T05 — Batch queue quick-connect associations

- Status: Complete.
- Goal: reduce Amazon Connect request pressure by managing an unordered set of quick-connect IDs per queue.
- Scope: replace the unpublished singular resource contract with `awscontrib_connect_queue_quick_connect_associations`; use required `quick_connect_ids` set semantics; batch associate and disassociate requests in groups up to the documented 50-ID API limit; use colon-delimited composite import with a comma-delimited, canonical ID suffix; recognize `TooManyRequestsException` during reconciliation; update tests, examples, generated references, and maintained documentation.
- Constraints: the resource owns only its declared association subset and must preserve unrelated remote associations; overlapping ownership across resources is unsupported; provider retry configuration remains unchanged; no real AWS calls or acceptance-fixture claims.
- Acceptance criteria: create and delete batch the declared IDs; read detects partial and total drift without adopting unrelated associations; import validates and canonicalizes all IDs; deterministic tests cover throttling, batching, pagination, drift, import, and same-queue concurrency.
- Roles: main agent, executor, tester.
- Dependencies: `M1-T04`.
- Verification gates: focused race tests, complete unit tests, build, lint, generation, generated-diff check, and independent tester report.
- Blockers: none.
- Parallel boundaries: resource lifecycle/schema edits may proceed independently from maintained documentation until provider registration and generated documentation integration.

## M2 — Amazon Connect quick-connect discovery

Goal: provide a plural data source that lists quick connects in an Amazon Connect instance.

Status: Proposed.

### M2-T01 — Define the data-source contract

- Goal: implement the decided Terraform-standard plural quick-connect contract in a reviewed schema proposal.
- Scope: contract and examples; no API implementation.
- Constraints: distinguish `ListQuickConnects` summary fields from `DescribeQuickConnect` fields; do not duplicate the singular `aws_connect_quick_connect` contract.
- Acceptance criteria: schema defines required string `instance_id`; optional string `name`; optional enum-validated set `quick_connect_types`; computed list nested attribute `quick_connects` sorted lexicographically by item ID; required computed item `id`; all other API summary fields as computed attributes; no synthetic top-level `id`; complete pagination; and deterministic empty state.
- Roles: architect, project analyst, main agent.
- Dependencies: `M0-T01`.
- Verification gates: contract is checked against the current AWS API and current Terraform Plugin Framework conventions.
- Blockers: none.
- Parallel boundaries: API test fixtures may be designed after the output shape is stable.

### M2-T02 — Implement and unit test the data source

- Goal: implement paginated `ListQuickConnects` behavior through a narrow Connect client interface.
- Scope: schema, configuration, API pagination, mapping, diagnostics, and unit/framework tests.
- Constraints: never return only the first page; do not call `DescribeQuickConnect` unless separately approved as part of the contract.
- Acceptance criteria: tests cover one page, multiple pages, API type filters, exact name filtering after pagination, empty results, ordering by ID, nil configuration, and AWS errors.
- Roles: executor, tester.
- Dependencies: `M0-T02`, `M0-T03`, `M2-T01`.
- Verification gates: targeted tests, `make test`, and `make lint` pass.
- Blockers: none after dependencies.
- Parallel boundaries: example drafting can proceed against the approved schema; generated documentation waits for compiled schema code.

### M2-T03 — Add examples and generated documentation

- Goal: publish accurate usage and reference documentation.
- Scope: provider example, data-source example, schema descriptions, generated data-source documentation, and changelog entry if the release process requires one.
- Constraints: generated reference files are not edited directly.
- Acceptance criteria: examples use no credentials, explain summary-only results, and generated files are reproducible.
- Roles: executor, scribe, tester.
- Dependencies: `M2-T02`.
- Verification gates: `make generate` followed by a clean second generation; example formatting passes.
- Blockers: none after implementation.
- Parallel boundaries: maintained project documentation may be updated separately from generated reference output.

### M2-T04 — Complete fixture-free verification

- Goal: establish the strongest repeatable verification possible without real Amazon Connect fixtures.
- Scope: mocked AWS client tests, Framework provider/data-source tests, build, lint, and reproducible documentation generation.
- Constraints: do not fabricate an acceptance-test result or require personal AWS infrastructure.
- Acceptance criteria: tests prove request filters, complete pagination, post-pagination name filtering, deterministic ordering, empty results, diagnostics, and provider configuration wiring. The release notes disclose that real-AWS integration was not automatically verified.
- Roles: tester, main agent.
- Dependencies: `M2-T02`, `M2-T03`.
- Verification gates: full unit/framework suite, lint, build, and generated-diff check. A separately authorized `TF_ACC` smoke test is supplementary, not required.
- Blockers: none for fixture-free verification.
- Parallel boundaries: an optional real-AWS smoke-test procedure may be documented independently, but no stable fixture is assumed.

## M3 — Initial release readiness

Goal: prepare the implemented provider for a verifiable `v0.1.0` release without publishing it.

Status: Complete.

### M3-T01 — Correct repository and release metadata

- Status: Complete.
- Goal: remove stale scaffold maintenance metadata while retaining valid upstream attribution.
- Scope: CODEOWNERS, community guidance, changelog, provider installation examples, and release configuration cleanup.
- Constraints: preserve MPL-2.0 and inherited IBM/HashiCorp notices; do not inspect signing secrets or publish artifacts.
- Acceptance criteria: repository ownership signals identify this project rather than HashiCorp; `0.1.0` changes and installation source are documented; release metadata is internally consistent.
- Roles: executor, main agent.
- Dependencies: `M1`.
- Verification gates: repository search, documentation review, and GoReleaser configuration check.
- Blockers: none.
- Parallel boundaries: CI and generator corrections may proceed independently.

### M3-T02 — Repair fixture-free CI and documentation generation

- Status: Complete.
- Goal: make automated verification accurately exercise the implemented packages and reproduce generated reference documentation.
- Scope: CI job names and commands, Terraform compatibility matrix, tfplugindocs configuration, and generation checks.
- Constraints: no real AWS calls; do not represent unit tests as acceptance tests; generated reference files remain generated outputs.
- Acceptance criteria: CI runs the complete unit suite and focused race tests without `TF_ACC`; `make generate` succeeds twice without tracked drift; the provider namespace remains `hans-m-song/awscontrib`.
- Roles: explorer, executor, tester, main agent.
- Dependencies: `M1-T04`.
- Verification gates: workflow review, tests, race tests, lint, build, and two clean generations.
- Blockers: current local generation fails during provider schema loading and requires root-cause correction.
- Parallel boundaries: generator investigation is read-only until its cause is isolated; metadata edits do not touch generator code.

### M3-T03 — Complete pre-release verification

- Status: Complete.
- Goal: validate unsigned artifacts and identify the remaining owner-only signing and publication steps.
- Scope: formatting, tests, race tests, lint, build, module tidiness, generated documentation, GoReleaser check, and unsigned snapshot inspection.
- Constraints: no signing-key inspection, AWS calls, commit, tag, push, GitHub release, or Registry publication.
- Acceptance criteria: all repository-controlled gates pass; remaining external steps are explicit and limited to signed release execution and publication authorization.
- Roles: tester, main agent.
- Dependencies: `M3-T01`, `M3-T02`.
- Verification gates: independent tester report and final diff audit.
- Blockers: none after dependencies.
- Parallel boundaries: unsigned packaging may run after source and documentation generation are stable.

## Deferred

- Actions, functions, and ephemeral resources remain out of scope unless a future milestone establishes a concrete use case.
- Shared retry, identifier, validator, and diagnostic packages are deferred until repeated implementations establish their contracts.
