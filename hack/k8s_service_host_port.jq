# This jq filter accepts a kubernetes service manifest as input and outputs a
# host:port string that can be used to access that service.

# Resolve the service's external IP
(
   .status.loadBalancer.ingress[0].ip //
    # Fallback to hostname if IP is unset
   .status.loadBalancer.ingress[0].hostname
) as $host

# Resolve the service's port
| .spec.ports[0].port as $port

# Combine into host:port string
| "\($host):\($port)"
