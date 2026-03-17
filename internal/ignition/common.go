package ignition

import (
	"encoding/json"
	"fmt"

	ignitioncommon "github.com/openshift/assisted-service/internal/common/ignition"
	"github.com/openshift/assisted-service/internal/host/hostutil"
	"github.com/openshift/assisted-service/models"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

/*
Contents of the base64 encoded file are:

#!/bin/bash

set -eux
unshare --mount
mount -oremount,rw /sysroot
rpm-ostree cleanup --os=rhcos -r
rpm-ostree cleanup --os=install -r
systemctl stop rpm-ostreed
rm -rf /sysroot/ostree/deploy/rhcos
systemctl start rpm-ostreed
*/
const cleanupDiscoveryStaterootIgnitionOverride = `{
	"ignition": {
	  "version": "3.2.0"
	},
	"storage": {
	  "files": [{
		"overwrite": true,
		"path": "/usr/local/bin/cleanup-assisted-discovery-stateroot.sh",
		"mode": 493,
		"user": {
			"name": "root"
		},
		"contents": { "source": "data:text/plain;charset=utf-8;base64,IyEvYmluL2Jhc2gKCnNldCAtZXV4CnVuc2hhcmUgLS1tb3VudAptb3VudCAtb3JlbW91bnQscncgL3N5c3Jvb3QKcnBtLW9zdHJlZSBjbGVhbnVwIC0tb3M9cmhjb3MgLXIKcnBtLW9zdHJlZSBjbGVhbnVwIC0tb3M9aW5zdGFsbCAtcgpzeXN0ZW1jdGwgc3RvcCBycG0tb3N0cmVlZApybSAtcmYgL3N5c3Jvb3Qvb3N0cmVlL2RlcGxveS9yaGNvcwpzeXN0ZW1jdGwgc3RhcnQgcnBtLW9zdHJlZWQK" }
	  }]
	},
	"systemd": {
	  "units": [
		{
		  "contents": "[Unit]\nDescription=Cleanup Assisted Installer discovery stateroot\nConditionFirstBoot=yes\nConditionPathExists=/sysroot/ostree/deploy/rhcos\nBefore=first-boot-complete.target\nWants=first-boot-complete.target\n\n[Service]\nType=oneshot\nRemainAfterExit=yes\nExecStart=/usr/local/bin/cleanup-assisted-discovery-stateroot.sh\n\n[Install]\nWantedBy=basic.target\n",
		  "enabled": true,
		  "name": "cleanup-assisted-discovery-stateroot.service"
		}
	  ]
	}
  }
  `

func SetHostnameForNodeIgnition(ignition []byte, host *models.Host) ([]byte, error) {
	config, err := ignitioncommon.ParseToLatest(ignition)
	if err != nil {
		return nil, errors.Errorf("error parsing ignition: %v", err)
	}

	hostname, err := hostutil.GetCurrentHostName(host)
	if err != nil {
		return nil, errors.Errorf("failed to get hostname for host %s", host.ID)
	}

	ignitioncommon.SetFileInIgnition(config, "/etc/hostname", fmt.Sprintf("data:,%s", hostname), false, 420, true)

	configBytes, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return configBytes, nil
}

func AddStateRootCleanupToIgnition(log logrus.FieldLogger, ignition []byte, host *models.Host) ([]byte, error) {
	inventory := models.Inventory{}
	if host.Inventory != "" {
		if err := json.Unmarshal([]byte(host.Inventory), &inventory); err != nil {
			return ignition, errors.Wrapf(err, "failed to unmarshal inventory for host %s", host.ID)
		}
	}
	if inventory.Boot != nil && inventory.Boot.DeviceType == models.BootDeviceTypePersistent {
		log.Infof("Adding stateroot cleanup ignition override for host %s", host.ID)
		merged, mergeErr := ignitioncommon.MergeIgnitionConfig(ignition, []byte(cleanupDiscoveryStaterootIgnitionOverride))
		if mergeErr != nil {
			return ignition, errors.Wrapf(mergeErr, "failed to apply stateroot cleanup ignition override for host %s", host.ID)
		}
		return []byte(merged), nil
	}
	return ignition, nil
}
