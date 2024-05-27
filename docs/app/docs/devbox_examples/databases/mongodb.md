---
title: MongoDB
---

MongoDB is a popular NoSQL database that is available on Nixpkgs. You can configure MongoDB for your Devbox project by using our official [MongoDB Plugin](https://github.com/jetify-com/devbox-plugins/tree/main/mongodb).

## Adding MongoDB to your Shell

You can start by adding the mongodb server to your project by running `devbox add mongodb`. We also recommend adding the MongoDB shell for interacting with your database using `devbox add mongosh`: 

```json
    "packages": [
        "mongodb@latest",
        "mongosh@latest",
    ]
```

You can add the MongoDB Plugin to your `devbox.json` by adding it to your `include` list:

```json
    "include": [
        "github:jetify-com/devbox-plugins?dir=mongodb"
    ]
```

Adding these packages and the plugin will configure Devbox for working with MongoDB. 

## Starting the MongoDB Service

The MongoDB plugin will automatically create a service for you that can be run with `devbox services up`. The process-compose.yaml for this default service is shown below:

```yaml
processes:
  mongodb:
    command: "mongod --config=$MONGODB_CONFIG --dbpath=$MONGODB_DATA --bind_ip_all"
    availability:
      restart: on_failure
      max_restarts: 5
```

You can configure this service by modifying the environment variable shown below. 

If you want to create your own version of the mongodb service, you can create a process-compose.yaml in your project's root, and define a new process named `mongodb`. For more details, see the [process-compose documentation](https://f1bonacc1.github.io/process-compose/)

### Environment Variables

The MongoDB plugin will configure the following environment variables

```bash
MONGODB_CONFIG = ./devbox.d/mongodb/mongod.conf
# Tells Devbox where to look for your mongod.conf file
MONGODB_DATA = ./devbox/virtenv/mongodb/data
# Tells Devbox where MongoDB's data directory should be located
```

You can override the default values of these variables using the `env` section of your devbox.json file.

### Files

The plugin will also create a default [`mongod.conf`] file in your `devbox.d` directory, if one doesn't already exist there. This default file mostly serves as a placeholder, and you can modify it as needed.

For a full list of configuration options, see the [MongoDB documentation](https://www.mongodb.com/docs/v6.0/reference/configuration-options/)
