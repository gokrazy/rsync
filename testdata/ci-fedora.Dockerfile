# vim:ft=Dockerfile
FROM fedora

# Install rsync (for running tests).
RUN dnf -y update && dnf -y install rsync openssh-clients && dnf clean all
