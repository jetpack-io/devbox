# devbox generate devcontainer

Generate Dockerfile and devcontainer.json files under .devcontainer/ directory

## Synopsis

Generate Dockerfile and devcontainer.json files necessary to run VSCode in remote container environments.

```bash
devbox generate devcontainer [flags]
```

The generated Dockerfile only copies `devbox.json` and `devbox.lock` files into the container. Users need to modify this file to include copying their project files as well.

### Options

<!-- Markdown Table of Options -->
| Option | Description |
| --- | --- |
| `-f, --force` | force overwrite on existing files |
| `--root-user` | use `root` as the user for container. Installs nix as single-user mode in Dockerfile |
| `-h, --help` | help for devcontainer |
| `-q, --quiet` | Quiet mode: Suppresses logs. |


### SEE ALSO

* [devbox generate](devbox_generate.md)	 - 

