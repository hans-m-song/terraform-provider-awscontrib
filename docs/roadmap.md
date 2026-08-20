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

### M1-T06 — Accept unknown quick-connect IDs during planning

- Status: Complete.
- Goal: allow `quick_connect_ids` to reference computed IDs of quick connects created in the same Terraform operation.
- Scope: make set validation preserve Terraform null and unknown semantics; continue validating every known quick-connect ID as a UUID; add regression coverage for unknown elements and known invalid values.
- Constraints: do not weaken apply-time behavior or introduce configuration workarounds; no provider retry changes; no real AWS calls.
- Acceptance criteria: planning a nonempty set containing unknown string elements produces no value-conversion diagnostic; known invalid UUIDs and an empty set still produce diagnostics; known valid values retain existing lifecycle behavior.
- Roles: main agent, executor, tester.
- Dependencies: `M1-T05`.
- Verification gates: focused unit and race tests, complete unit suite, build, lint, generated-document drift review, and independent tester report.
- Blockers: none.
- Parallel boundaries: implementation and tests share one file pair and remain one ownership unit; independent verification follows the implementation checkpoint.

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

## M4 — Connect lifecycle and exact lookups

Goal: support in-place queue association changes and exact lookup of Connect phone numbers and contact-flow modules.

Status: Complete.

### M4-T01 — Freeze in-place association reconciliation

- Status: Complete.
- Goal: change `quick_connect_ids` without replacing the Terraform resource.
- Scope: prior-state, planned-state, and complete remote-membership reconciliation under the existing queue coordinator.
- Constraints: preserve unrelated remote associations; retain replacement for `instance_id` and `queue_id`; batch requests in groups of at most 50; do not claim transactional or cross-process behavior.
- Acceptance criteria: additions are `planned - remote`; removals are `(prior - planned) intersect remote`; removed owned IDs are disassociated, unchanged IDs are untouched, and unrelated IDs remain associated. A failed batch returns a diagnostic without writing planned state; a later refresh exposes any partial remote completion.
- Roles: main agent, executor, tester.
- Dependencies: `M1-T06`.
- Verification gates: schema tests, differential lifecycle tests, batching tests, partial-failure tests, focused race tests, full unit suite, build, and lint.
- Blockers: none.
- Parallel boundaries: implementation and its fake-client tests are one ownership unit.

### M4-T02 — Implement in-place association updates

- Status: Complete.
- Goal: implement the `M4-T01` contract.
- Scope: remove replacement semantics from `quick_connect_ids`, add `Update`, and retain unknown-value behavior during planning.
- Constraints: no provider retry or mode settings; no real AWS calls.
- Acceptance criteria: plans can add and remove IDs in place, update reconciliation is idempotent, and existing create/read/delete/import behavior remains compatible.
- Roles: executor, tester, main agent.
- Dependencies: `M4-T01`.
- Verification gates: focused and full tests, race tests, build, lint, and generated documentation review.
- Blockers: none.
- Parallel boundaries: lookup data-source implementation may proceed independently after shared client interfaces are coordinated.

### M4-T03 — Implement exact phone-number lookup

- Status: Complete.
- Goal: add `awscontrib_connect_phone_number`, selected by a full phone number.
- Scope: required `instance_id` and E.164 `phone_number`; computed remote ID and summary attributes returned by `ListPhoneNumbersV2`.
- Constraints: use the API prefix only to reduce candidates, then require exact equality client-side after complete pagination; the API prefix length limit means a longer configured number must use a safe prefix no longer than the documented maximum; do not select an ambiguous result.
- Acceptance criteria: zero exact matches produce a not-found diagnostic, multiple exact matches produce an ambiguity diagnostic, one exact match populates deterministic state, and pagination and AWS errors are tested.
- Roles: executor, tester, main agent.
- Dependencies: `M0-T02`, `M0-T03`.
- Verification gates: narrow-client unit tests, Framework schema/configuration tests, full unit suite, build, and lint.
- Blockers: none.
- Parallel boundaries: independent from association reconciliation except for provider registration and the Connect client factory.

