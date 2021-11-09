# -*- coding: utf-8 -*-
import base64
import json
import os
import shutil
import time
import warnings
from logging import Logger
from typing import Any, Dict, List, Optional
from urllib.parse import urlparse

import requests
import waiting
from kubernetes.client import ApiClient as KubeApiClient
from kubernetes.config import load_kube_config
from kubernetes.config.kube_config import Configuration as KubeConfiguration
from retry import retry
from urllib3 import HTTPResponse

from assisted_service_client import ApiClient, Configuration, api, models
from junit_report import CaseFormatKeys, JsonJunitExporter

MINUTE = 60
WAIT_FOR_BM_API = 15 * MINUTE


class InventoryClient(object):
    def __init__(self, inventory_url: str, offline_token: Optional[str], pull_secret: str, logger):
        self.inventory_url = inventory_url
        configs = Configuration()
        configs.host = self.get_host(configs)
        configs.verify_ssl = False
        self._log = logger
        self.set_config_auth(configs, offline_token)
        self._set_x_secret_key(configs, pull_secret)

        self.api = ApiClient(configuration=configs)
        self.client = api.InstallerApi(api_client=self.api)
        self.events = api.EventsApi(api_client=self.api)
        self.versions = api.VersionsApi(api_client=self.api)
        self.domains = api.ManagedDomainsApi(api_client=self.api)
        self.operators = api.OperatorsApi(api_client=self.api)
        fmt = CaseFormatKeys(case_name="cluster_id", severity_key="severity", case_timestamp="event_time")
        self._events_junit_exporter = JsonJunitExporter(fmt)

