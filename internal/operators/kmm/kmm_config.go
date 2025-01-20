package kmm

type Config struct {
	// TODO: Currently we use the controller image to run the setup tools because all we need is the shell and the
	// `oc` command, and that way we don't need an additional image. But in the future we will probably want to have
	// a separate image that contains the things that we need to run these setup jobs.
	ControllerImage string `envconfig:"CONTROLLER_IMAGE" default:"quay.io/edge-infrastructure/assisted-installer-controller:latest"`
}