### M4-T04 — Implement exact contact-flow-module lookup

- Status: Complete.
- Goal: add `awscontrib_connect_contact_flow_module`, selected by exact name.
- Scope: required `instance_id` and `name`; computed fields available from the selected module returned by the current AWS API.
- Constraints: paginate the selected search/list operation completely and enforce exact client-side equality; do not add the already-available contact-flow lookup.
- Acceptance criteria: zero and multiple exact matches produce diagnostics, one exact match populates state, and pagination, mapping, nil configuration, and AWS errors are tested.
- Roles: executor, tester, main agent.
- Dependencies: `M0-T02`, `M0-T03`.
- Verification gates: narrow-client unit tests, Framework tests, full unit suite, build, and lint.
- Blockers: exact schema fields must be checked against the pinned AWS SDK before implementation.
- Parallel boundaries: independent from phone-number lookup after provider registration changes are coordinated.

### M4-T05 — Document and verify lifecycle and lookups

- Status: Complete.
- Goal: publish examples and reproducible generated reference documentation for `M4`.
- Scope: examples, schema descriptions, generated docs, maintained architecture documentation, and fixture-free verification.
- Constraints: generated reference pages are not hand-edited; no credentials, personal identifiers, real fixtures, or real AWS calls.
- Acceptance criteria: all `M4` behavior is documented and two consecutive generations are clean.
- Roles: scribe, tester, main agent.
- Dependencies: `M4-T02`, `M4-T03`, `M4-T04`.
- Verification gates: format, full tests, focused race tests, build, lint, two generations, and diff audit.
- Blockers: none after implementation.
- Parallel boundaries: example authoring may begin from frozen schemas; generation is serialized after provider registration.

## M5 — Hours-of-operation overrides

Goal: manage an Amazon Connect hours-of-operation override as a standalone Terraform resource so removed attributes are reconciled explicitly.

Status: Complete.

### M5-T01 — Freeze the override contract

- Status: Complete.
- Goal: define the schema and conditional semantics for dates, type, configuration, and recurrence from current AWS API evidence.
- Scope: standalone identity, CRUD, import, absence behavior, ordering, and validation.
- Constraints: do not invent undocumented conditional rules or canonical ordering; do not copy AWSCC optional/computed removal behavior.
- Acceptance criteria: the contract identifies every mutable field, distinguishes omitted from empty values where the API does, and specifies an unambiguous `instance_id:hours_of_operation_id:override_id` import.
- Roles: architect, explorer, main agent.
- Dependencies: `M0`.
- Verification gates: current AWS API and pinned SDK type audit.
- Blockers: none. The API can explicitly clear `config` with an empty list but cannot explicitly clear `description` or recurrence. Removing either optional field therefore requires replacement; value changes remain mutable.
- Parallel boundaries: fake fixtures may be drafted after the contract is frozen.

### M5-T02 — Implement override CRUD and import

- Status: Complete.
- Goal: implement create, describe/read, update, delete, and import through a narrow Connect interface.
- Scope: resource schema, API mapping, registration, not-found handling, and complete planned-value updates.
- Constraints: no provider-local lock unless evidence shows shared mutable parent state; no real AWS calls.
- Acceptance criteria: removals are sent explicitly where supported, read removes absent state, delete tolerates absence, and import hydrates all identity fields.
- Roles: executor, main agent.
- Dependencies: `M5-T01`.
- Verification gates: focused unit and Framework tests.
- Blockers: none.
- Parallel boundaries: examples may proceed only after schema compilation.

### M5-T03 — Verify and document overrides

- Status: Complete.
- Goal: establish fixture-free lifecycle confidence and publish accurate documentation.
- Scope: boundary validation, mapping, update/removal, not-found, import, examples, generated docs, and maintained docs.
- Constraints: real AWS verification remains supplementary and separately authorized.
- Acceptance criteria: focused and full tests, build, lint, and two clean generations pass.
- Roles: tester, scribe, main agent.
- Dependencies: `M5-T02`.
- Verification gates: repository standard verification sequence.
- Blockers: none after implementation.
- Parallel boundaries: documentation and independent testing may proceed after the implementation checkpoint.

