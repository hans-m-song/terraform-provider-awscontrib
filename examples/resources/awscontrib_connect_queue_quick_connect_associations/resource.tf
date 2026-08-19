resource "awscontrib_connect_queue_quick_connect_associations" "example" {
  instance_id = "aaaaaaaa-bbbb-cccc-dddd-111111111111"
  queue_id    = "aaaaaaaa-bbbb-cccc-dddd-222222222222"

  quick_connect_ids = toset([
    "aaaaaaaa-bbbb-cccc-dddd-333333333333",
    "aaaaaaaa-bbbb-cccc-dddd-444444444444",
  ])
}
