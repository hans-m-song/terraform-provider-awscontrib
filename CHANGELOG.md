## 0.1.0 (Unreleased)

FEATURES:

* **Provider:** add the `hans-m-song/awscontrib` provider with AWS SDK for Go v2 configuration and optional profile and region settings.
* **Resource:** add `awscontrib_connect_queue_quick_connect_association` for managing one Amazon Connect queue/quick-connect association edge.

VERIFICATION:

* Automated verification uses deterministic mocked and Terraform Plugin Framework tests. No stable Amazon Connect fixture is available, so real-AWS acceptance tests were not run.
