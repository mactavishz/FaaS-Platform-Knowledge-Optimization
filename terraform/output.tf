output "public_ip" {
  value = google_compute_instance.bench_vm.network_interface.0.access_config.0.nat_ip
}

output "deployed_faas_platform" {
  value = var.faas_platform
}

output "instance_name" {
  value = google_compute_instance.bench_vm.name
}

output "zone" {
  value = var.zone
}

output "gateway_url" {
  value = "http://${google_compute_instance.bench_vm.network_interface.0.access_config.0.nat_ip}:${var.gateway_port}"
}

output "ssh_command" {
  value = "ssh -i <private-key-path> ${var.ssh_user}@${google_compute_instance.bench_vm.network_interface.0.access_config.0.nat_ip}"
}

output "faasd_auth_user" {
  value = var.faas_platform == "faasd" ? local.faasd_auth_user : null
}

output "faasd_auth_password" {
  value     = var.faas_platform == "faasd" ? local.faasd_auth_password : null
  sensitive = true
}
