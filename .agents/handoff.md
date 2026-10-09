# Session handoff

## Updated

2026-10-09, Australia/Brisbane.

## Latest review

- M14-T03 is complete locally and uncommitted on `fix/data-table-default-discovery`. Table refresh now batch-describes defaults for every remote non-primary attribute with explicit empty primary values, including attributes absent from configuration. Exact observed `Value not found.` means absence; other failures or malformed/incomplete replies abort. Remote locks and ordinary record-resource reads are preserved.
- Full tests, formatting, lint (zero issues), generation with no reference changes, independent focused verification, and an offline SDK serialization probe passed. Additional Read/API-error regression preserves prior defaults and original cause. No agent-run AWS calls were made; corrected provider live behavior remains unverified. Table refresh requires `connect:BatchDescribeDataTableValue` permission. Next actions: review/commit/push the targeted-read changes, then verify normal plan against existing defaults using that build. Missing-item string matching remains an observed-contract limitation.

- M14-T02 is committed and pushed as `25cae94` on the same branch. It removed the DEFAULT record-ID assumption after repeated apply attempted duplicate default creation. Owner-run CLI showed the stored default has a UUID record ID and null primary values. Its full value scan is superseded by the uncommitted M14-T03 targeted reader; record resource filters remain unchanged.
- Regressions failed before the fix and passed after. Full tests, independent focused testing, formatting, lint (zero issues), generation without reference changes, and whitespace checks passed. No agent-run AWS calls were made. Next actions: review/commit/release the follow-up patch, then run a normal plan against the existing table using that provider build. Expected result: no proposed default addition. Live verification of the corrected provider remains pending.

- M14-T01 was merged in PR #34 (local merge commit `9d68531`). Create/Update planned-default retention remains, but its record-ID assumptions were incomplete and are superseded by M14-T02.

- Follow-up M13-T01 is complete: the owner approved optional `override_type`. Implemented optional/computed schema, enum omission, returned-type/null normalization, and stored-type retention after configuration removal. Created identity is recorded before type readback for recovery on errors.
- Full unit tests, focused independent tests, formatting, lint (zero issues with writable cache), and two deterministic generated-document runs passed. Real AWS remains unverified. The owner requested a PR; changes are prepared for branch `fix/optional-hours-override-type` targeting `main`. No release or merge was requested.
- Only `override_type` was changed. Nested time-window fields and data-table attributes retain their existing schemas; the M11 redesign remains pending.

- Completed a documentation/source review of API optionality across registered Connect schemas. Implementation remains unchanged; no tests or AWS calls were needed for this review.
- Confirmed mismatch: required `override_type` versus optional AWS create/update `OverrideType`. Any fix must cover request extraction and refresh normalization as well as schema flags; the omitted-type default remains unverified.
- Conditional candidates: optional nested override day/start/end fields require omission semantics; optional data-table `attributes` could default to an empty map while retaining authoritative ownership.
- AWS still marks table value lock level and recurrence frequency/interval required. Do not infer optionality from update operations or response models.
- The review initially made no code changes; the subsequently approved M13-T01 implementation is described above. The existing M11 contract/evidence work remains pending.

## Objective and outcome

Milestones `M9` and `M10` are implemented and fixture-free verified. Milestone `M11` targets a breaking `v0.5.0` redesign of the singular hours-of-operation override resource around four explicit practitioner intents, with state upgrading for classifiable `v0.4.x` states. Contract task `M11-T01` is in progress; implementation remains blocked on the remaining AWS payload evidence and contract approval.

`M12-T01` adds a singular Amazon Connect data-table lookup by ID or exact name. The source, registration, example, generated reference, and mocked/Framework verification are complete. `v0.4.4` is published on GitHub and indexed by Terraform Registry. Real-AWS behavior has not been verified.

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
- The owner authorized and completed the `v0.4.4` release, including the completed M10 Dependabot cooldown. Release commit: `3cae353acaf65a6dc4ea6104e7181bc331b2d1c2`.
- `internal/service/connect/handler.js` is an unrelated owner file. It was never read or modified and must not be staged without explicit owner direction.
- `data_tables_awscontrib.tf` is a parallel conversion of the owner-supplied, untracked `data_tables.tf`; the source file was not modified. `docs/runbooks/migrate-data-tables-to-awscontrib.md` describes the migration gates and rollback.
- Record import is now implemented, removing the provider-side migration blocker. The migration remains an operator-controlled state transition: discover each stable record ID, import every table and record, and approve all targeted plans before removing any old AWSCC state address.
- GitHub tag tests and the signed release workflow succeeded; GitHub release assets include checksums and a detached signature. Terraform Registry listed `0.4.4` on 2026-09-24.
- If feature work continues, `M2` plural quick-connect discovery remains the next proposed milestone.
- `M8` resource-name exposure is complete without source changes. Hours overrides and data tables already expose AWS names; association edges and records have no intrinsic AWS resource names and remain unnamed.