### M5-T04 — Improve time-window configuration

- Status: Complete.
- Goal: replace the API-shaped schedule input with concise Terraform time windows before publication.
- Scope: rename `config` to `time_windows`; replace nested hour/minute objects with validated `opens` and `closes` strings in `HH:MM` format; make the set optional with a canonical empty default.
- Constraints: `day` remains required and is never inferred; omission and an explicit empty set are equivalent; AWS create/update requests always receive a non-nil configuration slice; do not impose ordering or overlap rules not established by AWS.
- Acceptance criteria: `CLOSED` and `STANDARD` accept no windows and send `Config: []`; `OPEN` requires at least one window; removing the final window sends an explicit empty update; read maps an empty remote configuration to the canonical empty set; all time boundaries from `00:00` through `23:59` round-trip.
- Roles: executor, tester, scribe, main agent.
- Dependencies: `M5-T03`.
- Verification gates: schema implementation validation, mapping and lifecycle unit tests, focused race tests, full tests, build, lint, and two clean documentation generations.
- Blockers: none.
- Parallel boundaries: implementation owns the hours resource/test pair; examples and generated documentation follow the compiled schema.

## M6 — Connect data tables

Goal: manage data-table metadata, its complete provider-managed attribute set and explicit DEFAULT values together, plus non-default records with composite primary keys.

Status: Complete.

### M6-T01 — Freeze table ownership and schema

- Status: Complete.
- Goal: define authoritative ownership for table metadata, attributes, and explicit DEFAULT values.
- Scope: combined `awscontrib_connect_data_table` schema, lifecycle ordering, import, lock versions, and partial batch failures.
- Constraints: attributes are keyed by name for stable Terraform addressing; removal deletes the remote attribute; lock versions are computed operational tokens; no automatic rollback after partial success.
- Acceptance criteria: `attributes` and `default_values` have deterministic map semantics; omitted `PrimaryValues` creates the concrete AWS `RecordId` `DEFAULT`; absence from `default_values` means no stored default even if the console renders an implicit empty default row; import adopts the complete remote schema/default set.
- Roles: architect, explorer, main agent.
- Dependencies: `M0`.
- Verification gates: current AWS API and pinned SDK audit plus the owner's 2026-08-19 CLI observations recorded in the architecture log.
- Blockers: none for the initial schema. `status` exposes only the verified `PUBLISHED` value and is replacement-only; tags and attribute validation are deferred because their update/removal behavior is not representable reliably through the pinned SDK. Changing an attribute from primary to non-primary requires table replacement because the SDK serializer cannot send `Primary:false`.
- Parallel boundaries: record implementation waits for this contract.

### M6-T02 — Implement table, attributes, and DEFAULT lifecycle

- Status: Complete.
- Goal: implement the combined table resource and explicit deletion of removed attributes and defaults.
- Scope: table CRUD, attribute create/update/delete, DEFAULT create/update/delete with `PrimaryValues` omitted, registration, import, and refresh.
- Constraints: primary attributes are created before dependent values; removed attributes are deleted last; every paginated read is exhausted; nonempty batch `Failed` results are errors even on HTTP 200.
- Acceptance criteria: create and update converge metadata/schema/defaults, read reconstructs authoritative state, delete tolerates absence, and partial completion remains recoverable by refresh and reapply.
- Roles: executor, main agent.
- Dependencies: `M6-T01`.
- Verification gates: narrow-client lifecycle tests, pagination tests, Framework tests, and build.
- Blockers: `M6-T01`.
- Parallel boundaries: shared table coordination may be implemented with this task; record work starts after its interface is stable.

### M6-T03 — Freeze non-default record ownership

