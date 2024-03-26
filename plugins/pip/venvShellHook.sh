#!/bin/sh

if ! [ -d "$VENV_DIR" ]; then
    echo "Creating new venv environment in path: '${VENV_DIR}'"
    python3 -m venv "$VENV_DIR"
fi

if [ "${DEVBOX_ENTRYPOINT:-}" != "run" ]; then
    echo "You can activate the virtual environment by running 'source \$VENV_DIR/bin/activate'" >&2
fi