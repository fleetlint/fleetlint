# The release image: GoReleaser puts the binary for each platform at
# $TARGETPLATFORM/fleetlint. fleetlint runs git, so the base has it.
FROM cgr.dev/chainguard/git:latest@sha256:f30343d11878c992336af4f150dc5e2ab6200e4e5bc5e3a366aebb1665abbf65
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/fleetlint /usr/bin/fleetlint
# A mounted checkout belongs to another user; git refuses it unless trusted.
ENV GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0=*
USER 65532
WORKDIR /repo
ENTRYPOINT ["/usr/bin/fleetlint"]
