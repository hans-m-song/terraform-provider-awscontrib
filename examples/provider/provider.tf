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
