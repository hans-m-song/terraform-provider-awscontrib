# Terraform Provider AWS Contributions

`terraform-provider-awscontrib` provides focused AWS capabilities that are not available in the HashiCorp AWS or AWS Cloud Control providers.

The provider is intended for publication at `hans-m-song/awscontrib` and uses the Terraform Plugin Framework with AWS SDK for Go v2.

## Requirements

- Terraform CLI 1.0 or later
- Go 1.26 or later for development

## Provider configuration

The provider uses the standard AWS SDK configuration chain, including shared AWS configuration and credentials files. `profile` and `region` can be selected explicitly:

```hcl
terraform {
  required_providers {
    awscontrib = {
      source = "hans-m-song/awscontrib"
    }
  }
}

provider "awscontrib" {
  profile = "example"
  region  = "ap-southeast-2"
}
```

Do not place credentials in Terraform configuration. Configure credentials through supported AWS SDK mechanisms.

## Resources

- [`awscontrib_connect_queue_quick_connect_associations`](docs/resources/connect_queue_quick_connect_associations.md)
- [`awscontrib_connect_hours_of_operation_override`](docs/resources/connect_hours_of_operation_override.md)
- [`awscontrib_connect_data_table`](docs/resources/connect_data_table.md)
- [`awscontrib_connect_data_table_record`](docs/resources/connect_data_table_record.md)

## Data sources

- [`awscontrib_connect_phone_number`](docs/data-sources/connect_phone_number.md)
- [`awscontrib_connect_contact_flow_module`](docs/data-sources/connect_contact_flow_module.md)

## Development

```shell
make build
make test
make lint
make generate
```

`make testacc` is reserved for explicitly authorized tests against real AWS infrastructure. This repository does not currently maintain real Amazon Connect fixtures.

See [docs/overview.md](docs/overview.md), [docs/structure.md](docs/structure.md), and [docs/roadmap.md](docs/roadmap.md) for architecture and planning.
