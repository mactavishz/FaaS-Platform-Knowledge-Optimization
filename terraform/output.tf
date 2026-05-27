output "public_ips" {
  value = {
    for platform, address in google_compute_address.ip_address :
    platform => address.address
  }
}

output "deployed_faas_platforms" {
  value = {
    for platform in local.platforms :
    platform => platform
  }
}

output "instance_names" {
  value = {
    for platform, instance in google_compute_instance.bench_vm :
    platform => instance.name
  }
}

output "zones" {
  value = {
    for platform in local.platforms :
    platform => var.zone
  }
}

output "gateway_urls" {
  value = {
    for platform, address in google_compute_address.ip_address :
    platform => "http://${address.address}:${var.gateway_port}"
  }
}

output "ssh_commands" {
  value = {
    for platform, address in google_compute_address.ip_address :
    platform => "ssh -i <private-key-path> ${var.ssh_user}@${address.address}"
  }
}

output "faasd_auth_users" {
  value = {
    for platform in local.platforms :
    platform => platform == "faasd" ? local.faasd_auth_user[platform] : null
  }
}

output "faasd_auth_passwords" {
  value = {
    for platform in local.platforms :
    platform => platform == "faasd" ? local.faasd_auth_password[platform] : null
  }
  sensitive = true
}
