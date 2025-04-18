# vim:ft=Dockerfile
FROM debian:sid

RUN echo force-unsafe-io > /etc/dpkg/dpkg.cfg.d/docker-apt-speedup
# Paper over occasional network flakiness of some mirrors.
RUN echo 'APT::Acquire::Retries "5";' > /etc/apt/apt.conf.d/80retry

# NOTE: I tried exclusively using gce_debian_mirror.storage.googleapis.com
# instead of httpredir.debian.org, but the results (Fetched 123 MB in 36s (3357
# kB/s)) are not any better than httpredir.debian.org (Fetched 123 MB in 34s
# (3608 kB/s)). Hence, let’s stick with httpredir.debian.org (default) for now.

# Install rsync (for running tests).
RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    rsync ssh git ca-certificates build-essential golang-go && \
    rm -rf /var/lib/apt/lists/*

# Build openrsync (for running tests).
RUN cd /usr/src && \
    git clone https://github.com/kristapsdz/openrsync && \
    cd /usr/src/openrsync && \
    ./configure && make -j8 && make install
