/*
Copyright (c) 2020 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

  http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// IMPORTANT: This file has been generated automatically, refrain from modifying it manually as all
// your changes will be lost when the file is generated again.

package v1 // github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1

import (
	"context"
	"net/http"

	"github.com/golang/glog"
	"github.com/openshift-online/ocm-sdk-go/errors"
)

// ClusterServer represents the interface the manages the 'cluster' resource.
type ClusterServer interface {

	// Delete handles a request for the 'delete' method.
	//
	// Deletes the cluster.
	Delete(ctx context.Context, request *ClusterDeleteServerRequest, response *ClusterDeleteServerResponse) error

	// Get handles a request for the 'get' method.
	//
	// Retrieves the details of the cluster.
	Get(ctx context.Context, request *ClusterGetServerRequest, response *ClusterGetServerResponse) error

	// Hibernate handles a request for the 'hibernate' method.
	//
	// Initiates cluster hibernation. While hibernating a cluster will not consume any cloud provider infrastructure
	// but will be counted for quota.
	Hibernate(ctx context.Context, request *ClusterHibernateServerRequest, response *ClusterHibernateServerResponse) error

	// Resume handles a request for the 'resume' method.
	//
	// Resumes from Hibernation.
	Resume(ctx context.Context, request *ClusterResumeServerRequest, response *ClusterResumeServerResponse) error

	// Update handles a request for the 'update' method.
	//
	// Updates the cluster.
	Update(ctx context.Context, request *ClusterUpdateServerRequest, response *ClusterUpdateServerResponse) error

	// AWSInfrastructureAccessRoleGrants returns the target 'AWS_infrastructure_access_role_grants' resource.
	//
	// Reference to the resource that manages the collection of AWS infrastructure
	// access role grants on this cluster.
	AWSInfrastructureAccessRoleGrants() AWSInfrastructureAccessRoleGrantsServer

	// AddonInquiries returns the target 'addon_inquiries' resource.
	//
	// Reference to the resource that manages the collection of the add-on inquiries on this cluster.
	AddonInquiries() AddonInquiriesServer

	// Addons returns the target 'add_on_installations' resource.
	//
	// Reference to the resource that manages the collection of add-ons installed on this cluster.
	Addons() AddOnInstallationsServer

	// Clusterdeployment returns the target 'clusterdeployment' resource.
	//
	// Reference to the resource that manages the cluster deployment.
	Clusterdeployment() ClusterdeploymentServer

	// Credentials returns the target 'credentials' resource.
	//
	// Reference to the resource that manages the credentials of the cluster.
	Credentials() CredentialsServer

	// ExternalConfiguration returns the target 'external_configuration' resource.
	//
	// Reference to the resource that manages the external configuration.
	ExternalConfiguration() ExternalConfigurationServer

	// Groups returns the target 'groups' resource.
	//
	// Reference to the resource that manages the collection of groups.
	Groups() GroupsServer

	// IdentityProviders returns the target 'identity_providers' resource.
	//
	// Reference to the resource that manages the collection of identity providers.
	IdentityProviders() IdentityProvidersServer

	// Ingresses returns the target 'ingresses' resource.
	//
	// Reference to the resource that manages the collection of ingress resources.
	Ingresses() IngressesServer

	// LimitedSupportReasons returns the target 'limited_support_reasons' resource.
	//
	// Reference to cluster limited support reasons.
	LimitedSupportReasons() LimitedSupportReasonsServer

	// Logs returns the target 'logs' resource.
	//
	// Reference to the resource that manages the collection of logs of the cluster.
	Logs() LogsServer

	// MachinePools returns the target 'machine_pools' resource.
	//
	// Reference to the resource that manages the collection of machine pool resources.
	MachinePools() MachinePoolsServer

	// MetricQueries returns the target 'metric_queries' resource.
	//
	// Reference to the resource that manages metrics queries for the cluster.
	MetricQueries() MetricQueriesServer

	// Product returns the target 'product' resource.
	//
	// Reference to the resource that manages the product type of the cluster
	Product() ProductServer

	// ProvisionShard returns the target 'provision_shard' resource.
	//
	// Reference to the resource that manages the cluster's provision shard.
	ProvisionShard() ProvisionShardServer

	// Resources returns the target 'resources' resource.
	//
	// Reference to cluster resources.
	Resources() ResourcesServer

	// Status returns the target 'cluster_status' resource.
	//
	// Reference to the resource that manages the detailed status of the cluster.
	Status() ClusterStatusServer

	// UpgradePolicies returns the target 'upgrade_policies' resource.
	//
	// Reference to the resource that manages the collection of upgrade policies defined for this cluster.
	UpgradePolicies() UpgradePoliciesServer
}

// ClusterDeleteServerRequest is the request for the 'delete' method.
type ClusterDeleteServerRequest struct {
	deprovision *bool
}

// Deprovision returns the value of the 'deprovision' parameter.
//
// If false it will only delete from OCM but not the actual cluster resources.
// false is only allowed for OCP clusters. true by default.
func (r *ClusterDeleteServerRequest) Deprovision() bool {
	if r != nil && r.deprovision != nil {
		return *r.deprovision
	}
	return false
}

// GetDeprovision returns the value of the 'deprovision' parameter and
// a flag indicating if the parameter has a value.
//
// If false it will only delete from OCM but not the actual cluster resources.
// false is only allowed for OCP clusters. true by default.
func (r *ClusterDeleteServerRequest) GetDeprovision() (value bool, ok bool) {
	ok = r != nil && r.deprovision != nil
	if ok {
		value = *r.deprovision
	}
	return
}

// ClusterDeleteServerResponse is the response for the 'delete' method.
type ClusterDeleteServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *ClusterDeleteServerResponse) Status(value int) *ClusterDeleteServerResponse {
	r.status = value
	return r
}

// ClusterGetServerRequest is the request for the 'get' method.
type ClusterGetServerRequest struct {
}

// ClusterGetServerResponse is the response for the 'get' method.
type ClusterGetServerResponse struct {
	status int
	err    *errors.Error
	body   *Cluster
}

// Body sets the value of the 'body' parameter.
//
//
func (r *ClusterGetServerResponse) Body(value *Cluster) *ClusterGetServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *ClusterGetServerResponse) Status(value int) *ClusterGetServerResponse {
	r.status = value
	return r
}

// ClusterHibernateServerRequest is the request for the 'hibernate' method.
type ClusterHibernateServerRequest struct {
}

// ClusterHibernateServerResponse is the response for the 'hibernate' method.
type ClusterHibernateServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *ClusterHibernateServerResponse) Status(value int) *ClusterHibernateServerResponse {
	r.status = value
	return r
}

// ClusterResumeServerRequest is the request for the 'resume' method.
type ClusterResumeServerRequest struct {
}

// ClusterResumeServerResponse is the response for the 'resume' method.
type ClusterResumeServerResponse struct {
	status int
	err    *errors.Error
}

// Status sets the status code.
func (r *ClusterResumeServerResponse) Status(value int) *ClusterResumeServerResponse {
	r.status = value
	return r
}

// ClusterUpdateServerRequest is the request for the 'update' method.
type ClusterUpdateServerRequest struct {
	body *Cluster
}

// Body returns the value of the 'body' parameter.
//
//
func (r *ClusterUpdateServerRequest) Body() *Cluster {
	if r == nil {
		return nil
	}
	return r.body
}

// GetBody returns the value of the 'body' parameter and
// a flag indicating if the parameter has a value.
//
//
func (r *ClusterUpdateServerRequest) GetBody() (value *Cluster, ok bool) {
	ok = r != nil && r.body != nil
	if ok {
		value = r.body
	}
	return
}

// ClusterUpdateServerResponse is the response for the 'update' method.
type ClusterUpdateServerResponse struct {
	status int
	err    *errors.Error
	body   *Cluster
}

// Body sets the value of the 'body' parameter.
//
//
func (r *ClusterUpdateServerResponse) Body(value *Cluster) *ClusterUpdateServerResponse {
	r.body = value
	return r
}

// Status sets the status code.
func (r *ClusterUpdateServerResponse) Status(value int) *ClusterUpdateServerResponse {
	r.status = value
	return r
}

// dispatchCluster navigates the servers tree rooted at the given server
// till it finds one that matches the given set of path segments, and then invokes
// the corresponding server.
func dispatchCluster(w http.ResponseWriter, r *http.Request, server ClusterServer, segments []string) {
	if len(segments) == 0 {
		switch r.Method {
		case "DELETE":
			adaptClusterDeleteRequest(w, r, server)
			return
		case "GET":
			adaptClusterGetRequest(w, r, server)
			return
		case "PATCH":
			adaptClusterUpdateRequest(w, r, server)
			return
		default:
			errors.SendMethodNotAllowed(w, r)
			return
		}
	}
	switch segments[0] {
	case "hibernate":
		if r.Method != "POST" {
			errors.SendMethodNotAllowed(w, r)
			return
		}
		adaptClusterHibernateRequest(w, r, server)
		return
	case "resume":
		if r.Method != "POST" {
			errors.SendMethodNotAllowed(w, r)
			return
		}
		adaptClusterResumeRequest(w, r, server)
		return
	case "aws_infrastructure_access_role_grants":
		target := server.AWSInfrastructureAccessRoleGrants()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchAWSInfrastructureAccessRoleGrants(w, r, target, segments[1:])
	case "addon_inquiries":
		target := server.AddonInquiries()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchAddonInquiries(w, r, target, segments[1:])
	case "addons":
		target := server.Addons()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchAddOnInstallations(w, r, target, segments[1:])
	case "clusterdeployment":
		target := server.Clusterdeployment()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchClusterdeployment(w, r, target, segments[1:])
	case "credentials":
		target := server.Credentials()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchCredentials(w, r, target, segments[1:])
	case "external_configuration":
		target := server.ExternalConfiguration()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchExternalConfiguration(w, r, target, segments[1:])
	case "groups":
		target := server.Groups()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchGroups(w, r, target, segments[1:])
	case "identity_providers":
		target := server.IdentityProviders()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchIdentityProviders(w, r, target, segments[1:])
	case "ingresses":
		target := server.Ingresses()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchIngresses(w, r, target, segments[1:])
	case "limited_support_reasons":
		target := server.LimitedSupportReasons()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchLimitedSupportReasons(w, r, target, segments[1:])
	case "logs":
		target := server.Logs()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchLogs(w, r, target, segments[1:])
	case "machine_pools":
		target := server.MachinePools()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchMachinePools(w, r, target, segments[1:])
	case "metric_queries":
		target := server.MetricQueries()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchMetricQueries(w, r, target, segments[1:])
	case "product":
		target := server.Product()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchProduct(w, r, target, segments[1:])
	case "provision_shard":
		target := server.ProvisionShard()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchProvisionShard(w, r, target, segments[1:])
	case "resources":
		target := server.Resources()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchResources(w, r, target, segments[1:])
	case "status":
		target := server.Status()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchClusterStatus(w, r, target, segments[1:])
	case "upgrade_policies":
		target := server.UpgradePolicies()
		if target == nil {
			errors.SendNotFound(w, r)
			return
		}
		dispatchUpgradePolicies(w, r, target, segments[1:])
	default:
		errors.SendNotFound(w, r)
		return
	}
}

// adaptClusterDeleteRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptClusterDeleteRequest(w http.ResponseWriter, r *http.Request, server ClusterServer) {
	request := &ClusterDeleteServerRequest{}
	err := readClusterDeleteRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ClusterDeleteServerResponse{}
	response.status = 204
	err = server.Delete(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeClusterDeleteResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptClusterGetRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptClusterGetRequest(w http.ResponseWriter, r *http.Request, server ClusterServer) {
	request := &ClusterGetServerRequest{}
	err := readClusterGetRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ClusterGetServerResponse{}
	response.status = 200
	err = server.Get(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeClusterGetResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptClusterHibernateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptClusterHibernateRequest(w http.ResponseWriter, r *http.Request, server ClusterServer) {
	request := &ClusterHibernateServerRequest{}
	err := readClusterHibernateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ClusterHibernateServerResponse{}
	response.status = 200
	err = server.Hibernate(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeClusterHibernateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptClusterResumeRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptClusterResumeRequest(w http.ResponseWriter, r *http.Request, server ClusterServer) {
	request := &ClusterResumeServerRequest{}
	err := readClusterResumeRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ClusterResumeServerResponse{}
	response.status = 200
	err = server.Resume(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeClusterResumeResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}

// adaptClusterUpdateRequest translates the given HTTP request into a call to
// the corresponding method of the given server. Then it translates the
// results returned by that method into an HTTP response.
func adaptClusterUpdateRequest(w http.ResponseWriter, r *http.Request, server ClusterServer) {
	request := &ClusterUpdateServerRequest{}
	err := readClusterUpdateRequest(request, r)
	if err != nil {
		glog.Errorf(
			"Can't read request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	response := &ClusterUpdateServerResponse{}
	response.status = 200
	err = server.Update(r.Context(), request, response)
	if err != nil {
		glog.Errorf(
			"Can't process request for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		errors.SendInternalServerError(w, r)
		return
	}
	err = writeClusterUpdateResponse(response, w)
	if err != nil {
		glog.Errorf(
			"Can't write response for method '%s' and path '%s': %v",
			r.Method, r.URL.Path, err,
		)
		return
	}
}
