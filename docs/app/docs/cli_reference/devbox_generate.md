# devbox generate

Top level command for generating Devcontainers,  Dockerfiles, and other useful files for your Devbox Project. 

```bash
devbox generate <devcontainer|dockerfile|direnv|readme> [flags]
```

## Options

<!-- Markdown table of options -->
| Option | Description |
| --- | --- |
| `-c, --config string` | path to directory containing a devbox.json config file |
| `-h, --help` | help for generate |
| `-q, --quiet` | Quiet mode: Suppresses logs. |

## Subcommands

* [devbox generate devcontainer](devbox_generate_devcontainer.md)	 - Generate Dockerfile and devcontainer.json files under .devcontainer/ directory
* [devbox generate direnv](devbox_generate_direnv.md)  - Generate a .envrc file to use with direnv
* [devbox generate dockerfile](devbox_generate_dockerfile.md)	 - Generate a Dockerfile that replicates devbox shell
* [devbox generate readme](devbox_generate_dockerfile.md)	 -  Generate markdown readme file for your project

## SEE ALSO

* [devbox](devbox.md)	 - Instant, easy, predictable development environments

