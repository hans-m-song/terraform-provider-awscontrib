## 0.1.0 (Unreleased)

FEATURES:

* **Provider:** add the `hans-m-song/awscontrib` provider with AWS SDK for Go v2 configuration and optional profile and region settings.
* **Resource:** add `awscontrib_connect_queue_quick_connect_associations` for managing an additive set of Amazon Connect queue/quick-connect associations, with 50-ID request batching and canonical multi-ID import.
* **Resource:** support in-place reconciliation of `quick_connect_ids` while preserving unrelated queue associations.
* **Resource:** add `awscontrib_connect_hours_of_operation_override` with CRUD, composite import, recurrence validation, and explicit removal semantics.
* **Resource:** add `awscontrib_connect_data_table` for authoritative table attributes and explicit DEFAULT values.
* **Resource:** add `awscontrib_connect_data_table_record` for authoritative records with composite primary keys.
* **Data Source:** add exact lookup of Amazon Connect phone numbers and contact-flow modules.

VERIFICATION:

* Automated verification uses deterministic mocked and Terraform Plugin Framework tests. No stable Amazon Connect fixture is available, so real-AWS acceptance tests were not run.
