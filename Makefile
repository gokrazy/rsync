all:
	go install github.com/gokrazy/rsync/cmd/gokr-rsyncd

run: all
	gokr-rsyncd -modulemap=pwd=$$PWD

test:
	go test -count=1 -mod=mod -v github.com/gokrazy/rsync/...
