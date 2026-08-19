# Project overview

## Goal

`terraform-provider-awscontrib` is planned as a focused provider for miscellaneous AWS capabilities that are absent from the HashiCorp AWS and AWS Cloud Control providers. Its publication address is `registry.terraform.io/hans-m-song/awscontrib`, its Terraform type name is `awscontrib`, and its Go module is `github.com/hans-m-song/terraform-provider-awscontrib`.

The first implemented feature is an Amazon Connect queue/quick-connect association resource. Planned work adds in-place association reconciliation, exact phone-number and contact-flow-module lookups, standalone hours-of-operation overrides, and direct data-table lifecycle management. A plural quick-connect discovery data source remains a separate proposed milestone.

## Current state

As of 2026-08-19, the provider bootstrap and the approved Amazon Connect lifecycle expansion are implemented:

- the Go module is `github.com/hans-m-song/terraform-provider-awscontrib`;
- the provider type is `awscontrib` and server address is `registry.terraform.io/hans-m-song/awscontrib`;
- provider configuration uses AWS SDK for Go v2 with optional `profile` and `region`;
- `internal/conns` owns AWS configuration and client construction;
- `internal/service/connect` owns four resources and two exact-match data sources;
- scaffold resources, data sources, actions, functions, and ephemeral resources have been removed;
- registered resources manage queue/quick-connect associations, hours-of-operation overrides, combined data tables, and composite-key data-table records;
- registered data sources look up phone numbers by full number and contact-flow modules by exact name;
- maintained examples and generated reference documentation cover every registered surface;
- fixture-free CI runs the complete unit suite, focused race tests, build, lint, and deterministic documentation generation;
- tag-triggered releases run the same repository-controlled verification before signed GoReleaser packaging.

The implementation is fixture-free: mocked and Framework tests are required, while no real Amazon Connect acceptance result is claimed.

Documentation schema export is pinned to Terraform CLI 1.14.0. `terraform-plugin-docs` 0.25.0 failed to load its temporary provider installation with Terraform CLI 1.15.8 on 2026-08-18, while the same generation completed with 1.14.0. This tooling pin does not change the provider's documented Terraform CLI compatibility baseline.

## Planned stack

| Concern | Planned choice | Basis |
|---|---|---|
| Provider SDK | Terraform Plugin Framework, protocol 6 | The scaffold and registry manifest already use protocol 6; Framework is HashiCorp's recommended SDK for new providers. |
| AWS integration | AWS SDK for Go v2 | Current `terraform-provider-aws` guidance requires SDK v2 and Plugin Framework for new resources and data sources. |
| Packaging | Service-oriented modular monolith | Keeps provider configuration separate from Amazon Connect behavior without importing the AWS provider's scale-specific machinery. |
| Documentation | Schema and examples generate reference docs | This is the existing `tfplugindocs` workflow in `tools/tools.go`. |
| Testing | Unit and Framework tests; optional gated smoke tests | No stable real-AWS fixtures are available, so deterministic mocked verification is the required gate. |

The compatibility baseline is Terraform CLI 1.0 or newer and Go 1.26. Protocol 6 officially supports Terraform 1.0 and later. Go 1.26 aligns with the current `terraform-provider-aws` development baseline as observed on 2026-08-18; the exact patch version must be pinned consistently in `go.mod`, `tools/go.mod`, CI, and contributor documentation during bootstrap.

Provider configuration must support the AWS shared configuration and credentials files, including explicit `profile` selection. At implementation time, select the most well-supported maintained integration. Prefer stable AWS SDK for Go v2 APIs. Reuse a HashiCorp AWS configuration helper only if its then-current release status, maintenance, documentation, and upstream adoption make it better supported than direct SDK configuration. Record the evidence and pinned version; do not preserve a dependency merely because it was previously considered.

## Implemented association contract

The resource manages an additive subset of queue-to-quick-connect relationship edges:

```text
instance_id + queue_id + quick_connect_ids (set)
        |
        |-- Create: ListQueueQuickConnects, then AssociateQueueQuickConnects
        |            for declared IDs that are missing (batches of at most 50)
        |-- Read:   paginated ListQueueQuickConnects, retaining the
        |            intersection with the declared IDs
        `-- Delete: DisassociateQueueQuickConnects for owned IDs that are
                   present (batches of at most 50)
```

`instance_id`, `queue_id`, and `quick_connect_ids` are required. `instance_id` and `queue_id` are replacement-only. The unordered UUID set `quick_connect_ids` changes in place by reconciling prior ownership, planned ownership, and complete remote membership under the existing queue lock. Create does not adopt unrelated queue associations; read reports partial drift through the declared IDs that remain associated and removes state when none remain; delete disassociates only owned IDs that are present, preserving unrelated associations.

Overlapping ownership of the same instance, queue, and quick-connect ID across resource instances or Terraform states is unsupported. Mutations targeting the same instance and queue share a provider-local keyed coordinator, while independent queues are not globally serialized. AWS does not document cross-process serialization.

Import uses `instance_id:queue_id:quick_connect_id[,quick_connect_id...]`. IDs must be UUIDs, duplicate IDs are rejected, and the imported set is sorted into canonical state.

## Direct Connect lifecycle

The implemented feature set after association reconciliation is:

```text
exact lookup data sources
  ├── phone number by full number
  └── contact-flow module by exact name

