---
title: Using Devbox in CI/CD with GitHub Actions
---

This guide explains how to use Devbox in CI/CD using GitHub Actions. The [devbox-install-action](https://github.com/marketplace/actions/devbox-installer) will install Devbox CLI and any packages + configuration defined in your `devbox.json` file. You can then run tasks or scripts within `devbox shell` to reproduce your environment.

This GitHub Action also supports caching the packages and dependencies installed in your `devbox.json`, which can significantly improve CI build times. 

## Usage

`devbox-install-action` is available on the [GitHub Marketplace](https://github.com/marketplace/actions/devbox-installer) 

In your project's workflow YAML, add the following step: 

```yaml
- name: Install devbox
  uses: jetify-com/devbox-install-action@v0.11.0
```

## Example Workflow

The workflow below shows how to use the action to install Devbox, and then run arbitrary commands or [Devbox Scripts](../guides/scripts.md) in your shell.

```yaml
name: Testing with devbox

on: push

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Install devbox
        uses: jetify-com/devbox-install-action@v0.11.0

      - name: Run arbitrary commands
        run: devbox run -- echo "done!"

      - name: Run a script called test
        run: devbox run test
```

## Configuring the Action

See the [GitHub Marketplace page](https://github.com/marketplace/actions/devbox-installer) for the latest configuration settings and an example.

For stability over new features and bug fixes, consider pinning `devbox-version`. Remember to update this pinned version when you update your local Devbox via `devbox version update`.
