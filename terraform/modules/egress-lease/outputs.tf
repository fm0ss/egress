output "lease" {
  description = "Full lease JSON object"
  value       = data.external.lease.result
}

output "lease_id" {
  description = "Provisioned lease id"
  value       = try(data.external.lease.result.id, null)
}

output "public_ip" {
  description = "Provisioned public IP"
  value       = try(data.external.lease.result.public_ip, null)
}

output "endpoint" {
  description = "Client endpoint"
  value       = try(data.external.lease.result.endpoint, null)
}