- Status: Complete.
- Goal: define `awscontrib_connect_data_table_record` for nonempty, possibly composite primary keys.
- Scope: canonical primary-key map, authoritative non-primary value map, computed record ID, update/delete semantics, and import decision.
- Constraints: ordinary records may not use the DEFAULT sentinel; primary-value slices are sorted lexicographically by attribute name; removing a managed value deletes the remote cell; overlapping cell ownership is unsupported.
- Acceptance criteria: the full remote record value set is discovered during read so out-of-band values appear as drift; primary-key changes are replacement-only for the initial contract; the initial import decision is revisited by `M6-T06` after ownership reconstruction becomes verifiable.
- Roles: architect, explorer, main agent.
- Dependencies: `M6-T01`.
- Verification gates: current AWS API and pinned SDK type audit.
- Blockers: none. Read resolves the record ID from paginated primary values, then loads the complete remote record by that ID. The initial import omission was superseded by `M6-T06` after the record-ID lookup path established that both primary values and the complete authoritative value set can be reconstructed.
- Parallel boundaries: implementation follows the stable combined-table client/locking boundary.

### M6-T04 — Implement composite data-table records

- Status: Complete.
- Goal: implement non-default record create/read/update/delete with composite primary keys.
- Scope: canonical API conversion, authoritative value reconciliation, shared table-key coordination, computed record ID, and diagnostics for mixed batch results.
- Constraints: `primary_values` must be nonempty; `values` must be nonempty; no DEFAULT record ownership; no delimiter-based composite identity.
- Acceptance criteria: composite keys are stable regardless of Terraform map order, removed values are deleted explicitly, external values surface as drift, concurrent same-table mutations serialize within one provider process, and partial batch results are actionable.
- Roles: executor, tester, main agent.
- Dependencies: `M6-T02`, `M6-T03`.
- Verification gates: exhaustive mocked lifecycle, pagination, lock, batch-failure, unknown-value, and race tests.
- Blockers: preceding contracts.
- Parallel boundaries: documentation may proceed from the compiled schema; final verification is serialized.

### M6-T05 — Document and verify data tables

- Status: Complete.
- Goal: publish examples and complete fixture-free verification for both data-table resources.
- Scope: explicit defaults, composite keys, destructive attribute ownership warning, generated docs, maintained docs, and full verification.
- Constraints: no hand-edited generated pages and no claim of real-service acceptance coverage.
- Acceptance criteria: examples demonstrate DEFAULT and composite-record use; full tests, focused race tests, build, lint, and two clean generations pass.
- Roles: scribe, tester, main agent.
- Dependencies: `M6-T02`, `M6-T04`.
- Verification gates: repository standard verification sequence plus final diff audit.
- Blockers: none after implementation.
- Parallel boundaries: independent testing and maintained documentation may proceed after source completion.

### M6-T06 — Add non-default record import

- Status: Complete.
- Goal: permit safe adoption of existing non-default data-table records by stable AWS record ID.
- Scope: import `instance_id:data_table_id:record_id`, reconstruct the composite primary-value map and complete authoritative value map during refresh, document migration behavior, and reject absent, ambiguous, or `DEFAULT` identities.
- Constraints: import performs no mutation; imported state adopts every remote non-primary cell; identifiers are not encoded from delimiter-separated composite primary values; all reads paginate and detect repeated tokens.
- Acceptance criteria: import seeds stable identity, first refresh reconstructs `primary_values`, `values`, and `record_id`, missing records are removed from state, malformed identifiers produce actionable diagnostics, and the generated resource documentation includes an import example.
- Roles: executor, tester, scribe, main agent.
- Dependencies: `M6-T04`, `M6-T05`.
- Verification gates: focused import and pagination tests, full tests, focused race tests, build, lint, and two clean documentation generations.
- Blockers: none.
- Parallel boundaries: source and focused tests share one implementation boundary; documentation follows the compiled schema; independent verification begins after integration.

## Deferred

- Actions, functions, and ephemeral resources remain out of scope unless a future milestone establishes a concrete use case.
- Shared retry, identifier, validator, and diagnostic packages are deferred until repeated implementations establish their contracts.
