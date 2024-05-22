---
title: RabbitMQ
---

RabbitMQ is a reliable message and streaming broker. You can configure RabbitMQ for your Devbox project by using our official [RabbitMQ Plugin](https://github.com/jetify-com/devbox-plugins/tree/main/rabbitmq)

## Adding RabbitMQ to your shell

You can start by adding the RabbitMQ server to your project by running `devbox add rabbitmq-server`.

```json
    "packages": [
        "rabbitmq-server@latest"
    ]
```

You can then add the RabbitMQ Plugin to your devbox.json by adding it to your `include` list:

```json
    "include": [
        "github:jetify-com/devbox-plugins?dir=rabbitmq"
    ]
```

Adding these packages and the plugin will configure Devbox for working with RabbitMQ.

## Starting the RabbitMQ Service

The RabbitMQ plugin will automatically create a service for you that can be run with `devbox service up`. The process-compose.yaml for this service is shown below:

```yaml
processes:
  rabbitmq:
    command: "rabbitmq-server"
    availability:
      restart: on_failure
      max_restarts: 5
    daemon: true
    shutdown:
      command: "rabbitmqctl shutdown"
  rabbitmq-logs:
    command: "tail -f $RABBITMQ_LOG_BASE/$RABBITMQ_NODENAME@$(hostname -s).log"
    availability:
      restart: "always"
```

The `rabbitmq` process starts the server as a daemon in the background, and shuts it down whenever you terminate process compose. The `rabbitmq-logs` service will tail the logs of process-compose, and display them in the process-compose UI. You can configure the services by modifying the environment variables as described below.

If you want to create your own version of the RabbitMQ service, you can create a process-compose.yaml in your project's root, and define a new process named `rabbitmq`. For more details, see the [process-compose documentation](https://f1bonacc1.github.io/process-compose/)

## Environment Variables

The plugin will create the following environment variables:

```bash
RABBITMQ_CONFIG_FILE = {{.DevboxDir}}/conf.d
# Points to the directory containing your rabbitmq.conf file
RABBITMQ_MNESIA_BASE = {{.Virtenv}}/mnesia
# Points to the directory where your node database store, and state files will be kept. Changing this variable is not recommended
RABBITMQ_ENABLED_PLUGINS_FILE = {{.DevboxDir}}/conf.d/enabled_plugins
# Tells rabbit mq where to store the file with your enabled plugins
RABBITMQ_LOG_BASE = {{.Virtenv}}/log
# Where the logs for your RabbitMQ instance will be stored
RABBITMQ_NODENAME = rabbit
# Default nodename for your rabbitmq instance. This variable is used in other settings
RABBITMQ_PID_FILE = {{.Virtenv}}/pid/$RABBITMQ_NODENAME.pid
# Creates RabbitMQ's pidfile in a local directory for your project. Changing this is not recommended.
```

You can override the default values of these variables using the `env` section of your `devbox.json` file. For a full list of environment variables used by RabbitMQ, see the [RabbitMQ Configuration Docs](https://www.rabbitmq.com/docs/configure#customise-environment).

## Files

The plugin will also create a default [`rabbitmq.conf`](https://github.com/jetify-com/devbox-plugins/blob/main/rabbitmq/config/rabbitmq.conf) file in your devbox.d directory, if one doesn't already exist there. This default file serves as a starting point, and you can modify it as needed.

For a full list of configuration options, see the [RabbitMQ Configuration Docs](https://www.rabbitmq.com/docs/configure)