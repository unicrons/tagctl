# Release image, built by GoReleaser (dockers_v2 in .goreleaser.yaml): the build
# context holds the binaries it already compiled, one directory per platform.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:d62067879de890a1def9da0e7766b7d20810a5e5555cef8506b752437d7a3e88

ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/tagctl /usr/local/bin/tagctl

USER nonroot:nonroot
WORKDIR /home/nonroot

ENTRYPOINT ["/usr/local/bin/tagctl"]
