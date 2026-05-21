variable "region" {
  description = "Region to deploy the resources"
  default     = "europe-west3"
}

variable "zone" {
  description = "Zone in the region to deploy the resources"
  default     = "europe-west3-a"
}

variable "network" {
  description = "The name of the VPC Network where all resources should be created."
  type        = string
  default     = "default"
}

variable "ssh_pubkey" {
  description = "Absolute Path to the ssh public key for the vm"
  default     = "~/.ssh/id_ed25519.pub"
}

variable "ssh_user" {
  description = "User to create on the VM for SSH access"
  default     = "bench"
}

variable "machine_type" {
  description = "Machine type of the VM to create"
  default     = "n2-standard-4"
}

variable "deploy_dir" {
  description = "Directory on the VM to clone the repository and run the benchmarks from"
  default     = "/opt/faas-platform"
}

variable "gateway_port" {
  description = "Port for the gateway to listen on"
  default     = 8080
}

variable "gateway_health_max_retry" {
  description = "Maximum number of gateway health-check retries after VM creation"
  default     = 60
}

variable "gateway_health_retry_interval" {
  description = "Seconds to wait between gateway health-check retries"
  default     = 10
}

variable "faas_platform" {
  description = "FaaS platform to benchmark (faasd or tinyfaas)"
  default     = "tinyfaas"

  validation {
    condition     = contains(["faasd", "tinyfaas"], var.faas_platform)
    error_message = "faas_platform must be either \"faasd\" or \"tinyfaas\"."
  }
}

variable "faasd_auth_user" {
  description = "faasd basic-auth username"
  default     = "admin"
}

variable "faasd_auth_password" {
  description = "Optional faasd basic-auth password. If unset, Terraform generates one."
  type        = string
  default     = null
  sensitive   = true
}

variable "github_token" {
  description = "GitHub token used by the VM startup script to clone the benchmark repository and its submodules"
  type        = string
  sensitive   = true
}

variable "repo_url" {
  description = "HTTPS repository URL to clone on the benchmark VM"
  default     = "https://github.com/mactavishz/FaaS-Platform-Knowledge-Optimization.git"
}

variable "repo_ref" {
  description = "Git branch, tag, or commit to checkout after cloning"
  default     = "main"
}

variable "env_file" {
  description = "Local path to the platform .env file to install on the VM. Leave empty to create only Terraform-managed defaults."
  default     = ""
  sensitive   = true
}

variable "go_version" {
  description = "Go version to install on the VM"
  default     = "1.26.3"
}

variable "nerdctl_version" {
  description = "nerdctl version to install on the VM"
  default     = "2.2.2"
}

variable "containerd_version" {
  description = "containerd version to install for faasd"
  default     = "2.3.0"
}

variable "cni_plugin_version" {
  description = "CNI plugins version to install for faasd"
  default     = "1.9.1"
}
