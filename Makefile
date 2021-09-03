.PHONY: all run systemd test docker raspi mac

all:
	CGO_ENABLED=0 go install github.com/gokrazy/rsync/cmd/gokr-rsyncd

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
	go test -count=1 -mod=mod -v github.com/gokrazy/rsync/internal/...
	go test -mod=mod -c && sudo ./rsync.test -test.v && ./rsync.test

docker:
	CGO_ENABLED=0 GOBIN=$$PWD/docker go install github.com/gokrazy/rsync/cmd/gokr-rsyncd
	(cd docker && docker build -t=stapelberg/gokrazy-rsync .)

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
