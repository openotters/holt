# Built by goreleaser: the holt binary is compiled outside and copied
# in, so the image is scratch + one static binary. The hub needs no
# shell, no libc (CGO is off, SQLite is pure Go) and makes no outbound
# TLS calls, so no CA bundle either.
FROM scratch

COPY holt /usr/bin/holt

# Non-root; the state dir is a mounted volume made writable via
# fsGroup (see the Helm chart's pod security context).
USER 65532:65532

ENTRYPOINT ["/usr/bin/holt"]

CMD ["hub"]
