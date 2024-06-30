---
title: Valkey
---

Valkey can be configured automatically using Devbox's built in Valkey plugin. This plugin will activate automatically when you install Valkey using `devbox add valkey`

[**Example Repo**](https://github.com/jetify-com/devbox/tree/main/examples/databases/valkey)

[![Open In Devbox.sh](https://www.jetify.com/img/devbox/open-in-devbox.svg)](https://devbox.sh/open/templates/valkey)

## Adding Valkey to your shell

`devbox add valkey`, or in your Devbox.json

```json
    "packages": [
        "valkey@latest   "
    ],
```

This will install the latest version of Valkey. You can find other installable versions of Valkey by running `devbox search valkey`. You can also view the available versions on [Nixhub](https://www.nixhub.io/packages/valkey)

## Valkey Plugin Details

The Valkey plugin will automatically create the following configuration when you install Valkey with `devbox add`

### Services

* valkey

Use `devbox services start|stop [service]` to interact with services

### Helper Files

The following helper files will be created in your project directory:

* \{PROJECT_DIR\}/devbox.d/valkey/valkey.conf


### Environment Variables

```bash
VALKEY_PORT=6379
VALKEY_CONF=./devbox.d/valkey/valkey.conf
```

### Notes

Running `devbox services start valkey` will start valkey as a daemon in the background.

You can manually start Valkey in the foreground by running `valkey-server $VALKEY_CONF --port $VALKEY_PORT`.

Logs, pidfile, and data dumps are stored in `.devbox/virtenv/valkey`. You can change this by modifying the `dir` directive in `devbox.d/valkey/valkey.conf`
