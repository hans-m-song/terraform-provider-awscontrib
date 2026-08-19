variable "connect_instance_id" {
  type        = string
  description = "Amazon Connect instance identifier or ARN."
}

variable "connect_phone_number" {
  type        = string
  description = "Full E.164 phone number to look up."
}

data "awscontrib_connect_phone_number" "example" {
  instance_id  = var.connect_instance_id
  phone_number = var.connect_phone_number
}
