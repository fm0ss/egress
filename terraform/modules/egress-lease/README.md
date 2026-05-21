# egress-lease Terraform Module

This module wraps the `egress` CLI to provision and destroy a location-aware lease from Terraform.

It is intended for teams that already run Terraform from a machine or CI environment that has:

- this repository checked out
- Go installed
- AWS CLI installed and authenticated

## Example

```hcl
module "eu_test_egress" {
  source           = "../../terraform/modules/egress-lease"
  name             = "eu-test"
  root_dir         = "${path.root}/../.."
  aws_profile_name = "terraform-playground"
  account_name     = "ci-aws"
  location         = "eu-west-1"
  access_mode      = "proxy"
  workload_id      = "terraform-${terraform.workspace}"
}

output "eu_test_endpoint" {
  value = module.eu_test_egress.endpoint
}
```

## Notes

- create: imports the selected AWS CLI profile into egress state, then provisions a lease
- destroy: destroys the recorded lease by id
- the module writes the provisioned lease JSON into `artifact_dir`

This is a CLI-backed module, not a native Terraform provider. That makes it easy to distribute now, but it still depends on local execution.
