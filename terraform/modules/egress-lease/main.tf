terraform {
  required_version = ">= 1.4.0"
  required_providers {
    null = {
      source  = "hashicorp/null"
      version = ">= 3.2.0"
    }
    external = {
      source  = "hashicorp/external"
      version = ">= 2.3.0"
    }
  }
}

locals {
  account_name = var.account_name != "" ? var.account_name : var.aws_profile_name
  workload_id  = var.workload_id != "" ? var.workload_id : "${var.name}-${var.location}"
  state_path   = abspath(var.state_path)
  lease_file   = abspath("${var.artifact_dir}/${var.name}-${var.location}-${var.access_mode}.json")
  root_dir     = abspath(var.root_dir)
}

resource "null_resource" "lease" {
  triggers = {
    name             = var.name
    aws_profile_name = var.aws_profile_name
    account_name     = local.account_name
    location         = var.location
    access_mode      = var.access_mode
    workload_id      = local.workload_id
    root_dir         = local.root_dir
    state_path       = local.state_path
    lease_file       = local.lease_file
  }

  provisioner "local-exec" {
    command = "${local.root_dir}/scripts/terraform-lease-up.sh"
    environment = {
      ROOT_DIR          = local.root_dir
      STATE_PATH        = local.state_path
      AWS_PROFILE_NAME  = var.aws_profile_name
      ACCOUNT_NAME      = local.account_name
      LOCATION          = var.location
      ACCESS_MODE       = var.access_mode
      WORKLOAD_ID       = local.workload_id
      LEASE_FILE        = local.lease_file
    }
  }

  provisioner "local-exec" {
    when    = destroy
    command = "${self.triggers.root_dir}/scripts/terraform-lease-down.sh"
    environment = {
      ROOT_DIR   = self.triggers.root_dir
      STATE_PATH = self.triggers.state_path
      LEASE_FILE = self.triggers.lease_file
    }
  }
}

data "external" "lease" {
  program    = ["python3", "-c", "import json,sys; print(json.dumps(json.load(open(sys.argv[1]))))", local.lease_file]
  depends_on = [null_resource.lease]
}
