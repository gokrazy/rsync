# Existing rsync implementation survey

| Language | URL                                                                      | Note                                                      | Server mode? |
|----------|--------------------------------------------------------------------------|-----------------------------------------------------------|--------------|
| C        | https://github.com/WayneD/rsync                                          | original implementation                                   | yes          |
| C        | https://github.com/kristapsdz/openrsync                                  | OpenBSD, good docs                                        | yes          |
| Java     | https://github.com/APNIC-net/repositoryd                                 | archived                                                  | yes          |
| Java     | https://github.com/JohannesBuchner/Jarsync/blob/master/jarsync/rsync.txt | archived, internet draft RFC “The rsync Network Protocol” | yes          |
| Java     | https://github.com/perlundq/yajsync#example                              |                                                           | yes          |
| C++      | https://github.com/gilbertchen/acrosync-library                          | commercial                                                | no           |
| Go       | https://github.com/jbreiding/rsync-go                                    | rsync algorithm                                           | no           |
| Rust     | https://github.com/sourcefrog/rsyn#why-do-this                           | client, “rsyn is rsync with no c”                         | no           |
| Go       | https://github.com/kaiakz/rsync-os                                       | only client/receiver                                      | no           |
| Go       | https://gist.github.com/knight42/6ad35ce6fbf96519259b43a8c3f37478        | proxy                                                     | no           |
| Go       | https://github.com/c4milo/gsync                                          |                                                           | no           |
