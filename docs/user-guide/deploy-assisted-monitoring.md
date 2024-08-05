# WIP - Deploying Monitoring service for assisted-service

This will allow you to deploy Prometheus and Grafana already integrated with Assisted installer:

```shell
# Step by step
skipper make deploy-olm
skipper make deploy-prometheus
skipper make deploy-grafana

# Or just all-in
skipper make deploy-monitoring
```

NOTE: To expose the monitoring UI's on your local environment you could follow these steps

```shell
kubectl config set-context $(kubectl config current-context) --namespace assisted-installer

# To expose Prometheus
kubectl port-forward svc/prometheus-k8s 9090:9090

# To expose Grafana
kubectl port-forward svc/grafana 3000:3000
```

Now you just need to access [http://127.0.0.1:3000](http://127.0.0.1:3000) to access to your Grafana deployment or [http://127.0.0.1:9090](http://127.0.0.1:9090) for Prometheus.
