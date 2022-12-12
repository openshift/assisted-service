package provisioning

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
)

const (
	imageCustomizationService = "metal3-image-customization-service"
)

func newImageCustomizationService(targetNamespace string) *corev1.Service {
	ports := []corev1.ServicePort{
		{
			Name:       "http",
			Port:       80,
			TargetPort: intstr.FromInt(imageCustomizationPort),
		},
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageCustomizationService,
			Namespace: targetNamespace,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				cboLabelName: imageCustomizationService,
			},
			Ports: ports,
		},
	}
}

func EnsureImageCustomizationService(info *ProvisioningInfo) (updated bool, err error) {
	imageCustomizationService := newImageCustomizationService(info.Namespace)

	err = controllerutil.SetControllerReference(info.ProvConfig, imageCustomizationService, info.Scheme)
	if err != nil {
		err = fmt.Errorf("unable to set controllerReference on %s service: %w", imageCustomizationService, err)
		return
	}

	_, updated, err = resourceapply.ApplyService(context.Background(),
		info.Client.CoreV1(), info.EventRecorder, imageCustomizationService)
	if err != nil {
		err = fmt.Errorf("unable to apply %s service: %w", imageCustomizationService, err)
	}
	return
}

func DeleteImageCustomizationService(info *ProvisioningInfo) error {
	return client.IgnoreNotFound(info.Client.CoreV1().Services(info.Namespace).Delete(context.Background(), imageCustomizationService, metav1.DeleteOptions{}))
}
