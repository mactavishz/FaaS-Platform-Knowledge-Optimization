# All Vagrant configuration is done below. The "2" in Vagrant.configure
# configures the configuration version (we support older styles for
# backwards compatibility). Please don't change it unless you know what
# you're doing.
Vagrant.configure("2") do |config|
  # For a complete reference, please see the online documentation at
  # https://docs.vagrantup.com.

  config.vm.box = "bento/ubuntu-24.04"
  config.vm.box_version = "202510.26.0"
  # Allocate sufficient resources
  config.vm.provider "virtualbox" do |vb|
    vb.memory = "4096"
    vb.cpus = 2
  end
  
  # Common provisioning script for all VMs
  config.vm.provision "shell", name: "common", path: "scripts/install-common-dependencies.sh"

  # Disable automatic box update checking. If you disable this, then
  # boxes will only be checked for updates when the user runs
  # `vagrant box outdated`. This is not recommended.
  # config.vm.box_check_update = false
  
  config.vm.define "faasd" do |faasd|
    faasd.vm.hostname = "faasd"
    # Forward ports for faasd gateway
    faasd.vm.network "forwarded_port", guest: 8080, host: 8080, host_ip: "127.0.0.1"
    # Use private network to get a consistent IP
    faasd.vm.network "private_network", type: "dhcp"

    # Install faasd dependencies
    faasd.vm.provision "shell", name: "install-faasd-dependencies", path: "scripts/install-faasd-dependencies.sh"
    
    # Build and install faasd
    faasd.vm.provision "shell", name: "build", path: "scripts/build-faasd.sh", run: "never", privileged: false
  end

  config.vm.define "tinyfaas" do |tinyfaas|
    # Forward ports for faasd gateway
    tinyfaas.vm.network "forwarded_port", guest: 8000, host: 9090, host_ip: "127.0.0.1"
    tinyfaas.vm.network "forwarded_port", guest: 8080, host: 9091, host_ip: "127.0.0.1"
    tinyfaas.vm.network "forwarded_port", guest: 80, host: 8888, host_ip: "127.0.0.1"
    # Use private network to get a consistent IP
    tinyfaas.vm.network "private_network", type: "dhcp"

    # Install tinyfaas dependencies
    tinyfaas.vm.provision "shell", name: "install-tinyfaas-dependencies", path: "scripts/install-tinyfaas-dependencies.sh"

    # Build and install tinyfaas
    tinyfaas.vm.provision "shell", name: "build", path: "scripts/build-tinyfaas.sh", run: "never", privileged: false
  end
end
