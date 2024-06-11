package controllers

import (
	"context"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const clusterVersionCRDName = "clusterversions.config.openshift.io"

func ServerIsOpenShift(ctx context.Context, c client.Client) (bool, error) {
	clusterVersionCRD := apiextensionsv1.CustomResourceDefinition{}
	err := c.Get(ctx, types.NamespacedName{Name: clusterVersionCRDName}, &clusterVersionCRD)
	if err == nil {
		return true, nil
	}
	return false, client.IgnoreNotFound(err)
}
