data "google_client_config" "current" {
}

data "google_compute_image" "ubuntu" {
  project = "ubuntu-os-cloud"
  family  = "ubuntu-2404-lts-amd64"
}

locals {
  env_content             = var.env_file == "" ? "" : file(pathexpand(var.env_file))
  gateway_health_endpoint = var.faas_platform == "faasd" ? "system/functions" : "system/list"
  faasd_auth_user         = var.faas_platform == "faasd" ? var.faasd_auth_user : ""
  faasd_auth_password     = var.faas_platform == "faasd" ? coalesce(var.faasd_auth_password, one(random_password.faasd_auth_password[*].result)) : ""
  startup_template_vars = {
    github_token_b64        = base64encode(var.github_token)
    repo_url_b64            = base64encode(var.repo_url)
    repo_ref_b64            = base64encode(var.repo_ref)
    env_content_b64         = base64encode(local.env_content)
    faasd_auth_user_b64     = base64encode(local.faasd_auth_user)
    faasd_auth_password_b64 = base64encode(local.faasd_auth_password)
    gateway_port            = tostring(var.gateway_port)
    ssh_user                = var.ssh_user
    deploy_dir              = var.deploy_dir
    go_version              = var.go_version
    nerdctl_version         = var.nerdctl_version
    containerd_version      = var.containerd_version
    cni_plugin_version      = var.cni_plugin_version
  }
}

resource "random_password" "faasd_auth_password" {
  count = var.faas_platform == "faasd" && var.faasd_auth_password == null ? 1 : 0
  length  = 32
  special = true
}

resource "google_service_account" "bench_sa" {
  project      = data.google_client_config.current.project
  account_id   = "bench-vm-sa"
  display_name = "Benchmark SA for VM Instance"
}

# Grant necessary IAM roles to the service account for logging and monitoring (Ops Agent)
resource "google_project_iam_member" "monitoring" {
  project  = data.google_client_config.current.project
  for_each = toset(["roles/logging.logWriter", "roles/monitoring.metricWriter"])
  role     = each.value
  member   = format("serviceAccount:%s", google_service_account.bench_sa.email)
}

resource "google_compute_address" "ip_address" {
  project = data.google_client_config.current.project
  name    = "bench-vm-ip"
  region  = var.region
}

resource "google_compute_instance" "bench_vm" {
  project = data.google_client_config.current.project
  name = var.faas_platform == "faasd" ? "faasd-bench-vm" : "tinyfaas-bench-vm"
  # n2-standard-16
  machine_type = var.machine_type
  zone         = var.zone
  tags         = ["ssh", "http"]

  boot_disk {
    initialize_params {
      image = data.google_compute_image.ubuntu.self_link
      size  = 200
    }
  }

  network_interface {
    # A default network is created for all GCP projects
    network = var.network
    access_config {
      nat_ip = google_compute_address.ip_address.address
    }
  }

  metadata = {
    ssh-keys = "${var.ssh_user}:${trimspace(file(pathexpand(var.ssh_pubkey)))}"
  }

  metadata_startup_script = var.faas_platform == "faasd" ? templatefile("${path.module}/scripts/provision_faasd_tpl.sh", local.startup_template_vars) : templatefile("${path.module}/scripts/provision_tinyfaas_tpl.sh", local.startup_template_vars)

  service_account {
    # Google recommends custom service accounts that have cloud-platform scope and permissions granted via IAM Roles.
    email  = google_service_account.bench_sa.email
    scopes = ["cloud-platform"]
  }
}

resource "google_compute_firewall" "ssh" {
  name    = "ssh-access"
  network = var.network

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }

  target_tags   = ["ssh"]
  source_ranges = ["0.0.0.0/0"]
}

resource "google_compute_firewall" "http" {
  project = data.google_client_config.current.project
  name    = "http-access"
  network = var.network

  allow {
    protocol = "tcp"
    ports    = [var.gateway_port]
  }

  target_tags   = ["http"]
  source_ranges = ["0.0.0.0/0"]
}

resource "terracurl_request" "gateway_health_check" {
  name   = "gateway-health-check"
  url    = "http://${google_compute_address.ip_address.address}:${var.gateway_port}/${local.gateway_health_endpoint}"
  method = "GET"

  response_codes = var.faas_platform == "faasd" ? [200, 401, 403] : [200]
  max_retry      = var.gateway_health_max_retry
  retry_interval = var.gateway_health_retry_interval

  depends_on = [
    google_compute_firewall.http,
    google_compute_instance.bench_vm,
  ]
}
