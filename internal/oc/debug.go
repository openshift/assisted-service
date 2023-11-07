package oc

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/go-errors/errors"
	"github.com/openshift/assisted-service/pkg/executer"
	"golang.org/x/net/context"
)

type Debug interface {
	RebootsForNode(nodeName string) (int, error)
}

type debug struct {
	kubeconfig []byte
	exec       executer.Executer
}

func NewDebug(kubeconfig []byte) Debug {
	return &debug{
		kubeconfig: kubeconfig,
		exec:       &executer.CommonExecuter{},
	}
}

func NewDebugWithExecuter(kubeconfig []byte, exec executer.Executer) Debug {
	return &debug{
		kubeconfig: kubeconfig,
		exec:       exec,
	}
}

func (d *debug) kubeconfigFile() (string, error) {
	var n int

	f, err := os.CreateTemp("", "kubeconfig")
	if err != nil {
		return "", err
	}
	defer f.Close()
	numWritten := 0
	for n, err = f.Write(d.kubeconfig[numWritten:]); err == io.ErrShortWrite; n, err = f.Write(d.kubeconfig[numWritten:]) {
		numWritten += n
	}
	if err != nil {
		return "", err
	}
	return f.Name(), nil
}

func (d *debug) runDebug(entity string, args ...string) (string, string, error) {
	kubeconfigFname, err := d.kubeconfigFile()
	if err != nil {
		return "", "", err
	}
	defer func() {
		_ = os.RemoveAll(kubeconfigFname)
	}()
	execArgs := append([]string{
		"debug",
		"--kubeconfig",
		kubeconfigFname,
		entity,
		"--",
	}, args...)
	execCtx, cancel := context.WithTimeout(context.TODO(), 2*time.Minute)
	defer cancel()
	out, errOut, exitCode := d.exec.ExecuteWithContext(execCtx, "oc", execArgs...)
	if exitCode != 0 {
		return "", "", errors.Errorf("oc debug failed to execute with code %d: %s", exitCode, errOut)
	}
	return out, errOut, nil
}

func (d *debug) RebootsForNode(nodeName string) (int, error) {
	out, _, err := d.runDebug("node/"+nodeName,
		"chroot",
		"/host",
		"last",
		"reboot")
	if err != nil {
		return 0, err
	}
	lines := strings.Split(out, "\n")
	numReboots := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "reboot ") {
			numReboots++
		}
	}
	return numReboots, nil
}
