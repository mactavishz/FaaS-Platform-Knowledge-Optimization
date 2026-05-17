terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 7.32.0"
    }
    # Logical provider
    # Supports the use of randomness within Terraform configurations
    random = {
      source  = "hashicorp/random"
      version = "~> 3.9.0"
    }
    terracurl = {
      source  = "devops-rob/terracurl"
      version = "~> 2.3.0"
    }
  }
}


provider "google" {
  # ensure the environment variable GOOGLE_CREDENTIALS is set with 
  # your service account json
}

provider "terracurl" {}