def get_host(self, configs: Configuration) -> str:
        parsed_host = urlparse(configs.host)
        parsed_inventory_url = urlparse(self.inventory_url)
        return parsed_host._replace(netloc=parsed_inventory_url.netloc, scheme=parsed_inventory_url.scheme).geturl()

    def set_config_auth(self, c: Configuration, offline_token: Optional[str]) -> None:
        if not offline_token:
            self._log.info("OFFLINE_TOKEN not set, skipping authentication headers")
            return

        @retry(exceptions=requests.HTTPError, tries=5, delay=5)
        def refresh_api_key(config: Configuration) -> None:
            # Get the properly padded key segment
            auth = config.api_key.get("Authorization", None)
            if auth is not None:
                segment = auth.split(".")[1]
                padding = len(segment) % 4
                segment = segment + padding * "="

                expires_on = json.loads(base64.b64decode(segment))["exp"]

                # if this key doesn't expire or if it has more than 10 minutes left, don't refresh
                remaining = expires_on - time.time()
                if expires_on == 0 or remaining > 600:
                    return

            # fetch new key if expired or not set yet
            params = {
                "client_id": "cloud-services",
                "grant_type": "refresh_token",
                "refresh_token": offline_token,
            }

            self._log.info("Refreshing API key")
            response = requests.post(os.environ.get("SSO_URL"), data=params)
            response.raise_for_status()

            config.api_key["Authorization"] = response.json()["access_token"]

        c.api_key_prefix["Authorization"] = "Bearer"
        c.refresh_api_key_hook = refresh_api_key

    def _set_x_secret_key(self, c: Configuration, pull_secret: str) -> None:
        if not pull_secret:
            self._log.info("pull secret not set, skipping agent authentication headers")
            return

        self._log.info("Setting X-Secret-Key")
        c.api_key["X-Secret-Key"] = json.loads(pull_secret)["auths"]["cloud.openshift.com"]["auth"]

    def wait_for_api_readiness(self, timeout: int) -> None:
        self._log.info("Waiting for inventory api to be ready")
        waiting.wait(
            lambda: self.clusters_list() is not None,
            timeout_seconds=timeout,
            sleep_seconds=5,
            waiting_for="Wait till inventory is ready",
            expected_exceptions=Exception,
        )

    def create_cluster(
            self, name: str, ssh_public_key: Optional[str] = None, **cluster_params
    ) -> models.cluster.Cluster:
        cluster = models.ClusterCreateParams(name=name, ssh_public_key=ssh_public_key, **cluster_params)
        self._log.info("Creating cluster with params %s", cluster.__dict__)
        result = self.client.v2_register_cluster(new_cluster_params=cluster)
        return result

    def create_infra_env(
            self, name: str, ssh_public_key: Optional[str] = None, **infra_env_params
    ) -> models.infra_env.InfraEnv:
        infra_env = models.InfraEnvCreateParams(name=name, ssh_authorized_key=ssh_public_key, **infra_env_params)
        self._log.info("Creating infra-env with params %s", infra_env.__dict__)
        result = self.client.register_infra_env(infraenv_create_params=infra_env)
        return result

    def create_day2_cluster(self, name: str, cluster_uuid: str, **cluster_params) -> models.cluster.Cluster:
        cluster = models.AddHostsClusterCreateParams(name=name, id=cluster_uuid, **cluster_params)
        self._log.info("Creating day 2 cluster with params %s", cluster.__dict__)
        result = self.client.register_add_hosts_cluster(new_add_hosts_cluster_params=cluster)
        return result

    def get_cluster_hosts(self, cluster_id: str) -> List[Dict[str, Any]]:
        cluster_details = self.cluster_get(cluster_id)
        return list(map(lambda host: host.to_dict(), cluster_details.hosts))

    def get_infra_env_hosts(self, infra_env_id: str) -> List[Dict[str, Any]]:
        return self.client.v2_list_hosts(infra_env_id=infra_env_id)

    def get_infra_env(self, infra_env_id: str) -> models.infra_env.InfraEnv:
        return self.client.get_infra_env(infra_env_id=infra_env_id)

    def delete_infra_env(self, infra_env_id: str) -> None:
        self._log.info("Deleting infra_env %s", infra_env_id)
        self.client.deregister_infra_env(infra_env_id=infra_env_id)

    def get_cluster_operators(self, cluster_id: str) -> List[models.MonitoredOperator]:
        return self.cluster_get(cluster_id=cluster_id).monitored_operators

    def get_hosts_in_statuses(self, cluster_id: str, statuses: List[str]) -> List[dict]:
        hosts = self.get_cluster_hosts(cluster_id)
        return [host for host in hosts if host["status"] in statuses]

    def get_hosts_in_error_status(self, cluster_id: str):
        return self.get_hosts_in_statuses(cluster_id, [models.SourceState.ERROR])

    def clusters_list(self) -> List[Dict[str, Any]]:
        return self.client.v2_list_clusters()

    def infra_envs_list(self) -> List[models.InfraEnv]:
        return self.client.list_infra_envs()

    def get_all_clusters(self) -> List[Dict[str, Any]]:
        return self.client.v2_list_clusters(get_unregistered_clusters=True)

    def cluster_get(self, cluster_id: str) -> models.cluster.Cluster:
        return self.client.v2_get_cluster(cluster_id=cluster_id)

    def get_infra_env_by_cluster_id(self, cluster_id: str) -> List[models.infra_env.InfraEnv]:
        infra_envs = self.infra_envs_list()
        return filter(lambda infra_env: infra_env["cluster_id"] == cluster_id, infra_envs)

    @classmethod
    def _download(cls, response: HTTPResponse, file_path: str, verify_file_size=False) -> None:
        with open(file_path, "wb") as f:
            shutil.copyfileobj(response, f)
        if verify_file_size:
            content_length = int(response.headers["content-length"])
            actual_file_size = os.path.getsize(file_path)
            if actual_file_size < content_length:
                raise RuntimeError(
                    f"Could not complete ISO download {file_path}. "
                    f"Actual size: {actual_file_size}. Expected size: {content_length}"
                )

    def generate_image(
            self,
            cluster_id: str,
            ssh_key: str,
            image_type: str = models.ImageType.FULL_ISO,
            static_network_config: Optional[list] = None,
    ) -> models.cluster.Cluster:
        self._log.info("Generating image for cluster %s", cluster_id)
        image_create_params = models.ImageCreateParams(
            ssh_public_key=ssh_key, static_network_config=static_network_config, image_type=image_type
        )
        self._log.info("Generating image with params %s", image_create_params.__dict__)
        return self.client.generate_cluster_iso(cluster_id=cluster_id, image_create_params=image_create_params)

    @retry(exceptions=RuntimeError, tries=2, delay=3)
    def download_image(self, cluster_id: str, image_path: str) -> None:
        self._log.info("Downloading image for cluster %s to %s", cluster_id, image_path)
        response = self.client.download_cluster_iso_with_http_info(cluster_id=cluster_id, _preload_content=False)
        response_obj = response[0]
        self._download(response=response_obj, file_path=image_path, verify_file_size=True)

    def generate_and_download_image(
            self,
            cluster_id: str,
            ssh_key: str,
            image_path: str,
            image_type: str = models.ImageType.FULL_ISO,
            static_network_config: Optional[list] = None,
    ):
        self.generate_image(
            cluster_id=cluster_id, ssh_key=ssh_key, image_type=image_type, static_network_config=static_network_config
        )
        self.download_image(cluster_id=cluster_id, image_path=image_path)

    def update_infra_env(self, infra_env_id: str, infra_env_update_params):
        self._log.info("Updating infra env %s with values %s", infra_env_id, infra_env_update_params)
        self.client.update_infra_env(infra_env_id=infra_env_id, infra_env_update_params=infra_env_update_params)

    @retry(exceptions=RuntimeError, tries=2, delay=3)
    def download_infraenv_image(self, infraenv_id: str, image_path: str) -> None:
        self._log.info("Downloading image for infra-env %s to %s", infraenv_id, image_path)
        response = self.client.download_infra_env_discovery_image_with_http_info(
            infra_env_id=infraenv_id, _preload_content=False
        )
        response_obj = response[0]
        self._download(response=response_obj, file_path=image_path, verify_file_size=True)

    # TODO remove and use infraenv
    def update_hosts(
            self, cluster_id: str, hosts_with_roles, hosts_names: Optional[models.ClusterupdateparamsHostsNames] = None
    ) -> models.cluster.Cluster:
        warnings.warn("update_hosts is deprecated. Use update_host instead.", DeprecationWarning)
        self._log.info("Setting roles for hosts %s in cluster %s", hosts_with_roles, cluster_id)
        hosts = models.ClusterUpdateParams(hosts_roles=hosts_with_roles, hosts_names=hosts_names)
        return self.update_cluster(cluster_id=cluster_id, update_params=hosts)

    def update_host(self, infra_env_id: str, host_id: str, host_role: str = None, host_name: str = None):
        host_update_params = models.HostUpdateParams(host_role=host_role, host_name=host_name)
        self.client.v2_update_host(infra_env_id=infra_env_id, host_id=host_id, host_update_params=host_update_params)

    def select_installation_disk(self, infra_env_id: str, host_id: str, disk_paths: List[dict]) -> None:
        self._log.info("Setting installation disk for host %s in infra_env %s", host_id, infra_env_id)

        def role_to_selected_disk_config(disk_id: str, role: models.DiskRole) -> models.DiskConfigParams:
            return models.DiskConfigParams(id=disk_id, role=role)

        disks_selected_config = [
            role_to_selected_disk_config(disk["disk_id"] if "disk_id" in disk else disk["path"], disk["role"])
            for disk in disk_paths
        ]

        params = models.HostUpdateParams(disks_selected_config=disks_selected_config)
        return self.client.v2_update_host(infra_env_id=infra_env_id, host_id=host_id, host_update_params=params)

    def set_pull_secret(self, cluster_id: str, pull_secret: str) -> models.cluster.Cluster:
        self._log.info("Setting pull secret for cluster %s", cluster_id)
        update_params = models.ClusterUpdateParams(pull_secret=pull_secret)
        return self.update_cluster(cluster_id=cluster_id, update_params=update_params)

    def update_cluster(self, cluster_id, update_params) -> models.cluster.Cluster:
        self._log.info("Updating cluster %s with params %s", cluster_id, update_params)
        return self.client.v2_update_cluster(cluster_id=cluster_id, cluster_update_params=update_params)

    def delete_cluster(self, cluster_id: str):
        self._log.info("Deleting cluster %s", cluster_id)
        self.client.v2_deregister_cluster(cluster_id=cluster_id)

    def deregister_host(self, infra_env_id: str, host_id: str):
        self._log.info(f"Deleting host {host_id} in infra_env {infra_env_id}")
        self.client.v2_deregister_host(infra_env_id=infra_env_id, host_id=host_id)

    def get_hosts_id_with_macs(self, cluster_id: str) -> Dict[Any, List[str]]:
        hosts = self.get_cluster_hosts(cluster_id)
        hosts_data = {}
        for host in hosts:
            inventory = json.loads(host.get("inventory", '{"interfaces":[]}'))
            hosts_data[host["id"]] = [interface["mac_address"] for interface in inventory["interfaces"]]
        return hosts_data

    def get_host_by_mac(self, cluster_id: str, mac: str) -> Dict[str, Any]:
        hosts = self.get_cluster_hosts(cluster_id)

        for host in hosts:
            inventory = json.loads(host.get("inventory", '{"interfaces":[]}'))
            if mac.lower() in [interface["mac_address"].lower() for interface in inventory["interfaces"]]:
                return host

    def get_host_by_name(self, cluster_id: str, host_name: str) -> Dict[str, Any]:
        hosts = self.get_cluster_hosts(cluster_id)

        for host in hosts:
            hostname = host.get("requested_hostname")
            if hostname == host_name:
                self._log.info(f"Requested host by name: {host_name}, host details: {host}")
                return host

    def download_and_save_file(self, cluster_id: str, file_name: str, file_path: str) -> None:
        self._log.info("Downloading %s to %s", file_name, file_path)
        response = self.client.v2_download_cluster_files(
            cluster_id=cluster_id, file_name=file_name, _preload_content=False
        )
        with open(file_path, "wb") as _file:
            _file.write(response.data)

    def download_kubeconfig_no_ingress(self, cluster_id: str, kubeconfig_path: str) -> None:
        self._log.info("Downloading kubeconfig-noingress to %s", kubeconfig_path)
        response = self.client.v2_download_cluster_credentials(
            cluster_id=cluster_id, file_name="kubeconfig-noingress", _preload_content=False
        )
        with open(kubeconfig_path, "wb") as _file:
            _file.write(response.data)

    def download_host_ignition(self, infra_env_id: str, host_id: str, destination: str) -> None:
        self._log.info("Downloading host %s infra_env %s ignition files to %s", host_id, infra_env_id, destination)

        response = self.client.v2_download_host_ignition(
            infra_env_id=infra_env_id, host_id=host_id, _preload_content=False
        )
        with open(os.path.join(destination, f"host_{host_id}.ign"), "wb") as _file:
            _file.write(response.data)

    def download_kubeconfig(self, cluster_id: str, kubeconfig_path: str) -> None:
        self._log.info("Downloading kubeconfig to %s", kubeconfig_path)
        response = self.client.v2_download_cluster_credentials(
            cluster_id=cluster_id, file_name="kubeconfig", _preload_content=False
        )
        with open(kubeconfig_path, "wb") as _file:
            _file.write(response.data)

    def download_metrics(self, dest: str) -> None:
        self._log.info("Downloading metrics to %s", dest)

        url = self.inventory_url
        if not (url.startswith("http://") or url.startswith("https://")):
            url = f"http://{url}"
        response = requests.get(f"{url}/metrics")
        response.raise_for_status()

        with open(dest, "w") as _file:
            _file.write(response.text)

    def install_cluster(self, cluster_id: str) -> models.cluster.Cluster:
        self._log.info("Installing cluster %s", cluster_id)
        return self.client.v2_install_cluster(cluster_id=cluster_id)

    def install_day2_cluster(self, cluster_id: str) -> models.cluster.Cluster:
        self._log.info("Installing day2 cluster %s", cluster_id)
        return self.client.install_hosts(cluster_id=cluster_id)

    def install_day2_host(self, infra_env_id: str, host_id: str) -> models.cluster.Cluster:
        self._log.info("Installing day2 host %s, infra_env_id %s", host_id, infra_env_id)
        return self.client.v2_install_host(infra_env_id=infra_env_id, host_id=host_id)

    def download_cluster_logs(self, cluster_id: str, output_file: str) -> None:
        self._log.info("Downloading cluster logs to %s", output_file)
        response = self.client.v2_download_cluster_logs(cluster_id=cluster_id, _preload_content=False)
        with open(output_file, "wb") as _file:
            _file.write(response.data)

    def get_events(
            self,
            cluster_id: Optional[str] = "",
            host_id: Optional[str] = "",
            infra_env_id: Optional[str] = "",
            categories=None,
    ) -> dict:
        if categories is None:
            categories = ["user"]
        # Get users events
        response = self.events.v2_list_events(
            cluster_id=cluster_id,
            host_id=host_id,
            infra_env_id=infra_env_id,
            categories=categories,
            _preload_content=False,
        )

        return json.loads(response.data)

    def download_cluster_events(self, cluster_id: str, output_file: str, categories=None) -> None:
        if categories is None:
            categories = ["user"]
        self._log.info("Downloading cluster events to %s", output_file)

        with open(output_file, "wb") as _file:
            _file.write(json.dumps(self.get_events(cluster_id, categories=categories), indent=4).encode())
            events = self.get_events(cluster_id, categories=categories)
            _file.write(json.dumps(events, indent=4).encode())
            self._events_junit_exporter.collect(events, suite_name=f"cluster_events_{cluster_id}")

    def download_host_logs(self, cluster_id: str, host_id: str, output_file) -> None:
        self._log.info("Downloading host logs to %s", output_file)
        response = self.client.v2_download_cluster_logs(cluster_id=cluster_id, host_id=host_id, _preload_content=False)
        with open(output_file, "wb") as _file:
            _file.write(response.data)

    def cancel_cluster_install(self, cluster_id: str) -> models.cluster.Cluster:
        self._log.info("Canceling installation of cluster %s", cluster_id)
        return self.client.v2_cancel_installation(cluster_id=cluster_id)

    def reset_cluster_install(self, cluster_id: str) -> models.cluster.Cluster:
        self._log.info("Reset installation of cluster %s", cluster_id)
        return self.client.v2_reset_cluster(cluster_id=cluster_id)

    def disable_host(self, cluster_id: str, host_id: str) -> models.cluster.Cluster:
        warnings.warn("disable_host is deprecated. Use unbind_host instead.", DeprecationWarning)
        self._log.info(f"Disabling host: {host_id}, in cluster id: {cluster_id}")
        return self.client.disable_host(cluster_id=cluster_id, host_id=host_id)

    def enable_host(self, cluster_id: str, host_id: str) -> models.cluster.Cluster:
        warnings.warn("enable_host is deprecated. Use bind_host instead.", DeprecationWarning)
        self._log.info(f"Enabling host: {host_id}, in cluster id: {cluster_id}")
        return self.client.enable_host(cluster_id=cluster_id, host_id=host_id)

    def bind_host(self, infra_env_id: str, host_id: str, cluster_id: str) -> None:
        self._log.info(f"Enabling host: {host_id}, from infra_env {infra_env_id}, in cluster id: {cluster_id}")
        bind_host_params = models.BindHostParams(cluster_id=cluster_id)
        self.client.bind_host(infra_env_id=infra_env_id, host_id=host_id, bind_host_params=bind_host_params)

    def unbind_host(self, infra_env_id: str, host_id: str) -> None:
        self._log.info(f"Disabling host: {host_id}, from infra_env {infra_env_id}")
        self.client.unbind_host(infra_env_id=infra_env_id, host_id=host_id)

    def set_cluster_proxy(
            self, cluster_id: str, http_proxy: str, https_proxy: Optional[str] = "", no_proxy: Optional[str] = ""
    ) -> models.cluster.Cluster:
        self._log.info("Setting proxy for cluster %s", cluster_id)
        update_params = models.ClusterUpdateParams(http_proxy=http_proxy, https_proxy=https_proxy, no_proxy=no_proxy)
        return self.update_cluster(cluster_id=cluster_id, update_params=update_params)

    def get_cluster_install_config(self, cluster_id: str) -> str:
        self._log.info("Getting install-config for cluster %s", cluster_id)
        return self.client.v2_get_cluster_install_config(cluster_id=cluster_id)

    def patch_cluster_discovery_ignition(self, cluster_id: str, ignition_info: str) -> None:
        warnings.warn(
            "patch_cluster_discovery_ignition is deprecated. Use patch_discovery_ignition instead.", DeprecationWarning
        )
        self._log.info("Patching cluster %s discovery ignition", cluster_id)
        return self.client.update_discovery_ignition(
            cluster_id=cluster_id,
            discovery_ignition_params=models.DiscoveryIgnitionParams(config=json.dumps(ignition_info)),
        )

    def patch_discovery_ignition(self, infra_env_id: str, ignition_info: str) -> None:
        infra_env_update_params = models.InfraEnvUpdateParams(ignition_config_override=json.dumps(ignition_info))
        self.update_infra_env(infra_env_id=infra_env_id, infra_env_update_params=infra_env_update_params)

    def get_cluster_discovery_ignition(self, cluster_id: str) -> None:
        warnings.warn(
            "get_cluster_discovery_ignition is deprecated. Use get_discovery_ignition instead.", DeprecationWarning
        )
        self._log.info("Getting discovery ignition for cluster %s", cluster_id)
        return self.client.get_discovery_ignition(
            cluster_id=cluster_id,
        )

    def get_discovery_ignition(self, infra_env_id: str) -> str:
        infra_env = self.get_infra_env(infra_env_id=infra_env_id)
        return infra_env.ingition_config_override

    def register_host(self, infra_env_id: str, host_id: str) -> None:
        self._log.info(f"Registering host: {host_id} to cluster: {infra_env_id}")
        host_params = models.HostCreateParams(host_id=host_id)
        self.client.v2_register_host(infra_env_id=infra_env_id, new_host_params=host_params)

    def host_get_next_step(self, infra_env_id: str, host_id: str) -> models.Steps:
        self._log.info(f"Getting next step for host: {host_id} in cluster: {infra_env_id}")
        return self.client.v2_get_next_steps(infra_env_id=infra_env_id, host_id=host_id)

    def host_post_step_result(self, infra_env_id: str, host_id: str, **kwargs) -> None:
        reply = models.StepReply(**kwargs)
        self.client.v2_post_step_reply(infra_env_id=infra_env_id, host_id=host_id, reply=reply)

    def host_update_progress(
            self, infra_env_id: str, host_id: str, current_stage: models.HostStage, progress_info=None
    ) -> None:
        host_progress = models.HostProgress(current_stage=current_stage, progress_info=progress_info)
        self.client.v2_update_host_install_progress(
            infra_env_id=infra_env_id, host_id=host_id, host_progress=host_progress
        )

    def complete_cluster_installation(self, cluster_id: str, is_success: bool, error_info=None) -> None:
        completion_params = models.CompletionParams(is_success=is_success, error_info=error_info)
        self.client.v2_complete_installation(cluster_id=cluster_id, completion_params=completion_params)

    def get_cluster_admin_credentials(self, cluster_id: str) -> models.Credentials:
        return self.client.v2_get_credentials(cluster_id=cluster_id)

    def get_versions(self) -> dict:
        response = self.versions.v2_list_component_versions()
        return json.loads(json.dumps(response.to_dict(), sort_keys=True, default=str))

    def get_openshift_versions(self) -> models.OpenshiftVersions:
        return self.versions.v2_list_supported_openshift_versions()

    def get_supported_operators(self) -> List[str]:
        return self.operators.v2_list_supported_operators()

    # TODO remove in favor of get_preflight_requirements
    def get_cluster_host_requirements(self, cluster_id: str) -> models.ClusterHostRequirementsList:
        return self.client.get_cluster_host_requirements(cluster_id=cluster_id)

    def get_managed_domains(self) -> models.ListManagedDomains:
        return self.domains.v2_list_managed_domains()

    def get_preflight_requirements(self, cluster_id: str):
        return self.client.v2_get_preflight_requirements(cluster_id=cluster_id)


class ClientFactory:
    @staticmethod
    def create_client(
            log: Logger,
            url: str,
            offline_token: str,
            pull_secret: Optional[str] = "",
            wait_for_api: Optional[bool] = True,
            timeout: Optional[int] = WAIT_FOR_BM_API,
    ) -> InventoryClient:
        log.info("Creating assisted-service client for url: %s", url)
        c = InventoryClient(url, offline_token, pull_secret, log)
        if wait_for_api:
            c.wait_for_api_readiness(timeout)
        return c

    @staticmethod
    def create_kube_api_client(kubeconfig_path: str, log: Logger) -> ApiClient:
        log.info("creating kube client with config file: %s", kubeconfig_path)

        conf = KubeConfiguration()
        load_kube_config(config_file=kubeconfig_path, client_configuration=conf)
        return KubeApiClient(configuration=conf)
