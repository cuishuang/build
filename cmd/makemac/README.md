[![Go Reference](https://pkg.go.dev/badge/golang.org/x/build/cmd/makemac.svg)](https://pkg.go.dev/golang.org/x/build/cmd/makemac)

# golang.org/x/build/cmd/makemac

The makemac command manages creating & destroying macOS VMs for the
builders. See the README in x/build/env/darwin/macstadium for some
more background.

## Deploying `makemac`

```
* On Linux,
  $ cd cmd/makemac
  $ CGO_ENABLED=0 go build golang.org/x/build/cmd/makemac
  $ scp -i ~/.ssh/id_ed25519_golang1 ./makemac gopher@macstadiumd.golang.org:makemac.new
  $ ssh -i ~/.ssh/id_ed25519_golang1 gopher@macstadiumd.golang.org

On that host,
  $ cp makemac makemac.old
  $ install makemac.new makemac
  $ sudo systemctl restart makemac
  $ sudo journalctl -f -u makemac     # watch it
```

## Updating `makemac.service`

```
* On Linux,
  $ scp -i ~/.ssh/id_ed25519_golang1 cmd/makemac/makemac.service gopher@macstadiumd.golang.org:makemac.service

On that host,
  $ sudo mv makemac.service /etc/systemd/system/makemac.service
  $ sudo systemctl daemon-reload
  $ sudo systemctl restart makemac
  $ sudo journalctl -f -u makemac     # watch it
```

## Checking that it's running:

```
$ curl -v http://macstadiumd.golang.org:8713
```

(Note that URL won't work in a browser due to HSTS requirements on
 *.golang.org)
