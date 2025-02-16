# vim:ft=Dockerfile
FROM fedora

# Install rsync (for running tests).
RUN dnf -y update && dnf -y install rsync openssh-clients go && dnf clean all

# Enable toolchain management (and the module proxy, which is a requirement) so
# that Go 1.23 (from Fedora) will use Go 1.24 for gokrazy/rsync (or whichever
# version we specify as language/toolchain version in our go.mod).
RUN go env -w \
    GOPROXY=https://proxy.golang.org,direct \
    GOSUMDB=sum.golang.org \
    GOTOOLCHAIN=auto
