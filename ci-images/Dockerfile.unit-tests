FROM base

RUN dnf install -y postgresql-server

RUN cd / && /assisted-service/hack/setup_env.sh test_tools
