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

### `awscontrib_connect_queue_quick_connect_association`

Manages one association between an Amazon Connect queue and quick connect:

```hcl
resource "awscontrib_connect_queue_quick_connect_association" "example" {
  instance_id      = aws_connect_instance.example.id
  queue_id         = aws_connect_queue.example.queue_id
  quick_connect_id = aws_connect_quick_connect.example.quick_connect_id
}
```

The resource uses Amazon Connect's dedicated associate, list, and disassociate APIs. Mutations targeting the same instance and queue are serialized within one provider process. AWS does not document cross-process serialization, so one association edge must not be managed by multiple Terraform states.

Import uses the instance ID, queue ID, and quick-connect ID separated by commas:

```shell
terraform import awscontrib_connect_queue_quick_connect_association.example 'instance-id,queue-id,quick-connect-id'
```

## Development

```shell
make build
make test
make lint
make generate
```

`make testacc` is reserved for explicitly authorized tests against real AWS infrastructure. This repository does not currently maintain real Amazon Connect fixtures.

See [docs/overview.md](docs/overview.md), [docs/structure.md](docs/structure.md), and [docs/roadmap.md](docs/roadmap.md) for architecture and planning.
