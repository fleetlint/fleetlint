# The release image: GoReleaser puts the binary for each platform at
# $TARGETPLATFORM/fleetlint. fleetlint runs git, so the base has it.
FROM cgr.dev/chainguard/git:latest@sha256:f5f91faa60ed1b69deab0546d2154d2150f5f9163843793e72d45c51ff27153e
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/fleetlint /usr/bin/fleetlint
# A mounted checkout belongs to another user; git refuses it unless trusted.
ENV GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0=*
USER 65532
WORKDIR /repo
ENTRYPOINT ["/usr/bin/fleetlint"]
