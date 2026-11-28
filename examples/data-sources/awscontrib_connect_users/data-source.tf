variable "connect_instance_id" {
  type        = string
  description = "Amazon Connect instance identifier."
}

variable "username_contains" {
  type        = string
  description = "Optional case-sensitive username substring."
  default     = null
}

variable "first_name_contains" {
  type        = string
  description = "Optional case-sensitive first-name substring."
  default     = null
}

variable "last_name_contains" {
  type        = string
  description = "Optional case-sensitive last-name substring."
  default     = null
}

variable "user_tags" {
  type        = map(string)
  description = "Optional exact tag pairs that every returned user must match."
  default     = {}
}

data "awscontrib_connect_users" "example" {
  instance_id         = var.connect_instance_id
  username_contains   = var.username_contains
  first_name_contains = var.first_name_contains
  last_name_contains  = var.last_name_contains
  tags                = var.user_tags
}
