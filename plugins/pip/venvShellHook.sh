#!/bin/sh

if ! [ -d "$VENV_DIR" ]; then
    echo "Creating new venv environment in path: '${VENV_DIR}'"
    python3 -m venv "$VENV_DIR"
    echo "You can activate the virtual environment by running '. \$VENV_DIR/bin/activate' (for fish shell, replace '.' with 'source')" >&2
fi

