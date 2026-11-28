variable "connect_instance_id" {
  type        = string
  description = "Amazon Connect instance identifier."
}

variable "routing_profile_name_contains" {
  type        = string
  description = "Case-sensitive substring used to filter routing profile names."
}

variable "routing_profile_tags" {
  type        = map(string)
  description = "Tag key/value pairs that routing profiles must all match."
  default     = {}
}

data "awscontrib_connect_routing_profiles" "example" {
  instance_id   = var.connect_instance_id
  name_contains = var.routing_profile_name_contains
  tags          = var.routing_profile_tags
}
