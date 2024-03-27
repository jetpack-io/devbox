FROM jetpackio/devbox:latest

# Installing your devbox project
WORKDIR /code
USER ${DEVBOX_USER}:${DEVBOX_USER}
COPY --chown=${DEVBOX_USER}:${DEVBOX_USER} devbox.json devbox.json
COPY --chown=${DEVBOX_USER}:${DEVBOX_USER} devbox.lock devbox.lock
# Copy the rest of your project files and directories


RUN devbox run -- echo "Installed Packages."

CMD ["devbox", "shell"]
