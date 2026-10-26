# The release image: GoReleaser puts the binary for each platform at
# $TARGETPLATFORM/fleetlint. fleetlint runs git, so the base has it.
FROM cgr.dev/chainguard/git:latest@sha256:65ffd786aa1b5d0cbfb33f4bf8a08755eb9a16205758edcf8f8afbb54f93badb
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/fleetlint /usr/bin/fleetlint
# A mounted checkout belongs to another user; git refuses it unless trusted.
ENV GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0=*
USER 65532
WORKDIR /repo
ENTRYPOINT ["/usr/bin/fleetlint"]
