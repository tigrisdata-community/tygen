FROM docker.io/flyio/flyctl AS flyctl

FROM ghcr.io/xe/devcontainer-base/pre/go AS workspace

RUN apt update \
  && apt -y install redis postgresql-client

COPY --from=flyctl /flyctl /usr/local/bin/flyctl
RUN ln -s /usr/local/bin/flyctl /usr/local/bin/fly

CMD ["/usr/bin/sleep", "infinity"]
