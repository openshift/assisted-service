package openshiftai

// These requirements have been extracted from this document:
//
//	https://docs.redhat.com/en/documentation/red_hat_openshift_ai_self-managed/2.13/html/installing_and_uninstalling_openshift_ai_self-managed/installing-and-deploying-openshift-ai_install#installing-and-deploying-openshift-ai_install
type Config struct {
	MinWorkerNodes int64 `envconfig:"OPENSHIFT_AI_MIN_WORKER_NODES" default:"2"`
	MinMemoryGiB   int64 `envconfig:"OPENSHIFT_AI_MIN_MEMORY_GIB" default:"32"`
	MinCPUCores    int64 `envconfig:"OPENSHIFT_AI_MIN_CPU_CORES" default:"8"`
}
