package controllers

import (
	"context"
	"fmt"

	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/openshift/assisted-service/pkg/localclusterimport"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	agentServiceConfigLocalClusterImportFinalizerName = "agentserviceconfig." + aiv1beta1.Group + "/local-cluster-import-deprovision"
)

var (
	localClusterImport localclusterimport.LocalClusterImport
	hasManagedCluster  bool
)

type LocalClusterImportReconciler struct {
	client                 client.Client
	LocalClusterName       string
	Log                    logrus.FieldLogger
	localclusterimport     localclusterimport.LocalClusterImport
	HasLocalManagedCluster bool
}

func NewLocalClusterImportReconciler(client client.Client, localClusterName string, log logrus.FieldLogger) (LocalClusterImportReconciler, error) {
	localClusterImportOperations := localclusterimport.NewLocalClusterImportOperations(client, AgentServiceConfigName)
	localClusterImport = localclusterimport.NewLocalClusterImport(&localClusterImportOperations, localClusterName, localClusterName, log)
	reconciler := LocalClusterImportReconciler{
		localclusterimport: localClusterImport,
		client:             client,
		LocalClusterName:   "local-cluster",
		Log:                log,
	}
	var err error
	reconciler.HasLocalManagedCluster, err = localClusterImport.HasLocalManagedCluster()
	if err != nil {
		log.Errorf("Could not determine status of managed cluster due to error %s", err.Error())
		return reconciler, err
	}
	return reconciler, nil
}

func (r *LocalClusterImportReconciler) setReconciliationStatus(asc *ASC, completed bool, reason string, message string) error {
	status := corev1.ConditionFalse
	if completed {
		status = corev1.ConditionTrue
	}
	conditionsv1.SetStatusConditionNoHeartbeat(asc.conditions, conditionsv1.Condition{
		Type:    aiv1beta1.ConditionLocalClusterManaged,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
	err := r.client.Status().Update(context.Background(), asc.Object)
	if err != nil {
		r.Log.Errorf("Unable to update status of ASC while attempting to set condition %s", aiv1beta1.ConditionReconcileCompleted)
		return err
	}
	return nil
}

// +kubebuilder:rbac:groups=config.openshift.io,resources=dnses,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=proxies,verbs=get;list;watch

func (r *LocalClusterImportReconciler) Reconcile(origCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var asc ASC
	ctx := addRequestIdIfNeeded(origCtx)
	defer func() {
		r.Log.Info("AgentServiceConfig (LocalClusterImport) Reconcile ended")
	}()

	r.Log.Info("AgentServiceConfig (LocalClusterImport) Reconcile started")

	instance := &aiv1beta1.AgentServiceConfig{}
	asc.Client = r.client
	asc.Object = instance
	asc.spec = &instance.Spec
	asc.conditions = &instance.Status.Conditions

	// NOTE: ignoring the Namespace that seems to get set on request when syncing on namespaced objects
	// when our AgentServiceConfig is ClusterScoped.
	if err := r.client.Get(ctx, types.NamespacedName{Name: req.NamespacedName.Name}, instance); err != nil {
		r.Log.Errorf("unable to fetch AgentServiceConfig %s due to error %s", req.NamespacedName.Name, err.Error())
		if errors.IsNotFound(err) {
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if asc.Object.GetDeletionTimestamp().IsZero() {
		if err := r.handleManagedCluster(); err != nil {
			r.Log.WithError(err).Error("error while handling managed cluster")
			return ctrl.Result{Requeue: true}, err
		}
	}

	err := r.ensureFinalizers(ctx, asc, agentServiceConfigLocalClusterImportFinalizerName)
	if err != nil {
		r.Log.Errorf("Could not apply finalizers on local cluster import due to error %s", err.Error())
		_ = r.setReconciliationStatus(&asc, false, string(aiv1beta1.ReasonLocalClusterCRsNotRegistered), fmt.Sprintf("An error occurred: %s", err.Error()))
		return ctrl.Result{Requeue: true}, err
	}
	_ = r.setReconciliationStatus(&asc, true, string(aiv1beta1.ReasonLocalClusterCRsRegistered), "LocalClusterImport reconcile completed without error.")
	return ctrl.Result{}, err
}

func (r *LocalClusterImportReconciler) handleManagedCluster() error {
	if hasManagedCluster {
		err := localClusterImport.ImportLocalCluster()
		if err != nil {
			// Failure to import the local cluster is not fatal but we should warn in the log.
			r.Log.Warnf("failed to create managed cluster CRs %s", err.Error())
		}
		return nil
	}
	err := localClusterImport.DeleteLocalClusterImportCRs()
	if err != nil {
		// Failed to clean up the local cluster import CR's
		r.Log.Warnf("Could not clean up CR's for local cluster import due to error %s", err.Error())
	}
	return nil
}

func (r *LocalClusterImportReconciler) ensureFinalizers(ctx context.Context, asc ASC, finalizerName string) error {
	if asc.Object.GetDeletionTimestamp().IsZero() {
		r.Log.Infof("Zero deletion timestamp")
		// Check to see if the ASC has our finalizer, if so, we must add it
		if !controllerutil.ContainsFinalizer(asc.Object, finalizerName) {
			controllerutil.AddFinalizer(asc.Object, finalizerName)
			if err := asc.Client.Update(ctx, asc.Object); err != nil {
				r.Log.WithError(err).Error("failed to add finalizer to AgentServiceConfig")
				return err
			}
		}
	} else {
		r.Log.Infof("Non zero deletion timestamp")
		err := localClusterImport.DeleteLocalClusterImportCRs()
		if err != nil {
			// Failed to clean up the local cluster import CR's
			r.Log.Warnf("Could not clean up CR's for local cluster import due to error %s", err.Error())
		}
		controllerutil.RemoveFinalizer(asc.Object, finalizerName)
		if err := asc.Client.Update(ctx, asc.Object); err != nil {
			r.Log.WithError(err).Error("failed to remove finalizer from AgentServiceConfig")
			return err
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LocalClusterImportReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&aiv1beta1.AgentServiceConfig{}).
		Watches(&clusterv1.ManagedCluster{},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
