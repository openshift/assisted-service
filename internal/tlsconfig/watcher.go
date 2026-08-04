package tlsconfig

import (
	"context"
	"fmt"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	configclientset "github.com/openshift/client-go/config/clientset/versioned"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
)

// WatchForTLSProfileChanges watches the APIServer resource for TLS profile
// and tlsAdherence changes. When a change is detected, it calls the provided
// onChanged callback. The watcher compares spec.tlsSecurityProfile and
// spec.tlsAdherence to avoid unnecessary restarts on unrelated APIServer
// changes (e.g. status updates during node rollouts).
func WatchForTLSProfileChanges(ctx context.Context, restConfig *rest.Config, onChanged func()) error {
	configClient, err := configclientset.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("creating config client for TLS watcher: %w", err)
	}

	apiserver, err := configClient.ConfigV1().APIServers().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("fetching initial APIServer config for TLS watcher: %w", err)
	}

	currentAdherence, err := fetchTLSAdherence(ctx, restConfig)
	if err != nil {
		return fmt.Errorf("fetching initial TLS adherence for watcher: %w", err)
	}

	go watchAPIServer(ctx, configClient, restConfig, apiserver.Spec.TLSSecurityProfile, currentAdherence, onChanged)
	return nil
}

const watchReconnectInterval = 30 * time.Second

func watchAPIServer(ctx context.Context, configClient configclientset.Interface, restConfig *rest.Config, currentProfile *configv1.TLSSecurityProfile, currentAdherence string, onChanged func()) {
	for {
		if ctx.Err() != nil {
			return
		}

		w, err := configClient.ConfigV1().APIServers().Watch(ctx, metav1.ListOptions{
			FieldSelector: "metadata.name=cluster",
		})
		if err != nil {
			log.Error(err, "failed to watch APIServer, retrying")
			select {
			case <-time.After(watchReconnectInterval):
				continue
			case <-ctx.Done():
				return
			}
		}

		for event := range w.ResultChan() {
			if event.Type != watch.Modified {
				continue
			}
			updated, ok := event.Object.(*configv1.APIServer)
			if !ok {
				continue
			}

			profileChanged := !equality.Semantic.DeepEqual(currentProfile, updated.Spec.TLSSecurityProfile)

			adherenceChanged := false
			newAdherence, adhErr := fetchTLSAdherence(ctx, restConfig)
			if adhErr != nil {
				log.Error(adhErr, "unable to fetch TLS adherence, assuming unchanged")
			} else {
				adherenceChanged = newAdherence != currentAdherence
			}

			if !profileChanged && !adherenceChanged {
				continue
			}
			log.Info("APIServer TLS configuration changed, shutting down to reload")
			onChanged()
			return
		}

		w.Stop()
		if ctx.Err() != nil {
			return
		}
		log.Info("watch on APIServer connection closed, reconnecting")
	}
}
