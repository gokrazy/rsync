.PHONY: all run systemd test privileged-test docker raspi mac staticcheck

all:
	CGO_ENABLED=0 go install github.com/gokrazy/rsync/cmd/...

staticcheck:
	staticcheck ./...

run: all
	sudo ~/go/bin/gokr-rsyncd -modulemap=default=/etc/default

systemd: all
	sudo systemctl stop gokr-rsyncd.socket gokr-rsyncd.service && \
	sudo cp /home/michael/go/bin/gokr-rsyncd /usr/bin/ && \
	sudo cp systemd/gokr-rsyncd.socket systemd/gokr-rsyncd.service /etc/systemd/system/ && \
	sudo systemctl daemon-reload && \
	(sudo systemctl kill -f gokr-rsyncd.service; \
	sudo systemctl restart gokr-rsyncd.socket)

test:
	GOGC=off CGO_ENABLED=0 go test -fullpath ./...

privileged-test:
	GOGC=off CGO_ENABLED=0 sudo go test -fullpath ./integration/interop ./integration/receiver

docker:
	CGO_ENABLED=0 GOBIN=$$PWD/docker go install github.com/gokrazy/rsync/cmd/gokr-rsyncd
	(cd docker && docker build -t=stapelberg/gokrazy-rsync .)

router7:
	GOARCH=amd64 CGO_ENABLED=0 go install github.com/gokrazy/rsync/cmd/gokr-rsyncd && \
	(ssh router7.lan killall gokr-rsyncd || true) && \
	cp ~/go/bin/gokr-rsyncd /mnt/loop/ && \
	ssh router7.lan /perm/gokr-rsyncd -modulemap distri=/perm/srv/repo.distr1.org/distri/ -listen=10.0.0.1:8730 -monitoring_listen=10.0.0.1:8780

raspi:
	# -tags nonamespacing
	GOARCH=arm64 CGO_ENABLED=0 go install github.com/gokrazy/rsync/cmd/gokr-rsyncd && \
	ssh gokrazy.lan killall gokr-rsyncd && \
	cp ~/go/bin/linux_arm64/gokr-rsyncd /mnt/loop/ && \
	ssh gokrazy.lan /perm/gokr-rsyncd -modulemap pwd=/gokrazy -listen=:873

mac:
	GOARCH=arm64 GOOS=darwin CGO_ENABLED=0 go install github.com/gokrazy/rsync/cmd/gokr-rsyncd && \
	scp ~/go/bin/darwin_arm64/gokr-rsyncd m1a.lan: && \
	ssh m1a.lan ~/gokr-rsyncd -help
