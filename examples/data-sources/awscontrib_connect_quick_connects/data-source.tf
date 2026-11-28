variable "connect_instance_id" {
  type        = string
  description = "Amazon Connect instance identifier."
}

variable "quick_connect_name_contains" {
  type        = string
  description = "Case-sensitive literal substring matched against quick-connect names."
  default     = "Support"
}

variable "quick_connect_tags" {
  type        = map(string)
  description = "Exact tag key-value pairs that each quick connect must match."
  default = {
    environment = "example"
  }
}

data "awscontrib_connect_quick_connects" "example" {
  instance_id         = var.connect_instance_id
  name_contains       = var.quick_connect_name_contains
  quick_connect_types = ["QUEUE", "PHONE_NUMBER"]
  tags                = var.quick_connect_tags
}

output "quick_connect_ids" {
  value = [for quick_connect in data.awscontrib_connect_quick_connects.example.quick_connects : quick_connect.id]
}
