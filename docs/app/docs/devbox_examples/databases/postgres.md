---
title: PostgreSQL
---
PostgreSQL can be automatically configured by Devbox via the built-in Postgres Plugin. This plugin will activate automatically when you install Postgres using `devbox add postgresql`

[**Example Repo**](https://github.com/jetify-com/devbox/tree/main/examples/databases/postgres)

[![Open In Devbox.sh](https://www.jetify.com/img/devbox/open-in-devbox.svg)](https://devbox.sh/open/templates/postgres)

## Adding Postgres to your Shell

You can install the PostgreSQL server and client by running`devbox add postgresql`. In some Linux distributions, you may also need to add `glibcLocales`, which can be added using `devbox add glibcLocales --platform=x86_64-linux,aarch64-linux`.

Alternatively, you can add the following to your devbox.json:

```json
  "packages": {
    "postgresql": "latest",
    "glibcLocales": {
      "version":   "latest",
      "platforms": ["x86_64-linux", "aarch64-linux"]
    }
  }
```

This will install the latest version of Postgres. You can find other installable versions of Postgres by running `devbox search postgresql`. You can also view the available versions on [Nixhub](https://www.nixhub.io/packages/postgresql)

## Installing Extensions with PostgreSQL

To install and use extensions in PostgreSQL, you should first install the `lib` output for Postgres by running `devbox add postgresql --outputs=out,lib`: 

```json
  "packages": {
    "postgresql": {
      "version": "latest",
      "outputs": ["out", "lib"]
    },
  }
```

You can then install the extension using `devbox add postgresqlXXpackages.extension`, where `XX` is the major version of Postgres that you are using. For example, to install Postgis for PostgreSQL 15, you can run: 

```bash
devbox add postgresql15Packages.postgis
```

## PostgreSQL Plugin Support

Devbox will automatically create the following configuration when you run `devbox add postgresql`:

### Services
* postgresql

You can use `devbox services start|stop postgresql` to start or stop the Postgres server in the background.

### Environment Variables

`PGHOST=./.devbox/virtenv/postgresql`
`PGDATA=./.devbox/virtenv/postgresql/data`
`NIX_PGLIBDIR=./.devbox/nix/profile/default/lib`

### Notes

To initialize PostgreSQL run `initdb`. You also need to create a database using `createdb <db-name>`

