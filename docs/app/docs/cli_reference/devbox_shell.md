# devbox shell

Start a new shell or run a command with access to your packages

## Synopsis

Start a new shell or run a command with access to your packages.   
If the --config flag is set, the shell will be started using the devbox.json found in the --config flag directory.   
If --config isn't set, then devbox recursively searches the current directory and its parents.

```bash
devbox shell [flags]
```

## Options

<!-- Markdown Table of Options -->
| Option | Description |
| --- | --- |
|  `-c, --config string`|  path to directory containing a devbox.json config file |
|  `-e, --env stringToString` |  environment variables to set in the devbox environment (default []) |
|  `--env-file string` | path to a file containing environment variables to set in the devbox environment |
|  `--environment string` | environment to use, when supported (e.g.secrets support dev, prod, preview.) (default "dev") |
| `--print-env` | Print a script to setup a devbox shell environment |
| `--pure` | If this flag is specified, devbox creates an isolated shell inheriting almost no variables from the current environment. A few variables, in particular HOME, USER and DISPLAY, are retained. |
| `-h, --help` | help for shell |
| `-q, --quiet` | Quiet mode: Suppresses logs. |

## SEE ALSO

* [devbox](./devbox.md)	 - Instant, easy, predictable shells and containers

