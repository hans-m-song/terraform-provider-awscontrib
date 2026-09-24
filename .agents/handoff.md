# Session handoff

## Updated

2026-09-24, Australia/Brisbane.

## Objective and outcome

Milestones `M9` and `M10` are implemented and fixture-free verified. Milestone `M11` targets a breaking `v0.5.0` redesign of the singular hours-of-operation override resource around four explicit practitioner intents, with state upgrading for classifiable `v0.4.x` states. Contract task `M11-T01` is in progress; implementation remains blocked on the remaining AWS payload evidence and contract approval.

`M12-T01` adds a singular Amazon Connect data-table lookup by ID or exact name. The source, registration, example, generated reference, and mocked/Framework verification are complete. Real-AWS behavior has not been verified.

## Registered public surfaces

Resources:

- `awscontrib_connect_queue_quick_connect_associations`
- `awscontrib_connect_hours_of_operation_override`
- `awscontrib_connect_data_table`
- `awscontrib_connect_data_table_record`

Data sources:

- `awscontrib_connect_phone_number`
- `awscontrib_connect_contact_flow_module`
- `awscontrib_connect_data_table`

## Completed work

- Added data-table ID lookup through `DescribeDataTable` and name lookup through paginated `SearchDataTables`, exact local matching, and a final describe call. Registered the new source and generated its reference page. Mocked/Framework tests, full unit and focused race suites, `go build ./...`, lint, `make generate`, and diff checks passed.
- Added a seven-day routine-version cooldown to Dependabot's root Go module, tools Go module, and GitHub Actions entries. Security updates remain exempt under GitHub's documented behavior.
- Added stable plan behavior for computed data-table `id`/`arn` and record `record_id` using Terraform Plugin Framework `UseStateForUnknown` modifiers.
- Added executable tests proving identities remain known during update and unknown during creation, changed known `primary_values` still replace records, and ordinary `values` remain mutable in place.

- Reused one Amazon Connect SDK client per configured provider process and added operation-specific pacing at 2 requests per second with burst 1, applied to every physical SDK attempt including retries.
- Limited one provider process to two in-flight Connect attempts. Pacing precedes slot acquisition so queued requests for one API do not starve independent operations; cancellation releases waiters without background goroutines.
- Set explicit page sizes across current Connect paginators, filtered table DEFAULT reads at the API using `RecordIds: ["DEFAULT"]`, retained record-ID filters, and added repeated-token protection to queue pagination.
- Avoided unchanged table metadata mutations and unnecessary intermediate DEFAULT lock refreshes while retaining authoritative final refresh and changed-default lock handling.

- Made `quick_connect_ids` mutable in place. Update removes previously owned IDs absent from the plan, then adds missing planned IDs under the queue-scoped coordinator. Requests remain batched at 50 and unrelated associations are preserved.
- Added exact phone-number lookup through fully paginated `ListPhoneNumbersV2`, a conservative 11-character server prefix, and full-number client-side equality.
- Added exact contact-flow-module lookup through paginated `SearchContactFlowModules` and client-side exact name matching.
- Added standalone hours-of-operation override CRUD/import. Optional `time_windows` use required day and `HH:MM` opening/closing strings; omission is a canonical empty set for full-day `STANDARD` or `CLOSED` overrides. Description and recurrence removal replace the override because AWS cannot clear those fields in place.
- Added a combined data-table resource owning table metadata, the complete set of attributes represented by its schema, and explicit DEFAULT values created with `PrimaryValues` omitted.
- Added an authoritative non-default record resource with canonical composite primary maps, complete remote-cell drift discovery, current value locks, and import by stable record ID.
- Added `instance_id:data_table_id:record_id` import for non-default records. First refresh reconstructs the complete primary-value and authoritative value maps without mutation; `DEFAULT`, malformed identities, duplicate responses, and repeated tokens are rejected.
- Registered table and record constructors through one shared `{instance_id, data_table_id}` coordinator factory.
- Added examples and generated reference documentation for all new surfaces; README and changelog link/catalog entries are updated.

## Deliberate limitations

- No real AWS fixture or acceptance result exists. All current evidence is mocked, Framework, race, build, lint, and generation verification.
- Table tags and attribute validation rules are not represented. Post-create tag support is undocumented for data tables, and multiple validation false/zero removals cannot be serialized reliably by the pinned SDK.
- Data-table status accepts only `PUBLISHED`; `SAVED` remains contradictory in AWS prose versus the pinned SDK enum and official valid-values section.
- Changing a table attribute from primary to non-primary replaces the table because the SDK cannot serialize `Primary:false`. Attribute map-key renames are delete/create and can remove values.
- Record import adopts every remote non-primary cell. Configuration must include every value that should be preserved because a later authoritative plan may delete omitted cells.
- Queue/table coordinators are provider-process-local; they do not serialize separate Terraform processes or states.
- Connect scheduling is also provider-process-local. Aliased provider configurations in independent processes, concurrent Terraform runs, other AWS clients, and account-specific lower quotas can still collide with the account-and-Region service quota.

## Verification state

For `M10-T01`, local YAML parsing, exact-entry semantic checks, independent configuration review, and diff checks passed. GitHub's live Dependabot validator was not invoked.

For `M9-T01`, focused modifier tests, the full unit suite, independent Connect race tests, formatting, lint, and diff checks passed. Lint required a writable cache under `/private/tmp`; it reported zero issues and exited successfully. No AWS calls were made.

Parent verification passed:

- `make test` with Connect coverage 81.4%, connections coverage 87.3%, and provider coverage 95.7%;
- full ordinary and race tests independently, plus focused import verification;
- `go build ./...`;
- `make lint` with zero issues;
- two consecutive `make generate` runs using pinned Terraform CLI 1.14.0;
- `git diff --check` and example formatting.

Independent verification passed all feature-level tests and race checks. Its default-cache lint/generation attempts were environment-limited; parent escalated runs completed the genuine gates. No AWS calls were made.

## Working tree and next actions

- `M12-T01` is fixture-free verified. No real AWS call has been made; any future acceptance coverage requires authorized fixtures.
- `M11-T01` is in progress. Collect and verify payloads for temporary whole-day closure, temporary replacement hours, recurring partial closure, recurring open hours, and supported monthly/yearly recurrence shapes; then freeze the contract before implementation.
- Preserve one Terraform resource per AWS override. Do not introduce the rejected authoritative plural resource; AWS has no batch override mutation API and collection applies would be non-atomic.
- Design the `v0.4.x` schema-zero state upgrader only after intent mappings are frozen. It must not call AWS or guess at unclassifiable legacy combinations.
- The owner authorized pushing and releasing the current provider changes as `v0.4.4`, including the completed M10 Dependabot cooldown.
- `internal/service/connect/handler.js` is an unrelated owner file. It was never read or modified and must not be staged without explicit owner direction.
- `data_tables_awscontrib.tf` is a parallel conversion of the owner-supplied, untracked `data_tables.tf`; the source file was not modified. `docs/runbooks/migrate-data-tables-to-awscontrib.md` describes the migration gates and rollback.
- Record import is now implemented, removing the provider-side migration blocker. The migration remains an operator-controlled state transition: discover each stable record ID, import every table and record, and approve all targeted plans before removing any old AWSCC state address.
- Push the reviewed release commit and `v0.4.4` tag, then confirm the tag-triggered verification and signed GitHub release succeed.
- If feature work continues, `M2` plural quick-connect discovery remains the next proposed milestone.
- `M8` resource-name exposure is complete without source changes. Hours overrides and data tables already expose AWS names; association edges and records have no intrinsic AWS resource names and remain unnamed.
