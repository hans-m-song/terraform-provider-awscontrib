resource "awscontrib_connect_data_table_record" "example" {
  instance_id   = "aaaaaaaa-bbbb-cccc-dddd-111111111111"
  data_table_id = awscontrib_connect_data_table.example.id

  primary_values = {
    customer_id = "customer-123"
    channel     = "voice"
  }

  values = {
    routing_group = "priority"
  }
}
