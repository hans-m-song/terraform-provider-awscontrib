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

  default_tags {
    tags = {
      Environment = "production"
      ManagedBy   = "Terraform"
    }
  }

  ignore_tags {
    keys         = ["ExternalOwner"]
    key_prefixes = ["external:"]
  }
}
