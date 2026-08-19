resource "awscontrib_connect_data_table" "example" {
  instance_id      = "aaaaaaaa-bbbb-cccc-dddd-111111111111"
  name             = "customer routing"
  description      = "Customer routing attributes"
  time_zone        = "Australia/Brisbane"
  value_lock_level = "VALUE"
  status           = "PUBLISHED"

  attributes = {
    customer_id = {
      value_type = "TEXT"
      primary    = true
    }

    channel = {
      value_type  = "TEXT"
      description = "Preferred contact channel"
      primary     = true
    }

    routing_group = {
      value_type  = "TEXT"
      description = "Default routing group"
    }
  }

  default_values = {
    routing_group = "general"
  }
}
