<h1 align="center">Benchmarkoor</h1>

<p align="center">
  <img src="ui/public/img/logo_white.png" alt="Dispatchoor Logo" width="400">
</p>

## Overview

Benchmarkoor is a benchmarking tool for Ethereum execution clients. It runs standardized tests against multiple clients (Geth, Nethermind, Besu, Erigon, Reth, Nimbus) in isolated Docker containers and collects performance metrics.

## Documentation

- [Configuration Reference](docs/configuration.md) - All configuration options explained
- [Docker Guide](docs/docker.md) - Docker setup, requirements, and troubleshooting

## Docker Quickstart

The easiest way to get started is using Docker Compose:

Start the UI and API. By default the UI will be listening on [http://localhost:8080](http://localhost:8080) and the API on [http://localhost:9090](http://localhost:9090).
```bash
# Starts the UI (port 8080) and API (port 9090)
make docker-run

# Alternatively, you can also choose your own ports like
make docker-run UI_PORT=3000 API_PORT=9091
```

Next we'll run a benchmark. By default it will use the  [config.example.docker.yaml](config.example.docker.yaml) configuration file. By default, it just runs a subset of tests, via the `filter: bn128`. Have a look at the file and change it as you want. If you're just experimenting, you can leave it as it is.

To run the benchmark we can do the following:

```sh
# Run the default config.example.docker.yaml with all clients
make docker-run-benchmark

# Limit the client. In this case, just run geth
make docker-run-benchmark CLIENT=geth

# Run with your custom configuration
make docker-run-benchmark CONFIG=config.custom.yaml
```

After each run, if you refresh the UI, you should see new results there.

To stop the services:

```bash
make docker-down
```

## Development Quickstart

Build the binary
```sh
make build-core
```

Run the UI (On a different tab):
```sh
make run-ui
```

It should print the address where you can access it. By default it's http://localhost:5173/ .

Now we want to run benchmarkoor. We'll be using an example configuration file that contains some stateless tests. By default we'll be just running the `bn128` subset of that suite. Have a look at the config file for more details:
```sh
./bin/benchmarkoor run --config examples/configuration/config.stateless.eest.yaml
```

If you don't always want to build and run, you can also use it like this:

```sh
go run cmd/benchmarkoor/*.go run --config examples/configuration/config.stateless.eest.yaml
```

After the run, you should be able to see the results on the UI.

Note: If you want to limit your runs to a specific client that is on the config, you can either comment out those that you don't want, or use the `--limit-instance-client` flag. This allows you to limit the execution to certain clients. For example `--limit-instance-client=nethermind` would only run any instances that are of the `nethermind` client type.

Example:

```
./bin/benchmarkoor run \
      --config examples/configuration/config.stateless.eest.yaml \
      --limit-instance-client=nethermind
```

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
