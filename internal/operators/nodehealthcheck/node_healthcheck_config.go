package nodehealthcheck

type Config struct {
	SelfNodeRemediationTemplateName            string `envconfig:"SELF_NODE_REMEDIATION_TEMPLATE_NAME" default:"self-node-remediation-automatic-strategy-template"`
	MinPercentageOfMatchingNodesForRemediation string `envconfig:"MIN_PERCENTAGE_OF_MATCHING_NODES_FOR_REMEDIATION" default:"51%"`
	UnreadyDuration                            string `envconfig:"NODE_HEALTHCHECK_UNHEALTHY_CONDITIONS_UNREADY_DURATION" default:"300s"`
	UnknownDuration                            string `envconfig:"NODE_HEALTHCHECK_UNHEALTHY_CONDITIONS_UNKNOWN_DURATION" default:"300s"`
}
