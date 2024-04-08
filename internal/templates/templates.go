// Copyright 2024 Jetify Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package templates

var popularTemplates = []string{
	"node-npm",
	"node-typescript",
	"node-yarn",
	"python-pip",
	"python-pipenv",
	"python-poetry",
	"php",
	"ruby",
	"rust",
	"go",
}

var templates = map[string]string{
	"apache":          "examples/servers/apache/",
	"argo":            "examples/cloud_development/argo-workflows/",
	"bun":             "examples/development/bun/",
	"caddy":           "examples/servers/caddy/",
	"django":          "examples/stacks/django/",
	"dotnet":          "examples/development/csharp/hello-world/",
	"drupal":          "examples/stacks/drupal/",
	"elixir":          "examples/development/elixir/elixir_hello/",
	"fsharp":          "examples/development/fsharp/hello-world/",
	"go":              "examples/development/go/hello-world/",
	"gradio":          "examples/data_science/pytorch/gradio/",
	"haskell":         "examples/development/haskell/",
	"java-gradle":     "examples/development/java/gradle/hello-world/",
	"java-maven":      "examples/development/java/maven/hello-world/",
	"jekyll":          "examples/stacks/jekyll/",
	"jupyter":         "examples/data_science/jupyter/",
	"lapp-stack":      "examples/stacks/lapp-stack/",
	"laravel":         "examples/stacks/laravel/",
	"lepp-stack":      "examples/stacks/lepp-stack/",
	"llama":           "examples/data_science/llama/",
	"maelstrom":       "examples/cloud_development/maelstrom/",
	"minikube":        "examples/cloud_development/minikube/",
	"mariadb":         "examples/databases/mariadb/",
	"mysql":           "examples/databases/mysql/",
	"nginx":           "examples/servers/nginx/",
	"nim":             "examples/development/nim/spinnytest/",
	"node-npm":        "examples/development/nodejs/nodejs-npm/",
	"node-pnpm":       "examples/development/nodejs/nodejs-pnpm/",
	"node-typescript": "examples/development/nodejs/nodejs-typescript/",
	"node-yarn":       "examples/development/nodejs/nodejs-yarn/",
	"php":             "examples/development/php/latest/",
	"postgres":        "examples/databases/postgres/",
	"python-pip":      "examples/development/python/pip/",
	"python-pipenv":   "examples/development/python/pipenv/",
	"python-poetry":   "examples/development/python/poetry/poetry-demo/",
	"pytorch":         "examples/data_science/pytorch/basic-example/",
	"rails":           "examples/stacks/rails/",
	"redis":           "examples/databases/redis/",
	"ruby":            "examples/development/ruby/",
	"rust":            "examples/development/rust/rust-stable-hello-world/",
	"temporal":        "examples/cloud_development/temporal/",
	"tensorflow":      "examples/data_science/tensorflow/",
	"tutorial":        "examples/tutorial/",
	"zig":             "examples/development/zig/zig-hello-world/",
}
