# The release image: GoReleaser puts the binary for each platform at
# $TARGETPLATFORM/fleetlint. fleetlint runs git, so the base has it.
FROM cgr.dev/chainguard/git:latest@sha256:026a22f984cfdab8d9863d5e9c19d7a943f670cb56e0635b843e43edef428bcb
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/fleetlint /usr/bin/fleetlint
# A mounted checkout belongs to another user; git refuses it unless trusted.
ENV GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0=*
USER 65532
WORKDIR /repo
ENTRYPOINT ["/usr/bin/fleetlint"]
