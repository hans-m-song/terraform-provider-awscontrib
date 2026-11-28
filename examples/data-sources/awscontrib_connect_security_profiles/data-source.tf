variable "connect_instance_id" {
  type        = string
  description = "Amazon Connect instance identifier."
}

variable "security_profile_name_contains" {
  type        = string
  description = "Case-sensitive substring used to filter security profile names."
}

variable "security_profile_tags" {
  type        = map(string)
  description = "Tag key/value pairs that security profiles must all match."
  default     = {}
}

data "awscontrib_connect_security_profiles" "example" {
  instance_id   = var.connect_instance_id
  name_contains = var.security_profile_name_contains
  tags          = var.security_profile_tags
}
