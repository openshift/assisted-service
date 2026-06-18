package patch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sirupsen/logrus"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsEmpty returns true if the patch data contains no meaningful changes.
// With MergeFromWithOptimisticLock, a no-op patch still includes
// {"metadata":{"resourceVersion":"..."}}, so we check for that case too.
func IsEmpty(data []byte) bool {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	if len(m) == 0 {
		return true
	}
	if len(m) == 1 {
		if md, ok := m["metadata"]; ok {
			var mdm map[string]json.RawMessage
			if err := json.Unmarshal(md, &mdm); err != nil {
				return false
			}
			if len(mdm) == 1 {
				_, hasRV := mdm["resourceVersion"]
				return hasRV
			}
			return len(mdm) == 0
		}
	}
	return false
}

// IfNeeded computes the patch diff and applies it only if there are meaningful changes.
// NotFound errors are silently ignored since the object may have been deleted during reconciliation.
func IfNeeded(ctx context.Context, cl client.Client, obj client.Object, p client.Patch, log logrus.FieldLogger) error {
	data, err := p.Data(obj)
	if err != nil {
		return fmt.Errorf("failed to compute patch data: %w", err)
	}
	if IsEmpty(data) {
		return nil
	}
	log.Debugf("Patching %s/%s", obj.GetNamespace(), obj.GetName())
	if err := cl.Patch(ctx, obj, p); err != nil {
		if k8serrors.IsNotFound(err) {
			log.Debugf("Object %s/%s not found during patch, skipping", obj.GetNamespace(), obj.GetName())
			return nil
		}
		return fmt.Errorf("failed to patch object: %w", err)
	}
	return nil
}