standalone hours-of-operation override
  └── explicit create/read/update/delete and removal semantics

combined data table
  ├── table metadata
  ├── complete managed attribute set
  └── explicit DEFAULT values
        |
        v
non-default data-table record
  ├── composite primary-value map
  └── authoritative value map
```

The phone-number lookup may use `PhoneNumberPrefix` to narrow the AWS result set, but it must paginate and enforce equality against the full configured number. The contact-flow-module lookup likewise enforces exact name equality after complete pagination. A contact-flow lookup is deliberately omitted because the HashiCorp AWS provider already supplies one.

Hours overrides are modeled separately from their parent hours-of-operation resource. This gives Terraform a distinct remote identity and lets update code send explicit removals instead of relying on ambiguous nested optional/computed state. Schedule input uses an optional `time_windows` set with required `day` and zero-padded `opens`/`closes` strings. Omission is a canonical empty set: `STANDARD` and `CLOSED` may represent full-day closure without boilerplate, while `OPEN` requires at least one window.

The data-table resource combines table metadata and its complete managed attribute set because attributes are structurally subordinate to the table. The initial schema represents attribute type, description, and primary-key membership; AWS attribute validation rules and table tags remain deferred because their removal/update behavior is not reliably representable through the pinned SDK. Explicit default values are keyed by attribute name. AWS represents a stored default by returning `RecordId` `DEFAULT` when a value is created with `PrimaryValues` omitted; absence of a configured default means no stored default even if the console renders an implicit empty row. Non-default records remain separate resources and expose composite primary values as a map whose API representation is sorted by attribute name.

## Planned discovery contract

The proposed data source is plural rather than a replacement for the existing singular AWS provider data source:

```text
Amazon Connect instance
        |
        | ListQuickConnects, paginate NextToken
        v
quick-connect summaries
  - ID
  - ARN, when returned
  - name, when returned
  - type, when returned
  - last-modified metadata, when returned
```

Verified API behavior:

- `instance_id` accepts an Amazon Connect instance ID or ARN and is required.
- `quick_connect_types` accepts at most four values from `USER`, `QUEUE`, `PHONE_NUMBER`, and `FLOW`.
- `max_results` controls page size, not the total logical result count; the API defaults to 100 and permits 1 through 1000.
- `NextToken` indicates more pages.
- `ListQuickConnects` returns summary objects, not descriptions or nested quick-connect configuration.
- results are exposed as a list sorted lexicographically by quick-connect ID for deterministic Terraform state;
- `quick_connect_types` is passed to `ListQuickConnects` as a server-side filter;
- optional exact `name` filtering is applied client-side after every API page is collected because `ListQuickConnects` has no name parameter;
- `SearchQuickConnects` is outside the initial contract.

The schema follows current Terraform conventions:

- `instance_id`: required string;
- `name`: optional string for exact client-side matching;
- `quick_connect_types`: optional set of validated enum strings because filters are unordered and unique;
- `quick_connects`: computed list nested attribute, sorted by item `id`;
- quick-connect item `id`: required computed string;
- ARN, name, type, last-modified region, and last-modified time: computed when returned by `ListQuickConnects`.

Do not add a synthetic data-source-level `id`: the query has no separate remote identity, and modern Plugin Framework testing does not require one. The Terraform data-source address identifies the configured query while each returned object exposes its AWS quick-connect ID.

## Provider precedents

Use [`terraform-provider-aws`](https://github.com/hashicorp/terraform-provider-aws) as the primary precedent for AWS SDK configuration, service packages, diagnostics, and acceptance testing. New AWS provider resources and data sources are expected to use AWS SDK for Go v2 and Terraform Plugin Framework.

Use [`terraform-provider-awscc`](https://github.com/hashicorp/terraform-provider-awscc) only as secondary evidence for a large Plugin Framework provider. Its Cloud Control schema generation model does not match this provider's direct, service-specific APIs.

Do not reproduce either provider's scale-specific abstractions until this repository has evidence for them.

## Source references

- [Terraform Plugin Framework overview](https://developer.hashicorp.com/terraform/plugin/framework)
- [Framework provider configuration](https://developer.hashicorp.com/terraform/plugin/framework/providers)
- [Configuring data sources](https://developer.hashicorp.com/terraform/plugin/framework/data-sources/configure)
- [Framework acceptance tests](https://developer.hashicorp.com/terraform/plugin/framework/acctests)
- [Amazon Connect `ListQuickConnects`](https://docs.aws.amazon.com/connect/latest/APIReference/API_ListQuickConnects.html)
- [Amazon Connect `SearchQuickConnects`](https://docs.aws.amazon.com/connect/latest/APIReference/API_SearchQuickConnects.html)
- [Existing singular `aws_connect_quick_connect` data source](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/data-sources/connect_quick_connect)
- [Terraform protocol 6 compatibility](https://developer.hashicorp.com/terraform/plugin/framework/provider-servers)
- [Framework attribute and collection semantics](https://developer.hashicorp.com/terraform/plugin/framework/handling-data/attributes)
- [AWS Cloud Control provider documentation](https://registry.terraform.io/providers/hashicorp/awscc/latest/docs)

## Verification limitations

- No stable Amazon Connect fixture exists.
- Provider behavior is verified with fake AWS clients, Framework tests, race testing, build, lint, and generated-document checks.
- A real-AWS smoke test requires separate authorization and must be reported as supplementary evidence.
