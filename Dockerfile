# Release image, built by GoReleaser (dockers_v2 in .goreleaser.yaml): the build
# context holds the binaries it already compiled, one directory per platform.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/tagctl /usr/local/bin/tagctl

USER nonroot:nonroot
WORKDIR /home/nonroot

ENTRYPOINT ["/usr/local/bin/tagctl"]
