# stupid Go modules not supporting using forked code without modifying go.mod
# https://stackoverflow.com/questions/61311436/how-to-fix-parsing-go-mod-module-declares-its-path-as-x-but-was-required-as-y#comment109850472_61311436
module github.com/joonas-fi/rsync

go 1.17

require (
	github.com/DavidGamba/go-getoptions v0.23.0
	github.com/google/go-cmp v0.5.6
	github.com/stapelberg/rsync-os v0.3.0
	golang.org/x/crypto v0.0.0-20210817164053-32db794688a5
)

require (
	github.com/coreos/go-systemd v0.0.0-20191104093116-d3cd4ed1dbcf // indirect
	github.com/kaiakz/ubuffer v0.0.0-20200803053910-dd1083087166 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/sys v0.0.0-20210615035016-665e8c7367d1 // indirect
)
