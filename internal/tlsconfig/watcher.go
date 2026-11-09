package tlsconfig

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	crtls "github.com/openshift/controller-runtime-common/pkg/tls"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// SetupSecurityProfileWatcher registers a watcher that triggers a graceful
// shutdown (via cancel) when the cluster-wide TLS profile or adherence policy
// changes.
func SetupSecurityProfileWatcher(mgr manager.Manager, result TLSConfigResult, cancel context.CancelFunc) error {
	tlsLog := ctrl.Log.WithName("tls-profile-watcher")
	return (&crtls.SecurityProfileWatcher{
		Client:                    mgr.GetClient(),
		InitialTLSAdherencePolicy: result.TLSAdherencePolicy,
		InitialTLSProfileSpec:     result.TLSProfileSpec,
		OnAdherencePolicyChange: func(_ context.Context, oldPolicy, newPolicy configv1.TLSAdherencePolicy) {
			tlsLog.Info("TLS adherence policy changed, shutting down to reload",
				"oldPolicy", oldPolicy, "newPolicy", newPolicy)
			cancel()
		},
		OnProfileChange: func(_ context.Context, oldProfile, newProfile configv1.TLSProfileSpec) {
			if !libgocrypto.ShouldHonorClusterTLSProfile(result.TLSAdherencePolicy) {
				tlsLog.V(1).Info("TLS profile changed but adherence policy does not require honoring, ignoring",
					"policy", result.TLSAdherencePolicy,
					"oldProfile", oldProfile,
					"newProfile", newProfile)
				return
			}
			tlsLog.Info("TLS profile changed, shutting down to reload",
				"oldProfile", oldProfile, "newProfile", newProfile)
			cancel()
		},
	}).SetupWithManager(mgr)
}
