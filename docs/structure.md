# Repository structure

## Current layout

```text
.
├── main.go                         provider server entry point
├── internal/
│   ├── provider/                   provider schema, configuration, registration
│   ├── conns/                      AWS SDK configuration and client factory
│   └── service/connect/            association resource, coordinator, tests
├── examples/                       tfplugindocs example inputs
├── docs/                           generated references plus project docs
├── tools/                          separate module for generation tools
├── .github/workflows/              fixture-free CI and signed release automation
├── GNUmakefile                     build, test, lint, and generation commands
├── go.mod                          provider module
└── terraform-registry-manifest.json
```

The implemented packages follow the provider, connection, and service boundaries below. The plural discovery files shown in the target layout remain planned rather than implemented.

## Implemented and planned layout

The association paths are implemented; paths marked `planned M2` are not:

```text
.
├── main.go
├── internal/
│   ├── provider/
│   │   ├── provider.go             metadata, schema, configuration
│   │   ├── data_sources.go         explicit data-source registry
│   │   └── provider_test.go
│   ├── conns/
│   │   ├── config.go               AWS SDK v2 configuration
│   │   └── client.go               service client factory
│   └── service/
│       └── connect/
│           ├── client.go           narrow Connect interfaces
│           ├── coordinator.go      provider-local queue-keyed mutation locks
│           ├── queue_quick_connect_association_resource.go
│           ├── queue_quick_connect_association_resource_test.go
│           ├── quick_connects_data_source.go       planned M2
│           └── quick_connects_data_source_test.go  planned M2
├── examples/
│   ├── provider/provider.tf
│   ├── data-sources/
│   │   └── awscontrib_connect_quick_connects/data-source.tf  planned M2
│   └── resources/
│       └── awscontrib_connect_queue_quick_connect_association/
├── docs/
│   ├── overview.md                 maintained project documentation
│   ├── structure.md
│   ├── roadmap.md
│   └── data-sources/               generated reference documentation
└── .agents/
    ├── log.md                      durable decisions and lessons
    └── handoff.md                  session continuity
```

Provider and association resource names are implemented. Only the milestone `M2` discovery paths remain provisional until that implementation begins.

## Dependency boundaries

```text
Terraform CLI
     |
     v
main.go
     |
     v
internal/provider -------> internal/conns
     |                           |
     v                           |
internal/service/connect <-------+
     |
     v
Amazon Connect API
```

| Module | Owns | Must not own |
|---|---|---|
| `main` | server startup and build version | provider schema or AWS behavior |
| `provider` | provider identity, configuration, constructor registration | Amazon Connect mapping logic |
| `conns` | AWS SDK configuration and service client construction | Terraform schemas or service-specific CRUD/read logic |
| `service/connect` | schemas, API requests, pagination, mapping, diagnostics, tests | provider-wide configuration or other AWS services |
| `examples` | executable practitioner configurations used by documentation generation | generated prose |
| generated `docs/*` reference directories | generated provider reference output | hand-maintained design decisions |

Service packages must not import `internal/provider` or sibling services. Shared packages require more than one demonstrated consumer.

## Data-source test boundary

Define the smallest interface needed by the data source, structurally equivalent to:

```text
ListQuickConnects(context, input, options...) -> output, error
```

Use Framework nested attributes rather than nested blocks for the new schema. Model `quick_connect_types` as an optional set attribute and `quick_connects` as a computed list nested attribute.

Unit tests should supply a fake implementation and cover:

- one page and multiple pages;
- absent and present `NextToken`;
- no results;
- each supported quick-connect type and propagation of the API filter;
- exact name filtering after all pages are collected;
- lexicographic ordering by quick-connect ID regardless of API response order;
- AWS errors converted to actionable diagnostics.

No stable real-AWS fixtures are available. Mocked unit and framework tests are therefore the mandatory verification boundary. Any manual or automated real-AWS test requires separate authorization, must remain optional and gated by `TF_ACC`, and must be reported as unavailable rather than silently skipped when assessing release risk.

## Generated documentation boundary

`make generate` performs two operations:

1. formats Terraform examples;
2. invokes `tfplugindocs`.

Consequently, contributors edit Go schema descriptions and files under `examples/`, then commit the generated reference output. `docs/overview.md`, `docs/structure.md`, and `docs/roadmap.md` are maintained manually and must not contain the generated-file marker.

Schema export uses the Terraform CLI version pinned in `tools/tools.go`; it must not silently inherit an arbitrary PATH version. The release workflow reruns generation and requires a clean diff before signing begins.
