variable "connect_instance_id" {
  type        = string
  description = "Amazon Connect instance identifier."
}

variable "contact_flow_module_name" {
  type        = string
  description = "Exact contact flow module name to look up."
}

data "awscontrib_connect_contact_flow_module" "example" {
  instance_id = var.connect_instance_id
  name        = var.contact_flow_module_name
}
