data "google_client_config" "current" {
}

data "google_compute_image" "ubuntu" {
  project = "ubuntu-os-cloud"
  family  = "ubuntu-2404-lts-amd64"
}

locals {
  platforms   = toset(var.faas_platforms)
  env_content = var.env_file == "" ? "" : file(pathexpand(var.env_file))

  gateway_health_endpoint = {
    faasd    = "system/functions"
    tinyfaas = "system/list"
  }

  faasd_auth_user = {
    for platform in local.platforms :
    platform => platform == "faasd" ? var.faasd_auth_user : ""
  }

  faasd_auth_password = {
    for platform in local.platforms :
    platform => platform == "faasd" ? coalesce(var.faasd_auth_password, try(random_password.faasd_auth_password["faasd"].result, "")) : ""
  }

  startup_template_vars = {
    for platform in local.platforms :
    platform => {
      github_token_b64        = base64encode(var.github_token)
      repo_url_b64            = base64encode(var.repo_url)
      repo_ref_b64            = base64encode(var.repo_ref)
      env_content_b64         = base64encode(local.env_content)
      faasd_auth_user_b64     = base64encode(local.faasd_auth_user[platform])
      faasd_auth_password_b64 = base64encode(local.faasd_auth_password[platform])
      gateway_port            = tostring(var.gateway_port)
      ssh_user                = var.ssh_user
      deploy_dir              = var.deploy_dir
      go_version              = var.go_version
      nerdctl_version         = var.nerdctl_version
      buildkit_version        = var.buildkit_version
      containerd_version      = var.containerd_version
      cni_plugin_version      = var.cni_plugin_version
    }
  }

  iam_roles = toset(["roles/logging.logWriter", "roles/monitoring.metricWriter"])
  iam_members = {
    for pair in setproduct(local.platforms, local.iam_roles) :
    "${pair[0]}-${replace(pair[1], "/", "-")}" => {
      platform = pair[0]
      role     = pair[1]
    }
  }
}

resource "random_string" "vm_id" {
  for_each = local.platforms

  length  = 6
  special = false
  upper   = false
}

resource "random_password" "faasd_auth_password" {
  for_each = toset(contains(var.faas_platforms, "faasd") ? ["faasd"] : [])

  length  = 32
  special = false
}

resource "google_service_account" "bench_sa" {
  for_each = local.platforms

  project      = data.google_client_config.current.project
  account_id   = "faas-bench-${each.key}-sa-${random_string.vm_id[each.key].result}"
  display_name = "Service Account for ${each.key} Benchmark VM Instance"
}

resource "google_project_iam_member" "monitoring" {
  project  = data.google_client_config.current.project
  for_each = local.iam_members

  role   = each.value.role
  member = format("serviceAccount:%s", google_service_account.bench_sa[each.value.platform].email)
}

resource "google_compute_address" "ip_address" {
  for_each = local.platforms

  project = data.google_client_config.current.project
  name    = "faas-bench-${each.key}-ip-${random_string.vm_id[each.key].result}"
  region  = var.region
}

resource "google_compute_instance" "bench_vm" {
  for_each = local.platforms

  project      = data.google_client_config.current.project
  name         = "faas-bench-${each.key}-${random_string.vm_id[each.key].result}"
  machine_type = var.machine_type
  zone         = var.zone
  tags         = ["ssh", "http", "benchmark", each.key]

  boot_disk {
    initialize_params {
      image = data.google_compute_image.ubuntu.self_link
      size  = 50
    }
  }

  network_interface {
    network = var.network
    access_config {
      nat_ip = google_compute_address.ip_address[each.key].address
    }
  }

  metadata = {
    ssh-keys = "${var.ssh_user}:${trimspace(file(pathexpand(var.ssh_pubkey)))}"
  }

  metadata_startup_script = each.key == "faasd" ? templatefile("${path.module}/scripts/provision_faasd_tpl.sh", local.startup_template_vars[each.key]) : templatefile("${path.module}/scripts/provision_tinyfaas_tpl.sh", local.startup_template_vars[each.key])

  service_account {
    email  = google_service_account.bench_sa[each.key].email
    scopes = ["cloud-platform"]
  }
}

resource "google_compute_firewall" "ssh" {
  for_each = local.platforms

  project = data.google_client_config.current.project
  name    = "${google_compute_instance.bench_vm[each.key].name}-ssh-access"
  network = var.network

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  target_tags   = [each.key]
  source_ranges = ["0.0.0.0/0"]
}

resource "google_compute_firewall" "http" {
  for_each = local.platforms

  project = data.google_client_config.current.project
  name    = "${google_compute_instance.bench_vm[each.key].name}-http-access"
  network = var.network

  allow {
    protocol = "tcp"
    ports    = [var.gateway_port]
  }

  target_tags   = [each.key]
  source_ranges = ["0.0.0.0/0"]
}

resource "terracurl_request" "gateway_health_check" {
  for_each = local.platforms

  name   = "${each.key}-gateway-health-check"
  url    = "http://${google_compute_address.ip_address[each.key].address}:${var.gateway_port}/${local.gateway_health_endpoint[each.key]}"
  method = "GET"

  response_codes = each.key == "faasd" ? [200, 401, 403] : [200]
  max_retry      = var.gateway_health_max_retry
  retry_interval = var.gateway_health_retry_interval

  depends_on = [
    google_compute_firewall.http,
    google_compute_instance.bench_vm,
  ]
}
