variable "name" {
  type        = string
  description = "Logical name for this lease"
}

variable "aws_profile_name" {
  type        = string
  description = "AWS CLI profile name to import and use"
}

variable "account_name" {
  type        = string
  description = "Saved egress account name. Defaults to aws_profile_name"
  default     = ""
}

variable "location" {
  type        = string
  description = "Location id or region"
}

variable "access_mode" {
  type        = string
  description = "proxy or vpn"
  default     = "proxy"
}

variable "workload_id" {
  type        = string
  description = "Optional workload id stored in egress state"
  default     = ""
}

variable "root_dir" {
  type        = string
  description = "Absolute or relative path to the egress repo checkout"
}

variable "state_path" {
  type        = string
  description = "State file path used by the egress CLI"
  default     = ".egress/state.json"
}

variable "artifact_dir" {
  type        = string
  description = "Directory where the provisioned lease JSON will be written"
  default     = ".egress/terraform"
}
