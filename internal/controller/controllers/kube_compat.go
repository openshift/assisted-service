package controllers

import (
	"context"
	"fmt"

	certtypes "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	aiv1beta1 "github.com/openshift/assisted-service/api/v1beta1"
	"github.com/sirupsen/logrus"
	netv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	caCommonName                     = "Assisted Installer CA"
	caIssuerName                     = "assisted-installer-ca"
	certManagerCAInjectionAnnotation = "cert-manager.io/inject-ca-from"
	clusterVersionCRDName            = "clusterversions.config.openshift.io"
	selfSignedIssuerName             = "assisted-installer-selfsigned-ca"
)

func ServerIsOpenShift(ctx context.Context, c client.Client) (bool, error) {
	clusterVersionCRD := apiextensionsv1.CustomResourceDefinition{}
	err := c.Get(ctx, types.NamespacedName{Name: clusterVersionCRDName}, &clusterVersionCRD)
	if err == nil {
		return true, nil
	}
	return false, client.IgnoreNotFound(err)
}

func newIngress(ctx context.Context, log logrus.FieldLogger, asc ASC, name string, host string, port int32) (client.Object, controllerutil.MutateFn, error) {
	if asc.spec.Ingress == nil {
		return nil, nil, fmt.Errorf("ingress config is required for non-OpenShift deployments")
	}
	pathTypePrefix := netv1.PathTypePrefix
	ingress := &netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, ingress, asc.rec.Scheme); err != nil {
			return err
		}
		ingress.Spec = netv1.IngressSpec{
			IngressClassName: asc.spec.Ingress.ClassName,
			Rules: []netv1.IngressRule{{
				Host: host,
				IngressRuleValue: netv1.IngressRuleValue{HTTP: &netv1.HTTPIngressRuleValue{
					Paths: []netv1.HTTPIngressPath{{
						Path:     "/",
						PathType: &pathTypePrefix,
						Backend: netv1.IngressBackend{
							Service: &netv1.IngressServiceBackend{
								Name: name,
								Port: netv1.ServiceBackendPort{
									Number: port,
								},
							},
						},
					}},
				}},
			}},
		}
		return nil
	}

	return ingress, mutateFn, nil
}

func certManagerComponents() []component {
	return []component{
		{"SelfSignedIssuer", aiv1beta1.ReasonCertificateFailure, newSelfSignedIssuer},
		{"CAIssuer", aiv1beta1.ReasonCertificateFailure, newCAIssuer},
		{"CACert", aiv1beta1.ReasonCertificateFailure, newCACert},
		{"WebhookCert", aiv1beta1.ReasonCertificateFailure, newWebhookCert},
	}
}

// newSelfSignedIssuer describes how to create an Issuer for the self-signed root issuer
func newSelfSignedIssuer(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	is := &certtypes.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      selfSignedIssuerName,
			Namespace: asc.namespace,
		},
	}
	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, is, asc.rec.Scheme); err != nil {
			return err
		}
		is.Spec = certtypes.IssuerSpec{
			IssuerConfig: certtypes.IssuerConfig{
				SelfSigned: &certtypes.SelfSignedIssuer{},
			},
		}
		return nil
	}
	return is, mutateFn, nil
}

// newCACert describes how to create a Certificate for the root CA
func newCACert(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	cert := &certtypes.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      caIssuerName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, cert, asc.rec.Scheme); err != nil {
			return err
		}
		cert.Spec.CommonName = caCommonName
		cert.Spec.IsCA = true
		cert.Spec.SecretName = caIssuerName
		cert.Spec.IssuerRef = certmeta.ObjectReference{
			Kind: "Issuer",
			Name: selfSignedIssuerName,
		}
		return nil
	}

	return cert, mutateFn, nil
}

// newCAIssuer describes how to create an Issuer for creating certificates signed by the root CA cert
func newCAIssuer(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	is := &certtypes.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      caIssuerName,
			Namespace: asc.namespace,
		},
	}
	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, is, asc.rec.Scheme); err != nil {
			return err
		}
		is.Spec = certtypes.IssuerSpec{
			IssuerConfig: certtypes.IssuerConfig{
				CA: &certtypes.CAIssuer{
					SecretName: caIssuerName,
				},
			},
		}
		return nil
	}

	return is, mutateFn, nil
}

// newCertificate describes how to create a Certificate for a service signed by the assisted installer CA
func newCertificate(serviceName string, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	cert := &certtypes.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: asc.namespace,
		},
	}

	mutateFn := func() error {
		if err := controllerutil.SetControllerReference(asc.Object, cert, asc.rec.Scheme); err != nil {
			return err
		}
		cert.Spec.SecretName = serviceName
		cert.Spec.IssuerRef = certmeta.ObjectReference{
			Kind: "Issuer",
			Name: caIssuerName,
		}
		cert.Spec.DNSNames = []string{
			fmt.Sprintf("%s.%s.svc", serviceName, asc.namespace),
			fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, asc.namespace),
		}
		return nil
	}

	return cert, mutateFn, nil
}

func newWebhookCert(ctx context.Context, log logrus.FieldLogger, asc ASC) (client.Object, controllerutil.MutateFn, error) {
	return newCertificate(webhookServiceName, asc)
}
