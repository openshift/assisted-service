# Boot Discovery ISO over iPXE from GRUB

Following this doc you will be able to boot the Discovery ISO over iPXE from GRUB.

## Get PXE files from ISO and run a web server

First we need to setup a web server with the required files for PXE booting the discovery image.

1. On the web server create a temporary dir

    ~~~sh
    export IPXE_DIR=/tmp/ipxe/ai
    mkdir -p ${IPXE_DIR}
    ~~~
2. Run this container image for extracting the required files for PXE booting from the discovery image iso file

    ~~~sh
    # Container code: https://github.com/ohadlevy/ai-ipxe
    podman run -e BASE_URL=http://devscripts2ipv6.e2e.bos.redhat.com:8080/ipxe/ -e ISO_URL=<REPLACE WITH ISO URL> -v /tmp/ipxe/ai:/data:Z --net=host -it --rm quay.io/ohadlevy/ai-ipxe:latest
    ~~~
3. Run an nginx container for exposing the required files over http

    ~~~sh
    podman run -v ${IPXE_DIR}:/app:ro,Z -p 8080:8080 -d --rm bitnami/nginx:latest
    ~~~
4. Make sure the web server is working

    ~~~sh
    curl http://devscripts2ipv6.e2e.bos.redhat.com:8080/ipxe/ipxe
    ~~~

    ~~~sh
    #!ipxe                                                                                                                                                                                    
    set live_url http://devscripts2ipv6.e2e.bos.redhat.com:8080/ipxe/
    kernel ${live_url}/vmlinuz ignition.config.url=${live_url}/config.ign coreos.live.rootfs_url=${live_url}/rootfs.img random.trust_cpu=on rd.luks.options=discard ignition.firstboot ignition.platform.id=metal console=tty0 console=ttyS0,115200n8 coreos.inst.persistent-kargs="console=tty0 console=ttyS1,115200n8"
    initrd ${live_url}/initrd.img
    boot   
    ~~~

## Configure GRUB to boot from the iPXE server

Now that we have the PXE files available over http, we are going to configure GRUB for booting from that server

1. Login into the server you want to pxe boot as root
2. Get the iPXE boot module

    ~~~sh
    curl http://boot.ipxe.org/ipxe.lkrn -o /boot/ipxe.lkrn
    ~~~
3. Create an iPXE script for running the DHCP request and for chainload the PXE files from the server configured previously

    **DHCP Example**
    ~~~sh
    cat <<EOF > /boot/ai.ipxe
    #!ipxe

    dhcp
    chain http://webserver.example.com:8080/ipxe/ipxe
    EOF
    ~~~

    **Static IP Example**
    ~~~sh
    cat <<EOF > /boot/ai.ipxe
    #!ipxe
    ifopen net0
    set net0/ip 192.168.123.161
    set net0/netmask 255.255.255.0
    set net0/gateway 192.168.123.1
    set dns 192.168.123.1
    chain http://webserver.example.com:8080/ipxe/ipxe
    EOF
    ~~~

4. Create a custom GRUB entry for booting using the ipxe module

    > **NOTE**: Depending on the partition configuration on your server you might need to update the `set root` and the path to `ipxe.lkrn` and `ai.ipxe`.

    **Example1**
    ~~~sh
    cat <<EOF > /etc/grub.d/40_custom
    #!/bin/sh
    exec tail -n +3 \$0
    menuentry 'iPXE Environment' {
      set root='(hd0,1)'
      linux16 /boot/ipxe.lkrn
      initrd16 /boot/ai.ipxe
    }
    EOF
    ~~~

    **Example2**
    ~~~sh
    cat <<EOF > /etc/grub.d/40_custom
    #!/bin/sh
    exec tail -n +3 $0
    menuentry 'iPXE Environment' {
      set root='(hd0,1)'
      linux16 /ipxe.lkrn
      initrd16 /ai.ipxe
    }
    EOF
    ~~~

5. Make this new entry the default one

    ~~~sh
    grub2-set-default 'iPXE Environment'
    ~~~
6. Write the new GRUB configuration to disk

    ~~~sh
    grub2-mkconfig -o /boot/grub2/grub.cfg
    ~~~
7. Reboot the server and wait for the PXE boot to happen

    ~~~sh
    reboot
    ~~~
