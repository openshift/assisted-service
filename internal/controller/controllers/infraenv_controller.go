/*
Copyright 2021.

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

package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/internal/bminventory"
	"github.com/openshift/assisted-service/internal/common"
	"github.com/openshift/assisted-service/internal/gencrypto"
	"github.com/openshift/assisted-service/internal/imageservice"
	"github.com/openshift/assisted-service/internal/versions"
	"github.com/openshift/assisted-service/models"
	"github.com/openshift/assisted-service/pkg/auth"
	logutil "github.com/openshift/assisted-service/pkg/log"
	"github.com/openshift/assisted-service/restapi/operations/installer"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	hivev1 "github.com/openshift/hive/apis/hive/v1"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/thoas/go-funk"
	"gorm.io/gorm"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const defaultRequeueAfterPerRecoverableError = 2 * bminventory.WindowBetweenRequestsInSeconds
const InfraEnvFinalizerName = "infraenv." + aiv1beta1.Group + "/ai-deprovision"
const EnableIronicAgentAnnotation = "infraenv." + aiv1beta1.Group + "/enable-ironic-agent"

type InfraEnvConfig struct {
	ImageType models.ImageType `envconfig:"ISO_IMAGE_TYPE" default:"minimal-iso"`
}

// InfraEnvReconciler reconciles a InfraEnv object
type InfraEnvReconciler struct {
	client.Client
	APIReader           client.Reader
	Config              InfraEnvConfig
	Log                 logrus.FieldLogger
	Installer           bminventory.InstallerInternals
	CRDEventsHandler    CRDEventsHandler
	ServiceBaseURL      string
	ImageServiceBaseURL string
	AuthType            auth.AuthType
	VersionsHandler     versions.Handler
	PullSecretHandler
	InsecureIPXEURLs bool
}

// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=nmstateconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=agent-install.openshift.io,resources=infraenvs/status,verbs=get;update;patch

func (r *InfraEnvReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx := addRequestIdIfNeeded(origCtx)
	log := logutil.FromContext(ctx, r.Log).WithFields(
		logrus.Fields{
			"infra_env":           req.Name,
			"infra_env_namespace": req.Namespace,
		})

	defer func() {
		log.Info("InfraEnv Reconcile ended")
	}()

	log.Info("InfraEnv Reconcile started")

	infraEnv := &aiv1beta1.InfraEnv{}
	if err := r.Get(ctx, req.NamespacedName, infraEnv); err != nil {
		log.WithError(err).Errorf("Failed to get resource %s", req.NamespacedName)
		return r.deregisterInfraEnvIfNeeded(ctx, log, req.NamespacedName)
	}

	if infraEnv.ObjectMeta.DeletionTimestamp.IsZero() { // infraEnv not being deleted
		// Register a finalizer if it is absent.
		if !funk.ContainsString(infraEnv.GetFinalizers(), InfraEnvFinalizerName) {
			controllerutil.AddFinalizer(infraEnv, InfraEnvFinalizerName)
			if err := r.Update(ctx, infraEnv); err != nil {
				log.WithError(err).Errorf("failed to add finalizer %s to infraEnv %s %s", InfraEnvFinalizerName, infraEnv.Name, infraEnv.Namespace)
				return ctrl.Result{Requeue: true}, err
			}
		}
	} else { // infraEnv is being deleted
		if funk.ContainsString(infraEnv.GetFinalizers(), InfraEnvFinalizerName) {
			// deletion finalizer found, deregister the backend hosts and the infraenv
			cleanUpErr := r.deregisterInfraEnvWithHosts(ctx, log, req.NamespacedName)

			if cleanUpErr != nil {
				reply := ctrl.Result{RequeueAfter: longerRequeueAfterOnError}
				log.WithError(cleanUpErr).Errorf("failed to run pre-deletion cleanup for finalizer %s on resource %s %s", InfraEnvFinalizerName, infraEnv.Name, infraEnv.Namespace)
				return reply, cleanUpErr
			}
			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(infraEnv, InfraEnvFinalizerName)
			if err := r.Update(ctx, infraEnv); err != nil {
				log.WithError(err).Errorf("failed to remove finalizer %s from infraEnv %s %s", InfraEnvFinalizerName, infraEnv.Name, infraEnv.Namespace)
				return ctrl.Result{Requeue: true}, err
			}
		}
		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	return r.ensureISO(ctx, log, infraEnv)
}

func (r *InfraEnvReconciler) updateInfraEnv(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv, internalInfraEnv *common.InfraEnv) (*common.InfraEnv, error) {
	updateParams := installer.UpdateInfraEnvParams{
		InfraEnvID:           *internalInfraEnv.ID,
		InfraEnvUpdateParams: &models.InfraEnvUpdateParams{},
	}
	if infraEnv.Spec.Proxy != nil {
		proxy := &models.Proxy{}
		if infraEnv.Spec.Proxy.NoProxy != "" {
			proxy.NoProxy = swag.String(infraEnv.Spec.Proxy.NoProxy)
		}
		if infraEnv.Spec.Proxy.HTTPProxy != "" {
			proxy.HTTPProxy = swag.String(infraEnv.Spec.Proxy.HTTPProxy)
		}
		if infraEnv.Spec.Proxy.HTTPSProxy != "" {
			proxy.HTTPSProxy = swag.String(infraEnv.Spec.Proxy.HTTPSProxy)
		}
		updateParams.InfraEnvUpdateParams.Proxy = proxy
	}
	if len(infraEnv.Spec.AdditionalNTPSources) > 0 {
		updateParams.InfraEnvUpdateParams.AdditionalNtpSources = swag.String(strings.Join(infraEnv.Spec.AdditionalNTPSources[:], ","))
	}
	if infraEnv.Spec.IgnitionConfigOverride != "" {
		updateParams.InfraEnvUpdateParams.IgnitionConfigOverride = infraEnv.Spec.IgnitionConfigOverride
	}
	if infraEnv.Spec.SSHAuthorizedKey != internalInfraEnv.SSHAuthorizedKey {
		updateParams.InfraEnvUpdateParams.SSHAuthorizedKey = &infraEnv.Spec.SSHAuthorizedKey
	}

	pullSecret, err := r.PullSecretHandler.GetValidPullSecret(ctx, getPullSecretKey(infraEnv.Namespace, infraEnv.Spec.PullSecretRef))
	if err != nil {
		log.WithError(err).Error("failed to get pull secret")
		return nil, err
	}
	updateParams.InfraEnvUpdateParams.PullSecret = pullSecret

	staticNetworkConfig, err := r.processNMStateConfig(ctx, log, infraEnv)
	if err != nil {
		return nil, err
	}
	if len(staticNetworkConfig) > 0 {
		log.Infof("the amount of nmStateConfigs included in the image is: %d", len(staticNetworkConfig))
		updateParams.InfraEnvUpdateParams.StaticNetworkConfig = staticNetworkConfig
	}

	updateParams.InfraEnvUpdateParams.ImageType = r.Config.ImageType

	existingKargs, err := kubeKernelArgs(internalInfraEnv)
	if err != nil {
		return nil, err
	}
	if !funk.Equal(infraEnv.Spec.KernelArguments, existingKargs) {
		updateParams.InfraEnvUpdateParams.KernelArguments = internalKernelArgs(infraEnv.Spec.KernelArguments)
	}

	// UpdateInfraEnvInternal will generate an ISO only if there it was not generated before,
	return r.Installer.UpdateInfraEnvInternal(ctx, updateParams, nil)
}

func kubeKernelArgs(internalInfraEnv *common.InfraEnv) ([]aiv1beta1.KernelArgument, error) {
	if internalInfraEnv == nil {
		return nil, errors.New("kubeKernelArgs: nil infra-env argument received")
	}
	var args models.KernelArguments
	if internalInfraEnv.KernelArguments != nil {
		if err := json.Unmarshal([]byte(swag.StringValue(internalInfraEnv.KernelArguments)), &args); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal discovery kernel arguments of infra-env %s", internalInfraEnv.ID.String())
		}
	}
	var ret []aiv1beta1.KernelArgument
	for _, arg := range args {
		ret = append(ret, aiv1beta1.KernelArgument{
			Operation: arg.Operation,
			Value:     arg.Value,
		})
	}
	return ret, nil
}

func internalKernelArgs(kargs []aiv1beta1.KernelArgument) models.KernelArguments {
	var ret models.KernelArguments
	for _, arg := range kargs {
		ret = append(ret, &models.KernelArgument{
			Operation: arg.Operation,
			Value:     arg.Value,
		})
	}
	return ret
}

func BuildMacInterfaceMap(log logrus.FieldLogger, nmStateConfig aiv1beta1.NMStateConfig) models.MacInterfaceMap {
	macInterfaceMap := make(models.MacInterfaceMap, 0, len(nmStateConfig.Spec.Interfaces))
	for _, cfg := range nmStateConfig.Spec.Interfaces {
		log.Debugf("adding MAC interface map to host static network config - Name: %s, MacAddress: %s ,",
			cfg.Name, cfg.MacAddress)
		macInterfaceMap = append(macInterfaceMap, &models.MacInterfaceMapItems0{
			MacAddress:     cfg.MacAddress,
			LogicalNicName: cfg.Name,
		})
	}
	return macInterfaceMap
}

func (r *InfraEnvReconciler) processNMStateConfig(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv) ([]*models.HostStaticNetworkConfig, error) {
	var staticNetworkConfig []*models.HostStaticNetworkConfig
	var selector labels.Selector
	var err error

	//Find all NMStateConfig objects that are associated with the input infraEnv
	selector, err = metav1.LabelSelectorAsSelector(&infraEnv.Spec.NMStateConfigLabelSelector)
	if err != nil {
		return staticNetworkConfig, errors.Wrapf(err, "invalid label selector for InfraEnv %v", infraEnv)
	}

	if selector.Empty() {
		// If the user didn't specify any labels, it's probably because they don't want any NMStateConfigs,
		// not because they want *all of them*, so we return an empty slice.
		return staticNetworkConfig, nil
	}

	nmStateConfigs := &aiv1beta1.NMStateConfigList{}
	if err = r.List(ctx, nmStateConfigs, &client.ListOptions{LabelSelector: selector}); err != nil {
		return staticNetworkConfig, errors.Wrapf(err, "failed to list nmstate configs for InfraEnv %v", infraEnv)
	}

	for _, nmStateConfig := range nmStateConfigs.Items {
		staticNetworkConfig = append(staticNetworkConfig, &models.HostStaticNetworkConfig{
			MacInterfaceMap: BuildMacInterfaceMap(log, nmStateConfig),
			NetworkYaml:     string(nmStateConfig.Spec.NetConfig.Raw),
		})
	}
	return staticNetworkConfig, nil
}

// ensureISO generates ISO for the cluster if needed and will update the condition Reason and Message accordingly.
// It returns a result that includes ISODownloadURL.
func (r *InfraEnvReconciler) ensureISO(ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv) (ctrl.Result, error) {
	infraEnv.Status.AgentLabelSelector = metav1.LabelSelector{MatchLabels: map[string]string{aiv1beta1.InfraEnvNameLabel: infraEnv.Name}}
	var inventoryErr, err error
	var Requeue bool
	var cluster *common.Cluster

	if infraEnv.Spec.ClusterRef != nil {
		kubeKey := types.NamespacedName{
			Name:      infraEnv.Spec.ClusterRef.Name,
			Namespace: infraEnv.Spec.ClusterRef.Namespace,
		}
		clusterDeployment := &hivev1.ClusterDeployment{}

		// Retrieve clusterDeployment
		if err = r.Get(ctx, kubeKey, clusterDeployment); err != nil {
			errMsg := fmt.Sprintf("failed to get clusterDeployment with name %s in namespace %s",
				infraEnv.Spec.ClusterRef.Name, infraEnv.Spec.ClusterRef.Namespace)
			Requeue = false
			clientError := true
			if !k8serrors.IsNotFound(err) {
				Requeue = true
				clientError = false
			}
			clusterDeploymentRefErr := newKubeAPIError(errors.Wrapf(err, errMsg), clientError)

			// Update that we failed to retrieve the clusterDeployment
			conditionsv1.SetStatusConditionNoHeartbeat(&infraEnv.Status.Conditions, conditionsv1.Condition{
				Type:    aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionUnknown,
				Reason:  aiv1beta1.ImageCreationErrorReason,
				Message: aiv1beta1.ImageStateFailedToCreate + ": " + clusterDeploymentRefErr.Error(),
			})
			if updateErr := r.Status().Update(ctx, infraEnv); updateErr != nil {
				log.WithError(updateErr).Error("failed to update infraEnv status")
			}
			return ctrl.Result{Requeue: Requeue}, nil
		}

		// Retrieve cluster from the database
		cluster, err = r.Installer.GetClusterByKubeKey(types.NamespacedName{
			Name:      clusterDeployment.Name,
			Namespace: clusterDeployment.Namespace,
		})
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				Requeue = true
				msg := fmt.Sprintf("cluster does not exist: %s, ", clusterDeployment.Name)
				if clusterDeployment.Spec.ClusterInstallRef == nil {
					msg += "AgentClusterInstall is not defined in ClusterDeployment"
				} else {
					msg += fmt.Sprintf("check AgentClusterInstall conditions: name %s in namespace %s",
						clusterDeployment.Spec.ClusterInstallRef.Name, clusterDeployment.Namespace)
				}
				log.Errorf(msg)
				err = errors.Errorf(msg)

				inventoryErr = common.NewApiError(http.StatusNotFound, err)
			} else {
				Requeue = false
				inventoryErr = common.NewApiError(http.StatusInternalServerError, err)
			}
			// Update that we failed to retrieve the cluster from the database
			conditionsv1.SetStatusConditionNoHeartbeat(&infraEnv.Status.Conditions, conditionsv1.Condition{
				Type:    aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionUnknown,
				Reason:  aiv1beta1.ImageCreationErrorReason,
				Message: aiv1beta1.ImageStateFailedToCreate + ": " + inventoryErr.Error(),
			})
			if updateErr := r.Status().Update(ctx, infraEnv); updateErr != nil {
				log.WithError(updateErr).Error("failed to update infraEnv status")
			}
			return ctrl.Result{Requeue: Requeue}, nil
		}
	}

	// Retrieve infraenv from the database
	key := types.NamespacedName{
		Name:      infraEnv.Name,
		Namespace: infraEnv.Namespace,
	}
	infraEnvInternal, err := r.Installer.GetInfraEnvByKubeKey(key)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var clusterID *strfmt.UUID
			var openshiftVersion string
			if cluster != nil {
				clusterID = cluster.ID
				openshiftVersion = cluster.OpenshiftVersion
			}
			infraEnvInternal, err = r.createInfraEnv(ctx, log, &key, infraEnv, clusterID, openshiftVersion)
			if err != nil {
				log.Errorf("fail to create InfraEnv: %s, ", infraEnv.Name)
				return r.handleEnsureISOErrors(ctx, log, infraEnv, err, nil)
			} else {
				return r.updateInfraEnvStatus(ctx, log, infraEnv, infraEnvInternal)
			}
		} else {
			return r.handleEnsureISOErrors(ctx, log, infraEnv, err, infraEnvInternal)
		}
	}

	// Check for updates from user and update the infraenv
	updatedInfraEnv, err := r.updateInfraEnv(ctx, log, infraEnv, infraEnvInternal)
	if err != nil {
		log.WithError(err).Error("failed to update InfraEnv")
		return r.handleEnsureISOErrors(ctx, log, infraEnv, err, infraEnvInternal)
	}

	return r.updateInfraEnvStatus(ctx, log, infraEnv, updatedInfraEnv)
}

func CreateInfraEnvParams(infraEnv *aiv1beta1.InfraEnv, imageType models.ImageType, pullSecret string, clusterID *strfmt.UUID, openshiftVersion string) installer.RegisterInfraEnvParams {
	createParams := installer.RegisterInfraEnvParams{
		InfraenvCreateParams: &models.InfraEnvCreateParams{
			Name:                   &infraEnv.Name,
			ImageType:              imageType,
			IgnitionConfigOverride: infraEnv.Spec.IgnitionConfigOverride,
			PullSecret:             &pullSecret,
			SSHAuthorizedKey:       &infraEnv.Spec.SSHAuthorizedKey,
			CPUArchitecture:        infraEnv.Spec.CpuArchitecture,
			ClusterID:              clusterID,
			OpenshiftVersion:       openshiftVersion,
		},
	}
	if infraEnv.Spec.Proxy != nil {
		proxy := &models.Proxy{
			HTTPProxy:  &infraEnv.Spec.Proxy.HTTPProxy,
			HTTPSProxy: &infraEnv.Spec.Proxy.HTTPSProxy,
			NoProxy:    &infraEnv.Spec.Proxy.NoProxy,
		}
		createParams.InfraenvCreateParams.Proxy = proxy
	}

	if len(infraEnv.Spec.AdditionalNTPSources) > 0 {
		createParams.InfraenvCreateParams.AdditionalNtpSources = swag.String(strings.Join(infraEnv.Spec.AdditionalNTPSources[:], ","))
	}

	if len(infraEnv.Spec.KernelArguments) > 0 {
		createParams.InfraenvCreateParams.KernelArguments = internalKernelArgs(infraEnv.Spec.KernelArguments)
	}

	return createParams
}

func (r *InfraEnvReconciler) createInfraEnv(ctx context.Context, log logrus.FieldLogger, key *types.NamespacedName,
	infraEnv *aiv1beta1.InfraEnv, clusterID *strfmt.UUID, openshiftVersion string) (*common.InfraEnv, error) {

	pullSecret, err := r.PullSecretHandler.GetValidPullSecret(ctx, getPullSecretKey(infraEnv.Namespace, infraEnv.Spec.PullSecretRef))
	if err != nil {
		log.WithError(err).Error("failed to get pull secret")
		return nil, err
	}

	createParams := CreateInfraEnvParams(infraEnv, r.Config.ImageType, pullSecret, clusterID, openshiftVersion)

	staticNetworkConfig, err := r.processNMStateConfig(ctx, log, infraEnv)
	if err != nil {
		return nil, err
	}
	if len(staticNetworkConfig) > 0 {
		log.Infof("the amount of nmStateConfigs included in the image is: %d", len(staticNetworkConfig))
		createParams.InfraenvCreateParams.StaticNetworkConfig = staticNetworkConfig
	}

	return r.Installer.RegisterInfraEnvInternal(ctx, key, createParams)
}

func (r *InfraEnvReconciler) deregisterInfraEnvIfNeeded(ctx context.Context, log logrus.FieldLogger, key types.NamespacedName) (ctrl.Result, error) {

	buildReply := func(err error) (ctrl.Result, error) {
		reply := ctrl.Result{}
		if err == nil {
			return reply, nil
		}
		reply.RequeueAfter = defaultRequeueAfterOnError
		err = errors.Wrapf(err, "failed to deregister infraenv: %s", key.Name)
		log.Error(err)
		return reply, err
	}

	infraEnv, err := r.Installer.GetInfraEnvByKubeKey(key)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return if from any reason infraEnv is already deleted from db (or never existed)
		return buildReply(nil)
	}

	if err != nil {
		return buildReply(err)
	}

	if err = r.Installer.DeregisterInfraEnvInternal(ctx, installer.DeregisterInfraEnvParams{
		InfraEnvID: *infraEnv.ID,
	}); err != nil {
		return buildReply(err)
	}
	log.Infof("InfraEnv resource deleted : %s", infraEnv.ID)

	return buildReply(nil)
}

func (r *InfraEnvReconciler) deregisterInfraEnvWithHosts(ctx context.Context, log logrus.FieldLogger, key types.NamespacedName) error {
	infraEnv, err := r.Installer.GetInfraEnvByKubeKey(key)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// return if from any reason infraEnv is already deleted from db (or never existed)
		return nil
	}

	if err != nil {
		return err
	}
	allowedStatuses := []string{
		models.HostStatusInsufficientUnbound,
		models.HostStatusDisconnectedUnbound,
		models.HostStatusDiscoveringUnbound,
		models.HostStatusKnownUnbound,
		models.HostStatusInstalled,
		models.HostStatusAddedToExistingCluster,
		models.HostStatusUnbinding,
		models.HostStatusUnbindingPendingUserAction,
	}
	hosts, err := r.Installer.GetInfraEnvHostsInternal(ctx, *infraEnv.ID)
	if err != nil {
		return err
	}
	remainingHost := false
	for _, h := range hosts {
		status := swag.StringValue(h.Status)
		if funk.ContainsString(allowedStatuses, status) {
			hostId := *h.ID
			err = r.Installer.V2DeregisterHostInternal(
				ctx, installer.V2DeregisterHostParams{
					InfraEnvID: h.InfraEnvID,
					HostID:     hostId,
				})

			if err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					log.WithError(err).Errorf("failed to deregister host%s", *h.ID)
					return err
				}
			}
		} else {
			remainingHost = true
			log.Infof("Skipping host deletion : %s, Status: %s", *h.ID, status)
		}
	}

	if !remainingHost {
		if err = r.Installer.DeregisterInfraEnvInternal(ctx, installer.DeregisterInfraEnvParams{
			InfraEnvID: *infraEnv.ID,
		}); err != nil {
			return err
		}
		log.Infof("InfraEnv resource deleted : %s", infraEnv.ID)
	} else {
		errReply := errors.New("Failed to delete infraEnv, existing hosts bound and not installed")
		return errReply
	}

	return nil
}

func (r *InfraEnvReconciler) populateEventsURL(log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv, internalInfraEnv *common.InfraEnv) error {
	id := internalInfraEnv.ID.String()
	tokenGen := gencrypto.CryptoPair{JWTKeyType: gencrypto.InfraEnvKey, JWTKeyValue: id}
	eventUrl, err := generateEventsURL(r.ServiceBaseURL, r.AuthType, tokenGen, "infra_env_id", id)
	if err != nil {
		log.WithError(err).Error("failed to generate Events URL")
		return err
	}
	infraEnv.Status.InfraEnvDebugInfo.EventsURL = eventUrl
	return nil
}

func (r *InfraEnvReconciler) setSignedBootArtifactURLs(infraEnv *aiv1beta1.InfraEnv, initrdURL, infraEnvID, version, arch string) error {
	signedInitrdURL, err := signURL(initrdURL, r.AuthType, infraEnvID, gencrypto.InfraEnvKey)
	if err != nil {
		return err
	}
	infraEnv.Status.BootArtifacts.InitrdURL = signedInitrdURL

	builder := &installer.V2DownloadInfraEnvFilesURL{
		InfraEnvID: strfmt.UUID(infraEnvID),
		FileName:   "ipxe-script",
	}
	if infraEnv.Spec.IPXEScriptType == aiv1beta1.BootOrderControl {
		builder.IpxeScriptType = swag.String(bminventory.BootOrderControl)
	}
	filesURL, err := builder.Build()
	if err != nil {
		return err
	}
	baseURL, err := url.Parse(r.ServiceBaseURL)
	if err != nil {
		return err
	}
	// ASC may be configured to use http in ipxe artifact URLs so that all ipxe clients could consume those
	if r.InsecureIPXEURLs {
		baseURL.Scheme = "http"
	}
	baseURL.Path = path.Join(baseURL.Path, filesURL.Path)
	baseURL.RawQuery = filesURL.RawQuery

	infraEnv.Status.BootArtifacts.IpxeScriptURL, err = signURL(baseURL.String(), r.AuthType, infraEnvID, gencrypto.InfraEnvKey)
	if err != nil {
		return err
	}

	return nil
}

func (r *InfraEnvReconciler) initrdSchemeChanged(initrdURL string) (bool, error) {
	u, err := url.Parse(initrdURL)
	if err != nil {
		return false, err
	}
	desiredScheme := "https"
	if r.InsecureIPXEURLs {
		desiredScheme = "http"
	}
	return u.Scheme != desiredScheme, nil
}

func (r *InfraEnvReconciler) updateInfraEnvStatus(
	ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv, internalInfraEnv *common.InfraEnv) (ctrl.Result, error) {

	osImage, err := r.VersionsHandler.GetOsImageOrLatest(internalInfraEnv.OpenshiftVersion, internalInfraEnv.CPUArchitecture)
	if err != nil {
		return r.handleEnsureISOErrors(ctx, log, infraEnv, err, internalInfraEnv)
	}

	bootArtifactURLs, err := imageservice.GetBootArtifactURLs(r.ImageServiceBaseURL, internalInfraEnv.ID.String(), osImage, r.InsecureIPXEURLs)
	if err != nil {
		return r.handleEnsureISOErrors(ctx, log, infraEnv, err, internalInfraEnv)
	}
	infraEnv.Status.BootArtifacts.KernelURL = bootArtifactURLs.KernelURL
	infraEnv.Status.BootArtifacts.RootfsURL = bootArtifactURLs.RootFSURL

	var isoUpdated bool
	if infraEnv.Status.ISODownloadURL != internalInfraEnv.DownloadURL {
		log.Infof("ISODownloadURL changed from %s to %s", infraEnv.Status.ISODownloadURL, internalInfraEnv.DownloadURL)
		infraEnv.Status.ISODownloadURL = internalInfraEnv.DownloadURL
		imageCreatedAt := metav1.NewTime(time.Time(internalInfraEnv.GeneratedAt))
		infraEnv.Status.CreatedTime = &imageCreatedAt
		isoUpdated = true
	}

	// update boot artifacts URL if IPXE insecure setting was changed or if the ISO was updated
	schemeUpdated, err := r.initrdSchemeChanged(infraEnv.Status.BootArtifacts.InitrdURL)
	if err != nil {
		return r.handleEnsureISOErrors(ctx, log, infraEnv, err, internalInfraEnv)
	}
	if schemeUpdated || isoUpdated {
		if err := r.setSignedBootArtifactURLs(infraEnv, bootArtifactURLs.InitrdURL, internalInfraEnv.ID.String(), *osImage.OpenshiftVersion, *osImage.CPUArchitecture); err != nil {
			return r.handleEnsureISOErrors(ctx, log, infraEnv, err, internalInfraEnv)
		}
	}

	if infraEnv.Status.InfraEnvDebugInfo.EventsURL == "" {
		if r.populateEventsURL(log, infraEnv, internalInfraEnv) != nil {
			return ctrl.Result{Requeue: true}, nil
		}
	}

	conditionsv1.SetStatusConditionNoHeartbeat(&infraEnv.Status.Conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ImageCreatedCondition,
		Status:  corev1.ConditionTrue,
		Reason:  aiv1beta1.ImageCreatedReason,
		Message: aiv1beta1.ImageStateCreated,
	})

	if updateErr := r.Status().Update(ctx, infraEnv); updateErr != nil {
		log.WithError(updateErr).Error("failed to update infraEnv status")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{Requeue: false}, nil
}

func (r *InfraEnvReconciler) handleEnsureISOErrors(
	ctx context.Context, log logrus.FieldLogger, infraEnv *aiv1beta1.InfraEnv, err error, internalInfraEnv *common.InfraEnv) (ctrl.Result, error) {
	var (
		currentReason               = ""
		RequeueAfter  time.Duration = 0
		errMsg        string
		Requeue       bool
	)

	if internalInfraEnv != nil {
		if infraEnv.Status.InfraEnvDebugInfo.EventsURL == "" {
			if r.populateEventsURL(log, infraEnv, internalInfraEnv) != nil {
				return ctrl.Result{Requeue: true}, nil
			}
		}
	}

	// TODO: Checking currentCondition as a workaround until MGMT-4695 get resolved.
	// If the current condition is in an error state, avoid clearing it up.
	if currentCondition := conditionsv1.FindStatusCondition(infraEnv.Status.Conditions, aiv1beta1.ImageCreatedCondition); currentCondition != nil {
		currentReason = currentCondition.Reason
	}
	if imageBeingCreated(err) {
		Requeue = true
		RequeueAfter = defaultRequeueAfterPerRecoverableError
		err = nil                                                // clear up the error so it will requeue with RequeueAfter we set
		if currentReason != aiv1beta1.ImageCreationErrorReason { // Not an actual error, just an image generation in progress.
			log.Infof("Image %s being prepared", infraEnv.Name)
			conditionsv1.SetStatusConditionNoHeartbeat(&infraEnv.Status.Conditions, conditionsv1.Condition{
				Type:    aiv1beta1.ImageCreatedCondition,
				Status:  corev1.ConditionTrue,
				Reason:  aiv1beta1.ImageCreatedReason,
				Message: aiv1beta1.ImageStateCreated,
			})
		}
	} else { // Actual errors
		log.WithError(err).Error("infraEnv reconcile failed")
		if isClientError(err) { // errors it can't recover from
			Requeue = false
			errMsg = ": " + err.Error()
			err = nil // clear the error, to avoid requeue.
		} else { // errors it may recover from
			Requeue = true
			RequeueAfter = defaultRequeueAfterPerRecoverableError
			errMsg = " due to an internal error: " + err.Error()
		}
		conditionsv1.SetStatusConditionNoHeartbeat(&infraEnv.Status.Conditions, conditionsv1.Condition{
			Type:    aiv1beta1.ImageCreatedCondition,
			Status:  corev1.ConditionFalse,
			Reason:  aiv1beta1.ImageCreationErrorReason,
			Message: aiv1beta1.ImageStateFailedToCreate + errMsg,
		})
		// In a case of an error, clear the download URL.
		log.Debugf("cleanup up ISODownloadURL due to %s", errMsg)
		infraEnv.Status.ISODownloadURL = ""
		infraEnv.Status.CreatedTime = nil
	}

	if updateErr := r.Status().Update(ctx, infraEnv); updateErr != nil {
		log.WithError(updateErr).Error("failed to update infraEnv status")
	}
	return ctrl.Result{Requeue: Requeue, RequeueAfter: RequeueAfter}, err
}

func imageBeingCreated(err error) bool {
	return IsHTTPError(err, http.StatusConflict)
}

func (r *InfraEnvReconciler) SetupWithManager(mgr ctrl.Manager) error {
	mapNMStateConfigToInfraEnv := func(a client.Object) []reconcile.Request {
		log := logutil.FromContext(context.Background(), r.Log).WithFields(
			logrus.Fields{
				"nmstate_config":           a.GetName(),
				"nmstate_config_namespace": a.GetNamespace(),
			})
		infraEnvs := &aiv1beta1.InfraEnvList{}
		if len(a.GetLabels()) == 0 {
			log.Debugf("NMState config: %s has no labels", a.GetName())
			return []reconcile.Request{}
		}
		if err := r.List(context.Background(), infraEnvs, client.InNamespace(a.GetNamespace())); err != nil {
			log.Debugf("failed to list InfraEnvs")
			return []reconcile.Request{}
		}

		reply := make([]reconcile.Request, 0, len(infraEnvs.Items))
		for labelName, labelValue := range a.GetLabels() {
			log.Debugf("Detected NMState config with label name: %s with value %s, about to search for a matching InfraEnv",
				labelName, labelValue)
			for _, infraEnv := range infraEnvs.Items {
				if infraEnv.Spec.NMStateConfigLabelSelector.MatchLabels[labelName] == labelValue {
					log.Debugf("Detected NMState config for InfraEnv: %s in namespace: %s", infraEnv.Name, infraEnv.Namespace)
					reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
						Namespace: infraEnv.Namespace,
						Name:      infraEnv.Name,
					}})
				}
			}
		}
		return reply
	}

	mapClusterDeploymentToInfraEnv := func(clusterDeployment client.Object) []reconcile.Request {
		log := logutil.FromContext(context.Background(), r.Log).WithFields(
			logrus.Fields{
				"cluster_deployment":           clusterDeployment.GetName(),
				"cluster_deployment_namespace": clusterDeployment.GetNamespace(),
			})
		infraEnvs := &aiv1beta1.InfraEnvList{}
		if err := r.List(context.Background(), infraEnvs); err != nil {
			log.Debugf("failed to list InfraEnvs")
			return []reconcile.Request{}
		}

		reply := make([]reconcile.Request, 0, len(infraEnvs.Items))
		for _, infraEnv := range infraEnvs.Items {
			if infraEnv.Spec.ClusterRef != nil && infraEnv.Spec.ClusterRef.Name == clusterDeployment.GetName() &&
				infraEnv.Spec.ClusterRef.Namespace == clusterDeployment.GetNamespace() {
				reply = append(reply, reconcile.Request{NamespacedName: types.NamespacedName{
					Namespace: infraEnv.Namespace,
					Name:      infraEnv.Name,
				}})
			}
		}
		return reply
	}

	infraEnvUpdates := r.CRDEventsHandler.GetInfraEnvUpdates()
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.InfraEnv{}).
		Watches(&source.Kind{Type: &aiv1beta1.NMStateConfig{}}, handler.EnqueueRequestsFromMapFunc(mapNMStateConfigToInfraEnv)).
		Watches(&source.Kind{Type: &hivev1.ClusterDeployment{}}, handler.EnqueueRequestsFromMapFunc(mapClusterDeploymentToInfraEnv)).
		Watches(&source.Channel{Source: infraEnvUpdates}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
