variable "connect_instance_id" {
  type        = string
  description = "Amazon Connect instance identifier."
}

variable "data_table_name" {
  type        = string
  description = "Exact data-table name to look up."
}

data "awscontrib_connect_data_table" "by_name" {
  instance_id = var.connect_instance_id
  name        = var.data_table_name
}

data "awscontrib_connect_data_table" "by_id" {
  instance_id   = var.connect_instance_id
  data_table_id = data.awscontrib_connect_data_table.by_name.id
}
