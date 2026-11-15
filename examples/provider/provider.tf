terraform {
  required_providers {
    uptimecheck = {
      source = "uptimecheck-com/uptimecheck"
    }
  }
}

# The API key comes from UPTIMECHECK_API_KEY (Settings › API in the app, read & write).
provider "uptimecheck" {}
