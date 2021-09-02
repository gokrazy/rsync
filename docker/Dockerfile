# syntax=docker/dockerfile:1

FROM alpine

WORKDIR /

COPY gokr-rsyncd /usr/bin

USER nobody:nobody

EXPOSE 8730

ENTRYPOINT ["/usr/bin/gokr-rsyncd", "-listen=:8730"]
