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

  # Disable automatic box update checking. If you disable this, then
  # boxes will only be checked for updates when the user runs
  # `vagrant box outdated`. This is not recommended.
  # config.vm.box_check_update = false

  # Forward ports for OpenFaaS gateway
  config.vm.network "forwarded_port", guest: 8080, host: 8080, host_ip: "127.0.0.1"
  
  # Use private network to get a consistent IP
  config.vm.network "private_network", type: "dhcp"
  #
  # View the documentation for the provider you are using for more
  # information on available options.

  # Install faasd dependencies
  config.vm.provision "shell", name: "install-dependencies", path: "scripts/install-dependencies.sh"
end
