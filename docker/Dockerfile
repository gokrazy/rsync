# syntax=docker/dockerfile:1

FROM alpine

WORKDIR /

COPY gokr-rsync /usr/bin

USER nobody:nobody

EXPOSE 8730

ENTRYPOINT ["/usr/bin/gokr-rsync", "--daemon", "--gokr.listen=:8730"]
