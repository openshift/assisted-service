#! /bin/bash

function render_frontend_and_backend() {
    ports="$1"
    ips="$2"
    converted_ip=$(echo ${LOAD_BALANCER_IP} | sed 's/[.:]/_/g')
    for port in ${ports}; do
        upstream_name="upstream_${converted_ip}_${port}"
        cat <<EOF
    server {
        listen ${LOAD_BALANCER_IP}:${port};
        proxy_pass ${upstream_name};
    }
EOF
        if [ ${port} -eq 81 ]; then
            dport=80
        else
            dport=${port}
        fi
        echo "    upstream ${upstream_name} {"
        for ip in ${ips}; do
            echo "        server ${ip}:${dport};"
        done
        echo "    }"
        echo
    done
}

function get_spoke_ips() {
    for n in $*; do
        oc get -n ${SPOKE_NAMESPACE} agent $n -o jsonpath='{range .status.inventory.interfaces[*]}{.ipV4Addresses[*]} {end}' | sed 's@/[0-9]*@@g'
    done
}

function render_load_balancer_config_file() {
    fname=$1
    if [ -z "${fname}" ]; then
        echo "Missing destination file"
        exit 1
    fi
    master_ips=$(get_spoke_ips $(oc get agents -n ${SPOKE_NAMESPACE} | awk '/master/{print $1}'))
    if [ -z "${master_ips}" ]; then
        echo "Could not find master ips for spoke cluster"
        exit 1
    fi
    worker_ips=$(get_spoke_ips $(oc get agents -n ${SPOKE_NAMESPACE} | awk '/worker/{print $1}'))
    if [ -z "${worker_ips}" ]; then
        worker_ips="${master_ips}"
    fi
    render_frontend_and_backend "6443 22623 22624" "${master_ips}" >$fname

    # We use port 81 because port 80 is occupied by httpd in case the hub cluster was installed with dev-scripts
    render_frontend_and_backend "443 81" "${worker_ips}" >>$fname
}

function setup_libvirt_dns() {
    name=$(oc get -n ${SPOKE_NAMESPACE} clusterdeployment | awk '!/NAME/{print $1}')
    cluster_name=$(oc get -n ${SPOKE_NAMESPACE} clusterdeployment ${name} -o jsonpath='{.spec.clusterName}')
    base_domain=$(oc get -n ${SPOKE_NAMESPACE} clusterdeployment ${name} -o jsonpath='{.spec.baseDomain}')
    suffix="${cluster_name}.${base_domain}"
    xml="<host ip='${API_IP}'><hostname>virthost</hostname><hostname>api.${suffix}</hostname><hostname>api-int.${suffix}</hostname><hostname>console-openshift-console.apps.${suffix}</hostname><hostname>canary-openshift-ingress-canary.apps.${suffix}</hostname><hostname>oauth-openshift.apps.${suffix}</hostname></host>"
    virsh net-update ${LIBVIRT_NONE_PLATFORM_NETWORK} --command delete --section dns-host "${xml}"
    virsh net-update ${LIBVIRT_NONE_PLATFORM_NETWORK} --command add --section dns-host "${xml}" || exit 1
}

function open_firewall_ports() {
    for port in 6443 22623 22624 80 81 443; do
        firewall-cmd --zone=libvirt --add-port=${port}/tcp
    done
}

function redirect_port_80() {
    ips=$(get_spoke_ips $(oc get agents -n ${SPOKE_NAMESPACE} | awk '!/NAME/{print $1}'))
    for ip in ${ips}; do
        # Redirect from port 80 to port 81 because port 80 might already be allocated by httpd from dev-scripts installation
        iptables -t nat -I PREROUTING -s ${ip} -d ${LOAD_BALANCER_IP} -i ostestbm -p tcp -m tcp --dport 80 -j REDIRECT --to-ports 81
    done
}

function setup_and_run_load_balancer() {
    mkdir -p ./.nginx/conf.d
    fname=./.nginx/conf.d/stream_${LOAD_BALANCER_IP}.conf
    render_load_balancer_config_file $fname
    redirect_port_80
    lb_id=$(podman ps --quiet --filter "name=ztp_load_balancer")
    (test -z "${lb_id}" && echo "Starting load balancer ..." &&
        podman run -d --rm --dns=127.0.0.1 --net=host --privileged --name=ztp_load_balancer \
            -v ./.nginx/conf.d:/etc/nginx/conf.d \
            quay.io/odepaz/dynamic-load-balancer:latest) ||
        ! test -z "${lb_id}"
}
