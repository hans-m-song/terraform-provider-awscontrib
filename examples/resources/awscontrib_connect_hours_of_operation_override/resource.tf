resource "awscontrib_connect_hours_of_operation_override" "example" {
  instance_id           = "aaaaaaaa-bbbb-cccc-dddd-111111111111"
  hours_of_operation_id = "aaaaaaaa-bbbb-cccc-dddd-222222222222"

  name           = "holiday closure"
  description    = "Closed for a public holiday"
  effective_from = "2026-12-24"
  effective_till = "2026-12-26"
  override_type  = "CLOSED"

  recurrence = {
    frequency    = "MONTHLY"
    interval     = 1
    by_month_day = 25
  }
}
