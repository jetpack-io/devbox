# valkey-7.2.5

## valkey Notes

Running `devbox services start valkey` will start valkey as a daemon in the background.

You can manually start Valkey in the foreground by running `valkey-server $VALKEY_CONF --port $VALKEY_PORT`.

Logs, pidfile, and data dumps are stored in `.devbox/virtenv/valkey`. You can change this by modifying the `dir` directive in `devbox.d/valkey/valkey.conf`

## Services

* valkey

Use `devbox services start|stop [service]` to interact with services

## This plugin creates the following helper files

* ./devbox.d/valkey/valkey.conf

## This plugin sets the following environment variables

* VALKEY_PORT=6379
* VALKEY_CONF=./devbox.d/valkey/valkey.conf

To show this information, run `devbox info valkey`
