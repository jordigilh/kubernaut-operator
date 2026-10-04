FROM docker.io/library/alpine:3.22.1

RUN apk add --no-cache bash darkhttpd openssl \
    && ln -s /usr/local/bin/kind-contract /usr/local/bin/goose \
    && mkdir -p /opt/kind-contract-http \
    && printf 'ok\n' >/opt/kind-contract-http/healthz \
    && printf 'ok\n' >/opt/kind-contract-http/readyz

COPY entrypoint.sh /usr/local/bin/kind-contract
RUN chmod 0555 /usr/local/bin/kind-contract

USER 65532:65532
ENTRYPOINT ["/usr/local/bin/kind-contract"]
